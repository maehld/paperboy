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
	"time"

	"github.com/maehld/paperboy/internal/page"
)

// The device editor on top of the device API (ADR 0070): one device file with
// its display type, plan, refresh and menu settings, plus a new device. Like the
// page and plan editors it is a server-rendered template and one embedded
// script, which works on the JSON model of the API.

//go:embed ui/device-editor.html
var deviceEditorTemplate string

var deviceEditorTmpl = template.Must(template.New("device-editor").Parse(deviceEditorTemplate))

//go:embed ui/device-editor.js
var deviceEditorJS []byte

// deviceEditorView is the shell of the device editor.
type deviceEditorView struct {
	Version   string
	GoVersion string
	ID        string
	New       bool
	Error     string
	Data      template.JS
}

// deviceEditorState is what the script needs beyond the device file: the display
// types, the plans to choose from and the defaults of paperboy.yaml, so an empty
// field can show what it inherits.
type deviceEditorState struct {
	ID       string   `json:"id"`
	New      bool     `json:"new"`
	Displays []string `json:"displays"`
	Plans    []string `json:"plans"`
	Refresh  string   `json:"refresh"`   // default interval
	MenuShow string   `json:"menu_show"` // default menu duration
}

// handleDeviceEditor opens one device file. A new device starts from an empty
// file, because the API creates it on save.
func (s *Server) handleDeviceEditor(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	q := r.URL.Query()
	id := q.Get("id")
	version, goVersion := editorVersion(cfg)
	v := deviceEditorView{Version: version, GoVersion: goVersion, ID: id}
	if !page.ValidName(id) {
		v.Error = fmt.Sprintf("device id %q: only a-z, 0-9 and -", id)
		renderDeviceEditor(w, v, http.StatusBadRequest)
		return
	}
	missing := false
	if path, err := deviceFilePath(cfg.DevicesDir, id); err != nil {
		v.Error = err.Error()
		renderDeviceEditor(w, v, http.StatusBadRequest)
		return
	} else if _, err := os.Stat(path); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			v.Error = err.Error()
			renderDeviceEditor(w, v, http.StatusInternalServerError)
			return
		}
		missing = true
	}
	v.New = missing
	if missing && q.Get("new") != "1" {
		v.Error = fmt.Sprintf("there is no device %q; devices are files under devices/", id)
		renderDeviceEditor(w, v, http.StatusNotFound)
		return
	}
	menuShow := time.Duration(cfg.MenuShow)
	if menuShow == 0 {
		menuShow = defaultMenuDuration
	}
	st := deviceEditorState{ID: id, New: missing, Displays: displayNames(cfg),
		Plans: []string{}, Refresh: time.Duration(cfg.Refresh).String(), MenuShow: menuShow.String()}
	plans, err := planFiles(cfg.Plans, cfg.Timezone)
	if err != nil {
		slog.Warn("plan list for the device editor failed", "error", err)
	}
	for _, p := range plans {
		if p.Error == "" {
			st.Plans = append(st.Plans, p.Name)
		}
	}
	data, err := json.Marshal(st)
	if err != nil { // never expected: only strings and numbers
		slog.Error("device editor state encoding failed", "error", err)
		v.Error = err.Error()
		renderDeviceEditor(w, v, http.StatusInternalServerError)
		return
	}
	v.Data = template.JS(data)
	renderDeviceEditor(w, v, http.StatusOK)
}

func renderDeviceEditor(w http.ResponseWriter, v deviceEditorView, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := deviceEditorTmpl.Execute(w, v); err != nil {
		slog.Error("device editor rendering failed", "error", err)
	}
}

// handleDeviceEditorJS serves the device editor script from the binary.
func (s *Server) handleDeviceEditorJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(deviceEditorJS)
}
