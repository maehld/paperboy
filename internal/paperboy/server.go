package paperboy

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Server implements the upload API for producers and the TRMNL device API.
type Server struct {
	cfg *Config
	dir string
	now func() time.Time

	mu     sync.Mutex
	images map[string][]*pageImage // profile → pages
	state  map[string]*deviceState // MAC → state
}

// New loads stored images and device state from dir.
func New(cfg *Config, dir string) (*Server, error) {
	images, err := loadImages(dir, cfg.Profiles)
	if err != nil {
		return nil, err
	}
	state, err := loadState(filepath.Join(dir, "state.json"))
	if err != nil {
		return nil, err
	}
	return &Server{cfg: cfg, dir: dir, now: time.Now, images: images, state: state}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/profiles", s.handleProfiles)
	mux.HandleFunc("PUT /api/images/{profile}/{page}", s.handleUpload)
	mux.HandleFunc("GET /api/devices", s.handleDevices)
	mux.HandleFunc("GET /api/setup", s.handleSetup)
	mux.HandleFunc("GET /api/setup/", s.handleSetup)
	mux.HandleFunc("GET /api/display", s.handleDisplay)
	mux.HandleFunc("GET /api/display/", s.handleDisplay)
	mux.HandleFunc("POST /api/log", s.handleLog)
	mux.HandleFunc("GET /images/{key}", s.handleImage)
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
	Pages int `json:"pages"`
}

func (s *Server) handleProfiles(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := []profileInfo{}
	for name, p := range s.cfg.Profiles {
		list = append(list, profileInfo{name, p, len(s.images[name])})
	}
	writeJSON(w, http.StatusOK, map[string]any{"profiles": list})
}

func (s *Server) authorized(r *http.Request) bool {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.UploadToken)) == 1
}

// handleUpload stores one page. Pages must be uploaded without gaps, starting at 0.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "nicht berechtigt", http.StatusUnauthorized)
		return
	}
	name := r.PathValue("profile")
	profile, ok := s.cfg.Profiles[name]
	if !ok {
		http.Error(w, "unbekanntes Profil", http.StatusNotFound)
		return
	}
	page, err := strconv.Atoi(r.PathValue("page"))
	if err != nil || page < 0 || page >= maxPages {
		http.Error(w, "ungültige Seite", http.StatusBadRequest)
		return
	}
	img, err := png.Decode(http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil {
		http.Error(w, "kein gültiges PNG: "+err.Error(), http.StatusBadRequest)
		return
	}
	if img.Bounds().Dx() != profile.Width || img.Bounds().Dy() != profile.Height {
		http.Error(w, fmt.Sprintf("Bildgröße %dx%d erwartet", profile.Width, profile.Height), http.StatusBadRequest)
		return
	}
	stored := newPageImage(normalize(img, profile), s.now())
	s.mu.Lock()
	pages := s.images[name]
	if page > len(pages) {
		s.mu.Unlock()
		http.Error(w, fmt.Sprintf("Seite %d fehlt noch", len(pages)), http.StatusConflict)
		return
	}
	s.mu.Unlock()
	if err := writeAtomic(imagePath(s.dir, name, page), stored.data); err != nil {
		slog.Error("failed to store image", "profile", name, "page", page, "error", err)
		http.Error(w, "Speichern fehlgeschlagen", http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	if page == len(s.images[name]) {
		s.images[name] = append(s.images[name], stored)
	} else {
		s.images[name][page] = stored
	}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

type deviceInfo struct {
	Type    string `json:"type"`
	MAC     string `json:"mac,omitempty"`
	Address string `json:"address,omitempty"`
	Name    string `json:"name"`
	Profile string `json:"profile"`
	deviceState
	APIKey     string `json:"api_key,omitempty"` // shadows the embedded key; never exposed
	Registered bool   `json:"registered"`
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := []deviceInfo{}
	for _, d := range s.cfg.Devices {
		info := deviceInfo{Type: d.Type, MAC: d.MAC, Address: d.Address, Name: d.Name, Profile: d.Profile}
		if st := s.state[d.key()]; st != nil {
			info.deviceState = *st
			info.Registered = st.APIKey != "" || d.Type == TypeOpenDisplay
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

// pageImage returns the page to show, with the stale banner if the upload is
// too old. ok is false while nothing was uploaded for the profile.
func (s *Server) pageImage(profile string, page int, now time.Time) (img *image.Paletted, pages int, ok bool) {
	s.mu.Lock()
	all := s.images[profile]
	s.mu.Unlock()
	if len(all) == 0 {
		return nil, 0, false
	}
	stored := all[min(max(page, 0), len(all)-1)]
	if now.Sub(stored.uploaded) > time.Duration(s.cfg.StaleAfter) {
		return withBanner(stored.img, "Keine neuen Daten seit "+stored.uploaded.Local().Format("02.01. 15:04")), len(all), true
	}
	return stored.img, len(all), true
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
		writeJSON(w, http.StatusOK, map[string]any{"status": 404, "api_key": nil, "friendly_id": nil, "image_url": nil, "message": "Gerät nicht freigegeben"})
		return
	}
	s.mu.Lock()
	st := s.state[d.MAC]
	if st == nil {
		st = &deviceState{}
		s.state[d.MAC] = st
	}
	if st.APIKey == "" {
		st.APIKey, st.FriendlyID = newAPIKey(), friendlyID(d.MAC)
	}
	st.Model, st.Firmware = r.Header.Get("Model"), r.Header.Get("FW-Version")
	s.saveStateLocked()
	key, friendly := st.APIKey, st.FriendlyID
	s.mu.Unlock()
	slog.Info("display set up", "device", d.Name, "mac", d.MAC)
	writeJSON(w, http.StatusOK, map[string]any{"status": 200, "api_key": key, "friendly_id": friendly,
		"image_url": s.baseURL(r) + "/images/" + d.Profile + "-setup.png", "message": "Registriert als " + d.Name})
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
	st := s.state[mac]
	if d != nil && (st == nil || st.APIKey == "") && token != "" {
		// State was lost: accept the key the device still has instead of forcing a new setup.
		st = &deviceState{APIKey: token, FriendlyID: friendlyID(d.MAC)}
		s.state[mac] = st
		slog.Info("device key adopted", "device", d.Name)
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
	pages := s.images[d.Profile]
	switch {
	case critical: // the warning replaces all pages; buttons only show it again
		st.Page = 0
	case special: // double click: back to the overview
		st.Page, st.LastInput = 0, now
	case buttonWake(st.WakeReason) && !firstContact:
		st.Page, st.LastInput = st.Page+1, now
	case st.Page != 0 && now.Sub(st.LastInput) >= pageTimeout:
		st.Page = 0
	}
	if len(pages) == 0 || st.Page >= len(pages) || st.Page < 0 {
		st.Page = 0
	}
	page := st.Page
	refresh := d.refresh(now)
	if page != 0 {
		refresh = min(refresh, max(minRefresh, pageTimeout-now.Sub(st.LastInput)+5*time.Second))
	}
	key := d.Profile + "-setup"
	volts := voltageKey(st.BatteryVoltage)
	switch {
	case critical:
		key = d.Profile + "-battery-" + volts
	case len(pages) > 0:
		img := pages[page]
		key = fmt.Sprintf("%s-%d-%s", d.Profile, page, img.hash)
		if now.Sub(img.uploaded) > time.Duration(s.cfg.StaleAfter) {
			key += "-stale"
		}
		if st.BatteryLevel == batteryWarn {
			key += "-low" + volts
		}
	}
	logged := *st
	s.saveStateLocked()
	s.mu.Unlock()
	if logged.BatteryLevel != prevLevel {
		level := logged.BatteryLevel
		if level == batteryOK {
			level = "ok"
		}
		slog.Warn("battery warning", "device", d.Name, "level", level, "battery", logged.BatteryVoltage)
	}
	slog.Info("display request", "device", d.Name, "page", page, "wake", logged.WakeReason, "special", special,
		"battery", logged.BatteryVoltage, "rssi", logged.RSSI, "refresh", refresh)
	resp := map[string]any{"status": 0, "image_url": s.baseURL(r) + "/images/" + key + ".png", "filename": key,
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
		name = d.Name
	}
	slog.Warn("display log", "device", name, "log", string(body))
	w.WriteHeader(http.StatusNoContent)
}

// handleImage serves keys of the form <profile>-setup, <profile>-battery-<decivolts>
// and <profile>-<page>-<hash>, optionally followed by -stale and -low<decivolts>.
// A changed hash still returns the newest page.
func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	key, ok := strings.CutSuffix(r.PathValue("key"), ".png")
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	var data []byte
	for name, p := range s.cfg.Profiles {
		rest, ok := strings.CutPrefix(key, name+"-")
		if !ok {
			continue
		}
		if rest == "setup" {
			data = encodePNG(placeholder(p, "Paperboy", "Warte auf Bilder …"))
			break
		}
		if volts, ok := strings.CutPrefix(rest, "battery-"); ok {
			data = encodePNG(criticalBatteryImage(p, volts))
			break
		}
		parts := strings.Split(rest, "-")
		page, err := strconv.Atoi(parts[0])
		pages := s.images[name]
		if err != nil || len(parts) < 2 || page < 0 || page >= len(pages) {
			continue
		}
		img := pages[page]
		data = img.data
		var banners []string
		for _, flag := range parts[2:] {
			if flag == "stale" {
				banners = append(banners, "Keine neuen Daten seit "+img.uploaded.Local().Format("02.01. 15:04"))
			} else if volts, ok := strings.CutPrefix(flag, "low"); ok {
				banners = append(banners, lowBatteryText(volts))
			}
		}
		if len(banners) > 0 {
			data = encodePNG(withBanner(img.img, banners...))
		}
		break
	}
	s.mu.Unlock()
	if data == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Write(data)
}
