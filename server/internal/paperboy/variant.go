package paperboy

import (
	"bytes"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"sort"

	"github.com/maehld/paperboy/server/internal/page"
)

// A variant is a file of its own, named by the variants map of its page
// (ADR 0110). These two actions move it: a new label, or another page. Both
// rewrite the map and move the file as one step, so a failed write leaves the
// instance as it was.

// variantRename is the body of a label change: the new label and the hash of the
// page file that owns the map (ADR 0066, 0110).
type variantRename struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

// variantMove is the body of a reassignment: the page that takes the file and
// the hash of the page file that names it today.
type variantMove struct {
	Page string `json:"page"`
	Hash string `json:"hash"`
}

// variantAnswer is the answer of both actions: the file at its new place, the
// page's coverage and the editor link that opens the file for a display type it
// can render (ADR 0110).
type variantAnswer struct {
	Name     string              `json:"name"`
	File     string              `json:"file"`
	Path     string              `json:"path"`
	URL      string              `json:"url"`
	Variants map[string]string   `json:"variants,omitempty"`
	Coverage []pageCoverageEntry `json:"coverage,omitempty"`
}

// pageFileForVariant reads the page file that owns a variant and checks the hash
// the editor sent, so a move never overwrites a hand edit (ADR 0066, 0110). A
// missing page file is a 404 and a stale hash a 409.
func (s *Server) pageFileForVariant(w http.ResponseWriter, cfg *Config, name, hash string) (string, []byte, *page.Template, bool) {
	path, err := pageFilePath(cfg.Pages, name, "")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return "", nil, nil, false
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		writeAPIError(w, http.StatusNotFound, "there is no page %q; create its page file first", name)
		return "", nil, nil, false
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return "", nil, nil, false
	}
	if hash != "" && fileHash(data) != hash {
		writeJSON(w, http.StatusConflict, conflictAnswer{Error: "the page changed on disk; read it again before changing its variants", Hash: fileHash(data)})
		return "", nil, nil, false
	}
	t, err := page.Parse(bytes.NewReader(data))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "pages/%s.yaml does not read as a page: %v", name, err)
		return "", nil, nil, false
	}
	return path, data, t, true
}

// variantAnswerFor names one variant file after an action: its page, its label,
// its path and the editor link. The link picks a display type the file renders,
// so the browser opens the file and never a blank preview (ADR 0110).
func (s *Server) variantAnswerFor(cfg *Config, name, label string, data []byte) variantAnswer {
	out := variantAnswer{Name: name, File: label}
	if path, err := pageFilePath(cfg.Pages, name, label); err == nil {
		out.Path = relativePath(cfg, path)
	}
	// A file that does not parse still moves; the link falls back to the first
	// configured display type then.
	f := editorPageFile{Name: name, Label: label}
	if t, err := page.ParseVariant(bytes.NewReader(data)); err == nil {
		f.Kind, f.Grid = t.Kind, t.Grid
	}
	out.Coverage, out.Variants = s.pageCoverage(cfg, name)
	out.URL, _ = pageEditURL(f, out.Variants, cfg.Profiles, false)
	return out
}

// undoMoveFile moves a file back after a failed write. A second failure is
// logged, because the operator has to repair that by hand.
func undoMoveFile(from, to string) {
	if err := moveFiles([]fileMove{{from: from, to: to}}); err != nil {
		slog.Error("cannot undo a variant move", "file", to, "error", err)
	}
}

// restoreFile writes back what a failed step replaced.
func restoreFile(path string, data []byte) {
	if err := writeAtomic(path, data); err != nil {
		slog.Error("cannot undo a page write", "file", path, "error", err)
	}
}

// handleRenameVariant gives one variant file a new label and rewrites every
// entry of the page's variants map that names the old one (ADR 0110). A failed
// page write puts the file back.
func (s *Server) handleRenameVariant(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	name := r.PathValue("name")
	from := r.URL.Query().Get("from")
	if !page.ValidName(from) {
		writeAPIError(w, http.StatusBadRequest, "page file %q: only a-z, 0-9 and -", from)
		return
	}
	var body variantRename
	if err := decodeBody(w, r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	if body.Name == from {
		writeAPIError(w, http.StatusBadRequest, "the new label is the same as the old one")
		return
	}
	fromPath, err := pageFilePath(cfg.Pages, name, from)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	toPath, err := pageFilePath(cfg.Pages, name, body.Name)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	pagePath, pageData, pageT, ok := s.pageFileForVariant(w, cfg, name, body.Hash)
	if !ok {
		return
	}
	data, err := os.ReadFile(fromPath)
	if errors.Is(err, fs.ErrNotExist) {
		writeAPIError(w, http.StatusNotFound, "%s is missing", relativePath(cfg, fromPath))
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if targetExists(w, cfg, toPath) {
		return
	}
	renamed := false
	for display, label := range pageT.Variants {
		if label == from {
			pageT.Variants[display] = body.Name
			renamed = true
		}
	}
	var encoded []byte
	if renamed {
		encoded, err = page.Encode(pageT)
		if err != nil { // never expected: the map only gained a valid label
			writeAPIError(w, http.StatusInternalServerError, "%v", err)
			return
		}
	}
	if err := moveFiles([]fileMove{{from: fromPath, to: toPath}}); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	// A file that no entry names moves without a write, so its page keeps its
	// text and its backup count (ADR 0110).
	if renamed {
		if err := s.backupFile(cfg, "pages", name, pageData); err != nil {
			undoMoveFile(toPath, fromPath)
			writeAPIError(w, http.StatusInternalServerError, "cannot keep a backup of the page: %v", err)
			return
		}
		if err := writeAtomic(pagePath, encoded); err != nil {
			undoMoveFile(toPath, fromPath)
			writeAPIError(w, http.StatusInternalServerError, "%v", err)
			return
		}
	}
	writeJSON(w, http.StatusOK, s.variantAnswerFor(cfg, name, body.Name, data))
}

// handleMoveVariant reassigns a variant to another page: the file moves and its
// entries go from the source page's map to the target's (ADR 0110). A variant
// that no entry names moves without touching either map. A failed write puts the
// source page and the file back.
func (s *Server) handleMoveVariant(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	name := r.PathValue("name")
	from := r.URL.Query().Get("from")
	if !page.ValidName(from) {
		writeAPIError(w, http.StatusBadRequest, "page file %q: only a-z, 0-9 and -", from)
		return
	}
	var body variantMove
	if err := decodeBody(w, r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	if body.Page == name {
		writeAPIError(w, http.StatusBadRequest, "the target page is the page that owns the variant")
		return
	}
	fromPath, err := pageFilePath(cfg.Pages, name, from)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	toPath, err := pageFilePath(cfg.Pages, body.Page, from)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	pagePath, pageData, pageT, ok := s.pageFileForVariant(w, cfg, name, body.Hash)
	if !ok {
		return
	}
	data, err := os.ReadFile(fromPath)
	if errors.Is(err, fs.ErrNotExist) {
		writeAPIError(w, http.StatusNotFound, "%s is missing", relativePath(cfg, fromPath))
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	// The target owns the file after the move, so it needs a page file of its
	// own (ADR 0110).
	targetPath, err := pageFilePath(cfg.Pages, body.Page, "")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	targetData, err := os.ReadFile(targetPath)
	if errors.Is(err, fs.ErrNotExist) {
		writeAPIError(w, http.StatusBadRequest, "there is no page %q; create its page file first", body.Page)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	targetT, err := page.Parse(bytes.NewReader(targetData))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "pages/%s.yaml does not read as a page: %v", body.Page, err)
		return
	}
	var displays []string
	for display, label := range pageT.Variants {
		if label == from {
			displays = append(displays, display)
		}
	}
	sort.Strings(displays)
	// One display type has one file: the target must not send a moving type to
	// another variant (ADR 0110).
	for _, display := range displays {
		if label, named := targetT.Variants[display]; named && label != from {
			writeAPIError(w, http.StatusConflict,
				"display type %q: page %q already sends it to %s; %s would be a second file",
				display, body.Page, pageFileName(body.Page, label), pageFileName(name, from))
			return
		}
	}
	if targetExists(w, cfg, toPath) {
		return
	}
	if err := moveFiles([]fileMove{{from: fromPath, to: toPath}}); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if len(displays) == 0 {
		// An unused variant moves alone and stays unused (ADR 0110).
		writeJSON(w, http.StatusOK, s.variantAnswerFor(cfg, body.Page, from, data))
		return
	}
	for _, display := range displays {
		delete(pageT.Variants, display)
	}
	sourceOut, err := page.Encode(pageT)
	if err != nil { // never expected: the map only lost valid entries
		undoMoveFile(toPath, fromPath)
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if err := s.backupFile(cfg, "pages", name, pageData); err != nil {
		undoMoveFile(toPath, fromPath)
		writeAPIError(w, http.StatusInternalServerError, "cannot keep a backup of the page: %v", err)
		return
	}
	if err := writeAtomic(pagePath, sourceOut); err != nil {
		undoMoveFile(toPath, fromPath)
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if targetT.Variants == nil {
		targetT.Variants = map[string]string{}
	}
	for _, display := range displays {
		targetT.Variants[display] = from
	}
	targetOut, err := page.Encode(targetT)
	if err != nil { // never expected: the map only gained valid entries
		restoreFile(pagePath, pageData)
		undoMoveFile(toPath, fromPath)
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if err := s.backupFile(cfg, "pages", body.Page, targetData); err != nil {
		restoreFile(pagePath, pageData)
		undoMoveFile(toPath, fromPath)
		writeAPIError(w, http.StatusInternalServerError, "cannot keep a backup of the page: %v", err)
		return
	}
	if err := writeAtomic(targetPath, targetOut); err != nil {
		restoreFile(pagePath, pageData)
		undoMoveFile(toPath, fromPath)
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, s.variantAnswerFor(cfg, body.Page, from, data))
}
