package paperboy

import (
	"strings"
	"testing"
	"time"
)

// TestRuntimeText estimates the remaining runtime from the discharge history
// (issue #207).
func TestRuntimeText(t *testing.T) {
	t0 := time.Date(2026, time.November, 13, 12, 0, 0, 0, time.UTC)
	pct := func(v float64) *float64 { return &v }
	at := func(hours float64, v float64) batterySample {
		return batterySample{Time: t0.Add(time.Duration(hours * float64(time.Hour))), Percent: pct(v)}
	}
	for _, c := range []struct {
		name string
		st   deviceState
		want string
	}{
		{"ten days", deviceState{BatteryPercent: pct(40), BatterySamples: []batterySample{
			at(0, 90), at(24, 86), at(48, 82)}}, "~10 d"},
		{"hours", deviceState{BatteryPercent: pct(5), BatterySamples: []batterySample{
			at(0, 60), at(24, 40)}}, "~6 h"},
		{"one sample", deviceState{BatteryPercent: pct(40), BatterySamples: []batterySample{
			at(0, 90)}}, ""},
		{"short span", deviceState{BatteryPercent: pct(40), BatterySamples: []batterySample{
			at(0, 90), at(12, 88)}}, ""},
		{"charging", deviceState{BatteryPercent: pct(60), BatterySamples: []batterySample{
			at(0, 40), at(24, 55)}}, ""},
		{"no charge", deviceState{BatterySamples: []batterySample{
			at(0, 90), at(48, 82)}}, ""},
	} {
		if got := runtimeText(c.st); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// TestRecordBatterySample checks the sampling rules of the history: one per
// half day, one reading without a percentage is ignored, a charge resets it and
// the list stays capped.
func TestRecordBatterySample(t *testing.T) {
	now := time.Date(2026, time.November, 13, 12, 0, 0, 0, time.UTC)
	pct := func(v float64) *float64 { return &v }
	ma := func(v float64) *float64 { return &v }

	st := deviceState{}
	st.recordBatterySample(&syncBattery{Percent: pct(80)}, now)
	st.recordBatterySample(&syncBattery{Percent: pct(79)}, now.Add(time.Hour))
	if len(st.BatterySamples) != 1 {
		t.Fatalf("one sample per interval: %+v", st.BatterySamples)
	}
	st.recordBatterySample(&syncBattery{Percent: pct(78)}, now.Add(sampleInterval))
	if len(st.BatterySamples) != 2 {
		t.Fatalf("a second interval adds a sample: %+v", st.BatterySamples)
	}

	st.recordBatterySample(&syncBattery{Percent: pct(70), MA: ma(400)}, now.Add(2*sampleInterval))
	if len(st.BatterySamples) != 0 {
		t.Fatalf("a charge resets the history: %+v", st.BatterySamples)
	}
	st.recordBatterySample(&syncBattery{}, now)
	if len(st.BatterySamples) != 0 {
		t.Fatalf("no percentage, no sample: %+v", st.BatterySamples)
	}

	for i := 0; i < maxBatterySamples+3; i++ {
		st.recordBatterySample(&syncBattery{Percent: pct(50)}, now.Add(time.Duration(i+1)*sampleInterval))
	}
	if len(st.BatterySamples) != maxBatterySamples {
		t.Fatalf("samples are capped: %d", len(st.BatterySamples))
	}
}

// TestDashboardRuntime checks that the estimate reaches the card's details.
func TestDashboardRuntime(t *testing.T) {
	s, now := planServer(t, testPlan)
	dev := planDevice(t, s)
	percent := 42.0
	dev.req.Battery = &syncBattery{Volts: 3.7, Percent: &percent}
	displayPage(t, s, dev)

	st := s.state["AABBCCDDEE01"]
	first := 82.0
	st.BatterySamples = []batterySample{
		{Time: now.Add(-48 * time.Hour), Percent: &first},
		{Time: *now, Percent: &percent},
	}
	body := htmlRequest(t, s, "/ui").Body.String()
	if !strings.Contains(body, "<dt>Runtime</dt><dd>~2 d</dd>") {
		t.Fatalf("card misses the runtime estimate: %s", body)
	}
}
