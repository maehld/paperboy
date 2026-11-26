package paperboy

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDeviceEditorOverview lists the device files with their display type, MAC
// and plan, and offers a new device (ADR 0070).
func TestDeviceEditorOverview(t *testing.T) {
	s := editorServer(t)
	dir := s.config().Dir
	writeDeviceFile(t, dir, "kueche", "display_type: small\nmac: \"aa:bb:cc:dd:ee:02\"\nplan: tour\n")
	writeDeviceFile(t, dir, "kaputt", "display_typ: small\n")

	body := request(t, s, http.MethodGet, "/ui/devices", nil, nil).Body.String()
	for _, want := range []string{
		"<h1>Devices</h1>", `<a href="/ui/devices/kueche">kueche</a>`, "<td data-label=\"Display type\">small</td>", "<td data-label=\"Plan\">tour</td>",
		"/ui/devices/kueche", "New device", `action="/ui/devices/new"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("overview lacks %q:\n%s", want, body)
		}
	}
	// A broken device file is listed with its error and cannot be opened.
	if !strings.Contains(body, "<td class=\"name\" data-label=\"ID\">kaputt</td>") || !strings.Contains(body, "yaml: unmarshal errors") {
		t.Fatalf("broken device not reported:\n%s", body)
	}
	if strings.Contains(body, "/ui/devices/kaputt") {
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

	rec := request(t, s, http.MethodGet, "/ui/devices/kueche", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("device editor: %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<script id="device-data" type="application/json">`, `"id":"kueche"`, `"displays":["lcd","small"]`,
		`src="/ui/device-editor.js"`, `id="mac"`, `id="plan"`, `id="warn"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("device page lacks %q:\n%s", want, body)
		}
	}

	// A device without a file opens as a new one, but only when asked for.
	if rec := request(t, s, http.MethodGet, "/ui/devices/neu?new=1", nil, nil); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), `"new":true`) {
		t.Fatalf("new device: %d %s", rec.Code, rec.Body)
	}
	if rec := request(t, s, http.MethodGet, "/ui/devices/neu", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing device: %d %s", rec.Code, rec.Body)
	}
	for _, path := range []string{"/ui/devices/Nope", "/ui/devices/x_y"} {
		if rec := request(t, s, http.MethodGet, path, nil, nil); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
}

// TestDeviceEditorRecentLog shows the stored log lines, newest first (ADR 0171).
func TestDeviceEditorRecentLog(t *testing.T) {
	s := editorServer(t)
	writeDeviceFile(t, s.config().Dir, "test", "display_type: small\nmac: \"aa:bb:cc:dd:ee:01\"\n")
	s.config().Timezone = time.UTC
	key := s.config().Devices[0].stateKey()
	s.state[key] = &deviceState{Log: []deviceLogLine{
		{ID: 1, At: time.Date(2026, 11, 26, 20, 0, 0, 0, time.UTC), Level: "info", Message: "timing boot: 412 ms"},
		{ID: 2, Time: time.Date(2026, 11, 26, 21, 14, 5, 0, time.UTC).Unix(), Level: "warn", Message: "image download failed"},
	}}
	body := request(t, s, http.MethodGet, "/ui/devices/test", nil, nil).Body.String()
	for _, want := range []string{
		"<h3>Recent log</h3>", "image download failed", "timing boot: 412 ms",
		"2026-11-26 21:14:05", "2026-11-26 20:00:00", `class="warn"`, `class="info"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the editor lacks %q:\n%s", want, body)
		}
	}
	// The newest line comes first.
	if strings.Index(body, "image download failed") > strings.Index(body, "timing boot") {
		t.Fatalf("the log must show the newest line first:\n%s", body)
	}
}

// TestDeviceEditorScript is served from the binary and talks to the device API.
func TestDeviceEditorScript(t *testing.T) {
	s, _ := testServer(t)
	rec := request(t, s, http.MethodGet, "/ui/device-editor.js", nil, nil)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("script: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, want := range []string{"/api/devices/", "PUT", "devices/"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("script lacks %q", want)
		}
	}
}
