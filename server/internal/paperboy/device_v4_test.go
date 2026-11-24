package paperboy

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
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
	if st := s.state["AABBCCDDEE01"]; st.Page != 1 || st.LastEventID != 1 {
		t.Fatalf("first press: %+v", st)
	}
	sendEvents(t, s, dev, "button", next)
	if st := s.state["AABBCCDDEE01"]; st.Page != 1 {
		t.Fatalf("a repeated event moved the page: %+v", st)
	}
	// A retry may also bundle the event with the next one.
	out = sendEvents(t, s, dev, "button", next, syncEvent{ID: 2, Type: "button", Button: buttonNext})
	if st := s.state["AABBCCDDEE01"]; st.Page != 2 || st.LastEventID != 2 {
		t.Fatalf("second press: %+v", st)
	}
	if out["wake"] != nil {
		t.Fatalf("no menu open, no wake: %v", out["wake"])
	}
	// An event without an ID is applied as before.
	sendEvents(t, s, dev, "button", syncEvent{Type: "button", Button: buttonNext})
	if st := s.state["AABBCCDDEE01"]; st.Page != 3 {
		t.Fatalf("legacy event: %+v", st)
	}
	// A power cycle restarts the firmware's counter, so the server forgets the
	// last ID and accepts a small one again.
	sendEvents(t, s, dev, "powercycle", syncEvent{ID: 1, Type: "button", Button: buttonNext})
	if st := s.state["AABBCCDDEE01"]; st.Page != 0 || st.LastEventID != 1 {
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
