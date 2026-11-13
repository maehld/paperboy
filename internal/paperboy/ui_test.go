package paperboy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func htmlRequest(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// TestDashboardShowsDevices checks the operator overview: the root redirects to
// it, and every card names the plan, the rule and the page it previews.
func TestDashboardShowsDevices(t *testing.T) {
	s, _ := planServer(t, testPlan)

	rec := htmlRequest(t, s, "/")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/ui" {
		t.Fatalf("root redirect: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	rec = htmlRequest(t, s, "/ui")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("dashboard: %d %v", rec.Code, rec.Header())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "test") || !strings.Contains(body, "zimmer") {
		t.Fatalf("device card missing: %s", body)
	}
	if !strings.Contains(body, `http-equiv="refresh" content="30"`) {
		t.Fatal("dashboard must refresh itself")
	}

	dev := planDevice(t, s)
	percent := 61.0
	dev.req.Battery = &syncBattery{Volts: 3.74, Percent: &percent}
	out := displayPage(t, s, dev)
	body = htmlRequest(t, s, "/ui").Body.String()
	if want := pagePath(out); !strings.Contains(body, want) {
		t.Fatalf("dashboard must preview the served page %q: %s", want, body)
	}
	for _, want := range []string{"zimmer", "eins", "every 10 min", "3.74 V · 61 %", "small <span class=\"muted\">(sticky)</span>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard misses %q: %s", want, body)
		}
	}
	// The plan view and the virtual display are actions at the foot of the card
	// rather than links inside the plan row (ADR 0054, ADR 0091).
	for _, want := range []string{
		`<a href="/ui/plan?device=test">Plan preview</a>`,
		`<a href="/ui/virtual?display=small&amp;plan=zimmer">Virtual display</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard misses the action %q: %s", want, body)
		}
	}

	// A render error is shown as text, never as markup.
	s.mu.Lock()
	s.state["AABBCCDDEE01"].LastError = `<b>boom</b>`
	s.mu.Unlock()
	body = htmlRequest(t, s, "/ui").Body.String()
	if !strings.Contains(body, "&lt;b&gt;boom&lt;/b&gt;") || strings.Contains(body, "<b>boom</b>") {
		t.Fatalf("error not escaped: %s", body)
	}

	if rec := htmlRequest(t, s, "/unknown"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown path: %d", rec.Code)
	}
}

// TestDashboardAfterRestart checks that a stored hash without its rendered
// image never becomes a dead link: a page shows through its preview, a notice
// as a placeholder.
func TestDashboardAfterRestart(t *testing.T) {
	s, _ := planServer(t, testPlan)
	dev := planDevice(t, s)
	served := pagePath(displayPage(t, s, dev))

	s.mu.Lock()
	s.rendered = map[string]*renderedPage{}
	s.hints = map[string][]byte{}
	s.mu.Unlock()
	for _, d := range s.deviceInfos() {
		if d.LastPageURL != "" {
			t.Fatalf("dead image URL after restart: %q", d.LastPageURL)
		}
	}
	body := htmlRequest(t, s, "/ui").Body.String()
	if strings.Contains(body, served) || !strings.Contains(body, `src="/api/preview/`) {
		t.Fatalf("page must fall back to the preview: %s", body)
	}

	s.mu.Lock()
	s.state["AABBCCDDEE01"].LastPageKind = "notice"
	s.state["AABBCCDDEE01"].LastPage = ""
	s.mu.Unlock()
	body = htmlRequest(t, s, "/ui").Body.String()
	if strings.Contains(body, "/hints/") || !strings.Contains(body, "waiting for the next sync") {
		t.Fatalf("notice must become a placeholder: %s", body)
	}
}

func TestDashboardPower(t *testing.T) {
	cfg := &Config{Profiles: map[string]Profile{"auto": {}, "fixed": {Power: powerMains}}}
	volts := deviceState{BatteryVoltage: "4.12"}
	percent, discharging := 61.0, -68.0
	gauged := deviceState{BatteryVoltage: "3.91", BatteryPercent: &percent, BatteryMA: &discharging}
	for _, c := range []struct {
		dev      Device
		reported string
		st       deviceState
		want     string
		origin   string
	}{
		{Device{Profile: "auto", Power: powerMains}, "", deviceState{}, "Mains", "set on device"},
		{Device{Profile: "fixed"}, "", volts, "Mains · 4.12 V", "set on display type"},
		{Device{Profile: "auto"}, powerMains, deviceState{}, "Mains", "reported by firmware"},
		{Device{Profile: "auto"}, "", volts, "Battery · 4.12 V", "fallback"},
		{Device{Profile: "auto"}, powerBattery, gauged, "Battery · 3.91 V · 61 % · -68 mA", "reported by firmware"},
	} {
		got := powerSource(cfg.power(&c.dev, c.reported), c.st)
		if origin := powerOrigin(cfg, &c.dev, c.reported); got != c.want || origin != c.origin {
			t.Errorf("%+v %q: got %q (%s), want %q (%s)", c.dev, c.reported, got, origin, c.want, c.origin)
		}
	}
}
