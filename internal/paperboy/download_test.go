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
}

func newReleaseFeed(t *testing.T, tags ...string) (*releaseFeed, *httptest.Server) {
	f := &releaseFeed{t: t, tags: tags, files: map[string][]byte{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/me/paperboy/releases", func(w http.ResponseWriter, r *http.Request) {
		type asset struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		}
		type release struct {
			TagName string  `json:"tag_name"`
			Assets  []asset `json:"assets"`
		}
		var out []release
		for _, tag := range f.tags {
			rel := release{TagName: tag}
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

// pluginEntry is how the test program appears in the plugin zip.
func pluginEntry() string {
	if runtime.GOOS == "windows" {
		return "plugins/testplugin.exe"
	}
	return "plugins/testplugin"
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
	writeDeviceFile(t, dir, "flur", planDeviceFile)
	return configServer(t, dir)
}

func TestDownloadInstallsPluginsAndFirmware(t *testing.T) {
	feed, srv := newReleaseFeed(t, "v2.1.0")
	feed.add("v2.1.0", download.PluginAsset(), zipOf(t, map[string]string{
		pluginEntry():   "program",
		"plugins/other": "another plugin",
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
	body := htmlRequest(t, s, "/ui").Body.String()
	for _, want := range []string{"Downloads", "testplugin v2.1.0", "e1001 1.0.0"} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard lacks %q: %s", want, body)
		}
	}
}

func TestDownloadHonoursPins(t *testing.T) {
	feed, srv := newReleaseFeed(t, "plugins-v2.4.0", "v2.3.0", "v2.1.0", "v2.0.0")
	feed.add("plugins-v2.4.0", download.PluginAsset(), zipOf(t, map[string]string{pluginEntry(): "program newer"}))
	for _, rel := range []struct{ tag, image string }{{"v2.3.0", "1.1.0"}, {"v2.1.0", "1.0.0"}, {"v2.0.0", "0.9.0"}} {
		feed.add(rel.tag, download.PluginAsset(), zipOf(t, map[string]string{pluginEntry(): "program " + rel.tag}))
		feed.add(rel.tag, download.FirmwareAsset,
			zipOf(t, map[string]string{"firmware/e1001-v" + rel.image + ".bin": "image " + rel.tag}))
	}

	config := downloadConfig(t, srv.URL+"/repos/me/paperboy/releases",
		"    version: v2.0.0\n", `  version: v0.9.0
`)
	s := testDownloadServer(t, config)
	if err := s.downloadOnce(context.Background(), s.config()); err != nil {
		t.Fatal(err)
	}
	cfg := s.config()
	program, version := pluginProgram(cfg.PluginsDir, "testplugin", "v2.0.0")
	if !strings.HasSuffix(program, "testplugin-v2.0.0"+exeSuffix()) || version != "v2.0.0" {
		t.Fatalf("pinned plugin: %q %q", program, version)
	}
	for _, other := range []string{"v2.1.0", "v2.3.0"} {
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
	feed.add("v2.1.0", download.PluginAsset(), zipOf(t, map[string]string{pluginEntry(): "program"}))
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

// TestDownloadUpdatesDownloadedPlugin verifies that a newer release replaces
// the program in use with a new file next to the old one (ADR 0062).
func TestDownloadUpdatesDownloadedPlugin(t *testing.T) {
	feed, srv := newReleaseFeed(t, "v2.1.0")
	feed.add("v2.1.0", download.PluginAsset(), zipOf(t, map[string]string{pluginEntry(): "old program"}))
	feed.add("v2.1.0", download.FirmwareAsset, zipOf(t, map[string]string{"firmware/e1001-v1.0.0.bin": "image"}))
	s := testDownloadServer(t, downloadConfig(t, srv.URL+"/repos/me/paperboy/releases", "", ""))
	if err := s.downloadOnce(context.Background(), s.config()); err != nil {
		t.Fatal(err)
	}

	feed.tags = []string{"v2.3.0", "v2.1.0"}
	feed.add("v2.3.0", download.PluginAsset(), zipOf(t, map[string]string{pluginEntry(): "new program"}))
	feed.add("v2.3.0", download.FirmwareAsset, zipOf(t, map[string]string{"firmware/e1001-v1.1.0.bin": "image"}))
	if err := s.downloadOnce(context.Background(), s.config()); err != nil {
		t.Fatal(err)
	}

	cfg := s.config()
	program, version := pluginProgram(cfg.PluginsDir, "testplugin", "")
	if !strings.HasSuffix(program, "testplugin-v2.3.0"+exeSuffix()) || version != "v2.3.0" {
		t.Fatalf("plugin program: %q %q", program, version)
	}
	// The old version stays for a manual rollback, and the host restarts the
	// plugin on its next use because the resolved spec changed (ADR 0049).
	if _, err := os.Stat(filepath.Join(cfg.PluginsDir, "testplugin-v2.1.0"+exeSuffix())); err != nil {
		t.Fatalf("old plugin version missing: %v", err)
	}
	spec := cfg.pluginSpecs()["testplugin"]
	if spec.Version != "v2.3.0" || !strings.HasSuffix(spec.Command[0], "testplugin-v2.3.0"+exeSuffix()) {
		t.Fatalf("plugin spec: %+v", spec)
	}

	// Deleting the newer file rolls back to the older one; the same run does
	// not reinstall what the operator removed (ADR 0062).
	if err := os.Remove(filepath.Join(cfg.PluginsDir, "testplugin-v2.3.0"+exeSuffix())); err != nil {
		t.Fatal(err)
	}
	if err := s.downloadOnce(context.Background(), s.config()); err != nil {
		t.Fatal(err)
	}
	program, version = pluginProgram(cfg.PluginsDir, "testplugin", "")
	if !strings.HasSuffix(program, "testplugin-v2.1.0"+exeSuffix()) || version != "v2.1.0" {
		t.Fatalf("deleted version must fall back: %q %q", program, version)
	}
	if _, err := os.Stat(filepath.Join(cfg.PluginsDir, "testplugin-v2.3.0"+exeSuffix())); !os.IsNotExist(err) {
		t.Fatal("the deleted version must not be installed again in the same run")
	}
}

// TestDownloadInterval verifies the configurable cadence and that a reload with
// a shorter interval applies without a restart (ADR 0062).
func TestDownloadInterval(t *testing.T) {
	feed, srv := newReleaseFeed(t, "v2.1.0")
	feed.add("v2.1.0", download.PluginAsset(), zipOf(t, map[string]string{pluginEntry(): "old program"}))
	feed.add("v2.1.0", download.FirmwareAsset, zipOf(t, map[string]string{"firmware/e1001-v1.0.0.bin": "image"}))
	source := srv.URL + "/repos/me/paperboy/releases"
	config := downloadConfigInterval(t, source, "", "", "  interval: 6h\n")
	s := testDownloadServer(t, config)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	// The first check is due right away.
	s.checkDownloads(context.Background())

	feed.tags = []string{"v2.3.0", "v2.1.0"}
	feed.add("v2.3.0", download.PluginAsset(), zipOf(t, map[string]string{pluginEntry(): "new program"}))
	feed.add("v2.3.0", download.FirmwareAsset, zipOf(t, map[string]string{"firmware/e1001-v1.0.0.bin": "image"}))
	now = now.Add(5 * time.Hour)
	s.checkDownloads(context.Background())
	if _, version := pluginProgram(s.config().PluginsDir, "testplugin", ""); version != "v2.1.0" {
		t.Fatalf("a check before the interval must not run, got %q", version)
	}

	// A reload with a shorter interval makes the next wake-up check at once.
	writeConfig(t, s.config().Dir, downloadConfigInterval(t, source, "", "", "  interval: 1h0m\n"))
	now = now.Add(2 * time.Hour)
	s.checkDownloads(context.Background())
	if _, version := pluginProgram(s.config().PluginsDir, "testplugin", ""); version != "v2.3.0" {
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

// TestDownloadCheckEndpoint verifies that the dashboard button starts a check
// in the background and returns at once (ADR 0062).
func TestDownloadCheckEndpoint(t *testing.T) {
	feed, srv := newReleaseFeed(t, "v2.1.0")
	feed.add("v2.1.0", download.PluginAsset(), zipOf(t, map[string]string{pluginEntry(): "program"}))
	feed.add("v2.1.0", download.FirmwareAsset, zipOf(t, map[string]string{"firmware/e1001-v1.0.0.bin": "image"}))
	s := testDownloadServer(t, downloadConfig(t, srv.URL+"/repos/me/paperboy/releases", "", ""))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/ui/download", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/ui" {
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
	if _, version := pluginProgram(s.config().PluginsDir, "testplugin", ""); version != "v2.1.0" {
		t.Fatalf("the button must install the plugin, got %q", version)
	}
	body := htmlRequest(t, s, "/ui").Body.String()
	if !strings.Contains(body, "Check now") {
		t.Fatalf("the dashboard lacks the check button: %s", body)
	}
}

// exeSuffix is the platform suffix of a plugin program.
func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
