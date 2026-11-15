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

// TestRenamePage moves the page file only; its variants keep the old name and
// become orphans, which the assign action repairs (ADR 0110).
func TestRenamePage(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "", pageWithVariant)
	writePage(t, s, "solar", "hochkant", variantPage)
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

	// A name that is already taken is refused.
	rec = request(t, s, http.MethodPost, "/api/pages/solar/rename?hash="+page.Hash, map[string]string{"name": "other"}, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("collision: %d %s", rec.Code, rec.Body)
	}

	var out struct {
		Name    string
		Files   []string
		Orphans []string
	}
	rec = request(t, s, http.MethodPost, "/api/pages/solar/rename", map[string]string{"name": "sun"}, &out)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body)
	}
	if out.Name != "sun" || len(out.Files) != 1 || out.Files[0] != "pages/sun.yaml" {
		t.Fatalf("answer: %+v", out)
	}
	if len(out.Orphans) != 1 || out.Orphans[0] != "pages/solar.hochkant.yaml" {
		t.Fatalf("orphans: %+v", out)
	}
	if fileExists(t, filepath.Join(s.config().Pages, "solar.yaml")) || !fileExists(t, filepath.Join(s.config().Pages, "sun.yaml")) {
		t.Fatal("the page file did not move")
	}
	if !fileExists(t, filepath.Join(s.config().Pages, "solar.hochkant.yaml")) {
		t.Fatal("the variant moved with the page")
	}
	// The variant is an orphan now, and the page sun has no page file naming it.
	var list struct{ Pages []editorPageFile }
	request(t, s, http.MethodGet, "/api/pages", nil, &list)
	for _, f := range list.Pages {
		if f.Name == "solar" && f.Label == "hochkant" && !f.Orphan {
			t.Fatalf("variant is not an orphan: %+v", f)
		}
	}

	// The assign action makes the orphan a variant of the renamed page.
	rec = request(t, s, http.MethodPost, "/api/pages/sun/assign?from=solar&file=hochkant&display=small", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("assign: %d %s", rec.Code, rec.Body)
	}
	var sun editorPage
	request(t, s, http.MethodGet, "/api/pages/sun", nil, &sun)
	if sun.Variants["small"] != "hochkant" {
		t.Fatalf("variants after the assign: %+v", sun.Variants)
	}
	if !fileExists(t, filepath.Join(s.config().Pages, "sun.hochkant.yaml")) {
		t.Fatal("the variant did not move to the new page")
	}

	// The same name is not a rename.
	if rec := request(t, s, http.MethodPost, "/api/pages/sun/rename", map[string]string{"name": "sun"}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("same name: %d %s", rec.Code, rec.Body)
	}
	// A page that does not exist is a 404; so is a page that exists only as a
	// variant, because only the page file moves (ADR 0110).
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

	for _, tc := range []struct{ tab, link string }{
		{"pages", "/ui/editor/page?display=small&amp;name=solar"},
		{"plans", "/ui/editor/plan?name=tour"},
		{"devices", "/ui/editor/device?id=test"},
		{"displays", "/ui/editor/display?name=small"},
	} {
		body := request(t, s, http.MethodGet, "/ui/editor?display=small&tab="+tc.tab, nil, nil).Body.String()
		if !strings.Contains(body, `class="rowlink" data-href="`+tc.link+`"`) {
			t.Fatalf("%s tab lacks the row link %q:\n%s", tc.tab, tc.link, body)
		}
		if strings.Contains(body, "data-rename=") || strings.Contains(body, "data-delete=") {
			t.Fatalf("%s tab still acts on files:\n%s", tc.tab, body)
		}
	}

	// The editors carry the same rename, so the object can be renamed while it
	// is open; a new object has none (ADR 0079).
	for _, tc := range []struct{ url, want string }{
		{"/ui/editor/page?name=solar&display=small", `"rename":{"url":"/api/pages/solar/rename"`},
		{"/ui/editor/plan?name=tour", `"rename":{"url":"/api/plans/tour/rename"`},
		{"/ui/editor/device?id=test", `"rename":{"url":"/api/devices/test/rename"`},
		{"/ui/editor/display?name=small", `"rename":{"url":"/api/displays/small/rename"`},
	} {
		body := request(t, s, http.MethodGet, tc.url, nil, nil).Body.String()
		if !strings.Contains(body, tc.want) {
			t.Fatalf("%s lacks %q", tc.url, tc.want)
		}
	}
	for _, url := range []string{
		"/ui/editor/page?name=fresh&display=small&new=1",
		"/ui/editor/plan?name=fresh&new=1",
		"/ui/editor/device?id=fresh&new=1",
		"/ui/editor/display?name=fresh&new=1",
	} {
		if body := request(t, s, http.MethodGet, url, nil, nil).Body.String(); strings.Contains(body, `"rename":`) {
			t.Fatalf("%s offers a rename for a new object", url)
		}
	}
}
