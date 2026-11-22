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

// TestRenamePage moves the page file and every variant file, mapped or not, in
// one step; the labels and the variants map stay as they are (ADR 0113).
func TestRenamePage(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "", pageWithVariant)
	writePage(t, s, "solar", "hochkant", variantPage)
	writePage(t, s, "solar", "quer", variantPage)
	writePage(t, s, "other", "", previewTemplate)

	var page editorPage
	if rec := request(t, s, http.MethodGet, "/api/pages/solar", nil, &page); rec.Code != http.StatusOK {
		t.Fatalf("read: %d %s", rec.Code, rec.Body)
	}
	if page.Hash == "" {
		t.Fatal("the read page carries no hash")
	}

	// A stale hash is a conflict.
	rec := request(t, s, http.MethodPost, "/api/pages/solar/rename?hash=stale", map[string]string{"name": "sun"}, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale hash: %d %s", rec.Code, rec.Body)
	}
	if !fileExists(t, filepath.Join(s.config().Pages, "solar.yaml")) {
		t.Fatal("a refused rename moved a file")
	}

	// A page name that is already taken is refused.
	rec = request(t, s, http.MethodPost, "/api/pages/solar/rename?hash="+page.Hash, map[string]string{"name": "other"}, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("collision: %d %s", rec.Code, rec.Body)
	}

	// One taken variant target refuses the whole set, before anything moves.
	writePage(t, s, "sun", "quer", variantPage)
	rec = request(t, s, http.MethodPost, "/api/pages/solar/rename?hash="+page.Hash, map[string]string{"name": "sun"}, nil)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "pages/sun.quer.yaml already exists") {
		t.Fatalf("taken variant target: %d %s", rec.Code, rec.Body)
	}
	for _, name := range []string{"solar.yaml", "solar.hochkant.yaml", "solar.quer.yaml"} {
		if !fileExists(t, filepath.Join(s.config().Pages, name)) {
			t.Fatalf("a refused rename moved %s", name)
		}
	}
	if fileExists(t, filepath.Join(s.config().Pages, "sun.yaml")) {
		t.Fatal("a refused rename moved the page file")
	}
	if err := os.Remove(filepath.Join(s.config().Pages, "sun.quer.yaml")); err != nil {
		t.Fatal(err)
	}

	// The page file and both variants move as one set; the answer names each
	// file at its new place.
	var out struct {
		Name  string
		Files []string
	}
	rec = request(t, s, http.MethodPost, "/api/pages/solar/rename", map[string]string{"name": "sun"}, &out)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body)
	}
	want := []string{"pages/sun.yaml", "pages/sun.hochkant.yaml", "pages/sun.quer.yaml"}
	if out.Name != "sun" || len(out.Files) != len(want) {
		t.Fatalf("answer: %+v", out)
	}
	for i := range want {
		if out.Files[i] != want[i] {
			t.Fatalf("answer files: %+v", out.Files)
		}
	}
	pages := s.config().Pages
	for _, name := range []string{"sun.yaml", "sun.hochkant.yaml", "sun.quer.yaml"} {
		if !fileExists(t, filepath.Join(pages, name)) {
			t.Fatalf("%s did not move", name)
		}
	}
	for _, name := range []string{"solar.yaml", "solar.hochkant.yaml", "solar.quer.yaml"} {
		if fileExists(t, filepath.Join(pages, name)) {
			t.Fatalf("%s stayed under the old name", name)
		}
	}
	// The variants map is not rewritten: small still names hochkant.
	var moved editorPage
	request(t, s, http.MethodGet, "/api/pages/sun", nil, &moved)
	if moved.Variants["small"] != "hochkant" {
		t.Fatalf("the variants map changed: %+v", moved.Variants)
	}

	// The same name is not a rename.
	if rec := request(t, s, http.MethodPost, "/api/pages/sun/rename", map[string]string{"name": "sun"}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("same name: %d %s", rec.Code, rec.Body)
	}
	// A page that does not exist is a 404; so is the old page, whose files all
	// moved.
	for _, name := range []string{"nope", "solar"} {
		if rec := request(t, s, http.MethodPost, "/api/pages/"+name+"/rename", map[string]string{"name": "x"}, nil); rec.Code != http.StatusNotFound {
			t.Fatalf("missing page %q: %d %s", name, rec.Code, rec.Body)
		}
	}
	// An invalid new name is a 400.
	if rec := request(t, s, http.MethodPost, "/api/pages/sun/rename", map[string]string{"name": "Not Valid"}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid name: %d %s", rec.Code, rec.Body)
	}
}

// TestMoveFilesPreflight checks that every move is preflighted: an impossible
// move fails and leaves the whole set where it is (ADR 0113).
func TestMoveFilesPreflight(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "a.yaml")
	if err := os.WriteFile(source, []byte("page"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A target directory that does not exist refuses the move.
	to := filepath.Join(dir, "missing", "b.yaml")
	err := moveFiles([]fileMove{{from: source, to: to}})
	if err == nil || !strings.Contains(err.Error(), to) {
		t.Fatalf("missing directory: %v", err)
	}
	if !fileExists(t, source) || fileExists(t, to) {
		t.Fatal("a failed preflight moved the file")
	}

	// One impossible move in a set stops the set before the first rename.
	second := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(second, []byte("variant"), 0o644); err != nil {
		t.Fatal(err)
	}
	taken := filepath.Join(dir, "taken.yaml")
	if err := os.WriteFile(taken, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(dir, "moved.yaml")
	err = moveFiles([]fileMove{
		{from: source, to: moved},
		{from: second, to: taken},
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("taken target: %v", err)
	}
	if !fileExists(t, source) || !fileExists(t, second) || fileExists(t, moved) {
		t.Fatal("a failed preflight moved a file of the set")
	}
	if data, err := os.ReadFile(taken); err != nil || string(data) != "keep" {
		t.Fatalf("a failed preflight overwrote the target: %s %v", data, err)
	}

	// A move that passes every check happens.
	if err := moveFiles([]fileMove{{from: source, to: moved}}); err != nil {
		t.Fatal(err)
	}
	if fileExists(t, source) || !fileExists(t, moved) {
		t.Fatal("a valid move did not happen")
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

// TestRenameDevice moves only the device file: the state belongs to the MAC, so
// the renamed device keeps its cursor and battery reading (ADR 0080).
func TestRenameDevice(t *testing.T) {
	s := editorServer(t)
	writeDeviceFile(t, s.config().Dir, "kueche", "display_type: small\nmac: \"aa:bb:cc:dd:ee:02\"\n")

	var dev editorDevice
	if rec := request(t, s, http.MethodGet, "/api/devices/kueche", nil, &dev); rec.Code != http.StatusOK {
		t.Fatalf("read: %d %s", rec.Code, rec.Body)
	}
	s.state["AABBCCDDEE02"] = &deviceState{Page: 3, BatteryVoltage: "3.9"}

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
	if st := s.state["AABBCCDDEE02"]; st == nil || st.Page != 3 || st.BatteryVoltage != "3.9" {
		t.Fatalf("state did not stay with the MAC: %+v", s.state)
	}
	// A name that another device file uses is refused.
	writeDeviceFile(t, s.config().Dir, "again", "display_type: small\nmac: \"aa:bb:cc:dd:ee:03\"\n")
	writeDeviceFile(t, s.config().Dir, "other", "display_type: small\nmac: \"aa:bb:cc:dd:ee:04\"\n")
	var again editorDevice
	request(t, s, http.MethodGet, "/api/devices/again", nil, &again)
	if rec := request(t, s, http.MethodPost, "/api/devices/again/rename?hash="+again.Hash, map[string]string{"name": "other"}, nil); rec.Code != http.StatusConflict {
		t.Fatalf("existing device name: %d %s", rec.Code, rec.Body)
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

// TestEditorRenameButtons: the overview no longer carries Rename or Delete;
// the editors do, and the confirmation names what keeps the old name (ADR
// 0079, 0083).
func TestEditorRenameButtons(t *testing.T) {
	s := planEditorServer(t)
	s.config().Devices[0].Plan = "tour"
	writeDisplayFile(t, s.config().Dir, "small", "kind: image\nsize: [40, 20]\nppi: 125\n")
	writeDeviceFile(t, s.config().Dir, "test", "display_type: small\nmac: \"aa:bb:cc:dd:ee:01\"\nplan: tour\n")

	for _, tc := range []struct{ path, link string }{
		{"/ui/pages", "/ui/pages/solar?display=small"},
		{"/ui/plans", "/ui/plans/tour"},
		{"/ui/devices", "/ui/devices/test"},
		{"/ui/display-types", "/ui/display-types/small"},
	} {
		body := request(t, s, http.MethodGet, tc.path, nil, nil).Body.String()
		if !strings.Contains(body, `class="rowlink" data-href="`+tc.link+`"`) {
			t.Fatalf("%s lacks the row link %q:\n%s", tc.path, tc.link, body)
		}
		if strings.Contains(body, "data-rename=") || strings.Contains(body, "data-delete=") {
			t.Fatalf("%s still acts on files:\n%s", tc.path, body)
		}
	}

	// The editors carry the same rename, so the object can be renamed while it
	// is open; a new object has none (ADR 0079).
	for _, tc := range []struct{ url, want string }{
		{"/ui/pages/solar?display=small", `"rename":{"url":"/api/pages/solar/rename"`},
		{"/ui/plans/tour", `"rename":{"url":"/api/plans/tour/rename"`},
		{"/ui/devices/test", `"rename":{"url":"/api/devices/test/rename"`},
		{"/ui/display-types/small", `"rename":{"url":"/api/displays/small/rename"`},
	} {
		body := request(t, s, http.MethodGet, tc.url, nil, nil).Body.String()
		if !strings.Contains(body, tc.want) {
			t.Fatalf("%s lacks %q", tc.url, tc.want)
		}
	}
	for _, url := range []string{
		"/ui/pages/fresh?display=small&new=1",
		"/ui/plans/fresh?new=1",
		"/ui/devices/fresh?new=1",
		"/ui/display-types/fresh?new=1",
	} {
		if body := request(t, s, http.MethodGet, url, nil, nil).Body.String(); strings.Contains(body, `"rename":`) {
			t.Fatalf("%s offers a rename for a new object", url)
		}
	}
}
