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
		// The rows resolve for the selected type and the chips open exactly one
		// file; the path lives in the tooltip (ADR 0093).
		`href="/ui/editor/page?display=small&amp;file=small&amp;name=solar" class="selected" title="pages/solar.small.yaml"`,
		`href="/ui/editor/page?display=small&amp;name=shared&amp;shared=1" class="selected" title="pages/shared.yaml"`,
		"New page",
		`data-dialog="new-page"`, `id="new-page"`, `action="/ui/editor/page"`,
		`href="/ui/editor?display=small&amp;tab=pages" class="active">Pages<span class="count">2</span>`,
		`href="/ui/editor?tab=plans">Plans<span class="count">`,
		"<a href=\"/ui/downloads\">Downloads</a> · <a href=\"/ui/plugins\">Widgets</a> · <a class=\"active\" href=\"/ui/editor\">Editor",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("overview lacks %q:\n%s", want, body)
		}
	}
	// The shared page is reachable, so it keeps no Delete next to the variant.
	if strings.Contains(body, `data-delete="/api/pages/solar?`) {
		t.Fatalf("the shared page still only offers a Delete:\n%s", body)
	}
	// The plans and the device files are no longer stacked below the pages.
	if strings.Contains(body, "A plan is <code>plans/") {
		t.Fatalf("the pages tab carries the plan list:\n%s", body)
	}

	// Without a selection the editor works on the first image display type.
	if rec := request(t, s, http.MethodGet, "/ui/editor", nil, nil); !strings.Contains(rec.Body.String(), "href=\"/ui/editor?display=small\"") {
		t.Fatalf("default display missing:\n%s", rec.Body.String())
	}

	// A broken page is listed with its error but still opens read-only in the
	// editor; a variant of a display type that is not configured opens through a
	// configured type of its kind (ADR 0079, 0092).
	writePage(t, s, "broken", "small", "title: x\nnope: 1\n")
	writePage(t, s, "solar", "old", previewTemplate)
	body = request(t, s, http.MethodGet, "/ui/editor?display=small", nil, nil).Body.String()
	for _, want := range []string{
		`<strong>broken</strong>`,
		`href="/ui/editor/page?display=small&amp;file=small&amp;name=broken" class="broken selected"`,
		"pages/broken.small.yaml: yaml: unmarshal errors",
		`href="/ui/editor/page?display=small&amp;file=old&amp;name=solar"`,
		`display type &#34;old&#34; is not configured`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("overview lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "source=old") || strings.Contains(body, "/ui/editor/page?display=old") {
		t.Fatalf("a variant of an unknown display type is not offered by its own name:\n%s", body)
	}

	// A broken variant whose display type is gone cannot be opened at all, so it
	// is the one file that keeps its Delete in the list (ADR 0093).
	writePage(t, s, "gone", "old", "nope: 1\n")
	body = request(t, s, http.MethodGet, "/ui/editor?display=small", nil, nil).Body.String()
	if !strings.Contains(body, `data-delete="/api/pages/gone?display=old&amp;hash=`) {
		t.Fatalf("an unopenable file keeps no Delete:\n%s", body)
	}
}

// TestEditorOrphanVariant opens a page variant whose display type is gone
// through a configured display type of the same kind, so the file stays
// editable and can be saved as that type's variant (ADR 0079).
func TestEditorOrphanVariant(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "old", previewTemplate)
	// A text variant needs a text display type of its own.
	writePage(t, s, "clock", "gone", "kind: text\ngrid: [20, 4]\ncells:\n  - { at: [0, 0], span: [20, 1], widget: paperboy/text, args: { text: hi } }\n")

	// The file opens by name (ADR 0093); source is the old name of the same
	// link and keeps working (ADR 0079).
	for _, query := range []string{"file=old", "source=old"} {
		rec := request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=small&"+query, nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("orphan image variant (%s): %d %s", query, rec.Code, rec.Body)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `"orphan":"old"`) {
			t.Fatalf("orphan image variant lacks the marker (%s):\n%s", query, body)
		}
		if !strings.Contains(body, `"file":"old"`) {
			t.Fatalf("orphan image variant lacks the file (%s):\n%s", query, body)
		}
	}

	// The text variant opens for the text display type.
	if rec := request(t, s, http.MethodGet, "/ui/editor/page?name=clock&display=lcd&file=gone", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("orphan text variant: %d %s", rec.Code, rec.Body)
	}
	// Without the file the variant is not there for the selected display type.
	if rec := request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=small", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("variant without file: %d %s", rec.Code, rec.Body)
	}
	// A file that has no page is a 404.
	if rec := request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=small&file=nope", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing file: %d %s", rec.Code, rec.Body)
	}
}

// TestEditorPageRows shows every page once and resolves its row for the
// selected display type: the variant of that type, else the shared page
// (issue #107). Every file is a link that opens exactly it, so a shared page
// next to a variant needs no Delete (ADR 0093).
func TestEditorPageRows(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "small", previewTemplate)
	writePage(t, s, "solar", "lcd", "title: Solar text\nkind: text\ngrid: [20, 4]\ncells:\n  - { at: [0, 0], span: [20, 1], widget: paperboy/text, args: { text: hi } }\n")
	writePage(t, s, "solar", "", previewTemplate)
	writePage(t, s, "clock", "lcd", "title: Clock\nkind: text\ngrid: [20, 4]\ncells:\n  - { at: [0, 0], span: [20, 1], widget: paperboy/clock }\n")

	body := request(t, s, http.MethodGet, "/ui/editor?display=small", nil, nil).Body.String()
	for _, want := range []string{
		// The row shows the variant of the selected type and opens exactly it
		// (ADR 0084, 0092); the path lives in the tooltip.
		`title="pages/solar.small.yaml"`,
		`class="rowlink" data-href="/ui/editor/page?display=small&amp;file=small&amp;name=solar"`,
		// The other files stay reachable and are marked as not shown.
		`href="/ui/editor/page?display=small&amp;name=solar&amp;shared=1"`,
		`href="/ui/editor/page?display=lcd&amp;file=lcd&amp;name=solar"`,
		// A page with variants only is a normal state, not an error.
		`<p class="hint">no file for display type &#34;small&#34; and no shared page</p>`,
		`href="/ui/editor/page?display=lcd&amp;file=lcd&amp;name=clock"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("pages tab lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "data-delete=") {
		t.Fatalf("a reachable file keeps a Delete:\n%s", body)
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

// TestEditorSharedNextToVariant opens the shared page while the display type has
// a variant: both files stay reachable and the editor toggles between them
// (ADR 0093).
func TestEditorSharedNextToVariant(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "", previewTemplate)
	writePage(t, s, "solar", "small", previewTemplate)

	body := request(t, s, http.MethodGet, "/ui/editor?display=small", nil, nil).Body.String()
	// The shared page comes first and both files are links; the resolved file is
	// bold.
	if want := `href="/ui/editor/page?display=small&amp;name=solar&amp;shared=1" title="pages/solar.yaml">shared</a> · ` +
		`<a href="/ui/editor/page?display=small&amp;file=small&amp;name=solar" class="selected" title="pages/solar.small.yaml">small</a>`; !strings.Contains(body, want) {
		t.Fatalf("overview lacks %q:\n%s", want, body)
	}
	if strings.Contains(body, `data-delete="/api/pages/solar?`) {
		t.Fatalf("a reachable shared page keeps a Delete:\n%s", body)
	}

	// shared=1 opens the shared file although the type has a variant.
	body = request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=small&shared=1", nil, nil).Body.String()
	for _, want := range []string{
		`"display":"small","file":"","kind":"image"`,
		`"variant_exists":true`,
		`id="file-toggle" href="/ui/editor/page?display=small&amp;file=small&amp;name=solar">Edit small variant</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("shared page lacks %q:\n%s", want, body)
		}
	}
	// From the variant the toggle leads back to the shared page.
	body = request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=small&file=small", nil, nil).Body.String()
	if !strings.Contains(body, `id="file-toggle" href="/ui/editor/page?display=small&amp;name=solar&amp;shared=1">Edit shared</a>`) {
		t.Fatalf("variant page lacks the toggle back:\n%s", body)
	}

	// A broken file opens read-only instead of being unlinked (ADR 0093).
	writePage(t, s, "broken", "small", "nope: 1\n")
	if rec := request(t, s, http.MethodGet, "/ui/editor/page?name=broken&display=small&file=small", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("broken file does not open read-only: %d %s", rec.Code, rec.Body)
	}
}

// TestEditorSharedToggleKind keeps a cross-kind toggle honest: a text variant
// toggles to the shared image page for an image display type, not for its own
// text type (ADR 0093).
func TestEditorSharedToggleKind(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "", previewTemplate)
	writePage(t, s, "solar", "lcd", "title: Solar text\nkind: text\ngrid: [20, 4]\ncells:\n  - { at: [0, 0], span: [20, 1], widget: paperboy/text, args: { text: hi } }\n")

	body := request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=lcd&file=lcd", nil, nil).Body.String()
	if want := `id="file-toggle" href="/ui/editor/page?display=small&amp;name=solar&amp;shared=1">Edit shared</a>`; !strings.Contains(body, want) {
		t.Fatalf("cross-kind toggle lacks %q:\n%s", want, body)
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
	if !strings.Contains(plans, `id="plan-table"`) || !strings.Contains(plans, `<a href="/ui/editor/plan?name=zimmer">zimmer</a>`) {
		t.Fatalf("plans tab:\n%s", plans)
	}
	if strings.Contains(plans, `id="page-table"`) {
		t.Fatalf("plans tab carries the page list:\n%s", plans)
	}
	devices := tab("devices")
	if !strings.Contains(devices, `id="device-table"`) || !strings.Contains(devices, `<a href="/ui/editor/device?id=wohnzimmer">wohnzimmer</a>`) ||
		!strings.Contains(devices, "New device") || !strings.Contains(devices, `data-dialog="new-device"`) {
		t.Fatalf("devices tab:\n%s", devices)
	}
	// An unknown tab falls back to the pages.
	if body := tab("nope"); !strings.Contains(body, `id="page-table"`) {
		t.Fatalf("unknown tab:\n%s", body)
	}
}

// TestEditorDeleteWarnings keeps the reference warnings in the editors (ADR
// 0073) and keeps the list a list of links (ADR 0084).
func TestEditorDeleteWarnings(t *testing.T) {
	s := planEditorServer(t)
	s.config().Devices[0].Plan = "tour"

	plans := request(t, s, http.MethodGet, "/ui/editor?tab=plans", nil, nil).Body.String()
	for _, want := range []string{
		"New plan", `action="/ui/editor/plan"`,
		`class="rowlink" data-href="/ui/editor/plan?name=tour"`,
	} {
		if !strings.Contains(plans, want) {
			t.Fatalf("plans tab lacks %q:\n%s", want, plans)
		}
	}
	if strings.Contains(plans, "data-delete=") || strings.Contains(plans, "data-rename=") {
		t.Fatalf("the plans tab acts on files:\n%s", plans)
	}
	body := request(t, s, http.MethodGet, "/ui/editor?display=small", nil, nil).Body.String()
	if !strings.Contains(body, `class="rowlink" data-href="/ui/editor/page?display=small&amp;file=small&amp;name=solar"`) {
		t.Fatalf("pages tab lacks the row link:\n%s", body)
	}

	// The page editor knows the plans that name its page, the plan editor its
	// devices, the display type editor the page variants that use the type.
	page := request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=small", nil, nil).Body.String()
	if !strings.Contains(page, `"usedBy":["tour"]`) {
		t.Fatalf("page editor warning:\n%s", page)
	}
	plan := request(t, s, http.MethodGet, "/ui/editor/plan?name=tour", nil, nil).Body.String()
	if !strings.Contains(plan, `"devices":[{"id":"test","display":"small"}]`) {
		t.Fatalf("plan editor devices:\n%s", plan)
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
		`<script id="editor-data" type="application/json">{"name":"solar","display":"small","file":"small","kind":"image","width":40,"height":20,"new":false,"variant_exists":true,"grid":[6,8],"reference":[6,8],"rename":{"url":"/api/pages/solar/rename","warn":"Rename page solar?`,
		`<script src="/ui/editor.js"></script>`, `id="board"`, `id="palette"`, `id="palette-filter"`, `id="inspector"`,
		`id="show-cells" checked`, `id="render-hint"`, `id="preview-error"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("editor page lacks %q:\n%s", want, body)
		}
	}
	// The action bar keeps the page name and the actions, without a description
	// of the file (ADR 0093).
	if strings.Contains(body, "shared by all display types") {
		t.Fatalf("the action bar still describes the file:\n%s", body)
	}
	// The board carries the preview itself, so the page editor has no separate
	// preview panel (ADR 0074); the widgets page keeps it.
	if strings.Contains(body, `id="preview-img"`) {
		t.Fatalf("the page editor still has a preview panel:\n%s", body)
	}

	// The page has no variant for the text display: it is not there for it.
	if rec := request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=lcd", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing variant: %d %s", rec.Code, rec.Body)
	}
	// Asked as a new page it starts with the kind and character grid of the display.
	body = request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=lcd&new=1", nil, nil).Body.String()
	if !strings.Contains(body, `"file":"lcd","kind":"text","width":20,"height":4,"new":true,"variant_exists":false,"grid":[20,4]`) {
		t.Fatalf("new text page:\n%s", body)
	}

	// A shared page says which file it is, so the editor writes the shared file
	// again.
	writePage(t, s, "shared", "", previewTemplate)
	if body := request(t, s, http.MethodGet, "/ui/editor/page?name=shared&display=lcd", nil, nil).Body.String(); !strings.Contains(body, `"file":""`) {
		t.Fatalf("shared page:\n%s", body)
	}
	// Without a display type the first configured one is used.
	if body := request(t, s, http.MethodGet, "/ui/editor/page?name=shared", nil, nil).Body.String(); !strings.Contains(body, `"display":"small"`) {
		t.Fatalf("default display:\n%s", body)
	}

	for _, path := range []string{
		"/ui/editor/page?name=Nope&display=small",            // invalid name
		"/ui/editor/page?name=solar&display=nope",            // unknown display type
		"/ui/editor/page?name=solar&display=small&file=Nope", // invalid file
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
	for _, want := range []string{"/api/pages/", "/api/plugins", "/api/preview/", "PUT", "pointerdown", "handle-xy", "firstWidget"} {
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
