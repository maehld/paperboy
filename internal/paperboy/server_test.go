package paperboy

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const testToken = "0123456789abcdef-test"

func testServer(t *testing.T) (*Server, *time.Time) {
	t.Helper()
	cfg := &Config{UploadToken: testToken, Profiles: map[string]Profile{"small": {Width: 40, Height: 20, Gray: true}},
		Devices: []Device{{MAC: "aa:bb:cc:dd:ee:01", Name: "Test", Profile: "small", Refresh: Duration(10 * time.Minute)}}}
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

func upload(t *testing.T, s *Server, page string, level uint8) int {
	img := image.NewGray(image.Rect(0, 0, 40, 20))
	for i := range img.Pix {
		img.Pix[i] = level
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	req := httptest.NewRequest(http.MethodPut, "/api/images/small/"+page, &buf)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec.Code
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

func TestUploadRules(t *testing.T) {
	s, _ := testServer(t)
	req := httptest.NewRequest(http.MethodPut, "/api/images/small/0", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("without token: %d", rec.Code)
	}
	if code := upload(t, s, "1", 0); code != http.StatusConflict {
		t.Fatalf("gap must be rejected: %d", code)
	}
	if code := upload(t, s, "0", 0); code != http.StatusNoContent {
		t.Fatalf("upload: %d", code)
	}
	if code := upload(t, s, "1", 255); code != http.StatusNoContent {
		t.Fatalf("upload page 1: %d", code)
	}
	// Stored images survive a restart.
	s2, err := New(s.cfg, s.dir)
	if err != nil || len(s2.images["small"]) != 2 {
		t.Fatalf("restart: %v, %d pages", err, len(s2.images["small"]))
	}
}

func TestDeviceFlow(t *testing.T) {
	s, now := testServer(t)
	if out := call(t, s, "/api/setup", map[string]string{"ID": "11:22:33:44:55:66"}); out["status"].(float64) != 404 {
		t.Fatalf("unknown device: %v", out)
	}
	setup := call(t, s, "/api/setup", map[string]string{"ID": "AA:BB:CC:DD:EE:01"})
	key, _ := setup["api_key"].(string)
	if setup["status"].(float64) != 200 || key == "" {
		t.Fatalf("setup: %v", setup)
	}
	dev := map[string]string{"ID": "AA:BB:CC:DD:EE:01", "Access-Token": key, "Update-Source": "timer"}

	bad := call(t, s, "/api/display", map[string]string{"ID": "AA:BB:CC:DD:EE:01", "Access-Token": "wrong"})
	if bad["status"].(float64) != 202 || bad["reset_firmware"] == true {
		t.Fatalf("wrong key must never reset: %v", bad)
	}
	if out := call(t, s, "/api/display", dev); out["filename"] != "small-setup" {
		t.Fatalf("expected placeholder without images: %v", out)
	}
	upload(t, s, "0", 255)
	upload(t, s, "1", 0)
	page := func(out map[string]any) string { return out["filename"].(string)[:7] }

	out := call(t, s, "/api/display", dev)
	if page(out) != "small-0" || out["refresh_rate"].(float64) != 600 || out["special_function"] != "restart_playlist" {
		t.Fatalf("timer fetch: %v", out)
	}
	dev["Update-Source"] = "EXT1"
	out = call(t, s, "/api/display", dev)
	if page(out) != "small-1" || out["refresh_rate"].(float64) > 310 {
		t.Fatalf("button press must page and shorten the interval: %v", out)
	}
	if out = call(t, s, "/api/display", dev); page(out) != "small-0" {
		t.Fatalf("overview again after the last page: %v", out)
	}
	call(t, s, "/api/display", dev) // page 1
	dev["special_function"] = "true"
	out = call(t, s, "/api/display", dev)
	if page(out) != "small-0" || out["action"] != "restart_playlist" {
		t.Fatalf("double click must lead to the overview: %v", out)
	}
	delete(dev, "special_function")
	dev["Update-Source"] = "EXT1"
	call(t, s, "/api/display", dev) // page 1
	dev["Update-Source"] = "timer"
	*now = now.Add(6 * time.Minute)
	out = call(t, s, "/api/display", dev)
	if page(out) != "small-0" {
		t.Fatalf("back to overview after timeout: %v", out)
	}
	*now = now.Add(time.Hour)
	out = call(t, s, "/api/display", dev)
	if name := out["filename"].(string); name[len(name)-6:] != "-stale" {
		t.Fatalf("old image must be marked: %v", out)
	}
	req := httptest.NewRequest(http.MethodGet, "/images/"+out["filename"].(string)+".png", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	img, err := png.Decode(rec.Body)
	if err != nil || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("image fetch: %v %v", err, rec.Header())
	}
	if p, ok := img.(*image.Paletted); !ok || len(p.Palette) != 4 {
		t.Fatalf("gray profile must yield 2-bit palette: %T", img)
	}
	black := 0
	for x := 0; x < 40; x++ {
		if color.GrayModel.Convert(img.At(x, 19)).(color.Gray).Y == 0 {
			black++
		}
	}
	if black < 10 {
		t.Fatalf("notice bar missing (%d black, %T)", black, img)
	}
}

func TestFirstContactDoesNotPage(t *testing.T) {
	s, _ := testServer(t)
	upload(t, s, "0", 255)
	upload(t, s, "1", 0)
	key := call(t, s, "/api/setup", map[string]string{"ID": "AA:BB:CC:DD:EE:01"})["api_key"].(string)
	dev := map[string]string{"ID": "AA:BB:CC:DD:EE:01", "Access-Token": key, "Update-Source": "EXT0"}
	if out := call(t, s, "/api/display", dev); out["filename"].(string)[:7] != "small-0" {
		t.Fatalf("first fetch after setup must show the overview: %v", out)
	}
	if out := call(t, s, "/api/display", dev); out["filename"].(string)[:7] != "small-1" {
		t.Fatalf("afterwards the button pages: %v", out)
	}
}

func TestNightRefresh(t *testing.T) {
	d := Device{Refresh: Duration(5 * time.Minute), NightRefresh: Duration(time.Hour), NightStart: 22 * 60, NightEnd: 6 * 60}
	at := func(h int) time.Duration { return d.refresh(time.Date(2026, 1, 1, h, 0, 0, 0, time.Local)) }
	if at(23) != time.Hour || at(3) != time.Hour || at(6) != 5*time.Minute || at(12) != 5*time.Minute {
		t.Fatal("wrong night window across midnight")
	}
}

func TestNormalizeMono(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 2, 1))
	img.Pix = []uint8{40, 200}
	p := normalize(img, Profile{})
	if len(p.Palette) != 2 || p.Pix[0] != 0 || p.Pix[1] != 1 {
		t.Fatalf("wrong threshold: %v", p.Pix)
	}
	if b := encodePNG(p); b[24] != 1 { // IHDR bit depth
		t.Fatalf("expected 1-bit PNG, bit depth %d", b[24])
	}
}
