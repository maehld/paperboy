package paperboy

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/internal/download"
)

// releaseFeed is a fake release API with assets, used by the download tests.
type releaseFeed struct {
	t     *testing.T
	tags  []string
	files map[string][]byte // "<tag>/<name>"
	pre   map[string]bool   // tag -> published as a prerelease (ADR 0089)
}

func newReleaseFeed(t *testing.T, tags ...string) (*releaseFeed, *httptest.Server) {
	f := &releaseFeed{t: t, tags: tags, files: map[string][]byte{}, pre: map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/me/paperboy/releases", func(w http.ResponseWriter, r *http.Request) {
		type asset struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		}
		type release struct {
			TagName    string  `json:"tag_name"`
			Prerelease bool    `json:"prerelease"`
			Assets     []asset `json:"assets"`
		}
		var out []release
		for _, tag := range f.tags {
			rel := release{TagName: tag, Prerelease: f.pre[tag]}
			for key := range f.files {
				fileTag, name, ok := strings.Cut(key, "/")
				if ok && fileTag == tag {
					rel.Assets = append(rel.Assets, asset{Name: name,
						URL: "http://" + r.Host + "/releases/download/" + tag + "/" + name})
				}
			}
			out = append(out, rel)
		}
		json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/releases/download/", func(w http.ResponseWriter, r *http.Request) {
		data, ok := f.files[strings.TrimPrefix(r.URL.Path, "/releases/download/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(data)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return f, srv
}

// add stores an asset and appends its checksum to the release's SHA256SUMS.
func (f *releaseFeed) add(tag, name string, data []byte) {
	f.files[tag+"/"+name] = data
	sums := f.files[tag+"/SHA256SUMS"]
	sum := sha256.Sum256(data)
	f.files[tag+"/SHA256SUMS"] = append(sums, []byte(hex.EncodeToString(sum[:])+"  "+name+"\n")...)
}

// zipOf builds an archive with the given entries.
func zipOf(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// pluginEntry is how the test program appears in the plugin zip: the program is
// named <plugin>-v<version> (ADR 0075).
func pluginEntry(version string) string {
	if runtime.GOOS == "windows" {
		return "plugins/testplugin-v" + version + ".exe"
	}
	return "plugins/testplugin-v" + version
}

// downloadConfig builds an instance configuration with the download section.
func downloadConfig(t *testing.T, source, plugins, firmware string) string {
	t.Helper()
	return downloadConfigInterval(t, source, plugins, firmware, "")
}

// downloadConfigInterval adds a download interval (ADR 0062).
func downloadConfigInterval(t *testing.T, source, plugins, firmware, interval string) string {
	t.Helper()
	return fmt.Sprintf(timezoneConfig, "UTC") + `
download:
  source: ` + source + `
` + interval + `firmware:
  update: auto
` + firmware + `
plugins:
  testplugin:
` + plugins + `
    env: { PAPERBOY_TEST_PLUGIN: "1" }
    config: { secret: "x" }
`
}

// testDownloadServer builds an instance and a server for the download tests.
func testDownloadServer(t *testing.T, config string) *Server {
	t.Helper()
	dir := t.TempDir()
	writePageFile(t, filepath.Join(dir, "pages", "eins.e1001.yaml"), textPage("eins"))
	writePageFile(t, filepath.Join(dir, "plans", "zimmer.yaml"), "rules: [{name: immer, pages: [eins]}]\n")
	writeConfig(t, dir, config)
	writeDisplayFile(t, dir, "e1001", timezoneDisplay)
	writeDeviceFile(t, dir, "flur", planDeviceFile)
	return configServer(t, dir)
}

func TestDownloadInstallsPluginsAndFirmware(t *testing.T) {
	feed, srv := newReleaseFeed(t, "v2.1.0")
	feed.add("v2.1.0", download.PluginAsset(), zipOf(t, map[string]string{
		pluginEntry("2.1.0"):   "program",
		"plugins/other-v0.1.0": "another plugin",
	}))
	feed.add("v2.1.0", download.FirmwareAsset, zipOf(t, map[string]string{
		"firmware/e1001-v1.0.0.bin":  "image",
		"firmware/sticky-v1.0.0.bin": "not configured",
	}))

	s := testDownloadServer(t, downloadConfig(t, srv.URL+"/repos/me/paperboy/releases", "", ""))
	if err := s.downloadOnce(context.Background(), s.config()); err != nil {
		t.Fatal(err)
	}
	cfg := s.config()
	program, version := pluginProgram(cfg.PluginsDir, "testplugin", "")
	if !strings.HasSuffix(program, "testplugin-v2.1.0"+exeSuffix()) || version != "v2.1.0" {
		t.Fatalf("plugin program: %q %q", program, version)
	}
	if _, err := os.Stat(filepath.Join(cfg.FirmwareDir, "e1001-v1.0.0.bin")); err != nil {
		t.Fatalf("firmware image missing: %v", err)
	}
	// Every image of the bundle is installed: a display type's name says
	// nothing about the hardware a device reports (ADR 0061).
	if _, err := os.Stat(filepath.Join(cfg.FirmwareDir, "sticky-v1.0.0.bin")); err != nil {
		t.Fatalf("image of an unconfigured hardware type missing: %v", err)
	}
	// The host now runs the downloaded program (spec changed, ADR 0049).
	spec := cfg.pluginSpecs()["testplugin"]
	if spec.Version != "v2.1.0" || !strings.HasSuffix(spec.Command[0], "testplugin-v2.1.0"+exeSuffix()) {
		t.Fatalf("plugin spec: %+v", spec)
	}
	// A device of the display type gets the downloaded image offered.
	dev := planDevice(t, s)
	dev.req.Hardware = "e1001"
	dev.req.Battery = &syncBattery{Volts: 3.9}
	if offer := offerOf(displayPage(t, s, dev)); offer == nil || offer["version"] != "1.0.0" ||
		offer["path"] != "/firmware/e1001-v1.0.0.bin" {
		t.Fatalf("firmware offer: %v", offer)
	}
	// The page says when the program and the image arrived: the file times of
	// what Paperboy installed.
	arrived := time.Date(2026, 3, 4, 5, 6, 0, 0, time.UTC)
	for _, file := range []string{program, filepath.Join(cfg.FirmwareDir, "e1001-v1.0.0.bin")} {
		if err := os.Chtimes(file, arrived, arrived); err != nil {
			t.Fatal(err)
		}
	}
	body := htmlRequest(t, s, "/ui/downloads").Body.String()
	for _, want := range []string{"Release checks", "testplugin", "v2.1.0", "e1001", "e1001-v1.0.0.bin", "Downloaded", "2026-03-04 05:06"} {
		if !strings.Contains(body, want) {
			t.Fatalf("downloads page lacks %q: %s", want, body)
		}
	}
	if strings.Contains(body, "/ui/editor/device") {
		t.Fatalf("downloads page still lists the devices: %s", body)
	}
	// The dashboard keeps a link, not the card: a running check must not
	// reload every device card.
	dash := htmlRequest(t, s, "/ui").Body.String()
	if strings.Contains(dash, "Check now") || !strings.Contains(dash, "href=\"/ui/downloads\"") {
		t.Fatalf("dashboard still carries the download card: %s", dash)
	}
}

func TestDownloadHonoursPins(t *testing.T) {
	feed, srv := newReleaseFeed(t, "plugins-v2.4.0", "v2.3.0", "v2.1.0", "v2.0.0")
	feed.add("plugins-v2.4.0", download.PluginAsset(), zipOf(t, map[string]string{pluginEntry("1.5.0"): "program newer"}))
	for _, rel := range []struct{ tag, plugin, image string }{
		{"v2.3.0", "1.4.0", "1.1.0"},
		{"v2.1.0", "1.3.0", "1.0.0"},
		{"v2.0.0", "1.2.0", "0.9.0"},
	} {
		feed.add(rel.tag, download.PluginAsset(), zipOf(t, map[string]string{pluginEntry(rel.plugin): "program " + rel.tag}))
		feed.add(rel.tag, download.FirmwareAsset,
			zipOf(t, map[string]string{"firmware/e1001-v" + rel.image + ".bin": "image " + rel.tag}))
	}

	// A pin names a plugin version, not a release tag (ADR 0075).
	config := downloadConfig(t, srv.URL+"/repos/me/paperboy/releases",
		"    version: v1.2.0\n", `  version: v0.9.0
`)
	s := testDownloadServer(t, config)
	if err := s.downloadOnce(context.Background(), s.config()); err != nil {
		t.Fatal(err)
	}
	cfg := s.config()
	program, version := pluginProgram(cfg.PluginsDir, "testplugin", "v1.2.0")
	if !strings.HasSuffix(program, "testplugin-v1.2.0"+exeSuffix()) || version != "v1.2.0" {
		t.Fatalf("pinned plugin: %q %q", program, version)
	}
	for _, other := range []string{"v1.3.0", "v1.4.0", "v1.5.0"} {
		if _, err := os.Stat(filepath.Join(cfg.PluginsDir, "testplugin-"+other+exeSuffix())); !os.IsNotExist(err) {
			t.Fatalf("a pinned plugin must not install %s", other)
		}
	}
	if _, err := os.Stat(filepath.Join(cfg.FirmwareDir, "e1001-v0.9.0.bin")); err != nil {
		t.Fatalf("pinned image missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.FirmwareDir, "e1001-v1.0.0.bin")); !os.IsNotExist(err) {
		t.Fatal("the newer image must not be installed with a pin")
	}
	dev := planDevice(t, s)
	dev.req.Hardware = "e1001"
	dev.req.Firmware = "1.0.0" // the device runs a newer image: the pin is a rollback
	dev.req.Battery = &syncBattery{Volts: 3.9}
	if offer := offerOf(displayPage(t, s, dev)); offer == nil || offer["version"] != "0.9.0" {
		t.Fatalf("pinned offer: %v", offer)
	}
}

// TestDownloadSkipsHandPlacedProgram verifies that an unversioned program wins
// and keeps the download away (ADR 0060).
func TestDownloadSkipsHandPlacedProgram(t *testing.T) {
	feed, srv := newReleaseFeed(t, "v2.1.0")
	feed.add("v2.1.0", download.PluginAsset(), zipOf(t, map[string]string{pluginEntry("2.1.0"): "program"}))
	feed.add("v2.1.0", download.FirmwareAsset, zipOf(t, map[string]string{"firmware/e1001-v1.0.0.bin": "image"}))
	s := testDownloadServer(t, downloadConfig(t, srv.URL+"/repos/me/paperboy/releases", "", ""))
	if err := os.MkdirAll(s.config().PluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(s.config().PluginsDir, "testplugin"+exeSuffix())
	if err := os.WriteFile(own, []byte("own build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.downloadOnce(context.Background(), s.config()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(s.config().PluginsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), "-v") {
			t.Fatalf("nothing may be downloaded next to a hand-placed program: %s", e.Name())
		}
	}
}

// TestDownloadUpdatesDownloadedPlugin verifies that a newer plugin version in a
// release replaces the program in use with a new file next to the old one
// (ADR 0062, 0075).
func TestDownloadUpdatesDownloadedPlugin(t *testing.T) {
	feed, srv := newReleaseFeed(t, "v2.1.0")
	feed.add("v2.1.0", download.PluginAsset(), zipOf(t, map[string]string{pluginEntry("1.0.0"): "old program"}))
	feed.add("v2.1.0", download.FirmwareAsset, zipOf(t, map[string]string{"firmware/e1001-v1.0.0.bin": "image"}))
	s := testDownloadServer(t, downloadConfig(t, srv.URL+"/repos/me/paperboy/releases", "", ""))
	if err := s.downloadOnce(context.Background(), s.config()); err != nil {
		t.Fatal(err)
	}

	// A newer release with the same plugin version downloads nothing: only the
	// plugin's own version decides (ADR 0075).
	feed.tags = []string{"v2.2.0", "v2.1.0"}
	feed.add("v2.2.0", download.PluginAsset(), zipOf(t, map[string]string{pluginEntry("1.0.0"): "same program"}))
	feed.add("v2.2.0", download.FirmwareAsset, zipOf(t, map[string]string{"firmware/e1001-v1.0.0.bin": "image"}))
	if err := s.downloadOnce(context.Background(), s.config()); err != nil {
		t.Fatal(err)
	}
	if _, version := pluginProgram(s.config().PluginsDir, "testplugin", ""); version != "v1.0.0" {
		t.Fatalf("an unchanged plugin version must not install anything, got %q", version)
	}

	feed.tags = []string{"v2.3.0", "v2.2.0", "v2.1.0"}
	feed.add("v2.3.0", download.PluginAsset(), zipOf(t, map[string]string{pluginEntry("1.1.0"): "new program"}))
	feed.add("v2.3.0", download.FirmwareAsset, zipOf(t, map[string]string{"firmware/e1001-v1.1.0.bin": "image"}))
	if err := s.downloadOnce(context.Background(), s.config()); err != nil {
		t.Fatal(err)
	}

	cfg := s.config()
	program, version := pluginProgram(cfg.PluginsDir, "testplugin", "")
	if !strings.HasSuffix(program, "testplugin-v1.1.0"+exeSuffix()) || version != "v1.1.0" {
		t.Fatalf("plugin program: %q %q", program, version)
	}
	// The old version stays for a manual rollback, and the host restarts the
	// plugin on its next use because the resolved spec changed (ADR 0049).
	if _, err := os.Stat(filepath.Join(cfg.PluginsDir, "testplugin-v1.0.0"+exeSuffix())); err != nil {
		t.Fatalf("old plugin version missing: %v", err)
	}
	spec := cfg.pluginSpecs()["testplugin"]
	if spec.Version != "v1.1.0" || !strings.HasSuffix(spec.Command[0], "testplugin-v1.1.0"+exeSuffix()) {
		t.Fatalf("plugin spec: %+v", spec)
	}

	// Deleting the newer file rolls back to the older one; the same run does
	// not reinstall what the operator removed (ADR 0062).
	if err := os.Remove(filepath.Join(cfg.PluginsDir, "testplugin-v1.1.0"+exeSuffix())); err != nil {
		t.Fatal(err)
	}
	if err := s.downloadOnce(context.Background(), s.config()); err != nil {
		t.Fatal(err)
	}
	program, version = pluginProgram(cfg.PluginsDir, "testplugin", "")
	if !strings.HasSuffix(program, "testplugin-v1.0.0"+exeSuffix()) || version != "v1.0.0" {
		t.Fatalf("deleted version must fall back: %q %q", program, version)
	}
	if _, err := os.Stat(filepath.Join(cfg.PluginsDir, "testplugin-v1.1.0"+exeSuffix())); !os.IsNotExist(err) {
		t.Fatal("the deleted version must not be installed again in the same run")
	}
}

// TestDownloadInterval verifies the configurable cadence and that a reload with
// a shorter interval applies without a restart (ADR 0062).
func TestDownloadInterval(t *testing.T) {
	feed, srv := newReleaseFeed(t, "v2.1.0")
	feed.add("v2.1.0", download.PluginAsset(), zipOf(t, map[string]string{pluginEntry("1.0.0"): "old program"}))
	feed.add("v2.1.0", download.FirmwareAsset, zipOf(t, map[string]string{"firmware/e1001-v1.0.0.bin": "image"}))
	source := srv.URL + "/repos/me/paperboy/releases"
	config := downloadConfigInterval(t, source, "", "", "  interval: 6h\n")
	s := testDownloadServer(t, config)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	// The first check is due right away.
	s.checkDownloads(context.Background())

	feed.tags = []string{"v2.3.0", "v2.1.0"}
	feed.add("v2.3.0", download.PluginAsset(), zipOf(t, map[string]string{pluginEntry("1.1.0"): "new program"}))
	feed.add("v2.3.0", download.FirmwareAsset, zipOf(t, map[string]string{"firmware/e1001-v1.0.0.bin": "image"}))
	now = now.Add(5 * time.Hour)
	s.checkDownloads(context.Background())
	if _, version := pluginProgram(s.config().PluginsDir, "testplugin", ""); version != "v1.0.0" {
		t.Fatalf("a check before the interval must not run, got %q", version)
	}

	// A reload with a shorter interval makes the next wake-up check at once.
	writeConfig(t, s.config().Dir, downloadConfigInterval(t, source, "", "", "  interval: 1h0m\n"))
	now = now.Add(2 * time.Hour)
	s.checkDownloads(context.Background())
	if _, version := pluginProgram(s.config().PluginsDir, "testplugin", ""); version != "v1.1.0" {
		t.Fatalf("the shorter interval from the reload must apply, got %q", version)
	}
}

func TestDownloadIntervalValidation(t *testing.T) {
	cfg, _, err := loadConfig(t, downloadConfigInterval(t, "https://example.invalid", "", "", ""))
	if err != nil || cfg.Download.Interval != defaultDownloadInterval {
		t.Fatalf("default interval: %v %v", cfg, err)
	}
	if _, _, err := loadConfig(t, downloadConfigInterval(t, "https://example.invalid", "", "", "  interval: 30m\n")); err == nil ||
		!strings.Contains(err.Error(), "at least 1h") {
		t.Fatalf("short interval: %v", err)
	}
}

// TestDownloadCheckEndpoint verifies that the downloads page's button starts a
// check in the background and returns at once (ADR 0062).
func TestDownloadCheckEndpoint(t *testing.T) {
	feed, srv := newReleaseFeed(t, "v2.1.0")
	feed.add("v2.1.0", download.PluginAsset(), zipOf(t, map[string]string{pluginEntry("1.0.0"): "program"}))
	feed.add("v2.1.0", download.FirmwareAsset, zipOf(t, map[string]string{"firmware/e1001-v1.0.0.bin": "image"}))
	s := testDownloadServer(t, downloadConfig(t, srv.URL+"/repos/me/paperboy/releases", "", ""))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/ui/download", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/ui/downloads" {
		t.Fatalf("response: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if running, _, _, _, _, _ := s.download.view(); !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the check did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, version := pluginProgram(s.config().PluginsDir, "testplugin", ""); version != "v1.0.0" {
		t.Fatalf("the button must install the plugin, got %q", version)
	}
	body := htmlRequest(t, s, "/ui/downloads").Body.String()
	if !strings.Contains(body, "Check now") {
		t.Fatalf("the downloads page lacks the check button: %s", body)
	}
}

// TestDownloadsPageWithoutDownloadSection explains the missing section instead
// of drawing an empty page, and the dashboard links the page.
func TestDownloadsPageWithoutDownloadSection(t *testing.T) {
	s, _ := testServer(t)
	body := htmlRequest(t, s, "/ui/downloads").Body.String()
	for _, want := range []string{"Downloads", "No download section", "Plugins", "Firmware images", "Devices"} {
		if !strings.Contains(body, want) {
			t.Fatalf("downloads page lacks %q: %s", want, body)
		}
	}
	if !strings.Contains(htmlRequest(t, s, "/ui").Body.String(), `href="/ui/downloads"`) {
		t.Fatal("the dashboard does not link the downloads page")
	}
}

// exeSuffix is the platform suffix of a plugin program.
func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// prereleaseFeed is a release feed with one final and one prerelease release,
// each carrying a plugin program and a firmware image of the same version.
func prereleaseFeed(t *testing.T) (*releaseFeed, *httptest.Server) {
	t.Helper()
	feed, srv := newReleaseFeed(t, "v2.2.0-rc.1", "v2.1.0")
	feed.pre["v2.2.0-rc.1"] = true
	feed.add("v2.2.0-rc.1", download.PluginAsset(), zipOf(t, map[string]string{pluginEntry("1.1.0-rc.1"): "rc program"}))
	feed.add("v2.2.0-rc.1", download.FirmwareAsset, zipOf(t, map[string]string{"firmware/e1001-v1.1.0-rc.1.bin": "rc image"}))
	feed.add("v2.1.0", download.PluginAsset(), zipOf(t, map[string]string{pluginEntry("1.0.0"): "released program"}))
	feed.add("v2.1.0", download.FirmwareAsset, zipOf(t, map[string]string{"firmware/e1001-v1.0.0.bin": "released image"}))
	return feed, srv
}

// TestDownloadChannels verifies that the channel decides which delivery a check
// may install (ADR 0089).
func TestDownloadChannels(t *testing.T) {
	for _, tc := range []struct {
		name    string
		channel string
		program string
		image   string
	}{
		{"stable", "", "testplugin-v1.0.0", "e1001-v1.0.0.bin"},
		{"latest", "  plugin_channel: latest\n  firmware_channel: latest\n", "testplugin-v1.1.0-rc.1", "e1001-v1.1.0-rc.1.bin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, srv := prereleaseFeed(t)
			s := testDownloadServer(t, downloadConfigInterval(t, srv.URL+"/repos/me/paperboy/releases", "", "", tc.channel))
			if err := s.downloadOnce(context.Background(), s.config()); err != nil {
				t.Fatal(err)
			}
			cfg := s.config()
			program, _ := pluginProgram(cfg.PluginsDir, "testplugin", "")
			if !strings.HasSuffix(program, tc.program+exeSuffix()) {
				t.Fatalf("plugin program: %q", program)
			}
			if _, err := os.Stat(filepath.Join(cfg.FirmwareDir, tc.image)); err != nil {
				t.Fatalf("firmware image %s: %v", tc.image, err)
			}
			// The other channel's image is not touched: a check installs one
			// delivery, it does not mix them.
			other := "e1001-v1.1.0-rc.1.bin"
			if tc.name == "latest" {
				other = "e1001-v1.0.0.bin"
			}
			if _, err := os.Stat(filepath.Join(cfg.FirmwareDir, other)); !os.IsNotExist(err) {
				t.Fatalf("channel %s installed %s", tc.name, other)
			}
		})
	}
}

// TestDownloadPinBypassesChannel verifies that a pin names a version, not a
// delivery: a version out of a prerelease release is installed although the
// channel is stable (ADR 0089).
func TestDownloadPinBypassesChannel(t *testing.T) {
	feed, srv := newReleaseFeed(t, "plugins-v2.2.0-rc.1", "v2.1.0")
	feed.pre["plugins-v2.2.0-rc.1"] = true
	feed.add("plugins-v2.2.0-rc.1", download.PluginAsset(), zipOf(t, map[string]string{pluginEntry("1.5.0-rc.1"): "pinned rc"}))
	feed.add("v2.1.0", download.FirmwareAsset, zipOf(t, map[string]string{"firmware/e1001-v1.0.0.bin": "image"}))
	s := testDownloadServer(t, downloadConfig(t, srv.URL+"/repos/me/paperboy/releases", "    version: v1.5.0-rc.1\n", ""))
	if err := s.downloadOnce(context.Background(), s.config()); err != nil {
		t.Fatal(err)
	}
	program, version := pluginProgram(s.config().PluginsDir, "testplugin", "v1.5.0-rc.1")
	if !strings.HasSuffix(program, "testplugin-v1.5.0-rc.1"+exeSuffix()) || version != "v1.5.0-rc.1" {
		t.Fatalf("pinned prerelease: %q %q", program, version)
	}
}

// TestDownloadChannelSkipsPrereleaseInAStableRelease covers the second guard: a
// release nobody flagged must not smuggle an rc into stable (ADR 0089).
func TestDownloadChannelSkipsPrereleaseInAStableRelease(t *testing.T) {
	feed, srv := newReleaseFeed(t, "v2.1.0")
	feed.add("v2.1.0", download.PluginAsset(), zipOf(t, map[string]string{pluginEntry("1.0.0-rc.1"): "rc program"}))
	feed.add("v2.1.0", download.FirmwareAsset, zipOf(t, map[string]string{"firmware/e1001-v1.0.0-rc.1.bin": "rc image"}))
	s := testDownloadServer(t, downloadConfig(t, srv.URL+"/repos/me/paperboy/releases", "", ""))
	if err := s.downloadOnce(context.Background(), s.config()); err != nil {
		t.Fatal(err)
	}
	if program, _ := pluginProgram(s.config().PluginsDir, "testplugin", ""); program != "" {
		t.Fatalf("stable installed a prerelease program: %q", program)
	}
	entries, err := os.ReadDir(s.config().FirmwareDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("stable installed a prerelease image: %v", entries)
	}
}

// TestDownloadChannelValidation checks the two keys and their defaults.
func TestDownloadChannelValidation(t *testing.T) {
	cfg, _, err := loadConfig(t, downloadConfig(t, "https://example.invalid", "", ""))
	if err != nil || cfg.Download.PluginChannel != channelStable || cfg.Download.FirmwareChannel != channelStable {
		t.Fatalf("default channels: %+v %v", cfg.Download, err)
	}
	cfg, _, err = loadConfig(t, downloadConfigInterval(t, "https://example.invalid", "", "",
		"  plugin_channel: latest\n"))
	if err != nil || cfg.Download.PluginChannel != channelLatest || cfg.Download.FirmwareChannel != channelStable {
		t.Fatalf("one key alone: %+v %v", cfg.Download, err)
	}
	if _, _, err := loadConfig(t, downloadConfigInterval(t, "https://example.invalid", "", "",
		"  firmware_channel: beta\n")); err == nil || !strings.Contains(err.Error(), "expected stable or latest") {
		t.Fatalf("unknown channel: %v", err)
	}
}

// TestDownloadCacheKeepsOneFilePerTrain verifies the cache layout: one directory
// per train, one zip per asset, and the learned entry lists survive it
// (ADR 0090).
func TestDownloadCacheKeepsOneFilePerTrain(t *testing.T) {
	feed, srv := newReleaseFeed(t, "v2.1.0")
	feed.add("v2.1.0", download.PluginAsset(), zipOf(t, map[string]string{pluginEntry("1.0.0"): "program"}))
	feed.add("v2.1.0", download.FirmwareAsset, zipOf(t, map[string]string{"firmware/e1001-v1.0.0.bin": "image"}))
	s := testDownloadServer(t, downloadConfig(t, srv.URL+"/repos/me/paperboy/releases", "", ""))
	check := func() {
		t.Helper()
		if err := s.downloadOnce(context.Background(), s.config()); err != nil {
			t.Fatal(err)
		}
		for _, train := range []string{"plugins", "firmware"} {
			dir := filepath.Join(s.config().Dir, "state", "download", train)
			files := cacheFiles(t, dir)
			if len(files) != 1 {
				t.Fatalf("%s cache: %v", train, files)
			}
			if _, err := os.Stat(filepath.Join(dir, "cache.json")); err != nil {
				t.Fatalf("%s cache.json: %v", train, err)
			}
		}
	}
	check()
	feed.tags = []string{"v2.2.0", "v2.1.0"}
	feed.add("v2.2.0", download.PluginAsset(), zipOf(t, map[string]string{pluginEntry("1.1.0"): "newer program"}))
	feed.add("v2.2.0", download.FirmwareAsset, zipOf(t, map[string]string{"firmware/e1001-v1.1.0.bin": "newer image"}))
	check()
	if _, version := pluginProgram(s.config().PluginsDir, "testplugin", ""); version != "v1.1.0" {
		t.Fatalf("the second check must install the newer program, got %q", version)
	}
	// The programs and images are the inventory: both versions stay (ADR 0090).
	for _, name := range []string{"testplugin-v1.0.0", "testplugin-v1.1.0"} {
		if _, err := os.Stat(filepath.Join(s.config().PluginsDir, name+exeSuffix())); err != nil {
			t.Fatalf("inventory lost %s: %v", name, err)
		}
	}
}

// cacheFiles lists the archives of a cache directory, cache.json aside.
func cacheFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if e.Name() != "cache.json" && !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}
