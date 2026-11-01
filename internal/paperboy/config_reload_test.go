package paperboy

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const reloadConfig = `
display_types:
  e1001:
    kind: image
    size: [800, 480]
    gray_levels: 4
    width_mm: 163
devices:
  flur:
    display_type: e1001
    protocol: paperboy
    mac: "AA:BB:CC:DD:EE:01"
server:
  upload_token: ` + testToken + `
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
	writePageFile(t, filepath.Join(dir, "pages", "seite.e1001.yaml"), "rows: [1]\nblocks:\n  - text: \"eins\"\n")
	writeConfig(t, dir, reloadConfig)
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

	// The device disappears from the configuration; without a restart it must
	// no longer register.
	noDevices := `display_types:
  e1001:
    kind: image
    size: [800, 480]
    gray_levels: 4
server:
  upload_token: ` + testToken + `
`
	writeConfig(t, dir, noDevices)
	if rec := register(t, s, `{"mac":"AA:BB:CC:DD:EE:01","hardware":"sticky"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("removed device still registers: %d %s", rec.Code, rec.Body)
	}
	// The display type stays; only the device list changed.
	if resp := call(t, s, "/api/profiles", nil); len(resp["profiles"].([]any)) != 1 {
		t.Fatalf("profiles after removal: %#v", resp)
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
	writePageFile(t, filepath.Join(dir, "pages", "seite.e1001.yaml"), "rows: [1]\nblocks:\n  - text: \"eins\"\n")
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
	if resp := call(t, s, "/api/profiles", nil); len(resp["profiles"].([]any)) != 1 {
		t.Fatalf("configuration not applied after a reload: %#v", resp)
	}
	if rec := register(t, s, `{"mac":"AA:BB:CC:DD:EE:01","hardware":"sticky"}`); rec.Code != http.StatusOK {
		t.Fatalf("device cannot register after reload: %d %s", rec.Code, rec.Body)
	}
}

// TestConfigReloadStartsProviders verifies that the background loop starts a
// provider added to paperboy.yaml without a restart (ADR 0036, 0047).
func TestConfigReloadStartsProviders(t *testing.T) {
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	providerYAML := `
display_types:
  e1001:
    kind: image
    size: [800, 480]
    gray_levels: 4
server:
  upload_token: ` + testToken + `
providers:
  weather:
    command: [` + exe + `, -test.run=TestProviderHelperProcess]
    path: weather
    interval: 1h
    timeout: 30s
    env: { PAPERBOY_TEST_PROVIDER: "1" }
`
	writeConfig(t, dir, providerYAML)
	// The providers directory is derived from the instance dir; the runner
	// refuses to start without it.
	if err := os.MkdirAll(filepath.Join(dir, providersDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	s := configServer(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.StartProviders(ctx)

	waitFor(t, func() bool { _, ok := s.doc.Get("weather"); return ok }, "weather provider to write its branch")

	writeConfig(t, dir, providerYAML+`
  wind:
    command: [`+exe+`, -test.run=TestProviderHelperProcess]
    path: wind
    interval: 1h
    timeout: 30s
    env: { PAPERBOY_TEST_PROVIDER: "1" }
`)
	waitFor(t, func() bool { _, ok := s.doc.Get("wind"); return ok }, "added provider to start without a restart")
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
