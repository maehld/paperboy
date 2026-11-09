package paperboy

import (
	"encoding/json"
	"image"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

const menuDeviceMAC = "AA:BB:CC:DD:EE:02"

// menuPlugin is an in-process plugin with three actions: one that needs a
// confirmation, one that runs at once and one that is unavailable.
type menuPlugin struct {
	runs    []string
	changed bool
}

func (p *menuPlugin) Describe() plugin.Description {
	return plugin.Description{Title: "menu", Actions: []plugin.ActionSpec{
		{Name: "go", Title: "Go", Confirm: &plugin.Confirm{Required: true}},
		{Name: "quick", Title: "Quick"},
		{Name: "off", Title: "Off"},
	}}
}

func (p *menuPlugin) Configure(plugin.Values, *plugin.Request) error { return nil }

func (p *menuPlugin) Render(*plugin.Request, plugin.WidgetRequest, *image.Paletted) error {
	return nil
}

func (p *menuPlugin) Actions(req *plugin.Request) ([]plugin.ActionState, error) {
	out := make([]plugin.ActionState, len(req.Actions))
	for i, c := range req.Actions {
		out[i].Title = "State " + c.Action
		if c.Action == "off" {
			out[i].Disabled = "not now"
		}
	}
	return out, nil
}

func (p *menuPlugin) Invoke(_ *plugin.Request, c plugin.ActionCall) (plugin.ActionOutcome, error) {
	p.runs = append(p.runs, c.Action)
	return plugin.ActionOutcome{OK: true, Message: "done", Changed: p.changed}, nil
}

const menuPlanActions = `actions:
  - action: act/go
  - action: act/quick
  - action: act/off
`

// menuServer starts a server with a two-page plan and the menu actions above.
func menuServer(t *testing.T, actions string, tweak func(*Device)) (*Server, *time.Time, *menuPlugin) {
	t.Helper()
	return menuServerRules(t, "rules:\n  - name: immer\n    pages: [eins, zwei]\n", actions, tweak)
}

// menuServerRules is menuServer with a plan of the caller's rules.
func menuServerRules(t *testing.T, rules, actions string, tweak func(*Device)) (*Server, *time.Time, *menuPlugin) {
	t.Helper()
	dir := t.TempDir()
	writePageFile(t, filepath.Join(dir, "pages", "eins.small.yaml"), textPage("eins"))
	writePageFile(t, filepath.Join(dir, "pages", "zwei.small.yaml"), textPage("zwei"))
	writePageFile(t, filepath.Join(dir, "plans", "zimmer.yaml"), rules+actions)
	d := Device{ID: "test", MAC: menuDeviceMAC, Profile: "small", Plan: "zimmer",
		Refresh: Duration(10 * time.Minute)}
	if tweak != nil {
		tweak(&d)
	}
	cfg := &Config{Dir: dir, Language: "de", Timezone: time.UTC,
		Profiles: map[string]Profile{"small": {Width: 400, Height: 300, PPI: 125, Gray: true}},
		Devices:  []Device{d}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	p := &menuPlugin{}
	s.widgets.Builtin["act"] = p
	now := time.Date(2026, time.November, 7, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	return s, &now, p
}

func menuDevice(t *testing.T, s *Server) *testDevice {
	t.Helper()
	rec := register(t, s, `{"mac":"`+menuDeviceMAC+`","hardware":"sticky","firmware":"0.5.0"}`)
	var out struct{ Key, Device string }
	if err := json.Unmarshal(rec.Body.Bytes(), &out); rec.Code != http.StatusOK || err != nil || out.Key == "" {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	return &testDevice{key: out.Key, req: syncRequest{Firmware: "0.5.0", Hardware: "sticky", Reason: "timer"}}
}

// pressShown sends a button wake that reports the given shown hash, as a
// device does that painted an older image.
func pressShown(t *testing.T, s *Server, dev *testDevice, shown string, holds []int, roles ...string) map[string]any {
	t.Helper()
	body := syncRequest{Reason: "button", Shown: shown, Events: []syncEvent{}}
	for i, r := range roles {
		h := 0
		if i < len(holds) {
			h = holds[i]
		}
		body.Events = append(body.Events, syncEvent{Type: "button", Button: r, HoldMS: h})
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	rec := syncRaw(t, s, dev.key, string(data))
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); rec.Code != http.StatusOK || err != nil {
		t.Fatalf("sync: %d %s", rec.Code, rec.Body)
	}
	dev.req.Shown = imageOf(out)["hash"].(string)
	return out
}

func TestMenuOpensOnHomeWithoutRunning(t *testing.T) {
	s, _, p := menuServer(t, menuPlanActions, nil)
	dev := menuDevice(t, s)
	base := pagePath(displayPage(t, s, dev))
	out := press(t, s, dev, buttonHome)
	if pagePath(out) == base {
		t.Fatal("the menu must change the image")
	}
	if len(p.runs) != 0 {
		t.Fatalf("opening the menu ran %v", p.runs)
	}
	if st := s.state["test"]; !st.Menu.Open || st.Menu.Phase != menuListPhase {
		t.Fatalf("menu state: %+v", st.Menu)
	}
	wake, ok := out["wake"].(map[string]any)
	if !ok || wake["reason"] != "menu" || wake["after_s"].(float64) > 30 {
		t.Fatalf("the menu must schedule its deadline: %v", out["wake"])
	}
	if out["refresh_s"].(float64) != 600 {
		t.Fatalf("the regular refresh must stay untouched: %v", out["refresh_s"])
	}
	// The page cursor did not move.
	if st := s.state["test"]; st.Page != 0 {
		t.Fatalf("page moved: %d", st.Page)
	}
}

func TestMenuActionWithoutConfirmation(t *testing.T) {
	s, _, p := menuServer(t, menuPlanActions, nil)
	dev := menuDevice(t, s)
	base := pagePath(displayPage(t, s, dev))
	press(t, s, dev, buttonHome)        // open
	press(t, s, dev, buttonNext)        // select act/quick
	out := press(t, s, dev, buttonHome) // run it
	if len(p.runs) != 1 || p.runs[0] != "quick" {
		t.Fatalf("runs: %v", p.runs)
	}
	if st := s.state["test"]; st.Menu.Phase != menuResultPhase || !st.Menu.ResultOK {
		t.Fatalf("menu state: %+v", st.Menu)
	}
	if pagePath(out) == base {
		t.Fatal("the result overlay must be visible")
	}
	// The result goes away with the next regular sync.
	displayPage(t, s, dev)
	if st := s.state["test"]; st.Menu.Open {
		t.Fatalf("result must close on the next sync: %+v", st.Menu)
	}
	if got := pagePath(displayPage(t, s, dev)); got != base {
		t.Fatalf("page after the menu: %q, want %q", got, base)
	}
}

func TestMenuConfirmationNeedsPaintedScreen(t *testing.T) {
	s, _, p := menuServer(t, menuPlanActions, nil)
	dev := menuDevice(t, s)
	base := imageOf(displayPage(t, s, dev))["hash"].(string)
	press(t, s, dev, buttonHome)        // open
	out := press(t, s, dev, buttonHome) // select act/go: confirmation
	if len(p.runs) != 0 {
		t.Fatalf("an action ran without its confirmation: %v", p.runs)
	}
	if st := s.state["test"]; st.Menu.Phase != menuConfirmPhase || st.Menu.ConfirmHash == "" {
		t.Fatalf("menu state: %+v", st.Menu)
	}
	confirm := imageOf(out)["hash"].(string)
	if confirm != s.state["test"].Menu.ConfirmHash {
		t.Fatalf("confirmation hash: %q vs %q", confirm, s.state["test"].Menu.ConfirmHash)
	}
	// A home press from a device that never painted the confirmation is not a
	// yes: the screen is served again.
	pressShown(t, s, dev, base, nil, buttonHome)
	if len(p.runs) != 0 {
		t.Fatalf("unpainted confirmation ran the action: %v", p.runs)
	}
	if st := s.state["test"]; st.Menu.Phase != menuConfirmPhase {
		t.Fatalf("phase after a stale press: %q", st.Menu.Phase)
	}
	// Painted confirmation plus home is a yes.
	pressShown(t, s, dev, confirm, nil, buttonHome)
	if len(p.runs) != 1 || p.runs[0] != "go" {
		t.Fatalf("runs: %v", p.runs)
	}
}

func TestMenuSkipsDisabledAction(t *testing.T) {
	s, _, p := menuServer(t, menuPlanActions, nil)
	dev := menuDevice(t, s)
	displayPage(t, s, dev)
	press(t, s, dev, buttonHome)
	press(t, s, dev, buttonNext, buttonNext) // act/off, unavailable
	press(t, s, dev, buttonHome)
	if len(p.runs) != 0 {
		t.Fatalf("a disabled action ran: %v", p.runs)
	}
	if st := s.state["test"]; st.Menu.Phase != menuListPhase {
		t.Fatalf("menu state: %+v", st.Menu)
	}
}

func TestMenuPlanTitleWins(t *testing.T) {
	s, _, _ := menuServer(t, `actions:
  - action: act/quick
    title: "Schnell laden"
  - action: act/go
`, nil)
	dev := menuDevice(t, s)
	displayPage(t, s, dev)
	press(t, s, dev, buttonHome)
	acts := s.state["test"].Menu.Actions
	if len(acts) != 2 {
		t.Fatalf("actions: %+v", acts)
	}
	if acts[0].Title != "Schnell laden" {
		t.Errorf("the plan title must win: %q", acts[0].Title)
	}
	if acts[1].Title != "State go" {
		t.Errorf("the plugin's state must fill in without a plan title: %q", acts[1].Title)
	}
}

func TestMenuLongHomeClosesAndKeepsThePage(t *testing.T) {
	s, _, _ := menuServer(t, menuPlanActions, nil)
	dev := menuDevice(t, s)
	displayPage(t, s, dev)
	press(t, s, dev, buttonNext) // page zwei
	press(t, s, dev, buttonHome) // menu over zwei
	out := pressShown(t, s, dev, "", []int{800}, buttonHome)
	if st := s.state["test"]; st.Menu.Open {
		t.Fatalf("long home must close the menu: %+v", st.Menu)
	}
	if got := pagePath(out); !strings.Contains(got, "/zwei/") {
		t.Fatalf("long home must keep the page: %q", got)
	}
}

// TestMenuOpensWithoutActions covers the empty menu: a short home opens it
// even when the plan offers no actions, and any input dismisses it (ADR 0067).
func TestMenuOpensWithoutActions(t *testing.T) {
	s, _, _ := menuServer(t, "", nil)
	dev := menuDevice(t, s)
	base := pagePath(displayPage(t, s, dev))
	out := press(t, s, dev, buttonHome)
	if pagePath(out) == base {
		t.Fatal("the empty menu must show a banner")
	}
	st := s.state["test"]
	if !st.Menu.Open || st.Menu.Phase != menuEmptyPhase {
		t.Fatalf("menu state: %+v", st.Menu)
	}
	wake, ok := out["wake"].(map[string]any)
	if !ok || wake["reason"] != "menu" || wake["after_s"].(float64) > 10 {
		t.Fatalf("the empty menu must close after at most ten seconds: %v", out["wake"])
	}
	// Any input dismisses the banner and leaves the page unchanged.
	out = press(t, s, dev, buttonNext)
	if got := pagePath(out); got != base {
		t.Fatalf("dismissing the banner paged: %q, want %q", got, base)
	}
	if st := s.state["test"]; st.Menu.Open {
		t.Fatalf("input must dismiss the empty menu: %+v", st.Menu)
	}
}

// TestMenuLongNextGoesToFirstPage checks the new home of "go to start": a long
// next or previous with the menu closed, while a short press keeps paging.
func TestMenuLongNextGoesToFirstPage(t *testing.T) {
	s, _, _ := menuServer(t, menuPlanActions, nil)
	dev := menuDevice(t, s)
	displayPage(t, s, dev)
	press(t, s, dev, buttonNext) // page zwei
	if got := pagePath(pressShown(t, s, dev, "", []int{800}, buttonNext)); !strings.Contains(got, "/eins/") {
		t.Fatalf("long next must go to the first page: %q", got)
	}
	if st := s.state["test"]; st.Menu.Open {
		t.Fatalf("a long next must not open the menu: %+v", st.Menu)
	}
	if got := pagePath(press(t, s, dev, buttonNext)); !strings.Contains(got, "/zwei/") {
		t.Fatalf("a short next must page: %q", got)
	}
	if got := pagePath(pressShown(t, s, dev, "", []int{800}, buttonPrevious)); !strings.Contains(got, "/eins/") {
		t.Fatalf("long previous must go to the first page: %q", got)
	}
}

// TestMenuLongPressInsideTheMenuIsIgnored keeps the direction buttons simple
// while the menu is open (ADR 0067).
func TestMenuLongPressInsideTheMenuIsIgnored(t *testing.T) {
	s, _, _ := menuServer(t, menuPlanActions, nil)
	dev := menuDevice(t, s)
	displayPage(t, s, dev)
	press(t, s, dev, buttonHome) // opens on act/go
	before := s.state["test"].Menu.Selected
	out := pressShown(t, s, dev, "", []int{800}, buttonPrevious)
	st := s.state["test"]
	if !st.Menu.Open || st.Menu.Selected != before {
		t.Fatalf("a long press must not move the selection: %+v", st.Menu)
	}
	if st.Page != 0 {
		t.Fatalf("a long press in the menu must not page: %v", st.Page)
	}
	// The menu is still shown over the first page.
	if got := pagePath(out); !strings.Contains(got, "/hints/") {
		t.Fatalf("the menu overlay must stay served: %q", got)
	}
}

func TestMenuHoldThreshold(t *testing.T) {
	if held(0) || held(699) {
		t.Error("a press below 700 ms is short")
	}
	if !held(700) || !held(1500) {
		t.Error("a press of 700 ms and more is long")
	}
}

// TestMenuTapDismissesEmptyBanner closes the empty menu on the Sticky, which
// has no long-press-free exit beside the expiry.
func TestMenuTapDismissesEmptyBanner(t *testing.T) {
	s, _, _ := menuServer(t, "", nil)
	dev := menuDevice(t, s)
	base := pagePath(displayPage(t, s, dev))
	press(t, s, dev, buttonHome)
	out := tapAt(t, s, dev, 200, 20, "")
	if st := s.state["test"]; st.Menu.Open {
		t.Fatalf("a tap must dismiss the empty menu: %+v", st.Menu)
	}
	if got := pagePath(out); got != base {
		t.Fatalf("dismissing the banner paged: %q, want %q", got, base)
	}
}

func TestMenuExpires(t *testing.T) {
	s, now, _ := menuServer(t, menuPlanActions, nil)
	dev := menuDevice(t, s)
	base := pagePath(displayPage(t, s, dev))
	press(t, s, dev, buttonHome)
	*now = now.Add(31 * time.Second)
	out := displayPage(t, s, dev)
	if st := s.state["test"]; st.Menu.Open {
		t.Fatalf("the menu must expire: %+v", st.Menu)
	}
	if got := pagePath(out); got != base {
		t.Fatalf("page after expiry: %q, want %q", got, base)
	}
}

// TestMenuNavigationKeepsTheMenuOpen covers the reported bug: on a battery
// device every press costs a whole wake cycle, so navigating a menu easily
// takes longer than the show duration. The deadline counts from the last input,
// not from opening, so a press never finds an expired menu and pages instead of
// moving the selection (ADR 0078).
func TestMenuNavigationKeepsTheMenuOpen(t *testing.T) {
	s, now, _ := menuServer(t, menuPlanActions, nil)
	dev := menuDevice(t, s)
	base := pagePath(displayPage(t, s, dev))
	press(t, s, dev, buttonHome) // opens on act/go
	for i := 0; i < 2; i++ {
		// Each step is beyond the whole show duration in total but well inside
		// it since the previous press.
		*now = now.Add(20 * time.Second)
		out := press(t, s, dev, buttonNext)
		st := s.state["test"]
		if !st.Menu.Open || st.Menu.Phase != menuListPhase {
			t.Fatalf("press %d closed the menu: %+v", i+1, st.Menu)
		}
		if st.Page != 0 {
			t.Fatalf("press %d paged: %d", i+1, st.Page)
		}
		if got := pagePath(out); got == base {
			t.Fatalf("press %d did not show the menu: %q", i+1, got)
		}
	}
	if st := s.state["test"]; st.Menu.Selected != "act/off" {
		t.Fatalf("two next presses must select the last action: %q", st.Menu.Selected)
	}
	// An idle menu still expires and keeps the page.
	*now = now.Add(31 * time.Second)
	displayPage(t, s, dev)
	if st := s.state["test"]; st.Menu.Open || st.Page != 0 {
		t.Fatalf("an idle menu must expire: %+v", st)
	}
}

// TestMenuPressWhileThePanelShowsIt mirrors the live capture of #129: four
// presses 6 s apart move the selection, and the fifth arrives after the
// deadline. The device still reports the menu screen it painted, so the press
// belongs to the menu instead of finding it expired (ADR 0078).
func TestMenuPressWhileThePanelShowsIt(t *testing.T) {
	s, now, _ := menuServer(t, menuPlanActions, nil)
	dev := menuDevice(t, s)
	base := pagePath(displayPage(t, s, dev))
	press(t, s, dev, buttonHome)
	for i := 0; i < 4; i++ {
		*now = now.Add(6 * time.Second)
		if out := press(t, s, dev, buttonNext); pagePath(out) == base {
			t.Fatalf("press %d did not show the menu", i+1)
		}
	}
	// Four presses at T+6, +12, +18 and +24 s, then a pause: this press at
	// T+55 arrives 31 s after the last input, so the deadline has passed. The
	// panel cannot have repainted yet, so the press must still navigate.
	*now = now.Add(31 * time.Second)
	out := press(t, s, dev, buttonNext)
	st := s.state["test"]
	if !st.Menu.Open || st.Menu.Phase != menuListPhase {
		t.Fatalf("the press on a visible menu closed it: %+v", st.Menu)
	}
	if st.Page != 0 {
		t.Fatalf("the press on a visible menu paged: %d", st.Page)
	}
	if st.Menu.Selected != "act/off" {
		t.Fatalf("the fifth press must select the last action: %q", st.Menu.Selected)
	}
	if got := pagePath(out); got == base {
		t.Fatal("the menu overlay must stay served")
	}
	// Without input the same menu still expires at its deadline and keeps the
	// page, even though the panel still reports the menu screen.
	*now = now.Add(31 * time.Second)
	displayPage(t, s, dev)
	if st := s.state["test"]; st.Menu.Open || st.Page != 0 {
		t.Fatalf("an idle menu must expire: %+v", st)
	}
}

// TestMenuWakeFitsTheListenWindow keeps the panel and the menu state in step:
// the firmware listens before it sleeps, so listen_s and the one-shot wake have
// to share the remaining menu time (ADR 0078).
func TestMenuWakeFitsTheListenWindow(t *testing.T) {
	s, _, _ := menuServer(t, menuPlanActions, nil)
	dev := menuDevice(t, s)
	displayPage(t, s, dev)
	out := press(t, s, dev, buttonHome)
	listen, after, reason := listenAndWake(t, out)
	if reason != "menu" || listen+after != 30 {
		t.Fatalf("listen %v s + wake %v s (%s) must cover the show duration", listen, after, reason)
	}
	if listen > listenAfterInput.Seconds() || after < 1 {
		t.Fatalf("listen %v s, wake %v s", listen, after)
	}
	// The empty banner has a shorter deadline and stays inside it as well.
	s, _, _ = menuServer(t, "", nil)
	dev = menuDevice(t, s)
	displayPage(t, s, dev)
	if listen, after, _ = listenAndWake(t, press(t, s, dev, buttonHome)); listen+after != 10 {
		t.Fatalf("empty banner: listen %v s + wake %v s", listen, after)
	}
}

// TestFitWakeToListen pins the split itself: the listen window and the wake
// always add up to the remaining menu time, and the wake keeps at least a second
// so the device really sleeps for it (ADR 0078).
func TestFitWakeToListen(t *testing.T) {
	for _, c := range []struct{ listen, after int }{
		{20, 30}, // the show duration right after opening
		{20, 25}, // less left than the default listen window
		{20, 2},  // the deadline is almost here
		{20, 1},
		{0, 30}, // a timer wake listens anyway
	} {
		listen, wake := fitWakeToListen(time.Duration(c.listen)*time.Second,
			&syncWake{Reason: "menu", AfterS: c.after})
		want := c.after - c.listen
		if want < 1 {
			want = 1
		}
		if int(listen.Seconds())+wake.AfterS != c.after || wake.AfterS != want {
			t.Errorf("listen %d s, wake %d s -> listen %v s, wake %d s",
				c.listen, c.after, listen, wake.AfterS)
		}
		if wake == nil || wake.Reason != "menu" {
			t.Errorf("listen %d s, wake %d s: the wake lost its reason", c.listen, c.after)
		}
	}
}

// listenAndWake reads the listen window and the one-shot wake of a response.
func listenAndWake(t *testing.T, out map[string]any) (listen, after float64, reason string) {
	t.Helper()
	wake, ok := out["wake"].(map[string]any)
	if !ok {
		t.Fatalf("no wake scheduled: %v", out)
	}
	listen, _ = out["listen_s"].(float64)
	after, _ = wake["after_s"].(float64)
	reason, _ = wake["reason"].(string)
	return listen, after, reason
}

// TestMenuTapExtendsTheDeadline is the touch half of ADR 0078: a tap inside the
// overlay keeps the menu open just like a button.
func TestMenuTapExtendsTheDeadline(t *testing.T) {
	s, now, _ := menuServer(t, menuPlanActions, nil)
	dev := menuDevice(t, s)
	displayPage(t, s, dev)
	press(t, s, dev, buttonHome)
	*now = now.Add(25 * time.Second)
	// act/go needs a confirmation and stays in the menu.
	out := tapAt(t, s, dev, menuCellA1X, 75, "")
	st := s.state["test"]
	if !st.Menu.Open || st.Menu.Phase != menuConfirmPhase {
		t.Fatalf("the tap closed the menu: %+v", st.Menu)
	}
	if listen, after, _ := listenAndWake(t, out); listen+after != 30 {
		t.Fatalf("a tap must restart the deadline: listen %v s + wake %v s", listen, after)
	}
}

// TestMenuExpiryKeepsThePageWhileRotating covers the same with `rotate` in the
// plan, where every sync would otherwise advance the page: the wake at the
// menu deadline is not input and must keep the page the operator was looking
// at (ADR 0057).
func TestMenuExpiryKeepsThePageWhileRotating(t *testing.T) {
	s, now, _ := menuServerRules(t,
		"rules:\n  - name: immer\n    pages: [eins, zwei]\n    rotate: true\n", menuPlanActions, nil)
	dev := menuDevice(t, s)
	base := pagePath(displayPage(t, s, dev))
	press(t, s, dev, buttonHome)
	*now = now.Add(31 * time.Second)
	dev.req.Reason = "menu"
	out := displayPage(t, s, dev)
	if st := s.state["test"]; st.Menu.Open {
		t.Fatalf("the menu must expire: %+v", st.Menu)
	}
	if got := pagePath(out); got != base {
		t.Fatalf("the expiry wake rotated the page: %q, want %q", got, base)
	}
	// The next regular sync rotates again.
	dev.req.Reason = "timer"
	if got := pagePath(displayPage(t, s, dev)); got == base {
		t.Fatalf("rotation stopped after the menu: %q", got)
	}
}

func TestMenuDeniedByDevice(t *testing.T) {
	s, _, _ := menuServer(t, menuPlanActions, func(d *Device) { d.Actions = actionsDeny })
	dev := menuDevice(t, s)
	base := pagePath(displayPage(t, s, dev))
	out := press(t, s, dev, buttonHome)
	if st := s.state["test"]; st.Menu.Open {
		t.Fatalf("deny must not open the menu: %+v", st.Menu)
	}
	if got := pagePath(out); got != base {
		t.Fatalf("home must keep its old meaning: %q, want %q", got, base)
	}
}

func TestMenuClosesWhenSelectionLeavesThePlan(t *testing.T) {
	m := menuState{Open: true, Phase: menuListPhase, Selected: "gone"}
	m.step(menuInput{buttons: []string{buttonNext}}, nil, time.Now())
	if m.Open {
		t.Fatalf("menu with a vanished action must close: %+v", m)
	}
}

func TestMenuLayout(t *testing.T) {
	land := Profile{Width: 800, Height: 480}
	cells, _, overlay := menuCells(land, []string{cellClose, cellAction, cellAction, cellAction}, false)
	if overlay != image.Rect(0, 0, 800, 240) || len(cells) != 4 {
		t.Fatalf("landscape: %v %v", overlay, cells)
	}
	if cells[0].rect.Dx() != 200 || cells[3].rect.Max.X != 800 {
		t.Fatalf("landscape cells: %+v", cells)
	}
	port := Profile{Width: 480, Height: 800}
	cells, _, overlay = menuCells(port, []string{cellClose, cellAction}, false)
	if overlay != image.Rect(0, 0, 480, 400) {
		t.Fatalf("portrait overlay: %v", overlay)
	}
	if cells[1].rect.Min.Y != 200 || cells[1].rect.Max.Y != 400 {
		t.Fatalf("portrait cells: %+v", cells)
	}
}
