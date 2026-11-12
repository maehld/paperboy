package paperboy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/internal/download"
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
plugins:
  weather:
    version: 2.1.0
    timeout: 20s
    env: { OPEN_METEO_KEY: secret }
    config: { lat: 52.52, lon: 13.4, place: "${TEST_PLACE}", price: "5 $$", list: ["${TEST_PLACE}"] }
`

// The display type files of validConfig (ADR 0071).
const displayE1001 = `
kind: image
size: [800, 480]
gray_levels: 4
ppi: 125
min_refresh: 2m
`

const displaySticky = `
size: [800, 480]
ppi: 236
gray_levels: 1
`

var validDisplays = map[string]string{"e1001": displayE1001, "sticky": displaySticky}

// replaceDisplay returns validDisplays with one file edited, for the error cases.
func replaceDisplay(name, old, new string) map[string]string {
	out := make(map[string]string, len(validDisplays))
	for k, v := range validDisplays {
		out[k] = v
	}
	out[name] = strings.Replace(out[name], old, new, 1)
	return out
}

// The device files of validConfig (ADR 0070).
const deviceWohnzimmer = `
display_type: e1001
protocol: paperboy
mac: "AA:BB:CC:DD:EE:01"
plan: standard
battery: { warn_volts: 3.5 }
firmware: hold
`

const deviceFlur = `
display_type: sticky
mac: "AA:BB:CC:DD:EE:02"
`

var validDevices = map[string]string{"wohnzimmer": deviceWohnzimmer, "flur": deviceFlur}

// replaceDevice returns validDevices with one file edited, for the error cases.
func replaceDevice(id, old, new string) map[string]string {
	out := make(map[string]string, len(validDevices))
	for k, v := range validDevices {
		out[k] = v
	}
	out[id] = strings.Replace(out[id], old, new, 1)
	return out
}

// loadConfig writes text as paperboy.yaml into a fresh instance directory and
// loads it.
func loadConfig(t *testing.T, text string) (*Config, string, error) {
	t.Helper()
	return loadInstance(t, text, validDisplays, nil)
}

// loadConfigWith writes paperboy.yaml and the given device files into a fresh
// instance directory and loads it (ADR 0070).
func loadConfigWith(t *testing.T, text string, devices map[string]string) (*Config, string, error) {
	t.Helper()
	return loadInstance(t, text, validDisplays, devices)
}

// loadInstance writes paperboy.yaml and the given display type and device files
// into a fresh instance directory and loads it (ADR 0070, 0071).
func loadInstance(t *testing.T, text string, displays, devices map[string]string) (*Config, string, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "paperboy.yaml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, content := range displays {
		writeDisplayFile(t, dir, name, content)
	}
	for id, content := range devices {
		writeDeviceFile(t, dir, id, content)
	}
	cfg, err := LoadConfig(dir)
	return cfg, dir, err
}

func TestLoadConfig(t *testing.T) {
	t.Setenv("TEST_PLACE", "Berlin")
	cfg, dir, err := loadConfigWith(t, validConfig, validDevices)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Language != "en" {
		t.Fatalf("language: %q", cfg.Language)
	}
	if cfg.Timezone == nil || cfg.Timezone.String() != "Europe/Berlin" {
		t.Fatalf("timezone: %v", cfg.Timezone)
	}
	if cfg.Dir != dir || cfg.Pages != filepath.Join(dir, "pages") || cfg.Plans != filepath.Join(dir, "plans") ||
		cfg.DisplaysDir != filepath.Join(dir, "displays") || cfg.DevicesDir != filepath.Join(dir, "devices") {
		t.Fatalf("instance layout: %q %q %q %q %q", cfg.Dir, cfg.Pages, cfg.Plans, cfg.DisplaysDir, cfg.DevicesDir)
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

// TestLoadConfigKeepsDeviceWithMissingDisplayType: renaming a display type
// leaves device files that point at the old name. The loader keeps such a
// device, marks it and loads the rest, so the server stays usable while the
// operator repairs the file by hand (ADR 0079).
func TestLoadConfigKeepsDeviceWithMissingDisplayType(t *testing.T) {
	t.Setenv("TEST_PLACE", "Berlin")
	devices := replaceDevice("flur", "display_type: sticky", "display_type: fehlt")
	cfg, _, err := loadInstance(t, validConfig, validDisplays, devices)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Devices) != 2 {
		t.Fatalf("devices: %+v", cfg.Devices)
	}
	flur, living := cfg.Devices[0], cfg.Devices[1]
	if flur.ID != "flur" || !strings.Contains(flur.Problem, `"fehlt"`) || !strings.Contains(flur.Problem, "not configured") {
		t.Fatalf("flur: %+v", flur)
	}
	if living.Problem != "" {
		t.Fatalf("wohnzimmer: %+v", living)
	}
	// The defaults are filled before the display type is looked at, so the
	// device is complete once its file names a configured type.
	if flur.Refresh != Duration(10*time.Minute) || flur.MAC != "AA:BB:CC:DD:EE:02" {
		t.Fatalf("flur defaults: %+v", flur)
	}
	// The strict API check still refuses the same device.
	strict := flur
	strict.Problem = ""
	if err := cfg.validateDevice(&strict); err == nil || !strings.Contains(err.Error(), "unknown display_type") {
		t.Fatalf("API check: %v", err)
	}
}

func TestLoadConfigErrors(t *testing.T) {
	t.Setenv("TEST_PLACE", "Berlin")
	tests := []struct {
		name     string
		yaml     string
		displays map[string]string
		devices  map[string]string
		want     string
	}{
		{"unknown key", strings.Replace(validConfig, "  refresh: 10m", "  refrsh: 10m", 1), nil, nil, "field refrsh not found"},
		{"server section", "server:\n  upload_token: abc\n" + validConfig, nil, nil, "no data API"},
		{"providers section", "providers: {}\n" + validConfig, nil, nil, "replaced by plugins"},
		{"devices section", validConfig + "devices: {}\n", nil, nil, "devices moved to files of their own"},
		{"display_types section", validConfig + "display_types: {}\n", nil, nil, "display types moved to files of their own"},
		{"no display type", validConfig, map[string]string{}, nil, "no display type"},
		{"opendisplay", validConfig, nil, replaceDevice("wohnzimmer", "protocol: paperboy", "protocol: opendisplay"), "not supported yet"},
		{"http protocol", validConfig, nil, replaceDevice("wohnzimmer", "protocol: paperboy", "protocol: text-http"), "ADR 0044"},
		{"trmnl", validConfig, nil, replaceDevice("wohnzimmer", "protocol: paperboy", "protocol: trmnl"), "protocol trmnl was removed"},
		{"address", validConfig, nil, replaceDevice("wohnzimmer", `mac: "AA:BB:CC:DD:EE:01"`, "address: 10.0.0.1"), "opendisplay"},
		{"text kind with gray levels", validConfig, replaceDisplay("e1001", "kind: image\nsize: [800, 480]", "kind: text\nsize: [20, 4]"), nil, "do not apply to kind text"},
		{"unknown kind", validConfig, replaceDisplay("e1001", "kind: image", "kind: led"), nil, "unknown kind"},
		{"duplicate mac", validConfig, nil, replaceDevice("flur", `"AA:BB:CC:DD:EE:02"`, `"AA:BB:CC:DD:EE:01"`), "duplicate device"},
		{"bad size", validConfig, replaceDisplay("e1001", "size: [800, 480]", "size: [0, 480]"), nil, "invalid size"},
		{"width_mm", validConfig, replaceDisplay("e1001", "ppi: 125", "width_mm: 163"), nil, "width_mm was replaced by ppi"},
		{"missing ppi", validConfig, replaceDisplay("sticky", "ppi: 236\n", ""), nil, "ppi is required"},
		{"bad ppi", validConfig, replaceDisplay("sticky", "ppi: 236", "ppi: 5000"), nil, "ppi is required"},
		{"text kind with ppi", validConfig, replaceDisplay("e1001", "kind: image\nsize: [800, 480]", "kind: text\nsize: [20, 4]"), nil, "do not apply to kind text"},
		{"display type name", validConfig, map[string]string{"Gross": displayE1001}, nil, "only a-z"},
		{"bad plan", validConfig, nil, replaceDevice("wohnzimmer", "plan: standard", `plan: "Standard"`), "only a-z"},
		{"bad language", strings.Replace(validConfig, "language: en", "language: fr", 1), nil, nil, "expected de or en"},
		{"bad timezone", strings.Replace(validConfig, "timezone: Europe/Berlin", "timezone: Mars/Olympus", 1), nil, nil, "timezone"},
		{"plugin command", strings.Replace(validConfig, "    version: 2.1.0\n", "    command: [weather]\n", 1), nil, nil, "command is no longer supported"},
		{"plugin version", strings.Replace(validConfig, "version: 2.1.0", "version: latest", 1), nil, nil, "expected a version like v2.1.0 or v2.1.0-rc.1"},
		{"firmware version", strings.Replace(validConfig, "min_battery_volts: 3.7", "min_battery_volts: 3.7\n  version: main", 1), nil, nil, "expected a version like 1.0.0 or 1.0.0-rc.1"},
		{"plugin name", strings.Replace(validConfig, "  weather:\n", "  Weather:\n", 1), nil, nil, "name must use"},
		{"plugin reserved", strings.Replace(validConfig, "  weather:\n", "  paperboy:\n", 1), nil, nil, "reserved"},
		{"plugin variable", strings.Replace(validConfig, "${TEST_PLACE}", "${TEST_MISSING}", 1), nil, nil, "TEST_MISSING is not set"},
		{"gray levels", validConfig, replaceDisplay("e1001", "gray_levels: 4", "gray_levels: 2"), nil, "gray_levels must be 1 or 4"},
		{"directories", "directories:\n  pages: pages\n" + validConfig, nil, nil, "field directories not found"},
		{"firmware update", strings.Replace(validConfig, "update: auto", "update: always", 1), nil, nil, "expected auto or off"},
		{"firmware volts", strings.Replace(validConfig, "min_battery_volts: 3.7", "min_battery_volts: 7", 1), nil, nil, "min_battery_volts"},
		{"firmware hold", validConfig, nil, replaceDevice("wohnzimmer", "firmware: hold", "firmware: pin"), "only hold"},
		{"device unknown key", validConfig, nil, replaceDevice("flur", "display_type: sticky", "display_typ: sticky"), "field display_typ not found"},
		{"device id", validConfig, nil, map[string]string{"Flur": deviceFlur}, "id must use a-z, 0-9 and -"},
		{"empty", "", nil, nil, "empty configuration"},
		{"two documents", validConfig + "\n---\ndefaults: {}\n", nil, nil, "only one YAML document"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			displays := tt.displays
			if displays == nil {
				displays = validDisplays
			}
			devices := tt.devices
			if devices == nil {
				devices = validDevices
			}
			_, _, err := loadInstance(t, tt.yaml, displays, devices)
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

// TestLoadConfigEmptyDownloadSection verifies that the bare key is the opt-in
// (ADR 0060, 0062). YAML reads "download:" as null, which a pointer field
// cannot tell from a missing section, yet the section's presence is what turns
// the release checks on; a source is optional (the official repository).
func TestLoadConfigEmptyDownloadSection(t *testing.T) {
	cfg, _, err := loadConfig(t, "timezone: UTC\ndownload:\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Download == nil {
		t.Fatal("an empty download section must enable the release checks")
	}
	if cfg.Download.Source != download.OfficialSource || cfg.Download.Interval != defaultDownloadInterval {
		t.Fatalf("download: %+v", cfg.Download)
	}
}

// TestLoadConfigDownloadSectionOptional guards the two neighbours of the bare
// key: no section at all stays off, and a section without a source works.
func TestLoadConfigDownloadSectionOptional(t *testing.T) {
	cfg, _, err := loadConfig(t, "timezone: UTC\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Download != nil {
		t.Fatalf("a missing download section must stay off: %+v", cfg.Download)
	}
	cfg, _, err = loadConfig(t, "timezone: UTC\ndownload:\n  interval: 2h\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Download == nil || cfg.Download.Source != download.OfficialSource || cfg.Download.Interval != 2*time.Hour {
		t.Fatalf("download: %+v", cfg.Download)
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
