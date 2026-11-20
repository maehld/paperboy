package paperboy

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestManualAlertOnNextSync checks the whole path of a manual alert: the
// dashboard queues it, the next sync carries the methods of the device file,
// and the sync after that stays quiet (ADR 0142).
func TestManualAlertOnNextSync(t *testing.T) {
	logs := captureLog(t)
	s, _ := planServer(t, testPlan)
	s.config().Devices[0].AlertMethods = &[]string{alertBeep, alertBlink}
	dev := planDevice(t, s)
	displayPage(t, s, dev) // the first contact settles the rule

	rec := request(t, s, http.MethodPost, "/api/devices/test/alert", nil, nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/ui?alert=test" {
		t.Fatalf("queue: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	s.lock("test")
	queued := s.state[s.config().Devices[0].stateKey()].PendingAlert
	s.mu.Unlock()
	if want := s.now().Add(time.Hour); queued == nil || !queued.Until.Equal(want) {
		t.Fatalf("the request must last one hour: %+v", queued)
	}
	out := displayPage(t, s, dev)
	if out["beep"] != true || out["blink"] != true {
		t.Fatalf("the next sync must signal: %v", out)
	}
	if !strings.Contains(logs(), "alert=manual") {
		t.Fatalf("the log must name the manual source: %s", logs())
	}
	if out := displayPage(t, s, dev); out["beep"] != nil || out["blink"] != nil {
		t.Fatalf("the request is one-shot: %v", out)
	}
}

// TestManualAlertExpires checks that a request the device did not collect is
// dropped instead of signalling hours later.
func TestManualAlertExpires(t *testing.T) {
	s, now := planServer(t, testPlan)
	s.config().Devices[0].AlertMethods = &[]string{alertBeep}
	dev := planDevice(t, s)
	displayPage(t, s, dev)
	key := s.config().Devices[0].stateKey()
	s.lock("test")
	st := s.state[key]
	st.PendingAlert = &pendingAlert{At: now.Add(-3 * time.Hour), Until: now.Add(-2 * time.Hour)}
	s.mu.Unlock()

	if out := displayPage(t, s, dev); out["beep"] != nil {
		t.Fatalf("an expired request must stay quiet: %v", out)
	}
	s.lock("test")
	left := st.PendingAlert
	s.mu.Unlock()
	if left != nil {
		t.Fatalf("the expired request must be gone: %+v", left)
	}
}

// TestTakeManualAlert reads the three cases of the request itself: a fresh one
// signals, an expired one and one during the battery warning are dropped.
func TestTakeManualAlert(t *testing.T) {
	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	d := &Device{ID: "test"}
	fresh := func() *deviceState {
		return &deviceState{PendingAlert: &pendingAlert{At: now.Add(-time.Minute), Until: now.Add(time.Hour)}}
	}
	if st := fresh(); !takeManualAlert(d, st, now, false) || st.PendingAlert != nil {
		t.Fatal("a fresh request must be taken and cleared")
	}
	if st := (&deviceState{}); takeManualAlert(d, st, now, false) {
		t.Fatal("without a request nothing is taken")
	}
	st := fresh()
	st.PendingAlert.Until = now.Add(-time.Second)
	if takeManualAlert(d, st, now, false) || st.PendingAlert != nil {
		t.Fatal("an expired request must be dropped and cleared")
	}
	if st := fresh(); takeManualAlert(d, st, now, true) || st.PendingAlert != nil {
		t.Fatal("the battery warning must drop the request")
	}
}

// TestManualAlertRefused checks that Paperboy refuses a request that cannot
// reach the device: the dashboard learns why instead of waiting for nothing.
func TestManualAlertRefused(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, s *Server)
		id    string
		want  string
	}{
		{"no methods", func(t *testing.T, s *Server) { planDevice(t, s) }, "test", refuseMethods},
		{"unregistered", func(t *testing.T, s *Server) {
			s.config().Devices[0].AlertMethods = &[]string{alertBeep}
		}, "test", refuseUnregistered},
		{"unknown id", func(t *testing.T, s *Server) {}, "nope", refuseUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := planServer(t, testPlan)
			tc.setup(t, s)
			rec := request(t, s, http.MethodPost, "/api/devices/"+tc.id+"/alert", nil, nil)
			loc := rec.Header().Get("Location")
			if rec.Code != http.StatusSeeOther || !strings.Contains(loc, "refused="+tc.want) {
				t.Fatalf("refusal: %d %q, want %s", rec.Code, loc, tc.want)
			}
			for _, st := range s.state {
				if st.PendingAlert != nil {
					t.Fatalf("a refused request must not queue: %+v", st.PendingAlert)
				}
			}
		})
	}
}

// TestDashboardOffersSignal checks the card: the button posts to the device,
// a queued request shows in the details, and a device without methods names why
// it cannot signal.
func TestDashboardOffersSignal(t *testing.T) {
	s, _ := planServer(t, testPlan)
	s.config().Devices[0].AlertMethods = &[]string{alertBeep}
	planDevice(t, s)

	body := request(t, s, http.MethodGet, "/ui", nil, nil).Body.String()
	if !strings.Contains(body, `action="/api/devices/test/alert"`) {
		t.Fatalf("the card must offer the form: %s", body)
	}
	if strings.Contains(body, "Signal unavailable") {
		t.Fatalf("a device with methods must offer the signal: %s", body)
	}
	if rec := request(t, s, http.MethodPost, "/api/devices/test/alert", nil, nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("queue: %d %s", rec.Code, rec.Body)
	}
	body = request(t, s, http.MethodGet, "/ui?alert=test", nil, nil).Body.String()
	if !strings.Contains(body, "Signal queued") || !strings.Contains(body, "queued just now") {
		t.Fatalf("the card must confirm the request: %s", body)
	}

	s.config().Devices[0].AlertMethods = nil
	body = request(t, s, http.MethodGet, "/ui", nil, nil).Body.String()
	if !strings.Contains(body, "Signal unavailable: the device file names no alert method") {
		t.Fatalf("a silent device must name the reason: %s", body)
	}
}
