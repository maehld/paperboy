package paperboy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/internal/builtin"
	"github.com/maehld/paperboy/internal/page"
	"github.com/maehld/paperboy/internal/widget"
	"github.com/maehld/paperboy/pkg/plugin"
)

// Server implements the device API (ADR 0042). Pages are composed from plugin
// widgets (ADR 0049) and chosen by plans. The configuration reloads from
// paperboy.yaml when the file changes (ADR 0047): handlers read the current
// configuration through config, and the plugins are reconfigured on the same
// reload.
type Server struct {
	cfg        *configLoader // current configuration, reloaded on change (ADR 0047)
	devicesDir string        // state/devices/ with one file per MAC (ADR 0080)
	now        func() time.Time
	ctx        context.Context // lifecycle context for background checks (ADR 0062)

	mu    sync.Mutex
	state map[string]*deviceState // device key (MAC) → state
	// virtual holds the state of the virtual displays, keyed by plan and
	// display type: in memory only, never in state/devices (ADR 0091).
	virtual map[string]*deviceState
	plans   *planLoader // device plans (ADR 0025)
	// actionRuns holds the actions that outlive their sync (ADR 0064).
	actionRuns map[string]*actionRun

	rendered     map[string]*renderedPage // "<profile>/<page>" → last render
	hints        map[string]hintPage      // content hash → notice page (ADR 0097 keeps its display type)
	logs         *logSeen                 // delivered device log entries (own lock)
	firmware     *firmwareStore           // OTA images (ADR 0043; own lock)
	fwNotes      map[string]string        // device ID → last logged reason for withholding an update
	pages        *pageLoader              // pages by file, reloaded on change (own lock)
	skipNotes    map[string]string        // "<profile>/<page>" → last logged reason for skipping it
	qoiNotes     map[string]string        // page path → last logged reason for simplifying it (ADR 0088)
	pageKeyNotes map[string]string        // page path → last logged reason for refusing it (ADR 0097)
	widgets      *widget.Host             // plugins and their widget cache (ADR 0049; own lock)
	download     *downloadStatus          // release checks (ADR 0060; own lock)
}

// config returns the current configuration, reloading it from paperboy.yaml
// when the file changed (ADR 0047).
func (s *Server) config() *Config {
	return s.cfg.get()
}

// New loads the device state and prepares templates and plans. Only state/ is
// created; pages, plans and plugins stay operator-owned (ADR 0037).
func New(cfg *Config) (*Server, error) {
	if cfg.Dir == "" {
		return nil, errors.New("instance directory missing")
	}
	stateDir := filepath.Join(cfg.Dir, stateDirName)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, err
	}
	devicesDir := filepath.Join(stateDir, devicesStateDirName)
	state, err := loadDeviceStates(devicesDir)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(stateDir, legacyDevicesFileName)); err == nil {
		slog.Warn("state/devices.json is no longer used; move its entries to state/devices/<MAC>.json (ADR 0080)",
			"file", filepath.Join(stateDir, legacyDevicesFileName))
	}
	// The configuration is normalized here and then reloaded from paperboy.yaml
	// whenever the file changes (ADR 0047). The given configuration is the last
	// valid one; a degraded start hands over a configuration without display
	// types or devices and applies the file as soon as it becomes valid.
	if cfg.Pages == "" {
		cfg.Pages = filepath.Join(cfg.Dir, pagesDirName)
	}
	if cfg.Plans == "" {
		cfg.Plans = filepath.Join(cfg.Dir, plansDirName)
	}
	if cfg.DisplaysDir == "" {
		cfg.DisplaysDir = filepath.Join(cfg.Dir, displaysDirName)
	}
	if cfg.DevicesDir == "" {
		cfg.DevicesDir = filepath.Join(cfg.Dir, devicesDirName)
	}
	if cfg.PluginsDir == "" {
		cfg.PluginsDir = filepath.Join(cfg.Dir, pluginsDirName)
	}
	if cfg.FirmwareDir == "" {
		cfg.FirmwareDir = filepath.Join(cfg.Dir, firmwareDirName)
	}
	s := &Server{cfg: newConfigLoader(cfg.Dir, cfg), ctx: context.Background(),
		widgets:    &widget.Host{Builtin: map[string]plugin.Handler{builtin.Name: builtin.Plugin{}}},
		devicesDir: devicesDir, now: time.Now, state: state, virtual: map[string]*deviceState{},
		plans: newPlanLoader(), actionRuns: map[string]*actionRun{}, rendered: map[string]*renderedPage{}, hints: map[string]hintPage{},
		logs: newLogSeen(), fwNotes: map[string]string{}, pages: newPageLoader(), skipNotes: map[string]string{}}
	s.firmware = newFirmwareStore(cfg.FirmwareDir)
	s.download = newDownloadStatus()
	// A plan named in the configuration is reported when it is missing or broken
	// (ADR 0026); the device gets a notice page instead of an error, so a
	// reloaded configuration never aborts the server.
	for i := range cfg.Devices {
		d := &cfg.Devices[i]
		if d.Plan == "" {
			continue
		}
		if p, err := s.plans.get(cfg.Plans, d.Plan, cfg.Timezone); p == nil || err != nil {
			slog.Error("plan not usable", "device", d.ID, "plan", d.Plan, "error", err)
		}
	}
	s.widgets.Now = func() time.Time { return s.now() }
	s.configurePlugins(cfg)
	return s, nil
}

// Start runs the plugins, the configuration reload loop and the release checks
// until ctx is done (ADR 0047, 0049, 0060). Plugins start on first use and stop
// with ctx.
func (s *Server) Start(ctx context.Context) {
	s.ctx = ctx
	s.widgets.Start(ctx)
	go s.configLoop(ctx)
	go s.downloadLoop(ctx)
}

// route is one entry of a handler table: the method and path pattern of an
// http.ServeMux together with its handler.
type route struct {
	pattern string
	handle  http.HandlerFunc
}

// deviceRoutes are what a display touches: the device API (ADR 0042) and the
// images, notice pages and firmware it downloads. They stay reachable without
// the UI password, so nothing else belongs here; TestDeviceRoutesStayOpen pins
// their prefixes.
func (s *Server) deviceRoutes() []route {
	return []route{
		{"POST /api/device/register", s.handleRegister},
		{"POST /api/device/sync", s.handleSync},
		{"GET /api/device/text", s.handleText},
		{"GET /pages/{profile}/{page}/{file}", s.requirePageKey(s.handleRenderedImage)},
		{"GET /hints/{file}", s.requirePageKey(s.handleHint)},
		{"GET /firmware/{file}", s.handleFirmware},
	}
}

// uiRoutes are the operator UI, the API behind its editors and the widget and
// preview endpoints they use (ADR 0066, 0070, 0071, 0076). Every entry sits
// behind the password from ui.password (ADR 0096), because the pages carry the
// instance's content: calendars, departures, the home's energy.
func (s *Server) uiRoutes() []route {
	return []route{
		{"GET /{$}", s.handleRoot},
		{"GET /ui", s.handleDashboard},
		{"GET /ui/downloads", s.handleDownloads},
		{"GET /ui/plugins", s.handleWidgetsPage},
		{"GET /ui/plan", s.handlePlanView},
		{"GET /ui/virtual", s.handleVirtual},
		{"GET /ui/virtual.js", s.handleVirtualJS},
		// The virtual display has no device key, so it loads its picture
		// through these two routes (ADR 0091, 0097).
		{"GET /ui/virtual/pages/{profile}/{page}/{file}", s.handleRenderedImage},
		{"GET /ui/virtual/hints/{file}", s.handleHint},
		{"POST /ui/virtual/sync", s.handleVirtualSync},
		{"POST /ui/virtual/reset", s.handleVirtualReset},
		{"GET /ui/editor", s.handleEditor},
		{"GET /ui/editor/page", s.handleEditorPage},
		{"GET /ui/editor/plan", s.handlePlanEditor},
		{"GET /ui/editor/device", s.handleDeviceEditor},
		{"GET /ui/editor/display", s.handleDisplayEditor},
		{"GET /ui/editor.js", s.handleEditorJS},
		{"GET /ui/ui.css", s.handleUICSS},
		{"GET /ui/logo.svg", s.handleUILogo},
		{"GET /ui/favicon.svg", s.handleUIFavicon},
		{"GET /ui/editor/plan.js", s.handlePlanEditorJS},
		{"GET /ui/editor/device.js", s.handleDeviceEditorJS},
		{"GET /ui/editor/display.js", s.handleDisplayEditorJS},
		{"POST /ui/download", s.handleDownloadCheck},
		{"POST /ui/downloads/prune", s.handlePrune},
		{"GET /api/profiles", s.handleProfiles},
		{"GET /api/plugins", s.handlePlugins},
		{"GET /api/devices", s.handleDevices},
		{"DELETE /api/state/devices/{mac}", s.handleForgetDeviceState},
		{"GET /api/preview/{display}/{page}", s.handlePreview},
		{"GET /api/widget/{plugin}/{widget}", s.handleWidget},
		// Page and plan editing (ADR 0066, ADR 0073).
		{"GET /api/pages", s.handlePages},
		{"GET /api/pages/{name}", s.handlePage},
		{"PUT /api/pages/{name}", s.handleSavePage},
		{"DELETE /api/pages/{name}", s.handleDeletePage},
		{"POST /api/pages/{name}/rename", s.handleRenamePage},
		{"GET /api/plans", s.handlePlans},
		{"GET /api/plans/{name}", s.handlePlan},
		{"PUT /api/plans/{name}", s.handleSavePlan},
		{"DELETE /api/plans/{name}", s.handleDeletePlan},
		{"POST /api/plans/{name}/rename", s.handleRenamePlan},
		// Device files (ADR 0070).
		{"GET /api/devices/{id}", s.handleDevice},
		{"GET /api/devices/{id}/image", s.handleDeviceImage},
		{"PUT /api/devices/{id}", s.handleSaveDevice},
		{"DELETE /api/devices/{id}", s.handleDeleteDevice},
		{"POST /api/devices/{id}/rename", s.handleRenameDevice},
		// Display type files (ADR 0071).
		{"GET /api/displays", s.handleDisplays},
		{"GET /api/displays/{name}", s.handleDisplay},
		{"PUT /api/displays/{name}", s.handleSaveDisplay},
		{"DELETE /api/displays/{name}", s.handleDeleteDisplay},
		{"POST /api/displays/{name}/rename", s.handleRenameDisplay},
		{"POST /api/preview/{display}", s.handleDraftPreview},
	}
}

func newMux(routes []route) *http.ServeMux {
	mux := http.NewServeMux()
	for _, r := range routes {
		mux.HandleFunc(r.pattern, r.handle)
	}
	return mux
}

// Handler serves the device API and the operator UI under one origin. The route
// tables keep the two apart (ADR 0096): the UI mux decides which side a request
// belongs to, and only the UI side is wrapped in the password check. A new UI
// route is therefore protected by construction, and a route can only be open
// when it is listed in deviceRoutes.
func (s *Server) Handler() http.Handler {
	uiMux := newMux(s.uiRoutes())
	ui := s.requireUI(uiMux)
	devices := newMux(s.deviceRoutes())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if uiRequest(uiMux, r) {
			ui.ServeHTTP(w, r)
			return
		}
		devices.ServeHTTP(w, r)
	})
}

// uiRequest reports whether the UI table owns r. A request whose method no UI
// route accepts still belongs to the UI (it answers 405 once the password is
// right) instead of falling through to the device API's 404.
func uiRequest(ui *http.ServeMux, r *http.Request) bool {
	if _, pattern := ui.Handler(r); pattern != "" {
		return true
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return false
	}
	probe := *r
	probe.Method = http.MethodGet
	_, pattern := ui.Handler(&probe)
	return pattern != ""
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

type profileInfo struct {
	Name string `json:"name"`
	Profile
}

func (s *Server) handleProfiles(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	s.mu.Lock()
	defer s.mu.Unlock()
	list := []profileInfo{}
	for name, p := range cfg.Profiles {
		list = append(list, profileInfo{Name: name, Profile: p})
	}
	writeJSON(w, http.StatusOK, map[string]any{"profiles": list})
}

type deviceInfo struct {
	ID      string `json:"id"`
	Type    string `json:"protocol"`
	MAC     string `json:"mac,omitempty"`
	Profile string `json:"display_type"`
	Plan    string `json:"plan,omitempty"`
	deviceState
	APIKey            string `json:"api_key,omitempty"` // shadows the embedded key; never exposed
	Registered        bool   `json:"registered"`
	Status            string `json:"status"`                       // ok, late, never or unregistered
	LastPageURL       string `json:"last_page_url,omitempty"`      // UI route for the last served image (ADR 0097)
	FirmwareAvailable string `json:"firmware_available,omitempty"` // newest image for the hardware (ADR 0043)
	FirmwareStatus    string `json:"firmware_status"`              // current, offered, available, failed, held or none
	Power             string `json:"power"`                        // effective power source: battery or mains (ADR 0051)
	Problem           string `json:"problem,omitempty"`            // why the device cannot sync, e.g. a missing display type (ADR 0079)
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"devices": s.deviceInfos()})
}

// lastImagePath is the UI route that serves the last image a device received
// (ADR 0040, 0097).
func lastImagePath(id string) string {
	return "/api/devices/" + url.PathEscape(id) + "/image"
}

// handleDeviceImage serves the last image a device received. The page routes
// ask for the device key (ADR 0097) and the operator's browser has no key, so
// the dashboard reads the last served image through the UI instead of falling
// back to a live preview.
func (s *Server) handleDeviceImage(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	var device *Device
	for i := range cfg.Devices {
		if cfg.Devices[i].ID == r.PathValue("id") {
			device = &cfg.Devices[i]
			break
		}
	}
	if device == nil {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	var data []byte
	var hash, kind, page string
	if st := s.state[device.stateKey()]; st != nil {
		hash, kind, page = st.LastPageHash, st.LastPageKind, st.LastPage
	}
	switch {
	case kind == "page" && page != "":
		if rp := s.rendered[device.Profile+"/"+page]; rp != nil && rp.hash == hash {
			data = rp.data
		}
	default:
		if hp, ok := s.hints[hash]; ok && hp.profile == device.Profile {
			data = hp.data
		}
	}
	s.mu.Unlock()
	if data == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writePNG(w, data)
}

// handleForgetDeviceState deletes the state file of one MAC, so hardware the
// operator no longer needs disappears from the device editor (ADR 0080). A
// configured device loses its key and registers again on its next sync.
func (s *Server) handleForgetDeviceState(w http.ResponseWriter, r *http.Request) {
	key, err := deviceStateKey(r.PathValue("mac"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid MAC address")
		return
	}
	if err := s.forgetDeviceState(key); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	slog.Info("device state forgotten", "mac", formatStateKey(key))
	writeJSON(w, http.StatusOK, map[string]string{"mac": formatStateKey(key)})
}

// deviceInfos builds the device list for the devices API and the dashboard.
// The firmware directory is read after the state lock is released.
func (s *Server) deviceInfos() []deviceInfo {
	cfg := s.config()
	s.mu.Lock()
	now := s.now()
	list := []deviceInfo{}
	devices := make([]Device, 0, len(cfg.Devices))
	for _, d := range cfg.Devices {
		info := deviceInfo{ID: d.ID, Type: d.Type, MAC: d.MAC, Profile: d.Profile, Plan: d.Plan, Problem: d.Problem}
		if st := s.state[d.stateKey()]; st != nil {
			info.deviceState = *st
			info.Registered = st.APIKey != ""
		}
		info.Status = deviceStatus(d, info.deviceState, now)
		info.Power = cfg.power(&d, info.ReportedPower)
		// Rendered images live in memory only: after a restart the stored
		// hash points nowhere, so the URL is left out instead of a dead link.
		// Page downloads carry the device key (ADR 0097), so the card links the
		// UI's own route for the last served image (ADR 0040).
		if info.LastPageHash != "" && !cfg.Profiles[d.Profile].text() {
			if info.LastPageKind == "page" && info.LastPage != "" {
				if rp := s.rendered[d.Profile+"/"+info.LastPage]; rp != nil && rp.hash == info.LastPageHash {
					info.LastPageURL = lastImagePath(d.ID)
				}
			} else if hp, ok := s.hints[info.LastPageHash]; ok && hp.profile == d.Profile {
				info.LastPageURL = lastImagePath(d.ID)
			}
		}
		list = append(list, info)
		devices = append(devices, d)
	}
	s.mu.Unlock()
	images := map[string]*firmwareImage{}
	for i := range list {
		hw := list[i].Hardware
		img, ok := images[hw]
		if !ok {
			img, _ = s.firmware.latest(hw, cfg.Firmware.Version)
			images[hw] = img
		}
		if img != nil {
			list[i].FirmwareAvailable = img.Version
		}
		list[i].FirmwareStatus = firmwareStatus(devices[i], list[i].deviceState, img)
	}
	return list
}

// deviceStatus summarizes the last contact. Devices deep-sleep between syncs,
// so "ok" only means the last sync is not overdue: it may be twice the
// reported refresh old before a device counts as "late".
func deviceStatus(d Device, st deviceState, now time.Time) string {
	if st.APIKey == "" {
		return "unregistered"
	}
	if st.LastSeen.IsZero() {
		return "never"
	}
	interval := time.Duration(st.LastRefresh) * time.Second
	if interval <= 0 {
		interval = d.refresh()
	}
	if now.Sub(st.LastSeen) <= 2*interval {
		return "ok"
	}
	return "late"
}

// stateLocked returns the state of a device key (its MAC), creating it if
// needed; callers hold s.mu.
func (s *Server) stateLocked(key string) *deviceState {
	st := s.state[key]
	if st == nil {
		st = &deviceState{}
		s.state[key] = st
	}
	return st
}

// stateSnapshotLocked serializes one device's state; callers hold s.mu and
// write the result with saveState after unlocking, so that a slow disk cannot
// block other requests.
func (s *Server) stateSnapshotLocked(st *deviceState) []byte {
	if st == nil {
		return nil
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		slog.Error("failed to encode device state", "error", err)
		return nil
	}
	return data
}

// saveState persists the state of one device; call it without the lock.
func (s *Server) saveState(key string, data []byte) {
	if data == nil || key == "" {
		return
	}
	if err := writeStateFile(deviceStateFile(s.devicesDir, key), data); err != nil {
		slog.Error("failed to store device state", "device", key, "error", err)
	}
}

func newAPIKey() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// handlePreview composes a page as PNG (or text for text displays), so a
// layout can be checked without a device.
func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	// The preview shows a page of the instance (ADR 0096): no shared cache may
	// keep it.
	w.Header().Set("Cache-Control", "no-store")
	display, name := r.PathValue("display"), r.PathValue("page")
	cfg := s.config()
	p, ok := cfg.Profiles[display]
	if !ok {
		http.NotFound(w, r)
		return
	}
	now := s.now()
	if v := r.URL.Query().Get("at"); v != "" {
		loc := cfg.Timezone
		if loc == nil {
			loc = time.Local
		}
		t, err := time.ParseInLocation(atLayout, v, loc)
		if err != nil {
			http.Error(w, fmt.Sprintf("at: expected %s, got %q", atLayout, v), http.StatusBadRequest)
			return
		}
		now = t
	}
	if p.text() {
		text, err := s.renderText(cfg, display, name, now, pagePosition{index: 0, count: 1})
		if errors.Is(err, page.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			slog.Error("page preview failed", "display", display, "page", name, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(strings.Join(text.Lines, "\n") + "\n"))
		return
	}
	rp, err := s.renderPage(cfg, display, name, now, pagePosition{index: 0, count: 1})
	if errors.Is(err, page.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("page preview failed", "display", display, "page", name, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writePNG(w, rp.data)
}

// handleRenderedImage serves a page rendered from a template. An outdated hash
// still returns the newest render, so a device that missed a change catches up.
func (s *Server) handleRenderedImage(w http.ResponseWriter, r *http.Request) {
	if _, ok := strings.CutSuffix(r.PathValue("file"), ".png"); !ok {
		http.NotFound(w, r)
		return
	}
	key := r.PathValue("profile") + "/" + r.PathValue("page")
	s.mu.Lock()
	rp := s.rendered[key]
	s.mu.Unlock()
	if rp == nil {
		http.NotFound(w, r)
		return
	}
	s.writePageImage(w, r, rp.data)
}

// handleHint serves notice pages such as "Plan fehlt".
func (s *Server) handleHint(w http.ResponseWriter, r *http.Request) {
	key, ok := strings.CutSuffix(r.PathValue("file"), ".png")
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	hp := s.hints[key]
	s.mu.Unlock()
	if hp.data == nil {
		http.NotFound(w, r)
		return
	}
	s.writePageImage(w, r, hp.data)
}

// qoiLimit is the response buffer the firmware allocates for a page: a
// compile-time constant there (max_response_buffer_size in common.yaml, 512 KiB
// in PSRAM), and a larger answer is truncated, which makes the device keep its
// old picture. A four-level 800x480 page cannot exceed one QOI byte per pixel
// (384 KB), so the buffer holds every page of the shipped panels; Paperboy
// keeps its own limit in step and only has to simplify a page above it
// (ADR 0088).
const qoiLimit = 512 * 1024

// writePageImage serves a rendered page as PNG (browsers, the dashboard) or, on
// ?format=qoi, as QOI for the firmware, which decodes that far faster
// (ADR 0072). The device's shown hash stays the hash of the PNG, so the
// encoding is the device's business only, including a page that had to be
// simplified to fit the firmware's buffer (ADR 0088).
func (s *Server) writePageImage(w http.ResponseWriter, r *http.Request, data []byte) {
	if r.URL.Query().Get("format") != "qoi" {
		writePNG(w, data)
		return
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		slog.Error("qoi encoding failed", "error", err)
		writePNG(w, data)
		return
	}
	qoi := qoiWithin(img, qoiLimit)
	s.noteQOI(r.URL.Path, qoi)
	w.Header().Set("Content-Type", "image/qoi")
	w.Header().Set("Content-Length", strconv.Itoa(len(qoi.data)))
	w.Write(qoi.data)
}

// noteQOI reports a page that Paperboy could not send as it is, once per page
// and reduction.
func (s *Server) noteQOI(path string, qoi qoiResult) {
	note := qoi.note
	if !qoi.fits {
		note = "does not fit the buffer even without dither"
	}
	if note == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.qoiNotes == nil {
		s.qoiNotes = map[string]string{}
	}
	if s.qoiNotes[path] == note {
		return
	}
	s.qoiNotes[path] = note
	if !qoi.fits {
		slog.Error("page is too large as QOI for the firmware", "page", path, "bytes", len(qoi.data), "limit", qoiLimit)
		return
	}
	slog.Warn("page sent simplified for the firmware", "page", path, "bytes", len(qoi.data), "reduction", note)
}

func writePNG(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Write(data)
}
