package paperboy

import (
	"net/http"
	"strings"
	"testing"
)

// TestEditorDisplaysTab lists the display type files with their fields and the
// form for a new one (ADR 0071).
func TestEditorDisplaysTab(t *testing.T) {
	s := editorServer(t)
	writeDisplayFile(t, s.config().Dir, "small", "kind: image\nsize: [40, 20]\nppi: 125\ngray_levels: 4\n")
	writeDisplayFile(t, s.config().Dir, "lcd", "kind: text\nsize: [20, 4]\ncharset: ascii\n")
	writeDisplayFile(t, s.config().Dir, "broken", "size: [\n")

	body := request(t, s, http.MethodGet, "/ui/display-types", nil, nil).Body.String()
	for _, want := range []string{
		`<a href="/ui/display-types" class="active" aria-current="page">Display types</a>`,
		`id="display-table"`, `<a href="/ui/display-types/small">small</a>`, ">40 × 20</td>", "125 ppi", ">4</td>",
		">20 × 4</td>", "<td data-label=\"Density\">ascii</td>", "New display type",
		"/ui/display-types/small",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("displays tab lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "/ui/display-types/broken") {
		t.Fatalf("a file that does not parse is offered:\n%s", body)
	}
	if !strings.Contains(body, "class=\"error\"") {
		t.Fatalf("the broken file is not reported:\n%s", body)
	}
	// The other tabs do not carry the display type list.
	if other := request(t, s, http.MethodGet, "/ui/pages", nil, nil).Body.String(); strings.Contains(other, `id="display-table"`) {
		t.Fatalf("pages tab carries the display type list:\n%s", other)
	}
}

// TestDisplayEditor opens one display type file, lists what references it and
// starts a new type when there is no file yet.
func TestDisplayEditor(t *testing.T) {
	s := editorServer(t)
	writeDisplayFile(t, s.config().Dir, "small", "kind: image\nsize: [40, 20]\nppi: 125\n")
	writePage(t, s, "solar", "", pageWithVariant)
	writePage(t, s, "solar", "hochkant", variantPage)
	writeDeviceFile(t, s.config().Dir, "flur", "display_type: small\nmac: \"AA:BB:CC:DD:EE:02\"\n")

	rec := request(t, s, http.MethodGet, "/ui/display-types/small", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("display editor: %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`id="kind"`, `id="width"`, `id="height"`, `id="gray"`, `id="ppi"`, `id="charset"`,
		`id="panel-diagonal"`, `id="panel-width"`, `id="panel-height"`,
		`id="use-diagonal"`, `id="use-width"`, `id="use-height"`, `id="panel-hint"`,
		`id="min_refresh"`, `id="power"`, `id="yaml"`, `<script src="/ui/display-editor.js"></script>`,
		`id="header-height"`, `id="header-page-indicator"`, `id="header-temperature"`, `id="header-humidity"`, `id="header-clock"`, `id="header-battery"`, `id="header-preview-img"`,
		// The page names this display type: the editor links the variant.
		`href="/ui/pages/solar?display=small&amp;file=hochkant"`, `href="/ui/devices/flur"`,
		`"pages":["solar"]`,
		"<h1>Display type small</h1>",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("display editor lacks %q:\n%s", want, body)
		}
	}

	// A new type needs &new=1; without it the page explains the missing file.
	if rec := request(t, s, http.MethodGet, "/ui/display-types/neu?new=1", nil, nil); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), `"new":true`) {
		t.Fatalf("new display type: %d %s", rec.Code, rec.Body)
	}
	if rec := request(t, s, http.MethodGet, "/ui/display-types/neu", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing display type without new: %d", rec.Code)
	}
	for _, path := range []string{"/ui/display-types/Nope", "/ui/display-types/x_y"} {
		if rec := request(t, s, http.MethodGet, path, nil, nil); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
}

// TestDisplayEditorScript is served from the binary and talks to the display
// type API.
func TestDisplayEditorScript(t *testing.T) {
	s := editorServer(t)
	rec := request(t, s, http.MethodGet, "/ui/display-editor.js", nil, nil)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("script: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, want := range []string{"/api/displays/", "PUT", "displays/", "Math.hypot", "25.4", "grid-cols", "suggestedGrid", "/header-preview", "headerOut", "header-preview-img", "header-clock"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("script lacks %q", want)
		}
	}
	// The argument fields are one script for the page editor.
	rec = request(t, s, http.MethodGet, "/ui/args.js", nil, nil)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("args script: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "window.pbArgs") {
		t.Fatalf("args script does not export its renderer")
	}
}
