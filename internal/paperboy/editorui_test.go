package paperboy

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maehld/paperboy/internal/page"
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
		`<select name="kind" id="new-kind">`, `<option value="text">text</option>`,
		`<label id="new-grid-label">Character grid <select name="display" id="new-grid">`,
		`20×4 (lcd)`,
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
		`<strong class="missing" title="No page file. Create it with New page.">broken</strong>`,
		`class="chip orphan" title="pages/broken.klein.yaml — no display type names it — yaml: unmarshal errors`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("overview lacks %q:\n%s", want, body)
		}
	}

	// A variant without a page file is not editable: the operator creates the
	// page file first (ADR 0112).
	writePage(t, s, "clock", "old", variantPage)
	body = request(t, s, http.MethodGet, "/ui/editor", nil, nil).Body.String()
	for _, want := range []string{
		`title="pages/clock.old.yaml — no display type names it"`,
		`<strong class="missing" title="No page file. Create it with New page.">clock</strong>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("overview lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `href="/ui/editor/page?display=small&amp;file=old&amp;name=clock"`) {
		t.Fatalf("a variant without a page file is linked:\n%s", body)
	}
}

// TestEditorMissingPageFile shows a page without a page file in red, without an
// assign action and without links to its variants (ADR 0112).
func TestEditorMissingPageFile(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "alt", "klein", variantPage)

	body := request(t, s, http.MethodGet, "/ui/editor", nil, nil).Body.String()
	for _, want := range []string{
		`<strong class="missing" title="No page file. Create it with New page.">alt</strong>`,
		`class="chip orphan" title="pages/alt.klein.yaml`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("overview lacks %q:\n%s", want, body)
		}
	}
	for _, unwanted := range []string{"Assign", "No page file. Assign a variant", `data-dialog="assign-alt"`, `<a href="/ui/editor/page?display=small&amp;file=klein&amp;name=alt">`} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("the row carries %q:\n%s", unwanted, body)
		}
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
		// A variant no display type names stays a candidate in the Variants
		// select, so it is only styled and offers no dialog of its own.
		`class="chip unused" title="pages/solar.lcd.yaml — no display type names it"`,
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
	// The page has a page file: the Variants select is the way to use the unused
	// variant, and no dialog competes with it (ADR 0112).
	if strings.Contains(body, `data-dialog="assign-solar"`) {
		t.Fatalf("the pages list still offers a dialog:\n%s", body)
	}

	// The display type in the link no longer changes the list.
	if other := request(t, s, http.MethodGet, "/ui/editor?display=lcd", nil, nil).Body.String(); other != body {
		t.Fatalf("the list follows a display type from the link:\n%s", other)
	}
}

// TestEditorServesTable lists every configured display type with the files of
// the page that fit it (ADR 0110). Only the file the editor shows gets a preview
// TestEditorVariantsPanel lists every display type of the page file with the
// file that serves it; a variant shows its page instead (ADR 0110).
func TestEditorVariantsPanel(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "", pageWithVariant)
	writePage(t, s, "solar", "hochkant", variantPage)
	writePage(t, s, "solar", "lcd", textVariant)

	// The page file owns the table: small is served by the variant, so the type
	// links to that file, the select names the variant and the preview belongs
	// to the file that serves the type. Nothing serves the LCD: the fitting
	// file is an option and the reason sits in the tooltip, not in a red block.
	body := request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=small", nil, nil).Body.String()
	for _, want := range []string{
		`<h3>Variants</h3>`,
		`<a href="/ui/editor/page?display=small&amp;file=hochkant&amp;name=solar" title="Edit pages/solar.hochkant.yaml for small">small</a>`,
		`<select data-serves="small" data-value="hochkant" title="pages/solar.hochkant.yaml" aria-label="File that serves small"><option value="">page file</option><option value="hochkant" selected>hochkant</option></select>`,
		`<option value="__none__" selected disabled>— none —</option><option value="lcd">lcd</option>`,
		`title="page is for image displays (kind: image)"`,
		// One dialog creates a variant; no table row carries its own button.
		`data-dialog="create-variant"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("page file lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `aria-current="true">small</a>`) {
		t.Fatalf("the page file is not the layout for small:\n%s", body)
	}

	// A type the open file serves is its preview link, marked as current.
	writePage(t, s, "clock", "", "title: Clock\nkind: text\ngrid: [20, 4]\ncells:\n  - { at: [0, 0], span: [20, 1], widget: paperboy/clock }\n")
	body = request(t, s, http.MethodGet, "/ui/editor/page?name=clock&display=lcd", nil, nil).Body.String()
	for _, want := range []string{
		`<a href="/ui/editor/page?display=lcd&amp;name=clock" aria-current="true" title="Preview lcd with this file">lcd</a>`,
		`<option value="" selected>page file</option>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("text page lacks %q:\n%s", want, body)
		}
	}

	// A variant is a child page: it names its page file and shows no table.
	body = request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=small&file=hochkant", nil, nil).Body.String()
	if !strings.Contains(body, `This variant belongs to <a href="/ui/editor/page?display=small&amp;name=solar">pages/solar.yaml</a>.`) {
		t.Fatalf("the variant misses its page link:\n%s", body)
	}
	for _, unwanted := range []string{`<h3>Variants</h3>`, `data-serves=`, `data-dialog="create-variant"`} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("the variant carries %q:\n%s", unwanted, body)
		}
	}

	// A broken file opens read-only instead of being unlinked.
	writePage(t, s, "broken", "klein", "nope: 1\n")
	if rec := request(t, s, http.MethodGet, "/ui/editor/page?name=broken&display=small&file=klein", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("broken file does not open read-only: %d %s", rec.Code, rec.Body)
	}
}

// TestEditorCreateVariantDialog offers the new variant in one dialog: every
// configured display type and a file label (ADR 0110).
func TestEditorCreateVariantDialog(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "", pageWithVariant)
	writePage(t, s, "solar", "hochkant", variantPage)

	body := request(t, s, http.MethodGet, "/ui/editor/page?name=solar&display=small", nil, nil).Body.String()
	for _, want := range []string{
		`<button type="button" data-dialog="create-variant">Create variant…</button>`,
		`<dialog id="create-variant">`,
		`<select name="display">`,
		`<option value="lcd" data-label="lcd">lcd</option>`,
		`<option value="small" data-label="small" title="served by pages/solar.hochkant.yaml">small</option>`,
		`<input name="label" required pattern="[a-z0-9-]{1,40}" maxlength="40" size="16" value="lcd"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("create dialog lacks %q:\n%s", want, body)
		}
	}

	// A type whose file is missing keeps the label of the entry, so the repaired
	// file is the one the page names again.
	writePage(t, s, "clock", "", `
title: Clock
kind: image
grid: [2, 2]
variants:
  small: quer
cells:
  - { at: [0, 0], span: [2, 2], widget: paperboy/header }
`)
	body = request(t, s, http.MethodGet, "/ui/editor/page?name=clock&display=small", nil, nil).Body.String()
	for _, want := range []string{
		`<option value="small" data-label="quer">small</option>`,
		`title="pages/clock.quer.yaml is missing"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing variant lacks %q:\n%s", want, body)
		}
	}
}

// TestEditorPreviewDefaultsToServedType opens a page without a display type on
// the first one the page file serves (ADR 0110).
func TestEditorPreviewDefaultsToServedType(t *testing.T) {
	cfg := &Config{Dir: t.TempDir(), Profiles: map[string]Profile{
		"bild":  {Width: 40, Height: 20, PPI: 125},
		"klein": {Width: 40, Height: 20, PPI: 125},
		"lcd":   {Kind: kindText, Charset: "ascii", Width: 20, Height: 4},
	}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	writePage(t, s, "solar", "", `
title: Solar
kind: image
grid: [2, 2]
cells:
  - { at: [0, 0], span: [2, 2], widget: paperboy/header }
`)
	body := request(t, s, http.MethodGet, "/ui/editor/page?name=solar", nil, nil).Body.String()
	if !strings.Contains(body, `"display":"bild"`) {
		t.Fatalf("the default preview is not the first served type:\n%s", body)
	}
	// Both image types are preview links of the open page file; the first one is
	// the current one.
	for _, want := range []string{
		`<a href="/ui/editor/page?display=bild&amp;name=solar" aria-current="true" title="Preview bild with this file">bild</a>`,
		`<a href="/ui/editor/page?display=klein&amp;name=solar" title="Preview klein with this file">klein</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("served types lack %q:\n%s", want, body)
		}
	}
}

// TestPageServesAPI changes the file of one display type (ADR 0110): a label
// points the type at its variant, an empty label removes the entry, and the
// page hash guards the write.
func TestPageServesAPI(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "", pageWithVariant)
	writePage(t, s, "solar", "hochkant", variantPage)
	writePage(t, s, "solar", "lcd", textVariant)

	var read editorPage
	if rec := request(t, s, http.MethodGet, "/api/pages/solar", nil, &read); rec.Code != http.StatusOK {
		t.Fatalf("read: %d %s", rec.Code, rec.Body)
	}

	// Point the text type at its own layout.
	var doc editorPage
	rec := request(t, s, http.MethodPost, "/api/pages/solar/serves?display=lcd", servesSave{Hash: read.Hash, File: "lcd"}, &doc)
	if rec.Code != http.StatusOK {
		t.Fatalf("set lcd: %d %s", rec.Code, rec.Body)
	}
	if doc.Variants["lcd"] != "lcd" {
		t.Fatalf("variants after the set: %+v", doc.Variants)
	}
	if c := coverageOf(t, doc, "lcd"); c.File != "lcd" || c.Missing || c.Error != "" {
		t.Fatalf("coverage of lcd: %+v", c)
	}

	// An empty label removes the entry: the page file serves the type again.
	var removed editorPage
	rec = request(t, s, http.MethodPost, "/api/pages/solar/serves?display=small", servesSave{Hash: doc.Hash}, &removed)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove small: %d %s", rec.Code, rec.Body)
	}
	if _, named := removed.Variants["small"]; named {
		t.Fatalf("the entry of small is still there: %+v", removed.Variants)
	}
	if c := coverageOf(t, removed, "small"); c.File != "" || c.Missing || c.Error != "" {
		t.Fatalf("the page file does not serve small: %+v", c)
	}

	for _, tc := range []struct {
		name, path string
		body       servesSave
		status     int
		want       string
	}{
		{"unknown label", "/api/pages/solar/serves?display=small", servesSave{Hash: removed.Hash, File: "nope"}, http.StatusBadRequest, "is missing"},
		{"wrong kind", "/api/pages/solar/serves?display=small", servesSave{Hash: removed.Hash, File: "lcd"}, http.StatusBadRequest, "text displays"},
		{"unknown display type", "/api/pages/solar/serves?display=nope", servesSave{Hash: removed.Hash}, http.StatusBadRequest, "unknown display type"},
		{"stale hash", "/api/pages/solar/serves?display=small", servesSave{Hash: "stale"}, http.StatusConflict, "changed on disk"},
	} {
		rec := request(t, s, http.MethodPost, tc.path, tc.body, nil)
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("%s: %d %s", tc.name, rec.Code, rec.Body)
		}
	}
}

// TestCreateVariantLabel writes a new layout under the label the dialog asked
// for and adds its entry (ADR 0110).
func TestCreateVariantLabel(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "", pageWithVariant)

	tmpl, err := page.ParseVariant(strings.NewReader(variantPage))
	if err != nil {
		t.Fatal(err)
	}
	var doc editorPage
	rec := request(t, s, http.MethodPost, "/api/pages/solar/variant?display=small&file=quer", pageSave{Template: tmpl}, &doc)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if doc.File != "quer" || doc.Variants["small"] != "quer" {
		t.Fatalf("created file: %+v", doc)
	}
	if _, err := os.Stat(filepath.Join(s.config().Pages, "solar.quer.yaml")); err != nil {
		t.Fatalf("the layout file is missing: %v", err)
	}
	// A type that a file already serves is changed in the editor, not created
	// again.
	if rec := request(t, s, http.MethodPost, "/api/pages/solar/variant?display=small", pageSave{Template: tmpl}, nil); rec.Code != http.StatusConflict {
		t.Fatalf("second layout for small: %d %s", rec.Code, rec.Body)
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
		// The preview type is the served one; the file is not a link there.
		`data-serves="small"`,
		`"rename":{"url":"/api/pages/solar/rename","warn":"Rename page solar?`,
		`<script src="/ui/editor.js"></script>`, `id="board"`, `id="palette"`, `id="palette-filter"`, `id="inspector"`,
		`id="show-cells" checked`, `id="render-hint"`, `id="preview-error"`,
		// The panels list the files of the page and the file of every display
		// type; one dialog creates a layout (ADR 0110).
		`<h3>Variants</h3>`, `pages/solar.hochkant.yaml is missing`,
		`pages/solar.hochkant.yaml is missing`, `data-dialog="create-variant"`, `title-note`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("editor page lacks %q:\n%s", want, body)
		}
	}
	// The page editor has no display type switch and no coverage table with an
	// action per row (ADR 0110).
	for _, unwanted := range []string{"id=\"display-switch\"", "Display type:", `id="file-toggle"`, `id="specialize"`, "Save as ", "data-variant="} {
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
// with the kind the dialog asks for; a text page takes its character grid from
// a text display type (ADR 0110, issue #238).
func TestEditorNewPageCreatesFromType(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "wetter", "hochkant", variantPage)

	// A new text page next to a variant of another kind starts with the
	// character grid of the chosen text display type.
	body := request(t, s, http.MethodGet, "/ui/editor/page?name=wetter&kind=text&display=lcd&new=1", nil, nil).Body.String()
	if !strings.Contains(body, `"file":"","kind":"text","width":20,"height":4,"new":true,"grid":[20,4]`) {
		t.Fatalf("new text page next to a variant:\n%s", body)
	}
	// Without a chosen grid it takes the first configured text type.
	body = request(t, s, http.MethodGet, "/ui/editor/page?name=wetter&kind=text&new=1", nil, nil).Body.String()
	if !strings.Contains(body, `"kind":"text"`) || !strings.Contains(body, `"grid":[20,4]`) {
		t.Fatalf("new text page without a chosen grid:\n%s", body)
	}
	// A new image page is its own file as well, whatever display type a stale
	// link names.
	body = request(t, s, http.MethodGet, "/ui/editor/page?name=wetter&kind=image&display=lcd&new=1", nil, nil).Body.String()
	if !strings.Contains(body, `"file":"","kind":"image","width":40,"height":20,"new":true`) {
		t.Fatalf("new image page next to a variant:\n%s", body)
	}
	// An unknown kind is refused.
	if rec := request(t, s, http.MethodGet, "/ui/editor/page?name=wetter&kind=led&new=1", nil, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown kind: %d %s", rec.Code, rec.Body)
	}
	// The old link without a kind still derives it from the display type.
	body = request(t, s, http.MethodGet, "/ui/editor/page?name=wetter&display=lcd&new=1", nil, nil).Body.String()
	if !strings.Contains(body, `"kind":"text"`) {
		t.Fatalf("new page from a display type:\n%s", body)
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
