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

	"github.com/maehld/paperboy/server/internal/page"
)

// The display type editor on top of the display type API (ADR 0071): one file
// with kind, size, gray levels, pixel density and refresh floor, plus the pages
// and devices that reference it. Like the other editors it is a server-rendered
// template and one embedded script, which works on the JSON model of the API.

//go:embed ui/display-editor.html
var displayEditorTemplate string

var displayEditorTmpl = pageTemplate("display-editor", displayEditorTemplate, nil)

//go:embed ui/display-editor.js
var displayEditorJS []byte

// displayReference is one page or device that uses a display type.
type displayReference struct {
	Label string
	URL   string
}

// displayEditorView is the shell of the display type editor.
type displayEditorView struct {
	Version    string
	GoVersion  string
	Nav        []navItem // the rail, with the active section (ADR 0152)
	Breadcrumb []crumb   // the path to this page
	Name       string
	New        bool
	Error      string
	Pages      []displayReference
	Devices    []displayReference
	Data       template.JS
}

// displayEditorState is what the script needs besides the file itself.
type displayEditorState struct {
	Name    string        `json:"name"`
	New     bool          `json:"new"`
	Pages   []string      `json:"pages,omitempty"`   // pages that name a variant for the type
	Devices []string      `json:"devices,omitempty"` // devices that use the type
	Rename  *renameAction `json:"rename,omitempty"`
}

// handleDisplayEditor opens one display type file. A new type starts from an
// empty file, because the API creates it on save.
func (s *Server) handleDisplayEditor(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	q := r.URL.Query()
	name := q.Get("name")
	version, goVersion := editorVersion(cfg)
	v := displayEditorView{Version: version, GoVersion: goVersion, Name: name,
		Nav:        navFor("Devices"),
		Breadcrumb: crumbPath(crumb{Label: "Devices", URL: "/ui"}, crumb{Label: "Display types", URL: "/ui/display-types"}, crumb{Label: name}),
	}
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
	st := displayEditorState{Name: name, New: missing}
	for _, r := range v.Pages {
		st.Pages = append(st.Pages, r.Label)
	}
	for _, r := range v.Devices {
		st.Devices = append(st.Devices, r.Label)
	}
	if !missing {
		st.Rename = &renameAction{URL: "/api/displays/" + url.PathEscape(name) + "/rename"}
	}
	data, err := json.Marshal(st)
	if err != nil { // never expected: only strings and a bool
		slog.Error("display editor state encoding failed", "error", err)
		v.Error = err.Error()
		renderDisplayEditor(w, v, http.StatusInternalServerError)
		return
	}
	v.Data = template.JS(data)
	renderDisplayEditor(w, v, http.StatusOK)
}

// displayReferences lists what uses a display type: the pages whose variants
// map names it and the device files that point at it. Renaming or deleting a
// display type is a manual step, so the editor shows the consequences
// (ADR 0079, 0110).
func displayReferences(cfg *Config, name string) (pages, devices []displayReference) {
	files, err := pageFiles(cfg.Pages)
	if err != nil {
		slog.Warn("page list for the display type editor failed", "error", err)
	}
	for _, f := range files {
		label, named := f.Variants[name]
		if f.Label != "" || !named {
			continue
		}
		link := objectURL("/ui/pages", f.Name, url.Values{"file": {label}, "display": {name}})
		pages = append(pages, displayReference{Label: f.Name, URL: link})
	}
	list, err := deviceFileList(cfg.DevicesDir)
	if err != nil {
		slog.Warn("device list for the display type editor failed", "error", err)
	}
	for _, d := range list {
		if d.Display != name {
			continue
		}
		devices = append(devices, displayReference{Label: d.ID, URL: objectURL("/ui/devices", d.ID, nil)})
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

func (s *Server) handleDisplayEditorJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(displayEditorJS)
}
