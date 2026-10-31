package paperboy

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Button roles of the device API (ADR 0042). The firmware maps its physical
// buttons onto these roles per hardware type.
const (
	buttonNext     = "next"
	buttonPrevious = "previous"
	buttonHome     = "home"
)

// listenAfterInput is how long a device stays awake for further input after a
// button press or touch before it goes back to deep sleep.
const listenAfterInput = 20 * time.Second

// maxSyncBytes caps a register or sync body; log entries make up most of it.
const maxSyncBytes = 64 << 10

type registerRequest struct {
	MAC      string `json:"mac"`
	Hardware string `json:"hardware"`
	Firmware string `json:"firmware"`
}

type syncEvent struct {
	Type   string `json:"type"`   // "button" or "touch"
	Button string `json:"button"` // button role
	HoldMS int    `json:"hold_ms"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
}

type syncBattery struct {
	Volts   float64  `json:"volts"`
	Percent *float64 `json:"percent"`
	MAH     *float64 `json:"mah"`
	MA      *float64 `json:"ma"`
}

type syncRequest struct {
	Firmware string           `json:"firmware"`
	Hardware string           `json:"hardware"`
	Reason   string           `json:"reason"`
	Events   []syncEvent      `json:"events"`
	Shown    string           `json:"shown"`
	Battery  *syncBattery     `json:"battery"`
	RSSI     *int             `json:"rssi"`
	Log      []deviceLogEntry `json:"log"`
}

type syncImage struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
}

// syncText is the content of a text display (ADR 0044).
type syncText struct {
	Lines []string `json:"lines"`
	Hash  string   `json:"hash"`
}

type syncResponse struct {
	Image    *syncImage     `json:"image"`          // null for text displays
	Text     *syncText      `json:"text,omitempty"` // text displays only
	RefreshS int            `json:"refresh_s"`
	ListenS  int            `json:"listen_s"`
	Time     int64          `json:"time"`
	Firmware *firmwareOffer `json:"firmware"` // update offer (ADR 0043); null when none
}

// validate checks the parts of a sync that change device state; anything the
// firmware gets wrong is answered with 400 instead of being guessed.
func (r *syncRequest) validate() error {
	switch r.Reason {
	case "timer", "button", "touch", "powercycle":
	default:
		return fmt.Errorf("reason %q: expected timer, button, touch or powercycle", r.Reason)
	}
	for i, e := range r.Events {
		switch e.Type {
		case "button":
			switch e.Button {
			case buttonNext, buttonPrevious, buttonHome:
			default:
				return fmt.Errorf("event %d: button %q: expected next, previous or home", i, e.Button)
			}
			if e.HoldMS < 0 {
				return fmt.Errorf("event %d: hold_ms must not be negative", i)
			}
		case "touch":
		default:
			return fmt.Errorf("event %d: type %q: expected button or touch", i, e.Type)
		}
	}
	if r.Battery != nil && (r.Battery.Volts <= 0 || r.Battery.Volts > 10) {
		return errors.New("battery: volts missing or out of range")
	}
	return nil
}

// decodeDeviceJSON reads a size-limited JSON body; unknown fields are ignored
// so newer firmware can add fields without breaking older servers.
func decodeDeviceJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSyncBytes)).Decode(v); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

// handleRegister trades a configured MAC address for the device key. A known
// device keeps its key, so a reflashed display picks up its old state.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !decodeDeviceJSON(w, r, &req) {
		return
	}
	d := s.cfg.device(req.MAC)
	if d == nil {
		slog.Warn("registration from unknown device; add its MAC to the configuration",
			"mac", normalizeMAC(req.MAC), "hardware", req.Hardware, "firmware", req.Firmware)
		http.Error(w, "device not configured", http.StatusForbidden)
		return
	}
	s.mu.Lock()
	st := s.stateLocked(d.key())
	if st.APIKey == "" {
		st.APIKey = newAPIKey()
	}
	st.Hardware, st.Firmware = req.Hardware, req.Firmware
	key := st.APIKey
	stateData := s.stateSnapshotLocked()
	s.mu.Unlock()
	s.saveState(stateData)
	slog.Info("display registered", "device", d.ID, "mac", d.MAC, "hardware", req.Hardware, "firmware", req.Firmware)
	writeJSON(w, http.StatusOK, map[string]string{"key": key, "device": d.ID})
}

// deviceByKeyLocked finds the device that owns the bearer key; callers hold s.mu.
func (s *Server) deviceByKeyLocked(r *http.Request) (*Device, *deviceState) {
	key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || key == "" {
		return nil, nil
	}
	for i := range s.cfg.Devices {
		d := &s.cfg.Devices[i]
		st := s.state[d.key()]
		if st != nil && st.APIKey != "" && subtle.ConstantTimeCompare([]byte(key), []byte(st.APIKey)) == 1 {
			return d, st
		}
	}
	return nil, nil
}

// handleSync is the single request of a wake (ADR 0042): it records what the
// device reports, applies its button events to the plan cursor, renders the
// page and tells the device what to show and how long to sleep.
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	var req syncRequest
	if !decodeDeviceJSON(w, r, &req) {
		return
	}
	// The firmware directory is read before taking the state lock.
	var img *firmwareImage
	if req.Hardware != "" {
		img, _ = s.firmware.latest(req.Hardware)
	}
	s.mu.Lock()
	d, st := s.deviceByKeyLocked(r)
	if d == nil {
		s.mu.Unlock()
		slog.Warn("sync with unknown key; the device registers again", "remote", r.RemoteAddr)
		http.Error(w, "unknown key", http.StatusUnauthorized)
		return
	}
	if err := req.validate(); err != nil {
		s.mu.Unlock()
		slog.Warn("invalid sync", "device", d.ID, "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	firstContact := st.LastSeen.IsZero()
	st.LastSeen = now
	st.WakeReason = req.Reason
	if req.Firmware != "" {
		st.Firmware = req.Firmware
	}
	if req.Hardware != "" {
		st.Hardware = req.Hardware
	}
	if req.RSSI != nil {
		st.RSSI = strconv.Itoa(*req.RSSI)
	}
	if b := req.Battery; b != nil {
		st.BatteryVoltage = strconv.FormatFloat(b.Volts, 'f', 2, 64)
		st.BatteryPercent, st.BatteryMAH = b.Percent, b.MAH
	}
	prevLevel := st.BatteryLevel
	st.BatteryLevel = nextBatteryLevel(s.cfg.battery(d), prevLevel, st.BatteryVoltage)
	critical := st.BatteryLevel == batteryCritical
	pageTimeout := time.Duration(s.cfg.PageTimeout)
	s.settleFirmwareLocked(d, st)
	var volts float64
	if req.Battery != nil {
		volts = req.Battery.Volts
	}
	offer := s.offerFirmwareLocked(d, st, img, volts, req.Battery != nil, now)

	var buttons []string
	input := req.Reason == "button" || req.Reason == "touch"
	for _, e := range req.Events {
		input = true
		if e.Type == "button" {
			buttons = append(buttons, e.Button)
		}
	}

	floor := s.cfg.minRefresh(d.Profile)
	textDisplay := s.cfg.Profiles[d.Profile].text()
	var choice planChoice
	switch {
	case critical: // the warning replaces all pages; buttons only show it again
		st.Page = 0
	case d.Plan != "":
		choice = s.choosePlanPage(d, st, now, firstContact, buttons, pageTimeout, false)
	default:
		choice.title, choice.text = s.cfg.texts().noPlan()
	}
	logged := *st
	stateData := s.stateSnapshotLocked()
	s.mu.Unlock()
	s.saveState(stateData)
	s.logDevice(d.ID, req.Log)
	for _, e := range req.Events {
		// Touches and long presses carry no meaning yet; they are only recorded.
		if e.Type == "touch" {
			slog.Info("display touch", "device", d.ID, "x", e.X, "y", e.Y)
		} else if e.HoldMS > 0 {
			slog.Info("display button held", "device", d.ID, "button", e.Button, "hold_ms", e.HoldMS)
		}
	}

	// Drawing and rendering happen without the lock: a slow page, a font problem
	// or a panicking template must never block the other devices or the data API.
	var served servedPage
	switch {
	case critical:
		if p := s.cfg.Profiles[d.Profile]; p.text() {
			title, text := s.cfg.texts().criticalBattery(voltageKey(logged.BatteryVoltage))
			served = s.hint(d.Profile, title, text)
		} else {
			served = s.notice(d.Profile, criticalBatteryImage(p, s.cfg.texts(), voltageKey(logged.BatteryVoltage)))
		}
		served.refresh = d.refresh()
	case d.Plan != "":
		served = s.renderPlan(d, choice, now, false)
	default:
		served = s.hint(d.Profile, choice.title, choice.text)
	}
	served.refresh = max(served.refresh, floor)
	refresh := served.refresh
	// Remember what the device was pointed at, so /api/devices can show the
	// current content. Rendering happened above, outside the lock.
	shownKind, shownPage := "page", served.name
	switch {
	case critical:
		shownKind, shownPage = "battery", ""
	case served.name == "":
		shownKind = "notice"
	}
	errText := ""
	switch {
	case choice.err != nil:
		errText = shortError(choice.err)
	case served.err != nil:
		errText = shortError(served.err)
	}
	s.mu.Lock()
	var pageData []byte
	if st = s.state[d.key()]; st != nil {
		st.LastPage, st.LastPageKind, st.LastPageHash = shownPage, shownKind, served.hash
		st.LastPageTime, st.LastRefresh = now, int(refresh.Seconds())
		st.LastText = served.lines
		st.LastError = errText
		pageData = s.stateSnapshotLocked()
	}
	s.mu.Unlock()
	s.saveState(pageData)
	if logged.BatteryLevel != prevLevel {
		level := logged.BatteryLevel
		if level == batteryOK {
			level = "ok"
		}
		slog.Warn("battery warning", "device", d.ID, "level", level, "battery", logged.BatteryVoltage)
	}
	listen := time.Duration(0)
	if input && !textDisplay {
		listen = listenAfterInput
	}
	pageName := served.name
	switch {
	case critical:
		pageName = "battery"
	case pageName == "":
		pageName = "notice"
	}
	attrs := []any{"device", d.ID, "page", pageName}
	if served.pages > 1 {
		attrs = append(attrs, "index", served.page, "pages", served.pages)
	}
	attrs = append(attrs, "rule", logged.Rule, "reason", req.Reason)
	if len(buttons) > 0 {
		attrs = append(attrs, "buttons", strings.Join(buttons, ","))
	}
	attrs = append(attrs, "changed", req.Shown != served.hash, "battery", logged.BatteryVoltage,
		"rssi", logged.RSSI, "refresh", refresh)
	slog.Info("display sync", attrs...)
	resp := syncResponse{
		RefreshS: int(refresh.Seconds()),
		ListenS:  int(listen.Seconds()),
		Time:     now.Unix(),
		Firmware: offer,
	}
	if textDisplay {
		resp.Text = &syncText{Lines: served.lines, Hash: served.hash}
	} else {
		resp.Image = &syncImage{Path: served.path, Hash: served.hash}
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleText answers the text poll of a text display (ADR 0044, 0045): the
// lines of the page the cursor points to, without advancing it or writing the
// state file.
func (s *Server) handleText(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	s.mu.Lock()
	d, st := s.deviceByKeyLocked(r)
	if d == nil {
		s.mu.Unlock()
		http.Error(w, "unknown key", http.StatusUnauthorized)
		return
	}
	if !s.cfg.Profiles[d.Profile].text() {
		s.mu.Unlock()
		http.Error(w, "not a text display", http.StatusConflict)
		return
	}
	// Only kept in memory; the next sync persists it.
	st.LastSeen = now
	critical := st.BatteryLevel == batteryCritical
	voltage := st.BatteryVoltage
	var choice planChoice
	switch {
	case critical:
	case d.Plan != "":
		// A copy keeps the cursor: a rule change or page timeout only shows here
		// and is stored by the next sync.
		peek := *st
		choice = s.choosePlanPage(d, &peek, now, true, nil, time.Duration(s.cfg.PageTimeout), true)
	default:
		choice.title, choice.text = s.cfg.texts().noPlan()
	}
	s.mu.Unlock()

	var served servedPage
	switch {
	case critical:
		title, text := s.cfg.texts().criticalBattery(voltageKey(voltage))
		served = s.hint(d.Profile, title, text)
	case d.Plan != "":
		served = s.renderPlan(d, choice, now, true)
	default:
		served = s.hint(d.Profile, choice.title, choice.text)
	}
	// The dashboard and /api/devices show what the display shows now. Kept in
	// memory only; the next sync writes it to state/devices.json.
	s.mu.Lock()
	if st := s.state[d.key()]; st != nil && st.LastPageHash != served.hash {
		st.LastText, st.LastPageHash, st.LastPageTime = served.lines, served.hash, now
		st.LastPage, st.LastPageKind = served.name, "page"
		switch {
		case critical:
			st.LastPage, st.LastPageKind = "", "battery"
		case served.name == "":
			st.LastPageKind = "notice"
		}
		st.LastError = ""
		if choice.err != nil {
			st.LastError = shortError(choice.err)
		} else if served.err != nil {
			st.LastError = shortError(served.err)
		}
	}
	s.mu.Unlock()
	// ESPHome logs every non-2xx answer as a failed request, so unchanged text
	// is a 204 for the hash the device names in ?shown= (ADR 0045).
	if shown := r.URL.Query().Get("shown"); shown != "" && shown == served.hash {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, syncText{Lines: served.lines, Hash: served.hash})
}
