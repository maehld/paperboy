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
	"os"
	"sort"

	"github.com/maehld/paperboy/internal/page"
)

// The plan editor on top of the plan API (ADR 0066): one plan opened for
// editing, with the rules, the fallback and the actions a device menu offers
// (ADR 0025, ADR 0057, ADR 0069). Like the page editor it is a server-rendered
// template plus one embedded script, which works on the JSON model of the API.

//go:embed ui/plan-editor.html
var planEditorTemplate string

var planEditorTmpl = template.Must(template.New("plan-editor").Parse(planEditorTemplate))

//go:embed ui/plan-editor.js
var planEditorJS []byte

// planEditorDevice is one device that uses the edited plan; the editor links
// its plan preview and warns about pages that do not fit its display type.
type planEditorDevice struct {
	ID      string `json:"id"`
	Display string `json:"display"`
}

// planEditorPage is one page file the plan may name, with the reason it does
// not fit a display type of one of the plan's devices.
type planEditorPage struct {
	Name  string            `json:"name"`
	Kind  string            `json:"kind,omitempty"`
	Grid  [2]int            `json:"grid,omitempty"`
	Error string            `json:"error,omitempty"` // the file does not parse
	Fits  map[string]string `json:"fits,omitempty"`  // display type → reason it is skipped
}

// planEditorState is the data the script needs beyond the plan model: which
// devices use the plan and which pages fit their display types.
type planEditorState struct {
	Name     string             `json:"name"`
	New      bool               `json:"new"` // there is no file yet (ADR 0073)
	Devices  []planEditorDevice `json:"devices"`
	Displays []string           `json:"displays"`
	Pages    []planEditorPage   `json:"pages"`
}

// planEditorView is the shell of the plan editor.
type planEditorView struct {
	Version   string
	GoVersion string
	Name      string
	Timezone  string // the global timezone the windows are read in (ADR 0033)
	Error     string
	Data      template.JS
}

// planEditorData gathers the devices of a plan and the pages with their fit
// per display type of those devices.
func (s *Server) planEditorData(cfg *Config, name string) planEditorState {
	st := planEditorState{Name: name, Devices: []planEditorDevice{}, Displays: []string{}, Pages: []planEditorPage{}}
	seen := map[string]bool{}
	for _, d := range cfg.Devices {
		if d.Plan != name {
			continue
		}
		st.Devices = append(st.Devices, planEditorDevice{ID: d.ID, Display: d.Profile})
		if !seen[d.Profile] {
			seen[d.Profile] = true
			st.Displays = append(st.Displays, d.Profile)
		}
	}
	sort.Strings(st.Displays)
	files, err := pageFiles(cfg.Pages)
	if err != nil {
		slog.Warn("page list for the plan editor failed", "error", err)
	}
	listed := map[string]bool{}
	for _, f := range files {
		if listed[f.Name] {
			continue
		}
		listed[f.Name] = true
		p := planEditorPage{Name: f.Name, Kind: f.Kind, Grid: f.Grid, Error: f.Error}
		if len(st.Displays) > 0 {
			p.Fits = map[string]string{}
			for _, display := range st.Displays {
				if err := s.fits(cfg, display, f.Name); err != nil {
					p.Fits[display] = err.Error()
				}
			}
		}
		st.Pages = append(st.Pages, p)
	}
	return st
}

// handlePlanEditor opens one plan. A plan without a file opens as a new one
// when the overview asked for it (&new=1); the first save writes the file, and
// the plan parser wants at least one rule with a page (ADR 0073).
func (s *Server) handlePlanEditor(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	name := r.URL.Query().Get("name")
	version, goVersion := editorVersion(cfg)
	v := planEditorView{Version: version, GoVersion: goVersion, Name: name, Timezone: cfg.timezoneName()}
	if !page.ValidName(name) {
		v.Error = fmt.Sprintf("plan name %q: only a-z, 0-9 and -", name)
		renderPlanEditor(w, v, http.StatusBadRequest)
		return
	}
	path, err := planFilePath(cfg.Plans, name)
	if err != nil {
		v.Error = err.Error()
		renderPlanEditor(w, v, http.StatusBadRequest)
		return
	}
	missing := false
	if _, err := os.Stat(path); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			v.Error = err.Error()
			renderPlanEditor(w, v, http.StatusInternalServerError)
			return
		}
		missing = true
	}
	if missing && r.URL.Query().Get("new") != "1" {
		v.Error = fmt.Sprintf("there is no plan %q; plans are files under plans/", name)
		renderPlanEditor(w, v, http.StatusNotFound)
		return
	}
	st := s.planEditorData(cfg, name)
	st.New = missing
	data, err := json.Marshal(st)
	if err != nil { // never expected: only strings and numbers
		slog.Error("plan editor state encoding failed", "error", err)
		v.Error = err.Error()
		renderPlanEditor(w, v, http.StatusInternalServerError)
		return
	}
	v.Data = template.JS(data)
	renderPlanEditor(w, v, http.StatusOK)
}

func renderPlanEditor(w http.ResponseWriter, v planEditorView, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := planEditorTmpl.Execute(w, v); err != nil {
		slog.Error("plan editor rendering failed", "error", err)
	}
}

// handlePlanEditorJS serves the plan editor script from the binary.
func (s *Server) handlePlanEditorJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(planEditorJS)
}
