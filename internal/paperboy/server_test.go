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

const testToken = "0123456789abcdef-test"

func testServer(t *testing.T) (*Server, *time.Time) {
	t.Helper()
	cfg := &Config{UploadToken: testToken, Profiles: map[string]Profile{"small": {Width: 40, Height: 20, Gray: true}},
		Devices: []Device{{ID: "test", MAC: "aa:bb:cc:dd:ee:01", Profile: "small", Refresh: Duration(10 * time.Minute)}}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg, t.TempDir())
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
	s, _ := testServer(t)
	if out := call(t, s, "/api/setup", map[string]string{"ID": "11:22:33:44:55:66"}); out["status"].(float64) != 404 {
		t.Fatalf("unknown device: %v", out)
	}
	setup := call(t, s, "/api/setup", map[string]string{"ID": "AA:BB:CC:DD:EE:01"})
	key, _ := setup["api_key"].(string)
	if setup["status"].(float64) != 200 || key == "" {
		t.Fatalf("setup: %v", setup)
	}
	if rec := fetchImage(t, s, setup["image_url"].(string)); rec.Code != http.StatusOK {
		t.Fatalf("setup image: %d", rec.Code)
	}
	dev := map[string]string{"ID": "AA:BB:CC:DD:EE:01", "Access-Token": key, "Update-Source": "timer"}

	bad := call(t, s, "/api/display", map[string]string{"ID": "AA:BB:CC:DD:EE:01", "Access-Token": "wrong"})
	if bad["status"].(float64) != 202 || bad["reset_firmware"] == true {
		t.Fatalf("wrong key must never reset: %v", bad)
	}
	// Without a plan the device gets a notice page instead of uploaded images.
	out := call(t, s, "/api/display", dev)
	if name := pagePath(out); !strings.HasPrefix(name, "hints/") {
		t.Fatalf("expected a notice page: %v", out)
	}
	if out["refresh_rate"].(float64) != hintRefresh.Seconds() || out["special_function"] != "restart_playlist" {
		t.Fatalf("notice response: %v", out)
	}
	rec := fetchImage(t, s, out["image_url"].(string))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("notice image: %d %v", rec.Code, rec.Header())
	}

	devices := call(t, s, "/api/devices", nil)["devices"].([]any)
	info := devices[0].(map[string]any)
	if info["id"] != "test" || info["display_type"] != "small" || info["registered"] != true {
		t.Fatalf("devices API: %v", info)
	}
	if _, ok := info["api_key"]; ok {
		t.Fatal("devices API must not expose keys")
	}
	profiles := call(t, s, "/api/profiles", nil)["profiles"].([]any)
	p := profiles[0].(map[string]any)
	if p["name"] != "small" || p["min_refresh"] != "1m0s" {
		t.Fatalf("profiles API: %v", p)
	}
}

func TestDeviceStateMigratesToDeviceID(t *testing.T) {
	dir := t.TempDir()
	old := `{"AA:BB:CC:DD:EE:01": {"api_key": "existing-key-1234", "friendly_id": "DDEE01"}}`
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{UploadToken: testToken, Profiles: map[string]Profile{"small": {Width: 40, Height: 20}},
		Devices: []Device{{ID: "wohnzimmer", MAC: "aa:bb:cc:dd:ee:01", Profile: "small"}}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	if st := s.state["wohnzimmer"]; st == nil || st.APIKey != "existing-key-1234" {
		t.Fatalf("state not migrated: %v", s.state)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil || !strings.Contains(string(raw), `"wohnzimmer"`) || strings.Contains(string(raw), `"AA:BB:`) {
		t.Fatalf("state file not rewritten: %s (%v)", raw, err)
	}
	// The device keeps working with the key it already has.
	out := call(t, s, "/api/display", map[string]string{"ID": "AA:BB:CC:DD:EE:01", "Access-Token": "existing-key-1234"})
	if out["filename"] == nil {
		t.Fatalf("display after migration: %v", out)
	}
}
