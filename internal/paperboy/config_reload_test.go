package paperboy

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const reloadConfig = `
language: de
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
devices:
  flur:
    display_type: e1001
    protocol: paperboy
    mac: "AA:BB:CC:DD:EE:01"
    plan: zimmer
server:
  upload_token: ` + testToken + `
`

// TestConfigReloadMovesPlanRules verifies that the global timezone is part of
// the plan cache key, so changing it re-parses the plans and the time rules move
// without a restart (ADR 0033, 0047).
func TestConfigReloadMovesPlanRules(t *testing.T) {
	dir := t.TempDir()
	writePageFile(t, filepath.Join(dir, "pages", "eins.e1001.yaml"), "rows: [1]\nblocks:\n  - text: \"eins\"\n")
	writePageFile(t, filepath.Join(dir, "pages", "zwei.e1001.yaml"), "rows: [1]\nblocks:\n  - text: \"zwei\"\n")
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

// providerRunOf returns the run of a provider, or nil when it is not running.
func providerRunOf(s *Server, name string) *providerRun {
	s.providers.mu.Lock()
	defer s.providers.mu.Unlock()
	return s.providers.runs[name]
}

// TestProviderManagerReconciles verifies that an unchanged configuration keeps a
// running provider, a changed spec restarts it and a removed provider stops
// (ADR 0036, 0047).
func TestProviderManagerReconciles(t *testing.T) {
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	block := func(name, interval string) string {
		return "  " + name + ":\n    command: [" + exe + ", -test.run=TestProviderHelperProcess]\n" +
			"    path: " + name + "\n    interval: " + interval + "\n    timeout: 30s\n" +
			"    env: { PAPERBOY_TEST_PROVIDER: \"1\" }\n"
	}
	head := "\ndisplay_types:\n  e1001:\n    kind: image\n    size: [800, 480]\n" +
		"server:\n  upload_token: " + testToken + "\nproviders:\n"
	writeConfig(t, dir, head+block("weather", "1h")+block("wind", "1h"))
	if err := os.MkdirAll(filepath.Join(dir, providersDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	s := configServer(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.providers.sync(ctx, s.config(), s.doc, s.now)
	weather := providerRunOf(s, "weather")
	if weather == nil || providerRunOf(s, "wind") == nil {
		t.Fatal("providers not started")
	}
	s.providers.sync(ctx, s.config(), s.doc, s.now)
	if providerRunOf(s, "weather") != weather {
		t.Fatal("an unchanged configuration restarted the provider")
	}

	// A removed provider stops, the remaining one keeps running.
	writeConfig(t, dir, head+block("weather", "1h"))
	s.providers.sync(ctx, s.config(), s.doc, s.now)
	if providerRunOf(s, "wind") != nil {
		t.Fatal("a removed provider keeps running")
	}
	if providerRunOf(s, "weather") != weather {
		t.Fatal("removing another provider restarted this one")
	}

	// A changed interval restarts the provider with the new spec.
	writeConfig(t, dir, head+block("weather", "2h"))
	s.providers.sync(ctx, s.config(), s.doc, s.now)
	restarted := providerRunOf(s, "weather")
	if restarted == nil || restarted == weather || restarted.spec.Interval != 2*time.Hour {
		t.Fatalf("changed interval did not restart the provider: %#v", restarted)
	}
}
