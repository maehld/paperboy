package paperboy

import (
	"strings"
	"testing"
	"time"
)

// TestTrackCharge follows one charging episode from the fuel gauge current and
// checks the text the card's details show (issue #205).
func TestTrackCharge(t *testing.T) {
	s, now := planServer(t, testPlan)
	dev := planDevice(t, s)
	start, mid, end := 41.0, 47.0, 50.0
	charging, idle := 520.0, 0.0

	dev.req.Battery = &syncBattery{Volts: 3.70, Percent: &start, MA: &charging}
	displayPage(t, s, dev)
	*now = now.Add(10 * time.Minute)
	dev.req.Battery = &syncBattery{Volts: 3.82, Percent: &mid, MA: &charging}
	displayPage(t, s, dev)

	st := s.state["AABBCCDDEE01"]
	if st.Charge == nil || st.LastCharge != nil {
		t.Fatalf("a charge current must open an episode: %+v", st)
	}
	if st.Charge.StartPercent == nil || *st.Charge.StartPercent != start {
		t.Fatalf("start charge: %+v", st.Charge)
	}

	*now = now.Add(20 * time.Minute)
	dev.req.Battery = &syncBattery{Volts: 3.90, Percent: &end, MA: &idle}
	displayPage(t, s, dev)

	if st.Charge != nil || st.LastCharge == nil {
		t.Fatalf("an idle sync must close the episode: %+v", st)
	}
	if *st.LastCharge.StartPercent != start || *st.LastCharge.EndPercent != end || !st.LastCharge.End.Equal(*now) {
		t.Fatalf("last charge: %+v", st.LastCharge)
	}
	if got := chargeText(*st, *now); got != "just now · 41 → 50 %" {
		t.Fatalf("charge text now: %q", got)
	}
	if got := chargeText(*st, now.Add(2*time.Hour)); got != "2 h ago · 41 → 50 %" {
		t.Fatalf("charge text later: %q", got)
	}

	body := htmlRequest(t, s, "/ui").Body.String()
	if !strings.Contains(body, "<dt>Charged</dt><dd>just now · 41 → 50 %</dd>") {
		t.Fatalf("card misses the last charge: %s", body)
	}
}

// TestTrackChargeWithoutGauge checks that a device that reports no current
// never opens an episode, and that a charge without a percentage shows only
// what is known.
func TestTrackChargeWithoutGauge(t *testing.T) {
	percent := 50.0
	st := deviceState{}
	st.trackCharge(&syncBattery{Volts: 3.9, Percent: &percent}, time.Now())
	if st.Charge != nil || st.LastCharge != nil {
		t.Fatalf("no current, no episode: %+v", st)
	}

	now := time.Now()
	st.trackCharge(&syncBattery{Volts: 3.9, MA: floatPtr(500)}, now)
	st.trackCharge(&syncBattery{Volts: 3.9, MA: floatPtr(0)}, now)
	if got := chargeText(st, now); got != "just now" {
		t.Fatalf("a charge without percentages: %q", got)
	}
}

func floatPtr(v float64) *float64 { return &v }
