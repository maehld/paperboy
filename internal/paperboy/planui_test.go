package paperboy

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testEditablePlan = `rules:
  - name: day
    when: { from: "07:00", to: "22:00", days: [mon, tue] }
    pages: [solar, lcd]
    rotate: true
    refresh: 10m
  - name: night
    pages: [solar]
fallback: night
actions:
  - action: evcc/mode
    args: { loadpoint: 0, mode: pv }
    title: Charge
`

func writePlanFile(t *testing.T, s *Server, name, content string) {
	t.Helper()
	if err := os.MkdirAll(s.config().Plans, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.config().Plans, name+".yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// planEditorServer has a device on the plan "tour", an image page and a text
// page, so the editor can warn about pages that do not fit.
func planEditorServer(t *testing.T) *Server {
	t.Helper()
	s := editorServer(t)
	writePage(t, s, "solar", "small", previewTemplate)
	writePage(t, s, "lcd", "", "kind: text\ngrid: [20, 4]\ncells: [{ at: [0, 0], span: [20, 4], widget: paperboy/text, args: { text: hi } }]\n")
	writePlanFile(t, s, "tour", testEditablePlan)
	return s
}

// TestPlanEditor opens a plan and carries the devices, the display types and
// the pages with their fit for the script (ADR 0069).
func TestPlanEditor(t *testing.T) {
	s := planEditorServer(t)
	s.config().Devices[0].Plan = "tour"

	rec := request(t, s, http.MethodGet, "/ui/editor/plan?name=tour", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("plan editor: %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<script id="plan-data" type="application/json">`,
		`"name":"tour"`,
		`"devices":[{"id":"test","display":"small"}]`,
		`"displays":["small"]`,
		`<script src="/ui/editor/plan.js"></script>`,
		`id="rules"`, `id="actions"`, `id="fallback"`, `id="devices"`,
		`href="/ui/editor"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("plan editor lacks %q:\n%s", want, body)
		}
	}
	// The image page fits the image display, the text page does not.
	if !strings.Contains(body, `"name":"solar","kind":"image","grid":[2,2]`) {
		t.Fatalf("page data missing:\n%s", body)
	}
	if !strings.Contains(body, `"fits":{"small":"page is for text displays (kind: text)"}`) {
		t.Fatalf("fit warning missing:\n%s", body)
	}

	// A plan that is not there and a name that cannot be one are explained.
	if rec := request(t, s, http.MethodGet, "/ui/editor/plan?name=nope", nil, nil); rec.Code != http.StatusNotFound ||
		!strings.Contains(rec.Body.String(), "no plan") {
		t.Fatalf("missing plan: %d %s", rec.Code, rec.Body)
	}
	if rec := request(t, s, http.MethodGet, "/ui/editor/plan?name=Nope", nil, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid name: %d", rec.Code)
	}
}

// TestPlanEditorWithoutDevices has no fit warnings when no device uses the
// plan; the page list is still there to pick from.
func TestPlanEditorWithoutDevices(t *testing.T) {
	s := planEditorServer(t)
	body := request(t, s, http.MethodGet, "/ui/editor/plan?name=tour", nil, nil).Body.String()
	if !strings.Contains(body, `"devices":[]`) || !strings.Contains(body, `"displays":[]`) {
		t.Fatalf("device list:\n%s", body)
	}
	if strings.Contains(body, `"fits"`) {
		t.Fatalf("fit warnings without devices:\n%s", body)
	}
	if !strings.Contains(body, `"name":"solar"`) || !strings.Contains(body, `"name":"lcd"`) {
		t.Fatalf("pages missing:\n%s", body)
	}
}

// TestPlanEditorScript serves the plan script from the binary.
func TestPlanEditorScript(t *testing.T) {
	s, _ := testServer(t)
	rec := request(t, s, http.MethodGet, "/ui/editor/plan.js", nil, nil)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("script: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, want := range []string{"/api/plans/", "/api/plugins", "PUT"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("script lacks %q", want)
		}
	}
}

// TestEditorOverviewPlans lists the plans next to the pages (ADR 0069).
func TestEditorOverviewPlans(t *testing.T) {
	s := planEditorServer(t)
	writePlanFile(t, s, "broken", "rules:\n  - name: day\n    pages: [solar]\n    nope: 1\n")

	body := request(t, s, http.MethodGet, "/ui/editor?tab=plans", nil, nil).Body.String()
	for _, want := range []string{
		"<h2>Plans", "/ui/editor/plan?name=tour", "<td>tour</td>", "<td>night</td>",
		"<td>broken</td>", "yaml: unmarshal errors",
		`href="/ui/editor?display=small&amp;tab=pages"`, // the way back to the pages
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("overview lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "/ui/editor/plan?name=broken") {
		t.Fatalf("a broken plan is offered:\n%s", body)
	}
}
