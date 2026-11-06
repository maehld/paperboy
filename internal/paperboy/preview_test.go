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
)

const previewTemplate = `
title: Solar
grid: [2, 2]
cells:
  - { at: [0, 0], span: [2, 1], widget: paperboy/header }
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
	writePage(t, s, "solar", "small", previewTemplate)
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

// TestPreviewFallsBackToSharedPage verifies that pages/<name>.yaml serves
// every display type without its own variant (ADR 0049).
func TestPreviewFallsBackToSharedPage(t *testing.T) {
	s, _ := testServer(t)
	writePage(t, s, "solar", "", previewTemplate)
	if rec := preview(t, s, "/api/preview/small/solar"); rec.Code != http.StatusOK {
		t.Fatalf("shared page: %d %s", rec.Code, rec.Body)
	}
}

func TestPreviewNotFound(t *testing.T) {
	s, _ := testServer(t)
	writePage(t, s, "solar", "small", previewTemplate)
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
	writePage(t, s, "broken", "small", "grid: [1, 1]\ncells:\n  - widget: paperboy/text\n    unknown: 1\n")
	if rec := preview(t, s, "/api/preview/small/broken"); rec.Code != http.StatusInternalServerError {
		t.Fatalf("broken template: %d", rec.Code)
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
		len(out.Plugins[0].Description.Widgets) != 3 {
		t.Fatalf("plugins: %s", rec.Body)
	}
}
