package paperboy

import (
	"bytes"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const previewTemplate = `
watch:
  - { path: solar/current, stale_after: 5m }
rows: [1, 2]
blocks:
  - name: kopf
    header: { title: Solar, time: solar.current.latest.time }
  - name: kacheln
    tiles:
      - { label: PV, value: solar.current.latest.values.pv_power, format: power }
`

func writePage(t *testing.T, s *Server, name, display, yaml string) {
	t.Helper()
	if err := os.MkdirAll(s.cfg.Pages, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.cfg.Pages, name+"."+display+".yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
}

func preview(t *testing.T, s *Server, path string, withToken bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if withToken {
		req.Header.Set("Authorization", "Bearer "+testToken)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestPreviewRequiresToken(t *testing.T) {
	s, _ := testServer(t)
	writePage(t, s, "solar", "small", previewTemplate)
	if rec := preview(t, s, "/api/preview/small/solar", false); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without token: %d", rec.Code)
	}
}

func TestPreviewRendersPNG(t *testing.T) {
	s, _ := testServer(t)
	writePage(t, s, "solar", "small", previewTemplate)
	if code, body := dataCall(t, s, http.MethodPut, "/api/data/solar/current", `{"latest":{"values":{"pv_power":4210},"time":"2026-10-17T10:00:00Z"}}`); code != http.StatusNoContent {
		t.Fatalf("put data: %d %s", code, body)
	}

	rec := preview(t, s, "/api/preview/small/solar", true)
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

func TestPreviewNotFound(t *testing.T) {
	s, _ := testServer(t)
	writePage(t, s, "solar", "small", previewTemplate)
	for _, path := range []string{
		"/api/preview/e1001/solar",   // unknown display
		"/api/preview/small/unknown", // unknown page
		"/api/preview/small/..%2Fx",  // path traversal
	} {
		if rec := preview(t, s, path, true); rec.Code != http.StatusNotFound {
			t.Fatalf("%s: %d", path, rec.Code)
		}
	}
}

func TestPreviewBrokenTemplate(t *testing.T) {
	s, _ := testServer(t)
	writePage(t, s, "broken", "small", "rows: [1]\nblocks:\n  - text: x\n    unknown: 1\n")
	if rec := preview(t, s, "/api/preview/small/broken", true); rec.Code != http.StatusInternalServerError {
		t.Fatalf("broken template: %d", rec.Code)
	}
}
