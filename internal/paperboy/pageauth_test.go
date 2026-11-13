package paperboy

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// keyServer is a server with two display types and one device each, so a test
// can check that a key only reaches its own pages (ADR 0097).
func keyServer(t *testing.T, password string) *Server {
	t.Helper()
	cfg := &Config{Dir: t.TempDir(),
		Profiles: map[string]Profile{
			"small": {Width: 20, Height: 10, PPI: 125},
			"other": {Width: 20, Height: 10, PPI: 125},
		},
		Devices: []Device{
			{ID: "test", MAC: "aa:bb:cc:dd:ee:01", Profile: "small", Refresh: Duration(10 * time.Minute)},
			{ID: "zwei", MAC: "aa:bb:cc:dd:ee:02", Profile: "other", Refresh: Duration(10 * time.Minute)},
		},
		UI: UIConfig{Password: password}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Date(2026, 11, 13, 12, 0, 0, 0, time.Local) }
	return s
}

// registerKey registers a device by MAC and returns its key.
func registerKey(t *testing.T, s *Server, mac string) string {
	t.Helper()
	rec := register(t, s, `{"mac":"`+mac+`","hardware":"sticky"}`)
	var out struct{ Key string }
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK || out.Key == "" {
		t.Fatalf("register %s: %d %s", mac, rec.Code, rec.Body)
	}
	return out.Key
}

// storeTestPage registers a rendered page for a display type.
func storeTestPage(s *Server, profile, name string) *renderedPage {
	img := image.NewPaletted(image.Rect(0, 0, 20, 10), color.Palette{color.Gray{0}, color.Gray{255}})
	for i := range img.Pix {
		img.Pix[i] = uint8(i % 2)
	}
	data := encodePNG(img)
	rp := &renderedPage{data: data, hash: hashBytes(data), pix: img.Pix, rect: img.Rect}
	s.mu.Lock()
	s.rendered[profile+"/"+name] = rp
	s.mu.Unlock()
	return rp
}

func imageRequest(t *testing.T, s *Server, key, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// TestPageDownloadsNeedTheDeviceKey walks what a page and a notice download do
// without, with a wrong and with another device's key (ADR 0097).
func TestPageDownloadsNeedTheDeviceKey(t *testing.T) {
	s := keyServer(t, "")
	key := registerKey(t, s, "aa:bb:cc:dd:ee:01")
	other := registerKey(t, s, "aa:bb:cc:dd:ee:02")
	rp := storeTestPage(s, "small", "eins")
	hint := s.notice("small", placeholderImage())

	for _, c := range []struct {
		name, key, path string
		want            int
	}{
		{"page without a key", "", "/pages/small/eins/" + rp.hash + ".png", http.StatusUnauthorized},
		{"page with a wrong key", "falsch", "/pages/small/eins/" + rp.hash + ".png", http.StatusUnauthorized},
		{"page of the own type", key, "/pages/small/eins/" + rp.hash + ".png", http.StatusOK},
		{"page of another type", other, "/pages/small/eins/" + rp.hash + ".png", http.StatusForbidden},
		{"notice without a key", "", hint.path, http.StatusUnauthorized},
		{"notice of the own type", key, hint.path, http.StatusOK},
		{"notice of another type", other, hint.path, http.StatusForbidden},
	} {
		rec := imageRequest(t, s, c.key, c.path)
		if rec.Code != c.want {
			t.Errorf("%s: %d, want %d (%s)", c.name, rec.Code, c.want, rec.Body)
		}
		if rec.Code == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") != "" {
			t.Errorf("%s: a page download must not ask for the UI password", c.name)
		}
	}
	// The QOI the firmware asks for is behind the same check.
	rec := imageRequest(t, s, "", "/pages/small/eins/"+rp.hash+".png?format=qoi")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("qoi without a key: %d", rec.Code)
	}
}

// placeholderImage is a small paletted page for a notice.
func placeholderImage() *image.Paletted {
	img := image.NewPaletted(image.Rect(0, 0, 20, 10), color.Palette{color.Gray{0}, color.Gray{255}})
	return img
}

// TestDeviceImageServesTheLastPage gives the dashboard the image the panel
// received, behind the UI password, although the page route needs the device
// key (ADR 0040, 0097).
func TestDeviceImageServesTheLastPage(t *testing.T) {
	s := keyServer(t, "geheim")
	registerKey(t, s, "aa:bb:cc:dd:ee:01")
	rp := storeTestPage(s, "small", "eins")

	dev := s.config().Devices[0]
	s.mu.Lock()
	st := s.state[dev.stateKey()]
	st.LastPage, st.LastPageKind, st.LastPageHash = "eins", "page", rp.hash
	s.mu.Unlock()

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/devices/test/image", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("without the UI password: %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/devices/test/image", nil)
	req.SetBasicAuth("", "geheim")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("last image: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !bytes.Equal(rec.Body.Bytes(), rp.data) {
		t.Fatal("the route must serve the image the device received")
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache header: %q", rec.Header().Get("Cache-Control"))
	}
	// An unknown device and a device without a served image are 404.
	for _, path := range []string{"/api/devices/unbekannt/image", "/api/devices/zwei/image"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.SetBasicAuth("", "geheim")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", path, rec.Code)
		}
	}
}

// TestDeviceImageServesANotice covers the notice kind, whose profile is checked
// against the device (ADR 0097).
func TestDeviceImageServesANotice(t *testing.T) {
	s := keyServer(t, "geheim")
	registerKey(t, s, "aa:bb:cc:dd:ee:01")
	hint := s.notice("small", placeholderImage())

	dev := s.config().Devices[0]
	s.mu.Lock()
	st := s.state[dev.stateKey()]
	st.LastPage, st.LastPageKind, st.LastPageHash = "", "notice", hint.hash
	s.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/devices/test/image", nil)
	req.SetBasicAuth("", "geheim")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("notice image: %d", rec.Code)
	}
	// A notice of the other display type is not shown on this card.
	s.mu.Lock()
	s.hints[hint.hash] = hintPage{data: []byte("fremd"), profile: "other"}
	s.mu.Unlock()
	req = httptest.NewRequest(http.MethodGet, "/api/devices/test/image", nil)
	req.SetBasicAuth("", "geheim")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("notice of another type: %d", rec.Code)
	}
}
