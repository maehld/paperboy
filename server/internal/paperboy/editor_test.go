package paperboy

import (
	"bytes"
	"encoding/json"
	"errors"
	"image/png"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/server/internal/page"
	"github.com/maehld/paperboy/server/internal/plan"
)

// editorServer has an image and a text display type, for the draft preview.
func editorServer(t *testing.T) *Server {
	t.Helper()
	cfg := &Config{Dir: t.TempDir(), Profiles: map[string]Profile{
		"small": {Width: 40, Height: 20, PPI: 125, Levels: 4},
		"lcd":   {Kind: kindText, Charset: "ascii", Width: 20, Height: 4},
	}, Devices: []Device{{ID: "test", MAC: "aa:bb:cc:dd:ee:01", Profile: "small", Refresh: Duration(10 * time.Minute)}}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.Local)
	s.now = func() time.Time { return now }
	return s
}

// request calls the API and decodes a JSON answer into out, if given.
func request(t *testing.T, s *Server, method, path string, body any, out any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	switch b := body.(type) {
	case nil:
		reader = bytes.NewReader(nil)
	case string:
		reader = bytes.NewReader([]byte(b))
	default:
		data, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if out != nil && rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("%s %s: %v (%s)", method, path, err, rec.Body)
		}
	}
	return rec
}

// pageWithVariant is a page file that sends the display type small to the
// variant "hochkant" (ADR 0110).
const pageWithVariant = `
title: Solar
kind: image
grid: [2, 2]
variants:
  small: hochkant
cells:
  - { at: [0, 0], span: [2, 1], widget: paperboy/header }
  - { at: [0, 1], span: [2, 1], widget: paperboy/text, args: { text: "4,2 kW" } }
`

// variantPage is the layout of a variant: no title and no variants.
const variantPage = `
kind: image
grid: [2, 2]
cells:
  - { at: [0, 0], span: [2, 2], widget: paperboy/text, args: { text: hochkant } }
`

// textVariant is a text layout for the LCD of editorServer.
const textVariant = `
kind: text
cells:
  - { at: [0, 0], span: [20, 1], value: paperboy/title }
  - { at: [0, 1], span: [20, 1], value: paperboy/time, align: right }
  - { at: [0, 2], span: [20, 1], value: paperboy/date }
  - { at: [0, 3], span: [20, 1], value: paperboy/now, align: decimal }
`

// coverageOf returns the coverage entry of one display type.
func coverageOf(t *testing.T, page editorPage, display string) pageCoverageEntry {
	t.Helper()
	for _, c := range page.Coverage {
		if c.Display == display {
			return c
		}
	}
	t.Fatalf("coverage has no %q: %+v", display, page.Coverage)
	return pageCoverageEntry{}
}

func TestPagesListAndRead(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "", pageWithVariant)
	writePage(t, s, "solar", "hochkant", variantPage)
	writePage(t, s, "orphan", "klein", variantPage)

	var list struct{ Pages []editorPageFile }
	if rec := request(t, s, http.MethodGet, "/api/pages", nil, &list); rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	if len(list.Pages) != 3 {
		t.Fatalf("pages: %+v", list.Pages)
	}
	byFile := map[string]editorPageFile{}
	for _, f := range list.Pages {
		byFile[f.Name+"/"+f.Label] = f
	}
	if f := byFile["solar/"]; f.Label != "" || f.Title != "Solar" || f.Kind != "image" || f.Grid != [2]int{2, 2} ||
		f.Hash == "" || f.Variants["small"] != "hochkant" || f.Orphan {
		t.Fatalf("page file: %+v", f)
	}
	if f := byFile["solar/hochkant"]; f.Label != "hochkant" || f.Title != "" || f.Orphan || len(f.Variants) != 0 {
		t.Fatalf("variant: %+v", f)
	}
	// A variant without its page file is an orphan.
	if f := byFile["orphan/klein"]; !f.Orphan {
		t.Fatalf("orphan variant: %+v", f)
	}

	var one editorPage
	if rec := request(t, s, http.MethodGet, "/api/pages/solar", nil, &one); rec.Code != http.StatusOK {
		t.Fatalf("read: %d %s", rec.Code, rec.Body)
	}
	if one.File != "" || one.Template == nil || one.Template.Title != "Solar" || one.Hash != byFile["solar/"].Hash ||
		one.Variants["small"] != "hochkant" {
		t.Fatalf("read page file: %+v", one)
	}
	// The page sends small to the variant, so the coverage names that file.
	if c := coverageOf(t, one, "small"); c.File != "hochkant" || c.Missing || c.Error != "" {
		t.Fatalf("coverage small: %+v", c)
	}
	// The image page cannot serve the text display.
	if c := coverageOf(t, one, "lcd"); c.File != "" || c.Missing || !strings.Contains(c.Error, "image displays") {
		t.Fatalf("coverage lcd: %+v", c)
	}

	var variant editorPage
	if rec := request(t, s, http.MethodGet, "/api/pages/solar?file=hochkant", nil, &variant); rec.Code != http.StatusOK {
		t.Fatalf("read variant: %d %s", rec.Code, rec.Body)
	}
	if variant.File != "hochkant" || variant.Path != "pages/solar.hochkant.yaml" || variant.Template == nil ||
		variant.Template.Title != "" || len(variant.Variants) != 0 || variant.Hash != byFile["solar/hochkant"].Hash {
		t.Fatalf("read variant: %+v", variant)
	}

	for _, path := range []string{"/api/pages/unknown", "/api/pages/unknown?file=klein", "/api/pages/..%2Fx", "/api/pages/solar?file=Nope"} {
		if rec := request(t, s, http.MethodGet, path, nil, nil); rec.Code != http.StatusNotFound && rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d", path, rec.Code)
		}
	}
}

// TestPageCoverage tells a missing page file, a missing variant and a variant
// of another kind apart (ADR 0110).
func TestPageCoverage(t *testing.T) {
	s := editorServer(t)
	// The page names a variant that is not there, and its text layout does not
	// have the character grid of the LCD.
	writePage(t, s, "solar", "", `
title: Solar
kind: text
variants:
  small: fehlt
cells:
  - { at: [0, 0], span: [40, 1], value: paperboy/title }
`)
	writePage(t, s, "broken", "klein", "kind: image\ngrid: [1, 1]\ncells: [{ at: [0, 0], span: [1, 1], widget: paperboy/text }]\ntitle: nope\n")

	var read editorPage
	if rec := request(t, s, http.MethodGet, "/api/pages/solar", nil, &read); rec.Code != http.StatusOK {
		t.Fatalf("read: %d %s", rec.Code, rec.Body)
	}
	// small is named, but its file is missing.
	if c := coverageOf(t, read, "small"); !c.Missing || c.File != "fehlt" || !strings.Contains(c.Error, "solar.fehlt.yaml is missing") {
		t.Fatalf("coverage small: %+v", c)
	}
	// The page is text with another grid than the LCD.
	if c := coverageOf(t, read, "lcd"); c.Missing || c.Error == "" || !strings.Contains(c.Error, "leaves the 20x4 character grid") {
		t.Fatalf("coverage lcd: %+v", c)
	}

	// Without a page file every type is unserved.
	writePage(t, s, "lost", "klein", variantPage)
	var lost editorPage
	if rec := request(t, s, http.MethodGet, "/api/pages/lost?file=klein", nil, &lost); rec.Code != http.StatusOK {
		t.Fatalf("read orphan: %d %s", rec.Code, rec.Body)
	}
	for _, c := range lost.Coverage {
		if !c.Missing || !strings.Contains(c.Error, "pages/lost.yaml is missing") {
			t.Fatalf("coverage of an orphan: %+v", lost.Coverage)
		}
	}

	// A variant file that does not parse is named with its error, and it costs
	// only its display type.
	writePage(t, s, "solar", "", "kind: image\ngrid: [2, 2]\nvariants: {small: klein}\ncells: [{ at: [0, 0], span: [2, 2], widget: paperboy/text }]\n")
	writePage(t, s, "solar", "klein", "title: nope\nkind: image\ngrid: [2, 2]\ncells: [{ at: [0, 0], span: [2, 2], widget: paperboy/text }]\n")
	var broken editorPage
	request(t, s, http.MethodGet, "/api/pages/solar", nil, &broken)
	if c := coverageOf(t, broken, "small"); !c.Missing || c.File != "klein" || !strings.Contains(c.Error, "title belongs to the page") {
		t.Fatalf("coverage of a broken variant: %+v", c)
	}

	// A variant of the other kind costs its display type: the entry stays, the
	// file exists, and the type is not served (ADR 0110).
	writePage(t, s, "solar", "", "kind: image\ngrid: [2, 2]\nvariants: {lcd: bild}\ncells: [{ at: [0, 0], span: [2, 2], widget: paperboy/text }]\n")
	writePage(t, s, "solar", "bild", variantPage)
	var crossKind editorPage
	request(t, s, http.MethodGet, "/api/pages/solar", nil, &crossKind)
	if c := coverageOf(t, crossKind, "lcd"); c.Missing || c.File != "bild" || !strings.Contains(c.Error, "image displays") {
		t.Fatalf("coverage of a variant of the other kind: %+v", c)
	}
}

func TestSavePage(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "", pageWithVariant)
	writePage(t, s, "solar", "hochkant", variantPage)

	var read editorPage
	request(t, s, http.MethodGet, "/api/pages/solar", nil, &read)

	edited := *read.Template
	edited.Title = "Sonne"
	edited.Variants = map[string]string{"small": "hochkant"}
	rec := request(t, s, http.MethodPut, "/api/pages/solar", pageSave{Hash: read.Hash, Template: &edited}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	data, err := os.ReadFile(filepath.Join(s.config().Pages, "solar.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "title: Sonne") || !strings.Contains(string(data), "small: hochkant") {
		t.Fatalf("file: %s", data)
	}
	if _, err := page.Parse(bytes.NewReader(data)); err != nil {
		t.Fatalf("written page: %v", err)
	}

	// The old version is kept under state/editor.
	backups, err := filepath.Glob(filepath.Join(s.config().Dir, "state", "editor", "pages", "solar", "*.yaml"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups: %v %v", backups, err)
	}
	if old, err := os.ReadFile(backups[0]); err != nil || !strings.Contains(string(old), "title: Solar") {
		t.Fatalf("backup content: %s %v", old, err)
	}

	// An outdated hash is a conflict and leaves the file alone.
	if rec := request(t, s, http.MethodPut, "/api/pages/solar",
		pageSave{Hash: read.Hash, Template: &edited}, nil); rec.Code != http.StatusConflict {
		t.Fatalf("stale hash: %d %s", rec.Code, rec.Body)
	}
	if now, _ := os.ReadFile(filepath.Join(s.config().Pages, "solar.yaml")); !bytes.Equal(now, data) {
		t.Fatal("a conflicted save changed the file")
	}

	// A variant carries neither a title nor a variants map (ADR 0110).
	var variant editorPage
	request(t, s, http.MethodGet, "/api/pages/solar?file=hochkant", nil, &variant)
	withTitle := *variant.Template
	withTitle.Title = "Sonne"
	if rec := request(t, s, http.MethodPut, "/api/pages/solar?file=hochkant",
		pageSave{Hash: variant.Hash, Template: &withTitle}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("variant with a title: %d %s", rec.Code, rec.Body)
	}
	// A layout that parses is written as it is.
	saved := *variant.Template
	saved.Cells[0].Args = map[string]any{"text": "neu"}
	if rec := request(t, s, http.MethodPut, "/api/pages/solar?file=hochkant",
		pageSave{Hash: variant.Hash, Template: &saved}, nil); rec.Code != http.StatusOK {
		t.Fatalf("save variant: %d %s", rec.Code, rec.Body)
	}
	variantData, err := os.ReadFile(filepath.Join(s.config().Pages, "solar.hochkant.yaml"))
	if err != nil || !strings.Contains(string(variantData), "neu") || strings.Contains(string(variantData), "title:") {
		t.Fatalf("variant file: %s %v", variantData, err)
	}
	if _, err := page.ParseVariant(bytes.NewReader(variantData)); err != nil {
		t.Fatalf("written variant: %v", err)
	}
	if files, _ := filepath.Glob(filepath.Join(s.config().Dir, "state", "editor", "pages", "solar.hochkant", "*.yaml")); len(files) != 1 {
		t.Fatalf("variant backup: %v", files)
	}

	// Invalid templates never reach the file.
	bad := *read.Template
	bad.Grid = [2]int{1, 1}
	if rec := request(t, s, http.MethodPut, "/api/pages/solar",
		pageSave{Template: &bad}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid template: %d %s", rec.Code, rec.Body)
	}
	if now, _ := os.ReadFile(filepath.Join(s.config().Pages, "solar.yaml")); !bytes.Equal(now, data) {
		t.Fatal("an invalid save changed the file")
	}
	// Unknown body fields fail instead of being dropped.
	if rec := request(t, s, http.MethodPut, "/api/pages/solar",
		`{"hash":"`+read.Hash+`","template":{"grid":[2,2],"cells":[]},"nope":1}`, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", rec.Code)
	}

	// A file that disappeared since it was read is a conflict too, so a delete
	// by hand is not undone by a stale save.
	os.Remove(filepath.Join(s.config().Pages, "solar.yaml"))
	if rec := request(t, s, http.MethodPut, "/api/pages/solar",
		pageSave{Hash: read.Hash, Template: &edited}, nil); rec.Code != http.StatusConflict {
		t.Fatalf("removed file: %d %s", rec.Code, rec.Body)
	}
}

// TestCreateVariant creates the layout of a display type in one action: the
// file and its entry in the page's variants map (ADR 0110). The layout starts
// from the display type, so the image grid of the page never becomes a text
// variant (issue #238).
func TestCreateVariant(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "", previewTemplate)

	var read editorPage
	request(t, s, http.MethodGet, "/api/pages/solar", nil, &read)

	text, err := page.ParseVariant(strings.NewReader(textVariant))
	if err != nil {
		t.Fatal(err)
	}
	// The image grid of the open page does not fit the text display.
	wrong := *text
	wrong.Grid = [2]int{6, 8}
	if rec := request(t, s, http.MethodPost, "/api/pages/solar/variant?display=lcd",
		pageSave{Hash: read.Hash, Template: &wrong}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong grid: %d %s", rec.Code, rec.Body)
	}
	// A stale page hash is a conflict.
	if rec := request(t, s, http.MethodPost, "/api/pages/solar/variant?display=lcd",
		pageSave{Hash: "stale", Template: text}, nil); rec.Code != http.StatusConflict {
		t.Fatalf("stale hash: %d %s", rec.Code, rec.Body)
	}
	// An unknown display type is refused.
	if rec := request(t, s, http.MethodPost, "/api/pages/solar/variant?display=nope",
		pageSave{Hash: read.Hash, Template: text}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown display: %d %s", rec.Code, rec.Body)
	}

	var created editorPage
	rec := request(t, s, http.MethodPost, "/api/pages/solar/variant?display=lcd",
		pageSave{Hash: read.Hash, Template: text}, &created)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if created.File != "lcd" || created.Path != "pages/solar.lcd.yaml" || created.Template == nil ||
		created.Template.Kind != page.KindText || len(created.Template.Cells) != 4 {
		t.Fatalf("created variant: %+v", created)
	}
	if c := coverageOf(t, created, "lcd"); c.File != "lcd" || c.Missing || c.Error != "" {
		t.Fatalf("coverage lcd: %+v", c)
	}
	// The page file names the new variant and keeps its own layout.
	var pageNow editorPage
	request(t, s, http.MethodGet, "/api/pages/solar", nil, &pageNow)
	if pageNow.Variants["lcd"] != "lcd" || pageNow.Template.Kind != page.KindImage {
		t.Fatalf("page after the variant: %+v", pageNow)
	}
	// A second call does not overwrite the file.
	if rec := request(t, s, http.MethodPost, "/api/pages/solar/variant?display=lcd",
		pageSave{Hash: pageNow.Hash, Template: text}, nil); rec.Code != http.StatusConflict {
		t.Fatalf("existing variant: %d %s", rec.Code, rec.Body)
	}
	// The page needs a file to take an entry.
	if rec := request(t, s, http.MethodPost, "/api/pages/nope/variant?display=lcd",
		pageSave{Template: text}, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing page: %d %s", rec.Code, rec.Body)
	}
}

// TestSavePageCreatesAndBacksUp verifies that a new page needs no hash and
// that only the newest backups survive.
func TestSavePageCreatesAndBacksUp(t *testing.T) {
	s, _ := testServer(t)
	tmpl, err := page.Parse(strings.NewReader(previewTemplate))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.config().Pages, "neu.yaml")
	if rec := request(t, s, http.MethodPut, "/api/pages/neu", pageSave{Template: tmpl}, nil); rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("created page: %v", err)
	}
	for i := 0; i < editorBackups+3; i++ {
		tmpl.Title = strings.Repeat("x", i+1)
		if rec := request(t, s, http.MethodPut, "/api/pages/neu", pageSave{Template: tmpl}, nil); rec.Code != http.StatusOK {
			t.Fatalf("save %d: %d %s", i, rec.Code, rec.Body)
		}
	}
	backups, err := filepath.Glob(filepath.Join(s.config().Dir, "state", "editor", "pages", "neu", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != editorBackups {
		t.Fatalf("backups: %d, want %d", len(backups), editorBackups)
	}
}

func TestPlansListReadAndSave(t *testing.T) {
	s, _ := testServer(t)
	if err := os.MkdirAll(s.config().Plans, 0o755); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(s.config().Plans, "tour.yaml")
	if err := os.WriteFile(planPath, []byte("rules:\n  - name: day\n    pages: [solar]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var list struct{ Plans []editorPlanFile }
	if rec := request(t, s, http.MethodGet, "/api/plans", nil, &list); rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	if len(list.Plans) != 1 || list.Plans[0].Name != "tour" || list.Plans[0].Rules != 1 || list.Plans[0].Hash == "" {
		t.Fatalf("plans: %+v", list.Plans)
	}

	var read editorPlan
	if rec := request(t, s, http.MethodGet, "/api/plans/tour", nil, &read); rec.Code != http.StatusOK || read.Plan == nil {
		t.Fatalf("read: %d %s", rec.Code, rec.Body)
	}
	changed := *read.Plan
	changed.Fallback = "day"
	changed.Rules[0].OnRefresh = plan.RefreshFirst
	rec := request(t, s, http.MethodPut, "/api/plans/tour", planSave{Hash: read.Hash, Plan: &changed}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	data, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "fallback: day") || !strings.Contains(string(data), "on_refresh: first") {
		t.Fatalf("file:\n%s", data)
	}
	if _, err := plan.Parse(bytes.NewReader(data), nil); err != nil {
		t.Fatalf("written plan: %v", err)
	}
	if rec := request(t, s, http.MethodPut, "/api/plans/tour", planSave{Hash: read.Hash, Plan: &changed}, nil); rec.Code != http.StatusConflict {
		t.Fatalf("stale hash: %d", rec.Code)
	}
	invalid := plan.Plan{Rules: []plan.Rule{{Name: "day", Pages: []string{"solar", "solar"}}}}
	if rec := request(t, s, http.MethodPut, "/api/plans/tour", planSave{Plan: &invalid}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid plan: %d %s", rec.Code, rec.Body)
	}
	if rec := request(t, s, http.MethodGet, "/api/plans/unknown", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown plan: %d", rec.Code)
	}
}

// TestDeleteFiles removes a page, a plan, a device and a display type file. A
// hash that no longer matches refuses the delete, and every removed file stays
// under state/editor/ (ADR 0073).
// TestDeleteKeepsTheFileWhenTheBackupFails checks that a delete is not
// destructive without a copy: a state directory that cannot be written leaves
// the file in place.
func TestDeleteKeepsTheFileWhenTheBackupFails(t *testing.T) {
	s := editorServer(t)
	writePlanFile(t, s, "tour", testEditablePlan)
	blocker := filepath.Join(s.config().Dir, "state", "editor", "plans", "tour")
	if err := os.MkdirAll(filepath.Dir(blocker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rec := request(t, s, http.MethodDelete, "/api/plans/tour", nil, nil); rec.Code != http.StatusInternalServerError {
		t.Fatalf("delete without a backup: %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(s.config().Plans, "tour.yaml")); err != nil {
		t.Fatalf("the plan is gone although its backup failed: %v", err)
	}
}

func TestDeleteFiles(t *testing.T) {
	s := editorServer(t)
	backups := func(kind, label string) []string {
		t.Helper()
		files, err := filepath.Glob(filepath.Join(s.config().Dir, "state", "editor", kind, label, "*.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		return files
	}

	// A page: the stale hash is a conflict, the right one removes the file.
	writePage(t, s, "solar", "klein", variantPage)
	var read editorPage
	request(t, s, http.MethodGet, "/api/pages/solar?file=klein", nil, &read)
	pagePath := filepath.Join(s.config().Pages, "solar.klein.yaml")
	if rec := request(t, s, http.MethodDelete, "/api/pages/solar?file=klein&hash=stale", nil, nil); rec.Code != http.StatusConflict {
		t.Fatalf("stale hash: %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(pagePath); err != nil {
		t.Fatalf("a conflicted delete removed the page: %v", err)
	}
	var deleted struct{ Deleted string }
	rec := request(t, s, http.MethodDelete, "/api/pages/solar?file=klein&hash="+read.Hash, nil, &deleted)
	if rec.Code != http.StatusOK || deleted.Deleted != "pages/solar.klein.yaml" {
		t.Fatalf("delete page: %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(pagePath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("page still there: %v", err)
	}
	if files := backups("pages", "solar.klein"); len(files) != 1 {
		t.Fatalf("page backup: %v", files)
	}
	if rec := request(t, s, http.MethodDelete, "/api/pages/solar?file=klein", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("deleted page again: %d", rec.Code)
	}
	// The page file is a file of its own and is not touched by that.
	writePage(t, s, "solar", "", previewTemplate)
	if _, err := os.Stat(filepath.Join(s.config().Pages, "solar.yaml")); err != nil {
		t.Fatalf("page file: %v", err)
	}

	// A plan: no hash deletes it deliberately, like an overwrite without one.
	writePlanFile(t, s, "tour", testEditablePlan)
	if rec := request(t, s, http.MethodDelete, "/api/plans/tour", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete plan: %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(s.config().Plans, "tour.yaml")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("plan still there: %v", err)
	}
	if files := backups("plans", "tour"); len(files) != 1 {
		t.Fatalf("plan backup: %v", files)
	}

	// A device.
	model := &deviceFile{DisplayType: "small", MAC: "aa:bb:cc:dd:ee:09", Refresh: Duration(5 * time.Minute)}
	var created editorDevice
	if rec := request(t, s, http.MethodPut, "/api/devices/kueche", deviceSave{Device: model}, &created); rec.Code != http.StatusOK {
		t.Fatalf("create device: %d %s", rec.Code, rec.Body)
	}
	if rec := request(t, s, http.MethodDelete, "/api/devices/kueche?hash="+created.Hash, nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete device: %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(s.config().DevicesDir, "kueche.yaml")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("device still there: %v", err)
	}
	if files := backups("devices", "kueche"); len(files) != 1 {
		t.Fatalf("device backup: %v", files)
	}

	// A display type.
	disp := &displayFile{Kind: kindImage, Size: [2]int{40, 20}, GrayLevels: 4, PPI: 125}
	var made editorDisplayType
	if rec := request(t, s, http.MethodPut, "/api/displays/extra", displaySave{Display: disp}, &made); rec.Code != http.StatusOK {
		t.Fatalf("create display type: %d %s", rec.Code, rec.Body)
	}
	if rec := request(t, s, http.MethodDelete, "/api/displays/extra?hash="+made.Hash, nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete display type: %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(s.config().DisplaysDir, "extra.yaml")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("display type still there: %v", err)
	}
	if files := backups("displays", "extra"); len(files) != 1 {
		t.Fatalf("display type backup: %v", files)
	}
}

// TestDraftPreview renders a page that is not saved, for image and text
// displays.
func TestDraftPreview(t *testing.T) {
	s := editorServer(t)
	tmpl, err := page.Parse(strings.NewReader(previewTemplate))
	if err != nil {
		t.Fatal(err)
	}
	rec := request(t, s, http.MethodPost, "/api/preview/small", map[string]any{"template": tmpl}, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("draft image: %d %s %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 40 || img.Bounds().Dy() != 20 {
		t.Fatalf("draft size %v", img.Bounds())
	}
	// The draft endpoint writes no file.
	if files, _ := filepath.Glob(filepath.Join(s.config().Pages, "*.yaml")); len(files) != 0 {
		t.Fatalf("draft wrote files: %v", files)
	}

	text, err := page.Parse(strings.NewReader("kind: text\ncells:\n  - { at: [0, 0], span: [20, 1], value: paperboy/title }\n  - { at: [0, 1], span: [20, 1], value: paperboy/time }\n  - { at: [0, 2], span: [20, 1], value: paperboy/date }\n  - { at: [0, 3], span: [20, 1], value: paperboy/now }\n"))
	if err != nil {
		t.Fatal(err)
	}
	rec = request(t, s, http.MethodPost, "/api/preview/lcd", map[string]any{"template": text}, nil)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("draft text: %d %s", rec.Code, rec.Body)
	}
	if lines := strings.Count(rec.Body.String(), "\n"); lines != 4 {
		t.Fatalf("draft text lines: %d (%q)", lines, rec.Body)
	}
	// A variant carries no title of its own, so the editor sends the title of
	// its page file with the draft. The title value and the header then show it
	// in the preview as on the device (ADR 0110).
	titled, err := page.Parse(strings.NewReader("title: Solar\nkind: text\ncells:\n  - { at: [0, 0], span: [20, 1], value: paperboy/title }\n"))
	if err != nil {
		t.Fatal(err)
	}
	rec = request(t, s, http.MethodPost, "/api/preview/lcd", map[string]any{"template": titled}, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Solar") {
		t.Fatalf("draft title: %d %q", rec.Code, rec.Body)
	}
	// A text page does not fit an image display.
	if rec := request(t, s, http.MethodPost, "/api/preview/small", map[string]any{"template": text}, nil); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("wrong display kind: %d %s", rec.Code, rec.Body)
	}
	if rec := request(t, s, http.MethodPost, "/api/preview/nope", map[string]any{"template": tmpl}, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown display: %d", rec.Code)
	}
	if rec := request(t, s, http.MethodPost, "/api/preview/small", map[string]any{"template": &page.Template{Grid: [2]int{1, 1}}}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid draft: %d", rec.Code)
	}
}
