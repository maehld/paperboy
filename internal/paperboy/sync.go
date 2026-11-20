package paperboy

import (
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/maehld/paperboy/internal/plan"
	"github.com/maehld/paperboy/internal/reading"
)

// syncSession is one wake of a device. serveSync walks it phase by phase: apply
// changes the state under the lock, persist writes it, runAction lets a menu
// action run, render draws the page, and finish stores what the device shows
// and builds the answer. A field that crosses a phase boundary sits here.
type syncSession struct {
	s    *Server
	cfg  *Config
	d    *Device
	st   *deviceState
	req  *syncRequest
	now  time.Time
	opt  syncOptions
	plan planResolution

	prevPower, prevCharger, prevLevel string
	logged                            deviceState
	powerChanged                      bool
	stateData                         []byte
	critical                          bool
	offer                             *firmwareOffer
	buttons                           []string
	input                             bool
	textDisplay                       bool
	power                             string
	floor                             time.Duration
	menu                              menuState
	menuTouch                         bool
	choice                            planChoice
	alert                             bool
	alertSource                       string
	invoke                            *menuAction

	served               servedPage
	refresh              time.Duration
	shownKind, shownPage string
	errText              string
	textPart             *syncText
	pageData             []byte
}

// serveSync applies one wake to the state of a device and answers what to show
// and when to come back (ADR 0042). The caller has resolved the device and
// holds no lock; page selection happens under the state lock, rendering and
// plugin calls do not.
func (s *Server) serveSync(cfg *Config, d *Device, st *deviceState, req *syncRequest, now time.Time, opt syncOptions) syncResponse {
	defer s.stats.Start("sync.total")()
	if !opt.virtual {
		// Virtual displays have no device file; their syncs would muddy the
		// numbers of the real devices (ADR 0091, 0104).
		defer s.stats.Start("sync.device:" + d.ID)()
	}
	x := &syncSession{s: s, cfg: cfg, d: d, st: st, req: req, now: now, opt: opt}
	// The plan, its rule and the pages that fit are read before the state lock
	// (ADR 0138): a slow page file must not block the other devices.
	x.plan = s.resolvePlan(cfg, d, now, opt.values)
	x.apply()
	x.persist()
	x.runAction()
	x.render()
	return x.finish()
}

// apply folds the request into the state, chooses the page and takes the one
// snapshot the sync persists. It holds the state lock for all of it: the sync
// writes many fields at once, and a half-applied wake would be worse than a
// short wait. Plugin calls do not happen here.
func (x *syncSession) apply() {
	s, cfg, d, st, req, now, opt := x.s, x.cfg, x.d, x.st, x.req, x.now, x.opt
	s.lock("sync.lock")
	defer s.mu.Unlock()
	// The journal gets one line when the power source or the charger state
	// changes instead of repeating it on every sync (issue #200).
	x.prevPower, x.prevCharger = st.ReportedPower, chargerState(*st)
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
	// Readings are the measured values of this wake, e.g. the room's
	// temperature and humidity. A name the catalog does not know, a missing
	// value and an implausible number are discarded, so a sensor fault never
	// costs the device its page (ADR 0134).
	if len(req.Readings) > 0 {
		if st.Readings == nil {
			st.Readings = map[string]deviceReading{}
		}
		for _, r := range req.Readings {
			spec, ok := reading.Lookup(r.Name)
			if !ok {
				slog.Debug("ignoring an unknown reading", "device", d.ID, "reading", r.Name)
				continue
			}
			if r.Value == nil {
				slog.Warn("discarding a reading without a value", "device", d.ID, "reading", r.Name)
				continue
			}
			if !spec.Valid(*r.Value) {
				slog.Warn("discarding an implausible reading", "device", d.ID, "reading", r.Name, "value", *r.Value)
				continue
			}
			st.Readings[r.Name] = deviceReading{Value: *r.Value, At: now}
		}
	}
	x.prevLevel = st.BatteryLevel
	st.BatteryLevel = nextBatteryLevel(cfg.battery(d), x.prevLevel, st.BatteryVoltage)
	x.critical = st.BatteryLevel == batteryCritical
	if !opt.virtual {
		s.settleFirmwareLocked(d, st)
		var volts float64
		if req.Battery != nil {
			volts = req.Battery.Volts
		}
		x.offer = s.offerFirmwareLocked(d, st, cfg, opt.firmware, volts, req.Battery != nil, now)
	}

	var holds []int
	var touches []syncEvent
	x.input = req.Reason == "button" || req.Reason == "touch" || req.Reason == "menu" || req.Reason == "result"
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
		x.input = true
		switch e.Type {
		case "button":
			x.buttons = append(x.buttons, e.Button)
			holds = append(holds, e.HoldMS)
		case "touch":
			touches = append(touches, e)
		}
	}

	x.power = cfg.power(d, st.ReportedPower)
	x.floor = cfg.minRefresh(d, st.ReportedPower)
	x.textDisplay = cfg.Profiles[d.Profile].text()
	// The device menu (ADR 0057) consumes the buttons before the plan sees
	// them, so a press never both moves the selection and turns the page.
	menu := st.Menu
	menuConsumed := false
	// Whether the menu draws its tap-only controls for this device (ADR 0082).
	x.menuTouch = cfg.touch(d, st.TouchReported)
	// A menu that closes without user input (the wake at its deadline, a
	// replaced result screen) still paused the page while it was open, so that
	// sync must not turn it either (ADR 0057).
	menuClosed := false
	switch {
	case req.Reason == "powercycle", x.critical, x.textDisplay, d.Actions == actionsDeny, d.Plan == "":
		menu.close()
	default:
		// The deadline is a server-side timer, but the panel keeps the menu
		// screen until the deadline wake repaints it. A press that arrives while
		// the device still reports that screen belongs to the menu, even when
		// the deadline has passed: closing it would swallow the press and the
		// next one would turn the page (ADR 0078).
		onScreen := menu.Screen != "" && req.Shown == menu.Screen
		if menu.Open && !now.Before(menu.expiresAt(d)) && !(onScreen && (len(x.buttons) > 0 || len(touches) > 0)) {
			slog.Info("menu expired", "device", d.ID)
			menu.close()
			menuClosed = true
		}
		// A result stays until the next input or the next regular sync; the
		// result wake itself is the one that brings it, so it must not close
		// the screen before it is drawn.
		if menu.Open && menu.Phase == menuResultPhase && len(x.buttons) == 0 &&
			req.Reason != "button" && req.Reason != "touch" && req.Reason != "result" {
			menu.close()
			menuClosed = true
		}
		var acts []plan.PlanAction
		if x.plan.selected {
			acts = x.plan.selection.Actions
		}
		// Buttons of a wake that finds the menu open belong to the menu, even
		// when they close it: dismissing a banner must not turn the page
		// (ADR 0067).
		menuConsumed = menu.Open
		x.invoke = menu.step(menuInput{buttons: x.buttons, holds: holds, shown: req.Shown, touch: x.menuTouch}, acts, now)
		if x.invoke == nil && menu.Open {
			for _, e := range touches {
				if a := menu.tap(e.X, e.Y, cfg.Profiles[d.Profile], x.menuTouch, req.Shown, now); a != nil {
					x.invoke = a
				}
			}
		}
	}
	// A sync the menu took part in never pages: it is modal, and its own
	// buttons belong to it even when they close it (ADR 0067).
	menuSync := menu.Open || menuConsumed || menuClosed
	var pageButtons []string
	switch {
	case menuSync:
		// The menu consumed the input, so it never pages.
	case d.Actions != actionsDeny:
		// A long next/previous jumps to the first page of the rule; a long
		// home belongs to the open menu and is dropped (ADR 0067).
		pageButtons = pageRoles(x.buttons, holds)
	default:
		// A denied device keeps the old meaning of every button.
		pageButtons = x.buttons
	}
	if d.Actions == actionsDeny && slices.Contains(x.buttons, buttonHome) {
		slog.Info("device menu denied; home keeps paging", "device", d.ID)
	}
	st.Menu = menu
	x.menu = menu
	switch {
	case x.critical: // the warning replaces all pages; buttons only show it again
		// The cursor starts over when the pages return.
		st.Page, st.PageName = 0, ""
	case d.Plan != "":
		planDone := s.stats.Start("sync.plan")
		x.choice = s.choosePlanPage(x.plan, cfg, d, st, now, firstContact, req.Reason == "timer", pageButtons, menuSync, false)
		planDone()
	default:
		x.choice.title, x.choice.text = cfg.texts().noPlan()
	}
	// The rule or the operator asks for a signal (ADR 0131, 0142). The request
	// is consumed here, before the snapshot, so it is one-shot and stored with
	// the rest of this sync.
	x.alert, x.alertSource = x.choice.alert, "rule"
	if !opt.virtual && takeManualAlert(d, st, now, x.critical) {
		x.alertSource = "manual"
		if x.alert {
			x.alertSource = "rule+manual"
		}
		x.alert = true
	}
	x.logged = *st
	x.powerChanged = x.prevPower != x.logged.ReportedPower || x.prevCharger != chargerState(x.logged)
	if !opt.virtual {
		x.stateData = s.stateSnapshotLocked(st)
	}
}

// persist writes the snapshot and the journal lines of this wake; the state
// lock is already released.
func (x *syncSession) persist() {
	s, d, req := x.s, x.d, x.req
	writeDone := s.stats.Start("sync.state_write")
	s.saveState(d.stateKey(), x.stateData)
	writeDone()
	s.logDevice(d.ID, req.Log)
	for _, e := range req.Events {
		// Touches and long presses carry no meaning yet; they are only recorded.
		if e.Type == "touch" {
			slog.Info("display touch", "device", d.ID, "x", e.X, "y", e.Y)
		} else if e.HoldMS > 0 {
			slog.Info("display button held", "device", d.ID, "button", e.Button, "hold_ms", e.HoldMS)
		}
	}
}

// runAction runs the action the menu selected, or names it on a virtual display
// (ADR 0091). Rendering waits for it, so its result can be drawn.
func (x *syncSession) runAction() {
	s, cfg, d, now, opt := x.s, x.cfg, x.d, x.now, x.opt
	menu := x.menu
	if x.invoke == nil {
		return
	}
	if opt.virtual {
		// A virtual display stops before the plugin call (ADR 0091): the
		// banner names the action instead of claiming a result.
		title := x.invoke.Title
		if title == "" {
			title = x.invoke.Action
		}
		menu.Phase, menu.Result, menu.ResultOK, menu.Simulated, menu.ConfirmHash =
			menuResultPhase, title, true, true, ""
		x.st.Menu = menu
		x.menu = menu
		slog.Info("virtual display: action not run", "device", d.ID, "action", x.invoke.Action)
		return
	}
	run := s.startAction(cfg, d, *x.invoke, now)
	actionDone := s.stats.Start("sync.action")
	select {
	case <-run.done:
	case <-time.After(actionBudget):
		slog.Info("action still running", "device", d.ID, "action", x.invoke.Action)
	}
	actionDone()
	s.lock("sync.lock")
	if st := s.state[d.stateKey()]; st != nil {
		menu = st.Menu
	}
	s.mu.Unlock()
	x.menu = menu
}

// render draws what the device gets: the battery warning, a plan page, the
// notice without a plan, and the menu over it. No lock is held, so a slow page
// or a panicking template never blocks another device.
func (x *syncSession) render() {
	s, cfg, d, now := x.s, x.cfg, x.d, x.now
	menu := x.menu
	switch {
	case x.critical:
		if p := cfg.Profiles[d.Profile]; p.text() {
			title, text := cfg.texts().criticalBattery(voltageKey(x.logged.BatteryVoltage))
			x.served = s.hint(cfg, d.Profile, title, text)
		} else {
			x.served = s.notice(d.Profile, criticalBatteryImage(p, cfg.texts(), voltageKey(x.logged.BatteryVoltage)))
		}
		x.served.refresh = d.refresh()
	case d.Plan != "":
		renderDone := s.stats.Start("sync.render")
		x.served = s.renderPlan(cfg, d, x.choice, now, false)
		renderDone()
	default:
		x.served = s.hint(cfg, d.Profile, x.choice.title, x.choice.text)
	}
	if menu.Open && !x.critical && !x.textDisplay {
		x.served = s.overlayMenu(cfg, d, x.served, &menu, x.menuTouch)
		x.menu = menu
	}
	if x.served.interval == 0 {
		x.served.interval = x.served.refresh
	}
	x.served.refresh = max(x.served.refresh, x.floor)
	x.served.interval = max(x.served.interval, x.floor)
	x.refresh = x.served.refresh
	// A timer sync lands on the grid of its interval, so a page that shows a
	// clock, a departure or a schedule is composed at that time (ADR 0129).
	// A plan boundary or the page timeout shortened the sleep already: that cap
	// wins, and the next sync aligns again.
	if !x.textDisplay && x.refresh == x.served.interval {
		if aligned := alignRefresh(now, x.served.interval, x.floor, cfg.Timezone); aligned > 0 {
			x.refresh = aligned
		}
	}
	// Remember what the device was pointed at, so /api/devices can show the
	// current content. Rendering happened above, outside the lock.
	x.shownKind, x.shownPage = "page", x.served.name
	switch {
	case x.critical:
		x.shownKind, x.shownPage = "battery", ""
	case x.served.name == "":
		x.shownKind = "notice"
	}
	x.errText = ""
	switch {
	case x.choice.err != nil:
		x.errText = shortError(x.choice.err)
	case x.served.err != nil:
		x.errText = shortError(x.served.err)
	}
}

// finish stores what the device now shows and builds the answer. The state may
// have changed while the page was rendered (a background action), so the lock
// is taken again and the menu merged.
func (x *syncSession) finish() syncResponse {
	s, cfg, d, now, opt := x.s, x.cfg, x.d, x.now, x.opt
	menu := x.menu
	s.lock("sync.lock")
	if !opt.virtual {
		// A background action may have finished and replaced the menu while
		// the page was rendered; a virtual display has no such path.
		x.st = s.state[d.stateKey()]
	}
	if st := x.st; st != nil {
		menu = keepActionResult(menu, st.Menu)
		st.LastPage, st.LastPageKind, st.LastPageHash = x.shownPage, x.shownKind, x.served.hash
		st.LastPages = x.served.pages
		st.LastPageTime, st.LastRefresh, st.LastInterval = now, int(x.refresh.Seconds()), int(x.served.interval.Seconds())
		st.LastText = x.served.lines
		st.LastError = x.errText
		st.Menu = menu
		if x.textDisplay {
			x.textPart, st.ScrollHash = textResponse(x.served, cfg.Profiles[d.Profile], st.ScrollHash)
		}
		if !opt.virtual {
			x.pageData = s.stateSnapshotLocked(st)
		}
	}
	s.mu.Unlock()
	x.menu = menu
	writeDone := s.stats.Start("sync.state_write")
	s.saveState(d.stateKey(), x.pageData)
	writeDone()
	x.logPower()
	listen := time.Duration(0)
	if x.input && !x.textDisplay && !opt.virtual {
		listen = listenAfterInput
	}
	wake := menuWake(menu, d, now, x.critical)
	// The firmware sleeps only after listening, and the one-shot wake counts
	// from the sync: give the wake the time it needs, so the menu expires when
	// the panel is repainted instead of listen_s later (ADR 0078).
	listen, wake = fitWakeToListen(listen, wake)
	x.logSync()
	resp := syncResponse{
		RefreshS: int(x.refresh.Seconds()),
		ListenS:  int(listen.Seconds()),
		Sleep:    x.power == powerBattery,
		Time:     now.Unix(),
		Rotation: cfg.rotation(d),
		Firmware: x.offer,
		Wake:     wake,
	}
	// The rule or the operator decides whether to signal; the device file
	// decides which methods it may use (ADR 0132). The firmware ignores a
	// method it lacks.
	if x.alert {
		resp.Beep, resp.Blink = d.alertMethods()
	}
	if x.textDisplay {
		if x.textPart == nil { // the state vanished between rendering and the final lock
			x.textPart, _ = textResponse(x.served, cfg.Profiles[d.Profile], "")
		}
		resp.Text = x.textPart
	} else {
		resp.Image = &syncImage{Path: x.served.path, Hash: x.served.hash}
	}
	return resp
}

// logPower writes the journal lines about the power source and the battery
// level; they differ from the previous sync or are warnings.
func (x *syncSession) logPower() {
	d := x.d
	logged := x.logged
	if x.powerChanged {
		attrs := []any{"device", d.ID, "power", x.power}
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
	if logged.BatteryLevel != x.prevLevel {
		level := logged.BatteryLevel
		if level == batteryOK {
			level = "ok"
		}
		slog.Warn("battery warning", "device", d.ID, "level", level, "battery", logged.BatteryVoltage)
	}
}

// logSync writes the one display line of this wake: page, rule, reason and the
// values an operator uses to explain what the device shows.
func (x *syncSession) logSync() {
	d, req := x.d, x.req
	pageName := x.served.name
	switch {
	case x.critical:
		pageName = "battery"
	case pageName == "":
		pageName = "notice"
	}
	attrs := []any{"device", d.ID, "page", pageName}
	if x.served.pages > 1 {
		attrs = append(attrs, "index", x.served.page, "pages", x.served.pages)
	}
	attrs = append(attrs, "rule", x.logged.Rule, "reason", req.Reason)
	if x.alert {
		attrs = append(attrs, "alert", x.alertSource)
	}
	if len(x.buttons) > 0 {
		attrs = append(attrs, "buttons", strings.Join(x.buttons, ","))
	}
	if x.menu.Open {
		attrs = append(attrs, "menu", x.menu.Phase, "action", x.menu.Selected)
	}
	attrs = append(attrs, "changed", req.Shown != x.served.hash, "battery", x.logged.BatteryVoltage,
		"rssi", x.logged.RSSI, "refresh", x.refresh, "power", x.power)
	// The fuel gauge current shows whether the battery is charging (positive)
	// or discharging (negative); only devices with a gauge send it.
	if x.logged.BatteryMA != nil {
		attrs = append(attrs, "battery_ma", fmt.Sprintf("%+.0f", *x.logged.BatteryMA))
	}
	// The charger's own state (the Sticky's STAT pin) next to the gauge
	// current, so the log shows when the two disagree.
	if x.logged.BatteryCharging != nil {
		charger := "idle"
		if *x.logged.BatteryCharging {
			charger = "charging"
		}
		attrs = append(attrs, "charger", charger)
	}
	slog.Info("display sync", attrs...)
}
