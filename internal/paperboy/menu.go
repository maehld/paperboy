package paperboy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"log/slog"
	"strings"
	"time"

	"github.com/maehld/paperboy/internal/plan"
	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

// The device menu (ADR 0057): a server-drawn overlay over the current page
// that offers the plan's actions. The firmware only reports button and touch
// events; everything about the menu lives here, so the look, the language and
// the availability of an action are decided on the server.

// Phases of the menu.
const (
	menuEmptyPhase   = "empty" // the plan offers no actions: a banner only
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
	Offset      int          `json:"offset,omitempty"` // first visible action
	Session     int          `json:"session,omitempty"`
	ConfirmHash string       `json:"confirm_hash,omitempty"` // hash of the served confirmation screen
	Screen      string       `json:"screen,omitempty"`       // hash of the last served menu screen
	OpenedAt    time.Time    `json:"opened_at,omitempty"`
	LastInput   time.Time    `json:"last_input,omitempty"` // last button or touch that kept the menu open
	Actions     []menuAction `json:"actions,omitempty"`
	Resolved    bool         `json:"resolved,omitempty"` // plugin state is filled in
	Result      string       `json:"result,omitempty"`
	ResultOK    bool         `json:"result_ok,omitempty"`
}

// closeMenu drops all menu state.
func (m *menuState) close() { *m = menuState{} }

// menuDeadline is how long an open menu may stay without input: the configured
// duration, shortened for the empty banner (ADR 0067). The deadline slides with
// every button or touch that keeps the menu open, because a press costs a whole
// wake cycle on a battery device (ADR 0078).
func menuDeadline(m menuState, d *Device) time.Duration {
	show := time.Duration(d.MenuShowDuration)
	if m.Phase == menuEmptyPhase {
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
	if len(actions) == 0 {
		phase = menuEmptyPhase
	}
	*m = menuState{Open: true, Phase: phase, Session: session, OpenedAt: now, Actions: actions}
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

// move changes the selection by delta, wrapping. It also scrolls so that the
// selection stays visible.
func (m *menuState) move(delta int) {
	n := len(m.Actions)
	if n == 0 {
		return
	}
	i := 0
	for j := range m.Actions {
		if m.Actions[j].Key == m.Selected {
			i = j
		}
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
}

// tap applies one touch inside the overlay (ADR 0057, issue #54). It returns
// an action to run when the tap confirms or selects one. A tap outside the
// overlay, or in a cell that the current phase does not have, is ignored.
// The geometry is derived from the phase and the display, so a tap always
// hits the cells Paperboy last drew for that state.
func (m *menuState) tap(x, y int, p Profile, shown string, now time.Time) *menuAction {
	if !m.Open {
		return nil
	}
	kinds := menuKinds(m.Phase, len(m.Actions))
	cells, _, overlay := menuCells(p, kinds, m.Phase != menuListPhase)
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
	for i, c := range cells {
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
			idx := m.Offset + i - 1
			if idx < 0 || idx >= len(m.Actions) {
				return nil
			}
			a := &m.Actions[idx]
			m.Selected = a.Key
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
	if m.Phase == menuEmptyPhase {
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
					m.move(1)
				}
			case buttonPrevious:
				if !held(hold(in.holds, i)) {
					m.move(-1)
				}
			case buttonHome:
				// The first home only opens the menu: the plugin state of the
				// actions is resolved after this step, so an action can never
				// run without the user having seen its cell.
				if !m.Resolved {
					continue
				}
				a := m.selected()
				if a == nil || a.Disabled != "" {
					continue
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

// overlayRect is the area the menu covers: the top half, where the buttons
// are on both current devices.
func overlayRect(p Profile) image.Rectangle {
	return image.Rect(0, 0, p.Width, max(1, p.Height/2))
}

// bannerRect is the strip that shows a running action or its result. It is
// not a menu screen: it needs no selection and no close box, and it leaves
// almost the whole page visible.
func bannerRect(p Profile) image.Rectangle {
	return image.Rect(0, 0, p.Width, max(28, p.Height/8))
}

// menuCells splits the top half into equal cells. header reserves a strip for
// the confirmation or result text.
func menuCells(p Profile, kinds []string, header bool) (cells []menuCell, headerRect, overlay image.Rectangle) {
	overlay = overlayRect(p)
	area := overlay
	if header {
		h := max(18, overlay.Dy()/4)
		headerRect = image.Rect(overlay.Min.X, overlay.Min.Y, overlay.Max.X, overlay.Min.Y+h)
		area = image.Rect(overlay.Min.X, headerRect.Max.Y, overlay.Max.X, overlay.Max.Y)
	}
	n := max(len(kinds), 1)
	portrait := p.Height > p.Width
	for i, kind := range kinds {
		var r image.Rectangle
		if portrait {
			h := area.Dy() / n
			r = image.Rect(area.Min.X, area.Min.Y+i*h, area.Max.X, area.Min.Y+(i+1)*h)
			if i == n-1 {
				r.Max.Y = area.Max.Y
			}
		} else {
			w := area.Dx() / n
			r = image.Rect(area.Min.X+i*w, area.Min.Y, area.Min.X+(i+1)*w, area.Max.Y)
			if i == n-1 {
				r.Max.X = area.Max.X
			}
		}
		cells = append(cells, menuCell{kind: kind, index: -1, rect: r})
	}
	return cells, headerRect, overlay
}

// menuView is what the overlay shows in one request.
type menuView struct {
	phase   string
	actions []menuAction // the full list, for the visible page and the header
	offset  int
	sel     string
	title   string // confirmation or result title
	text    string // confirmation hint or result message
}

// menuKinds returns the cells of one phase in reading order.
func menuKinds(phase string, count int) []string {
	switch phase {
	case menuConfirmPhase:
		// Yes and no only: a close box would be the same as "no".
		return []string{cellYes, cellNo}
	case menuRunningPhase, menuResultPhase, menuEmptyPhase:
		return nil // drawn as a banner, no cells
	}
	kinds := []string{cellClose}
	for i := 0; i < menuPerPage; i++ {
		kinds = append(kinds, cellAction)
	}
	if count > menuPerPage {
		kinds = append(kinds, cellPrev, cellNext)
	}
	return kinds
}

// drawMenu paints the overlay onto the page image (ADR 0057). The page stays
// visible below the top half.
func drawMenu(img *image.Paletted, p Profile, v menuView, t texts) {
	overlay := overlayRect(p)
	if overlay.Intersect(img.Rect).Empty() {
		return
	}
	bg := image.Rect(overlay.Min.X, overlay.Min.Y, min(overlay.Max.X, img.Rect.Max.X), min(overlay.Max.Y, img.Rect.Max.Y))
	render.FillColor(img, bg, render.White)
	header := v.phase != menuListPhase
	if v.phase == menuRunningPhase || v.phase == menuResultPhase || v.phase == menuEmptyPhase {
		drawBanner(img, p, v)
		return
	}
	kinds := menuKinds(v.phase, len(v.actions))
	cells, headerRect, _ := menuCells(p, kinds, header)
	size := max(render.MinSize, float64(overlay.Dy())/5)
	if len(cells) > 0 {
		c := cells[0].rect
		size = max(render.MinSize, min(float64(c.Dx())/7.5, float64(c.Dy())/4.5))
	}
	ai := v.offset
	for i := range cells {
		c := &cells[i]
		var label string
		var action *menuAction
		switch c.kind {
		case cellClose:
			label = t.menuClose()
		case cellPrev:
			label = "<"
		case cellNext:
			label = ">"
		case cellYes:
			label = t.menuYes()
		case cellNo:
			label = t.menuNo()
		case cellAction:
			c.index = ai
			if ai < len(v.actions) {
				action = &v.actions[ai]
				label = action.Title
			}
			ai++
		}
		selected := action != nil && action.Key == v.sel
		drawCell(img, *c, label, size, selected, action != nil && action.Disabled != "")
	}
	if header {
		drawMenuHeader(img, headerRect, v, size, t)
	}
	// A rule under the overlay separates it from the page.
	thickness := max(1, int(p.PPI/72*0.6))
	render.HLine(img, overlay.Min.X, overlay.Max.Y-1, overlay.Max.X, thickness)
}

func drawCell(img *image.Paletted, c menuCell, label string, size float64, selected, disabled bool) {
	if c.rect.Empty() {
		return
	}
	if disabled {
		// Unavailable actions are hatched whether selected or not; the
		// selection gets a heavier frame, because inverting would fight the
		// hatch.
		render.Fill(img, c.rect, "hatch")
	} else if selected {
		render.Fill(img, c.rect, "solid")
	}
	ink := render.Black
	if selected && !disabled {
		ink = render.White
	} else if disabled {
		ink = render.DarkGray
	}
	// Cell borders keep the buttons visible on the page beneath.
	render.FillColor(img, image.Rect(c.rect.Min.X, c.rect.Min.Y, c.rect.Min.X+1, c.rect.Max.Y), render.Black)
	inner := c.rect.Inset(1)
	lines := wrapMenu(label, inner.Dx()-6, size)
	if len(lines) == 0 && label != "" {
		lines = []string{render.Ellipsize(label, size, false, inner.Dx()-6)}
	}
	// Center the block of lines vertically in the cell.
	lineH := int(size * 1.2)
	top := inner.Min.Y + (inner.Dy()-len(lines)*lineH)/2 + int(size)
	cx := inner.Min.X + inner.Dx()/2
	if disabled && len(lines) > 0 {
		// The label gets a white plate, so it stays readable over the hatch.
		widest := 0
		for _, line := range lines {
			widest = max(widest, render.Measure(line, size, false))
		}
		plate := image.Rect(cx-widest/2-3, top-lineH+2, cx+widest/2+3, top+lineH*(len(lines)-1)+4)
		render.FillColor(img, plate.Intersect(inner), render.White)
	}
	for _, line := range lines {
		if top > inner.Max.Y-int(size)/2 {
			break
		}
		render.Text(img, line, cx, top, render.Style{Size: size, Bold: selected, Shrink: true}, inner.Dx()-6, ink, render.Center)
		top += lineH
	}
	if disabled && selected {
		frameCell(img, c.rect, 2)
	}
}

// drawBanner paints the running action and its result as a strip at the top:
// the outcome is an announcement, not a dialog, so it has no cells and no
// close box. A running action is outlined, a result is filled.
func drawBanner(img *image.Paletted, p Profile, v menuView) {
	r := bannerRect(p)
	r = r.Intersect(img.Rect)
	if r.Empty() {
		return
	}
	text := v.title
	if v.text != "" {
		if text == "" {
			text = v.text
		} else {
			text += ": " + v.text
		}
	}
	ink := render.White
	if v.phase == menuRunningPhase {
		render.FillColor(img, r, render.White)
		frameCell(img, r, 2)
		ink = render.Black
	} else {
		render.FillColor(img, r, render.Black)
	}
	size := max(render.MinSize, float64(r.Dy())*0.52)
	render.Text(img, text, r.Min.X+12, r.Min.Y+r.Dy()/2+int(size*0.36),
		render.Style{Size: size, Bold: true, Shrink: true}, r.Dx()-24, ink, render.Left)
}

// frameCell draws a border of the given thickness inside r.
func frameCell(img *image.Paletted, r image.Rectangle, t int) {
	render.FillColor(img, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+t), render.Black)
	render.FillColor(img, image.Rect(r.Min.X, r.Max.Y-t, r.Max.X, r.Max.Y), render.Black)
	render.FillColor(img, image.Rect(r.Min.X, r.Min.Y, r.Min.X+t, r.Max.Y), render.Black)
	render.FillColor(img, image.Rect(r.Max.X-t, r.Min.Y, r.Max.X, r.Max.Y), render.Black)
}

func drawMenuHeader(img *image.Paletted, r image.Rectangle, v menuView, size float64, t texts) {
	if r.Empty() {
		return
	}
	render.FillColor(img, image.Rect(r.Min.X, r.Max.Y-1, r.Max.X, r.Max.Y), render.Black)
	titleSize := min(size, float64(r.Dy())*0.5)
	hintSize := max(render.MinSize, titleSize*0.62)
	if v.title != "" {
		render.Text(img, v.title, r.Min.X+5, r.Min.Y+int(titleSize*1.15)+3,
			render.Style{Size: titleSize, Bold: true, Shrink: true}, r.Dx()-10, render.Black, render.Left)
	}
	if v.text != "" {
		render.Text(img, v.text, r.Min.X+5, r.Max.Y-5,
			render.Style{Size: hintSize, Shrink: true}, r.Dx()-10, render.DarkGray, render.Left)
	}
}

// wrapMenu breaks a title into at most two lines that fit width pixels.
func wrapMenu(s string, width int, size float64) []string {
	if s == "" {
		return nil
	}
	if render.Measure(s, size, false) <= width {
		return []string{s}
	}
	words := strings.Fields(s)
	var lines []string
	cur := ""
	for _, w := range words {
		try := w
		if cur != "" {
			try = cur + " " + w
		}
		if render.Measure(try, size, false) <= width || cur == "" {
			cur = try
			continue
		}
		lines = append(lines, cur)
		cur = w
		if len(lines) == 2 {
			break
		}
	}
	if len(lines) < 2 && cur != "" {
		lines = append(lines, cur)
	}
	if len(lines) == 2 {
		lines[1] = render.Ellipsize(lines[1], size, false, width)
	}
	return lines
}

// menuOverlay decodes a rendered page, draws the menu over it and registers
// the result as a notice page, so the page cache is not overwritten.
func (s *Server) menuOverlay(page servedPage, profile string, p Profile, v menuView, t texts) (servedPage, error) {
	s.mu.Lock()
	data := s.hints[page.hash]
	if len(data) == 0 {
		if rp := s.rendered[profile+"/"+page.name]; rp != nil && rp.hash == page.hash {
			data = rp.data
		}
	}
	s.mu.Unlock()
	if len(data) == 0 {
		return page, fmt.Errorf("page %s is not available", page.hash)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return page, err
	}
	pal, ok := img.(*image.Paletted)
	if !ok {
		return page, errors.New("page is not paletted")
	}
	if pal.Rect.Dx() != p.Width || pal.Rect.Dy() != p.Height {
		return page, fmt.Errorf("page is %dx%d, display is %dx%d", pal.Rect.Dx(), pal.Rect.Dy(), p.Width, p.Height)
	}
	drawMenu(pal, p, v, t)
	out := s.notice(p.Kind, pal)
	out.name, out.page, out.pages, out.refresh = page.name, page.page, page.pages, page.refresh
	return out, nil
}

// overlayMenu draws the open menu over the served page. It resolves the
// plugin state of the actions when the menu opened and records the hash of
// the confirmation screen, which the next sync compares with the device's
// shown hash (ADR 0057).
func (s *Server) overlayMenu(cfg *Config, d *Device, served servedPage, m *menuState) servedPage {
	prof := cfg.Profiles[d.Profile]
	if !m.Resolved {
		ctx, cancel := context.WithTimeout(context.Background(), actionBudget)
		m.Actions = s.resolveActions(ctx, cfg, d, m.Actions, s.now())
		cancel()
		m.Resolved = true
		if m.Selected == "" && len(m.Actions) > 0 {
			m.Selected = m.Actions[0].Key
		}
	}
	t := cfg.texts()
	v := menuView{phase: m.Phase, actions: m.Actions, offset: m.Offset, sel: m.Selected}
	switch m.Phase {
	case menuEmptyPhase:
		v.title = t.menuEmpty()
	case menuConfirmPhase:
		if a := m.selected(); a != nil {
			v.title = a.Title
			if a.ConfirmText != "" {
				v.text = a.ConfirmText
			} else {
				_, v.text = t.menuConfirm("")
			}
		}
	case menuRunningPhase:
		if a := m.selected(); a != nil {
			v.title = a.Title
		}
		_, v.text = t.menuRunning("")
	case menuResultPhase:
		v.title, v.text = t.menuResult(m.ResultOK, m.Result)
	}
	out, err := s.menuOverlay(served, d.Profile, prof, v, t)
	if err != nil {
		slog.Warn("menu overlay failed", "device", d.ID, "error", err)
		return served
	}
	if m.Phase == menuConfirmPhase {
		m.ConfirmHash = out.hash
	}
	// The device reports this hash as "shown" until the menu is repainted. A
	// press that arrives with it belongs to the menu even when the deadline has
	// passed in the meantime (ADR 0078).
	m.Screen = out.hash
	return out
}
