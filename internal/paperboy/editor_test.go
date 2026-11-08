package paperboy

import (
	"bytes"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/internal/page"
	"github.com/maehld/paperboy/internal/plan"
)

// editorServer has an image and a text display type, for the draft preview.
func editorServer(t *testing.T) *Server {
	t.Helper()
	cfg := &Config{Dir: t.TempDir(), Profiles: map[string]Profile{
		"small": {Width: 40, Height: 20, PPI: 125, Gray: true},
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

func TestPagesListAndRead(t *testing.T) {
	s, _ := testServer(t)
	writePage(t, s, "solar", "small", previewTemplate)
	writePage(t, s, "shared", "", previewTemplate)

	var list struct{ Pages []editorPageFile }
	if rec := request(t, s, http.MethodGet, "/api/pages", nil, &list); rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	if len(list.Pages) != 2 {
		t.Fatalf("pages: %+v", list.Pages)
	}
	byName := map[string]editorPageFile{}
	for _, f := range list.Pages {
		byName[f.Name] = f
	}
	if f := byName["solar"]; f.Display != "small" || f.Title != "Solar" || f.Kind != "image" || f.Grid != [2]int{2, 2} || f.Hash == "" {
		t.Fatalf("solar: %+v", f)
	}
	if f := byName["shared"]; f.Display != "" || f.Title != "Solar" {
		t.Fatalf("shared: %+v", f)
	}

	var one editorPage
	if rec := request(t, s, http.MethodGet, "/api/pages/solar?display=small", nil, &one); rec.Code != http.StatusOK {
		t.Fatalf("read: %d %s", rec.Code, rec.Body)
	}
	if one.Display != "small" || one.Template == nil || len(one.Template.Cells) != 2 || one.Hash != byName["solar"].Hash {
		t.Fatalf("read solar: %+v", one)
	}
	// Without a display type the shared page is read; the variant for small
	// does not leak into it.
	var shared editorPage
	if rec := request(t, s, http.MethodGet, "/api/pages/shared", nil, &shared); rec.Code != http.StatusOK || shared.Display != "" || shared.Template == nil {
		t.Fatalf("read shared: %d %s", rec.Code, rec.Body)
	}
	// A display type without its own variant gets the shared page, reported
	// with display "" so a save writes the file that was read.
	shared = editorPage{}
	if rec := request(t, s, http.MethodGet, "/api/pages/shared?display=small", nil, &shared); rec.Code != http.StatusOK || shared.Display != "" || shared.Path != "pages/shared.yaml" {
		t.Fatalf("read shared as small: %d %+v", rec.Code, shared)
	}
	for _, path := range []string{"/api/pages/unknown", "/api/pages/..%2Fx"} {
		if rec := request(t, s, http.MethodGet, path, nil, nil); rec.Code != http.StatusNotFound && rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d", path, rec.Code)
		}
	}
}

func TestSavePage(t *testing.T) {
	s, _ := testServer(t)
	writePage(t, s, "solar", "small", previewTemplate)

	var read editorPage
	request(t, s, http.MethodGet, "/api/pages/solar?display=small", nil, &read)

	edited := *read.Template
	edited.Title = "Sonne"
	rec := request(t, s, http.MethodPut, "/api/pages/solar?display=small", pageSave{Hash: read.Hash, Template: &edited}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	data, err := os.ReadFile(filepath.Join(s.config().Pages, "solar.small.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "title: Sonne") {
		t.Fatalf("file: %s", data)
	}
	if _, err := page.Parse(bytes.NewReader(data)); err != nil {
		t.Fatalf("written page: %v", err)
	}

	// The old version is kept under state/editor.
	backups, err := filepath.Glob(filepath.Join(s.config().Dir, "state", "editor", "pages", "solar.small", "*.yaml"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups: %v %v", backups, err)
	}
	if old, err := os.ReadFile(backups[0]); err != nil || !strings.Contains(string(old), "title: Solar") {
		t.Fatalf("backup content: %s %v", old, err)
	}

	// An outdated hash is a conflict and leaves the file alone.
	if rec := request(t, s, http.MethodPut, "/api/pages/solar?display=small",
		pageSave{Hash: read.Hash, Template: &edited}, nil); rec.Code != http.StatusConflict {
		t.Fatalf("stale hash: %d %s", rec.Code, rec.Body)
	}
	if now, _ := os.ReadFile(filepath.Join(s.config().Pages, "solar.small.yaml")); !bytes.Equal(now, data) {
		t.Fatal("a conflicted save changed the file")
	}

	// Invalid templates never reach the file.
	bad := *read.Template
	bad.Grid = [2]int{1, 1}
	if rec := request(t, s, http.MethodPut, "/api/pages/solar?display=small",
		pageSave{Template: &bad}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid template: %d %s", rec.Code, rec.Body)
	}
	if now, _ := os.ReadFile(filepath.Join(s.config().Pages, "solar.small.yaml")); !bytes.Equal(now, data) {
		t.Fatal("an invalid save changed the file")
	}

	// A typo in the display type would create a file no display uses.
	if rec := request(t, s, http.MethodPut, "/api/pages/solar?display=nope",
		pageSave{Template: &edited}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown display: %d", rec.Code)
	}
	// Unknown body fields fail instead of being dropped.
	if rec := request(t, s, http.MethodPut, "/api/pages/solar?display=small",
		`{"hash":"`+read.Hash+`","template":{"grid":[2,2],"cells":[]},"nope":1}`, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", rec.Code)
	}

	// A file that disappeared since it was read is a conflict too, so a delete
	// by hand is not undone by a stale save.
	os.Remove(filepath.Join(s.config().Pages, "solar.small.yaml"))
	if rec := request(t, s, http.MethodPut, "/api/pages/solar?display=small",
		pageSave{Hash: read.Hash, Template: &edited}, nil); rec.Code != http.StatusConflict {
		t.Fatalf("removed file: %d %s", rec.Code, rec.Body)
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
	changed.Rules[0].Rotate = true
	rec := request(t, s, http.MethodPut, "/api/plans/tour", planSave{Hash: read.Hash, Plan: &changed}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	data, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "fallback: day") || !strings.Contains(string(data), "rotate: true") {
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

	text, err := page.Parse(strings.NewReader("kind: text\ngrid: [20, 4]\ncells: [{ at: [0, 0], span: [20, 4], widget: paperboy/text, args: { text: hi } }]\n"))
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
