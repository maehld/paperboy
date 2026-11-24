package paperboy

import (
	"image"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

// actionPage has one cell that offers the test plugin's mode action and one
// cell that does not, so a tap outside the action view has a target.
const actionPage = `kind: image
grid: [2, 1]
style: { padding: 0 }
cells:
  - { at: [0, 0], span: [1, 1], widget: act/loadpoint, args: { index: 0 }, action: mode }
  - { at: [1, 0], span: [1, 1], widget: paperboy/text, args: { text: side } }
`

// actionPlugin offers one mode action with three enum choices and records
// what the server asks and runs.
type actionPlugin struct {
	mode  string
	fail  bool
	calls []plugin.ActionCall
	runs  []string
}

func (p *actionPlugin) Describe() plugin.Description {
	return plugin.Description{
		Title:   "act",
		Widgets: []plugin.WidgetInfo{{Widget: "loadpoint", Kinds: []string{plugin.KindImage}}},
		Actions: []plugin.ActionSpec{{Name: "mode", Title: "Charge mode", Args: map[string]plugin.Arg{
			"index": {Type: "int", Required: true},
			"mode":  {Type: "string", Required: true, Enum: []any{"off", "pv", "now"}, EnumLabels: map[string]string{"off": "Off", "pv": "PV", "now": "Now"}},
		}}},
	}
}

func (p *actionPlugin) Configure(plugin.Values, *plugin.Request) error { return nil }

func (p *actionPlugin) Render(_ *plugin.Request, _ plugin.WidgetRequest, canvas *image.Paletted) error {
	if p.mode == "now" {
		render.FillSolid(canvas, canvas.Rect)
	}
	return nil
}

func (p *actionPlugin) Actions(req *plugin.Request) ([]plugin.ActionState, error) {
	out := make([]plugin.ActionState, len(req.Actions))
	for i, call := range req.Actions {
		p.calls = append(p.calls, call)
		mode := call.Arg().String("mode", "")
		out[i].Title = mode
		if mode == p.mode {
			out[i].Disabled = "already active"
		}
	}
	return out, nil
}

func (p *actionPlugin) Invoke(_ *plugin.Request, call plugin.ActionCall) (plugin.ActionOutcome, error) {
	mode := call.Arg().String("mode", "")
	p.runs = append(p.runs, mode)
	if p.fail {
		return plugin.ActionOutcome{OK: false, Message: "nope"}, nil
	}
	p.mode = mode
	return plugin.ActionOutcome{OK: true, Changed: true}, nil
}

// actionServer starts an instance with one action page and a device that has
// a digitiser when touch is true.
func actionServer(t *testing.T, pageYAML string, touch bool) (*Server, *testDevice, *actionPlugin) {
	return actionServerWith(t, pageYAML, touch, false)
}

func actionServerWith(t *testing.T, pageYAML string, touch, deny bool) (*Server, *testDevice, *actionPlugin) {
	t.Helper()
	dir := t.TempDir()
	writePageFile(t, filepath.Join(dir, "pages", "eins.yaml"), pageYAML)
	writePageFile(t, filepath.Join(dir, "plans", "zimmer.yaml"), "pages: [eins]\n")
	dev := Device{ID: "test", MAC: planDeviceMAC, Profile: "small", Plan: "zimmer", Refresh: Duration(10 * time.Minute)}
	if touch {
		on := true
		dev.Touch = &on
	}
	if deny {
		dev.Actions = actionsDeny
	}
	cfg := &Config{Dir: dir, Language: "en", Timezone: time.UTC,
		Profiles: map[string]Profile{"small": {Width: 400, Height: 300, PPI: 125, Levels: 4}},
		Devices:  []Device{dev}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.November, 24, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	p := &actionPlugin{mode: "off"}
	s.widgets.Builtin["act"] = p
	return s, planDevice(t, s), p
}

// actionStateOf returns the stored action state and its options.
func actionStateOf(t *testing.T, s *Server) (actionState, []actionOption) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state["AABBCCDDEE01"].Action
	return st, append([]actionOption(nil), st.Options...)
}

// TestActionModeRunsATap checks the whole touch path: short home shows the
// options, a tap runs one and the next answer is the normal page with the new
// state (ADR 0162).
func TestActionModeRunsATap(t *testing.T) {
	s, dev, p := actionServer(t, actionPage, true)
	base := displayPage(t, s, dev)
	mode := press(t, s, dev, buttonHome)
	if pagePath(mode) == pagePath(base) {
		t.Fatal("a short home must show the action view")
	}
	w, ok := mode["wake"].(map[string]any)
	if !ok || w["reason"] != "action" {
		t.Fatalf("the action view must ask for the deadline wake: %v", mode["wake"])
	}
	if mode["listen_s"].(float64)+w["after_s"].(float64) != float64(actionLifetime/time.Second) {
		t.Fatalf("listen and wake must cover the deadline: %v + %v", mode["listen_s"], w["after_s"])
	}
	st, opts := actionStateOf(t, s)
	if !st.Open || st.Screen != imageOf(mode)["hash"].(string) || len(opts) != 3 {
		t.Fatalf("action state: %+v", st)
	}
	if len(p.calls) != 3 {
		t.Fatalf("the plugin must see one call per choice: %v", p.calls)
	}
	for _, call := range p.calls {
		if call.Arg().Int("index", -1) != 0 || call.Arg().String("mode", "") == "" {
			t.Fatalf("the widget argument and the choice must reach the plugin: %+v", call)
		}
	}
	var tap actionOption
	for _, o := range opts {
		if o.Title == "now" {
			tap = o
		}
	}
	if tap.Title == "" {
		t.Fatalf("no now option: %+v", opts)
	}
	x, y := (tap.Rect[0]+tap.Rect[2])/2, (tap.Rect[1]+tap.Rect[3])/2
	dev.req.Reason = "touch"
	dev.req.Shown = st.Screen
	dev.req.Events = []syncEvent{{ID: 1, Type: "touch", X: x, Y: y}}
	out := displayPage(t, s, dev)
	if p.mode != "now" || len(p.runs) != 1 {
		t.Fatalf("the tap must run one action: mode %q runs %v", p.mode, p.runs)
	}
	if !strings.Contains(pagePath(out), "/pages/small/eins/") || pagePath(out) == pagePath(mode) {
		t.Fatalf("the answer after the tap must be the normal page: %q", pagePath(out))
	}
	if st, _ := actionStateOf(t, s); st.Open {
		t.Fatalf("the tap must close the mode: %+v", st)
	}
}

// TestActionTapGuardsTheShownHash checks that a tap on another screen runs
// nothing (ADR 0162).
func TestActionTapGuardsTheShownHash(t *testing.T) {
	s, dev, p := actionServer(t, actionPage, true)
	base := displayPage(t, s, dev)
	press(t, s, dev, buttonHome)
	_, opts := actionStateOf(t, s)
	x, y := (opts[1].Rect[0]+opts[1].Rect[2])/2, (opts[1].Rect[1]+opts[1].Rect[3])/2
	dev.req.Reason = "touch"
	dev.req.Shown = imageOf(base)["hash"].(string) // not the action view
	dev.req.Events = []syncEvent{{ID: 1, Type: "touch", X: x, Y: y}}
	displayPage(t, s, dev)
	if len(p.runs) != 0 {
		t.Fatalf("a stale tap ran %v", p.runs)
	}
	if open, _ := actionStateOf(t, s); open.Open {
		t.Fatal("a stale tap must leave the mode")
	}
}

// TestActionTapOutsideLeavesTheMode checks the plain page beside the action
// cell.
func TestActionTapOutsideLeavesTheMode(t *testing.T) {
	s, dev, p := actionServer(t, actionPage, true)
	displayPage(t, s, dev)
	press(t, s, dev, buttonHome)
	st, _ := actionStateOf(t, s)
	dev.req.Reason = "touch"
	dev.req.Shown = st.Screen
	dev.req.Events = []syncEvent{{ID: 1, Type: "touch", X: 300, Y: 150}}
	displayPage(t, s, dev)
	if len(p.runs) != 0 {
		t.Fatalf("a tap outside ran %v", p.runs)
	}
	if open, _ := actionStateOf(t, s); open.Open {
		t.Fatal("a tap outside must leave the mode")
	}
}

// TestActionDeadlineWakeClearsTheMode checks that the one-shot wake without
// input closes the mode and serves the plain page.
func TestActionDeadlineWakeClearsTheMode(t *testing.T) {
	s, dev, _ := actionServer(t, actionPage, true)
	base := displayPage(t, s, dev)
	press(t, s, dev, buttonHome)
	dev.req.Reason = "action"
	out := displayPage(t, s, dev)
	if pagePath(out) != pagePath(base) {
		t.Fatalf("the deadline must serve the plain page: %q vs %q", pagePath(out), pagePath(base))
	}
	if st, _ := actionStateOf(t, s); st.Open {
		t.Fatalf("the deadline must close the mode: %+v", st)
	}
}

// TestActionModeNeedsTouchAndAnAction checks the two switches: a device
// without a digitiser and a page without an action never open the mode and
// never keep the device listening (ADR 0162).
func TestActionModeNeedsTouchAndAnAction(t *testing.T) {
	plain := `kind: image
grid: [1, 1]
style: { padding: 0 }
cells:
  - { at: [0, 0], widget: act/loadpoint, args: { index: 0 } }
`
	s, dev, _ := actionServer(t, plain, true)
	base := displayPage(t, s, dev)
	out := press(t, s, dev, buttonHome)
	if pagePath(out) != pagePath(base) || out["listen_s"].(float64) != listenAfterInput.Seconds() {
		t.Fatalf("a page without actions must stay plain and keep the input window: %v", out)
	}
	if st, _ := actionStateOf(t, s); st.Open {
		t.Fatalf("a page without actions opened the mode: %+v", st)
	}

	s, dev, _ = actionServer(t, actionPage, false)
	base = displayPage(t, s, dev)
	out = press(t, s, dev, buttonHome)
	if pagePath(out) != pagePath(base) || out["listen_s"].(float64) != listenAfterInput.Seconds() {
		t.Fatalf("a device without a digitiser must stay plain and keep the input window: %v", out)
	}
	if st, _ := actionStateOf(t, s); st.Open {
		t.Fatalf("a device without a digitiser opened the mode: %+v", st)
	}
}

// TestActionModeDisabledAndUnknown checks that a device with actions: deny
// gets no mode, and that an unknown action stays a plain cell.
func TestActionModeDisabled(t *testing.T) {
	s, dev, _ := actionServerWith(t, actionPage, true, true)
	base := displayPage(t, s, dev)
	out := press(t, s, dev, buttonHome)
	if pagePath(out) != pagePath(base) || out["listen_s"].(float64) != listenAfterInput.Seconds() {
		t.Fatalf("actions: deny must keep the page plain and the input window: %v", out)
	}
}

// TestActionUnknownName keeps the cell and logs instead of breaking the page.
func TestActionUnknownName(t *testing.T) {
	page := strings.Replace(actionPage, "action: mode", "action: missing", 1)
	s, dev, _ := actionServer(t, page, true)
	base := displayPage(t, s, dev)
	mode := press(t, s, dev, buttonHome)
	st, opts := actionStateOf(t, s)
	if !st.Open || len(opts) != 1 || !strings.Contains(opts[0].Disabled, "unknown action") {
		t.Fatalf("unknown action: %+v", st)
	}
	if pagePath(mode) == pagePath(base) {
		t.Fatal("the mode should show the reason tile")
	}
}

// TestVirtualDisplayRunsNoAction checks that a tap in the browser shows and
// closes the mode but never calls the plugin (ADR 0091, 0162).
func TestVirtualDisplayRunsNoAction(t *testing.T) {
	dir := t.TempDir()
	writePageFile(t, filepath.Join(dir, "pages", "eins.yaml"), actionPage)
	writePageFile(t, filepath.Join(dir, "plans", "zimmer.yaml"), "pages: [eins]\n")
	cfg := &Config{Dir: dir, Language: "en", Timezone: time.UTC,
		Profiles: map[string]Profile{"small": {Width: 400, Height: 300, PPI: 125, Levels: 4}}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Date(2026, time.November, 24, 12, 0, 0, 0, time.UTC) }
	p := &actionPlugin{mode: "off"}
	s.widgets.Builtin["act"] = p
	query := "plan=zimmer&display=small"
	base := virtualSync(t, s, query, syncRequest{Reason: "timer"})
	mode := virtualPress(t, s, query, imageHash(base), 1, buttonHome)
	s.mu.Lock()
	opts := append([]actionOption(nil), s.virtual["zimmer/small"].Action.Options...)
	s.mu.Unlock()
	if len(opts) != 3 {
		t.Fatalf("virtual action options: %+v", opts)
	}
	var tap actionOption
	for _, o := range opts {
		if o.Title == "now" {
			tap = o
		}
	}
	x, y := (tap.Rect[0]+tap.Rect[2])/2, (tap.Rect[1]+tap.Rect[3])/2
	virtualSync(t, s, query, syncRequest{Reason: "touch", Shown: imageHash(mode),
		Events: []syncEvent{{ID: 1, Type: "touch", X: x, Y: y}}})
	if len(p.runs) != 0 {
		t.Fatalf("the virtual display ran %v", p.runs)
	}
}

// TestActionFailureShowsABanner checks that a failed call is visible on the
// device and that the next sync serves the plain page again (ADR 0162).
func TestActionFailureShowsABanner(t *testing.T) {
	s, dev, p := actionServer(t, actionPage, true)
	p.fail = true
	base := displayPage(t, s, dev)
	mode := press(t, s, dev, buttonHome)
	st, opts := actionStateOf(t, s)
	var tap actionOption
	for _, o := range opts {
		if o.Title == "now" {
			tap = o
		}
	}
	x, y := (tap.Rect[0]+tap.Rect[2])/2, (tap.Rect[1]+tap.Rect[3])/2
	dev.req.Reason = "touch"
	dev.req.Shown = st.Screen
	dev.req.Events = []syncEvent{{ID: 1, Type: "touch", X: x, Y: y}}
	out := displayPage(t, s, dev)
	if len(p.runs) != 1 {
		t.Fatalf("the failing tap must run once: %v", p.runs)
	}
	if got := pagePath(out); !strings.HasPrefix(got, "/hints/") || got == pagePath(base) || got == pagePath(mode) {
		t.Fatalf("the failure banner must be a hint image of its own: %q", got)
	}
	if st, _ := actionStateOf(t, s); st.Open {
		t.Fatalf("the failed action must close the mode: %+v", st)
	}
	dev.req.Reason = "timer"
	if got := pagePath(displayPage(t, s, dev)); got != pagePath(base) {
		t.Fatalf("the next sync must clear the banner: %q vs %q", got, pagePath(base))
	}
}
