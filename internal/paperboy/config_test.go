package paperboy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const validConfig = `
language: en
timezone: Europe/Berlin
defaults:
  refresh: 10m
  page_timeout: 3m
  battery: { warn_volts: 3.45, critical_volts: 3.3 }
firmware:
  update: auto
  min_battery_volts: 3.7
display_types:
  e1001:
    kind: image
    size: [800, 480]
    gray_levels: 4
    ppi: 125
    min_refresh: 2m
  sticky:
    size: [800, 480]
    ppi: 236
    gray_levels: 1
devices:
  wohnzimmer:
    display_type: e1001
    protocol: paperboy
    mac: "AA:BB:CC:DD:EE:01"
    plan: standard
    battery: { warn_volts: 3.5 }
    firmware: hold
  flur:
    display_type: sticky
    mac: "aa:bb:cc:dd:ee:02"
plugins:
  weather:
    version: 2.1.0
    timeout: 20s
    env: { OPEN_METEO_KEY: secret }
    config: { lat: 52.52, lon: 13.4, place: "${TEST_PLACE}", price: "5 $$", list: ["${TEST_PLACE}"] }
`

// loadConfig writes text as paperboy.yaml into a fresh instance directory and
// loads it.
func loadConfig(t *testing.T, text string) (*Config, string, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "paperboy.yaml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(dir)
	return cfg, dir, err
}

func TestLoadConfig(t *testing.T) {
	t.Setenv("TEST_PLACE", "Berlin")
	cfg, dir, err := loadConfig(t, validConfig)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Language != "en" {
		t.Fatalf("language: %q", cfg.Language)
	}
	if cfg.Timezone == nil || cfg.Timezone.String() != "Europe/Berlin" {
		t.Fatalf("timezone: %v", cfg.Timezone)
	}
	if cfg.Dir != dir || cfg.Pages != filepath.Join(dir, "pages") || cfg.Plans != filepath.Join(dir, "plans") {
		t.Fatalf("instance layout: %q %q %q", cfg.Dir, cfg.Pages, cfg.Plans)
	}
	if cfg.PluginsDir != filepath.Join(dir, "plugins") {
		t.Fatalf("plugins directory: %q", cfg.PluginsDir)
	}
	if len(cfg.Plugins) != 1 {
		t.Fatalf("plugins: %+v", cfg.Plugins)
	}
	if p := cfg.Plugins[0]; p.Name != "weather" || p.Version != "v2.1.0" || p.Timeout != Duration(20*time.Second) ||
		p.Env["OPEN_METEO_KEY"] != "secret" || p.Config["lat"] != 52.52 || p.Config["place"] != "Berlin" ||
		p.Config["price"] != "5 $" || p.Config["list"].([]any)[0] != "Berlin" {
		t.Fatalf("plugin: %+v", p)
	}
	spec := cfg.pluginSpecs()["weather"]
	if spec.Env["PAPERBOY_LANGUAGE"] != "en" || spec.Env["PAPERBOY_TIMEZONE"] != "Europe/Berlin" || spec.Dir != cfg.PluginsDir {
		t.Fatalf("plugin spec: %+v", spec)
	}
	if want := filepath.Join(dir, "state", "plugins", "weather"); spec.StateDir != want {
		t.Fatalf("state directory: %q, want %q", spec.StateDir, want)
	}
	if cfg.Refresh != Duration(10*time.Minute) || cfg.PageTimeout != Duration(3*time.Minute) {
		t.Fatalf("defaults: %v %v", cfg.Refresh, cfg.PageTimeout)
	}
	if p := cfg.Profiles["e1001"]; p.Width != 800 || p.Height != 480 || !p.Gray || p.PPI != 125 || p.MinRefresh != Duration(2*time.Minute) {
		t.Fatalf("e1001: %+v", p)
	}
	if p := cfg.Profiles["sticky"]; p.Gray || p.MinRefresh != 0 || p.Power != powerAuto {
		t.Fatalf("sticky: %+v", p)
	}
	devFlur, devWohnzimmer := &cfg.Devices[0], &cfg.Devices[1]
	if cfg.minRefresh(devWohnzimmer, "") != 2*time.Minute || cfg.minRefresh(devFlur, "") != time.Minute {
		t.Fatal("min refresh floor")
	}
	if len(cfg.Devices) != 2 || cfg.Devices[0].ID != "flur" || cfg.Devices[1].ID != "wohnzimmer" {
		t.Fatalf("devices: %+v", cfg.Devices)
	}
	flur, living := cfg.Devices[0], cfg.Devices[1]
	if flur.Type != TypePaperboy || flur.MAC != "AA:BB:CC:DD:EE:02" || flur.Refresh != Duration(10*time.Minute) || flur.Plan != "" {
		t.Fatalf("flur: %+v", flur)
	}
	if living.Type != TypePaperboy || living.MAC != "AA:BB:CC:DD:EE:01" || living.Plan != "standard" || !living.Hold || flur.Hold {
		t.Fatalf("wohnzimmer: %+v", living)
	}
	if cfg.Firmware.Update != firmwareAuto || cfg.Firmware.MinBatteryVolts != 3.7 || cfg.FirmwareDir != filepath.Join(dir, "firmware") {
		t.Fatalf("firmware: %+v %q", cfg.Firmware, cfg.FirmwareDir)
	}
	b := cfg.battery(&living)
	if b == nil || b.WarnVolts != 3.5 || b.CriticalVolts != 3.3 || b.HysteresisVolts != 0.1 {
		t.Fatalf("device battery override: %+v", b)
	}
	if cfg.battery(&flur) == nil || cfg.battery(&flur).WarnVolts != 3.45 {
		t.Fatalf("flur must inherit the default battery: %+v", cfg.battery(&flur))
	}
}

func TestLoadConfigErrors(t *testing.T) {
	t.Setenv("TEST_PLACE", "Berlin")
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"unknown key", strings.Replace(validConfig, "  refresh: 10m", "  refrsh: 10m", 1), "field refrsh not found"},
		{"server section", "server:\n  upload_token: abc\n" + validConfig, "no data API"},
		{"providers section", "providers: {}\n" + validConfig, "replaced by plugins"},
		{"opendisplay", strings.Replace(validConfig, "protocol: paperboy", "protocol: opendisplay", 1), "not supported yet"},
		{"http protocol", strings.Replace(validConfig, "protocol: paperboy", "protocol: text-http", 1), "ADR 0044"},
		{"text kind with gray levels", strings.Replace(validConfig, "kind: image", "kind: text", 1), "do not apply to kind text"},
		{"unknown kind", strings.Replace(validConfig, "kind: image", "kind: led", 1), "unknown kind"},
		{"unknown display type", strings.Replace(validConfig, "display_type: sticky", "display_type: fehlt", 1), "unknown display_type"},
		{"duplicate mac", strings.Replace(validConfig, `"aa:bb:cc:dd:ee:02"`, `"AA:BB:CC:DD:EE:01"`, 1), "duplicate device"},
		{"bad size", strings.Replace(validConfig, "size: [800, 480]", "size: [0, 480]", 1), "invalid size"},
		{"width_mm", strings.Replace(validConfig, "ppi: 125", "width_mm: 163", 1), "width_mm was replaced by ppi"},
		{"missing ppi", strings.Replace(validConfig, "    ppi: 236\n", "", 1), "ppi is required"},
		{"bad ppi", strings.Replace(validConfig, "ppi: 236", "ppi: 5000", 1), "ppi is required"},
		{"text kind with ppi", strings.Replace(validConfig, "    kind: image\n    size: [800, 480]\n    gray_levels: 4\n", "    kind: text\n    size: [20, 4]\n", 1), "do not apply to kind text"},
		{"bad plan", strings.Replace(validConfig, "plan: standard", `plan: "Standard"`, 1), "only a-z"},
		{"bad language", strings.Replace(validConfig, "language: en", "language: fr", 1), "expected de or en"},
		{"bad timezone", strings.Replace(validConfig, "timezone: Europe/Berlin", "timezone: Mars/Olympus", 1), "timezone"},
		{"address", strings.Replace(validConfig, "mac: \"AA:BB:CC:DD:EE:01\"", "address: 10.0.0.1", 1), "opendisplay"},
		{"plugin command", strings.Replace(validConfig, "    version: 2.1.0\n", "    command: [weather]\n", 1), "command is no longer supported"},
		{"plugin version", strings.Replace(validConfig, "version: 2.1.0", "version: latest", 1), "expected a version like v2.1.0"},
		{"firmware version", strings.Replace(validConfig, "min_battery_volts: 3.7", "min_battery_volts: 3.7\n  version: main", 1), "expected a version like 1.0.0"},
		{"plugin name", strings.Replace(validConfig, "  weather:\n", "  Weather:\n", 1), "name must use"},
		{"plugin reserved", strings.Replace(validConfig, "  weather:\n", "  paperboy:\n", 1), "reserved"},
		{"plugin variable", strings.Replace(validConfig, "${TEST_PLACE}", "${TEST_MISSING}", 1), "TEST_MISSING is not set"},
		{"gray levels", strings.Replace(validConfig, "gray_levels: 4", "gray_levels: 2", 1), ""},
		{"directories", "directories:\n  pages: pages\n" + validConfig, "field directories not found"},
		{"trmnl", strings.Replace(validConfig, "protocol: paperboy", "protocol: trmnl", 1), "protocol trmnl was removed"},
		{"firmware update", strings.Replace(validConfig, "update: auto", "update: always", 1), "expected auto or off"},
		{"firmware volts", strings.Replace(validConfig, "min_battery_volts: 3.7", "min_battery_volts: 7", 1), "min_battery_volts"},
		{"firmware hold", strings.Replace(validConfig, "firmware: hold", "firmware: pin", 1), "only hold"},
		{"empty", "", "empty configuration"},
		{"two documents", validConfig + "\n---\ndefaults: {}\n", "only one YAML document"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := loadConfig(t, tt.yaml)
			if err == nil {
				t.Fatal("expected an error")
			}
			if tt.want != "" && !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

func TestLoadConfigRejectsJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "paperboy.json")
	if err := os.WriteFile(path, []byte(`{"upload_token":"0123456789abcdef"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(dir); err == nil || !strings.Contains(err.Error(), "paperboy.yaml") {
		t.Fatalf("JSON config: %v", err)
	}
}

func TestLoadConfigRejectsMissingConfig(t *testing.T) {
	if _, err := LoadConfig(t.TempDir()); err == nil || !strings.Contains(err.Error(), "paperboy.yaml") {
		t.Fatalf("missing config: %v", err)
	}
}

func TestLoadConfigRejectsFilePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "paperboy.yaml")
	if err := os.WriteFile(path, []byte(validConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "instance directory") {
		t.Fatalf("file argument: %v", err)
	}
}

func TestLoadConfigDefaultsLanguageAndTimezone(t *testing.T) {
	t.Setenv("TEST_PLACE", "Berlin")
	text := strings.Replace(validConfig, "language: en\n", "", 1)
	text = strings.Replace(text, "timezone: Europe/Berlin\n", "", 1)
	cfg, _, err := loadConfig(t, text)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Timezone != time.Local {
		t.Fatalf("default timezone: %v", cfg.Timezone)
	}
	if cfg.Language != "de" && cfg.Language != "en" {
		t.Fatalf("default language: %q", cfg.Language)
	}
	cfg, _, err = loadConfig(t, strings.Replace(validConfig, "firmware:\n  update: auto\n  min_battery_volts: 3.7\n", "", 1))
	if err != nil || cfg.Firmware.Update != firmwareOff || cfg.Firmware.MinBatteryVolts != defaultFirmwareVolts {
		t.Fatalf("firmware defaults: %+v %v", cfg, err)
	}
}
