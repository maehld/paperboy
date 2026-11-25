package paperboy

import (
	"bytes"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/server/internal/builtin"
)

const previewTemplate = `
title: Solar
kind: image
grid: [2, 2]
cells:
  - { at: [0, 0], span: [2, 1], widget: paperboy/text, args: {text: x} }
  - { at: [0, 1], span: [2, 1], widget: paperboy/text, args: { text: "4,2 kW" } }
`

func writePage(t *testing.T, s *Server, name, display, yaml string) {
	t.Helper()
	if err := os.MkdirAll(s.config().Pages, 0o755); err != nil {
		t.Fatal(err)
	}
	file := name + ".yaml"
	if display != "" {
		file = name + "." + display + ".yaml"
	}
	if err := os.WriteFile(filepath.Join(s.config().Pages, file), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
}

func preview(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestPreviewRendersPNG(t *testing.T) {
	s, _ := testServer(t)
	writePage(t, s, "solar", "", previewTemplate)
	rec := preview(t, s, "/api/preview/small/solar")
	if rec.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content type: %q", ct)
	}
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 40 || img.Bounds().Dy() != 20 {
		t.Fatalf("preview size %v, want 40x20", img.Bounds())
	}
}

// TestPreviewServesTheKind verifies that a page file serves every display type
// of its kind (ADR 0110).
func TestPreviewServesTheKind(t *testing.T) {
	s, _ := testServer(t)
	writePage(t, s, "solar", "", previewTemplate)
	if rec := preview(t, s, "/api/preview/small/solar"); rec.Code != http.StatusOK {
		t.Fatalf("page: %d %s", rec.Code, rec.Body)
	}
}

func TestPreviewNotFound(t *testing.T) {
	s, _ := testServer(t)
	writePage(t, s, "solar", "", previewTemplate)
	for _, path := range []string{
		"/api/preview/e1001/solar",   // unknown display
		"/api/preview/small/unknown", // unknown page
		"/api/preview/small/..%2Fx",  // path traversal
	} {
		if rec := preview(t, s, path); rec.Code != http.StatusNotFound {
			t.Fatalf("%s: %d", path, rec.Code)
		}
	}
}

func TestPreviewBrokenTemplate(t *testing.T) {
	s, _ := testServer(t)
	writePage(t, s, "broken", "", "kind: image\ngrid: [1, 1]\ncells:\n  - widget: paperboy/text\n    unknown: 1\n")
	if rec := preview(t, s, "/api/preview/small/broken"); rec.Code != http.StatusInternalServerError {
		t.Fatalf("broken template: %d", rec.Code)
	}
}

// TestPreviewNamesADevice checks the optional device parameter of a page
// preview (ADR 0123): without it the page shows the same sample device on every
// request, with it the named device. The device-status row differs, so the
// pictures differ too.
func TestPreviewNamesADevice(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "status", "", `
kind: image
grid: [1, 1]
cells:
  - { at: [0, 0], span: [1, 1], widget: paperboy/device-status }
`)
	sample := preview(t, s, "/api/preview/small/status").Body.Bytes()
	if again := preview(t, s, "/api/preview/small/status").Body.Bytes(); !bytes.Equal(sample, again) {
		t.Fatal("the sample device is not deterministic")
	}
	named := preview(t, s, "/api/preview/small/status?device=test").Body.Bytes()
	if bytes.Equal(sample, named) {
		t.Fatal("the preview did not switch from the sample device to the named one")
	}
}

// TestPluginsListsBuiltin verifies that /api/plugins describes the built-in
// plugin, the input of a page editor.
func TestPluginsListsBuiltin(t *testing.T) {
	s, _ := testServer(t)
	rec := preview(t, s, "/api/plugins")
	var out struct {
		Plugins []pluginInfo `json:"plugins"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Plugins) != 1 || out.Plugins[0].Name != "paperboy" || out.Plugins[0].Description == nil ||
		len(out.Plugins[0].Description.Widgets) != 6 {
		t.Fatalf("plugins: %s", rec.Body)
	}
	// The band is internal chrome; the old header and title widgets are gone.
	for _, w := range out.Plugins[0].Description.Widgets {
		if w.Widget == "header" || w.Widget == "title" || w.Widget == "band" {
			t.Errorf("the built-in plugin lists %q", w.Widget)
		}
	}
	// The logo is an image widget: a text display cannot show the mark.
	for _, w := range out.Plugins[0].Description.Widgets {
		if w.Widget == "logo" && (len(w.Kinds) != 1 || w.Kinds[0] != plugin.KindImage) {
			t.Errorf("logo kinds: %v", w.Kinds)
		}
	}
}

// TestPluginsOfferDeviceIDs checks that the plugins answer offers the
// configured device ids for the two built-in device arguments (ADR 0123). The
// built-in Describe stays enum-free, so an old page still renders.
func TestPluginsOfferDeviceIDs(t *testing.T) {
	s := editorServer(t)
	rec := preview(t, s, "/api/plugins")
	var out struct {
		Plugins []pluginInfo `json:"plugins"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Plugins) != 1 || out.Plugins[0].Description == nil {
		t.Fatalf("plugins: %s", rec.Body)
	}
	seen := map[string]bool{}
	for _, w := range out.Plugins[0].Description.Widgets {
		for _, name := range deviceArgWidgets[w.Widget] {
			seen[w.Widget] = true
			arg, ok := w.Args[name]
			if !ok {
				t.Fatalf("%s misses the argument %q", w.Widget, name)
			}
			if len(arg.Enum) != 1 || arg.Enum[0] != "test" {
				t.Errorf("%s.%s enum: %v", w.Widget, name, arg.Enum)
			}
			if arg.EnumLabels[""] == "" {
				t.Errorf("%s.%s has no label for the empty value", w.Widget, name)
			}
			if !arg.Allows("test") {
				t.Errorf("%s.%s rejects a configured id", w.Widget, name)
			}
			if arg.Allows("fehlt") {
				t.Errorf("%s.%s allows a removed id", w.Widget, name)
			}
		}
	}
	if len(seen) != len(deviceArgWidgets) {
		t.Fatalf("device arguments of the plugins answer: %v", seen)
	}
	for _, w := range (builtin.Plugin{}).Describe().Widgets {
		for _, name := range deviceArgWidgets[w.Widget] {
			if len(w.Args[name].Enum) != 0 {
				t.Errorf("the built-in %s.%s describes an enum; a page with a removed id would fail", w.Widget, name)
			}
		}
	}
}
