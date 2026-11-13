package paperboy

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/maehld/paperboy/internal/plan"
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

// hasButton reports whether one of the button roles is in the list.
func hasButton(buttons []string, role string) bool {
	for _, b := range buttons {
		if b == role {
			return true
		}
	}
	return false
}

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
	RSSI     *int             `json:"rssi"`
	Log      []deviceLogEntry `json:"log"`
	Power    string           `json:"power"` // battery or mains, if the hardware knows (ADR 0051)
	Touch    *bool            `json:"touch"` // the hardware has a digitiser (ADR 0082)
}

type syncImage struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
}

// syncText is the content of a text display (ADR 0044).
type syncText struct {
	Lines  []string `json:"lines"`
	Scroll []string `json:"scroll,omitempty"` // full text of scrolling rows, "" for a static row (ADR 0092)
	Hash   string   `json:"hash"`
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
	Rotation int            `json:"rotation,omitempty"`
	Firmware *firmwareOffer `json:"firmware"` // update offer (ADR 0043); null when none
	// Wake names the reason of the next wake and how long to sleep until then
	// (ADR 0064). It overrides refresh_s for exactly one sleep, so the menu can
	// close at its deadline and a slow action can report its result.
	Wake *syncWake `json:"wake,omitempty"`
}

// syncWake is a one-shot wake instruction.
type syncWake struct {
	Reason string `json:"reason"`  // timer, menu or result
	AfterS int    `json:"after_s"` // seconds until that wake
}

// validate checks the parts of a sync that change device state; anything the
// firmware gets wrong is answered with 400 instead of being guessed.
func (r *syncRequest) validate() error {
	switch r.Reason {
	case "timer", "button", "touch", "powercycle", "menu", "result":
	default:
		return fmt.Errorf("reason %q: expected timer, button, touch, powercycle, menu or result", r.Reason)
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
	d := s.config().device(req.MAC)
	s.mu.Lock()
	st := s.stateLocked(key)
	if st.FirstSeen.IsZero() {
		st.FirstSeen = s.now()
	}
	st.Hardware, st.Firmware = req.Hardware, req.Firmware
	st.ClientIP = clientIP(r)
	if req.Power != "" {
		st.ReportedPower = req.Power
	}
	if d == nil {
		st.LastSeen = s.now()
		stateData := s.stateSnapshotLocked(st)
		s.mu.Unlock()
		s.saveState(key, stateData)
		slog.Warn("registration from unknown device; claim it in the device editor or add its MAC to a device file",
			"mac", formatStateKey(key), "hardware", req.Hardware, "firmware", req.Firmware)
		http.Error(w, "device not configured", http.StatusForbidden)
		return
	}
	if st.APIKey == "" {
		st.APIKey = newAPIKey()
	}
	if req.Touch != nil {
		st.TouchReported = *req.Touch
	}
	apiKey := st.APIKey
	stateData := s.stateSnapshotLocked(st)
	s.mu.Unlock()
	s.saveState(key, stateData)
	slog.Info("display registered", "device", d.ID, "mac", d.MAC, "hardware", req.Hardware, "firmware", req.Firmware)
	writeJSON(w, http.StatusOK, map[string]string{"key": apiKey, "device": d.ID})
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
	cfg := s.config()
	// The firmware directory is read before taking the state lock.
	var img *firmwareImage
	if req.Hardware != "" {
		img, _ = s.firmware.latest(req.Hardware, cfg.Firmware.Version)
	}
	s.mu.Lock()
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
	resp := s.serveSync(cfg, d, st, &req, now, syncOptions{
		firmware:    img,
		pageTimeout: time.Duration(cfg.PageTimeout),
		clientIP:    clientIP(r),
	})
	writeJSON(w, http.StatusOK, resp)
}

// syncOptions is what a wake needs beyond the request itself: the firmware
// image a device may be offered and the page timeout in force. virtual marks a
// display without hardware (ADR 0091): it gets no firmware offer, does not run
// its actions and writes no state file.
type syncOptions struct {
	firmware    *firmwareImage
	pageTimeout time.Duration
	virtual     bool
	clientIP    string // source address of the contact, empty for a virtual display
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

// serveSync applies one wake to the state of a device and answers what to show
// and when to come back (ADR 0042). The caller has resolved the device and
// holds no lock; page selection happens under the state lock, rendering and
// plugin calls do not.
func (s *Server) serveSync(cfg *Config, d *Device, st *deviceState, req *syncRequest, now time.Time, opt syncOptions) syncResponse {
	s.mu.Lock()
	// The journal gets one line when the power source or the charger state
	// changes instead of repeating it on every sync (issue #200).
	prevPower, prevCharger := st.ReportedPower, chargerState(*st)
	firstContact := st.LastSeen.IsZero()
	st.LastSeen = now
	st.ClientIP = opt.clientIP
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
	if req.Power != "" {
		st.ReportedPower = req.Power
	}
	if b := req.Battery; b != nil {
		st.BatteryVoltage = strconv.FormatFloat(b.Volts, 'f', 2, 64)
		st.BatteryPercent, st.BatteryMAH, st.BatteryMA = b.Percent, b.MAH, b.MA
		st.BatteryCharging = b.Charging
		trackBattery(st, b, now)
	}
	prevLevel := st.BatteryLevel
	st.BatteryLevel = nextBatteryLevel(cfg.battery(d), prevLevel, st.BatteryVoltage)
	critical := st.BatteryLevel == batteryCritical
	pageTimeout := opt.pageTimeout
	var offer *firmwareOffer
	if !opt.virtual {
		s.settleFirmwareLocked(d, st)
		var volts float64
		if req.Battery != nil {
			volts = req.Battery.Volts
		}
		offer = s.offerFirmwareLocked(d, st, cfg, opt.firmware, volts, req.Battery != nil, now)
	}

	var buttons []string
	var holds []int
	var touches []syncEvent
	input := req.Reason == "button" || req.Reason == "touch" || req.Reason == "menu" || req.Reason == "result"
	if req.Reason == "powercycle" {
		// A power cycle clears the RTC event counter, so the IDs start again
		// (ADR 0064).
		st.LastEventID = 0
		st.Menu.close()
	}
	for _, e := range req.Events {
		// A retried sync resends the same event IDs; anything at or below the
		// last applied ID is a repeat (ADR 0064).
		if e.ID > 0 {
			if e.ID <= st.LastEventID {
				continue
			}
			st.LastEventID = e.ID
		}
		input = true
		switch e.Type {
		case "button":
			buttons = append(buttons, e.Button)
			holds = append(holds, e.HoldMS)
		case "touch":
			touches = append(touches, e)
		}
	}

	power := cfg.power(d, st.ReportedPower)
	floor := cfg.minRefresh(d, st.ReportedPower)
	textDisplay := cfg.Profiles[d.Profile].text()
	// The device menu (ADR 0057) consumes the buttons before the plan sees
	// them, so a press never both moves the selection and turns the page.
	menu := st.Menu
	var invoke *menuAction
	menuConsumed := false
	// Whether the menu draws its tap-only controls for this device (ADR 0082).
	menuTouch := cfg.touch(d, st.TouchReported)
	// A menu that closes without user input (the wake at its deadline, a
	// replaced result screen) still paused the page while it was open, so that
	// sync must not turn it either (ADR 0057).
	menuClosed := false
	switch {
	case req.Reason == "powercycle", critical, textDisplay, d.Actions == actionsDeny, d.Plan == "":
		menu.close()
	default:
		// The deadline is a server-side timer, but the panel keeps the menu
		// screen until the deadline wake repaints it. A press that arrives while
		// the device still reports that screen belongs to the menu, even when
		// the deadline has passed: closing it would swallow the press and the
		// next one would turn the page (ADR 0078).
		onScreen := menu.Screen != "" && req.Shown == menu.Screen
		if menu.Open && !now.Before(menu.expiresAt(d)) && !(onScreen && (len(buttons) > 0 || len(touches) > 0)) {
			slog.Info("menu expired", "device", d.ID)
			menu.close()
			menuClosed = true
		}
		// A result stays until the next input or the next regular sync; the
		// result wake itself is the one that brings it, so it must not close
		// the screen before it is drawn.
		if menu.Open && menu.Phase == menuResultPhase && len(buttons) == 0 &&
			req.Reason != "button" && req.Reason != "touch" && req.Reason != "result" {
			menu.close()
			menuClosed = true
		}
		var acts []plan.PlanAction
		if p, _ := s.plans.get(cfg.Plans, d.Plan, cfg.Timezone); p != nil {
			if sel, ok := p.Select(now); ok {
				acts = sel.Actions
			}
		}
		// Buttons of a wake that finds the menu open belong to the menu, even
		// when they close it: dismissing a banner must not turn the page
		// (ADR 0067).
		menuConsumed = menu.Open
		invoke = menu.step(menuInput{buttons: buttons, holds: holds, shown: req.Shown, touch: menuTouch}, acts, now)
		if invoke == nil && menu.Open {
			for _, e := range touches {
				if a := menu.tap(e.X, e.Y, cfg.Profiles[d.Profile], menuTouch, req.Shown, now); a != nil {
					invoke = a
				}
			}
		}
	}
	menuOpen := menu.Open
	// A sync the menu took part in never pages: it is modal, and its own
	// buttons belong to it even when they close it (ADR 0067).
	menuSync := menuOpen || menuConsumed || menuClosed
	var pageButtons []string
	switch {
	case menuSync:
		// The menu consumed the input, so it never pages.
	case d.Actions != actionsDeny:
		// A long next/previous jumps to the first page of the rule; a long
		// home belongs to the open menu and is dropped (ADR 0067).
		pageButtons = pageRoles(buttons, holds)
	default:
		// A denied device keeps the old meaning of every button.
		pageButtons = buttons
	}
	if d.Actions == actionsDeny && hasButton(buttons, buttonHome) {
		slog.Info("device menu denied; home keeps paging", "device", d.ID)
	}
	st.Menu = menu
	var choice planChoice
	switch {
	case critical: // the warning replaces all pages; buttons only show it again
		st.Page = 0
	case d.Plan != "":
		choice = s.choosePlanPage(cfg, d, st, now, firstContact, pageButtons, pageTimeout, menuSync, false)
	default:
		choice.title, choice.text = cfg.texts().noPlan()
	}
	logged := *st
	powerChanged := prevPower != logged.ReportedPower || prevCharger != chargerState(logged)
	var stateData []byte
	if !opt.virtual {
		stateData = s.stateSnapshotLocked(st)
	}
	s.mu.Unlock()
	s.saveState(d.stateKey(), stateData)
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
	// or a panicking template must never block the other devices.
	if invoke != nil {
		if opt.virtual {
			// A virtual display stops before the plugin call (ADR 0091): the
			// banner names the action instead of claiming a result.
			title := invoke.Title
			if title == "" {
				title = invoke.Action
			}
			menu.Phase, menu.Result, menu.ResultOK, menu.Simulated, menu.ConfirmHash =
				menuResultPhase, title, true, true, ""
			st.Menu = menu
			slog.Info("virtual display: action not run", "device", d.ID, "action", invoke.Action)
		} else {
			run := s.startAction(cfg, d, *invoke, now)
			select {
			case <-run.done:
			case <-time.After(actionBudget):
				slog.Info("action still running", "device", d.ID, "action", invoke.Action)
			}
			s.mu.Lock()
			if st := s.state[d.stateKey()]; st != nil {
				menu = st.Menu
			}
			s.mu.Unlock()
		}
	}
	var served servedPage
	switch {
	case critical:
		if p := cfg.Profiles[d.Profile]; p.text() {
			title, text := cfg.texts().criticalBattery(voltageKey(logged.BatteryVoltage))
			served = s.hint(cfg, d.Profile, title, text)
		} else {
			served = s.notice(d.Profile, criticalBatteryImage(p, cfg.texts(), voltageKey(logged.BatteryVoltage)))
		}
		served.refresh = d.refresh()
	case d.Plan != "":
		served = s.renderPlan(cfg, d, choice, now, false)
	default:
		served = s.hint(cfg, d.Profile, choice.title, choice.text)
	}
	if menu.Open && !critical && !textDisplay {
		served = s.overlayMenu(cfg, d, served, &menu, menuTouch)
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
	if !opt.virtual {
		// A background action may have finished and replaced the menu while
		// the page was rendered; a virtual display has no such path.
		st = s.state[d.stateKey()]
	}
	if st != nil {
		st.LastPage, st.LastPageKind, st.LastPageHash = shownPage, shownKind, served.hash
		st.LastPages = served.pages
		st.LastPageTime, st.LastRefresh = now, int(refresh.Seconds())
		st.LastText = served.lines
		st.LastError = errText
		st.Menu = menu
		if !opt.virtual {
			pageData = s.stateSnapshotLocked(st)
		}
	}
	s.mu.Unlock()
	s.saveState(d.stateKey(), pageData)
	if powerChanged {
		attrs := []any{"device", d.ID, "power", power}
		if charger := chargerState(logged); charger != "" {
			attrs = append(attrs, "charger", charger)
		}
		if logged.BatteryVoltage != "" {
			attrs = append(attrs, "battery", logged.BatteryVoltage)
		}
		if logged.BatteryMA != nil {
			attrs = append(attrs, "battery_ma", fmt.Sprintf("%+.0f", *logged.BatteryMA))
		}
		slog.Info("power changed", attrs...)
	}
	if logged.BatteryLevel != prevLevel {
		level := logged.BatteryLevel
		if level == batteryOK {
			level = "ok"
		}
		slog.Warn("battery warning", "device", d.ID, "level", level, "battery", logged.BatteryVoltage)
	}
	listen := time.Duration(0)
	if input && !textDisplay && !opt.virtual {
		listen = listenAfterInput
	}
	wake := menuWake(menu, d, now, critical)
	// The firmware sleeps only after listening, and the one-shot wake counts
	// from the sync: give the wake the time it needs, so the menu expires when
	// the panel is repainted instead of listen_s later (ADR 0078).
	listen, wake = fitWakeToListen(listen, wake)
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
	if menu.Open {
		attrs = append(attrs, "menu", menu.Phase, "action", menu.Selected)
	}
	attrs = append(attrs, "changed", req.Shown != served.hash, "battery", logged.BatteryVoltage,
		"rssi", logged.RSSI, "refresh", refresh, "power", power)
	// The fuel gauge current shows whether the battery is charging (positive)
	// or discharging (negative); only devices with a gauge send it.
	if logged.BatteryMA != nil {
		attrs = append(attrs, "battery_ma", fmt.Sprintf("%+.0f", *logged.BatteryMA))
	}
	// The charger's own state (the Sticky's STAT pin) next to the gauge
	// current, so the log shows when the two disagree.
	if logged.BatteryCharging != nil {
		charger := "idle"
		if *logged.BatteryCharging {
			charger = "charging"
		}
		attrs = append(attrs, "charger", charger)
	}
	slog.Info("display sync", attrs...)
	resp := syncResponse{
		RefreshS: int(refresh.Seconds()),
		ListenS:  int(listen.Seconds()),
		Sleep:    power == powerBattery,
		Time:     now.Unix(),
		Rotation: cfg.rotation(d),
		Firmware: offer,
		Wake:     wake,
	}
	if textDisplay {
		resp.Text = &syncText{Lines: served.lines, Scroll: scrollRows(served.scroll), Hash: served.hash}
	} else {
		resp.Image = &syncImage{Path: served.path, Hash: served.hash}
	}
	return resp
}

// menuWake names the one-shot wake the device needs (ADR 0064): the menu
// deadline while it is open, or a short wake for the result of a running
// action. Without an open menu the regular refresh applies.
func menuWake(menu menuState, d *Device, now time.Time, critical bool) *syncWake {
	if !menu.Open || critical {
		return nil
	}
	switch menu.Phase {
	case menuRunningPhase:
		return &syncWake{Reason: "result", AfterS: int(actionBudget.Seconds())}
	case menuResultPhase:
		return nil // the next regular sync closes the result
	}
	left := menu.expiresAt(d).Sub(now)
	if left < time.Second {
		left = time.Second
	}
	return &syncWake{Reason: "menu", AfterS: int(left.Seconds())}
}

// fitWakeToListen shortens a listen window that would swallow a one-shot wake:
// the firmware listens before it sleeps, so a wake after after_s seconds would
// arrive listen_s too late. The time it moves out of the window goes into the
// wake, which then fires when it was meant to (ADR 0078).
func fitWakeToListen(listen time.Duration, wake *syncWake) (time.Duration, *syncWake) {
	if wake == nil || wake.AfterS < 1 {
		return listen, wake
	}
	room := int(listen.Seconds())
	if room > wake.AfterS-1 {
		room = wake.AfterS - 1
	}
	if room < 0 {
		room = 0
	}
	wake.AfterS -= room
	return time.Duration(room) * time.Second, wake
}

// handleText answers the text poll of a text display (ADR 0044, 0045): the
// lines of the page the cursor points to, without advancing it or writing the
// state file.
func (s *Server) handleText(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	cfg := s.config()
	s.mu.Lock()
	d, st := s.deviceByKeyLocked(r, cfg)
	if d == nil {
		s.mu.Unlock()
		http.Error(w, "unknown key", http.StatusUnauthorized)
		return
	}
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
	// Only kept in memory; the next sync persists it.
	st.LastSeen = now
	st.ClientIP = clientIP(r)
	critical := st.BatteryLevel == batteryCritical
	voltage := st.BatteryVoltage
	var choice planChoice
	switch {
	case critical:
	case d.Plan != "":
		// A copy keeps the cursor: a rule change or page timeout only shows here
		// and is stored by the next sync.
		peek := *st
		choice = s.choosePlanPage(cfg, d, &peek, now, true, nil, time.Duration(cfg.PageTimeout), false, true)
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
	s.mu.Lock()
	if st := s.state[d.stateKey()]; st != nil && st.LastPageHash != served.hash {
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
	writeJSON(w, http.StatusOK, syncText{Lines: served.lines, Scroll: scrollRows(served.scroll), Hash: served.hash})
}
