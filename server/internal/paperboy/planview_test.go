package paperboy

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlanViewShowsPagesAndInterval checks that the plan view names the
// effective interval, the next window edge and previews every page without
// touching the device state.
func TestPlanViewShowsPagesAndInterval(t *testing.T) {
	s, _ := planServer(t, testPlan)
	dev := planDevice(t, s)
	displayPage(t, s, dev)
	s.mu.Lock()
	before := *s.state["AABBCCDDEE01"]
	s.mu.Unlock()

	rec := htmlRequest(t, s, "/ui/devices/test/plan")
	if rec.Code != http.StatusOK {
		t.Fatalf("plan view: %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"refresh <strong>every 5 min</strong>",
		"next change Wed 19:00 → every 30 min",
		"/api/preview/small/eins?at=2026-10-21T08%3A00&amp;device=test",
		"alarm",
		"hidden",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("plan view misses %q: %s", want, body)
		}
	}

	body = htmlRequest(t, s, "/ui/devices/test/plan?at=2026-10-21T20%3A30").Body.String()
	if !strings.Contains(body, "refresh <strong>every 30 min</strong>") || !strings.Contains(body, "next change Thu 07:00 → every 5 min") {
		t.Fatalf("plan view at 20:30: %s", body)
	}

	s.mu.Lock()
	after := *s.state["AABBCCDDEE01"]
	s.mu.Unlock()
	if after.Trigger != before.Trigger || after.Page != before.Page || after.PageName != before.PageName || after.LastSeen != before.LastSeen {
		t.Fatalf("plan view changed the device state: %+v -> %+v", before, after)
	}

	if rec := htmlRequest(t, s, "/ui/devices/test/plan?at=tomorrow"); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad time: %d", rec.Code)
	}
	if rec := preview(t, s, "/api/preview/small/eins?at=2026-10-21T20:30"); rec.Code != http.StatusOK {
		t.Fatalf("preview at a time: %d %s", rec.Code, rec.Body)
	}
}

// TestPlanViewShowsTrigger marks the page a trigger shows and its condition.
func TestPlanViewShowsTrigger(t *testing.T) {
	s, _ := planServer(t, testPlan)
	withTriggerValue(t, s, "low")
	dev := planDevice(t, s)
	displayPage(t, s, dev)
	body := htmlRequest(t, s, "/ui/devices/test/plan").Body.String()
	for _, want := range []string{
		"trigger <strong>alarm</strong>",
		`<span class="badge active">triggered</span>`,
		`<span class="badge active">active</span>`,
		`<span class="badge match">matches</span>`,
		"alert refresh",
		"fake/state is low, urgent · now low",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("plan view misses %q: %s", want, body)
		}
	}
}

// TestPlanViewSkippedAndBrokenPages shows why a page is not drawn.
func TestPlanViewSkippedAndBrokenPages(t *testing.T) {
	s, _ := planServer(t, "pages: [eins, fehlt, kaputt]\n")
	if err := os.WriteFile(filepath.Join(s.config().Pages, "kaputt.yaml"), []byte("kind: image\ngrid: [1, 1]\nnope: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := htmlRequest(t, s, "/ui/devices/test/plan").Body.String()
	for _, want := range []string{
		"Skipped: no page for this display type",
		`<p class="error">`,
		"kaputt.yaml",
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
	if rec := htmlRequest(t, s, "/ui/devices/nope/plan"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown device: %d", rec.Code)
	}
	if rec := htmlRequest(t, s, "/ui/plan"); rec.Code != http.StatusNotFound {
		t.Fatalf("the old plan route: %d, want it gone", rec.Code)
	}
}
