package paperboy

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

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

// The menu of the test server covers 400x300, so the top half is 400x150 and
// the six cells are 66 pixels wide.
// tapSeq gives every tap its own event ID; a repeated ID is deduplicated.
var tapSeq int64

// Cell centres for the two layouts the tests use: four cells of 100 px for
// three actions, six cells of 66 px when the list scrolls.
const (
	menuCellCloseX = 50
	menuCellA1X    = 150
	menuCellA2X    = 250
	menuCellPrevX  = 297
	menuCellNextX  = 363
	confirmYesX    = 100
	confirmNoX     = 333
)

func TestMenuTapSelectsAndRuns(t *testing.T) {
	s, _, p := menuServer(t, menuPlanActions, nil)
	dev := menuDevice(t, s)
	displayPage(t, s, dev)
	press(t, s, dev, buttonHome)          // open
	tapAt(t, s, dev, menuCellA1X, 75, "") // act/go needs a confirmation
	if st := s.state["AABBCCDDEE02"]; st.Menu.Selected != "act/go" || st.Menu.Phase != menuConfirmPhase {
		t.Fatalf("state after tapping the confirming action: %+v", st.Menu)
	}
	if len(p.runs) != 0 {
		t.Fatalf("a confirmation was skipped: %v", p.runs)
	}
	// "Nein" returns to the list, then a tap on an action without a
	// confirmation runs it at once.
	tapAt(t, s, dev, confirmNoX, 110, "")
	tapAt(t, s, dev, menuCellA2X, 75, "")
	if len(p.runs) != 1 || p.runs[0] != "quick" {
		t.Fatalf("runs: %v", p.runs)
	}
}

func TestMenuTapCloses(t *testing.T) {
	s, _, _ := menuServer(t, menuPlanActions, nil)
	dev := menuDevice(t, s)
	base := pagePath(displayPage(t, s, dev))
	press(t, s, dev, buttonHome)
	out := tapAt(t, s, dev, menuCellCloseX, 75, "")
	if st := s.state["AABBCCDDEE02"]; st.Menu.Open {
		t.Fatalf("X must close the menu: %+v", st.Menu)
	}
	if pagePath(out) != base {
		t.Fatal("closing with X must keep the page")
	}
}

func TestMenuTapOutsideIsIgnored(t *testing.T) {
	s, _, _ := menuServer(t, menuPlanActions, nil)
	dev := menuDevice(t, s)
	displayPage(t, s, dev)
	press(t, s, dev, buttonHome)
	tapAt(t, s, dev, 200, 250, "") // below the overlay
	if st := s.state["AABBCCDDEE02"]; !st.Menu.Open || st.Menu.Selected != "act/go" {
		t.Fatalf("a tap below the overlay changed the menu: %+v", st.Menu)
	}
}

func TestMenuTapConfirmation(t *testing.T) {
	s, _, p := menuServer(t, menuPlanActions, nil)
	dev := menuDevice(t, s)
	base := imageOf(displayPage(t, s, dev))["hash"].(string)
	press(t, s, dev, buttonHome)                 // open
	tapAt(t, s, dev, menuCellA1X, 75, "")        // select act/go: confirmation
	out := tapAt(t, s, dev, menuCellA1X, 75, "") // tap again on the action cell
	_ = out
	if st := s.state["AABBCCDDEE02"]; st.Menu.Phase != menuConfirmPhase {
		t.Fatalf("menu state: %+v", st.Menu)
	}
	confirm := s.state["AABBCCDDEE02"].Menu.ConfirmHash
	// The "Nein" cell is the right third of the confirm screen.
	if s.state["AABBCCDDEE02"].Menu.find("act/go") == nil {
		t.Fatal("action missing")
	}
	tapAt(t, s, dev, 330, 110, confirm) // "Nein"
	if st := s.state["AABBCCDDEE02"]; st.Menu.Phase != menuListPhase {
		t.Fatalf("Nein must return to the list: %+v", st.Menu)
	}
	// Confirming with a stale screen does nothing.
	press(t, s, dev, buttonHome)
	tapAt(t, s, dev, 130, 110, base) // "Ja" without the painted screen
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
	s, _, _ := menuServer(t, actions, nil)
	dev := menuDevice(t, s)
	displayPage(t, s, dev)
	press(t, s, dev, buttonHome)
	m := s.state["AABBCCDDEE02"].Menu
	if len(m.Actions) != 5 || m.Offset != 0 {
		t.Fatalf("menu: %+v", m)
	}
	tapAt(t, s, dev, menuCellNextX, 75, "")
	if got := s.state["AABBCCDDEE02"].Menu; got.Offset != 2 || got.Selected != "act/a3" {
		t.Fatalf("next page: %+v", got)
	}
	tapAt(t, s, dev, menuCellPrevX, 75, "")
	if got := s.state["AABBCCDDEE02"].Menu; got.Offset != 0 || got.Selected != "act/a1" {
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
	out := tapAt(t, s, dev, menuCellA1X, 75, "")
	wake, ok := out["wake"].(map[string]any)
	if !ok || wake["reason"] != "menu" {
		t.Fatalf("tap wake: %v", out["wake"])
	}
	_ = time.Second
}
