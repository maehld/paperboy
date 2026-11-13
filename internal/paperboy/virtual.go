package paperboy

import (
	_ "embed"
	"fmt"
	"html/template"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The virtual display renders a plan as a display of a chosen type in the
// browser and behaves like the firmware: it syncs, paints the answer, reports
// the shown hash and comes back for its refresh or a one-shot wake (ADR 0091).
//
//go:embed ui/virtual.html
var virtualTemplate string

var virtualTmpl = template.Must(template.New("virtual").Parse(virtualTemplate))

//go:embed ui/virtual.js
var virtualJS []byte

// virtualPage is what the template needs: the validated configuration and the
// endpoint the script talks to, so the page parses no query itself.
type virtualPage struct {
	Version   string
	GoVersion string
	Plan      string
	Profile   string
	Width     int
	Height    int
	Power     string
	SyncURL   string
	ResetURL  string
}

// virtualOptions is the configuration of one virtual display. Only the plan
// and the display type are required; the rest falls back to the instance
// defaults, so a URL carries only what differs (ADR 0091).
type virtualOptions struct {
	plan        string
	profile     string
	refresh     time.Duration
	menu        time.Duration
	pageTimeout time.Duration
	power       string
}

// device builds the in-memory device a virtual display acts as. It has no MAC
// and no state file: its cursor and menu live in the server's memory.
func (o virtualOptions) device() *Device {
	return &Device{
		ID:               "virtual",
		Type:             TypePaperboy,
		Profile:          o.profile,
		Plan:             o.plan,
		Power:            o.power,
		Refresh:          Duration(o.refresh),
		MenuShowDuration: Duration(o.menu),
		Actions:          actionsAllow,
	}
}

// virtualOptions reads and checks the query of a virtual display. It returns
// the HTTP status of a bad request, so the page and the sync answer alike.
func parseVirtualOptions(cfg *Config, q url.Values) (virtualOptions, int, error) {
	plan := strings.TrimSpace(q.Get("plan"))
	profile := strings.TrimSpace(q.Get("display"))
	if plan == "" || profile == "" {
		return virtualOptions{}, http.StatusBadRequest,
			fmt.Errorf("plan and display are required, e.g. /ui/virtual?plan=tour&display=e1001")
	}
	if !validName(plan) {
		return virtualOptions{}, http.StatusBadRequest, fmt.Errorf("plan %q: only a-z, 0-9 and -", plan)
	}
	if !validName(profile) {
		return virtualOptions{}, http.StatusBadRequest, fmt.Errorf("display %q: only a-z, 0-9 and -", profile)
	}
	if _, ok := cfg.Profiles[profile]; !ok {
		return virtualOptions{}, http.StatusNotFound, fmt.Errorf("unknown display type %q", profile)
	}
	o := virtualOptions{plan: plan, profile: profile, refresh: time.Duration(cfg.Refresh),
		menu: time.Duration(cfg.MenuShow), pageTimeout: time.Duration(cfg.PageTimeout), power: powerMains}
	if o.menu == 0 {
		o.menu = defaultMenuDuration
	}
	var err error
	if o.refresh, err = querySeconds(q, "refresh", o.refresh, time.Second, 0); err != nil {
		return virtualOptions{}, http.StatusBadRequest, err
	}
	if o.menu, err = querySeconds(q, "menu", o.menu, 5*time.Second, menuMaxDuration); err != nil {
		return virtualOptions{}, http.StatusBadRequest, err
	}
	if o.pageTimeout, err = querySeconds(q, "page_timeout", o.pageTimeout, time.Second, 0); err != nil {
		return virtualOptions{}, http.StatusBadRequest, err
	}
	switch q.Get("power") {
	case "":
	case powerMains, powerBattery:
		o.power = q.Get("power")
	default:
		return virtualOptions{}, http.StatusBadRequest,
			fmt.Errorf("power %q: expected %s or %s", q.Get("power"), powerMains, powerBattery)
	}
	return o, http.StatusOK, nil
}

// querySeconds reads an optional duration in seconds. A zero upper bound means
// no limit; the value is rejected instead of guessed.
func querySeconds(q url.Values, name string, def, low, high time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(q.Get(name))
	if raw == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 || n > math.MaxInt64/int64(time.Second) {
		return 0, fmt.Errorf("%s %q: expected seconds", name, raw)
	}
	d := time.Duration(n) * time.Second
	if d < low || (high > 0 && d > high) {
		if high > 0 {
			return 0, fmt.Errorf("%s: expected %d to %d seconds", name, int(low.Seconds()), int(high.Seconds()))
		}
		return 0, fmt.Errorf("%s: expected at least %d seconds", name, int(low.Seconds()))
	}
	return d, nil
}

// handleVirtual serves the page that acts as a virtual display. The browser
// runs the firmware loop against /ui/virtual/sync; the page itself carries no
// controls for the settings, the URL is the configuration (ADR 0091).
func (s *Server) handleVirtual(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	opt, status, err := parseVirtualOptions(cfg, r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), status)
		return
	}
	prof := cfg.Profiles[opt.profile]
	q := url.Values{}
	q.Set("plan", opt.plan)
	q.Set("display", opt.profile)
	version, goVersion := editorVersion(cfg)
	page := virtualPage{
		Version:   version,
		GoVersion: goVersion,
		Plan:      opt.plan,
		Profile:   opt.profile,
		Width:     prof.Width,
		Height:    prof.Height,
		Power:     opt.power,
		SyncURL:   "/ui/virtual/sync?" + q.Encode(),
		ResetURL:  "/ui/virtual/reset?" + q.Encode(),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := virtualTmpl.Execute(w, page); err != nil {
		slog.Error("virtual display page failed", "error", err)
	}
}

// handleVirtualJS serves the firmware stand-in of the virtual display.
func (s *Server) handleVirtualJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(virtualJS)
}

// handleVirtualSync answers a wake of a virtual display. It takes the same
// body as the device API, but no key: the session is the display (ADR 0091).
func (s *Server) handleVirtualSync(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	var req syncRequest
	if !decodeDeviceJSON(w, r, &req) {
		return
	}
	cfg := s.config()
	opt, status, err := parseVirtualOptions(cfg, r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), status)
		return
	}
	if err := req.validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	optDevice := opt.device()
	st := s.virtualState(opt)
	resp := s.serveSync(cfg, optDevice, st, &req, now, syncOptions{
		pageTimeout: opt.pageTimeout,
		virtual:     true,
	})
	if resp.Image != nil {
		// The browser has no device key, so the page fetches its picture
		// through the UI side of the server (ADR 0097).
		resp.Image.Path = virtualImagePath(resp.Image.Path)
	}
	s.mu.Lock()
	debug := virtualDebug{Rule: st.Rule, Page: st.Page, Pages: st.LastPages, PageName: st.LastPage,
		PageKind: st.LastPageKind, Error: st.LastError}
	if st.Menu.Open {
		debug.Menu = st.Menu.Phase
		if a := st.Menu.find(st.Menu.Selected); a != nil {
			debug.Menu += " · " + a.Title
		}
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, virtualSyncResponse{syncResponse: resp, Debug: debug})
}

// virtualSyncResponse is the device answer plus what the page shows below the
// display. The sync fields stay where the firmware expects them (ADR 0091).
type virtualSyncResponse struct {
	syncResponse
	Debug virtualDebug `json:"debug"`
}

// virtualDebug names the page selection a device only logs, so the virtual
// display can show it.
type virtualDebug struct {
	Rule     string `json:"rule,omitempty"`
	Page     int    `json:"page"`
	Pages    int    `json:"pages,omitempty"`
	PageName string `json:"page_name,omitempty"`
	PageKind string `json:"page_kind,omitempty"`
	Error    string `json:"error,omitempty"`
	Menu     string `json:"menu,omitempty"`
}

// handleVirtualReset drops the cursor and the menu of one virtual display.
func (s *Server) handleVirtualReset(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	opt, status, err := parseVirtualOptions(cfg, r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), status)
		return
	}
	s.mu.Lock()
	delete(s.virtual, virtualKey(opt))
	s.mu.Unlock()
	slog.Info("virtual display reset", "plan", opt.plan, "display", opt.profile)
	w.WriteHeader(http.StatusNoContent)
}

// virtualKey names a session: the plan and the display type are the identity,
// so every browser with the same URL operates the same display.
func virtualKey(o virtualOptions) string { return o.plan + "/" + o.profile }

// virtualImagePath moves a served page to the UI side of the server. A virtual
// display has no device file and therefore no key, while /pages and /hints only
// answer a device that fetches with one (ADR 0097); the UI routes behind the
// password serve the same picture (ADR 0091).
func virtualImagePath(path string) string {
	switch {
	case strings.HasPrefix(path, "/pages/"), strings.HasPrefix(path, "/hints/"):
		return "/ui/virtual" + path
	default:
		return path
	}
}

// virtualURL links a plan and a display type to the virtual display, so the
// plan view and the dashboard need no query building of their own (ADR 0091).
func virtualURL(plan, profile string) string {
	if plan == "" || profile == "" {
		return ""
	}
	q := url.Values{}
	q.Set("plan", plan)
	q.Set("display", profile)
	return "/ui/virtual?" + q.Encode()
}

// virtualState returns the in-memory state of a virtual display, creating it
// on first use. The pointer stays valid until the session is reset.
func (s *Server) virtualState(o virtualOptions) *deviceState {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := virtualKey(o)
	st := s.virtual[key]
	if st == nil {
		st = &deviceState{}
		s.virtual[key] = st
	}
	return st
}
