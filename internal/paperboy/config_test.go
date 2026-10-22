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
server:
  public_url: https://paper.example.com/
  upload_token_env: TEST_UPLOAD_TOKEN
directories:
  pages: pages
  plans: plans
  data: data
  providers: providers
defaults:
  refresh: 10m
  page_timeout: 3m
  battery: { warn_volts: 3.45, critical_volts: 3.3 }
display_types:
  e1001:
    kind: image
    size: [800, 480]
    gray_levels: 4
    width_mm: 163
    min_refresh: 2m
  sticky:
    size: [800, 480]
    gray_levels: 1
devices:
  wohnzimmer:
    display_type: e1001
    protocol: trmnl
    mac: "44:1B:F6:85:AA:1C"
    plan: standard
    battery: { warn_volts: 3.5 }
  flur:
    display_type: sticky
    mac: "44:1b:f6:85:aa:1d"
providers:
  weather:
    command: [weather, -lat, "52.52"]
    path: weather
    interval: 30m
    timeout: 20s
    env: { OPEN_METEO_KEY: secret }
`

// loadConfig writes text as paperboy.yaml next to a fresh temporary directory
// and loads it. The token comes from the environment.
func loadConfig(t *testing.T, text string) (*Config, string, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "paperboy.yaml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	return cfg, dir, err
}

func TestLoadConfig(t *testing.T) {
	t.Setenv("TEST_UPLOAD_TOKEN", "0123456789abcdef")
	t.Setenv("PAPERBOY_UPLOAD_TOKEN", "")
	t.Setenv("DISPLAYD_UPLOAD_TOKEN", "")
	cfg, dir, err := loadConfig(t, validConfig)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UploadToken != "0123456789abcdef" {
		t.Fatalf("token: %q", cfg.UploadToken)
	}
	if cfg.PublicURL != "https://paper.example.com" {
		t.Fatalf("public url: %q", cfg.PublicURL)
	}
	if cfg.Language != "en" {
		t.Fatalf("language: %q", cfg.Language)
	}
	if cfg.Timezone == nil || cfg.Timezone.String() != "Europe/Berlin" {
		t.Fatalf("timezone: %v", cfg.Timezone)
	}
	if cfg.Pages != filepath.Join(dir, "pages") || cfg.Plans != filepath.Join(dir, "plans") || cfg.DataDir != filepath.Join(dir, "data") {
		t.Fatalf("directories: %q %q %q", cfg.Pages, cfg.Plans, cfg.DataDir)
	}
	if cfg.ProvidersDir != filepath.Join(dir, "providers") {
		t.Fatalf("providers directory: %q", cfg.ProvidersDir)
	}
	if len(cfg.Providers) != 1 {
		t.Fatalf("providers: %+v", cfg.Providers)
	}
	if p := cfg.Providers[0]; p.Name != "weather" || p.Path != "weather" ||
		len(p.Command) != 3 || p.Interval != Duration(30*time.Minute) || p.Timeout != Duration(20*time.Second) ||
		p.Env["OPEN_METEO_KEY"] != "secret" {
		t.Fatalf("provider: %+v", p)
	}
	if cfg.Refresh != Duration(10*time.Minute) || cfg.PageTimeout != Duration(3*time.Minute) {
		t.Fatalf("defaults: %v %v", cfg.Refresh, cfg.PageTimeout)
	}
	if p := cfg.Profiles["e1001"]; p.Width != 800 || p.Height != 480 || !p.Gray || p.WidthMM != 163 || p.MinRefresh != Duration(2*time.Minute) {
		t.Fatalf("e1001: %+v", p)
	}
	if p := cfg.Profiles["sticky"]; p.Gray || p.MinRefresh != Duration(time.Minute) {
		t.Fatalf("sticky: %+v", p)
	}
	if cfg.minRefresh("e1001") != 2*time.Minute || cfg.minRefresh("sticky") != time.Minute {
		t.Fatal("min refresh floor")
	}
	if len(cfg.Devices) != 2 || cfg.Devices[0].ID != "flur" || cfg.Devices[1].ID != "wohnzimmer" {
		t.Fatalf("devices: %+v", cfg.Devices)
	}
	flur, living := cfg.Devices[0], cfg.Devices[1]
	if flur.Type != TypeTRMNL || flur.MAC != "44:1B:F6:85:AA:1D" || flur.Refresh != Duration(10*time.Minute) || flur.Plan != "" {
		t.Fatalf("flur: %+v", flur)
	}
	if living.Type != TypeTRMNL || living.MAC != "44:1B:F6:85:AA:1C" || living.Plan != "standard" {
		t.Fatalf("wohnzimmer: %+v", living)
	}
	b := cfg.battery(&living)
	if b == nil || b.WarnVolts != 3.5 || b.CriticalVolts != 3.3 || b.HysteresisVolts != 0.1 {
		t.Fatalf("device battery override: %+v", b)
	}
	if cfg.battery(&flur) == nil || cfg.battery(&flur).WarnVolts != 3.45 {
		t.Fatalf("flur must inherit the default battery: %+v", cfg.battery(&flur))
	}
}

func TestLoadConfigToken(t *testing.T) {
	t.Setenv("PAPERBOY_UPLOAD_TOKEN", "")
	t.Setenv("DISPLAYD_UPLOAD_TOKEN", "")
	// The direct token is used when upload_token_env is empty.
	cfg, _, err := loadConfig(t, strings.Replace(validConfig, "  upload_token_env: TEST_UPLOAD_TOKEN", `  upload_token: "abcdef0123456789"`, 1))
	if err != nil || cfg.UploadToken != "abcdef0123456789" {
		t.Fatalf("direct token: %v %q", err, cfg.UploadToken)
	}
	// The environment still overrides it for existing installations.
	t.Setenv("PAPERBOY_UPLOAD_TOKEN", "fedcba9876543210")
	cfg, _, err = loadConfig(t, strings.Replace(validConfig, "  upload_token_env: TEST_UPLOAD_TOKEN", `  upload_token: "abcdef0123456789"`, 1))
	if err != nil || cfg.UploadToken != "fedcba9876543210" {
		t.Fatalf("environment token: %v %q", err, cfg.UploadToken)
	}
	// An empty upload_token_env is a startup error unless a token is configured.
	t.Setenv("MISSING_TOKEN", "")
	if _, _, err := loadConfig(t, strings.Replace(validConfig, "TEST_UPLOAD_TOKEN", "MISSING_TOKEN", 1)); err == nil || !strings.Contains(err.Error(), "MISSING_TOKEN") {
		t.Fatalf("empty token env: %v", err)
	}
}

func TestLoadConfigErrors(t *testing.T) {
	t.Setenv("TEST_UPLOAD_TOKEN", "0123456789abcdef")
	t.Setenv("PAPERBOY_UPLOAD_TOKEN", "")
	t.Setenv("DISPLAYD_UPLOAD_TOKEN", "")
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"unknown key", strings.Replace(validConfig, "  public_url:", "  public_ur:", 1), "field public_ur not found"},
		{"no token", strings.Replace(validConfig, "upload_token_env: TEST_UPLOAD_TOKEN", "", 1), "upload_token missing"},
		{"opendisplay", strings.Replace(validConfig, "protocol: trmnl", "protocol: opendisplay", 1), "not supported yet"},
		{"http protocol", strings.Replace(validConfig, "protocol: trmnl", "protocol: text-http", 1), "ADR 0027"},
		{"text kind", strings.Replace(validConfig, "kind: image", "kind: text", 1), "ADR 0027"},
		{"unknown display type", strings.Replace(validConfig, "display_type: sticky", "display_type: fehlt", 1), "unknown display_type"},
		{"duplicate mac", strings.Replace(validConfig, `"44:1b:f6:85:aa:1d"`, `"44:1B:F6:85:AA:1C"`, 1), "duplicate device"},
		{"bad size", strings.Replace(validConfig, "size: [800, 480]", "size: [0, 480]", 1), "invalid size"},
		{"bad plan", strings.Replace(validConfig, "plan: standard", `plan: "Standard"`, 1), "only a-z"},
		{"bad language", strings.Replace(validConfig, "language: en", "language: fr", 1), "expected de or en"},
		{"bad timezone", strings.Replace(validConfig, "timezone: Europe/Berlin", "timezone: Mars/Olympus", 1), "timezone"},
		{"address", strings.Replace(validConfig, "mac: \"44:1B:F6:85:AA:1C\"", "address: 10.0.0.1", 1), "opendisplay"},
		{"provider command", strings.Replace(validConfig, "    command: [weather, -lat, \"52.52\"]\n", "", 1), "command missing"},
		{"provider path", strings.Replace(validConfig, "    path: weather\n", "", 1), "path missing"},
		{"provider root path", strings.Replace(validConfig, "    path: weather\n", "    path: /\n", 1), "not the whole document"},
		{"provider bad path", strings.Replace(validConfig, "    path: weather\n", "    path: Weather/Now\n", 1), "segment"},
		{"provider interval", strings.Replace(validConfig, "    interval: 30m\n", "    interval: 1s\n", 1), "at least"},
		{"provider timeout", strings.Replace(validConfig, "    timeout: 20s\n", "    timeout: 60m\n", 1), "must not exceed interval"},
		{"provider name", strings.Replace(validConfig, "  weather:\n", "  Weather:\n", 1), "name must use"},
		{"provider overlap", strings.Replace(validConfig, "    path: weather\n", "    path: weather/current\n", 1) +
			"  weather2:\n    command: [weather]\n    path: weather\n", "one owner"},
		{"gray levels", strings.Replace(validConfig, "gray_levels: 4", "gray_levels: 2", 1), ""},
		{"empty", "", "empty configuration"},
		{"two documents", validConfig + "\n---\nserver: {}\n", "only one YAML document"},
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
	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "YAML") {
		t.Fatalf("JSON config: %v", err)
	}
}

func TestLoadConfigKeepsAbsoluteDirectories(t *testing.T) {
	t.Setenv("TEST_UPLOAD_TOKEN", "0123456789abcdef")
	pages := t.TempDir()
	cfg, _, err := loadConfig(t, strings.Replace(validConfig, "pages: pages", "pages: "+pages, 1))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Pages != pages {
		t.Fatalf("absolute pages: %q", cfg.Pages)
	}
}

func TestLoadConfigDefaultsLanguageAndTimezone(t *testing.T) {
	t.Setenv("TEST_UPLOAD_TOKEN", "0123456789abcdef")
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
}
