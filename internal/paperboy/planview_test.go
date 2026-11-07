package paperboy

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlanViewShowsActiveRule checks that the plan view marks the rule that
// applies now or at a chosen time, names the next change and previews the
// pages without touching the device state.
func TestPlanViewShowsActiveRule(t *testing.T) {
	s, _ := planServer(t, testPlan)
	dev := planDevice(t, s)
	displayPage(t, s, dev)
	s.mu.Lock()
	before := *s.state["test"]
	s.mu.Unlock()

	rec := htmlRequest(t, s, "/ui/plan?device=test")
	if rec.Code != http.StatusOK {
		t.Fatalf("plan view: %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"rule <strong>morgen</strong>",
		`<section class="rule active">` + "\n  <h2>morgen",
		"next change Wed 09:00 → tag",
		"/api/preview/small/eins?at=2026-10-21T08%3A00",
		"07:00–09:00",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("plan view misses %q: %s", want, body)
		}
	}

	body = htmlRequest(t, s, "/ui/plan?device=test&at=2026-10-21T20:30").Body.String()
	if !strings.Contains(body, "rule <strong>nacht</strong>") || !strings.Contains(body, "next change Thu 07:00 → morgen") {
		t.Fatalf("plan view at 20:30: %s", body)
	}

	s.mu.Lock()
	after := *s.state["test"]
	s.mu.Unlock()
	if after.Rule != before.Rule || after.Page != before.Page || after.LastSeen != before.LastSeen {
		t.Fatalf("plan view changed the device state: %+v -> %+v", before, after)
	}

	if rec := htmlRequest(t, s, "/ui/plan?device=test&at=tomorrow"); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad time: %d", rec.Code)
	}
	if rec := preview(t, s, "/api/preview/small/eins?at=2026-10-21T20:30"); rec.Code != http.StatusOK {
		t.Fatalf("preview at a time: %d %s", rec.Code, rec.Body)
	}
}

// TestPlanViewFallback marks a rule that only applies as the fallback.
func TestPlanViewFallback(t *testing.T) {
	s, _ := planServer(t, `
rules:
  - name: morgen
    when: { from: "07:00", to: "08:00" }
    pages: [eins]
  - name: rest
    when: { days: [sa] }
    pages: [zwei]
fallback: rest
`)
	body := htmlRequest(t, s, "/ui/plan?device=test").Body.String()
	if !strings.Contains(body, "rule <strong>rest</strong> (fallback)") {
		t.Fatalf("fallback not shown: %s", body)
	}
}

// TestPlanViewSkippedAndBrokenPages shows why a page is not drawn.
func TestPlanViewSkippedAndBrokenPages(t *testing.T) {
	s, _ := planServer(t, `
rules:
  - name: alles
    pages: [eins, fehlt, kaputt]
`)
	if err := os.WriteFile(filepath.Join(s.config().Pages, "kaputt.small.yaml"), []byte("grid: [1, 1]\nnope: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := htmlRequest(t, s, "/ui/plan?device=test").Body.String()
	for _, want := range []string{
		"Skipped: no page for this display type",
		`<p class="error">`,
		"kaputt.small.yaml",
		"/api/preview/small/eins",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("plan view misses %q: %s", want, body)
		}
	}
	if strings.Contains(body, "/api/preview/small/kaputt") || strings.Contains(body, "/api/preview/small/fehlt") {
		t.Fatalf("skipped or broken pages must not be previewed: %s", body)
	}
}

func TestPlanViewUnknownDevice(t *testing.T) {
	s, _ := planServer(t, testPlan)
	if rec := htmlRequest(t, s, "/ui/plan?device=nope"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown device: %d", rec.Code)
	}
	if rec := htmlRequest(t, s, "/ui/plan"); rec.Code != http.StatusNotFound {
		t.Fatalf("missing device: %d", rec.Code)
	}
}
