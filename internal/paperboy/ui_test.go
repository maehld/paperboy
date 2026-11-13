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
	displayPage(t, s, dev)
	body = htmlRequest(t, s, "/ui").Body.String()
	if want := lastImagePath("test"); !strings.Contains(body, want) {
		t.Fatalf("dashboard must show the served page through %q: %s", want, body)
	}
	for _, want := range []string{"zimmer", "eins", "every 10 min", "3.74 V · 61 %", "small <span class=\"muted\">(sticky)</span>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard misses %q: %s", want, body)
		}
	}
	// The power row names the state and the charge; the raw reading, the type
	// and the other static facts sit in the collapsed details, without the old
	// origin hint.
	for _, want := range []string{"Discharging · 61 %", `<details class="more">`, "<summary>Details</summary>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard misses %q: %s", want, body)
		}
	}
	if strings.Contains(body, "reported by firmware") {
		t.Fatalf("the power origin must not be shown: %s", body)
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
	s.hints = map[string]hintPage{}
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
	percent, lowPercent, charging, idle, discharging := 50.0, 12.0, 568.0, 0.0, -57.0
	for _, c := range []struct {
		name    string
		power   string
		st      deviceState
		want    string
		details string
	}{
		{"charging", powerMains, deviceState{BatteryVoltage: "3.83", BatteryPercent: &percent, BatteryMA: &charging},
			"Charging · 50 %", "3.83 V · 50 % · +568 mA"},
		{"connected without gauge", powerMains, deviceState{}, "Connected", ""},
		{"idle on mains", powerMains, deviceState{BatteryVoltage: "3.85", BatteryPercent: &percent, BatteryMA: &idle},
			"Connected · 50 %", "3.85 V · 50 % · 0 mA"},
		{"on battery", powerBattery, deviceState{BatteryVoltage: "3.63", BatteryPercent: &percent, BatteryMA: &discharging},
			"Discharging · 50 %", "3.63 V · 50 % · -57 mA"},
		{"voltage without a gauge", powerBattery, deviceState{BatteryVoltage: "4.03"},
			"Discharging · 4.03 V", "4.03 V"},
		{"draining on mains", powerMains, deviceState{BatteryVoltage: "3.62", BatteryPercent: &percent, BatteryMA: &discharging},
			"Discharging · 50 %", "3.62 V · 50 % · -57 mA"},
		{"low battery", powerBattery, deviceState{BatteryVoltage: "3.40", BatteryPercent: &lowPercent, BatteryLevel: batteryWarn},
			"Discharging · 12 % · low", "3.40 V · 12 %"},
	} {
		if got := powerText(c.power, c.st); got != c.want {
			t.Errorf("%s: power %q, want %q", c.name, got, c.want)
		}
		if got := batteryText(c.st); got != c.details {
			t.Errorf("%s: details %q, want %q", c.name, got, c.details)
		}
	}
}
