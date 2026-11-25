package paperboy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/maehld/paperboy/server/internal/page"
	"github.com/maehld/paperboy/server/internal/plan"
)

// Page and plan editing (ADR 0066): the API an editor builds on. The YAML
// files stay the source of truth; these endpoints read and write them, reject
// anything the runtime would reject, refuse to overwrite a file that changed
// since it was read, and keep the last few versions under state/editor/.
const (
	editorBackups = 10
	editorMaxBody = 1 << 20
)

// conflictAnswer tells an editor that the file changed on disk. Hash is the
// current content, so the editor can reload and try again.
type conflictAnswer struct {
	Error string `json:"error"`
	Hash  string `json:"hash"`
}

// editedFile is a file an editor read before it writes it; data is empty when
// the file does not exist yet.
type editedFile struct {
	path   string
	data   []byte
	exists bool
}

// readEditedFile reads the file an editor is about to replace and checks the
// hash it sent. A missing file is no error, because a save creates it. It
// answers the request itself on a conflict or a read error.
func readEditedFile(w http.ResponseWriter, path, hash, noun string) (editedFile, bool) {
	data, err := os.ReadFile(path)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return editedFile{}, false
	}
	if exists && hash != "" && fileHash(data) != hash {
		writeJSON(w, http.StatusConflict, conflictAnswer{
			Error: fmt.Sprintf("the %s changed on disk; read it again before saving", noun), Hash: fileHash(data)})
		return editedFile{}, false
	}
	if !exists && hash != "" {
		writeAPIError(w, http.StatusConflict, "the %s was removed on disk; read it again before saving", noun)
		return editedFile{}, false
	}
	return editedFile{path: path, data: data, exists: exists}, true
}

// saveEditedFile keeps a backup of the old content and writes data. It answers
// the request itself when the backup or the write fails, so no caller writes
// without one.
func (s *Server) saveEditedFile(w http.ResponseWriter, cfg *Config, f editedFile, kind, label string, data []byte, noun string) bool {
	if f.exists {
		if err := s.backupFile(cfg, kind, label, f.data); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "cannot keep a backup of the %s: %v", noun, err)
			return false
		}
	}
	if err := writeAtomic(f.path, data); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return false
	}
	return true
}

// pagesAnswer is the body of GET /api/pages.
type pagesAnswer struct {
	Pages []editorPageFile `json:"pages"`
}

// plansAnswer is the body of GET /api/plans.
type plansAnswer struct {
	Plans []editorPlanFile `json:"plans"`
}

// displaysAnswer is the body of GET /api/displays.
type displaysAnswer struct {
	Displays []editorDisplayFile `json:"displays"`
}

// deletedAnswer is the body of a successful delete.
type deletedAnswer struct {
	Deleted string `json:"deleted"`
}

// editorPageFile is one page file in the list: pages/<name>.yaml or
// pages/<name>.<label>.yaml (ADR 0110).
type editorPageFile struct {
	Name   string `json:"name"`
	Label  string `json:"label,omitempty"` // empty: the page file
	Title  string `json:"title,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Grid   [2]int `json:"grid"`
	Rows   int    `json:"rows,omitempty"` // text pages: number of rows (ADR 0111)
	Hash   string `json:"hash"`
	Error  string `json:"error,omitempty"`
	Orphan bool   `json:"orphan,omitempty"` // no page file of this name
	// textWidth is the widest row of a text page, for whether a display type
	// can show it.
	textWidth int
	// Variants is the map of the page file: the display type each label
	// serves. A variant file carries none (ADR 0110).
	Variants map[string]string `json:"variants,omitempty"`
}

// editorPage is one page file with its model, the answer of a read and of a
// successful write.
type editorPage struct {
	Name     string              `json:"name"`
	File     string              `json:"file,omitempty"` // label of the open file; empty: the page file
	Path     string              `json:"path"`           // relative to the instance directory
	Hash     string              `json:"hash"`
	YAML     string              `json:"yaml"`
	Template *page.Template      `json:"template,omitempty"`
	Variants map[string]string   `json:"variants,omitempty"` // the page's map; empty for a variant
	Coverage []pageCoverageEntry `json:"coverage"`           // every configured display type (ADR 0110)
	Error    string              `json:"error,omitempty"`
}

// pageCoverageEntry says whether a page serves one configured display type and
// through which file (ADR 0110).
type pageCoverageEntry struct {
	Display string `json:"display"`
	Kind    string `json:"kind"`              // image or text: the kind of the display type
	File    string `json:"file"`              // the label that serves it; empty: the page file
	Missing bool   `json:"missing,omitempty"` // no file serves the display type
	Error   string `json:"error,omitempty"`   // why the display type is not served
}

// editorPlanFile is one plan file in the list.
type editorPlanFile struct {
	Name     string   `json:"name"`
	Hash     string   `json:"hash"`
	Pages    []string `json:"pages,omitempty"` // every page of the plan, for the delete warning
	Triggers int      `json:"triggers,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// editorPlan is one plan file with its model.
type editorPlan struct {
	Name  string     `json:"name"`
	Path  string     `json:"path"`
	Hash  string     `json:"hash"`
	YAML  string     `json:"yaml"`
	Plan  *plan.Plan `json:"plan,omitempty"`
	Error string     `json:"error,omitempty"`
}

// pageSave is the body of a write: the model and the hash of the file the
// editor read. An empty hash creates a file or overwrites one deliberately.
type pageSave struct {
	Hash     string         `json:"hash"`
	Template *page.Template `json:"template"`
}

type planSave struct {
	Hash string     `json:"hash"`
	Plan *plan.Plan `json:"plan"`
}

func fileHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeAPIError(w http.ResponseWriter, status int, format string, args ...any) {
	writeJSON(w, status, map[string]string{"error": fmt.Sprintf(format, args...)})
}

// decodeBody reads a small JSON body strictly, so a misspelled key fails
// instead of being ignored.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, editorMaxBody))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// pageFilePath is one page file: empty label is the page file
// pages/<name>.yaml, else the variant pages/<name>.<label>.yaml (ADR 0110).
func pageFilePath(dir, name, label string) (string, error) {
	if !page.ValidName(name) {
		return "", fmt.Errorf("page name %q: only a-z, 0-9 and -", name)
	}
	if label == "" {
		return page.PagePath(dir, name)
	}
	if !page.ValidName(label) {
		return "", fmt.Errorf("page file %q: only a-z, 0-9 and -", label)
	}
	return page.VariantPath(dir, name, label)
}

// pageFileParts splits pages/<name>[.<label>].yaml; names cannot contain dots,
// so anything else is not a page of this service.
func pageFileParts(file string) (name, label string, ok bool) {
	base, found := strings.CutSuffix(file, ".yaml")
	if !found {
		return "", "", false
	}
	name, label, _ = strings.Cut(base, ".")
	if !page.ValidName(name) || (label != "" && !page.ValidName(label)) {
		return "", "", false
	}
	return name, label, true
}

// pageFiles lists the page files of the instance directory: the data behind
// GET /api/pages and the editor's overview (ADR 0068, 0110). A file that does
// not parse still appears, with its error, so it can be found. A variant whose
// page file is missing is an orphan.
// pageCount counts the pages in a directory without reading them. A page with
// variants counts once, and a file that does not parse counts like the lists
// show it (ADR 0153).
func pageCount(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	names := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if name, _, ok := pageFileParts(e.Name()); ok {
			names[name] = true
		}
	}
	return len(names), nil
}

func pageFiles(dir string) ([]editorPageFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []editorPageFile{}, nil
		}
		return nil, err
	}
	list := []editorPageFile{}
	hasPage := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name, label, ok := pageFileParts(e.Name())
		if !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		f := editorPageFile{Name: name, Label: label, Hash: fileHash(data)}
		if label == "" {
			hasPage[name] = true
		}
		var t *page.Template
		if label == "" {
			t, err = page.Parse(bytes.NewReader(data))
		} else {
			t, err = page.ParseVariant(bytes.NewReader(data))
		}
		if err != nil {
			f.Error = shortError(err)
		} else {
			f.Title, f.Kind, f.Grid = t.Title, t.Kind, t.Grid
			if t.Kind == page.KindText {
				f.Rows = 0
				for _, c := range t.Cells {
					f.Rows = max(f.Rows, c.At[1]+c.Span[1])
					f.textWidth = max(f.textWidth, c.At[0]+c.Span[0])
				}
			}
			f.Variants = t.Variants
		}
		list = append(list, f)
	}
	for i := range list {
		list[i].Orphan = list[i].Label != "" && !hasPage[list[i].Name]
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Name != list[j].Name {
			return list[i].Name < list[j].Name
		}
		return list[i].Label < list[j].Label
	})
	return list, nil
}

// pageCoverage answers which configured display type a page serves and through
// which file (ADR 0110). It reads the page file and the variant the map names
// for each type, so a missing or broken file costs one display type, not the
// page. It also returns the page's variants map.
func (s *Server) pageCoverage(cfg *Config, name string) ([]pageCoverageEntry, map[string]string) {
	coverage := []pageCoverageEntry{}
	variants := map[string]string{}
	pagePath, err := pageFilePath(cfg.Pages, name, "")
	if err != nil {
		return coverage, variants
	}
	var pageErr error
	var t *page.Template
	data, err := os.ReadFile(pagePath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		pageErr = fmt.Errorf("pages/%s.yaml is missing", name)
	case err != nil:
		pageErr = err
	default:
		t, pageErr = page.Parse(bytes.NewReader(data))
		if pageErr != nil {
			pageErr = fmt.Errorf("pages/%s.yaml: %w", name, pageErr)
		} else {
			variants = t.Variants
		}
	}
	for _, display := range displayNames(cfg) {
		p := cfg.Profiles[display]
		entry := pageCoverageEntry{Display: display, Kind: page.KindImage}
		if p.text() {
			entry.Kind = page.KindText
		}
		if pageErr != nil {
			entry.Missing = true
			entry.Error = shortError(pageErr)
			coverage = append(coverage, entry)
			continue
		}
		label, named := variants[display]
		if !named {
			// The page file serves its kind, and a text page only the types
			// with its character grid (ADR 0110).
			if err := t.Fits(s.display(cfg, p)); err != nil {
				entry.Error = shortError(err)
			}
			coverage = append(coverage, entry)
			continue
		}
		entry.File = label
		v, err := s.variantTemplate(cfg, name, label)
		if err != nil {
			entry.Missing = true
			entry.Error = shortError(err)
		} else if err := v.Fits(s.display(cfg, p)); err != nil {
			entry.Error = shortError(err)
		}
		coverage = append(coverage, entry)
	}
	return coverage, variants
}

// variantTemplate reads and validates one variant file of a page.
func (s *Server) variantTemplate(cfg *Config, name, label string) (*page.Template, error) {
	path, err := pageFilePath(cfg.Pages, name, label)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s is missing", relativePath(cfg, path))
	}
	if err != nil {
		return nil, err
	}
	t, err := page.ParseVariant(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", relativePath(cfg, path), err)
	}
	return t, nil
}

func (s *Server) handlePages(w http.ResponseWriter, r *http.Request) {
	list, err := pageFiles(s.config().Pages)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, pagesAnswer{Pages: list})
}

// handlePage reads one page file exactly: the page file without a label, else
// the variant with that label (ADR 0110). A broken file still answers, so an
// editor can show and repair its text. Every answer carries the coverage of the
// page, so the editor can say which display type no file serves.
func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	name, label := r.PathValue("name"), r.URL.Query().Get("file")
	path, err := pageFilePath(cfg.Pages, name, label)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	out := editorPage{Name: name, File: label, Path: relativePath(cfg, path),
		Hash: fileHash(data), YAML: string(data)}
	var variants map[string]string
	out.Coverage, variants = s.pageCoverage(cfg, name)
	if label == "" {
		out.Variants = variants
	}
	var t *page.Template
	if label == "" {
		t, err = page.Parse(bytes.NewReader(data))
	} else {
		t, err = page.ParseVariant(bytes.NewReader(data))
	}
	if err != nil {
		out.Error = shortError(err)
	} else {
		out.Template = t
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSavePage validates a page file or a variant, then writes it. A variant
// carries the layout without the title and without variants (ADR 0110). A hash
// that no longer matches the file on disk is a conflict, so a hand edit is
// never lost.
func (s *Server) handleSavePage(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	name, label := r.PathValue("name"), r.URL.Query().Get("file")
	path, err := pageFilePath(cfg.Pages, name, label)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	var body pageSave
	if err := decodeBody(w, r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	if body.Template == nil {
		writeAPIError(w, http.StatusBadRequest, "template missing")
		return
	}
	var data []byte
	if label == "" {
		data, err = page.Encode(body.Template)
	} else {
		data, err = page.EncodeVariant(body.Template)
	}
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	current, ok := readEditedFile(w, path, body.Hash, "page")
	if !ok {
		return
	}
	if !s.saveEditedFile(w, cfg, current, "pages", backupLabel(name, label), data, "page") {
		return
	}
	out := editorPage{Name: name, File: label, Path: relativePath(cfg, path),
		Hash: fileHash(data), YAML: string(data)}
	var variants map[string]string
	out.Coverage, variants = s.pageCoverage(cfg, name)
	if label == "" {
		out.Variants = variants
	}
	var t *page.Template
	if label == "" {
		t, err = page.Parse(bytes.NewReader(data))
	} else {
		t, err = page.ParseVariant(bytes.NewReader(data))
	}
	if err != nil { // never expected: Encode wrote what Parse reads
		writeAPIError(w, http.StatusInternalServerError, "written page does not read back: %v", err)
		return
	}
	out.Template = t
	writeJSON(w, http.StatusOK, out)
}

// handleCreateVariant writes the layout of one display type and adds its entry
// to the page in one action (ADR 0110). The body is the layout; its kind and
// grid have to fit the display type, so a variant never inherits them from
// another file (issue #238). The optional file names the label of the new file.
func (s *Server) handleCreateVariant(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	name := r.PathValue("name")
	display := r.URL.Query().Get("display")
	requested := r.URL.Query().Get("file")
	p, ok := cfg.Profiles[display]
	if !ok {
		writeAPIError(w, http.StatusBadRequest, "unknown display type %q", display)
		return
	}
	var body pageSave
	if err := decodeBody(w, r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	if body.Template == nil {
		writeAPIError(w, http.StatusBadRequest, "template missing")
		return
	}
	pagePath, err := pageFilePath(cfg.Pages, name, "")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	cur, err := os.ReadFile(pagePath)
	if errors.Is(err, fs.ErrNotExist) {
		writeAPIError(w, http.StatusNotFound, "there is no page %q; create its page file first", name)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	pageT, err := page.Parse(bytes.NewReader(cur))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "pages/%s.yaml does not read as a page: %v", name, err)
		return
	}
	if body.Hash != "" && fileHash(cur) != body.Hash {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "the page changed on disk; read it again before adding a variant",
			"hash":  fileHash(cur),
		})
		return
	}
	data, err := page.EncodeVariant(body.Template)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	written, err := page.ParseVariant(bytes.NewReader(data))
	if err != nil { // never expected: EncodeVariant wrote what ParseVariant reads
		writeAPIError(w, http.StatusInternalServerError, "written variant does not read back: %v", err)
		return
	}
	if err := written.Fits(s.display(cfg, p)); err != nil {
		writeAPIError(w, http.StatusBadRequest, "page %q for display type %q: %v", name, display, err)
		return
	}
	// A type that a file already serves is not a layout to create; the select in
	// the editor changes it instead (ADR 0110).
	if l, named := pageT.Variants[display]; named {
		namedPath, err := pageFilePath(cfg.Pages, name, l)
		if err == nil {
			if _, err := os.Stat(namedPath); err == nil {
				writeAPIError(w, http.StatusConflict, "display type %q is already served by %s; change it in the page editor",
					display, relativePath(cfg, namedPath))
				return
			}
		}
	}
	// The map decides the file name; a display type that already names a label
	// keeps it, so a missing file is repaired in place (ADR 0110).
	label := display
	if l, named := pageT.Variants[display]; named {
		label = l
	}
	if requested != "" {
		label = requested
	}
	variantPath, err := pageFilePath(cfg.Pages, name, label)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	if _, err := os.Stat(variantPath); err == nil {
		writeAPIError(w, http.StatusConflict, "%s already exists", relativePath(cfg, variantPath))
		return
	} else if !errors.Is(err, fs.ErrNotExist) {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if err := writeAtomic(variantPath, data); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if pageT.Variants == nil {
		pageT.Variants = map[string]string{}
	}
	pageT.Variants[display] = label
	pageData, err := page.Encode(pageT)
	if err != nil { // never expected: the map only gained a valid entry
		os.Remove(variantPath)
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if err := s.backupFile(cfg, "pages", name, cur); err != nil {
		os.Remove(variantPath)
		writeAPIError(w, http.StatusInternalServerError, "cannot keep a backup of the page: %v", err)
		return
	}
	if err := writeAtomic(pagePath, pageData); err != nil {
		os.Remove(variantPath)
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	out := editorPage{Name: name, File: label, Path: relativePath(cfg, variantPath),
		Hash: fileHash(data), YAML: string(data), Template: written}
	var variants map[string]string
	out.Coverage, variants = s.pageCoverage(cfg, name)
	out.Variants = variants
	writeJSON(w, http.StatusOK, out)
}

// servesSave is the body of a display type change: the hash of the page file the
// editor read and the label that should serve the type; empty removes the entry,
// so the page file serves it again (ADR 0110).
type servesSave struct {
	Hash string `json:"hash"`
	File string `json:"file"`
}

// handleSetServes changes the file that serves one display type (ADR 0110). The
// label must be a file of the page that fits the type, so the map never names a
// layout that the display cannot render.
func (s *Server) handleSetServes(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	name := r.PathValue("name")
	display := r.URL.Query().Get("display")
	var body servesSave
	if err := decodeBody(w, r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	// An entry for a type that is no longer configured can be removed, but no
	// file can be assigned to it (ADR 0110).
	p, ok := cfg.Profiles[display]
	if !ok && body.File != "" {
		writeAPIError(w, http.StatusBadRequest, "unknown display type %q", display)
		return
	}
	pagePath, err := pageFilePath(cfg.Pages, name, "")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	cur, err := os.ReadFile(pagePath)
	if errors.Is(err, fs.ErrNotExist) {
		writeAPIError(w, http.StatusNotFound, "there is no page %q; create its page file first", name)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if body.Hash != "" && fileHash(cur) != body.Hash {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "the page changed on disk; read it again before changing its display types",
			"hash":  fileHash(cur),
		})
		return
	}
	pageT, err := page.Parse(bytes.NewReader(cur))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "pages/%s.yaml does not read as a page: %v", name, err)
		return
	}
	if body.File == "" {
		delete(pageT.Variants, display)
	} else {
		variantPath, err := pageFilePath(cfg.Pages, name, body.File)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "%v", err)
			return
		}
		data, err := os.ReadFile(variantPath)
		if errors.Is(err, fs.ErrNotExist) {
			writeAPIError(w, http.StatusBadRequest, "%s is missing; create the layout first", relativePath(cfg, variantPath))
			return
		}
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "%v", err)
			return
		}
		t, err := page.ParseVariant(bytes.NewReader(data))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "%s does not read as a page: %v", relativePath(cfg, variantPath), err)
			return
		}
		if err := t.Fits(s.display(cfg, p)); err != nil {
			writeAPIError(w, http.StatusBadRequest, "page %q for display type %q: %v", name, display, err)
			return
		}
		if pageT.Variants == nil {
			pageT.Variants = map[string]string{}
		}
		pageT.Variants[display] = body.File
	}
	data, err := page.Encode(pageT)
	if err != nil { // never expected: the map only lost or gained a valid entry
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if err := s.backupFile(cfg, "pages", name, cur); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "cannot keep a backup of the page: %v", err)
		return
	}
	if err := writeAtomic(pagePath, data); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	out := editorPage{Name: name, Path: relativePath(cfg, pagePath), Hash: fileHash(data),
		YAML: string(data), Template: pageT}
	var variants map[string]string
	out.Coverage, variants = s.pageCoverage(cfg, name)
	out.Variants = variants
	writeJSON(w, http.StatusOK, out)
}

// planFiles lists the plan files of the instance directory: the data behind
// GET /api/plans and the editor's overview (ADR 0068, 0069). Times are checked
// in the global timezone.
// planCount counts the plan files without parsing them, so the overview names
// as many plans as the list shows (ADR 0153).
func planCount(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if name, found := strings.CutSuffix(e.Name(), ".yaml"); found && page.ValidName(name) {
			count++
		}
	}
	return count, nil
}

func planFiles(dir string, loc *time.Location) ([]editorPlanFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []editorPlanFile{}, nil
		}
		return nil, err
	}
	list := []editorPlanFile{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name, found := strings.CutSuffix(e.Name(), ".yaml")
		if !found || !page.ValidName(name) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		f := editorPlanFile{Name: name, Hash: fileHash(data)}
		if p, err := plan.Parse(bytes.NewReader(data), loc); err != nil {
			f.Error = shortError(err)
		} else {
			f.Triggers = len(p.Triggers)
			for _, pg := range p.Pages {
				f.Pages = append(f.Pages, pg.Name)
			}
		}
		list = append(list, f)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

func (s *Server) handlePlans(w http.ResponseWriter, r *http.Request) {
	list, err := planFiles(s.config().Plans, s.config().Timezone)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, plansAnswer{Plans: list})
}

func planFilePath(dir, name string) (string, error) {
	if !page.ValidName(name) {
		return "", fmt.Errorf("plan name %q: only a-z, 0-9 and -", name)
	}
	return filepath.Join(dir, name+".yaml"), nil
}

func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	name := r.PathValue("name")
	path, err := planFilePath(cfg.Plans, name)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	out := editorPlan{Name: name, Path: relativePath(cfg, path), Hash: fileHash(data), YAML: string(data)}
	if p, err := plan.Parse(bytes.NewReader(data), cfg.Timezone); err != nil {
		out.Error = shortError(err)
	} else {
		out.Plan = p
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSavePlan validates a plan, then writes it. Time windows are checked in
// the global timezone, like the runtime reads them.
func (s *Server) handleSavePlan(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	name := r.PathValue("name")
	path, err := planFilePath(cfg.Plans, name)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	var body planSave
	if err := decodeBody(w, r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	if body.Plan == nil {
		writeAPIError(w, http.StatusBadRequest, "plan missing")
		return
	}
	data, err := plan.Encode(body.Plan)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	if _, err := plan.Parse(bytes.NewReader(data), cfg.Timezone); err != nil { // never expected
		writeAPIError(w, http.StatusInternalServerError, "written plan does not read back: %v", err)
		return
	}
	current, ok := readEditedFile(w, path, body.Hash, "plan")
	if !ok {
		return
	}
	if !s.saveEditedFile(w, cfg, current, "plans", name, data, "plan") {
		return
	}
	p, _ := plan.Parse(bytes.NewReader(data), cfg.Timezone)
	writeJSON(w, http.StatusOK, editorPlan{Name: name, Path: relativePath(cfg, path), Hash: fileHash(data),
		YAML: string(data), Plan: p})
}

// editorDevice is one device file with its model, the answer of a read and of a
// successful write (ADR 0070).
type editorDevice struct {
	ID     string      `json:"id"`
	Path   string      `json:"path"` // relative to the instance directory
	Hash   string      `json:"hash"`
	YAML   string      `json:"yaml"`
	Device *deviceFile `json:"device,omitempty"`
	Error  string      `json:"error,omitempty"`
	// Pairing tells the editor whether the display may take a key (ADR 0109).
	Pairing pairingInfo `json:"pairing"`
}

// deviceSave is the body of a device write: the model and the hash of the file
// the editor read, like a page or a plan save (ADR 0066).
type deviceSave struct {
	Hash   string      `json:"hash"`
	Device *deviceFile `json:"device"`
}

// handleDevice reads one device file. A broken file still answers, so an editor
// can show and repair it.
func (s *Server) handleDevice(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	id := r.PathValue("id")
	path, err := deviceFilePath(cfg.DevicesDir, id)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	out := editorDevice{ID: id, Path: relativePath(cfg, path), Hash: fileHash(data), YAML: string(data)}
	if f, err := parseDeviceFile(data); err != nil {
		out.Error = shortError(err)
	} else {
		out.Device = &f
		out.Pairing = s.pairingOf(f.MAC)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSaveDevice validates a device like the runtime would on a reload, then
// writes the file. The plan stays on the device, so assigning one is a single
// small write that never touches paperboy.yaml (ADR 0070).
func (s *Server) handleSaveDevice(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	id := r.PathValue("id")
	path, err := deviceFilePath(cfg.DevicesDir, id)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	var body deviceSave
	if err := decodeBody(w, r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	if body.Device == nil {
		writeAPIError(w, http.StatusBadRequest, "device missing")
		return
	}
	data, err := encodeDeviceFile(body.Device)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	written, err := parseDeviceFile(data)
	if err != nil { // never expected: encodeDeviceFile wrote what parseDeviceFile reads
		writeAPIError(w, http.StatusInternalServerError, "written device does not read back: %v", err)
		return
	}
	if err := written.validate(id); err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	dev := written.device(id, cfg.Battery)
	if err := cfg.validateDevice(&dev); err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	for i := range cfg.Devices {
		if other := &cfg.Devices[i]; other.ID != id && other.MAC == dev.MAC {
			writeAPIError(w, http.StatusBadRequest, "MAC %s is already used by %s", dev.MAC, other.ID)
			return
		}
	}
	current, ok := readEditedFile(w, path, body.Hash, "device")
	if !ok {
		return
	}
	if !s.saveEditedFile(w, cfg, current, "devices", id, data, "device") {
		return
	}
	writeJSON(w, http.StatusOK, editorDevice{ID: id, Path: relativePath(cfg, path), Hash: fileHash(data),
		YAML: string(data), Device: &written, Pairing: s.pairingOf(written.MAC)})
}

// editorDisplayType is one display type file with its model, the answer of a
// read and of a successful write (ADR 0071).
type editorDisplayType struct {
	Name    string       `json:"name"`
	Path    string       `json:"path"` // relative to the instance directory
	Hash    string       `json:"hash"`
	YAML    string       `json:"yaml"`
	Display *displayFile `json:"display,omitempty"`
	Error   string       `json:"error,omitempty"`
}

// displaySave is the body of a display type write: the model and the hash of
// the file the editor read, like a page, plan or device save (ADR 0066).
type displaySave struct {
	Hash    string       `json:"hash"`
	Display *displayFile `json:"display"`
}

// handleDisplays lists the display type files (ADR 0071).
func (s *Server) handleDisplays(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	list, err := displayFileList(cfg.DisplaysDir)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, displaysAnswer{Displays: list})
}

// handleDisplay reads one display type file. A broken file still answers, so an
// editor can show and repair it.
func (s *Server) handleDisplay(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	name := r.PathValue("name")
	path, err := displayFilePath(cfg.DisplaysDir, name)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	out := editorDisplayType{Name: name, Path: relativePath(cfg, path), Hash: fileHash(data), YAML: string(data)}
	if f, err := parseDisplayFile(data); err != nil {
		out.Error = shortError(err)
	} else {
		out.Display = &f
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSaveDisplay validates a display type like the runtime would on a
// reload, then writes the file. The name comes from the path and cannot be
// changed here: it is the suffix of page files and a value in device files.
func (s *Server) handleSaveDisplay(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	name := r.PathValue("name")
	path, err := displayFilePath(cfg.DisplaysDir, name)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	var body displaySave
	if err := decodeBody(w, r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	if body.Display == nil {
		writeAPIError(w, http.StatusBadRequest, "display missing")
		return
	}
	if body.Display.WidthMM != nil {
		writeAPIError(w, http.StatusBadRequest, "display type %s: width_mm was replaced by ppi (ADR 0050): use ppi = pixels × 25.4 / mm of the same side", name)
		return
	}
	data, err := encodeDisplayFile(body.Display)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	written, err := parseDisplayFile(data)
	if err != nil { // never expected: encodeDisplayFile wrote what parseDisplayFile reads
		writeAPIError(w, http.StatusInternalServerError, "written display type does not read back: %v", err)
		return
	}
	if err := written.check(name); err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	p := written.profile()
	if err := validateProfile(name, &p); err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	current, ok := readEditedFile(w, path, body.Hash, "display type")
	if !ok {
		return
	}
	if !s.saveEditedFile(w, cfg, current, "displays", name, data, "display type") {
		return
	}
	writeJSON(w, http.StatusOK, editorDisplayType{Name: name, Path: relativePath(cfg, path), Hash: fileHash(data),
		YAML: string(data), Display: &written})
}

// handleDeletePage removes one page file (ADR 0073, 0110). The label names the
// variant exactly; without one the page file is removed. Nothing resolves, so a
// delete hits the file the row names and nothing else.
func (s *Server) handleDeletePage(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	name, label := r.PathValue("name"), r.URL.Query().Get("file")
	path, err := pageFilePath(cfg.Pages, name, label)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	s.deleteFile(w, cfg, "pages", backupLabel(name, label), path, r.URL.Query().Get("hash"), "page")
}

// handleDeletePlan removes one plan file.
func (s *Server) handleDeletePlan(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	name := r.PathValue("name")
	path, err := planFilePath(cfg.Plans, name)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	s.deleteFile(w, cfg, "plans", name, path, r.URL.Query().Get("hash"), "plan")
}

// handleDeleteDevice removes one device file. Its state stays under the MAC,
// so a recreated file picks the page cursor up again (ADR 0080).
func (s *Server) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	id := r.PathValue("id")
	path, err := deviceFilePath(cfg.DevicesDir, id)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	s.deleteFile(w, cfg, "devices", id, path, r.URL.Query().Get("hash"), "device")
}

// handleDeleteDisplay removes one display type file. Its page variants and
// device files stay and are listed with their error afterwards.
func (s *Server) handleDeleteDisplay(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	name := r.PathValue("name")
	path, err := displayFilePath(cfg.DisplaysDir, name)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	s.deleteFile(w, cfg, "displays", name, path, r.URL.Query().Get("hash"), "display type")
}

// deleteEditedFile removes one file an editor read, after a copy under
// state/editor/ (ADR 0073). A hash that no longer matches refuses the delete,
// so a hand edit is never lost. References are not checked: the editor names
// them in its confirmation and the operator decides.
func (s *Server) deleteEditedFile(cfg *Config, kind, label, path, hash, noun string) (int, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return http.StatusNotFound, fmt.Errorf("there is no such %s", noun)
	}
	if err != nil {
		return http.StatusInternalServerError, err
	}
	if hash != "" && fileHash(data) != hash {
		return http.StatusConflict, fmt.Errorf("the %s changed on disk; read it again before deleting", noun)
	}
	if err := s.backupFile(cfg, kind, label, data); err != nil {
		return http.StatusInternalServerError, fmt.Errorf("cannot keep a backup of the %s: %w", noun, err)
	}
	if err := os.Remove(path); err != nil {
		return http.StatusInternalServerError, err
	}
	return 0, nil
}

// deleteFile answers a delete: the removed path as JSON, or the status and the
// reason why nothing was removed.
func (s *Server) deleteFile(w http.ResponseWriter, cfg *Config, kind, label, path, hash, noun string) {
	status, err := s.deleteEditedFile(cfg, kind, label, path, hash, noun)
	if err != nil {
		writeAPIError(w, status, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, deletedAnswer{Deleted: relativePath(cfg, path)})
}

// handleDisplayHeaderPreview renders the header band of a display type as a
// draft, so the display type editor shows what its parts draw before a save
// (ADR 0168). The band is the top strip of the panel; the page title stands in
// for the type's pages.
func (s *Server) handleDisplayHeaderPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	cfg := s.config()
	name := r.PathValue("name")
	profile, ok := cfg.Profiles[name]
	if !ok {
		writeAPIError(w, http.StatusNotFound, "unknown display type %q", name)
		return
	}
	var body struct {
		Header *page.Header `json:"header"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	if body.Header == nil || len(body.Header.Parts) == 0 {
		writeAPIError(w, http.StatusBadRequest, "header needs at least one part")
		return
	}
	if err := body.Header.Validate(); err != nil {
		writeAPIError(w, http.StatusBadRequest, "header: %v", err)
		return
	}
	if profile.text() {
		writeAPIError(w, http.StatusBadRequest, "a text display type has no header band")
		return
	}
	img, err := s.renderBand(cfg, name, body.Header, s.now())
	if err != nil {
		writeAPIError(w, http.StatusUnprocessableEntity, "%v", err)
		return
	}
	writePNG(w, encodePNG(img))
}

// renderBand composes the band strip of a display type with a draft header. It
// renders the whole page first, because a widget sizes its text on the panel
// height, and keeps the top strip. A panicking widget becomes an error like on
// the device path.
func (s *Server) renderBand(cfg *Config, display string, header *page.Header, now time.Time) (img *image.Paletted, err error) {
	defer func() {
		if v := recover(); v != nil {
			slog.Error("header preview panicked", "display", display, "panic", v, "stack", string(debug.Stack()))
			img, err = nil, fmt.Errorf("header preview panicked: %v", v)
		}
	}()
	d := s.display(cfg, cfg.Profiles[display])
	d.Header = header
	tmpl := &page.Template{Title: display}
	status := s.statusSnapshot(cfg, display, "", selectedDeviceIDs(tmpl, d.Header), now)
	ctx := page.Context{Now: now, Page: 1, Pages: 1, Widgets: s.widgetSource(), Values: s.valueSource(), ServerStatus: status}
	full, err := tmpl.RenderImage(d, ctx)
	if err != nil {
		return nil, err
	}
	band := image.NewPaletted(image.Rect(0, 0, d.Width, tmpl.BandHeight(d)), full.Palette)
	draw.Draw(band, band.Bounds(), full, image.Point{}, draw.Src)
	return band, nil
}

// handleDraftPreview renders a page that is not saved yet, so an editor can
// show a draft without writing a file (ADR 0066).
func (s *Server) handleDraftPreview(w http.ResponseWriter, r *http.Request) {
	// A draft is composed from plugins and belongs to the instance (ADR 0096):
	// no shared cache may keep it.
	w.Header().Set("Cache-Control", "no-store")
	cfg := s.config()
	display := r.PathValue("display")
	if _, ok := cfg.Profiles[display]; !ok {
		writeAPIError(w, http.StatusNotFound, "unknown display type %q", display)
		return
	}
	var body struct {
		Template *page.Template `json:"template"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	if body.Template == nil {
		writeAPIError(w, http.StatusBadRequest, "template missing")
		return
	}
	data, err := page.Encode(body.Template)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	tmpl, err := page.Parse(bytes.NewReader(data))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	now := s.now()
	if v := r.URL.Query().Get("at"); v != "" {
		loc := cfg.Timezone
		if loc == nil {
			loc = time.Local
		}
		at, err := time.ParseInLocation(atLayout, v, loc)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "at: expected %s, got %q", atLayout, v)
			return
		}
		now = at
	}
	img, lines, err := s.renderDraft(cfg, display, tmpl, now)
	if err != nil {
		writeAPIError(w, http.StatusUnprocessableEntity, "%v", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if lines != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(strings.Join(lines, "\n") + "\n"))
		return
	}
	writePNG(w, encodePNG(img))
}

// renderDraft composes a template without a file; a panicking widget becomes
// an error like on the device path.
func (s *Server) renderDraft(cfg *Config, display string, tmpl *page.Template, now time.Time) (img *image.Paletted, lines []string, err error) {
	defer func() {
		if v := recover(); v != nil {
			slog.Error("draft rendering panicked", "display", display, "panic", v, "stack", string(debug.Stack()))
			img, lines, err = nil, nil, fmt.Errorf("page rendering panicked: %v", v)
		}
	}()
	d := s.display(cfg, cfg.Profiles[display])
	status := s.statusSnapshot(cfg, display, "", selectedDeviceIDs(tmpl, d.Header), now)
	ctx := page.Context{Now: now, Page: 1, Pages: 1, Widgets: s.widgetSource(), Values: s.valueSource(), ServerStatus: status}
	if d.Text {
		text, err := tmpl.RenderText(d, ctx)
		return nil, text.Lines, err
	}
	img, err = tmpl.RenderImage(d, ctx)
	return img, nil, err
}

func backupLabel(name, label string) string {
	if label == "" {
		return name
	}
	return name + "." + label
}

// backupFile keeps a copy of a file before it is overwritten and drops the
// oldest beyond editorBackups versions, so a save can be undone by hand. It
// returns an error when the copy cannot be kept; the caller then leaves the
// file unchanged.
func (s *Server) backupFile(cfg *Config, kind, label string, data []byte) error {
	dir := filepath.Join(cfg.Dir, "state", "editor", kind, label)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// The wall clock names the copy: the name must stay unique even when the
	// server clock is pinned, and it orders the versions for pruning.
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.yaml", time.Now().UnixNano())), data, 0o644); err != nil {
		return err
	}
	pruneBackups(dir, editorBackups)
	return nil
}

func pruneBackups(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for len(names) > keep {
		os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}

// relativePath names a file the way an operator sees it in the instance
// directory; a file outside it keeps its full path.
func relativePath(cfg *Config, path string) string {
	rel, err := filepath.Rel(cfg.Dir, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return filepath.ToSlash(rel)
}
