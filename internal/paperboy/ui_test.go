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
	for _, want := range []string{"zimmer", "eins", "every 10 min", "3.74 V · 61 %"} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard misses %q: %s", want, body)
		}
	}

	// A render error is shown as text, never as markup.
	s.mu.Lock()
	s.state["test"].LastError = `<b>boom</b>`
	s.mu.Unlock()
	body = htmlRequest(t, s, "/ui").Body.String()
	if !strings.Contains(body, "&lt;b&gt;boom&lt;/b&gt;") || strings.Contains(body, "<b>boom</b>") {
		t.Fatalf("error not escaped: %s", body)
	}

	if rec := htmlRequest(t, s, "/unknown"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown path: %d", rec.Code)
	}
}
