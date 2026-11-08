package paperboy

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"

	"github.com/maehld/paperboy/internal/page"
)

// The display type editor on top of the display type API (ADR 0071): one file
// with kind, size, gray levels, pixel density and refresh floor, plus the pages
// and devices that reference it. Like the other editors it is a server-rendered
// template and one embedded script, which works on the JSON model of the API.

//go:embed ui/display-editor.html
var displayEditorTemplate string

var displayEditorTmpl = template.Must(template.New("display-editor").Parse(displayEditorTemplate))

//go:embed ui/display-editor.js
var displayEditorJS []byte

// displayReference is one page variant or device that uses a display type.
type displayReference struct {
	Label string
	URL   string
}

// displayEditorView is the shell of the display type editor.
type displayEditorView struct {
	Version   string
	GoVersion string
	Name      string
	New       bool
	Error     string
	Pages     []displayReference
	Devices   []displayReference
	Data      template.JS
}

// displayEditorState is what the script needs besides the file itself.
type displayEditorState struct {
	Name string `json:"name"`
	New  bool   `json:"new"`
}

// handleDisplayEditor opens one display type file. A new type starts from an
// empty file, because the API creates it on save.
func (s *Server) handleDisplayEditor(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	q := r.URL.Query()
	name := q.Get("name")
	version, goVersion := editorVersion(cfg)
	v := displayEditorView{Version: version, GoVersion: goVersion, Name: name}
	if !page.ValidName(name) {
		v.Error = fmt.Sprintf("display type %q: only a-z, 0-9 and -", name)
		renderDisplayEditor(w, v, http.StatusBadRequest)
		return
	}
	path, err := displayFilePath(cfg.DisplaysDir, name)
	if err != nil {
		v.Error = err.Error()
		renderDisplayEditor(w, v, http.StatusBadRequest)
		return
	}
	missing := false
	if _, err := os.Stat(path); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			v.Error = err.Error()
			renderDisplayEditor(w, v, http.StatusInternalServerError)
			return
		}
		missing = true
	}
	v.New = missing
	if missing && q.Get("new") != "1" {
		v.Error = fmt.Sprintf("there is no display type %q; display types are files under displays/", name)
		renderDisplayEditor(w, v, http.StatusNotFound)
		return
	}
	v.Pages, v.Devices = displayReferences(cfg, name)
	data, err := json.Marshal(displayEditorState{Name: name, New: missing})
	if err != nil { // never expected: only strings and a bool
		slog.Error("display editor state encoding failed", "error", err)
		v.Error = err.Error()
		renderDisplayEditor(w, v, http.StatusInternalServerError)
		return
	}
	v.Data = template.JS(data)
	renderDisplayEditor(w, v, http.StatusOK)
}

// displayReferences lists what uses a display type: the page variants named
// after it and the device files that point at it. Renaming or deleting a
// display type is a manual step, so the editor shows the consequences.
func displayReferences(cfg *Config, name string) (pages, devices []displayReference) {
	files, err := pageFiles(cfg.Pages)
	if err != nil {
		slog.Warn("page list for the display type editor failed", "error", err)
	}
	for _, f := range files {
		if f.Display != name {
			continue
		}
		pages = append(pages, displayReference{Label: f.Name, URL: "/ui/editor/page?" + url.Values{
			"name": {f.Name}, "display": {name}}.Encode()})
	}
	list, err := deviceFileList(cfg.DevicesDir)
	if err != nil {
		slog.Warn("device list for the display type editor failed", "error", err)
	}
	for _, d := range list {
		if d.Display != name {
			continue
		}
		devices = append(devices, displayReference{Label: d.ID, URL: "/ui/editor/device?" + url.Values{"id": {d.ID}}.Encode()})
	}
	return pages, devices
}

func renderDisplayEditor(w http.ResponseWriter, v displayEditorView, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := displayEditorTmpl.Execute(w, v); err != nil {
		slog.Error("display type editor rendering failed", "error", err)
	}
}

// handleDisplayEditorJS serves the display type editor script from the binary.
func (s *Server) handleDisplayEditorJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(displayEditorJS)
}
