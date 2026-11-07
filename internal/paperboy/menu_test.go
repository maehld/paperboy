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
	dir := t.TempDir()
	writePageFile(t, filepath.Join(dir, "pages", "eins.small.yaml"), textPage("eins"))
	writePageFile(t, filepath.Join(dir, "pages", "zwei.small.yaml"), textPage("zwei"))
	writePageFile(t, filepath.Join(dir, "plans", "zimmer.yaml"), `
rules:
  - name: immer
    pages: [eins, zwei]
`+actions)
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
	if out["refresh_s"].(float64) > 60 {
		t.Fatalf("the menu must schedule its deadline: %v", out["refresh_s"])
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

func TestMenuLongHomeClosesAndGoesToFirstPage(t *testing.T) {
	s, _, _ := menuServer(t, menuPlanActions, nil)
	dev := menuDevice(t, s)
	displayPage(t, s, dev)
	press(t, s, dev, buttonNext) // page zwei
	press(t, s, dev, buttonHome) // menu over zwei
	out := pressShown(t, s, dev, "", []int{800}, buttonHome)
	if st := s.state["test"]; st.Menu.Open {
		t.Fatalf("long home must close the menu: %+v", st.Menu)
	}
	if got := pagePath(out); !strings.Contains(got, "/eins/") {
		t.Fatalf("long home must go to the first page: %q", got)
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
