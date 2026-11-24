package paperboy

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
)

// Renaming moves files (ADR 0079). References are not rewritten: a plan keeps
// naming the old page, a device keeps its old display type, and the operator
// updates them by hand. The editor names what keeps the old name before it
// renames, and the server stays usable with the dangling reference.

// renameBody is the body of every rename: the new name only.
type renameBody struct {
	Name string `json:"name"`
}

// fileMove is one file a rename moves.
type fileMove struct {
	from, to string
}

// checkMove preflights one file move (ADR 0113): the source must exist and be
// readable, the target must not exist and its directory must be writable. The
// writability probe creates and removes a temporary file, so it needs no
// platform-specific permission check.
func checkMove(m fileMove) error {
	src, err := os.Open(m.from)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", m.from, err)
	}
	src.Close()
	if _, err := os.Stat(m.to); err == nil {
		return fmt.Errorf("%s already exists", m.to)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	probe, err := os.CreateTemp(filepath.Dir(m.to), ".move-*")
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", m.to, err)
	}
	probe.Close()
	if err := os.Remove(probe.Name()); err != nil {
		return fmt.Errorf("cannot write %s: %w", m.to, err)
	}
	return nil
}

// moveFiles checks every move before the first rename and then renames the
// files in order. It undoes the ones already moved when a rename fails late, so
// a page never ends up half renamed (ADR 0113).
func moveFiles(moves []fileMove) error {
	for _, m := range moves {
		if err := checkMove(m); err != nil {
			return err
		}
	}
	done := make([]fileMove, 0, len(moves))
	for _, m := range moves {
		if err := os.Rename(m.from, m.to); err != nil {
			for i := len(done) - 1; i >= 0; i-- {
				if err := os.Rename(done[i].to, done[i].from); err != nil {
					slog.Error("cannot undo a rename", "file", done[i].to, "error", err)
				}
			}
			return err
		}
		done = append(done, m)
	}
	return nil
}

// renameAnswer tells the editor the new name and the files that moved.
func renameAnswer(cfg *Config, name string, moved []fileMove) map[string]any {
	files := make([]string, 0, len(moved))
	for _, m := range moved {
		files = append(files, relativePath(cfg, m.to))
	}
	return map[string]any{"name": name, "files": files}
}

// readRename decodes the new name and refuses the request when the body is not
// a rename; the name itself is checked where it becomes a file name.
func readRename(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body renameBody
	if err := decodeBody(w, r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return "", false
	}
	return body.Name, true
}

// targetExists refuses a rename onto an existing file, so nothing is
// overwritten by a typo.
func targetExists(w http.ResponseWriter, cfg *Config, path string) bool {
	_, err := os.Stat(path)
	switch {
	case err == nil:
		writeAPIError(w, http.StatusConflict, "%s already exists", relativePath(cfg, path))
		return true
	case errors.Is(err, fs.ErrNotExist):
		return false
	default:
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return true
	}
}

// handleRenamePage moves every file of the page: the page file pages/<name>.yaml
// and its variant files pages/<name>.<label>.yaml (ADR 0113). The labels and the
// variants map stay unchanged, so no page file is rewritten. The optional hash
// guards the page file.
func (s *Server) handleRenamePage(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	old := r.PathValue("name")
	name, ok := readRename(w, r)
	if !ok {
		return
	}
	if name == old {
		writeAPIError(w, http.StatusBadRequest, "the new page name is the same as the old one")
		return
	}
	from, err := pageFilePath(cfg.Pages, old, "")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	data, err := os.ReadFile(from)
	if errors.Is(err, fs.ErrNotExist) {
		writeAPIError(w, http.StatusNotFound, "there is no page %q; only its page file can be renamed", old)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if hash := r.URL.Query().Get("hash"); hash != "" && fileHash(data) != hash {
		writeAPIError(w, http.StatusConflict, "the page changed on disk; read it again before renaming")
		return
	}
	to, err := pageFilePath(cfg.Pages, name, "")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	files, err := pageFiles(cfg.Pages)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	moves := []fileMove{{from: from, to: to}}
	for _, f := range files {
		if f.Name != old || f.Label == "" {
			continue
		}
		fromVariant, err := pageFilePath(cfg.Pages, old, f.Label)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "%v", err)
			return
		}
		toVariant, err := pageFilePath(cfg.Pages, name, f.Label)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "%v", err)
			return
		}
		moves = append(moves, fileMove{from: fromVariant, to: toVariant})
	}
	// One target of the set is enough to refuse the whole rename, so a typo
	// never overwrites a file of the new name (ADR 0113).
	for _, m := range moves {
		if targetExists(w, cfg, m.to) {
			return
		}
	}
	if err := moveFiles(moves); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, renameAnswer(cfg, name, moves))
}

// handleRenamePlan moves one plan file. Devices that name the plan keep the old
// name and show a notice page until the operator updates them (ADR 0079).
func (s *Server) handleRenamePlan(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	old := r.PathValue("name")
	name, ok := readRename(w, r)
	if !ok {
		return
	}
	if name == old {
		writeAPIError(w, http.StatusBadRequest, "the new plan name is the same as the old one")
		return
	}
	from, err := planFilePath(cfg.Plans, old)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	data, err := os.ReadFile(from)
	if errors.Is(err, fs.ErrNotExist) {
		writeAPIError(w, http.StatusNotFound, "there is no plan %q", old)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if hash := r.URL.Query().Get("hash"); hash != "" && fileHash(data) != hash {
		writeAPIError(w, http.StatusConflict, "the plan changed on disk; read it again before renaming")
		return
	}
	to, err := planFilePath(cfg.Plans, name)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	if targetExists(w, cfg, to) {
		return
	}
	moves := []fileMove{{from: from, to: to}}
	if err := moveFiles(moves); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, renameAnswer(cfg, name, moves))
}

// handleRenameDevice moves one device file and its state, so the renamed device
// keeps its page cursor and battery reading (ADR 0079). Its MAC is what
// the firmware registers with, so no device notices the rename.
func (s *Server) handleRenameDevice(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	old := r.PathValue("id")
	name, ok := readRename(w, r)
	if !ok {
		return
	}
	if name == old {
		writeAPIError(w, http.StatusBadRequest, "the new device id is the same as the old one")
		return
	}
	from, err := deviceFilePath(cfg.DevicesDir, old)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	data, err := os.ReadFile(from)
	if errors.Is(err, fs.ErrNotExist) {
		writeAPIError(w, http.StatusNotFound, "there is no device %q", old)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if hash := r.URL.Query().Get("hash"); hash != "" && fileHash(data) != hash {
		writeAPIError(w, http.StatusConflict, "the device changed on disk; read it again before renaming")
		return
	}
	to, err := deviceFilePath(cfg.DevicesDir, name)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	if targetExists(w, cfg, to) {
		return
	}
	moves := []fileMove{{from: from, to: to}}
	if err := moveFiles(moves); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	// The state belongs to the MAC, so a rename moves files only (ADR 0080).
	s.mu.Lock()
	delete(s.fwNotes, old)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, renameAnswer(cfg, name, moves))
}

// handleRenameDisplay moves one display type file. Page variants named after it
// and device files that point at it keep the old name; the device rows and the
// dashboard name the reason until the operator updates them (ADR 0079).
func (s *Server) handleRenameDisplay(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	old := r.PathValue("name")
	name, ok := readRename(w, r)
	if !ok {
		return
	}
	if name == old {
		writeAPIError(w, http.StatusBadRequest, "the new display type name is the same as the old one")
		return
	}
	from, err := displayFilePath(cfg.DisplaysDir, old)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	data, err := os.ReadFile(from)
	if errors.Is(err, fs.ErrNotExist) {
		writeAPIError(w, http.StatusNotFound, "there is no display type %q", old)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if hash := r.URL.Query().Get("hash"); hash != "" && fileHash(data) != hash {
		writeAPIError(w, http.StatusConflict, "the display type changed on disk; read it again before renaming")
		return
	}
	to, err := displayFilePath(cfg.DisplaysDir, name)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	if targetExists(w, cfg, to) {
		return
	}
	moves := []fileMove{{from: from, to: to}}
	if err := moveFiles(moves); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, renameAnswer(cfg, name, moves))
}

// renameAction is what an editor needs to rename its object: the endpoint
// (ADR 0079). The editor adds the hash it read, so a save in between does not
// make the rename stale.
type renameAction struct {
	URL string `json:"url"`
}

// pageRenameAction is the page's rename: only a page with a page file has one
// (ADR 0110).
func pageRenameAction(cfg *Config, name string) *renameAction {
	path, err := pageFilePath(cfg.Pages, name, "")
	if err != nil {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	return &renameAction{URL: "/api/pages/" + url.PathEscape(name) + "/rename"}
}

// renameForPage is the action bar's rename of the file the editor shows: only
// the page file has one. A variant renames its label in the Variant block, so a
// view shows one Rename (ADR 0079, 0110).
func renameForPage(cfg *Config, name string, label string) *renameAction {
	if label != "" {
		return nil
	}
	return pageRenameAction(cfg, name)
}
