package paperboy

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDeviceEditorOverview lists the device files with their display type, MAC
// and plan, and offers a new device (ADR 0070).
func TestDeviceEditorOverview(t *testing.T) {
	s := editorServer(t)
	dir := s.config().Dir
	writeDeviceFile(t, dir, "kueche", "display_type: small\nmac: \"aa:bb:cc:dd:ee:02\"\nplan: tour\n")
	writeDeviceFile(t, dir, "kaputt", "display_typ: small\n")

	body := request(t, s, http.MethodGet, "/ui/editor?tab=devices", nil, nil).Body.String()
	for _, want := range []string{
		"<h2>Devices", "<td>kueche</td>", "<td>small</td>", "<td>tour</td>",
		"/ui/editor/device?id=kueche", "New device", `action="/ui/editor/device"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("overview lacks %q:\n%s", want, body)
		}
	}
	// A broken device file is listed with its error and cannot be opened.
	if !strings.Contains(body, "<td>kaputt</td>") || !strings.Contains(body, "yaml: unmarshal errors") {
		t.Fatalf("broken device not reported:\n%s", body)
	}
	if strings.Contains(body, "/ui/editor/device?id=kaputt") {
		t.Fatalf("a broken device file is offered:\n%s", body)
	}
}

// TestDeviceEditorPage opens one device and starts a new one.
func TestDeviceEditorPage(t *testing.T) {
	s := editorServer(t)
	writeDeviceFile(t, s.config().Dir, "kueche", "display_type: small\nmac: \"aa:bb:cc:dd:ee:02\"\n")
	// Other files in devices/ are ignored, like in pages and plans.
	if err := os.WriteFile(filepath.Join(s.config().DevicesDir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := request(t, s, http.MethodGet, "/ui/editor/device?id=kueche", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("device editor: %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<script id="device-data" type="application/json">`, `"id":"kueche"`, `"displays":["lcd","small"]`,
		`src="/ui/editor/device.js"`, `id="mac"`, `id="plan"`, `id="warn"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("device page lacks %q:\n%s", want, body)
		}
	}

	// A device without a file opens as a new one, but only when asked for.
	if rec := request(t, s, http.MethodGet, "/ui/editor/device?id=neu&new=1", nil, nil); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), `"new":true`) {
		t.Fatalf("new device: %d %s", rec.Code, rec.Body)
	}
	if rec := request(t, s, http.MethodGet, "/ui/editor/device?id=neu", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing device: %d %s", rec.Code, rec.Body)
	}
	for _, path := range []string{"/ui/editor/device?id=Nope", "/ui/editor/device?id=..%2Fx", "/ui/editor/device"} {
		if rec := request(t, s, http.MethodGet, path, nil, nil); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
}

// TestDeviceEditorScript is served from the binary and talks to the device API.
func TestDeviceEditorScript(t *testing.T) {
	s, _ := testServer(t)
	rec := request(t, s, http.MethodGet, "/ui/editor/device.js", nil, nil)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("script: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, want := range []string{"/api/devices/", "PUT", "devices/"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("script lacks %q", want)
		}
	}
}
