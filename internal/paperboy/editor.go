package paperboy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/maehld/paperboy/internal/page"
	"github.com/maehld/paperboy/internal/plan"
)

// Page and plan editing (ADR 0066): the API an editor builds on. The YAML
// files stay the source of truth; these endpoints read and write them, reject
// anything the runtime would reject, refuse to overwrite a file that changed
// since it was read, and keep the last few versions under state/editor/.
const (
	editorBackups = 10
	editorMaxBody = 1 << 20
)

// editorPageFile is one page file in the list.
type editorPageFile struct {
	Name    string `json:"name"`
	Display string `json:"display,omitempty"` // empty: pages/<name>.yaml, the shared page
	Title   string `json:"title,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Grid    [2]int `json:"grid"`
	Hash    string `json:"hash"`
	Error   string `json:"error,omitempty"`
}

// editorPage is one page file with its model, the answer of a read and of a
// successful write.
type editorPage struct {
	Name     string         `json:"name"`
	Display  string         `json:"display,omitempty"`
	Path     string         `json:"path"` // relative to the instance directory
	Hash     string         `json:"hash"`
	YAML     string         `json:"yaml"`
	Template *page.Template `json:"template,omitempty"`
	Error    string         `json:"error,omitempty"`
}

// editorPlanFile is one plan file in the list.
type editorPlanFile struct {
	Name     string `json:"name"`
	Hash     string `json:"hash"`
	Rules    int    `json:"rules"`
	Fallback string `json:"fallback,omitempty"`
	Error    string `json:"error,omitempty"`
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

// pageFilePath is the file of a page for a display type; an empty display
// type is the shared page pages/<name>.yaml.
func pageFilePath(dir, name, display string) (string, error) {
	if !page.ValidName(name) {
		return "", fmt.Errorf("page name %q: only a-z, 0-9 and -", name)
	}
	if display != "" && !page.ValidName(display) {
		return "", fmt.Errorf("display type %q: only a-z, 0-9 and -", display)
	}
	if display == "" {
		return filepath.Join(dir, name+".yaml"), nil
	}
	return filepath.Join(dir, name+"."+display+".yaml"), nil
}

// existingPageFile resolves the file a display type would use, the variant
// first and the shared page second, like page.Load. It reports the display the
// file belongs to, so the editor writes back to the file it read.
func existingPageFile(dir, name, display string) (path, fileDisplay string, err error) {
	tries := []string{""}
	if display != "" {
		tries = []string{display, ""}
	}
	for _, d := range tries {
		p, err := pageFilePath(dir, name, d)
		if err != nil {
			return "", "", err
		}
		if _, err := os.Stat(p); err == nil {
			return p, d, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", "", err
		}
	}
	return "", "", fs.ErrNotExist
}

// pageFileParts splits pages/<name>[.<display>].yaml; names cannot contain
// dots, so anything else is not a page of this service.
func pageFileParts(file string) (name, display string, ok bool) {
	base, found := strings.CutSuffix(file, ".yaml")
	if !found {
		return "", "", false
	}
	name, display, _ = strings.Cut(base, ".")
	if !page.ValidName(name) || (display != "" && !page.ValidName(display)) {
		return "", "", false
	}
	return name, display, true
}

// pageFiles lists the page files of the instance directory: the data behind
// GET /api/pages and the editor's overview (ADR 0068). A file that does not
// parse still appears, with its error, so it can be found.
func pageFiles(dir string) ([]editorPageFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []editorPageFile{}, nil
		}
		return nil, err
	}
	list := []editorPageFile{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name, display, ok := pageFileParts(e.Name())
		if !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		f := editorPageFile{Name: name, Display: display, Hash: fileHash(data)}
		if t, err := page.Parse(bytes.NewReader(data)); err != nil {
			f.Error = shortError(err)
		} else {
			f.Title, f.Kind, f.Grid = t.Title, t.Kind, t.Grid
		}
		list = append(list, f)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Name != list[j].Name {
			return list[i].Name < list[j].Name
		}
		return list[i].Display < list[j].Display
	})
	return list, nil
}

func (s *Server) handlePages(w http.ResponseWriter, r *http.Request) {
	list, err := pageFiles(s.config().Pages)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pages": list})
}

// handlePage reads one page file: the variant for the display type, else the
// shared page. A broken file still answers, so an editor can show and repair
// its text.
func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	name, display := r.PathValue("name"), r.URL.Query().Get("display")
	path, fileDisplay, err := existingPageFile(cfg.Pages, name, display)
	if errors.Is(err, fs.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	out := editorPage{Name: name, Display: fileDisplay, Path: relativePath(cfg, path),
		Hash: fileHash(data), YAML: string(data)}
	if t, err := page.Parse(bytes.NewReader(data)); err != nil {
		out.Error = shortError(err)
	} else {
		out.Template = t
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSavePage validates a page, then writes it. A hash that no longer
// matches the file on disk is a conflict, so a hand edit is never lost.
func (s *Server) handleSavePage(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	name, display := r.PathValue("name"), r.URL.Query().Get("display")
	path, err := pageFilePath(cfg.Pages, name, display)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	if display != "" {
		if _, ok := cfg.Profiles[display]; !ok {
			writeAPIError(w, http.StatusBadRequest, "unknown display type %q", display)
			return
		}
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
	data, err := page.Encode(body.Template)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	cur, err := os.ReadFile(path)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if exists && body.Hash != "" && fileHash(cur) != body.Hash {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "the page changed on disk; read it again before saving",
			"hash":  fileHash(cur),
		})
		return
	}
	if !exists && body.Hash != "" {
		writeAPIError(w, http.StatusConflict, "the page was removed on disk; read it again before saving")
		return
	}
	if exists {
		s.backupFile(cfg, "pages", backupLabel(name, display), cur)
	}
	if err := writeAtomic(path, data); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	t, err := page.Parse(bytes.NewReader(data))
	if err != nil { // never expected: Encode wrote what Parse reads
		writeAPIError(w, http.StatusInternalServerError, "written page does not read back: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, editorPage{Name: name, Display: display, Path: relativePath(cfg, path),
		Hash: fileHash(data), YAML: string(data), Template: t})
}

func (s *Server) handlePlans(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	entries, err := os.ReadDir(cfg.Plans)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
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
		data, err := os.ReadFile(filepath.Join(cfg.Plans, e.Name()))
		if err != nil {
			continue
		}
		f := editorPlanFile{Name: name, Hash: fileHash(data)}
		if p, err := plan.Parse(bytes.NewReader(data), cfg.Timezone); err != nil {
			f.Error = shortError(err)
		} else {
			f.Rules, f.Fallback = len(p.Rules), p.Fallback
		}
		list = append(list, f)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	writeJSON(w, http.StatusOK, map[string]any{"plans": list})
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
	cur, err := os.ReadFile(path)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if exists && body.Hash != "" && fileHash(cur) != body.Hash {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "the plan changed on disk; read it again before saving",
			"hash":  fileHash(cur),
		})
		return
	}
	if !exists && body.Hash != "" {
		writeAPIError(w, http.StatusConflict, "the plan was removed on disk; read it again before saving")
		return
	}
	if exists {
		s.backupFile(cfg, "plans", name, cur)
	}
	if err := writeAtomic(path, data); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	p, _ := plan.Parse(bytes.NewReader(data), cfg.Timezone)
	writeJSON(w, http.StatusOK, editorPlan{Name: name, Path: relativePath(cfg, path), Hash: fileHash(data),
		YAML: string(data), Plan: p})
}

// handleDraftPreview renders a page that is not saved yet, so an editor can
// show a draft without writing a file (ADR 0066).
func (s *Server) handleDraftPreview(w http.ResponseWriter, r *http.Request) {
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
	ctx := page.Context{Now: now, Page: 1, Pages: 1, Widgets: s.widgetSource()}
	if d.Text {
		lines, err = tmpl.RenderText(d, ctx)
		return nil, lines, err
	}
	img, err = tmpl.RenderImage(d, ctx)
	return img, nil, err
}

func backupLabel(name, display string) string {
	if display == "" {
		return name
	}
	return name + "." + display
}

// backupFile keeps a copy of a file before it is overwritten and drops the
// oldest beyond editorBackups versions, so a save can be undone by hand.
func (s *Server) backupFile(cfg *Config, kind, label string, data []byte) {
	dir := filepath.Join(cfg.Dir, "state", "editor", kind, label)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Warn("cannot keep a backup of an edited file", "dir", dir, "error", err)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.yaml", time.Now().UnixNano())), data, 0o644); err != nil {
		slog.Warn("cannot keep a backup of an edited file", "dir", dir, "error", err)
		return
	}
	pruneBackups(dir, editorBackups)
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
