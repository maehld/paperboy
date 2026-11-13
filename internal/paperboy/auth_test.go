package paperboy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testServerWithPassword is testServer with a UI password (ADR 0096).
func testServerWithPassword(t *testing.T, password string) *Server {
	t.Helper()
	cfg := &Config{Dir: t.TempDir(), Profiles: map[string]Profile{"small": {Width: 40, Height: 20, PPI: 125, Gray: true}},
		Devices: []Device{{ID: "test", MAC: "aa:bb:cc:dd:ee:01", Profile: "small", Refresh: Duration(10 * time.Minute)}},
		UI:      UIConfig{Password: password}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.Local)
	s.now = func() time.Time { return now }
	return s
}

// sampleRequest builds a request that matches a route pattern: path values
// become "x" and the root pattern becomes "/".
func sampleRequest(t *testing.T, pattern string) *http.Request {
	t.Helper()
	method, path, ok := strings.Cut(pattern, " ")
	if !ok {
		t.Fatalf("route pattern %q has no method", pattern)
	}
	path = strings.ReplaceAll(path, "/{$}", "/")
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, "{") {
			parts[i] = "x"
		}
	}
	return httptest.NewRequest(method, strings.Join(parts, "/"), nil)
}

// TestUIRequiresPassword walks the UI table: every route answers 401 without
// credentials, whatever the handler would do (ADR 0096). A new UI route is
// therefore covered by this test without being listed here.
func TestUIRequiresPassword(t *testing.T) {
	s := testServerWithPassword(t, "geheim")
	h := s.Handler()
	if len(s.uiRoutes()) < 40 {
		t.Fatalf("UI table has only %d routes; did the split drop entries?", len(s.uiRoutes()))
	}
	for _, r := range s.uiRoutes() {
		req := sampleRequest(t, r.pattern)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without credentials: %d, want 401 (%s)", r.pattern, rec.Code, rec.Body)
			continue
		}
		if got := rec.Header().Get("WWW-Authenticate"); !strings.Contains(got, `Basic realm="Paperboy"`) {
			t.Errorf("%s: WWW-Authenticate %q", r.pattern, got)
		}
	}
}

// TestUIDeviceRoutesStayOpen walks the device table: none of those routes may
// run into the UI password, because the firmware sends no Basic credentials
// (ADR 0042, 0096).
func TestUIDeviceRoutesStayOpen(t *testing.T) {
	s := testServerWithPassword(t, "geheim")
	h := s.Handler()
	prefixes := []string{"/api/device/", "/pages/", "/hints/", "/firmware/"}
	for _, r := range s.deviceRoutes() {
		_, path, _ := strings.Cut(r.pattern, " ")
		open := false
		for _, p := range prefixes {
			if strings.HasPrefix(path, p) {
				open = true
			}
		}
		if !open {
			t.Errorf("device route %s is not under %v: is it really a device route?", r.pattern, prefixes)
		}
		req := sampleRequest(t, r.pattern)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Header().Get("WWW-Authenticate") != "" {
			t.Errorf("%s hit the UI password: %d %s", r.pattern, rec.Code, rec.Body)
		}
	}
}

// TestUIPasswordAllowsAccess checks the other direction: the right password
// opens the UI, a wrong one does not, and the device registration keeps working
// next to it.
func TestUIPasswordAllowsAccess(t *testing.T) {
	s := testServerWithPassword(t, "geheim")
	h := s.Handler()
	get := func(password string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
		if password != "" {
			req.SetBasicAuth("paperboy", password)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := get(""); code != http.StatusUnauthorized {
		t.Fatalf("no credentials: %d", code)
	}
	if code := get("falsch"); code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", code)
	}
	if code := get("geheim "); code != http.StatusUnauthorized {
		t.Fatalf("password with trailing space: %d", code)
	}
	if code := get("geheim"); code != http.StatusOK {
		t.Fatalf("right password: %d", code)
	}
	// The device API keeps its own model: register with the configured MAC.
	rec := register(t, s, `{"mac":"aa:bb:cc:dd:ee:01","hardware":"sticky"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"key"`) {
		t.Fatalf("register with a UI password: %d %s", rec.Code, rec.Body)
	}
}

// TestUIWithoutPasswordIsOpen keeps an instance that never set ui.password
// working, which is what every v2 instance does today.
func TestUIWithoutPasswordIsOpen(t *testing.T) {
	s, _ := testServer(t)
	for _, path := range []string{"/ui", "/api/devices", "/api/pages", "/"} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code == http.StatusUnauthorized {
			t.Errorf("%s without a configured password: 401", path)
		}
	}
}

// TestUIWrongMethodStaysOnUISide keeps the classification independent of the
// method: a UI path with a method no UI route accepts answers 401 (and 405 with
// the password), not the device API's 404.
func TestUIWrongMethodStaysOnUISide(t *testing.T) {
	s := testServerWithPassword(t, "geheim")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/pages/x", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /api/pages/x without credentials: %d, want 401", rec.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/pages/x", nil)
	req.SetBasicAuth("", "geheim")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/pages/x with credentials: %d, want 405", rec.Code)
	}
}

// TestUICrossSiteWritesRefused blocks the cross-site form post that a browser
// would sign with its cached credentials (ADR 0096). Writes without an Origin
// (curl, tests) stay allowed.
func TestUICrossSiteWritesRefused(t *testing.T) {
	s := testServerWithPassword(t, "geheim")
	cases := []struct {
		name   string
		origin string
		secret string
		want   int
	}{
		{"cross-site with credentials", "http://evil.example", "geheim", http.StatusForbidden},
		{"opaque origin with credentials", "null", "geheim", http.StatusForbidden},
		{"cross-site, fetch metadata", "http://evil.example", "geheim", http.StatusForbidden},
		{"same origin", "http://example.com", "geheim", http.StatusBadRequest},
		{"no origin", "", "geheim", http.StatusBadRequest},
		{"cross-site without credentials", "http://evil.example", "", http.StatusForbidden},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/preview/small", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		if c.origin != "" {
			req.Header.Set("Origin", c.origin)
		}
		if c.name == "cross-site, fetch metadata" {
			req.Header.Set("Sec-Fetch-Site", "cross-site")
		}
		if c.secret != "" {
			req.SetBasicAuth("", c.secret)
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s: %d, want %d (%s)", c.name, rec.Code, c.want, rec.Body)
		}
	}
}

// TestUIReadsIgnoreCrossSite keeps GET requests working from anywhere; the
// origin check only guards writes.
func TestUIReadsIgnoreCrossSite(t *testing.T) {
	s := testServerWithPassword(t, "geheim")
	req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
	req.Header.Set("Origin", "http://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.SetBasicAuth("", "geheim")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cross-site read: %d", rec.Code)
	}
}

// TestUIPasswordFromEnvironment pins the ${VAR} expansion in ui.password.
func TestUIPasswordFromEnvironment(t *testing.T) {
	t.Setenv("TEST_PLACE", "Berlin")
	t.Setenv("TEST_UI_PASSWORD", "geheim")
	cfg, _, err := loadInstance(t, validConfig+"ui:\n  password: \"${TEST_UI_PASSWORD}\"\n", validDisplays, validDevices)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.Password != "geheim" {
		t.Fatalf("password: %q", cfg.UI.Password)
	}
}

// TestUIPasswordUnsetVariable fails like any other secret (ADR 0049).
func TestUIPasswordUnsetVariable(t *testing.T) {
	t.Setenv("TEST_PLACE", "Berlin")
	_, _, err := loadInstance(t, validConfig+"ui:\n  password: \"${TEST_UI_PASSWORD_MISSING}\"\n", validDisplays, validDevices)
	if err == nil || !strings.Contains(err.Error(), "TEST_UI_PASSWORD_MISSING") {
		t.Fatalf("unset variable: %v", err)
	}
}
