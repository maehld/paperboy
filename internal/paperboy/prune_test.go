package paperboy

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFiles creates empty files in dir.
func writeFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// pruneServer builds an instance with a pinned plugin, a second plugin, two
// hardware types and a device that reports the oldest e1001 image.
func pruneServer(t *testing.T) *Server {
	t.Helper()
	s := testDownloadServer(t, downloadConfig(t, "https://example.invalid", "    version: v1.0.0\n", ""))
	cfg := s.config()
	writeFiles(t, cfg.PluginsDir,
		"testplugin-v1.0.0", "testplugin-v1.1.0", "testplugin-v1.2.0",
		"other-v1.0.0", "other-v1.1.0", "other-v1.2.0",
		"handmade")
	writeFiles(t, cfg.FirmwareDir,
		"e1001-v1.0.0.bin", "e1001-v1.1.0.bin", "e1001-v1.2.0.bin",
		"sticky-v1.0.0.bin", "sticky-v1.1.0.bin", "sticky-v1.2.0.bin")
	dev := planDevice(t, s)
	dev.req.Hardware = "e1001"
	dev.req.Firmware = "1.0.0"
	dev.req.Battery = &syncBattery{Volts: 3.9}
	displayPage(t, s, dev)
	return s
}

// TestPrunePlanKeepsWhatIsInUse verifies the rule table of ADR 0090: the pinned
// and the newest versions stay, a reported image stays, and a hand-built program
// is never a candidate.
func TestPrunePlanKeepsWhatIsInUse(t *testing.T) {
	s := pruneServer(t)
	cfg := s.config()
	remove, kept := s.prunePlan(cfg)
	var names []string
	for _, f := range remove {
		names = append(names, f.File)
	}
	want := []string{"other-v1.0.0", "sticky-v1.0.0.bin"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("prune plan: %v", names)
	}
	if kept != 11 {
		t.Fatalf("kept %d files, want 11", kept)
	}
	for _, f := range remove {
		if f.Version == "" {
			t.Fatalf("%s: no version in the preview", f.File)
		}
		if f.Train == "" {
			t.Fatalf("%s: no train", f.File)
		}
	}
}

// TestPruneDeletesWhatThePreviewListed walks the dialog: the preview names the
// files, the button deletes exactly them, and nothing else (ADR 0090).
func TestPruneDeletesWhatThePreviewListed(t *testing.T) {
	s := pruneServer(t)
	cfg := s.config()

	if body := htmlRequest(t, s, "/ui/downloads").Body.String(); !strings.Contains(body, "Preview cleanup") {
		t.Fatalf("the page does not offer the preview: %s", body)
	}
	body := htmlRequest(t, s, "/ui/downloads?prune=1").Body.String()
	for _, want := range []string{"Delete 2 files", "other-v1.0.0", "sticky-v1.0.0.bin"} {
		if !strings.Contains(body, want) {
			t.Fatalf("preview lacks %q: %s", want, body)
		}
	}
	if strings.Contains(body, "Delete 3 files") {
		t.Fatalf("the preview counts a file it keeps: %s", body)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/ui/downloads/prune", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 303 {
		t.Fatalf("prune: %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/ui/downloads?pruned=2&failed=0" {
		t.Fatalf("prune redirect: %q", loc)
	}
	for _, gone := range []string{"other-v1.0.0", "sticky-v1.0.0.bin"} {
		dir := cfg.PluginsDir
		if strings.HasSuffix(gone, ".bin") {
			dir = cfg.FirmwareDir
		}
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Fatalf("%s must be deleted", gone)
		}
	}
	for _, stay := range []string{"handmade", "testplugin-v1.0.0", "other-v1.1.0"} {
		if _, err := os.Stat(filepath.Join(cfg.PluginsDir, stay)); err != nil {
			t.Fatalf("%s must stay: %v", stay, err)
		}
	}
	if _, err := os.Stat(filepath.Join(cfg.FirmwareDir, "e1001-v1.0.0.bin")); err != nil {
		t.Fatalf("the reported image must stay: %v", err)
	}
	// The page confirms the result and nothing is left to prune.
	body = htmlRequest(t, s, "/ui/downloads?pruned=2&failed=0").Body.String()
	if !strings.Contains(body, "Deleted 2 files.") {
		t.Fatalf("no confirmation: %s", body)
	}
	if remove, _ := s.prunePlan(cfg); len(remove) != 0 {
		t.Fatalf("a second prune has work to do: %v", remove)
	}
}

// TestPruneWithoutDownloadSection cleans an inventory that was unpacked by hand.
func TestPruneWithoutDownloadSection(t *testing.T) {
	s, _ := testServer(t)
	writeFiles(t, s.config().PluginsDir, "weather-v1.0.0", "weather-v1.1.0", "weather-v1.2.0")
	body := htmlRequest(t, s, "/ui/downloads?prune=1").Body.String()
	if !strings.Contains(body, "weather-v1.0.0") {
		t.Fatalf("the preview needs no download section: %s", body)
	}
}
