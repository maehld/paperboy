package paperboy

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// tapSeq gives every tap its own event ID; a repeated ID is deduplicated.
var tapSeq int64

// menuStateKey is the file the test device state lands in: its MAC without
// the colons (ADR 0080).
const menuStateKey = "AABBCCDDEE02"

// menuProfile is the display the menu tests run on: 400x300, so the overlay is
// the top half, 400x150.
var menuProfile = Profile{Width: 400, Height: 300, PPI: 125, Levels: 4}

// tapAt sends a touch at x/y; shown is the hash the device painted last.
func tapAt(t *testing.T, s *Server, dev *testDevice, x, y int, shown string) map[string]any {
	t.Helper()
	tapSeq++
	body := syncRequest{Reason: "touch", Shown: shown, Events: []syncEvent{{ID: tapSeq, Type: "touch", X: x, Y: y}}}
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

// tapCell taps the middle of the menu cell of one kind. The geometry comes from
// menuLayout, the same function the overlay draws from, so a layout change
// cannot leave the tests tapping the wrong place.
func tapCell(t *testing.T, s *Server, dev *testDevice, touch bool, kind string, index int) map[string]any {
	t.Helper()
	return tapCellShown(t, s, dev, touch, kind, index, "")
}

// tapCellShown is tapCell with the hash the device reports as painted.
func tapCellShown(t *testing.T, s *Server, dev *testDevice, touch bool, kind string, index int, shown string) map[string]any {
	t.Helper()
	m := s.state[menuStateKey].Menu
	actions, controls, _ := menuLayout(menuProfile, m.Phase, len(m.Actions), touch)
	for _, c := range append(actions, controls...) {
		if c.kind != kind || (kind == cellAction && c.index != index) {
			continue
		}
		return tapAt(t, s, dev, c.rect.Min.X+c.rect.Dx()/2, c.rect.Min.Y+c.rect.Dy()/2, shown)
	}
	t.Fatalf("no %s cell %d in phase %s", kind, index, m.Phase)
	return nil
}

// menuServerTouch is menuServer with the digitiser turned on, which is what
// makes Paperboy draw the control strip.
func menuServerTouch(t *testing.T, actions string) (*Server, *time.Time, *menuPlugin) {
	t.Helper()
	yes := true
	return menuServer(t, actions, func(d *Device) { d.Touch = &yes })
}

func TestMenuTapSelectsAndRuns(t *testing.T) {
	s, _, p := menuServer(t, menuPlanActions, nil)
	dev := menuDevice(t, s)
	displayPage(t, s, dev)
	press(t, s, dev, buttonHome)             // open
	tapCell(t, s, dev, false, cellAction, 0) // act/go needs a confirmation
	if st := s.state[menuStateKey]; st.Menu.Selected != "act/go" || st.Menu.Phase != menuConfirmPhase {
		t.Fatalf("state after tapping the confirming action: %+v", st.Menu)
	}
	if len(p.runs) != 0 {
		t.Fatalf("a confirmation was skipped: %v", p.runs)
	}
	// "Nein" returns to the list, then a tap on an action without a
	// confirmation runs it at once.
	tapCell(t, s, dev, false, cellNo, 0)
	tapCell(t, s, dev, false, cellAction, 1)
	if len(p.runs) != 1 || p.runs[0] != "quick" {
		t.Fatalf("runs: %v", p.runs)
	}
}

func TestMenuTapCloses(t *testing.T) {
	s, _, _ := menuServerTouch(t, menuPlanActions)
	dev := menuDevice(t, s)
	base := pagePath(displayPage(t, s, dev))
	press(t, s, dev, buttonHome)
	out := tapCell(t, s, dev, true, cellClose, 0)
	if st := s.state[menuStateKey]; st.Menu.Open {
		t.Fatalf("X must close the menu: %+v", st.Menu)
	}
	if pagePath(out) != base {
		t.Fatal("closing with X must keep the page")
	}
}

// A device without touch has no close box at all: a tap where it would be must
// not close the menu (ADR 0082).
func TestMenuTapCloseNeedsTouch(t *testing.T) {
	s, _, _ := menuServer(t, menuPlanActions, nil)
	dev := menuDevice(t, s)
	displayPage(t, s, dev)
	press(t, s, dev, buttonHome)
	tapAt(t, s, dev, 300, 130, "") // the middle of the strip a touch device would show
	if st := s.state[menuStateKey]; !st.Menu.Open {
		t.Fatalf("a tap on the empty strip closed the menu: %+v", st.Menu)
	}
}

func TestMenuTapOutsideIsIgnored(t *testing.T) {
	s, _, _ := menuServer(t, menuPlanActions, nil)
	dev := menuDevice(t, s)
	displayPage(t, s, dev)
	press(t, s, dev, buttonHome)
	tapAt(t, s, dev, 200, 250, "") // below the overlay
	if st := s.state[menuStateKey]; !st.Menu.Open || st.Menu.Selected != "act/go" {
		t.Fatalf("a tap below the overlay changed the menu: %+v", st.Menu)
	}
}

func TestMenuTapConfirmation(t *testing.T) {
	s, _, p := menuServer(t, menuPlanActions, nil)
	dev := menuDevice(t, s)
	base := imageOf(displayPage(t, s, dev))["hash"].(string)
	press(t, s, dev, buttonHome)             // open
	tapCell(t, s, dev, false, cellAction, 0) // select act/go: confirmation
	// A tap on "Ja" while the device has not painted the confirmation runs
	// nothing and leaves the screen in place.
	tapCell(t, s, dev, false, cellYes, 0)
	if st := s.state[menuStateKey]; st.Menu.Phase != menuConfirmPhase {
		t.Fatalf("menu state: %+v", st.Menu)
	}
	if s.state[menuStateKey].Menu.ConfirmHash == "" {
		t.Fatal("the confirmation screen has no hash")
	}
	if s.state[menuStateKey].Menu.find("act/go") == nil {
		t.Fatal("action missing")
	}
	tapCell(t, s, dev, false, cellNo, 0) // "Nein"
	if st := s.state[menuStateKey]; st.Menu.Phase != menuListPhase {
		t.Fatalf("Nein must return to the list: %+v", st.Menu)
	}
	// Confirming with a stale screen does nothing.
	press(t, s, dev, buttonHome)
	tapCellShown(t, s, dev, false, cellYes, 0, base) // "Ja" without the painted screen
	if len(p.runs) != 0 {
		t.Fatalf("an unpainted yes ran the action: %v", p.runs)
	}
}

func TestMenuScrollButtons(t *testing.T) {
	actions := `actions:
  - {action: act/a1, title: A1}
  - {action: act/a2, title: A2}
  - {action: act/a3, title: A3}
  - {action: act/a4, title: A4}
  - {action: act/a5, title: A5}
`
	s, _, _ := menuServerTouch(t, actions)
	dev := menuDevice(t, s)
	displayPage(t, s, dev)
	press(t, s, dev, buttonHome)
	m := s.state[menuStateKey].Menu
	if len(m.Actions) != 5 || m.Offset != 0 {
		t.Fatalf("menu: %+v", m)
	}
	tapCell(t, s, dev, true, cellNext, 0)
	if got := s.state[menuStateKey].Menu; got.Offset != 2 || got.Selected != "act/a3" {
		t.Fatalf("next page: %+v", got)
	}
	tapCell(t, s, dev, true, cellPrev, 0)
	if got := s.state[menuStateKey].Menu; got.Offset != 0 || got.Selected != "act/a1" {
		t.Fatalf("previous page: %+v", got)
	}
}

func TestMenuWakeAfterTap(t *testing.T) {
	s, _, _ := menuServer(t, menuPlanActions, nil)
	dev := menuDevice(t, s)
	displayPage(t, s, dev)
	press(t, s, dev, buttonHome)
	// Opening the confirmation keeps the menu alive, so the deadline wake is
	// scheduled; an action that runs closes it with a result instead.
	out := tapCell(t, s, dev, false, cellAction, 0)
	wake, ok := out["wake"].(map[string]any)
	if !ok || wake["reason"] != "menu" {
		t.Fatalf("tap wake: %v", out["wake"])
	}
}
