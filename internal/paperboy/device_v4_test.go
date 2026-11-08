package paperboy

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// sendEvents posts a sync body with the given events and reason.
func sendEvents(t *testing.T, s *Server, dev *testDevice, reason string, events ...syncEvent) map[string]any {
	t.Helper()
	body := syncRequest{Reason: reason, Shown: dev.req.Shown, Events: events}
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

func TestSyncEventIDsAreDeduplicated(t *testing.T) {
	s, _ := planServer(t, testPlan)
	dev := planDevice(t, s)
	out := displayPage(t, s, dev)
	if !strings.Contains(pagePath(out), "/eins/") {
		t.Fatalf("first page: %q", pagePath(out))
	}
	next := syncEvent{ID: 1, Type: "button", Button: buttonNext}
	// A retried sync carries the same ID and must not page again.
	sendEvents(t, s, dev, "button", next)
	if st := s.state["test"]; st.Page != 1 || st.LastEventID != 1 {
		t.Fatalf("first press: %+v", st)
	}
	sendEvents(t, s, dev, "button", next)
	if st := s.state["test"]; st.Page != 1 {
		t.Fatalf("a repeated event moved the page: %+v", st)
	}
	// A retry may also bundle the event with the next one.
	out = sendEvents(t, s, dev, "button", next, syncEvent{ID: 2, Type: "button", Button: buttonNext})
	if st := s.state["test"]; st.Page != 0 || st.LastEventID != 2 {
		t.Fatalf("second press: %+v", st)
	}
	if out["wake"] != nil {
		t.Fatalf("no menu open, no wake: %v", out["wake"])
	}
	// An event without an ID is applied as before.
	sendEvents(t, s, dev, "button", syncEvent{Type: "button", Button: buttonNext})
	if st := s.state["test"]; st.Page != 1 {
		t.Fatalf("legacy event: %+v", st)
	}
	// A power cycle restarts the firmware's counter, so the server forgets the
	// last ID and accepts a small one again.
	sendEvents(t, s, dev, "powercycle", syncEvent{ID: 1, Type: "button", Button: buttonNext})
	if st := s.state["test"]; st.Page != 0 || st.LastEventID != 1 {
		t.Fatalf("after a power cycle: %+v", st)
	}
}

func TestSyncAcceptsMenuAndResultReasons(t *testing.T) {
	for _, reason := range []string{"menu", "result"} {
		if err := (&syncRequest{Reason: reason}).validate(); err != nil {
			t.Fatalf("%s: %v", reason, err)
		}
	}
	if err := (&syncRequest{Reason: "wakeup"}).validate(); err == nil {
		t.Fatal("unknown reason accepted")
	}
	if err := (&syncRequest{Reason: "timer", Events: []syncEvent{{ID: -1, Type: "button", Button: buttonNext}}}).validate(); err == nil {
		t.Fatal("negative event id accepted")
	}
}

func TestMenuWakeForRunningAction(t *testing.T) {
	d := Device{MenuShowDuration: Duration(30 * time.Second)}
	now := time.Date(2026, time.November, 7, 12, 0, 0, 0, time.UTC)
	menu := menuState{Open: true, Phase: menuListPhase, OpenedAt: now.Add(-5 * time.Second)}
	if w := menuWake(menu, &d, now, false); w == nil || w.Reason != "menu" || w.AfterS != 25 {
		t.Fatalf("list wake: %+v", w)
	}
	menu.Phase = menuConfirmPhase
	if w := menuWake(menu, &d, now, false); w == nil || w.Reason != "menu" {
		t.Fatalf("confirm wake: %+v", w)
	}
	menu.Phase = menuRunningPhase
	if w := menuWake(menu, &d, now, false); w == nil || w.Reason != "result" {
		t.Fatalf("running wake: %+v", w)
	}
	menu.Phase = menuResultPhase
	if w := menuWake(menu, &d, now, false); w != nil {
		t.Fatalf("result needs no special wake: %+v", w)
	}
	menu.Phase = menuListPhase
	if w := menuWake(menu, &d, now, true); w != nil {
		t.Fatalf("a battery warning must not schedule a menu wake: %+v", w)
	}
	if w := menuWake(menuState{}, &d, now, false); w != nil {
		t.Fatalf("a closed menu must not schedule a wake: %+v", w)
	}
}

func TestResultWakeKeepsTheResult(t *testing.T) {
	s, _, p := menuServer(t, menuPlanActions, nil)
	dev := menuDevice(t, s)
	displayPage(t, s, dev)
	sendEvents(t, s, dev, "button", syncEvent{ID: 1, Type: "button", Button: buttonHome})
	// act/go needs a confirmation; act/quick does not. Take the second.
	sendEvents(t, s, dev, "button", syncEvent{ID: 2, Type: "button", Button: buttonNext})
	sendEvents(t, s, dev, "button", syncEvent{ID: 3, Type: "button", Button: buttonHome})
	if len(p.runs) != 1 {
		t.Fatalf("runs: %v", p.runs)
	}
	if st := s.state["test"]; st.Menu.Phase != menuResultPhase {
		t.Fatalf("phase after the action: %q", st.Menu.Phase)
	}
	// The wake that brings an outcome must not close the screen before it is
	// drawn.
	sendEvents(t, s, dev, "result")
	if st := s.state["test"]; !st.Menu.Open || st.Menu.Phase != menuResultPhase {
		t.Fatalf("result wake: %+v", st.Menu)
	}
	// The next regular sync closes it.
	sendEvents(t, s, dev, "timer")
	if st := s.state["test"]; st.Menu.Open {
		t.Fatalf("timer sync must close the result: %+v", st.Menu)
	}
}

func TestFinishActionStoresResult(t *testing.T) {
	s, _, p := menuServer(t, menuPlanActions, nil)
	dev := menuDevice(t, s)
	displayPage(t, s, dev)
	press(t, s, dev, buttonHome)
	press(t, s, dev, buttonNext)

	s.mu.Lock()
	st := s.state["test"]
	st.Menu.Phase = menuRunningPhase
	st.Menu.Selected = "act/quick"
	s.mu.Unlock()

	run := &actionRun{done: make(chan struct{}), ok: true, message: "done"}
	s.mu.Lock()
	s.actionRuns["test"] = run
	s.mu.Unlock()
	s.finishAction("test", "act/quick", run)

	s.mu.Lock()
	menu := s.state["test"].Menu
	s.mu.Unlock()
	if menu.Phase != menuResultPhase || !menu.ResultOK || menu.Result != "done" {
		t.Fatalf("menu after the run: %+v", menu)
	}
	if len(p.runs) != 0 {
		t.Fatalf("the plugin must not run here: %v", p.runs)
	}
	// A run of another action leaves the menu alone.
	s.mu.Lock()
	s.state["test"].Menu.Phase = menuRunningPhase
	s.state["test"].Menu.Selected = "act/quick"
	s.mu.Unlock()
	other := &actionRun{done: make(chan struct{}), ok: true}
	s.mu.Lock()
	s.actionRuns["test"] = other
	s.mu.Unlock()
	s.finishAction("test", "act/off", other)
	s.mu.Lock()
	phase := s.state["test"].Menu.Phase
	s.mu.Unlock()
	if phase != menuRunningPhase {
		t.Fatalf("an unrelated run changed the menu: %q", phase)
	}
}
