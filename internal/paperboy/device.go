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

type syncResponse struct {
	Image    syncImage      `json:"image"`
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
	var choice planChoice
	switch {
	case critical: // the warning replaces all pages; buttons only show it again
		st.Page = 0
	case d.Plan != "":
		choice = s.choosePlanPage(d, st, now, firstContact, buttons, pageTimeout)
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
		served = s.notice(d.Profile, criticalBatteryImage(s.cfg.Profiles[d.Profile], s.cfg.texts(), voltageKey(logged.BatteryVoltage)))
		served.refresh = d.refresh()
	case d.Plan != "":
		served = s.renderPlan(d, choice, now)
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
	if input {
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
	writeJSON(w, http.StatusOK, syncResponse{
		Image:    syncImage{Path: served.path, Hash: served.hash},
		RefreshS: int(refresh.Seconds()),
		ListenS:  int(listen.Seconds()),
		Time:     now.Unix(),
		Firmware: offer,
	})
}
