package paperboy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testServer(t *testing.T) (*Server, *time.Time) {
	t.Helper()
	cfg := &Config{Dir: t.TempDir(), Profiles: map[string]Profile{"small": {Width: 40, Height: 20, Gray: true}},
		Devices: []Device{{ID: "test", MAC: "aa:bb:cc:dd:ee:01", Profile: "small", Refresh: Duration(10 * time.Minute)}}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.Local)
	s.now = func() time.Time { return now }
	return s, &now
}

func call(t *testing.T, s *Server, path string, headers map[string]string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s: %v (%s)", path, err, rec.Body)
	}
	return out
}

func TestDeviceFlow(t *testing.T) {
	s, now := testServer(t)
	if rec := register(t, s, `{"mac":"11:22:33:44:55:66","hardware":"sticky"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("unknown device: %d %s", rec.Code, rec.Body)
	}
	if rec := register(t, s, `not json`); rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed registration: %d", rec.Code)
	}
	rec := register(t, s, `{"mac":"aa:bb:cc:dd:ee:01","hardware":"sticky","firmware":"0.4.0"}`)
	var reg struct{ Key, Device string }
	if err := json.Unmarshal(rec.Body.Bytes(), &reg); rec.Code != http.StatusOK || err != nil || reg.Key == "" || reg.Device != "test" {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	// Registering again keeps the key, so a reflashed device keeps its state.
	again := register(t, s, `{"mac":"AA:BB:CC:DD:EE:01","hardware":"sticky","firmware":"0.4.1"}`)
	if !strings.Contains(again.Body.String(), reg.Key) {
		t.Fatalf("key must stay: %s", again.Body)
	}

	if rec := syncRaw(t, s, "wrong", `{"reason":"timer"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong key: %d", rec.Code)
	}
	if rec := syncRaw(t, s, "", `{"reason":"timer"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing key: %d", rec.Code)
	}
	for _, body := range []string{
		`{"reason":"sleepy"}`,
		`{"reason":"button","events":[{"type":"button","button":"up"}]}`,
		`{"reason":"button","events":[{"type":"swipe"}]}`,
		`{"reason":"timer","battery":{"percent":50}}`,
	} {
		if rec := syncRaw(t, s, reg.Key, body); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d", body, rec.Code)
		}
	}

	// Without a plan the device gets a notice page.
	rec = syncRaw(t, s, reg.Key, `{"firmware":"0.4.1","hardware":"sticky","reason":"powercycle",
		"battery":{"volts":3.912,"percent":61,"mah":449,"ma":-68},"rssi":-63}`)
	var out struct {
		Image    struct{ Path, Hash string }
		RefreshS int `json:"refresh_s"`
		ListenS  int `json:"listen_s"`
		Time     int64
		Firmware any
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); rec.Code != http.StatusOK || err != nil {
		t.Fatalf("sync: %d %s", rec.Code, rec.Body)
	}
	if !strings.HasPrefix(out.Image.Path, "/hints/") || !strings.Contains(out.Image.Path, out.Image.Hash) {
		t.Fatalf("expected a notice page: %+v", out)
	}
	if out.RefreshS != int(hintRefresh.Seconds()) || out.ListenS != 0 || out.Time != now.Unix() || out.Firmware != nil {
		t.Fatalf("sync response: %s", rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"firmware":null`) {
		t.Fatalf("firmware must be null: %s", rec.Body)
	}
	img := fetchImage(t, s, out.Image.Path)
	if img.Code != http.StatusOK || img.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("notice image: %d %v", img.Code, img.Header())
	}

	info := deviceInfoOf(t, s)
	if info["id"] != "test" || info["display_type"] != "small" || info["registered"] != true || info["protocol"] != "paperboy" {
		t.Fatalf("devices API: %v", info)
	}
	if info["battery_voltage"] != "3.91" || info["battery_percent"] != float64(61) || info["battery_mah"] != float64(449) ||
		info["rssi"] != "-63" || info["firmware"] != "0.4.1" || info["hardware"] != "sticky" || info["wake_reason"] != "powercycle" {
		t.Fatalf("reported values: %v", info)
	}
	if _, ok := info["api_key"]; ok {
		t.Fatal("devices API must not expose keys")
	}
	profiles := call(t, s, "/api/profiles", nil)["profiles"].([]any)
	p := profiles[0].(map[string]any)
	if p["name"] != "small" || p["min_refresh"] != "1m0s" {
		t.Fatalf("profiles API: %v", p)
	}
	// The TRMNL endpoints are gone.
	for _, path := range []string{"/api/setup", "/api/display"} {
		if rec := htmlRequest(t, s, path); rec.Code != http.StatusNotFound {
			t.Fatalf("%s: %d", path, rec.Code)
		}
	}
}

func TestDeviceStateMigratesToDeviceID(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(stateDir, "devices.json")
	old := `{"AA:BB:CC:DD:EE:01": {"api_key": "existing-key-1234", "friendly_id": "DDEE01"}}`
	if err := os.WriteFile(statePath, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{Dir: dir, Profiles: map[string]Profile{"small": {Width: 40, Height: 20}},
		Devices: []Device{{ID: "wohnzimmer", MAC: "aa:bb:cc:dd:ee:01", Profile: "small"}}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if st := s.state["wohnzimmer"]; st == nil || st.APIKey != "existing-key-1234" {
		t.Fatalf("state not migrated: %v", s.state)
	}
	raw, err := os.ReadFile(statePath)
	if err != nil || !strings.Contains(string(raw), `"wohnzimmer"`) || strings.Contains(string(raw), `"AA:BB:`) {
		t.Fatalf("state file not rewritten: %s (%v)", raw, err)
	}
	// Registration hands out the key the device state already has.
	if rec := register(t, s, `{"mac":"AA:BB:CC:DD:EE:01"}`); !strings.Contains(rec.Body.String(), "existing-key-1234") {
		t.Fatalf("register after migration: %s", rec.Body)
	}
	if rec := syncRaw(t, s, "existing-key-1234", `{"reason":"timer"}`); rec.Code != http.StatusOK {
		t.Fatalf("sync after migration: %d %s", rec.Code, rec.Body)
	}
}

func TestNewCreatesOnlyStateDirectory(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{Dir: dir, Profiles: map[string]Profile{"small": {Width: 40, Height: 20}},
		Devices: []Device{{ID: "test", MAC: "aa:bb:cc:dd:ee:01", Profile: "small"}}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if s.config().Pages != filepath.Join(dir, "pages") || s.config().Plans != filepath.Join(dir, "plans") ||
		s.config().PluginsDir != filepath.Join(dir, "plugins") {
		t.Fatalf("derived directories: %q %q %q", s.config().Pages, s.config().Plans, s.config().PluginsDir)
	}
	if fi, err := os.Stat(filepath.Join(dir, "state")); err != nil || !fi.IsDir() {
		t.Fatalf("state directory: %v", err)
	}
	for _, name := range []string{"pages", "plans", "plugins"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s must stay operator-owned (err %v)", name, err)
		}
	}
}

func TestNewRejectsMissingInstanceDirectory(t *testing.T) {
	if _, err := New(&Config{}); err == nil || !strings.Contains(err.Error(), "instance directory") {
		t.Fatalf("missing instance directory: %v", err)
	}
}
