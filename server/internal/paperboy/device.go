package paperboy

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/maehld/paperboy/server/internal/plan"
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
	Power    string `json:"power"` // battery or mains, if the hardware knows (ADR 0051)
	Touch    *bool  `json:"touch"` // the hardware has a digitiser (ADR 0082)
}

type syncEvent struct {
	ID     int64  `json:"id"`     // monotonically increasing per device (ADR 0064); 0 = no deduplication
	Type   string `json:"type"`   // "button" or "touch"
	Button string `json:"button"` // button role
	HoldMS int    `json:"hold_ms"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
}

type syncBattery struct {
	Volts    float64  `json:"volts"`
	Percent  *float64 `json:"percent"`
	MAH      *float64 `json:"mah"`
	MA       *float64 `json:"ma"`
	Charging *bool    `json:"charging"` // charger's own state, e.g. the Sticky's STAT pin
}

type syncRequest struct {
	Firmware string           `json:"firmware"`
	Hardware string           `json:"hardware"`
	Reason   string           `json:"reason"`
	Events   []syncEvent      `json:"events"`
	Shown    string           `json:"shown"`
	Battery  *syncBattery     `json:"battery"`
	Readings []syncReading    `json:"readings"` // measured values of this wake (ADR 0134)
	RSSI     *int             `json:"rssi"`
	Log      []deviceLogEntry `json:"log"`
	Power    string           `json:"power"` // battery or mains, if the hardware knows (ADR 0051)
	Touch    *bool            `json:"touch"` // the hardware has a digitiser (ADR 0082)
}

// syncReading is one measured value of this wake. The name is the key of the
// reading catalog; the firmware sends a bare number in the catalog's unit.
type syncReading struct {
	Name  string   `json:"name"`
	Value *float64 `json:"value"` // absent or null when the measurement failed
}

type syncImage struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
}

// syncText is the content of a text display (ADR 0044).
type syncText struct {
	Lines  []string `json:"lines"`
	Scroll []string `json:"scroll,omitempty"` // legacy single-row marquee (ADR 0092)
	// Scrolls describe the scrolling text boxes with their full text, window,
	// direction and step (ADR 0118).
	Scrolls       []syncScroll `json:"scrolls,omitempty"`
	ScrollChanged bool         `json:"scroll_changed,omitempty"`
	Hash          string       `json:"hash"`
}

// syncScroll is one scrolling text box of a text page (ADR 0118).
type syncScroll struct {
	Row       int      `json:"row"`
	Col       int      `json:"col"`
	Width     int      `json:"width"`
	Height    int      `json:"height"`
	Text      []string `json:"text"`   // full content, not cut to the box
	Window    []string `json:"window"` // the first visible lines
	StepMS    int      `json:"step_ms"`
	Direction string   `json:"direction"`       // horizontal, vertical or both
	Align     string   `json:"align,omitempty"` // left, right or decimal
}

// textResponse builds the text part of a sync or poll answer. previous is the
// scroll hash of the last answer; the new hash is returned for the caller to
// store under the state lock. The function does not touch device state.
func textResponse(served servedPage, p Profile, previous string) (*syncText, string) {
	step := p.ScrollStep
	if step <= 0 {
		step = DefaultScrollStep
	}
	out := &syncText{Lines: served.lines, Scroll: legacyScroll(served.scrolls, p.Height), Hash: served.hash}
	hash := ""
	for _, box := range served.scrolls {
		out.Scrolls = append(out.Scrolls, syncScroll{
			Row: box.At[1], Col: box.At[0], Width: box.Span[0], Height: box.Span[1],
			Text: box.Lines, Window: box.Window, StepMS: int(step / time.Millisecond), Direction: box.Direction,
			Align: box.Align,
		})
	}
	if len(served.scrolls) > 0 {
		raw, _ := json.Marshal(served.scrolls)
		hash = hashBytes(raw)
	}
	out.ScrollChanged = hash != previous
	return out, hash
}

type syncResponse struct {
	Image    *syncImage `json:"image"`          // null for text displays
	Text     *syncText  `json:"text,omitempty"` // text displays only
	RefreshS int        `json:"refresh_s"`
	ListenS  int        `json:"listen_s"`
	Sleep    bool       `json:"sleep"` // false: stay awake and sync again after refresh_s (ADR 0051)
	Time     int64      `json:"time"`
	// Rotation is the quarter turn the firmware applies to composed pages
	// before painting, in degrees clockwise relative to its built-in mounting
	// (ADR 0102). 0 leaves it as built; old firmware ignores the field.
	Rotation int `json:"rotation,omitempty"`
	// Beep and Blink ask the device to signal after the paint (ADR 0131).
	// The device runs the method it has; a missing field is false.
	Beep     bool           `json:"beep,omitempty"`
	Blink    bool           `json:"blink,omitempty"`
	Firmware *firmwareOffer `json:"firmware"` // update offer (ADR 0043); null when none
	// Wake names the reason of the next wake and how long to sleep until then
	// (ADR 0064). It overrides refresh_s for exactly one sleep. Nothing sets it
	// until widget actions need a deadline or a result (issue #432).
	Wake *syncWake `json:"wake,omitempty"`
}

// syncWake is a one-shot wake instruction.
type syncWake struct {
	Reason string `json:"reason"`  // timer, menu, result or action
	AfterS int    `json:"after_s"` // seconds until that wake
}

// validate checks the parts of a sync that change device state; anything the
// firmware gets wrong is answered with 400 instead of being guessed.
func (r *syncRequest) validate() error {
	switch r.Reason {
	case "timer", "button", "touch", "powercycle", "menu", "result", "action":
	default:
		return fmt.Errorf("reason %q: expected timer, button, touch, powercycle, menu, result or action", r.Reason)
	}
	for i, e := range r.Events {
		if e.ID < 0 {
			return fmt.Errorf("event %d: id must not be negative", i)
		}
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
	return validReportedPower(r.Power)
}

// validReportedPower checks the power source a device reports; it may leave
// it out, but never report auto or anything else.
func validReportedPower(p string) error {
	switch p {
	case "", powerBattery, powerMains:
		return nil
	}
	return fmt.Errorf("power %q: expected battery or mains", p)
}

// clientIP is the source address of a request, the address the device reaches
// Paperboy from. It comes from the connection, never from a header: the device
// API is reached directly, and a header would be content the caller chooses
// (ADR 0083). A reverse proxy shows its own address instead.
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return strings.Trim(r.RemoteAddr, "[]")
}

// maxUnclaimed bounds the hardware that registered without a device file
// (ADR 0137). Registration takes no key, so any LAN client can create an entry;
// the operator claims or forgets hardware in the devices tab.
const (
	maxUnclaimed    = 10
	unknownNoteTTL  = time.Hour
	unknownNotesMax = 256
)

// unclaimedCountLocked counts the state entries no device file uses. Callers
// hold s.mu.
func (s *Server) unclaimedCountLocked(cfg *Config) int {
	n := 0
	for key, st := range s.state {
		if !st.FirstSeen.IsZero() && cfg.device(formatStateKey(key)) == nil {
			n++
		}
	}
	return n
}

// noteUnknownLocked reports whether the journal should log this MAC now: once
// per TTL, and never beyond a bounded number of remembered MACs. Callers hold
// s.mu.
func (s *Server) noteUnknownLocked(key string, now time.Time) bool {
	if s.unknownNotes == nil {
		s.unknownNotes = map[string]time.Time{}
	}
	if t, ok := s.unknownNotes[key]; ok && now.Sub(t) < unknownNoteTTL {
		return false
	}
	if len(s.unknownNotes) >= unknownNotesMax {
		for k, t := range s.unknownNotes {
			if now.Sub(t) >= unknownNoteTTL {
				delete(s.unknownNotes, k)
			}
		}
		if len(s.unknownNotes) >= unknownNotesMax {
			return false
		}
	}
	s.unknownNotes[key] = now
	return true
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
// device keeps its key, so a reflashed display picks up its old state. An
// unknown MAC gets a state file without a key, which the devices tab shows as
// unclaimed (ADR 0080).
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !decodeDeviceJSON(w, r, &req) {
		return
	}
	key, err := deviceStateKey(req.MAC)
	if err != nil {
		http.Error(w, "invalid MAC address", http.StatusBadRequest)
		return
	}
	if err := validReportedPower(req.Power); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cfg := s.config()
	if cfg.degraded {
		// A broken paperboy.yaml must not look like a forgotten device: a 401
		// would make the firmware drop its key, and a 403 would start a
		// pairing. The display keeps its key and retries (ADR 0047, 0079).
		http.Error(w, "configuration unavailable", http.StatusServiceUnavailable)
		return
	}
	device := cfg.device(req.MAC)
	now := s.now()
	s.mu.Lock()
	st := s.state[key]
	if device == nil && (st == nil || st.FirstSeen.IsZero()) && s.unclaimedCountLocked(cfg) >= maxUnclaimed {
		// The state map and state/devices/ stay bounded: any LAN client can
		// register a MAC without a key, so unclaimed hardware is capped
		// (ADR 0137). The log names a MAC once, not once per attempt.
		logged := s.noteUnknownLocked(key, now)
		s.mu.Unlock()
		if logged {
			slog.Warn("registration from unknown device refused: the unclaimed list is full",
				"mac", formatStateKey(key), "limit", maxUnclaimed, "hardware", req.Hardware, "firmware", req.Firmware)
		}
		http.Error(w, "the unclaimed hardware list is full; claim or forget a device in the devices tab first", http.StatusForbidden)
		return
	}
	if st == nil {
		st = s.stateLocked(key)
	}
	if st.FirstSeen.IsZero() {
		st.FirstSeen = now
	}
	st.Hardware, st.Firmware = req.Hardware, req.Firmware
	st.ClientIP = clientIP(r)
	if req.Power != "" {
		st.ReportedPower = req.Power
	}
	if device == nil {
		st.LastSeen = now
		stateData := s.stateSnapshotLocked(st)
		logged := s.noteUnknownLocked(key, now)
		s.mu.Unlock()
		s.saveState(key, stateData)
		if logged {
			slog.Warn("registration from unknown device; claim it in the device editor or add its MAC to a device file",
				"mac", formatStateKey(key), "hardware", req.Hardware, "firmware", req.Firmware)
		}
		http.Error(w, "device not configured", http.StatusForbidden)
		return
	}
	// The operator has to confirm this display before it takes a key; until
	// then it shows a code (ADR 0109).
	if code, challenge, fresh := s.pairChallengeLocked(st, now); challenge {
		stateData := s.stateSnapshotLocked(st)
		s.mu.Unlock()
		s.saveState(key, stateData)
		if fresh {
			// The display repeats the register every 15 seconds while its setup
			// screen is up (ADR 0109); one line per code is enough.
			slog.Info("registration waits for pairing", "device", device.ID, "mac", device.MAC)
		}
		writeJSON(w, http.StatusForbidden, pairChallenge{
			Error:     "pairing required: enter the code the display shows in the device editor",
			Code:      code,
			ExpiresIn: int(pairTTL.Seconds()),
		})
		return
	}
	if st.APIKey == "" {
		key, err := newAPIKey()
		if err != nil {
			s.mu.Unlock()
			slog.Error("cannot create a device key", "device", device.ID, "error", err)
			writeAPIError(w, http.StatusInternalServerError, "cannot create a device key")
			return
		}
		st.APIKey = key
	}
	if st.PairAuthorized {
		usePairingLocked(st, now)
	}
	if req.Touch != nil {
		st.TouchReported = *req.Touch
	}
	apiKey := st.APIKey
	stateData := s.stateSnapshotLocked(st)
	s.mu.Unlock()
	s.saveState(key, stateData)
	slog.Info("display registered", "device", device.ID, "mac", device.MAC, "hardware", req.Hardware, "firmware", req.Firmware)
	writeJSON(w, http.StatusOK, registerAnswer{Key: apiKey, Device: device.ID})
}

// deviceByKeyLocked finds the device that owns the bearer key. cfg is the
// configuration snapshot the caller works with; callers hold s.mu.
func (s *Server) deviceByKeyLocked(r *http.Request, cfg *Config) (*Device, *deviceState) {
	key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || key == "" {
		return nil, nil
	}
	for i := range cfg.Devices {
		d := &cfg.Devices[i]
		st := s.state[d.stateKey()]
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
	configDone := s.stats.Start("sync.config")
	cfg := s.config()
	configDone()
	if cfg.degraded {
		// The display keeps its key while the configuration is unusable; a
		// 401 would drop it and force a new pairing (ADR 0047, 0079).
		http.Error(w, "configuration unavailable", http.StatusServiceUnavailable)
		return
	}
	// The firmware directory is read before taking the state lock.
	var img *firmwareImage
	if req.Hardware != "" {
		img, _ = s.firmware.latest(req.Hardware, cfg.Firmware.Version)
	}
	s.lock("sync.lock")
	d, st := s.deviceByKeyLocked(r, cfg)
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
	if d.Problem != "" {
		// The device file points at a display type that is not configured, e.g.
		// after a rename. Never a 401: that would make the firmware forget its
		// key, and the file is the problem (ADR 0079).
		st.LastSeen = now
		st.ClientIP = clientIP(r)
		stateData := s.stateSnapshotLocked(st)
		s.mu.Unlock()
		s.saveState(d.stateKey(), stateData)
		slog.Warn("device cannot sync: its device file needs attention", "device", d.ID, "problem", d.Problem)
		http.Error(w, d.Problem, http.StatusServiceUnavailable)
		return
	}
	s.mu.Unlock()
	valuesDone := s.stats.Start("sync.values")
	values := s.planValues(r.Context(), cfg, d.Plan, d.Profile, d)
	valuesDone()
	resp := s.serveSync(cfg, d, st, &req, now, syncOptions{
		firmware: img,
		clientIP: clientIP(r),
		values:   values,
	})
	writeJSON(w, http.StatusOK, resp)
}

// registerAnswer gives a display its key and its name in the instance.
type registerAnswer struct {
	Key    string `json:"key"`
	Device string `json:"device"`
}

// syncOptions is what a wake needs beyond the request itself: the firmware
// image a device may be offered. virtual marks a display without hardware
// (ADR 0091): it gets no firmware offer, does not run its actions and writes
// no state file. values is the plan's evaluated value state, asked before the
// state lock (ADR 0127).
type syncOptions struct {
	firmware *firmwareImage
	virtual  bool
	clientIP string // source address of the contact, empty for a virtual display
	values   plan.Values
}

// chargerState names what the charger does for the journal: the hardware's own
// state when it reports one (the Sticky reads the BQ25616's STAT pin), else the
// direction of the fuel gauge current. An idle gauge and a missing reading both
// answer "".
func chargerState(st deviceState) string {
	if st.BatteryCharging != nil {
		if *st.BatteryCharging {
			return "charging"
		}
		return "idle"
	}
	switch {
	case st.BatteryMA != nil && *st.BatteryMA > idleMA:
		return "charging"
	case st.BatteryMA != nil && *st.BatteryMA < -idleMA:
		return "discharging"
	}
	return ""
}

// hardwareOf reports the firmware target of a device for plugin requests.
func (s *Server) hardwareOf(d *Device) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.state[d.stateKey()]; st != nil {
		return st.Hardware
	}
	return ""
}

// handleText answers the text poll of a text display (ADR 0044, 0045): the
// lines of the page the cursor points to, without advancing it or writing the
// state file.
func (s *Server) handleText(w http.ResponseWriter, r *http.Request) {
	defer s.stats.Start("poll.total")()
	now := s.now()
	cfg := s.config()
	if cfg.degraded {
		// The text display keeps its key while the configuration is unusable
		// (ADR 0047, 0079).
		http.Error(w, "configuration unavailable", http.StatusServiceUnavailable)
		return
	}
	s.lock("poll.lock")
	d, st := s.deviceByKeyLocked(r, cfg)
	if d == nil {
		s.mu.Unlock()
		http.Error(w, "unknown key", http.StatusUnauthorized)
		return
	}
	defer s.stats.Start("poll.device:" + d.ID)()
	if d.Problem != "" {
		s.mu.Unlock()
		http.Error(w, d.Problem, http.StatusServiceUnavailable)
		return
	}
	if !cfg.Profiles[d.Profile].text() {
		s.mu.Unlock()
		http.Error(w, "not a text display", http.StatusConflict)
		return
	}
	// Plan values are asked outside the state lock (ADR 0127). The device is
	// resolved again afterwards, because a sync may have written its state in
	// between; the configuration snapshot stays the same.
	planName, profile := d.Plan, d.Profile
	s.mu.Unlock()
	values := s.planValues(r.Context(), cfg, planName, profile, d)
	resolved := s.resolvePlan(cfg, d, now, values)
	s.lock("poll.lock")
	d, st = s.deviceByKeyLocked(r, cfg)
	if d == nil {
		s.mu.Unlock()
		http.Error(w, "unknown key", http.StatusUnauthorized)
		return
	}
	// Only kept in memory; the next sync persists it.
	st.LastSeen = now
	st.ClientIP = clientIP(r)
	critical := st.BatteryLevel == batteryCritical
	voltage := st.BatteryVoltage
	var choice planChoice
	switch {
	case critical:
	case d.Plan != "":
		// A copy keeps the cursor: a rule change only shows here and is
		// stored by the next sync. The copy also keeps a queued manual alert,
		// because only a sync consumes one (ADR 0142).
		peek := *st
		choice = s.choosePlanPage(resolved, cfg, d, &peek, now, true, false, nil, false, false, true)
	default:
		choice.title, choice.text = cfg.texts().noPlan()
	}
	s.mu.Unlock()

	var served servedPage
	switch {
	case critical:
		title, text := cfg.texts().criticalBattery(voltageKey(voltage))
		served = s.hint(cfg, d.Profile, title, text)
	case d.Plan != "":
		served = s.renderPlan(cfg, d, choice, now, true)
	default:
		served = s.hint(cfg, d.Profile, choice.title, choice.text)
	}
	// The dashboard and /api/devices show what the display shows now. Kept in
	// memory only; the next sync writes the device's state file (ADR 0080).
	s.lock("poll.lock")
	var textPart *syncText
	if st := s.state[d.stateKey()]; st != nil {
		var scrollHash string
		textPart, scrollHash = textResponse(served, cfg.Profiles[d.Profile], st.ScrollHash)
		st.ScrollHash = scrollHash
		if st.LastPageHash != served.hash {
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
	}
	s.mu.Unlock()
	if textPart == nil { // the state vanished between rendering and the poll lock
		textPart, _ = textResponse(served, cfg.Profiles[d.Profile], "")
	}
	// ESPHome logs every non-2xx answer as a failed request, so unchanged text
	// is a 204 for the hash the device names in ?shown= (ADR 0045).
	if shown := r.URL.Query().Get("shown"); shown != "" && shown == served.hash {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, textPart)
}
