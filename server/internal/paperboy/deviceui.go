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
	"time"

	"github.com/maehld/paperboy/server/internal/page"
)

// The device editor on top of the device API (ADR 0070): one device file with
// its display type, plan, refresh and action settings, plus a new device. Like the
// page and plan editors it is a server-rendered template and one embedded
// script, which works on the JSON model of the API.

//go:embed ui/device-editor.html
var deviceEditorTemplate string

var deviceEditorTmpl = pageTemplate("device-editor", deviceEditorTemplate, nil)

//go:embed ui/device-editor.js
var deviceEditorJS []byte

// deviceEditorView is the shell of the device editor.
type deviceEditorView struct {
	Version    string
	GoVersion  string
	Nav        []navItem // the rail, with the active section (ADR 0152)
	Breadcrumb []crumb   // the path to this page
	ID         string
	FileHint   string // devices/<id>.yaml in the title
	New        bool
	Error      string
	Data       template.JS
	Log        []deviceLogRow // the device's recent log, newest first (ADR 0171)
}

// deviceLogRow is one line of the device editor's recent log.
type deviceLogRow struct {
	When    string
	Level   string
	Message string
}

// deviceEditorState is what the script needs beyond the device file: the display
// types, the plans to choose from and the defaults of paperboy.yaml, so an empty
// field can show what it inherits.
type deviceEditorState struct {
	ID       string        `json:"id"`
	New      bool          `json:"new"`
	MAC      string        `json:"mac,omitempty"` // prefill of a claimed state file (ADR 0080)
	Displays []string      `json:"displays"`
	Plans    []string      `json:"plans"`
	Refresh  string        `json:"refresh"` // default interval
	Rename   *renameAction `json:"rename,omitempty"`
}

// handleDeviceEditor opens one device file. A new device starts from an empty
// file, because the API creates it on save.
func (s *Server) handleDeviceEditor(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	q := r.URL.Query()
	id := q.Get("id")
	version, goVersion := editorVersion(cfg)
	v := deviceEditorView{Version: version, GoVersion: goVersion, ID: id, FileHint: "devices/" + id + ".yaml",
		Nav:        navFor("Devices"),
		Breadcrumb: crumbPath(crumb{Label: "Devices", URL: "/ui/devices"}, crumb{Label: "Device " + id}),
	}
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
	st := deviceEditorState{ID: id, New: missing, Displays: displayNames(cfg),
		Plans: []string{}, Refresh: time.Duration(cfg.Refresh).String()}
	if missing {
		// Claiming hardware from the devices tab prefills its MAC (ADR 0080).
		st.MAC = formatMAC(q.Get("mac"))
	}
	if !missing {
		st.Rename = &renameAction{URL: "/api/devices/" + url.PathEscape(id) + "/rename"}
		v.Log = s.deviceLogRows(cfg, id)
	}
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

// deviceLogRows renders the stored log of one device, newest first, for the
// device editor. The device clock supplies the time; a line the device reported
// before its first sync falls back to the moment Paperboy received it
// (ADR 0171).
func (s *Server) deviceLogRows(cfg *Config, id string) []deviceLogRow {
	var dev *Device
	for i := range cfg.Devices {
		if cfg.Devices[i].ID == id {
			dev = &cfg.Devices[i]
			break
		}
	}
	if dev == nil {
		return nil
	}
	s.mu.Lock()
	var lines []deviceLogLine
	if st := s.state[dev.stateKey()]; st != nil {
		lines = append(lines, st.Log...)
	}
	s.mu.Unlock()
	loc := cfg.Timezone
	if loc == nil {
		loc = time.UTC
	}
	rows := make([]deviceLogRow, 0, len(lines))
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		at := line.At
		if line.Time > 0 {
			at = time.Unix(line.Time, 0)
		}
		level := line.Level
		if level == "" {
			level = "info"
		}
		rows = append(rows, deviceLogRow{When: at.In(loc).Format("2006-01-02 15:04:05"), Level: level,
			Message: line.Message})
	}
	return rows
}

func renderDeviceEditor(w http.ResponseWriter, v deviceEditorView, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := deviceEditorTmpl.Execute(w, v); err != nil {
		slog.Error("device editor rendering failed", "error", err)
	}
}

func (s *Server) handleDeviceEditorJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(deviceEditorJS)
}
