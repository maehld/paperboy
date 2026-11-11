package paperboy

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fileExists reports whether a file is there, for the rename assertions.
func fileExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	return err == nil
}

// TestRenamePage moves the shared page and every variant as one set, and
// refuses a set that changed since it was read (ADR 0079).
func TestRenamePage(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "small", previewTemplate)
	writePage(t, s, "solar", "", previewTemplate)
	writePage(t, s, "other", "small", previewTemplate)

	var page editorPage
	if rec := request(t, s, http.MethodGet, "/api/pages/solar?display=small", nil, &page); rec.Code != http.StatusOK {
		t.Fatalf("read: %d %s", rec.Code, rec.Body)
	}
	if page.SetHash == "" {
		t.Fatal("the read page carries no set hash")
	}

	// A stale set hash is a conflict.
	rec := request(t, s, http.MethodPost, "/api/pages/solar/rename?hash=stale", map[string]string{"name": "sun"}, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale hash: %d %s", rec.Code, rec.Body)
	}
	if !fileExists(t, filepath.Join(s.config().Pages, "solar.small.yaml")) {
		t.Fatal("a refused rename moved a file")
	}

	// A name that is already taken is refused.
	rec = request(t, s, http.MethodPost, "/api/pages/solar/rename?hash="+page.SetHash, map[string]string{"name": "other"}, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("collision: %d %s", rec.Code, rec.Body)
	}

	var out struct {
		Name  string
		Files []string
	}
	rec = request(t, s, http.MethodPost, "/api/pages/solar/rename", map[string]string{"name": "sun"}, &out)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body)
	}
	if out.Name != "sun" || len(out.Files) != 2 {
		t.Fatalf("answer: %+v", out)
	}
	for _, gone := range []string{"solar.small.yaml", "solar.yaml"} {
		if fileExists(t, filepath.Join(s.config().Pages, gone)) {
			t.Fatalf("%s survived the rename", gone)
		}
	}
	for _, moved := range []string{"sun.small.yaml", "sun.yaml"} {
		if !fileExists(t, filepath.Join(s.config().Pages, moved)) {
			t.Fatalf("%s is missing after the rename", moved)
		}
	}
	// The same name is not a rename.
	if rec := request(t, s, http.MethodPost, "/api/pages/sun/rename", map[string]string{"name": "sun"}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("same name: %d %s", rec.Code, rec.Body)
	}
	// A page that does not exist is a 404.
	if rec := request(t, s, http.MethodPost, "/api/pages/nope/rename", map[string]string{"name": "x"}, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing: %d %s", rec.Code, rec.Body)
	}
	// An invalid new name is a 400.
	if rec := request(t, s, http.MethodPost, "/api/pages/sun/rename", map[string]string{"name": "Not Valid"}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid name: %d %s", rec.Code, rec.Body)
	}
}

// TestRenamePlan moves one plan file, leaves the devices alone and guards the
// hash (ADR 0079).
func TestRenamePlan(t *testing.T) {
	s := planEditorServer(t)
	s.config().Devices[0].Plan = "tour"

	var plan editorPlan
	if rec := request(t, s, http.MethodGet, "/api/plans/tour", nil, &plan); rec.Code != http.StatusOK {
		t.Fatalf("read: %d %s", rec.Code, rec.Body)
	}
	if rec := request(t, s, http.MethodPost, "/api/plans/tour/rename?hash=stale", map[string]string{"name": "travel"}, nil); rec.Code != http.StatusConflict {
		t.Fatalf("stale hash: %d %s", rec.Code, rec.Body)
	}
	var out struct{ Name string }
	rec := request(t, s, http.MethodPost, "/api/plans/tour/rename?hash="+plan.Hash, map[string]string{"name": "travel"}, &out)
	if rec.Code != http.StatusOK || out.Name != "travel" {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body)
	}
	if fileExists(t, filepath.Join(s.config().Plans, "tour.yaml")) || !fileExists(t, filepath.Join(s.config().Plans, "travel.yaml")) {
		t.Fatal("the plan file did not move")
	}
	// The device keeps the old name; the rename does not rewrite it.
	if s.config().Devices[0].Plan != "tour" {
		t.Fatalf("device plan changed: %+v", s.config().Devices[0])
	}
	if rec := request(t, s, http.MethodPost, "/api/plans/nope/rename", map[string]string{"name": "x"}, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing: %d %s", rec.Code, rec.Body)
	}
}

// TestRenameDevice moves the device file and its state, so the renamed device
// keeps its cursor and battery reading. State of a deleted device must not be
// replaced silently (ADR 0070, 0079).
func TestRenameDevice(t *testing.T) {
	s := editorServer(t)
	writeDeviceFile(t, s.config().Dir, "kueche", "display_type: small\nmac: \"aa:bb:cc:dd:ee:02\"\n")

	var dev editorDevice
	if rec := request(t, s, http.MethodGet, "/api/devices/kueche", nil, &dev); rec.Code != http.StatusOK {
		t.Fatalf("read: %d %s", rec.Code, rec.Body)
	}
	// Give the device some state that must travel with it.
	s.state["kueche"] = &deviceState{Page: 3, BatteryVoltage: "3.9"}

	if rec := request(t, s, http.MethodPost, "/api/devices/kueche/rename?hash=stale", map[string]string{"name": "kitchen"}, nil); rec.Code != http.StatusConflict {
		t.Fatalf("stale hash: %d %s", rec.Code, rec.Body)
	}
	var out struct{ Name string }
	rec := request(t, s, http.MethodPost, "/api/devices/kueche/rename?hash="+dev.Hash, map[string]string{"name": "kitchen"}, &out)
	if rec.Code != http.StatusOK || out.Name != "kitchen" {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body)
	}
	if fileExists(t, filepath.Join(s.config().DevicesDir, "kueche.yaml")) || !fileExists(t, filepath.Join(s.config().DevicesDir, "kitchen.yaml")) {
		t.Fatal("the device file did not move")
	}
	if st := s.state["kitchen"]; st == nil || st.Page != 3 || st.BatteryVoltage != "3.9" {
		t.Fatalf("state did not move: %+v", s.state)
	}
	if _, ok := s.state["kueche"]; ok {
		t.Fatal("the old state key survived")
	}
	// A state key that already exists is refused: a deleted device keeps its
	// state so a recreated file picks it up.
	s.state["blocked"] = &deviceState{Page: 1}
	writeDeviceFile(t, s.config().Dir, "again", "display_type: small\nmac: \"aa:bb:cc:dd:ee:03\"\n")
	var again editorDevice
	request(t, s, http.MethodGet, "/api/devices/again", nil, &again)
	if rec := request(t, s, http.MethodPost, "/api/devices/again/rename?hash="+again.Hash, map[string]string{"name": "blocked"}, nil); rec.Code != http.StatusConflict {
		t.Fatalf("state collision: %d %s", rec.Code, rec.Body)
	}
	if rec := request(t, s, http.MethodPost, "/api/devices/nope/rename", map[string]string{"name": "x"}, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing: %d %s", rec.Code, rec.Body)
	}
}

// TestRenameDisplay moves only the display type file; the device that names it
// keeps the old name and answers 503, so the server stays usable (ADR 0079).
func TestRenameDisplay(t *testing.T) {
	s := editorServer(t)
	writeDisplayFile(t, s.config().Dir, "small", "kind: image\nsize: [40, 20]\nppi: 125\n")

	var disp editorDisplayType
	if rec := request(t, s, http.MethodGet, "/api/displays/small", nil, &disp); rec.Code != http.StatusOK {
		t.Fatalf("read: %d %s", rec.Code, rec.Body)
	}
	if rec := request(t, s, http.MethodPost, "/api/displays/small/rename?hash=stale", map[string]string{"name": "tiny"}, nil); rec.Code != http.StatusConflict {
		t.Fatalf("stale hash: %d %s", rec.Code, rec.Body)
	}
	var out struct{ Name string }
	rec := request(t, s, http.MethodPost, "/api/displays/small/rename?hash="+disp.Hash, map[string]string{"name": "tiny"}, &out)
	if rec.Code != http.StatusOK || out.Name != "tiny" {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body)
	}
	if fileExists(t, filepath.Join(s.config().DisplaysDir, "small.yaml")) || !fileExists(t, filepath.Join(s.config().DisplaysDir, "tiny.yaml")) {
		t.Fatal("the display type file did not move")
	}
	// The device keeps the old display type and is not rewritten.
	if s.config().Devices[0].Profile != "small" {
		t.Fatalf("device display type changed: %+v", s.config().Devices[0])
	}
	if rec := request(t, s, http.MethodPost, "/api/displays/nope/rename", map[string]string{"name": "x"}, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing: %d %s", rec.Code, rec.Body)
	}
}

// TestEditorRenameButtons: every row of the overview offers Rename, and the
// confirmation names what keeps the old name (ADR 0079).
func TestEditorRenameButtons(t *testing.T) {
	s := planEditorServer(t)
	s.config().Devices[0].Plan = "tour"
	writeDisplayFile(t, s.config().Dir, "small", "kind: image\nsize: [40, 20]\nppi: 125\n")
	writeDeviceFile(t, s.config().Dir, "test", "display_type: small\nmac: \"aa:bb:cc:dd:ee:01\"\nplan: tour\n")

	pages := request(t, s, http.MethodGet, "/ui/editor?display=small", nil, nil).Body.String()
	for _, want := range []string{
		`data-rename="/api/pages/solar/rename?hash=`,
		"Rename page solar?", "Plans that name it are not rewritten",
	} {
		if !strings.Contains(pages, want) {
			t.Fatalf("pages tab lacks %q:\n%s", want, pages)
		}
	}
	plans := request(t, s, http.MethodGet, "/ui/editor?tab=plans", nil, nil).Body.String()
	for _, want := range []string{
		`data-rename="/api/plans/tour/rename?hash=`,
		"Rename plan tour?", "Devices that use it are not rewritten",
	} {
		if !strings.Contains(plans, want) {
			t.Fatalf("plans tab lacks %q:\n%s", want, plans)
		}
	}
	devices := request(t, s, http.MethodGet, "/ui/editor?tab=devices", nil, nil).Body.String()
	for _, want := range []string{
		`data-rename="/api/devices/test/rename?hash=`,
		"Rename device test?", "Its state (page cursor, battery reading, menu) moves",
	} {
		if !strings.Contains(devices, want) {
			t.Fatalf("devices tab lacks %q:\n%s", want, devices)
		}
	}
	displays := request(t, s, http.MethodGet, "/ui/editor?tab=displays", nil, nil).Body.String()
	for _, want := range []string{
		`data-rename="/api/displays/small/rename?hash=`,
		"Rename display type small?", "Only the file moves; nothing is rewritten",
	} {
		if !strings.Contains(displays, want) {
			t.Fatalf("displays tab lacks %q:\n%s", want, displays)
		}
	}
}
