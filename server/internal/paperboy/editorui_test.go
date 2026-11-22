package paperboy

import (
	"bytes"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maehld/paperboy/server/internal/page"
)

// TestEditorOverview lists one row per page with its variant files as chips
// (ADR 0110): the path and the display types a file serves are in the tooltip.
func TestEditorOverview(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "", pageWithVariant)
	writePage(t, s, "solar", "hochkant", variantPage)

	rec := request(t, s, http.MethodGet, "/ui/pages", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("editor: %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<td class="page name" data-label="Page"><a href="/ui/pages/solar?display=small">solar</a></td>`,
		// The row opens the page file; the preview type is one the file serves.
		`class="rowlink" data-href="/ui/pages/solar?display=small"`,
		`title="pages/solar.hochkant.yaml — serves small"`,
		`<a href="/ui/pages/solar?display=small&amp;file=hochkant">hochkant</a>`,
		"New page",
		`data-dialog="new-page"`, `id="new-page"`, `action="/ui/pages/new"`,
		`<select name="kind" id="new-kind">`, `<option value="text">text</option>`,
		`<label id="new-grid-label">Character grid <select name="display" id="new-grid">`,
		`20×4 (lcd)`,
		`<a href="/ui/pages" class="active" aria-current="page">Pages</a>`,
		`<a href="/ui/plans">Plans</a>`,
		`<a href="/ui/devices">Devices</a>`,
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
	body = request(t, s, http.MethodGet, "/ui/pages", nil, nil).Body.String()
	for _, want := range []string{
		`<span class="missing" title="No page file. Create it with New page.">broken</span>`,
		`class="chip orphan" title="pages/broken.klein.yaml — no display type names it — yaml: unmarshal errors`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("overview lacks %q:\n%s", want, body)
		}
	}

	// A variant without a page file is not editable: the operator creates the
	// page file first (ADR 0112).
	writePage(t, s, "clock", "old", variantPage)
	body = request(t, s, http.MethodGet, "/ui/pages", nil, nil).Body.String()
	for _, want := range []string{
		`title="pages/clock.old.yaml — no display type names it"`,
		`<span class="missing" title="No page file. Create it with New page.">clock</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("overview lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `href="/ui/pages/clock?display=small&amp;file=old"`) {
		t.Fatalf("a variant without a page file is linked:\n%s", body)
	}
}

// TestEditorMissingPageFile shows a page without a page file in red, without an
// assign action and without links to its variants (ADR 0112).
func TestEditorMissingPageFile(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "alt", "klein", variantPage)

	body := request(t, s, http.MethodGet, "/ui/pages", nil, nil).Body.String()
	for _, want := range []string{
		`<span class="missing" title="No page file. Create it with New page.">alt</span>`,
		`class="chip orphan" title="pages/alt.klein.yaml`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("overview lacks %q:\n%s", want, body)
		}
	}
	for _, unwanted := range []string{"Assign", "No page file. Assign a variant", `data-dialog="assign-alt"`, `<a href="/ui/pages/alt?display=small&amp;file=klein">`} {
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

	rec := request(t, s, http.MethodGet, "/ui/pages/solar?display=small&file=hochkant", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("orphan variant: %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"file":"hochkant"`,
		`"orphan":"hochkant"`,
		`"page_exists":false`,
		`<p class="page-parent missing">no page file</p>`,
		"This file belongs to no page.",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("orphan variant lacks %q:\n%s", want, body)
		}
	}

	// A variant file that does not exist is not opened as a new one: the
	// coverage creates it from the display type (issue #238).
	if rec := request(t, s, http.MethodGet, "/ui/pages/solar?display=small&file=nope", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing variant: %d %s", rec.Code, rec.Body)
	}
	if rec := request(t, s, http.MethodGet, "/ui/pages/solar?display=small&file=nope&new=1", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing variant as new: %d %s", rec.Code, rec.Body)
	}
}

// TestEditorTextRows opens a text page with rows: the editor gets the rows,
// the list counts them and the no-JS view renders their fields (ADR 0111).
func TestEditorTextRows(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "werte", "", textVariant)

	var doc editorPage
	if rec := request(t, s, http.MethodGet, "/api/pages/werte", nil, &doc); rec.Code != http.StatusOK {
		t.Fatalf("read: %d %s", rec.Code, rec.Body)
	}
	if doc.Template == nil || doc.Template.Kind != page.KindText || len(doc.Template.Cells) != 4 ||
		doc.Template.Cells[0].Value != "paperboy/title" || doc.Template.Cells[0].Span[0] != 20 {
		t.Fatalf("text page cells: %+v", doc.Template)
	}
	body := request(t, s, http.MethodGet, "/ui/pages/werte?display=lcd", nil, nil).Body.String()
	for _, want := range []string{
		`"kind":"text"`,
		`<noscript>`,
		`<h3>Text cells</h3>`,
		`value="paperboy/title" readonly`,
		`value="0,0" readonly`,
		`value="20×1" readonly`,
		`.cell.text`,
		`.drop-marker`,
		`Filter values…`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("text page editor lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "board-caption") {
		t.Fatal("the editor still carries a caption below the board")
	}
	list := request(t, s, http.MethodGet, "/ui/pages", nil, nil).Body.String()
	if !strings.Contains(list, `<td class="num" data-label="Grid">4 rows</td>`) {
		t.Fatalf("the pages list does not count the rows:\n%s", list)
	}
	for _, want := range []string{"renderTextInspector", "textCellFields", "renderValuePalette",
		"placeTextAt", "pickTextValue", "pickTextLiteral", "startPaletteDrag", "startDrag",
		"cellAtPointer", "setPointerCapture", "showDropMarker", "drop-marker", "removeCell"} {
		if !strings.Contains(string(editorJS), want) {
			t.Fatalf("the editor script lacks %q", want)
		}
	}
}

// TestEditorPageRows shows one row per page and names the display types a file
// serves (ADR 0110). A page whose page file exists opens it, not its variants.
func TestEditorPageRows(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "", pageWithVariant)
	writePage(t, s, "solar", "hochkant", variantPage)
	writePage(t, s, "solar", "lcd", textVariant)
	writePage(t, s, "clock", "", "title: Clock\nkind: text\ncells:\n  - { at: [0, 0], span: [20, 1], value: paperboy/title }\n  - { at: [0, 1], span: [20, 1], value: paperboy/time }\n")

	body := request(t, s, http.MethodGet, "/ui/pages", nil, nil).Body.String()
	for _, want := range []string{
		// The page file wins the row; the preview type is a display type it
		// serves for each file.
		`class="rowlink" data-href="/ui/pages/solar?display=small"`,
		`title="pages/solar.hochkant.yaml — serves small"`,
		`title="pages/solar.lcd.yaml — no display type names it"`,
		// A variant no display type names stays a candidate in the Variants
		// select, so it is only styled and offers no dialog of its own.
		`class="chip unused" title="pages/solar.lcd.yaml — no display type names it"`,
		// A page file of the text kind opens for the text type.
		`class="rowlink" data-href="/ui/pages/clock?display=lcd"`,
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
	if other := request(t, s, http.MethodGet, "/ui/pages?display=lcd", nil, nil).Body.String(); other != body {
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
	body := request(t, s, http.MethodGet, "/ui/pages/solar?display=small", nil, nil).Body.String()
	for _, want := range []string{
		`<h3>Variants</h3>`,
		`<a href="/ui/pages/solar?display=small&amp;file=hochkant" title="Edit pages/solar.hochkant.yaml for small">small</a>`,
		`<select data-serves="small" data-value="hochkant" title="pages/solar.hochkant.yaml" aria-label="File that serves small"><option value="">page file</option><option value="hochkant" selected>hochkant</option></select>`,
		`<option value="lcd">lcd</option><option value="__none__" selected>— none —</option>`,
		`<select class="none" data-serves="lcd" data-value="__none__" title="page is for image displays (kind: image)"`,
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
	writePage(t, s, "clock", "", "title: Clock\nkind: text\ncells:\n  - { at: [0, 0], span: [20, 1], value: paperboy/title }\n  - { at: [0, 1], span: [20, 1], value: paperboy/time }\n")
	body = request(t, s, http.MethodGet, "/ui/pages/clock?display=lcd", nil, nil).Body.String()
	for _, want := range []string{
		`<a href="/ui/pages/clock?display=lcd" aria-current="true" title="Preview lcd with this file">lcd</a>`,
		`<option value="" selected>page file</option>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("text page lacks %q:\n%s", want, body)
		}
	}

	// A variant is a child page: it names its page file and shows no table.
	body = request(t, s, http.MethodGet, "/ui/pages/solar?display=small&file=hochkant", nil, nil).Body.String()
	for _, want := range []string{
		`"page_title":"Solar"`,
		`<h3>Variant</h3>`,
		`<p class="page-parent"><a href="/ui/pages/solar?display=small" title="pages/solar.yaml">Solar</a></p>`,
		`<p class="types-label">Assigned display types</p>`,
		`<ul class="variant-types"><li><a href="/ui/pages/solar?display=small&amp;file=hochkant" class="active" aria-current="true">small</a></li></ul>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the variant misses %q:\n%s", want, body)
		}
	}
	for _, unwanted := range []string{`<h3>Variants</h3>`, `data-serves=`, `data-dialog="create-variant"`, `This is a variant for`} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("the variant carries %q:\n%s", unwanted, body)
		}
	}
	// The page file is the parent: it carries no back link to itself.
	if page := request(t, s, http.MethodGet, "/ui/pages/solar?display=small", nil, nil).Body.String(); strings.Contains(page, `class="page-parent"`) {
		t.Fatalf("the page file carries a parent link:\n%s", page)
	}

	// A variant that no display type names warns that no device shows it; the
	// notice about a preview type is for pages, not for it.
	body = request(t, s, http.MethodGet, "/ui/pages/solar?display=lcd&file=lcd", nil, nil).Body.String()
	for _, want := range []string{
		`class="warn" id="file-notice">This variant is unused. It is not shown on any device.`,
		`<p class="hint">none</p>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("unused variant lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "is served by") {
		t.Fatalf("the unused variant carries the page notice:\n%s", body)
	}

	// A broken file opens read-only instead of being unlinked.
	writePage(t, s, "broken", "klein", "nope: 1\n")
	if rec := request(t, s, http.MethodGet, "/ui/pages/broken?display=small&file=klein", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("broken file does not open read-only: %d %s", rec.Code, rec.Body)
	}
}

// TestEditorUnusedEntry keeps the row of an entry whose display type is gone,
// so the select can drop it (ADR 0110).
func TestEditorUnusedEntry(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "", "title: Solar\nkind: image\ngrid: [2, 2]\nvariants:\n  gone: alt\ncells:\n  - { at: [0, 0], span: [2, 2], widget: paperboy/text }\n")
	writePage(t, s, "solar", "alt", variantPage)

	body := request(t, s, http.MethodGet, "/ui/pages/solar?display=small", nil, nil).Body.String()
	for _, want := range []string{
		// The row names the type and the kind of the file that is still there.
		`<td class="type">gone</td>`,
		`<span class="kind">image</span>`,
		`<select class="none" data-serves="gone" data-value="alt" title="pages/solar.alt.yaml"`,
		`<option value="alt" selected>alt</option><option value="__none__">— none —</option>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("editor lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Unused entries") {
		t.Fatalf("the editor carries a second list:\n%s", body)
	}

	var read editorPage
	request(t, s, http.MethodGet, "/api/pages/solar", nil, &read)
	// A file cannot be assigned to a type that is not configured.
	if rec := request(t, s, http.MethodPost, "/api/pages/solar/serves?display=gone",
		servesSave{Hash: read.Hash, File: "alt"}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("assign to an unknown type: %d %s", rec.Code, rec.Body)
	}
	// Its entry can be removed.
	if rec := request(t, s, http.MethodPost, "/api/pages/solar/serves?display=gone",
		servesSave{Hash: read.Hash}, nil); rec.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body)
	}
	var after editorPage
	request(t, s, http.MethodGet, "/api/pages/solar", nil, &after)
	if _, ok := after.Variants["gone"]; ok {
		t.Fatalf("the entry survived: %+v", after.Variants)
	}
}

// TestEditorVariantActions offers both actions of a variant in its block: a new
// label and another page. The page rename stays with the page file, so a view
// carries one Rename (ADR 0079, 0110).
func TestEditorVariantActions(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "", pageWithVariant)
	writePage(t, s, "solar", "hochkant", variantPage)
	writePage(t, s, "ziel", "", "title: Ziel\nkind: image\ngrid: [2, 2]\ncells:\n  - { at: [0, 0], span: [2, 2], widget: paperboy/header }\n")

	body := request(t, s, http.MethodGet, "/ui/pages/solar?display=small&file=hochkant", nil, nil).Body.String()
	for _, want := range []string{
		`<button type="button" data-dialog="rename-variant">Rename…</button>`,
		`<button type="button" data-dialog="reassign">Reassign…</button>`,
		`<dialog id="rename-variant">`,
		`<form data-rename-variant>`,
		`<h3>Rename variant</h3>`,
		`<input name="label" required pattern="[a-z0-9-]{1,40}" maxlength="40" size="16" value="hochkant"`,
		`<dialog id="reassign">`,
		`<form data-reassign>`,
		`<select name="page"><option value="ziel">ziel · Ziel</option></select>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the variant lacks %q:\n%s", want, body)
		}
	}
	// The bar keeps no page rename on a variant: its own action renames the
	// label, and the page is renamed from its page file.
	if strings.Contains(body, `"rename":`) {
		t.Fatalf("the variant offers a page rename:\n%s", body)
	}
	// The page file offers no variant action of its own.
	body = request(t, s, http.MethodGet, "/ui/pages/solar?display=small", nil, nil).Body.String()
	for _, unwanted := range []string{`data-dialog="rename-variant"`, `<dialog id="rename-variant">`, `data-dialog="reassign"`, `form data-reassign`} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("the page file carries %q:\n%s", unwanted, body)
		}
	}
	if !strings.Contains(body, `"rename":{"url":"/api/pages/solar/rename"`) {
		t.Fatalf("the page file lost its rename:\n%s", body)
	}

	// Only a page without a page file stays read-only: its variant offers no
	// action at all (ADR 0112).
	writePage(t, s, "lost", "klein", variantPage)
	body = request(t, s, http.MethodGet, "/ui/pages/lost?display=small&file=klein", nil, nil).Body.String()
	for _, unwanted := range []string{`data-dialog="rename-variant"`, `data-dialog="reassign"`, `<dialog id="reassign">`} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("an orphan variant carries %q:\n%s", unwanted, body)
		}
	}

	// Without another page file the Reassign button waits for one.
	only := editorServer(t)
	writePage(t, only, "clock", "", "title: Clock\nkind: image\ngrid: [2, 2]\ncells:\n  - { at: [0, 0], span: [2, 2], widget: paperboy/header }\n")
	writePage(t, only, "clock", "klein", variantPage)
	body = request(t, only, http.MethodGet, "/ui/pages/clock?display=small&file=klein", nil, nil).Body.String()
	for _, want := range []string{
		`data-dialog="reassign" disabled title="No other page has a page file yet"`,
		"No other page has a page file yet; create one with New page first.",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the only variant of the instance lacks %q:\n%s", want, body)
		}
	}
}

// TestEditorCreateVariantDialog offers the new variant in one dialog: every
// configured display type and a file label (ADR 0110).
func TestEditorCreateVariantDialog(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "", pageWithVariant)
	writePage(t, s, "solar", "hochkant", variantPage)

	body := request(t, s, http.MethodGet, "/ui/pages/solar?display=small", nil, nil).Body.String()
	for _, want := range []string{
		`<button type="button" data-dialog="create-variant">Create variant…</button>`,
		`<dialog id="create-variant">`,
		`<select name="display">`,
		`<option value="lcd" data-label="lcd" selected>lcd</option>`,
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
	body = request(t, s, http.MethodGet, "/ui/pages/clock?display=small", nil, nil).Body.String()
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
	body := request(t, s, http.MethodGet, "/ui/pages/solar", nil, nil).Body.String()
	if !strings.Contains(body, `"display":"bild"`) {
		t.Fatalf("the default preview is not the first served type:\n%s", body)
	}
	// Both image types are preview links of the open page file; the first one is
	// the current one.
	for _, want := range []string{
		`<a href="/ui/pages/solar?display=bild" aria-current="true" title="Preview bild with this file">bild</a>`,
		`<a href="/ui/pages/solar?display=klein" title="Preview klein with this file">klein</a>`,
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
		{"unknown display type", "/api/pages/solar/serves?display=nope", servesSave{Hash: removed.Hash, File: "lcd"}, http.StatusBadRequest, "unknown display type"},
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
		path := map[string]string{
			"plans":    "/ui/plans",
			"devices":  "/ui/devices",
			"displays": "/ui/display-types",
		}[name]
		if path == "" {
			path = "/ui/pages"
		}
		return request(t, s, http.MethodGet, path, nil, nil).Body.String()
	}
	plans := tab("plans")
	if !strings.Contains(plans, `id="plan-table"`) || !strings.Contains(plans, `<a href="/ui/plans/zimmer">zimmer</a>`) {
		t.Fatalf("plans tab:\n%s", plans)
	}
	if strings.Contains(plans, `id="page-table"`) {
		t.Fatalf("plans tab carries the page list:\n%s", plans)
	}
	devices := tab("devices")
	if !strings.Contains(devices, `id="device-table"`) || !strings.Contains(devices, `<a href="/ui/devices/wohnzimmer">wohnzimmer</a>`) ||
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

	plans := request(t, s, http.MethodGet, "/ui/plans", nil, nil).Body.String()
	for _, want := range []string{
		"New plan", `action="/ui/plans/new"`,
		`class="rowlink" data-href="/ui/plans/tour"`,
	} {
		if !strings.Contains(plans, want) {
			t.Fatalf("plans tab lacks %q:\n%s", want, plans)
		}
	}
	if strings.Contains(plans, "data-delete=") || strings.Contains(plans, "data-rename=") {
		t.Fatalf("the plans tab acts on files:\n%s", plans)
	}
	body := request(t, s, http.MethodGet, "/ui/pages?display=small", nil, nil).Body.String()
	if !strings.Contains(body, `class="rowlink" data-href="/ui/pages/solar?display=small"`) {
		t.Fatalf("pages tab lacks the row link:\n%s", body)
	}

	// The page editor knows the plans that name its page, the plan editor its
	// devices, the display type editor the pages that name it.
	page := request(t, s, http.MethodGet, "/ui/pages/solar?display=small", nil, nil).Body.String()
	if !strings.Contains(page, `"usedBy":["tour"]`) {
		t.Fatalf("page editor warning:\n%s", page)
	}
	plan := request(t, s, http.MethodGet, "/ui/plans/tour", nil, nil).Body.String()
	if !strings.Contains(plan, `"devices":[{"id":"test","display":"small"}]`) {
		t.Fatalf("plan editor devices:\n%s", plan)
	}
	display := request(t, s, http.MethodGet, "/ui/display-types/small?new=1", nil, nil).Body.String()
	if !strings.Contains(display, `"pages":["solar"]`) || !strings.Contains(display, `id="delete"`) {
		t.Fatalf("display editor warning:\n%s", display)
	}
}

// TestEditorPage opens one page file with a preview display type and starts a
// new page from the type (ADR 0110).
func TestEditorPage(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "", pageWithVariant)

	rec := request(t, s, http.MethodGet, "/ui/pages/solar?display=small", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("editor page: %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"name":"solar","display":"small","file":"","kind":"image","width":40,"height":20,"new":false,"grid":[2,2],"reference":[6,8],"page_hash":`,
		`"page_exists":true`,
		`"displays":[{"name":"lcd","kind":"text","width":20,"height":4,"scroll_step":300},{"name":"small","kind":"image","width":40,"height":20,"grid":[6,8]}]`,
		// The preview type is the served one; the file is not a link there.
		`data-serves="small"`,
		`"rename":{"url":"/api/pages/solar/rename"}`,
		`<script src="/ui/editor.js"></script>`, `id="board"`, `id="palette"`, `id="palette-filter"`, `id="inspector"`,
		`id="show-cells" checked`, `id="render-hint"`, `id="preview-error"`, `id="grid-lock"`,
		// The panels list the files of the page and the file of every display
		// type; one dialog creates a layout (ADR 0110).
		`<h3>Variants</h3>`, `pages/solar.hochkant.yaml is missing`,
		`pages/solar.hochkant.yaml is missing`, `data-dialog="create-variant"`,
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
	body = request(t, s, http.MethodGet, "/ui/pages/neu?display=lcd&new=1", nil, nil).Body.String()
	if !strings.Contains(body, `"name":"neu","display":"lcd","file":"","kind":"text","width":20,"height":4,"new":true,"grid":[20,4]`) {
		t.Fatalf("new text page:\n%s", body)
	}

	// Without a display type the preview is one the file can render; a text
	// page opens for the text type.
	writePage(t, s, "clock", "", textVariant)
	if body := request(t, s, http.MethodGet, "/ui/pages/clock", nil, nil).Body.String(); !strings.Contains(body, `"display":"lcd"`) {
		t.Fatalf("default preview of a text page:\n%s", body)
	}

	for _, path := range []string{
		"/ui/pages/Nope?display=small",            // invalid name
		"/ui/pages/solar?display=nope",            // unknown display type
		"/ui/pages/solar?display=small&file=No",   // invalid file
		"/ui/pages/solar?display=small&file=nope", // missing variant
		"/ui/pages/x_y?display=small",
	} {
		if rec := request(t, s, http.MethodGet, path, nil, nil); rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
	if rec := request(t, s, http.MethodGet, "/ui/pages/solar?display=nope", nil, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown display type: %d %s", rec.Code, rec.Body)
	}
	if rec := request(t, s, http.MethodGet, "/ui/pages/solar?display=small&file=nope", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing variant: %d %s", rec.Code, rec.Body)
	}
}

// TestVariantPreviewSwitch checks that a variant which several display types
// name lists each type as a preview link and marks the type the board renders
// (ADR 0110).
func TestVariantPreviewSwitch(t *testing.T) {
	s := editorServer(t)
	s.config().Profiles["wide"] = Profile{Width: 80, Height: 20, PPI: 125, Levels: 4}
	writePage(t, s, "solar", "", `
title: Solar
kind: image
grid: [2, 2]
variants:
  small: hochkant
  wide: hochkant
cells:
  - { at: [0, 0], span: [2, 1], widget: paperboy/header }
`)
	writePage(t, s, "solar", "hochkant", variantPage)

	body := request(t, s, http.MethodGet, "/ui/pages/solar?display=small&file=hochkant", nil, nil).Body.String()
	for _, want := range []string{
		`<li><a href="/ui/pages/solar?display=small&amp;file=hochkant" class="active" aria-current="true">small</a></li>`,
		`<li><a href="/ui/pages/solar?display=wide&amp;file=hochkant">wide</a></li>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the variant lacks %q:\n%s", want, body)
		}
	}

	// The other type renders the same file, so the board follows the link.
	body = request(t, s, http.MethodGet, "/ui/pages/solar?display=wide&file=hochkant", nil, nil).Body.String()
	for _, want := range []string{
		`<li><a href="/ui/pages/solar?display=small&amp;file=hochkant">small</a></li>`,
		`<li><a href="/ui/pages/solar?display=wide&amp;file=hochkant" class="active" aria-current="true">wide</a></li>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the switched variant lacks %q:\n%s", want, body)
		}
	}
}

// TestEditorGridFromDisplayType checks the default grid of a display type
// (ADR 0128): a new page starts there, the Playground previews a cell at the
// page's grid, and the widget endpoint divides the page by it.
func TestEditorGridFromDisplayType(t *testing.T) {
	s := editorServer(t)
	grid := [2]int{10, 6}
	s.config().Profiles["wide"] = Profile{Width: 800, Height: 480, PPI: 125, Levels: 2, Grid: &grid}

	// A new image page starts at the type's grid; the reference grid stays
	// 6 × 8, because plugins declare their sizes on it.
	body := request(t, s, http.MethodGet, "/ui/pages/neu?kind=image&display=wide&new=1", nil, nil).Body.String()
	for _, want := range []string{`"kind":"image","width":800,"height":480,"new":true`, `"grid":[10,6]`, `"reference":[6,8]`} {
		if !strings.Contains(body, want) {
			t.Fatalf("new page on the wide type lacks %q:\n%s", want, body)
		}
	}
	// The type list names the grid, so a new variant starts from it too.
	if !strings.Contains(body, `"name":"wide","kind":"image","width":800,"height":480,"grid":[10,6]`) {
		t.Fatalf("the type list lacks the grid:\n%s", body)
	}

	// A Playground link from a page carries the page's grid, so the state and
	// the preview use the cell the widget really gets.
	body = request(t, s, http.MethodGet, "/ui/playground?display=wide&widget=paperboy/clock&span=5x3&grid=10x6", nil, nil).Body.String()
	for _, want := range []string{`"display":"wide"`, `"span":[5,3]`, `"grid":[10,6]`} {
		if !strings.Contains(body, want) {
			t.Fatalf("the widgets page lacks %q:\n%s", want, body)
		}
	}

	// The endpoint divides the page by the grid: 5×3 of 10×6 on 800×480.
	rec := preview(t, s, "/api/widget/paperboy/clock?display=wide&span=5x3&grid=10x6")
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil || img.Bounds().Dx() != 400 || img.Bounds().Dy() != 240 {
		t.Fatalf("cell of the page grid: %v %v", img.Bounds(), err)
	}
	// Without a span the preview is half of the type's default grid.
	rec = preview(t, s, "/api/widget/paperboy/clock?display=wide")
	if img, err := png.Decode(bytes.NewReader(rec.Body.Bytes())); err != nil || img.Bounds().Dx() != 400 || img.Bounds().Dy() != 240 {
		t.Fatalf("default cell: %v %v", img.Bounds(), err)
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
	body := request(t, s, http.MethodGet, "/ui/pages/wetter?kind=text&display=lcd&new=1", nil, nil).Body.String()
	if !strings.Contains(body, `"file":"","kind":"text","width":20,"height":4,"new":true,"grid":[20,4]`) {
		t.Fatalf("new text page next to a variant:\n%s", body)
	}
	// Without a chosen grid it takes the first configured text type.
	body = request(t, s, http.MethodGet, "/ui/pages/wetter?kind=text&new=1", nil, nil).Body.String()
	if !strings.Contains(body, `"kind":"text"`) || !strings.Contains(body, `"grid":[20,4]`) {
		t.Fatalf("new text page without a chosen grid:\n%s", body)
	}
	// A new image page is its own file as well, whatever display type a stale
	// link names.
	body = request(t, s, http.MethodGet, "/ui/pages/wetter?kind=image&display=lcd&new=1", nil, nil).Body.String()
	if !strings.Contains(body, `"file":"","kind":"image","width":40,"height":20,"new":true`) {
		t.Fatalf("new image page next to a variant:\n%s", body)
	}
	// An unknown kind is refused.
	if rec := request(t, s, http.MethodGet, "/ui/pages/wetter?kind=led&new=1", nil, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown kind: %d %s", rec.Code, rec.Body)
	}
	// The old link without a kind still derives it from the display type.
	body = request(t, s, http.MethodGet, "/ui/pages/wetter?display=lcd&new=1", nil, nil).Body.String()
	if !strings.Contains(body, `"kind":"text"`) {
		t.Fatalf("new page from a display type:\n%s", body)
	}
	// A page file that exists opens as it is, &new=1 or not.
	writePage(t, s, "solar", "", pageWithVariant)
	for _, path := range []string{"/ui/pages/solar?display=small", "/ui/pages/solar?display=small&new=1"} {
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
	if body := request(t, s, http.MethodGet, "/ui/pages", nil, nil).Body.String(); !strings.Contains(body, "No display types configured") {
		t.Fatalf("overview lacks the hint:\n%s", body)
	}
	rec := request(t, s, http.MethodGet, "/ui/pages/solar?new=1", nil, nil)
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
	for _, want := range []string{"/api/pages/", "/api/plugins", "/api/preview/", "previewBody()", "mapSpan(", "grid-lock", "gridFromInputs", "PUT", "pointerdown", "handle-xy", "firstWidget",
		"renderTextInspector", "textCellFields(box, c)", "where.className = 'muted'", "placeTextAt", "startPaletteDrag"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("script lacks %q", want)
		}
	}
	// The text inspector is a panel like the image one: no nested box and no
	// leftover wrapper it could return.
	for _, unwanted := range []string{"text-cell", "return wrap"} {
		if strings.Contains(rec.Body.String(), unwanted) {
			t.Fatalf("the text inspector still carries %q", unwanted)
		}
	}
}

// TestTextPreviewFont serves the vendored pixel font and names it in the
// shared stylesheet and the text board (ADR 0116).
func TestTextPreviewFont(t *testing.T) {
	s := editorServer(t)
	rec := request(t, s, http.MethodGet, "/ui/fonts/dotgothic16-latin.woff2", nil, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "font/woff2" {
		t.Fatalf("font: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if body := rec.Body.Bytes(); len(body) < 1000 || string(body[:4]) != "wOF2" {
		t.Fatalf("font body: %d bytes, not woff2", len(body))
	}
	css := request(t, s, http.MethodGet, "/ui/ui.css", nil, nil).Body.String()
	if !strings.Contains(css, `@font-face`) || !strings.Contains(css, `/ui/fonts/dotgothic16-latin.woff2`) ||
		!strings.Contains(css, `font-family: "DotGothic16"`) {
		t.Fatal("the stylesheet does not load the vendored font")
	}
	pageBody := request(t, s, http.MethodGet, "/ui/pages/solar?display=small", nil, nil).Body.String()
	if !strings.Contains(pageBody, `font-family: "DotGothic16"`) {
		t.Fatal("the board does not use the pixel font")
	}
	virtual := request(t, s, http.MethodGet, "/ui/virtual?plan=standard&display=lcd", nil, nil).Body.String()
	if !strings.Contains(virtual, `data-cols="20"`) || !strings.Contains(virtual, `#text .cell`) {
		t.Fatalf("the virtual display does not lay out one slot per character:\n%s", virtual)
	}
}

// TestEditorNavigation links the editor from the other views.
func TestEditorNavigation(t *testing.T) {
	s, _ := testServer(t)
	for _, path := range []string{"/ui", "/ui/playground?", "/ui/devices/test/plan"} {
		rec := request(t, s, http.MethodGet, path, nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
		if !strings.Contains(rec.Body.String(), `href="/ui/pages"`) {
			t.Fatalf("%s does not link the editor", path)
		}
	}
}
