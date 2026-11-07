package paperboy

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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
	cfg         *configLoader // current configuration, reloaded on change (ADR 0047)
	devicesPath string        // state/devices.json in the instance directory
	now         func() time.Time

	mu    sync.Mutex
	state map[string]*deviceState // device ID → state
	plans *planLoader             // device plans (ADR 0025)

	rendered  map[string]*renderedPage // "<profile>/<page>" → last render
	hints     map[string][]byte        // content hash → notice page
	logs      *logSeen                 // delivered device log entries (own lock)
	firmware  *firmwareStore           // OTA images (ADR 0043; own lock)
	fwNotes   map[string]string        // device ID → last logged reason for withholding an update
	pages     *pageLoader              // pages by file, reloaded on change (own lock)
	skipNotes map[string]string        // "<profile>/<page>" → last logged reason for skipping it
	widgets   *widget.Host             // plugins and their widget cache (ADR 0049; own lock)
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
	devicesPath := filepath.Join(stateDir, devicesFileName)
	state, err := loadState(devicesPath)
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
		if err := writeAtomic(devicesPath, data); err != nil {
			return nil, err
		}
		slog.Info("device state migrated to device ids", "entries", moved)
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
	if cfg.PluginsDir == "" {
		cfg.PluginsDir = filepath.Join(cfg.Dir, pluginsDirName)
	}
	if cfg.FirmwareDir == "" {
		cfg.FirmwareDir = filepath.Join(cfg.Dir, firmwareDirName)
	}
	s := &Server{cfg: newConfigLoader(cfg.Dir, cfg),
		widgets:     &widget.Host{Builtin: map[string]plugin.Handler{builtin.Name: builtin.Plugin{}}},
		devicesPath: devicesPath, now: time.Now, state: state,
		plans: newPlanLoader(), rendered: map[string]*renderedPage{}, hints: map[string][]byte{},
		logs: newLogSeen(), fwNotes: map[string]string{}, pages: newPageLoader(), skipNotes: map[string]string{}}
	s.firmware = newFirmwareStore(cfg.FirmwareDir)
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

// Start runs the plugins and the configuration reload loop until ctx is done
// (ADR 0047, 0049). Plugins start on first use and stop with ctx.
func (s *Server) Start(ctx context.Context) {
	s.widgets.Start(ctx)
	go s.configLoop(ctx)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/profiles", s.handleProfiles)
	mux.HandleFunc("GET /api/plugins", s.handlePlugins)
	mux.HandleFunc("GET /api/devices", s.handleDevices)
	mux.HandleFunc("GET /api/preview/{display}/{page}", s.handlePreview)
	mux.HandleFunc("GET /pages/{profile}/{page}/{file}", s.handleRenderedImage)
	mux.HandleFunc("GET /hints/{file}", s.handleHint)
	mux.HandleFunc("GET /firmware/{hardware}/{file}", s.handleFirmware)
	mux.HandleFunc("POST /api/device/register", s.handleRegister)
	mux.HandleFunc("POST /api/device/sync", s.handleSync)
	mux.HandleFunc("GET /api/device/text", s.handleText)
	mux.HandleFunc("GET /{$}", s.handleRoot)
	mux.HandleFunc("GET /ui", s.handleDashboard)
	mux.HandleFunc("GET /ui/plugins", s.handleGallery)
	mux.HandleFunc("GET /ui/plan", s.handlePlanView)
	mux.HandleFunc("GET /api/widget/{plugin}/{widget}", s.handleWidget)
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
	LastPageURL       string `json:"last_page_url,omitempty"`      // where the last served image sits
	FirmwareAvailable string `json:"firmware_available,omitempty"` // newest image for the hardware (ADR 0043)
	FirmwareStatus    string `json:"firmware_status"`              // current, offered, available, failed, held or none
	Power             string `json:"power"`                        // effective power source: battery or mains (ADR 0051)
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"devices": s.deviceInfos()})
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
		info := deviceInfo{ID: d.ID, Type: d.Type, MAC: d.MAC, Profile: d.Profile, Plan: d.Plan}
		if st := s.state[d.key()]; st != nil {
			info.deviceState = *st
			info.Registered = st.APIKey != ""
		}
		info.Status = deviceStatus(d, info.deviceState, now)
		info.Power = cfg.power(&d, info.ReportedPower)
		// Rendered images live in memory only: after a restart the stored
		// hash points nowhere, so the URL is left out instead of a dead link.
		if info.LastPageHash != "" && !cfg.Profiles[d.Profile].text() {
			if info.LastPageKind == "page" && info.LastPage != "" {
				if s.rendered[d.Profile+"/"+info.LastPage] != nil {
					info.LastPageURL = fmt.Sprintf("/pages/%s/%s/%s.png", d.Profile, info.LastPage, info.LastPageHash)
				}
			} else if s.hints[info.LastPageHash] != nil {
				info.LastPageURL = "/hints/" + info.LastPageHash + ".png"
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
			img, _ = s.firmware.latest(hw)
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

// stateLocked returns the device state, creating it if needed; callers hold s.mu.
func (s *Server) stateLocked(key string) *deviceState {
	st := s.state[key]
	if st == nil {
		st = &deviceState{}
		s.state[key] = st
	}
	return st
}

// stateSnapshotLocked serializes device state; callers hold s.mu and write the
// result with saveState after unlocking, so that a slow disk cannot block other
// requests.
func (s *Server) stateSnapshotLocked() []byte {
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		slog.Error("failed to encode device state", "error", err)
		return nil
	}
	return data
}

// saveState persists a snapshot from stateSnapshotLocked outside the lock.
func (s *Server) saveState(data []byte) {
	if data == nil {
		return
	}
	if err := writeAtomic(s.devicesPath, data); err != nil {
		slog.Error("failed to store device state", "error", err)
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
		lines, err := s.renderText(cfg, display, name, now, pagePosition{index: 0, count: 1})
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
		w.Write([]byte(strings.Join(lines, "\n") + "\n"))
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
