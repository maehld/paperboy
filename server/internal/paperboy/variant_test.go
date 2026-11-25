package paperboy

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// targetPage is a page file that names no variant.
const targetPage = `
title: Ziel
kind: image
grid: [2, 2]
cells:
  - { at: [0, 0], span: [2, 2], widget: paperboy/text, args: {text: x} }
`

// backupFiles counts the versions of one edited file under state/editor/.
func backupFiles(t *testing.T, s *Server, kind, label string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(s.config().Dir, "state", "editor", kind, label, "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestRenameVariantLabel gives one variant file a new label and rewrites every
// entry that names the old one (ADR 0110).
func TestRenameVariantLabel(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "", pageWithVariant)
	writePage(t, s, "solar", "hochkant", variantPage)
	writePage(t, s, "solar", "quer", variantPage)

	var read editorPage
	if rec := request(t, s, http.MethodGet, "/api/pages/solar", nil, &read); rec.Code != http.StatusOK {
		t.Fatalf("read: %d %s", rec.Code, rec.Body)
	}
	pagePath := filepath.Join(s.config().Pages, "solar.yaml")
	before, err := os.ReadFile(pagePath)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, path string
		body       variantRename
		status     int
		want       string
	}{
		{"stale hash", "/api/pages/solar/variant/rename?from=hochkant", variantRename{Name: "neu", Hash: "stale"}, http.StatusConflict, "changed on disk"},
		{"same label", "/api/pages/solar/variant/rename?from=hochkant", variantRename{Name: "hochkant", Hash: read.Hash}, http.StatusBadRequest, "same as the old one"},
		{"unknown label", "/api/pages/solar/variant/rename?from=fehlt", variantRename{Name: "neu", Hash: read.Hash}, http.StatusNotFound, "solar.fehlt.yaml is missing"},
		{"taken label", "/api/pages/solar/variant/rename?from=hochkant", variantRename{Name: "quer", Hash: read.Hash}, http.StatusConflict, "solar.quer.yaml already exists"},
		{"invalid label", "/api/pages/solar/variant/rename?from=hochkant", variantRename{Name: "Nope", Hash: read.Hash}, http.StatusBadRequest, "only a-z, 0-9 and -"},
		{"page without a file", "/api/pages/nix/variant/rename?from=hochkant", variantRename{Name: "neu"}, http.StatusNotFound, "there is no page"},
	} {
		rec := request(t, s, http.MethodPost, tc.path, tc.body, nil)
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("%s: %d %s", tc.name, rec.Code, rec.Body)
		}
	}
	// No refused call moved a file or touched the page.
	if !fileExists(t, filepath.Join(s.config().Pages, "solar.hochkant.yaml")) ||
		fileExists(t, filepath.Join(s.config().Pages, "solar.neu.yaml")) {
		t.Fatal("a refused rename moved the file")
	}
	if now, err := os.ReadFile(pagePath); err != nil || string(now) != string(before) {
		t.Fatalf("a refused rename changed the page: %s %v", now, err)
	}

	// The label moves and the map follows it.
	var out variantAnswer
	rec := request(t, s, http.MethodPost, "/api/pages/solar/variant/rename?from=hochkant",
		variantRename{Name: "neu", Hash: read.Hash}, &out)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body)
	}
	if out.Name != "solar" || out.File != "neu" || out.Path != "pages/solar.neu.yaml" || out.Variants["small"] != "neu" {
		t.Fatalf("answer: %+v", out)
	}
	// The answer links the editor of the new file for the type it serves.
	if !strings.Contains(out.URL, "display=small") || !strings.Contains(out.URL, "file=neu") {
		t.Fatalf("editor link: %q", out.URL)
	}
	if c := coverageOf(t, editorPage{Coverage: out.Coverage}, "small"); c.File != "neu" || c.Missing || c.Error != "" {
		t.Fatalf("coverage of small: %+v", c)
	}
	if !fileExists(t, filepath.Join(s.config().Pages, "solar.neu.yaml")) ||
		fileExists(t, filepath.Join(s.config().Pages, "solar.hochkant.yaml")) {
		t.Fatal("the variant file did not move")
	}
	if files := backupFiles(t, s, "pages", "solar"); len(files) != 1 {
		t.Fatalf("page backup: %v", files)
	}

	// A file that no entry names moves without a page write, so its text and
	// its backups stay as they are (ADR 0110).
	writePage(t, s, "solar", "lcd", textVariant)
	var now editorPage
	request(t, s, http.MethodGet, "/api/pages/solar", nil, &now)
	pageBefore, err := os.ReadFile(pagePath)
	if err != nil {
		t.Fatal(err)
	}
	var unused variantAnswer
	rec = request(t, s, http.MethodPost, "/api/pages/solar/variant/rename?from=lcd",
		variantRename{Name: "tv", Hash: now.Hash}, &unused)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename unused: %d %s", rec.Code, rec.Body)
	}
	if _, named := unused.Variants["lcd"]; named {
		t.Fatalf("the unused file gained an entry: %+v", unused.Variants)
	}
	if after, err := os.ReadFile(pagePath); err != nil || string(after) != string(pageBefore) {
		t.Fatalf("an unused variant rewrote the page: %s %v", after, err)
	}
	if files := backupFiles(t, s, "pages", "solar"); len(files) != 1 {
		t.Fatalf("an unused variant added a backup: %v", files)
	}
	if !fileExists(t, filepath.Join(s.config().Pages, "solar.tv.yaml")) {
		t.Fatal("the unused variant did not move")
	}
}

// TestMoveVariant reassigns a variant to another page: the file moves and its
// entries go from the source page's map to the target's (ADR 0110).
func TestMoveVariant(t *testing.T) {
	s := editorServer(t)
	writePage(t, s, "solar", "", pageWithVariant)
	writePage(t, s, "solar", "hochkant", variantPage)
	writePage(t, s, "ziel", "", targetPage)

	var read editorPage
	if rec := request(t, s, http.MethodGet, "/api/pages/solar", nil, &read); rec.Code != http.StatusOK {
		t.Fatalf("read: %d %s", rec.Code, rec.Body)
	}

	for _, tc := range []struct {
		name string
		body variantMove
		want string
	}{
		{"stale hash", variantMove{Page: "ziel", Hash: "stale"}, "changed on disk"},
		{"target without a file", variantMove{Page: "nix", Hash: read.Hash}, "there is no page"},
		{"target is the source", variantMove{Page: "solar", Hash: read.Hash}, "page that owns the variant"},
	} {
		rec := request(t, s, http.MethodPost, "/api/pages/solar/variant/move?from=hochkant", tc.body, nil)
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusConflict {
			t.Fatalf("%s: %d %s", tc.name, rec.Code, rec.Body)
		}
		if !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("%s: %d %s", tc.name, rec.Code, rec.Body)
		}
	}

	// The target already sends the moving display type to another file: one type
	// has one file (ADR 0110).
	writePage(t, s, "ziel", "", strings.Replace(targetPage, "grid: [2, 2]", "variants:\n  small: anders\ngrid: [2, 2]", 1))
	writePage(t, s, "ziel", "anders", variantPage)
	rec := request(t, s, http.MethodPost, "/api/pages/solar/variant/move?from=hochkant",
		variantMove{Page: "ziel", Hash: read.Hash}, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("conflict: %d %s", rec.Code, rec.Body)
	}
	for _, want := range []string{`display type \"small\"`, "pages/ziel.anders.yaml", "pages/solar.hochkant.yaml"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("conflict lacks %q: %s", want, rec.Body)
		}
	}

	// A target file of that label is never overwritten.
	writePage(t, s, "ziel", "", targetPage)
	writePage(t, s, "ziel", "hochkant", variantPage)
	if rec := request(t, s, http.MethodPost, "/api/pages/solar/variant/move?from=hochkant",
		variantMove{Page: "ziel", Hash: read.Hash}, nil); rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), "pages/ziel.hochkant.yaml already exists") {
		t.Fatalf("taken file: %d %s", rec.Code, rec.Body)
	}
	os.Remove(filepath.Join(s.config().Pages, "ziel.hochkant.yaml"))

	// The move takes the file and its entry with it.
	var out variantAnswer
	rec = request(t, s, http.MethodPost, "/api/pages/solar/variant/move?from=hochkant",
		variantMove{Page: "ziel", Hash: read.Hash}, &out)
	if rec.Code != http.StatusOK {
		t.Fatalf("move: %d %s", rec.Code, rec.Body)
	}
	if out.Name != "ziel" || out.File != "hochkant" || out.Path != "pages/ziel.hochkant.yaml" || out.Variants["small"] != "hochkant" {
		t.Fatalf("answer: %+v", out)
	}
	if !strings.Contains(out.URL, "/ui/pages/ziel") || !strings.Contains(out.URL, "file=hochkant") {
		t.Fatalf("editor link: %q", out.URL)
	}
	if !fileExists(t, filepath.Join(s.config().Pages, "ziel.hochkant.yaml")) ||
		fileExists(t, filepath.Join(s.config().Pages, "solar.hochkant.yaml")) {
		t.Fatal("the variant file did not move")
	}
	var source, target editorPage
	request(t, s, http.MethodGet, "/api/pages/solar", nil, &source)
	request(t, s, http.MethodGet, "/api/pages/ziel", nil, &target)
	if _, named := source.Variants["small"]; named {
		t.Fatalf("the source still names the type: %+v", source.Variants)
	}
	if target.Variants["small"] != "hochkant" {
		t.Fatalf("the target does not name the type: %+v", target.Variants)
	}
	// Both page files keep a backup of the move.
	for _, label := range []string{"solar", "ziel"} {
		if files := backupFiles(t, s, "pages", label); len(files) != 1 {
			t.Fatalf("backup of %s: %v", label, files)
		}
	}

	// A variant that no entry names moves alone; neither page is rewritten.
	writePage(t, s, "solar", "lcd", textVariant)
	request(t, s, http.MethodGet, "/api/pages/solar", nil, &source)
	solarBefore, err := os.ReadFile(filepath.Join(s.config().Pages, "solar.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	zielBefore, err := os.ReadFile(filepath.Join(s.config().Pages, "ziel.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if rec := request(t, s, http.MethodPost, "/api/pages/solar/variant/move?from=lcd",
		variantMove{Page: "ziel", Hash: source.Hash}, nil); rec.Code != http.StatusOK {
		t.Fatalf("move unused: %d %s", rec.Code, rec.Body)
	}
	if !fileExists(t, filepath.Join(s.config().Pages, "ziel.lcd.yaml")) {
		t.Fatal("the unused variant did not move")
	}
	for path, want := range map[string][]byte{
		filepath.Join(s.config().Pages, "solar.yaml"): solarBefore,
		filepath.Join(s.config().Pages, "ziel.yaml"):  zielBefore,
	} {
		if now, err := os.ReadFile(path); err != nil || string(now) != string(want) {
			t.Fatalf("%s changed by an unused variant: %s %v", path, now, err)
		}
	}

	// A failed target write puts the file and the source page back: the test
	// replaces the atomic writer for that one path.
	writePage(t, s, "quell", "", strings.Replace(pageWithVariant, "title: Solar", "title: Quelle", 1))
	writePage(t, s, "quell", "hochkant", variantPage)
	writePage(t, s, "voll", "", targetPage)
	targetPath := filepath.Join(s.config().Pages, "voll.yaml")
	oldWrite := atomicWrite
	atomicWrite = func(path string, data []byte, dirPerm, perm fs.FileMode) error {
		if path == targetPath {
			return errors.New("target write blocked")
		}
		return oldWrite(path, data, dirPerm, perm)
	}
	defer func() { atomicWrite = oldWrite }()
	var quell editorPage
	request(t, s, http.MethodGet, "/api/pages/quell", nil, &quell)
	quellBefore, err := os.ReadFile(filepath.Join(s.config().Pages, "quell.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	vollBefore, err := os.ReadFile(filepath.Join(s.config().Pages, "voll.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if rec := request(t, s, http.MethodPost, "/api/pages/quell/variant/move?from=hochkant",
		variantMove{Page: "voll", Hash: quell.Hash}, nil); rec.Code != http.StatusInternalServerError {
		t.Fatalf("failed target write: %d %s", rec.Code, rec.Body)
	}
	if !fileExists(t, filepath.Join(s.config().Pages, "quell.hochkant.yaml")) ||
		fileExists(t, filepath.Join(s.config().Pages, "voll.hochkant.yaml")) {
		t.Fatal("the file did not move back")
	}
	for path, want := range map[string][]byte{
		filepath.Join(s.config().Pages, "quell.yaml"): quellBefore,
		filepath.Join(s.config().Pages, "voll.yaml"):  vollBefore,
	} {
		if now, err := os.ReadFile(path); err != nil || string(now) != string(want) {
			t.Fatalf("%s was not rolled back: %s %v", path, now, err)
		}
	}
}
