package paperboy

import (
	"context"
	"fmt"
	"image"
	"log/slog"
	"strings"
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/server/internal/plan"
)

// The device menu (ADR 0057): a server-drawn overlay over the current page
// that offers the plan's actions. The firmware only reports button and touch
// events; everything about the menu lives here, so the look, the language and
// the availability of an action are decided on the server.

// Phases of the menu.
const (
	menuEmptyPhase   = "empty" // the plan offers no actions: a banner only
	menuOffPhase     = "off"   // every action is unavailable, or the chosen one is
	menuListPhase    = "list"
	menuConfirmPhase = "confirm"
	menuRunningPhase = "running"
	menuResultPhase  = "result"
)

const (
	// menuPerPage is how many actions the overlay shows at once.
	menuPerPage = 3
	// actionBudget bounds how long a sync waits for a running action, so a slow
	// plugin never holds the device open for its full timeout (ADR 0057).
	actionBudget = 5 * time.Second
	// pluginActionTimeout bounds the background run of an action (ADR 0064).
	pluginActionTimeout = 30 * time.Second
	// menuHoldLong is the press duration from which a button counts as held
	// (ADR 0067). The firmware reports the measured time and leaves the
	// threshold to the server.
	menuHoldLong = 700 * time.Millisecond
	// menuEmptyDuration shortens the open time of the empty menu, which is
	// only an announcement (ADR 0067).
	menuEmptyDuration = 10 * time.Second
)

// menuAction is one plan action with the state the menu shows. It is stored
// in the device's state file, so it stays plain JSON.
type menuAction struct {
	Key         string         `json:"key"`                  // identity: id or plugin/action
	Action      string         `json:"action"`               // plugin/action
	Plugin      string         `json:"plugin"`               // part before the slash
	Name        string         `json:"name"`                 // part after the slash
	Args        map[string]any `json:"args,omitempty"`       // the plan's arguments
	Title       string         `json:"title"`                // plugin title, overridden by the plan
	PlanTitle   bool           `json:"plan_title,omitempty"` // Title came from the plan
	Disabled    string         `json:"disabled,omitempty"`   // reason the action cannot run now
	Confirm     bool           `json:"confirm,omitempty"`
	ConfirmText string         `json:"confirm_text,omitempty"`
}

// menuState is the persisted menu of one device.
type menuState struct {
	Open        bool         `json:"open,omitempty"`
	Phase       string       `json:"phase,omitempty"` // list, confirm, running, result
	Selected    string       `json:"selected,omitempty"`
	CloseSel    bool         `json:"close_selected,omitempty"` // the close box of the control strip has the selection
	Offset      int          `json:"offset,omitempty"`         // first visible action
	Session     int          `json:"session,omitempty"`
	ConfirmHash string       `json:"confirm_hash,omitempty"` // hash of the served confirmation screen
	Screen      string       `json:"screen,omitempty"`       // hash of the last served menu screen
	OpenedAt    time.Time    `json:"opened_at,omitzero"`
	LastInput   time.Time    `json:"last_input,omitzero"` // last button or touch that kept the menu open
	Actions     []menuAction `json:"actions,omitempty"`
	Resolved    bool         `json:"resolved,omitempty"` // plugin state is filled in
	Result      string       `json:"result,omitempty"`
	Reason      string       `json:"reason,omitempty"` // why no action is available
	ResultOK    bool         `json:"result_ok,omitempty"`
	// Simulated marks a result the virtual display did not run: it names the
	// action instead of reporting an outcome (ADR 0091).
	Simulated bool `json:"simulated,omitempty"`
}

// allDisabled reports whether no action of the list can run now.
func allDisabled(actions []menuAction) bool {
	for _, a := range actions {
		if a.Disabled == "" {
			return false
		}
	}
	return len(actions) > 0
}

// closeMenu drops all menu state.
func (m *menuState) close() { *m = menuState{} }

// menuDeadline is how long an open menu may stay without input: the configured
// duration, shortened for the empty banner (ADR 0067). The deadline slides with
// every button or touch that keeps the menu open, because a press costs a whole
// wake cycle on a battery device (ADR 0078).
func menuDeadline(m menuState, d *Device) time.Duration {
	show := time.Duration(d.MenuShowDuration)
	if m.Phase == menuEmptyPhase || m.Phase == menuOffPhase {
		show = min(show, menuEmptyDuration)
	}
	return show
}

// expiresAt is when the menu closes unless the user keeps it busy: the show
// duration counted from opening or from the last input, whichever is later.
func (m menuState) expiresAt(d *Device) time.Time {
	from := m.OpenedAt
	if m.LastInput.After(from) {
		from = m.LastInput
	}
	return from.Add(menuDeadline(m, d))
}

// openMenu starts a new menu session with the resolved actions. A plan without
// actions still opens the menu; it shows a banner instead of the action grid
// (ADR 0067).
func (m *menuState) openMenu(session int, actions []menuAction, now time.Time) {
	phase := menuListPhase
	reason := ""
	if len(actions) == 0 {
		phase = menuEmptyPhase
	} else if allDisabled(actions) {
		// Nothing to choose: say so once instead of showing a list in which
		// every cell is hatched (ADR 0082).
		phase, reason = menuOffPhase, actions[0].Disabled
	}
	*m = menuState{Open: true, Phase: phase, Session: session, OpenedAt: now, Actions: actions, Reason: reason}
	if len(actions) > 0 {
		m.Selected = actions[0].Key
	}
}

func (m *menuState) selected() *menuAction {
	return m.find(m.Selected)
}

func (m *menuState) find(key string) *menuAction {
	for i := range m.Actions {
		if m.Actions[i].Key == key {
			return &m.Actions[i]
		}
	}
	return nil
}

// nextTurn reports whether the menu may act; the plan must still offer the
// selected identity, otherwise the menu closes (ADR 0057). An empty menu has
// no selection to lose.
func (m *menuState) stale(acts []plan.PlanAction) bool {
	if !m.Open || m.Selected == "" {
		return false
	}
	for _, a := range acts {
		if a.Key() == m.Selected {
			return false
		}
	}
	return true
}

// move walks the selection and scrolls so that it stays visible. On a touch
// device the close box sits behind the last action, so `next` past the end
// reaches it and `previous` returns (ADR 0082); without the control strip the
// list simply wraps.
func (m *menuState) move(delta int, touch bool) {
	n := len(m.Actions)
	if n == 0 {
		return
	}
	if m.CloseSel {
		if delta < 0 {
			m.CloseSel = false
		}
		return
	}
	i := 0
	for j := range m.Actions {
		if m.Actions[j].Key == m.Selected {
			i = j
		}
	}
	if touch && delta > 0 && i == n-1 {
		m.CloseSel = true
		return
	}
	i = ((i+delta)%n + n) % n
	m.Selected = m.Actions[i].Key
	m.reveal(i)
}

func (m *menuState) reveal(i int) {
	if i < 0 || len(m.Actions) == 0 {
		return
	}
	if m.Offset > i || i >= m.Offset+menuPerPage {
		m.Offset = i
	}
	if m.Offset+menuPerPage > len(m.Actions) {
		m.Offset = max(0, len(m.Actions)-menuPerPage)
	}
	if m.Offset < 0 {
		m.Offset = 0
	}
}

// menuInput is what one wake did to the menu.
type menuInput struct {
	buttons []string // roles in order
	holds   []int    // hold_ms of the matching buttons
	shown   string   // hash the device painted last
	touch   bool     // the device draws the tap-only control strip (ADR 0082)
}

// tap applies one touch inside the overlay (ADR 0057, issue #54). It returns
// an action to run when the tap confirms or selects one. A tap outside the
// overlay, or in a cell that the current phase does not have, is ignored.
// The geometry is derived from the phase and the display, so a tap always
// hits the cells Paperboy last drew for that state.
func (m *menuState) tap(x, y int, p Profile, touch bool, shown string, now time.Time) *menuAction {
	if !m.Open {
		return nil
	}
	actions, controls, overlay := menuLayout(p, m.Phase, len(m.Actions), touch)
	at := image.Pt(x, y)
	if !at.In(overlay) {
		return nil
	}
	if m.Phase == menuRunningPhase {
		// Nothing to dismiss while the action runs.
		return nil
	}
	if m.Phase == menuResultPhase || m.Phase == menuEmptyPhase {
		// The result and the empty menu are banners: any tap dismisses them.
		m.close()
		return nil
	}
	for _, c := range append(actions, controls...) {
		if !at.In(c.rect) {
			continue
		}
		// A tap that keeps the menu open also keeps its deadline (ADR 0078).
		m.LastInput = now
		switch c.kind {
		case cellClose:
			m.close()
		case cellPrev:
			m.scroll(-menuPerPage)
		case cellNext:
			m.scroll(menuPerPage)
		case cellYes:
			if m.Phase != menuConfirmPhase || m.ConfirmHash == "" || shown != m.ConfirmHash {
				return nil
			}
			m.Phase = menuRunningPhase
			return m.selected()
		case cellNo:
			if m.Phase != menuConfirmPhase {
				return nil
			}
			m.Phase = menuListPhase
			m.ConfirmHash = ""
		case cellAction:
			if m.Phase != menuListPhase {
				return nil
			}
			idx := m.Offset + c.index
			if idx < 0 || idx >= len(m.Actions) {
				return nil
			}
			a := &m.Actions[idx]
			m.Selected = a.Key
			m.CloseSel = false
			m.reveal(idx)
			if a.Disabled != "" || !m.Resolved {
				return nil
			}
			if a.Confirm {
				m.Phase = menuConfirmPhase
				m.ConfirmHash = ""
				return nil
			}
			m.Phase = menuRunningPhase
			return a
		}
		return nil
	}
	return nil
}

// scroll moves the visible window by delta actions and keeps the selection
// inside it.
func (m *menuState) scroll(delta int) {
	n := len(m.Actions)
	if n <= menuPerPage {
		return
	}
	m.Offset += delta
	if m.Offset > n-menuPerPage {
		m.Offset = n - menuPerPage
	}
	if m.Offset < 0 {
		m.Offset = 0
	}
	// Scrolling selects the first action of the new page, so a following
	// "home" or tap acts on something the user sees.
	m.Selected = m.Actions[m.Offset].Key
}

// step applies the wake's input to the menu and returns whether an action must
// run now. It is called under the state lock; running the action needs plugins
// and therefore happens outside it. A short home opens the menu even when the
// plan offers no actions; a long home closes it and keeps the page (ADR 0067).
// Any button that keeps the menu open keeps its deadline too (ADR 0078).
func (m *menuState) step(in menuInput, acts []plan.PlanAction, now time.Time) (invoke *menuAction) {
	invoke = m.applyInput(in, acts, now)
	if m.Open && len(in.buttons) > 0 {
		m.LastInput = now
	}
	return invoke
}

// applyInput is the menu's state machine for one wake.
func (m *menuState) applyInput(in menuInput, acts []plan.PlanAction, now time.Time) (invoke *menuAction) {
	if !m.Open {
		for i, b := range in.buttons {
			if b == buttonHome && !held(hold(in.holds, i)) {
				m.openMenu(m.Session+1, planActions(acts), now)
				return nil
			}
		}
		return nil
	}
	// A long home always leaves the menu; it no longer jumps to page 1.
	for i, b := range in.buttons {
		if b == buttonHome && held(hold(in.holds, i)) {
			m.close()
			return nil
		}
	}
	if m.stale(acts) {
		m.close()
		return nil
	}
	if m.Phase == menuEmptyPhase || m.Phase == menuOffPhase {
		// The banner has nothing to offer: any input dismisses it.
		if len(in.buttons) > 0 {
			m.close()
		}
		return nil
	}
	switch m.Phase {
	case menuListPhase:
		for i, b := range in.buttons {
			switch b {
			case buttonNext:
				// Long presses are ignored inside the menu (ADR 0067).
				if !held(hold(in.holds, i)) {
					m.move(1, in.touch)
				}
			case buttonPrevious:
				if !held(hold(in.holds, i)) {
					m.move(-1, in.touch)
				}
			case buttonHome:
				// The close box of the control strip has the selection: home
				// dismisses the menu instead of running an action (ADR 0082).
				if m.CloseSel {
					m.close()
					return nil
				}
				// The first home only opens the menu: the plugin state of the
				// actions is resolved after this step, so an action can never
				// run without the user having seen its cell.
				if !m.Resolved {
					continue
				}
				a := m.selected()
				if a == nil {
					continue
				}
				if a.Disabled != "" {
					// Nothing to run: show the plugin's reason as a banner.
					m.Phase = menuOffPhase
					m.Reason = a.Disabled
					return nil
				}
				if a.Confirm {
					m.Phase = menuConfirmPhase
					m.ConfirmHash = ""
				} else {
					m.Phase = menuRunningPhase
					m.ConfirmHash = ""
					invoke = a
				}
			}
		}
	case menuConfirmPhase:
		for i, b := range in.buttons {
			switch b {
			case buttonHome:
				// Only a home press from a device that painted the
				// confirmation screen executes the action; anything else
				// shows the confirmation again.
				if m.ConfirmHash == "" || in.shown != m.ConfirmHash {
					continue
				}
				m.Phase = menuRunningPhase
				invoke = m.selected()
			case buttonNext, buttonPrevious:
				if !held(hold(in.holds, i)) {
					m.Phase = menuListPhase
					m.ConfirmHash = ""
				}
			}
		}
	case menuResultPhase:
		// Any input acknowledges the result and leaves the menu.
		if len(in.buttons) > 0 {
			m.close()
		}
	}
	return invoke
}

// hold reads the hold_ms of the button at i.
func hold(holds []int, i int) int {
	if i < len(holds) {
		return holds[i]
	}
	return 0
}

// held reports whether a press lasted long enough to count as held. The
// firmware reports the measured duration; the threshold lives here (ADR 0067).
func held(ms int) bool {
	return time.Duration(ms)*time.Millisecond >= menuHoldLong
}

// pageRoles translates the buttons of a wake in which the menu stays closed
// (ADR 0067): a long next or previous jumps to the first page of the active
// rule, and a long home is dropped because it only belongs to the open menu.
func pageRoles(buttons []string, holds []int) []string {
	out := make([]string, 0, len(buttons))
	for i, b := range buttons {
		switch {
		case b == buttonHome && held(hold(holds, i)):
		case (b == buttonNext || b == buttonPrevious) && held(hold(holds, i)):
			out = append(out, buttonHome)
		default:
			out = append(out, b)
		}
	}
	return out
}

// planActions turns the plan's list into menu entries without plugin state.
// Plugin state is filled in by resolveActions once the menu opens.
func planActions(acts []plan.PlanAction) []menuAction {
	out := make([]menuAction, 0, len(acts))
	for _, a := range acts {
		p, n, _ := strings.Cut(a.Action, "/")
		out = append(out, menuAction{Key: a.Key(), Action: a.Action, Plugin: p, Name: n, Args: a.Args, Title: a.Title, PlanTitle: a.Title != ""})
	}
	return out
}

// resolveActions asks the plugins for the state of the plan's actions. A
// plugin that is down keeps its entries visible as unavailable with a reason
// instead of hiding them (ADR 0057).
func (s *Server) resolveActions(ctx context.Context, cfg *Config, d *Device, entries []menuAction, now time.Time) []menuAction {
	out := entries
	descs := map[string]*plugin.Description{}
	byPlugin := map[string][]int{}
	var names []string
	for i, a := range out {
		if _, ok := byPlugin[a.Plugin]; !ok {
			names = append(names, a.Plugin)
		}
		byPlugin[a.Plugin] = append(byPlugin[a.Plugin], i)
	}
	prof := cfg.Profiles[d.Profile]
	page := plugin.PageInfo{Width: prof.Width, Height: prof.Height, PPI: prof.PPI}
	device := &plugin.DeviceInfo{ID: d.ID, Hardware: s.hardwareOf(d)}
	for _, name := range names {
		idx := byPlugin[name]
		desc, err := s.widgets.Describe(ctx, name)
		if err != nil {
			reason := shortError(err)
			for _, i := range idx {
				out[i].Disabled = reason
			}
			continue
		}
		descs[name] = desc
		req := plugin.Request{Version: plugin.Version, Language: cfg.Language, Timezone: cfg.timezoneName(),
			Now: now, Page: page, Device: device}
		for _, i := range idx {
			req.Actions = append(req.Actions, plugin.ActionCall{Action: out[i].Name, Args: out[i].Args})
		}
		states, err := s.widgets.Actions(ctx, name, req)
		if err != nil {
			reason := shortError(err)
			for _, i := range idx {
				out[i].Disabled = reason
			}
			continue
		}
		specs := map[string]plugin.ActionSpec{}
		for _, a := range desc.Actions {
			specs[a.Name] = a
		}
		for n, i := range idx {
			a := &out[i]
			spec, known := specs[a.Name]
			if !known {
				a.Disabled = fmt.Sprintf("unknown action %s/%s", a.Plugin, a.Name)
				continue
			}
			// A title from the plan wins; otherwise the plugin's state names
			// the concrete action, and the spec is the fallback.
			st := states[n]
			if !a.PlanTitle {
				switch {
				case st.Title != "":
					a.Title = st.Title
				case spec.Title != "":
					a.Title = spec.Title
				default:
					a.Title = a.Name
				}
			}
			if err := plugin.Values(a.Args).Check(spec.Args); err != nil {
				a.Disabled = err.Error()
				continue
			}
			if st.Disabled != "" {
				a.Disabled = st.Disabled
			}
			conf := spec.Confirm
			if st.Confirm != nil {
				conf = st.Confirm
			}
			if conf != nil && conf.Required {
				a.Confirm = true
				a.ConfirmText = conf.Text
			}
		}
	}
	return out
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

// invokeAction runs one action with a bounded context and returns its outcome.
func (s *Server) invokeAction(ctx context.Context, cfg *Config, d *Device, a *menuAction, now time.Time) (ok bool, message string) {
	prof := cfg.Profiles[d.Profile]
	req := plugin.Request{Version: plugin.Version, Language: cfg.Language, Timezone: cfg.timezoneName(),
		Now: now, Page: plugin.PageInfo{Width: prof.Width, Height: prof.Height, PPI: prof.PPI},
		Device:  &plugin.DeviceInfo{ID: d.ID, Hardware: s.hardwareOf(d)},
		Actions: []plugin.ActionCall{{Action: a.Name, Args: a.Args}}}
	out, err := s.widgets.Invoke(ctx, a.Plugin, req)
	if err != nil {
		slog.Warn("action failed", "device", d.ID, "action", a.Action, "error", err)
		return false, shorten(err.Error())
	}
	slog.Info("action run", "device", d.ID, "action", a.Action, "ok", out.OK, "message", out.Message)
	return out.OK, out.Message
}

// actionRun is one action that is still running after its sync.
type actionRun struct {
	done    chan struct{}
	ok      bool
	message string
}

// startAction runs an action in the background (ADR 0064): the sync waits at
// most actionBudget for it, and a longer run reports its result with the next
// wake of reason "result".
func (s *Server) startAction(cfg *Config, d *Device, a menuAction, now time.Time) *actionRun {
	run := &actionRun{done: make(chan struct{})}
	s.mu.Lock()
	s.actionRuns[d.stateKey()] = run
	s.mu.Unlock()
	go func() {
		defer close(run.done)
		ctx, cancel := context.WithTimeout(context.Background(), pluginActionTimeout)
		defer cancel()
		run.ok, run.message = s.invokeAction(ctx, cfg, d, &a, now)
		s.finishAction(d.stateKey(), a.Key, run)
	}()
	return run
}

// finishAction stores the outcome of a background action if its menu is still
// waiting for it.
func (s *Server) finishAction(key, identity string, run *actionRun) {
	s.mu.Lock()
	if s.actionRuns[key] != run {
		s.mu.Unlock()
		return
	}
	delete(s.actionRuns, key)
	st := s.state[key]
	if st == nil || !st.Menu.Open || st.Menu.Phase != menuRunningPhase || st.Menu.Selected != identity {
		s.mu.Unlock()
		return
	}
	st.Menu.Phase = menuResultPhase
	st.Menu.Result, st.Menu.ResultOK, st.Menu.ConfirmHash = run.message, run.ok, ""
	data := s.stateSnapshotLocked(st)
	s.mu.Unlock()
	s.saveState(key, data)
}

// keepActionResult keeps a result that arrived while the page was rendered.
// local was copied before the action ran and still names the running phase;
// stored is the current state, which finishAction may have moved to the result.
// The screen has to stay the one this sync serves, because the device reports
// that hash as shown until it paints again.
func keepActionResult(local, stored menuState) menuState {
	if local.Phase != menuRunningPhase || stored.Phase != menuResultPhase ||
		!stored.Open || stored.Session != local.Session || stored.Selected != local.Selected {
		return local
	}
	stored.Screen = local.Screen
	return stored
}

// menuCells lays the overlay out: the top half of the display, one row in
// landscape and a stack in portrait (ADR 0057). kinds are in reading order.
type menuCell struct {
	kind  string // close, prev, next, action, yes, no
	index int    // action index in the full list; -1 otherwise
	rect  image.Rectangle
}

const (
	cellClose  = "close"
	cellPrev   = "prev"
	cellNext   = "next"
	cellAction = "action"
	cellYes    = "yes"
	cellNo     = "no"
)
