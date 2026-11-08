package paperboy

import (
	"context"
	"fmt"
	"image"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

const reloadConfig = `
language: de
display_types:
  e1001:
    kind: image
    size: [800, 480]
    gray_levels: 4
    ppi: 125
`

// reloadDevice is the device file of reloadConfig (ADR 0070).
const reloadDevice = `
display_type: e1001
protocol: paperboy
mac: "AA:BB:CC:DD:EE:01"
`

func writeConfig(t *testing.T, dir, content string) {
	t.Helper()
	writePageFile(t, filepath.Join(dir, configFileName), content)
}

// configServer loads paperboy.yaml from dir like the binary does and fixes the
// clock so that replies are deterministic.
func configServer(t *testing.T, dir string) *Server {
	t.Helper()
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC) }
	return s
}

// TestConfigReloadChangesDeviceList verifies that a device added to
// paperboy.yaml registers without a restart and that a removed device stops
// being served (ADR 0047).
func TestConfigReloadChangesDeviceList(t *testing.T) {
	dir := t.TempDir()
	writePageFile(t, filepath.Join(dir, "pages", "seite.e1001.yaml"), textPage("eins"))
	writeConfig(t, dir, reloadConfig)
	writeDeviceFile(t, dir, "flur", reloadDevice)
	s := configServer(t, dir)

	if rec := register(t, s, `{"mac":"AA:BB:CC:DD:EE:01","hardware":"sticky"}`); rec.Code != http.StatusOK {
		t.Fatalf("registered device refused: %d %s", rec.Code, rec.Body)
	}
	resp := call(t, s, "/api/profiles", nil)
	profiles := resp["profiles"].([]any)
	if len(profiles) != 1 {
		t.Fatalf("profiles: %#v", profiles)
	}
	if p := profiles[0].(map[string]any); p["width"] != float64(800) {
		t.Fatalf("profile width not reloaded: %#v", p)
	}

	// A second device file registers without a restart.
	writeDeviceFile(t, dir, "kueche", strings.Replace(reloadDevice, "AA:BB:CC:DD:EE:01", "AA:BB:CC:DD:EE:02", 1))
	if rec := register(t, s, `{"mac":"AA:BB:CC:DD:EE:02","hardware":"sticky"}`); rec.Code != http.StatusOK {
		t.Fatalf("added device does not register: %d %s", rec.Code, rec.Body)
	}

	// The device file disappears from devices/; without a restart it must no
	// longer register.
	if err := os.Remove(filepath.Join(dir, devicesDirName, "flur.yaml")); err != nil {
		t.Fatal(err)
	}
	if rec := register(t, s, `{"mac":"AA:BB:CC:DD:EE:01","hardware":"sticky"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("removed device still registers: %d %s", rec.Code, rec.Body)
	}
	// The display type stays; only the device list changed.
	if resp := call(t, s, "/api/profiles", nil); len(resp["profiles"].([]any)) != 1 {
		t.Fatalf("profiles after removal: %#v", resp)
	}
}

// TestConfigReloadKeepsLastValidOnBrokenDevice verifies that a broken device
// file keeps the last valid configuration active (ADR 0047, 0070).
func TestConfigReloadKeepsLastValidOnBrokenDevice(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, reloadConfig)
	writeDeviceFile(t, dir, "flur", reloadDevice)
	s := configServer(t, dir)

	writeDeviceFile(t, dir, "flur", "display_type: [\n") // truncated, invalid YAML
	if rec := register(t, s, `{"mac":"AA:BB:CC:DD:EE:01","hardware":"sticky"}`); rec.Code != http.StatusOK {
		t.Fatalf("broken device file must keep the last valid configuration: %d %s", rec.Code, rec.Body)
	}
}

// TestConfigReloadKeepsLastValid verifies that a broken paperboy.yaml keeps the
// last valid configuration active, like a broken plan keeps the last plan
// (ADR 0025, 0047).
func TestConfigReloadKeepsLastValid(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, reloadConfig)
	s := configServer(t, dir)

	writeConfig(t, dir, "display_types: [\n") // truncated, invalid YAML
	if resp := call(t, s, "/api/profiles", nil); len(resp["profiles"].([]any)) != 1 {
		t.Fatalf("broken config must keep the last valid one: %#v", resp)
	}

	// Removing the file keeps the current configuration too.
	os.Remove(filepath.Join(dir, configFileName))
	if resp := call(t, s, "/api/profiles", nil); len(resp["profiles"].([]any)) != 1 {
		t.Fatalf("missing config must keep the current configuration: %#v", resp)
	}
}

// TestDegradedStartAppliesConfig verifies the startup path of the binary: with
// no usable paperboy.yaml the server still starts (ADR 0047), serves an empty
// configuration, and applies the file once it becomes valid.
func TestDegradedStartAppliesConfig(t *testing.T) {
	dir := t.TempDir()
	writePageFile(t, filepath.Join(dir, "pages", "seite.e1001.yaml"), textPage("eins"))
	if _, err := LoadConfig(dir); err == nil {
		t.Fatal("unexpected: directory without paperboy.yaml loads")
	}
	s, err := New(DegradedConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	if resp := call(t, s, "/api/profiles", nil); len(resp["profiles"].([]any)) != 0 {
		t.Fatalf("degraded start must serve no profiles: %#v", resp)
	}

	writeConfig(t, dir, reloadConfig)
	writeDeviceFile(t, dir, "flur", reloadDevice)
	if resp := call(t, s, "/api/profiles", nil); len(resp["profiles"].([]any)) != 1 {
		t.Fatalf("configuration not applied after a reload: %#v", resp)
	}
	if rec := register(t, s, `{"mac":"AA:BB:CC:DD:EE:01","hardware":"sticky"}`); rec.Code != http.StatusOK {
		t.Fatalf("device cannot register after reload: %d %s", rec.Code, rec.Body)
	}
}

// TestMain doubles as the plugin of the tests below: with PAPERBOY_TEST_PLUGIN
// set, the copied test binary serves the plugin instead of running tests
// (ADR 0060: a plugin program is named like the plugin, without arguments).
func TestMain(m *testing.M) {
	if os.Getenv("PAPERBOY_TEST_PLUGIN") == "1" {
		plugin.Serve(&testPlugin{})
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// installTestPlugin copies the test binary into the instance's plugins
// directory as the program of the plugin, so that no command is needed.
func installTestPlugin(t *testing.T, dir, name string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(dir, pluginsDirName, name)
	if runtime.GOOS == "windows" {
		program += ".exe"
	}
	if err := os.WriteFile(program, data, 0o755); err != nil {
		t.Fatal(err)
	}
}

// testPlugin fills its widget black when its config says so.
type testPlugin struct{ black bool }

func (p *testPlugin) Describe() plugin.Description {
	return plugin.Description{Title: "Test", Widgets: []plugin.WidgetInfo{{Widget: "area", Kinds: []string{plugin.KindImage}}}}
}

func (p *testPlugin) Configure(cfg plugin.Values, _ *plugin.Request) error {
	if cfg.String("secret", "") != "s3cret" {
		return fmt.Errorf("secret not expanded: %v", cfg["secret"])
	}
	p.black = cfg.Bool("black", false)
	return nil
}

func (p *testPlugin) Render(_ *plugin.Request, _ plugin.WidgetRequest, img *image.Paletted) error {
	if p.black {
		render.FillSolid(img, img.Rect)
	}
	return nil
}

// TestConfigReloadReconfiguresPlugins verifies that a plugin gets its config
// section with ${VAR} expanded, and that a changed section restarts it so the
// next page shows the new configuration (ADR 0047, 0049).
func TestConfigReloadReconfiguresPlugins(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PAPERBOY_TEST_SECRET", "s3cret")
	pluginYAML := func(black bool) string {
		return fmt.Sprintf(timezoneConfig, "UTC") + `
plugins:
  test:
    env: { PAPERBOY_TEST_PLUGIN: "1" }
    config: { black: ` + fmt.Sprint(black) + `, secret: "${PAPERBOY_TEST_SECRET}" }
`
	}
	writePageFile(t, filepath.Join(dir, "pages", "eins.yaml"), "grid: [1, 1]\ncells: [{widget: test/area}]\n")
	writePageFile(t, filepath.Join(dir, "plans", "zimmer.yaml"), "rules: [{name: immer, pages: [eins]}]\n")
	if err := os.MkdirAll(filepath.Join(dir, pluginsDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	installTestPlugin(t, dir, "test")
	writeConfig(t, dir, pluginYAML(true))
	writeDeviceFile(t, dir, "flur", planDeviceFile)
	s := configServer(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	dev := planDevice(t, s)
	if n := blackPixels(t, s, pagePath(displayPage(t, s, dev))); n < 800*480/2 {
		t.Fatalf("plugin with black: true drew %d black pixels", n)
	}
	writeConfig(t, dir, pluginYAML(false))
	s.configurePlugins(s.config())
	if n := blackPixels(t, s, pagePath(displayPage(t, s, dev))); n > 800*480/10 {
		t.Fatalf("reconfigured plugin still draws black: %d pixels", n)
	}
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for " + what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestDegradedStartRetriesUnchangedFile verifies that a degraded start applies
// paperboy.yaml even when its mod time and size did not change: a repaired file
// or a fixed permission must not need a restart or a touch (ADR 0047).
func TestDegradedStartRetriesUnchangedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, configFileName)
	// Same length as the valid file, only the language is unsupported.
	broken := strings.Replace(reloadConfig, "language: de", "language: xx", 1)
	if len(broken) != len(reloadConfig) {
		t.Fatal("test setup: broken and valid file must have the same length")
	}
	writeConfig(t, dir, broken)
	if _, err := LoadConfig(dir); err == nil {
		t.Fatal("unexpected: the broken configuration loads")
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(DegradedConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	if resp := call(t, s, "/api/profiles", nil); len(resp["profiles"].([]any)) != 0 {
		t.Fatalf("degraded start must serve no profiles: %#v", resp)
	}

	// Repair the file, then put the mod time back: only the content changed.
	writeConfig(t, dir, reloadConfig)
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	if after, err := os.Stat(path); err != nil || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("test setup: the file signature changed: %v %v", after, err)
	}
	if resp := call(t, s, "/api/profiles", nil); len(resp["profiles"].([]any)) != 1 {
		t.Fatalf("degraded start did not retry the unchanged file: %#v", resp)
	}
}

// timezoneConfig is one device on a plan, parameterized by the global timezone.
const timezoneConfig = `
timezone: %s
display_types:
  e1001:
    kind: image
    size: [800, 480]
    ppi: 236
`

// planDeviceFile is the device of timezoneConfig (ADR 0070).
const planDeviceFile = `
display_type: e1001
protocol: paperboy
mac: "AA:BB:CC:DD:EE:01"
plan: zimmer
`

// TestConfigReloadMovesPlanRules verifies that the global timezone is part of
// the plan cache key, so changing it re-parses the plans and the time rules move
// without a restart (ADR 0033, 0047).
func TestConfigReloadMovesPlanRules(t *testing.T) {
	dir := t.TempDir()
	writePageFile(t, filepath.Join(dir, "pages", "eins.e1001.yaml"), textPage("eins"))
	writePageFile(t, filepath.Join(dir, "pages", "zwei.e1001.yaml"), textPage("zwei"))
	writePageFile(t, filepath.Join(dir, "plans", "zimmer.yaml"), `
rules:
  - name: frueh
    when: { from: "00:00", to: "09:00" }
    pages: [eins]
  - name: tag
    when: { from: "09:00", to: "23:59" }
    pages: [zwei]
fallback: frueh
`)
	writeConfig(t, dir, fmt.Sprintf(timezoneConfig, "UTC"))
	writeDeviceFile(t, dir, "flur", planDeviceFile)
	s := configServer(t, dir) // 08:00 UTC
	dev := planDevice(t, s)
	if got := pagePath(displayPage(t, s, dev)); !strings.HasPrefix(got, "/pages/e1001/eins/") {
		t.Fatalf("page at 08:00 UTC: %q", got)
	}

	// Central European Summer Time is two hours ahead: 10:00, so the second
	// rule applies. Without the timezone in the cache key the plan would keep
	// the first rule.
	writeConfig(t, dir, fmt.Sprintf(timezoneConfig, "Europe/Berlin"))
	if got := pagePath(displayPage(t, s, dev)); !strings.HasPrefix(got, "/pages/e1001/zwei/") {
		t.Fatalf("page after the timezone change: %q", got)
	}
}
