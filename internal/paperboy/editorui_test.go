package paperboy

import (
	"net/http"
	"strings"
	"testing"
)

// TestEditorOverview lists one row per page with its variant files as chips
// (ADR 0110): the path and the display types a file serves are in the tooltip.
func TestEditorOverview(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "", pageWithVariant)
	writePage(t, s, "solar", "hochkant", variantPage)

	rec := request(t, s, http.MethodGet, "/ui/editor", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("editor: %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<strong>solar</strong>`,
		// The row opens the page file; the preview type is one the file serves.
		`class="rowlink" data-href="/ui/editor/page?display=small&amp;name=solar"`,
		`title="pages/solar.hochkant.yaml — serves small"`,
		`<a href="/ui/editor/page?display=small&amp;file=hochkant&amp;name=solar">hochkant</a>`,
		"New page",
		`data-dialog="new-page"`, `id="new-page"`, `action="/ui/editor/page"`,
		`href="/ui/editor?tab=pages" class="active">Pages<span class="count">1</span>`,
		`href="/ui/editor?tab=plans">Plans<span class="count">`,
		"<a href=\"/ui/downloads\">Downloads</a> · <a href=\"/ui/plugins\">Widgets</a> · <a class=\"active\" href=\"/ui/editor\">Editor",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("overview lacks %q:\n%s", want, body)
		}
	}
	// The list has no display type switch, no shared label and no action of its
	// own: the row and the chips are the way in (ADR 0110).
	for _, unwanted := range []string{"Display type:", "data-delete=", ">shared<", "shared page"} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("the pages tab still carries %q:\n%s", unwanted, body)
		}
	}
	// The plans and the device files are no longer stacked below the pages.
	if strings.Contains(body, "A plan is <code>plans/") {
		t.Fatalf("the pages tab carries the plan list:\n%s", body)
	}

	// A broken file is listed with its error but still opens read-only in the
	// editor.
	writePage(t, s, "broken", "klein", "kind: image\nnope: 1\n")
	body = request(t, s, http.MethodGet, "/ui/editor", nil, nil).Body.String()
	for _, want := range []string{
		`<strong>broken</strong>`,
		`class="chip orphan" title="pages/broken.klein.yaml — no display type names it — yaml: unmarshal errors`,
		"Assign",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("overview lacks %q:\n%s", want, body)
		}
	}

	// A file whose display type is gone still opens through a configured type
	// of its kind, so it stays editable.
	writePage(t, s, "clock", "old", variantPage)
	body = request(t, s, http.MethodGet, "/ui/editor", nil, nil).Body.String()
	for _, want := range []string{
		`title="pages/clock.old.yaml — no display type names it"`,
		`href="/ui/editor/page?display=small&amp;file=old&amp;name=clock"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("overview lacks %q:\n%s", want, body)
		}
	}
}

// TestEditorAssignDialog lists a file without a page as an orphan and offers the
// assign dialog (ADR 0110).
func TestEditorAssignDialog(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "alt", "klein", variantPage)

	body := request(t, s, http.MethodGet, "/ui/editor", nil, nil).Body.String()
	for _, want := range []string{
		`<strong>alt</strong>`,
		"No page file. Assign a variant to become it.",
		`class="chip orphan" title="pages/alt.klein.yaml`,
		`data-dialog="assign-alt"`,
		`<dialog id="assign-alt">`,
		`data-assign data-source="alt"`,
		`<option value="klein">pages/alt.klein.yaml</option>`,
		`<option value="">make it the page file</option>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("overview lacks %q:\n%s", want, body)
		}
	}
	// The page itself is not offered: it has no page file to open.
	if strings.Contains(body, `data-href="/ui/editor/page?display=small&amp;file=klein&amp;name=alt"`) {
		t.Fatalf("the row is not a link to the variant:\n%s", body)
	}
}

// TestEditorOrphanVariant opens a variant whose page file is missing; the file
// stays editable and the editor names the missing page (ADR 0110).
func TestEditorOrphanVariant(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "hochkant", variantPage)

	rec := request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=small&file=hochkant", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("orphan variant: %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"file":"hochkant"`,
		`"orphan":"hochkant"`,
		`"page_exists":false`,
		"This file belongs to no page.",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("orphan variant lacks %q:\n%s", want, body)
		}
	}

	// A variant file that does not exist is not opened as a new one: the
	// coverage creates it from the display type (issue #238).
	if rec := request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=small&file=nope", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing variant: %d %s", rec.Code, rec.Body)
	}
	if rec := request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=small&file=nope&new=1", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing variant as new: %d %s", rec.Code, rec.Body)
	}
}

// TestEditorPageRows shows one row per page and names the display types a file
// serves (ADR 0110). A page whose page file exists opens it, not its variants.
func TestEditorPageRows(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "", pageWithVariant)
	writePage(t, s, "solar", "hochkant", variantPage)
	writePage(t, s, "solar", "lcd", textVariant)
	writePage(t, s, "clock", "", "title: Clock\nkind: text\ngrid: [20, 4]\ncells:\n  - { at: [0, 0], span: [20, 1], widget: paperboy/clock }\n")

	body := request(t, s, http.MethodGet, "/ui/editor", nil, nil).Body.String()
	for _, want := range []string{
		// The page file wins the row; the preview type is a display type it
		// serves for each file.
		`class="rowlink" data-href="/ui/editor/page?display=small&amp;name=solar"`,
		`title="pages/solar.hochkant.yaml — serves small"`,
		`title="pages/solar.lcd.yaml — no display type names it"`,
		// A page file of the text kind opens for the text type.
		`class="rowlink" data-href="/ui/editor/page?display=lcd&amp;name=clock"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("pages tab lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "data-delete=") {
		t.Fatalf("the pages tab still acts on files:\n%s", body)
	}

	// The display type in the link no longer changes the list.
	if other := request(t, s, http.MethodGet, "/ui/editor?display=lcd", nil, nil).Body.String(); other != body {
		t.Fatalf("the list follows a display type from the link:\n%s", other)
	}
}

// TestEditorServedByAnotherFile warns when the preview type is served by
// another file of the page, and offers to open that file. The editor never
// renders another file in place of the open one (ADR 0110).
func TestEditorServedByAnotherFile(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "", pageWithVariant)
	writePage(t, s, "solar", "hochkant", variantPage)

	// The page file is open while small is served by the variant.
	body := request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=small", nil, nil).Body.String()
	for _, want := range []string{
		`"file":""`,
		`Display type &#34;small&#34; is served by pages/solar.hochkant.yaml; this file is not used for it`,
		`<a href="/ui/editor/page?display=small&amp;file=hochkant&amp;name=solar">Open pages/solar.hochkant.yaml</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("page file lacks %q:\n%s", want, body)
		}
	}
	// The variant is the serving file: no warning.
	body = request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=small&file=hochkant", nil, nil).Body.String()
	if strings.Contains(body, `id="file-warning"`) {
		t.Fatalf("the serving file warns:\n%s", body)
	}
	// A preview type the file cannot render names the reason, and the variant
	// previews the page for the text type it does not serve.
	writePage(t, s, "solar", "lcd", textVariant)
	body = request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=small&file=lcd", nil, nil).Body.String()
	for _, want := range []string{
		`This file cannot render display type &#34;small&#34;`,
		`Open pages/solar.hochkant.yaml`,
		// The selector lists the types the open file can render.
		`data-display="lcd"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("text variant lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `data-display="small"`) {
		t.Fatalf("the selector offers a type the text variant cannot render:\n%s", body)
	}

	// A broken file opens read-only instead of being unlinked.
	writePage(t, s, "broken", "klein", "nope: 1\n")
	if rec := request(t, s, http.MethodGet, "/ui/editor/page?name=broken&display=small&file=klein", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("broken file does not open read-only: %d %s", rec.Code, rec.Body)
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
	if !strings.Contains(body, `class="rowlink" data-href="/ui/editor/page?display=small&amp;name=solar"`) {
		t.Fatalf("pages tab lacks the row link:\n%s", body)
	}

	// The page editor knows the plans that name its page, the plan editor its
	// devices, the display type editor the pages that name it.
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

// TestEditorPage opens one page file with a preview display type and starts a
// new page from the type (ADR 0110).
func TestEditorPage(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "", pageWithVariant)

	rec := request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=small", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("editor page: %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"name":"solar","display":"small","file":"","kind":"image","width":40,"height":20,"new":false,"grid":[2,2],"reference":[6,8],"page_hash":`,
		`"page_exists":true`,
		`"displays":[{"name":"lcd","kind":"text","width":20,"height":4},{"name":"small","kind":"image","width":40,"height":20}]`,
		// The preview selector offers the types the image file can render.
		`data-display="small"`,
		`"rename":{"url":"/api/pages/solar/rename","warn":"Rename page solar?`,
		`<script src="/ui/editor.js"></script>`, `id="board"`, `id="palette"`, `id="palette-filter"`, `id="inspector"`,
		`id="show-cells" checked`, `id="render-hint"`, `id="preview-error"`,
		// The coverage names the file that serves every configured type, and it
		// offers a layout for a type that no file serves (ADR 0110).
		`<h3>Coverage</h3>`, `pages/solar.yaml`, `pages/solar.hochkant.yaml`,
		`pages/solar.hochkant.yaml is missing`, `data-variant="small"`, `title-note`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("editor page lacks %q:\n%s", want, body)
		}
	}
	// The page editor shows the preview type, not a display type switch, and it
	// has no file toggle or "Save as variant" any more (ADR 0110).
	for _, unwanted := range []string{"Display type:", `id="file-toggle"`, `id="specialize"`, "Save as "} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("the page editor still carries %q:\n%s", unwanted, body)
		}
	}
	// The board carries the preview itself, so the page editor has no separate
	// preview panel (ADR 0074); the widgets page keeps it.
	if strings.Contains(body, `id="preview-img"`) {
		t.Fatalf("the page editor still has a preview panel:\n%s", body)
	}

	// A new page starts with the kind and the character grid of the display
	// type, and its state names the page file the first save writes.
	body = request(t, s, http.MethodGet, "/ui/editor/page?name=neu&display=lcd&new=1", nil, nil).Body.String()
	if !strings.Contains(body, `"name":"neu","display":"lcd","file":"","kind":"text","width":20,"height":4,"new":true,"grid":[20,4]`) {
		t.Fatalf("new text page:\n%s", body)
	}

	// Without a display type the preview is one the file can render; a text
	// page opens for the text type.
	writePage(t, s, "clock", "", textVariant)
	if body := request(t, s, http.MethodGet, "/ui/editor/page?name=clock", nil, nil).Body.String(); !strings.Contains(body, `"display":"lcd"`) {
		t.Fatalf("default preview of a text page:\n%s", body)
	}

	for _, path := range []string{
		"/ui/editor/page?name=Nope&display=small",            // invalid name
		"/ui/editor/page?name=solar&display=nope",            // unknown display type
		"/ui/editor/page?name=solar&display=small&file=No",   // invalid file
		"/ui/editor/page?name=solar&display=small&file=nope", // missing variant
		"/ui/editor/page?name=..%2Fx&display=small",
	} {
		if rec := request(t, s, http.MethodGet, path, nil, nil); rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
	if rec := request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=nope", nil, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown display type: %d %s", rec.Code, rec.Body)
	}
	if rec := request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=small&file=nope", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing variant: %d %s", rec.Code, rec.Body)
	}
}

// TestEditorNewPageCreatesFromType starts a new page as exactly the page file
// with the kind and grid of the display type; other files of the page never
// open in its place (ADR 0110, issue #238).
func TestEditorNewPageCreatesFromType(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "wetter", "hochkant", variantPage)

	// A new text page next to a variant of another kind starts with the
	// character grid of the text display.
	body := request(t, s, http.MethodGet, "/ui/editor/page?name=wetter&display=lcd&new=1", nil, nil).Body.String()
	if !strings.Contains(body, `"file":"","kind":"text","width":20,"height":4,"new":true,"grid":[20,4]`) {
		t.Fatalf("new text page next to a variant:\n%s", body)
	}
	// A new image page is its own file as well.
	body = request(t, s, http.MethodGet, "/ui/editor/page?name=wetter&display=small&new=1", nil, nil).Body.String()
	if !strings.Contains(body, `"file":"","kind":"image","width":40,"height":20,"new":true`) {
		t.Fatalf("new image page next to a variant:\n%s", body)
	}
	// A page file that exists opens as it is, &new=1 or not.
	writePage(t, s, "solar", "", pageWithVariant)
	for _, path := range []string{"/ui/editor/page?name=solar&display=small", "/ui/editor/page?name=solar&display=small&new=1"} {
		if body := request(t, s, http.MethodGet, path, nil, nil).Body.String(); !strings.Contains(body, `"new":false`) {
			t.Fatalf("%s does not open the existing file:\n%s", path, body)
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
