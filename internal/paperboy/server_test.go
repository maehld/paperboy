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
	cfg := &Config{Dir: t.TempDir(), Profiles: map[string]Profile{"small": {Width: 40, Height: 20, PPI: 125, Gray: true}},
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
		info["battery_ma"] != float64(-68) ||
		info["rssi"] != "-63" || info["firmware"] != "0.4.1" || info["hardware"] != "sticky" || info["wake_reason"] != "powercycle" {
		t.Fatalf("reported values: %v", info)
	}
	if _, ok := info["api_key"]; ok {
		t.Fatal("devices API must not expose keys")
	}
	profiles := call(t, s, "/api/profiles", nil)["profiles"].([]any)
	p := profiles[0].(map[string]any)
	if _, set := p["min_refresh"]; p["name"] != "small" || set || p["power"] != powerAuto {
		t.Fatalf("profiles API: %v", p)
	}
	// The TRMNL endpoints are gone.
	for _, path := range []string{"/api/setup", "/api/display"} {
		if rec := htmlRequest(t, s, path); rec.Code != http.StatusNotFound {
			t.Fatalf("%s: %d", path, rec.Code)
		}
	}
}

func TestDeviceStateLivesInOwnFile(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state", "devices")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The file is named by the MAC, so a renamed device file keeps its state.
	statePath := filepath.Join(stateDir, "AABBCCDDEE01.json")
	if err := os.WriteFile(statePath, []byte(`{"api_key": "existing-key-1234", "page": 2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{Dir: dir, Profiles: map[string]Profile{"small": {Width: 40, Height: 20, PPI: 125}},
		Devices: []Device{{ID: "wohnzimmer", MAC: "aa:bb:cc:dd:ee:01", Profile: "small"}}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if st := s.state["AABBCCDDEE01"]; st == nil || st.APIKey != "existing-key-1234" || st.Page != 2 {
		t.Fatalf("state not loaded by MAC: %+v", s.state)
	}
	// Registration hands out the key the device state already has.
	if rec := register(t, s, `{"mac":"AA:BB:CC:DD:EE:01","hardware":"sticky"}`); !strings.Contains(rec.Body.String(), "existing-key-1234") {
		t.Fatalf("register: %s", rec.Body)
	}
	if rec := syncRaw(t, s, "existing-key-1234", `{"reason":"timer"}`); rec.Code != http.StatusOK {
		t.Fatalf("sync: %d %s", rec.Code, rec.Body)
	}
	if fi, err := os.Stat(statePath); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("state file: %v %v", fi, err)
	}
}

func TestUnknownDeviceGetsStateFile(t *testing.T) {
	s, _ := testServer(t)
	// An unknown MAC leaves a state file for the device editor and still gets
	// 403, so the firmware keeps the device unconfigured (ADR 0080).
	if rec := register(t, s, `{"mac":"aa:bb:cc:dd:ee:99","hardware":"sticky","firmware":"1.2.0"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("unknown device: %d %s", rec.Code, rec.Body)
	}
	path := deviceStateFile(s.devicesDir, "AABBCCDDEE99")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var st deviceState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	if st.APIKey != "" || st.Hardware != "sticky" || st.Firmware != "1.2.0" || st.FirstSeen.IsZero() || st.LastSeen.IsZero() {
		t.Fatalf("unclaimed state: %+v", st)
	}
	// Claiming writes a device file with the MAC; the next register keeps the
	// state and adds the key. The configuration reloads from disk, so a new
	// server loads the claimed device here.
	writeDeviceFile(t, s.config().Dir, "flur", "display_type: small\nmac: \"aa:bb:cc:dd:ee:99\"\n")
	cfg := *s.config()
	cfg.Devices = append(append([]Device{}, cfg.Devices...), Device{ID: "flur", MAC: "AA:BB:CC:DD:EE:99", Profile: "small"})
	claimed, err := New(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	if st := claimed.state["AABBCCDDEE99"]; st == nil || st.Hardware != "sticky" || st.FirstSeen.IsZero() {
		t.Fatalf("unclaimed state did not survive: %+v", claimed.state)
	}
	rec := register(t, claimed, `{"mac":"aa:bb:cc:dd:ee:99","hardware":"sticky"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"device":"flur"`) {
		t.Fatalf("claim: %d %s", rec.Code, rec.Body)
	}
	if st := claimed.state["AABBCCDDEE99"]; st == nil || st.APIKey == "" {
		t.Fatalf("claim did not issue a key: %+v", claimed.state)
	}
}

func TestLegacyDevicesFileIsIgnored(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(stateDir, "devices.json")
	if err := os.WriteFile(legacy, []byte(`{"wohnzimmer": {"api_key": "old"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{Dir: dir, Profiles: map[string]Profile{"small": {Width: 40, Height: 20, PPI: 125}},
		Devices: []Device{{ID: "wohnzimmer", MAC: "aa:bb:cc:dd:ee:01", Profile: "small"}}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.state) != 0 {
		t.Fatalf("legacy state was read: %+v", s.state)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy file was touched: %v", err)
	}
}

func TestNewCreatesOnlyStateDirectory(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{Dir: dir, Profiles: map[string]Profile{"small": {Width: 40, Height: 20, PPI: 125}},
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
