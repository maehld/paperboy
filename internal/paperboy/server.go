package paperboy

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/internal/data"
	"github.com/maehld/paperboy/internal/page"
)

// Server implements the TRMNL device API and the data document. Pages are
// rendered from templates, notice pages replace uploads.
type Server struct {
	cfg *Config
	dir string
	now func() time.Time

	mu    sync.Mutex
	state map[string]*deviceState // device ID â†’ state
	doc   *data.Document          // shared data document (ADR 0024)
	plans *planLoader             // plans of TRMNL devices (ADR 0025)

	rendered map[string]*renderedPage // "<profile>/<page>" â†’ last render
	hints    map[string][]byte        // content hash â†’ notice page
}

// New loads device state from dir and prepares templates and plans.
func New(cfg *Config, dir string) (*Server, error) {
	state, err := loadState(filepath.Join(dir, "state.json"))
	if err != nil {
		return nil, err
	}
	if moved, err := migrateState(state, cfg.Devices); err != nil {
		return nil, err
	} else if moved > 0 {
		data, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			return nil, err
		}
		if err := writeAtomic(filepath.Join(dir, "state.json"), data); err != nil {
			return nil, err
		}
		slog.Info("device state migrated to device ids", "entries", moved)
	}
	s := &Server{cfg: cfg, dir: dir, now: time.Now, state: state,
		plans: newPlanLoader(), rendered: map[string]*renderedPage{}, hints: map[string][]byte{}}
	if s.cfg.Pages == "" {
		s.cfg.Pages = filepath.Join(dir, "pages")
	}
	if s.cfg.Plans == "" {
		s.cfg.Plans = filepath.Join(dir, "plans")
	}
	// A plan named in the configuration must exist (ADR 0026). A broken file is
	// reported and replaced by a notice page instead.
	for i := range s.cfg.Devices {
		d := &s.cfg.Devices[i]
		if d.Plan == "" {
			continue
		}
		path := filepath.Join(s.cfg.Plans, d.Plan+".yaml")
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("device %s: plan %q: %w", d.ID, d.Plan, err)
		}
		if p, err := s.plans.get(s.cfg.Plans, d.Plan); p == nil || err != nil {
			slog.Error("plan not usable", "device", d.ID, "plan", d.Plan, "error", err)
		}
	}
	// The document shares the server clock so that tests can move time.
	s.doc, err = data.New(filepath.Join(dir, "data.json"), func() time.Time { return s.now() })
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/profiles", s.handleProfiles)
	mux.HandleFunc("PUT /api/data", s.handlePutData)
	mux.HandleFunc("PUT /api/data/{path...}", s.handlePutData)
	mux.HandleFunc("GET /api/data", s.handleGetData)
	mux.HandleFunc("GET /api/data/{path...}", s.handleGetData)
	mux.HandleFunc("DELETE /api/data/{path...}", s.handleDeleteData)
	mux.HandleFunc("GET /api/devices", s.handleDevices)
	mux.HandleFunc("GET /api/preview/{display}/{page}", s.handlePreview)
	mux.HandleFunc("GET /pages/{profile}/{page}/{file}", s.handleRenderedImage)
	mux.HandleFunc("GET /hints/{file}", s.handleHint)
	mux.HandleFunc("GET /api/setup", s.handleSetup)
	mux.HandleFunc("GET /api/setup/", s.handleSetup)
	mux.HandleFunc("GET /api/display", s.handleDisplay)
	mux.HandleFunc("GET /api/display/", s.handleDisplay)
	mux.HandleFunc("POST /api/log", s.handleLog)
	return mux
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
	s.mu.Lock()
	defer s.mu.Unlock()
	list := []profileInfo{}
	for name, p := range s.cfg.Profiles {
		list = append(list, profileInfo{Name: name, Profile: p})
	}
	writeJSON(w, http.StatusOK, map[string]any{"profiles": list})
}

func (s *Server) authorized(r *http.Request) bool {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.UploadToken)) == 1
}

// maxDataBytes caps a single write to the data document.
const maxDataBytes = 1 << 20

// handlePutData replaces the subtree at the path. PUT never merges: write a
// deeper path to update part of a branch.
func (s *Server) handlePutData(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var value any
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxDataBytes)).Decode(&value); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "document too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	var at time.Time
	if h := strings.TrimSpace(r.Header.Get("X-Data-Time")); h != "" {
		t, err := time.Parse(time.RFC3339, h)
		if err != nil {
			http.Error(w, "invalid X-Data-Time: expected RFC 3339", http.StatusBadRequest)
			return
		}
		at = t
	}
	if err := s.doc.Put(r.PathValue("path"), value, at); err != nil {
		writeDataError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetData(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	v, ok := s.doc.Get(r.PathValue("path"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleDeleteData(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := s.doc.Delete(r.PathValue("path")); err != nil {
		writeDataError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeDataError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, data.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, data.ErrConflict):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

type deviceInfo struct {
	ID      string `json:"id"`
	Type    string `json:"protocol"`
	MAC     string `json:"mac,omitempty"`
	Profile string `json:"display_type"`
	Plan    string `json:"plan,omitempty"`
	deviceState
	APIKey     string `json:"api_key,omitempty"` // shadows the embedded key; never exposed
	Registered bool   `json:"registered"`
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := []deviceInfo{}
	for _, d := range s.cfg.Devices {
		info := deviceInfo{ID: d.ID, Type: d.Type, MAC: d.MAC, Profile: d.Profile, Plan: d.Plan}
		if st := s.state[d.key()]; st != nil {
			info.deviceState = *st
			info.Registered = st.APIKey != ""
		}
		list = append(list, info)
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": list})
}

// stateLocked returns the device state, creating it if needed; callers hold s.mu.
func (s *Server) stateLocked(key string) *deviceState {
	st := s.state[key]
	if st == nil {
		st = &deviceState{}
		s.state[key] = st
	}
	return st
}

func (s *Server) baseURL(r *http.Request) string {
	if s.cfg.PublicURL != "" {
		return s.cfg.PublicURL
	}
	return "http://" + r.Host
}

// saveStateLocked persists device state; callers hold s.mu.
func (s *Server) saveStateLocked() {
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err == nil {
		err = writeAtomic(filepath.Join(s.dir, "state.json"), data)
	}
	if err != nil {
		slog.Error("failed to store device state", "error", err)
	}
}

func newAPIKey() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func friendlyID(mac string) string {
	return strings.ReplaceAll(mac[len(mac)-8:], ":", "")
}

// handleSetup trades a known MAC address for an API key. Unknown devices are
// rejected so that only configured displays can register.
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	mac := normalizeMAC(r.Header.Get("ID"))
	d := s.cfg.device(mac)
	if d == nil {
		slog.Warn("setup from unknown device; add its MAC to the configuration", "mac", mac, "model", r.Header.Get("Model"))
		writeJSON(w, http.StatusOK, map[string]any{"status": 404, "api_key": nil, "friendly_id": nil, "image_url": nil, "message": "device not allowed"})
		return
	}
	s.mu.Lock()
	st := s.state[d.ID]
	if st == nil {
		st = &deviceState{}
		s.state[d.ID] = st
	}
	if st.APIKey == "" {
		st.APIKey, st.FriendlyID = newAPIKey(), friendlyID(d.MAC)
	}
	st.Model, st.Firmware = r.Header.Get("Model"), r.Header.Get("FW-Version")
	notice := s.hint(d.Profile, "Paperboy", "GerÃ¤t eingerichtet â€“ warte auf Plan")
	s.saveStateLocked()
	key, friendly := st.APIKey, st.FriendlyID
	s.mu.Unlock()
	slog.Info("display set up", "device", d.ID, "mac", d.MAC)
	writeJSON(w, http.StatusOK, map[string]any{"status": 200, "api_key": key, "friendly_id": friendly,
		"image_url": s.baseURL(r) + notice.path, "message": "Registered as " + d.ID})
}

// buttonWake reports whether the device was woken by its button rather than the timer.
func buttonWake(source string) bool {
	switch strings.ToLower(source) {
	case "", "timer", "powercycle", "unknown":
		return false
	}
	return true
}

func (s *Server) handleDisplay(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	mac := normalizeMAC(r.Header.Get("ID"))
	token := r.Header.Get("Access-Token")
	d := s.cfg.device(mac)
	s.mu.Lock()
	var st *deviceState
	if d != nil {
		st = s.state[d.ID]
	}
	if d != nil && (st == nil || st.APIKey == "") && token != "" {
		// State was lost: accept the key the device still has instead of forcing a new setup.
		st = &deviceState{APIKey: token, FriendlyID: friendlyID(d.MAC)}
		s.state[d.ID] = st
		slog.Info("device key adopted", "device", d.ID)
	}
	if d == nil || st == nil || subtle.ConstantTimeCompare([]byte(token), []byte(st.APIKey)) != 1 {
		s.mu.Unlock()
		slog.Warn("request from unknown device or with wrong key", "mac", mac)
		// Never 500: the firmware treats it as a reset and erases its credentials.
		writeJSON(w, http.StatusOK, map[string]any{"status": 202, "refresh_rate": 600})
		return
	}
	firstContact := st.LastSeen.IsZero() // wake reason after setup still reflects the setup boot
	st.LastSeen = now
	st.BatteryVoltage, st.RSSI = r.Header.Get("Battery-Voltage"), r.Header.Get("RSSI")
	st.Firmware, st.WakeReason = r.Header.Get("FW-Version"), r.Header.Get("Update-Source")
	if model := r.Header.Get("Model"); model != "" {
		st.Model = model
	}
	prevLevel := st.BatteryLevel
	st.BatteryLevel = nextBatteryLevel(s.cfg.battery(d), prevLevel, st.BatteryVoltage)
	critical := st.BatteryLevel == batteryCritical
	pageTimeout := time.Duration(s.cfg.PageTimeout)
	special := r.Header.Get("special_function") != ""

	var served servedPage
	floor := s.cfg.minRefresh(d.Profile)
	switch {
	case critical: // the warning replaces all pages; buttons only show it again
		st.Page = 0
		served = s.notice(d.Profile, criticalBatteryImage(s.cfg.Profiles[d.Profile], voltageKey(st.BatteryVoltage)))
		served.refresh = max(floor, d.refresh())
	case d.Plan != "":
		served = s.planPage(d, st, now, firstContact, special, pageTimeout)
	default:
		served = s.hint(d.Profile, "Kein Plan konfiguriert", "FÃ¼r dieses GerÃ¤t ist in der Konfiguration kein Plan hinterlegt.")
	}
	served.refresh = max(served.refresh, floor)
	page := served.page
	refresh := served.refresh
	logged := *st
	s.saveStateLocked()
	s.mu.Unlock()
	if logged.BatteryLevel != prevLevel {
		level := logged.BatteryLevel
		if level == batteryOK {
			level = "ok"
		}
		slog.Warn("battery warning", "device", d.ID, "level", level, "battery", logged.BatteryVoltage)
	}
	slog.Info("display request", "device", d.ID, "page", page, "rule", logged.Rule, "wake", logged.WakeReason,
		"special", special, "battery", logged.BatteryVoltage, "rssi", logged.RSSI, "refresh", refresh)
	resp := map[string]any{"status": 0, "image_url": s.baseURL(r) + served.path, "filename": served.filename,
		"refresh_rate": int(refresh.Seconds()), "reset_firmware": false, "update_firmware": false, "firmware_url": nil,
		"special_function": "restart_playlist"}
	if special {
		resp["action"] = "restart_playlist"
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleLog(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	name := normalizeMAC(r.Header.Get("ID"))
	if d := s.cfg.device(name); d != nil {
		name = d.ID
	}
	slog.Warn("display log", "device", name, "log", string(body))
	w.WriteHeader(http.StatusNoContent)
}

// handlePreview renders a page template from the data document as PNG, so a
// layout can be checked without a device.
func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	display, name := r.PathValue("display"), r.PathValue("page")
	if _, ok := s.cfg.Profiles[display]; !ok {
		http.NotFound(w, r)
		return
	}
	rp, err := s.renderPage(display, name, s.now())
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
	writePNG(w, rp.data)
}

// handleHint serves notice pages such as "Plan fehlt".
func (s *Server) handleHint(w http.ResponseWriter, r *http.Request) {
	key, ok := strings.CutSuffix(r.PathValue("file"), ".png")
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	data := s.hints[key]
	s.mu.Unlock()
	if data == nil {
		http.NotFound(w, r)
		return
	}
	writePNG(w, data)
}

func writePNG(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Write(data)
}
