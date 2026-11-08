package paperboy

import (
	"net/http"
	"strings"
	"testing"
)

// TestEditorOverview lists the page files by page and opens a shared page for
// the selected display type (ADR 0068); the other object types sit in their own
// sub-tab.
func TestEditorOverview(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "small", previewTemplate)
	writePage(t, s, "shared", "", previewTemplate)

	rec := request(t, s, http.MethodGet, "/ui/editor?display=small", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("editor: %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<strong>solar</strong>`, `<strong>shared</strong>`,
		`pages/solar.small.yaml`, `pages/shared.yaml · shared`,
		"/ui/editor/page?display=small&amp;name=solar",  // the variant of the selected type
		"/ui/editor/page?display=small&amp;name=shared", // the shared page follows the selection
		"New page",
		`href="/ui/editor?display=small&amp;tab=pages" class="active">Pages<span class="count">2</span>`,
		`href="/ui/editor?tab=plans">Plans<span class="count">`,
		"<a href=\"/ui/downloads\">Downloads</a> · <a href=\"/ui/plugins\">Widgets</a> · <a class=\"active\" href=\"/ui/editor\">Editor",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("overview lacks %q:\n%s", want, body)
		}
	}
	// The plans and the device files are no longer stacked below the pages.
	if strings.Contains(body, "A plan is <code>plans/") {
		t.Fatalf("the pages tab carries the plan list:\n%s", body)
	}

	// Without a selection the editor works on the first image display type.
	if rec := request(t, s, http.MethodGet, "/ui/editor", nil, nil); !strings.Contains(rec.Body.String(), "href=\"/ui/editor?display=small\"") {
		t.Fatalf("default display missing:\n%s", rec.Body.String())
	}

	// A broken page is listed with its error and cannot be opened; a variant of
	// a display type that is not configured is listed as well.
	writePage(t, s, "broken", "small", "title: x\nnope: 1\n")
	writePage(t, s, "solar", "old", previewTemplate)
	body = request(t, s, http.MethodGet, "/ui/editor?display=small", nil, nil).Body.String()
	if !strings.Contains(body, `<strong>broken</strong>`) || !strings.Contains(body, "yaml: unmarshal errors") {
		t.Fatalf("broken page not reported:\n%s", body)
	}
	if !strings.Contains(body, `display type &#34;old&#34; is not configured`) {
		t.Fatalf("unknown variant not reported:\n%s", body)
	}
	if strings.Contains(body, "/ui/editor/page?display=old") {
		t.Fatalf("a variant of an unknown display type is offered:\n%s", body)
	}
}

// TestEditorPageRows shows every page once and resolves its row for the
// selected display type: the variant of that type, else the shared page
// (issue #107). The files the page has stay reachable through the variants
// column, and a file neither the row nor the switch can reach keeps its own
// Delete.
func TestEditorPageRows(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "small", previewTemplate)
	writePage(t, s, "solar", "lcd", "title: Solar text\nkind: text\ngrid: [20, 4]\ncells:\n  - { at: [0, 0], span: [20, 1], widget: paperboy/text, args: { text: hi } }\n")
	writePage(t, s, "solar", "", previewTemplate)
	writePage(t, s, "clock", "lcd", "title: Clock\nkind: text\ngrid: [20, 4]\ncells:\n  - { at: [0, 0], span: [20, 1], widget: paperboy/clock }\n")

	body := request(t, s, http.MethodGet, "/ui/editor?display=small", nil, nil).Body.String()
	for _, want := range []string{
		// The row shows the variant of the selected type, its Delete names that file.
		"pages/solar.small.yaml",
		`data-delete="/api/pages/solar?display=small&amp;hash=`,
		// The other variant stays reachable and is marked as not shown.
		`href="/ui/editor/page?display=lcd&amp;name=solar">lcd</a>`,
		// The shared page has no switch entry next to a variant, so its chip keeps
		// its own Delete.
		`data-delete="/api/pages/solar?display=&amp;hash=`,
		// A page without a variant for the type and without a shared page says so
		// and offers no Open; its variant is still a link.
		"no file for display type &#34;small&#34; and no shared page",
		`href="/ui/editor/page?display=lcd&amp;name=clock">lcd</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("pages tab lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "/ui/editor/page?display=small&amp;name=clock") {
		t.Fatalf("a page without a file for the display type offers Open:\n%s", body)
	}
	if strings.Contains(body, "pages/clock.small.yaml") {
		t.Fatalf("a page without a file for the display type shows one:\n%s", body)
	}

	// With the text display type selected the same page shows its text variant.
	body = request(t, s, http.MethodGet, "/ui/editor?display=lcd", nil, nil).Body.String()
	if !strings.Contains(body, "pages/solar.lcd.yaml") || !strings.Contains(body, "pages/clock.lcd.yaml") {
		t.Fatalf("the text display type does not resolve the text variants:\n%s", body)
	}
}

// TestEditorTabs shows one object type per sub-tab, with its own filter.
func TestEditorTabs(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "small", previewTemplate)
	writePlanFile(t, s, "zimmer", testPlan)
	writeDeviceFile(t, s.config().Dir, "wohnzimmer", "display_type: small\nmac: aa:bb:cc:dd:ee:02\nplan: zimmer\n")

	tab := func(name string) string {
		return request(t, s, http.MethodGet, "/ui/editor?tab="+name, nil, nil).Body.String()
	}
	plans := tab("plans")
	if !strings.Contains(plans, `id="plan-table"`) || !strings.Contains(plans, "<td>zimmer</td>") {
		t.Fatalf("plans tab:\n%s", plans)
	}
	if strings.Contains(plans, `id="page-table"`) {
		t.Fatalf("plans tab carries the page list:\n%s", plans)
	}
	devices := tab("devices")
	if !strings.Contains(devices, `id="device-table"`) || !strings.Contains(devices, "<td>wohnzimmer</td>") ||
		!strings.Contains(devices, "New device") {
		t.Fatalf("devices tab:\n%s", devices)
	}
	// An unknown tab falls back to the pages.
	if body := tab("nope"); !strings.Contains(body, `id="page-table"`) {
		t.Fatalf("unknown tab:\n%s", body)
	}
}

// TestEditorDeleteWarnings names the references in the delete confirmations and
// carries them to the editors (ADR 0073).
func TestEditorDeleteWarnings(t *testing.T) {
	s := planEditorServer(t)
	s.config().Devices[0].Plan = "tour"

	plans := request(t, s, http.MethodGet, "/ui/editor?tab=plans", nil, nil).Body.String()
	for _, want := range []string{
		"New plan", `action="/ui/editor/plan"`,
		`data-delete="/api/plans/tour?hash=`,
		"Delete plans/tour.yaml?", "Devices that use it: test",
	} {
		if !strings.Contains(plans, want) {
			t.Fatalf("plans tab lacks %q:\n%s", want, plans)
		}
	}
	body := request(t, s, http.MethodGet, "/ui/editor?display=small", nil, nil).Body.String()
	for _, want := range []string{
		`data-delete="/api/pages/solar?display=small&amp;hash=`,
		"Delete pages/solar.small.yaml?", "Plans that name it: tour",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("pages tab lacks %q:\n%s", want, body)
		}
	}

	// The page editor knows the plans that name its page, the display type
	// editor the page variants that use the type.
	page := request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=small", nil, nil).Body.String()
	if !strings.Contains(page, `"usedBy":["tour"]`) {
		t.Fatalf("page editor warning:\n%s", page)
	}
	display := request(t, s, http.MethodGet, "/ui/editor/display?name=small&new=1", nil, nil).Body.String()
	if !strings.Contains(display, `"pages":["solar"]`) || !strings.Contains(display, `id="delete"`) {
		t.Fatalf("display editor warning:\n%s", display)
	}
}

// TestEditorPage opens one page for a display type and starts a new page when
// there is no file yet.
func TestEditorPage(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "small", previewTemplate)

	rec := request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=small", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("editor page: %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<script id="editor-data" type="application/json">{"name":"solar","display":"small","kind":"image","width":40,"height":20,"new":false,"shared":false,"grid":[6,8],"reference":[6,8]}</script>`,
		`<script src="/ui/editor.js"></script>`, `id="board"`, `id="palette"`, `id="palette-filter"`, `id="inspector"`,
		`id="show-cells" checked`, `id="render-hint"`, `id="preview-error"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("editor page lacks %q:\n%s", want, body)
		}
	}
	// The board carries the preview itself, so the page editor has no separate
	// preview panel (ADR 0074); the playground keeps it.
	if strings.Contains(body, `id="preview-img"`) {
		t.Fatalf("the page editor still has a preview panel:\n%s", body)
	}

	// The page has no variant for the text display: it is not there for it.
	if rec := request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=lcd", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing variant: %d %s", rec.Code, rec.Body)
	}
	// Asked as a new page it starts with the kind and character grid of the display.
	body = request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=lcd&new=1", nil, nil).Body.String()
	if !strings.Contains(body, `"kind":"text","width":20,"height":4,"new":true,"shared":false,"grid":[20,4]`) {
		t.Fatalf("new text page:\n%s", body)
	}

	// A shared page says so, so the editor writes the shared file again.
	writePage(t, s, "shared", "", previewTemplate)
	if body := request(t, s, http.MethodGet, "/ui/editor/page?name=shared&display=lcd", nil, nil).Body.String(); !strings.Contains(body, `"shared":true`) {
		t.Fatalf("shared page:\n%s", body)
	}
	// Without a display type the first configured one is used.
	if body := request(t, s, http.MethodGet, "/ui/editor/page?name=shared", nil, nil).Body.String(); !strings.Contains(body, `"display":"small"`) {
		t.Fatalf("default display:\n%s", body)
	}

	for _, path := range []string{
		"/ui/editor/page?name=Nope&display=small", // invalid name
		"/ui/editor/page?name=solar&display=nope", // unknown display type
		"/ui/editor/page?name=..%2Fx&display=small",
	} {
		if rec := request(t, s, http.MethodGet, path, nil, nil); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
}

// TestEditorPageWithoutDisplayTypes explains a configuration that has no
// display type instead of drawing an empty editor.
func TestEditorPageWithoutDisplayTypes(t *testing.T) {
	cfg := &Config{Dir: t.TempDir()}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if body := request(t, s, http.MethodGet, "/ui/editor", nil, nil).Body.String(); !strings.Contains(body, "No display types configured") {
		t.Fatalf("overview lacks the hint:\n%s", body)
	}
	rec := request(t, s, http.MethodGet, "/ui/editor/page?name=solar&new=1", nil, nil)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "no display type is configured") {
		t.Fatalf("editor page: %d %s", rec.Code, rec.Body)
	}
}

// TestEditorScript is served from the binary, so the editor needs no separate
// file next to it.
func TestEditorScript(t *testing.T) {
	s, _ := testServer(t)
	rec := request(t, s, http.MethodGet, "/ui/editor.js", nil, nil)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("script: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, want := range []string{"/api/pages/", "/api/plugins", "/api/preview/", "PUT"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("script lacks %q", want)
		}
	}
}

// TestEditorNavigation links the editor from the other views.
func TestEditorNavigation(t *testing.T) {
	s, _ := testServer(t)
	for _, path := range []string{"/ui", "/ui/plugins?", "/ui/plan?device=test"} {
		rec := request(t, s, http.MethodGet, path, nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
		if !strings.Contains(rec.Body.String(), `href="/ui/editor"`) {
			t.Fatalf("%s does not link the editor", path)
		}
	}
}
