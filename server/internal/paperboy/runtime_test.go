package paperboy

import (
	"strings"
	"testing"
	"time"
)

var runtimeStart = time.Date(2026, time.October, 1, 8, 0, 0, 0, time.UTC)

// history builds samples with one percentage per step.
func history(step time.Duration, percents ...float64) []batterySample {
	samples := make([]batterySample, 0, len(percents))
	for i, percent := range percents {
		value := percent
		samples = append(samples, batterySample{
			Time:    runtimeStart.Add(time.Duration(i) * step),
			Percent: &value,
			Volts:   "3.90",
		})
	}
	return samples
}

// capacity builds samples with one remaining capacity per step, the scale a
// fuel gauge reports.
func capacity(step time.Duration, values ...float64) []batterySample {
	samples := make([]batterySample, 0, len(values))
	for i, value := range values {
		mah := value
		samples = append(samples, batterySample{Time: runtimeStart.Add(time.Duration(i) * step), MAH: &mah})
	}
	return samples
}

// voltage builds samples with one cell voltage per step, for hardware without
// a fuel gauge.
func voltage(step time.Duration, values ...string) []batterySample {
	samples := make([]batterySample, 0, len(values))
	for i, value := range values {
		samples = append(samples, batterySample{Time: runtimeStart.Add(time.Duration(i) * step), Volts: value})
	}
	return samples
}

// gauge builds ten hourly samples whose capacity falls by one mAh and whose
// percentage falls by two points in the same time, so the two scales would say
// different things.
func gauge(step time.Duration, mah, percent float64) []batterySample {
	samples := make([]batterySample, 0, 11)
	for i := 0; i <= 10; i++ {
		m, p := mah-float64(i), percent-0.2*float64(i)
		samples = append(samples, batterySample{Time: runtimeStart.Add(time.Duration(i) * step), MAH: &m, Percent: &p})
	}
	return samples
}

// closeTo compares an estimate, so the float arithmetic of the fit cannot fail
// a test by a second.
func closeTo(t *testing.T, name string, got, want time.Duration) {
	t.Helper()
	if want == 0 {
		if got != 0 {
			t.Errorf("%s: %v, want no estimate", name, got)
		}
		return
	}
	if diff := got - want; diff < -time.Minute || diff > time.Minute {
		t.Errorf("%s: %v, want %v", name, got, want)
	}
}

// TestBatteryEstimate checks the projection: which scale it follows, when the
// series gives none and how the fit behaves.
func TestBatteryEstimate(t *testing.T) {
	hour := time.Hour
	for _, c := range []struct {
		name    string
		samples []batterySample
		want    time.Duration
	}{
		{"no history", nil, 0},
		{"one sample", history(hour, 60), 0},
		// One point per ten hours, 58 points left: 290 h.
		{"percent", history(hour, 60, 59.8, 59.6, 59.4, 59.2, 59, 58.8, 58.6, 58.4, 58.2, 58), 290 * hour},
		{"flat", history(hour, 50, 50, 50), 0},
		{"rising", history(hour, 50, 51, 52), 0},
		// A capacity is the finer scale and wins over the percentage next to it.
		{"capacity", gauge(hour, 3000, 60), 2990 * hour},
		// 3.90 V to 3.80 V in ten hours, 0.80 V above the empty cell.
		{"voltage", voltage(hour, "3.90", "3.89", "3.88", "3.87", "3.86", "3.85", "3.84", "3.83", "3.82", "3.81", "3.80"), 80 * hour},
		// A projection beyond a year says nothing.
		{"beyond a year", capacity(24*hour, 3000, 2998), 0},
	} {
		got := estimateBattery(deviceState{BatteryHistory: c.samples})
		closeTo(t, c.name, got.Runtime, c.want)
	}
}

// TestBatteryEstimateWait checks when the series shows its first estimate: the
// drop has to leave the noise of the scale, and a promise beyond two days says
// nothing.
func TestBatteryEstimateWait(t *testing.T) {
	hour := time.Hour
	for _, c := range []struct {
		name    string
		samples []batterySample
		want    time.Duration
	}{
		// 0.1 points per hour: the missing 0.9 points take another 9 h.
		{"percent", history(hour, 60, 59.9), 9 * hour},
		// A hundredth of a point per hour fills the point only after 99 h.
		{"too far", history(hour, 60, 59.98), 0},
		{"rising", history(hour, 50, 51), 0},
		{"one sample", history(hour, 60), 0},
	} {
		if got := estimateBattery(deviceState{BatteryHistory: c.samples}).Wait; got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// TestRuntimeText checks the day and hour forms of the estimate.
func TestRuntimeText(t *testing.T) {
	for _, c := range []struct {
		d    time.Duration
		want string
	}{
		{0, ""},
		{-time.Hour, ""},
		{18 * time.Hour, "~18 h"},
		{47 * time.Hour, "~47 h"},
		{48 * time.Hour, "~2 d"},
		{12*24*time.Hour + 11*time.Hour, "~12 d"},
	} {
		if got := runtimeText(c.d); got != c.want {
			t.Errorf("%v: %q, want %q", c.d, got, c.want)
		}
	}
}

// TestSpanText checks the data line of the tooltip.
func TestDataSpanText(t *testing.T) {
	for _, c := range []struct {
		d    time.Duration
		want string
	}{
		{0, "under an hour"},
		{30 * time.Minute, "under an hour"},
		{time.Hour, "1 h"},
		{6 * time.Hour, "6 h"},
		{47 * time.Hour, "47 h"},
		{48 * time.Hour, "2 d"},
		{6 * 24 * time.Hour, "6 d"},
	} {
		if got := dataSpanText(c.d); got != c.want {
			t.Errorf("%v: %q, want %q", c.d, got, c.want)
		}
	}
}

// TestBatteryHistorySamplesOncePerHour checks that only the first sync of an
// hour adds a sample and that the series keeps a week, however rarely the
// device syncs.
func TestBatteryHistorySamplesOncePerHour(t *testing.T) {
	now := runtimeStart
	st := &deviceState{}
	ma, percent := 0.0, 60.0
	for i := 0; i < 10*24; i++ {
		for _, at := range []time.Time{now, now.Add(30 * time.Minute)} {
			trackBattery(st, &syncBattery{Volts: 3.9, Percent: &percent, MA: &ma}, at)
		}
		now = now.Add(time.Hour)
		percent -= 0.01
	}
	if n := len(st.BatteryHistory); n > 169 {
		t.Fatalf("%d samples, want at most a week of hours", n)
	}
	first, last := st.BatteryHistory[0], st.BatteryHistory[len(st.BatteryHistory)-1]
	if span := last.Time.Sub(first.Time); span != retention {
		t.Fatalf("history spans %v, want %v", span, retention)
	}
}

// TestBatteryHistoryClearedByCharge checks that a charge starts a new series,
// so it does not look like a slow discharge, and that one sample is not enough
// for an estimate.
func TestBatteryHistoryClearedByCharge(t *testing.T) {
	now := runtimeStart
	st := &deviceState{}
	ma, percent := 0.0, 60.0
	for i := 0; i < 4; i++ {
		trackBattery(st, &syncBattery{Volts: 3.9, Percent: &percent, MA: &ma}, now)
		now = now.Add(time.Hour)
		percent--
	}
	if estimateBattery(*st).Runtime == 0 {
		t.Fatal("four hours of discharge must give an estimate")
	}

	ma = 568
	trackBattery(st, &syncBattery{Volts: 3.7, Percent: &percent, MA: &ma}, now)
	if len(st.BatteryHistory) != 0 || estimateBattery(*st).Runtime != 0 {
		t.Fatalf("a charge must clear the history: %+v", st.BatteryHistory)
	}

	ma = 0
	trackBattery(st, &syncBattery{Volts: 4.0, Percent: &percent, MA: &ma}, now.Add(2*time.Hour))
	if got := estimateBattery(*st); got.Runtime != 0 || got.Samples != 1 {
		t.Fatalf("one sample after a charge is no estimate: %+v", got)
	}
}

// TestRuntimeCard checks the Runtime row of a card: the collecting state, the
// early and the settled estimate and the devices that get no row at all.
func TestRuntimeCard(t *testing.T) {
	hour := time.Hour
	battery := deviceState{BatteryVoltage: "3.90"}
	with := func(st deviceState) (string, string, string) { return runtimeCard(powerBattery, st) }

	if value, note, hint := with(deviceState{BatteryVoltage: "3.90"}); value != "still collecting" || note != "" || hint != "waiting for the second sample" {
		t.Fatalf("no samples: %q %q %q", value, note, hint)
	}

	// Five hours with two points: that is not a day of data.
	st := battery
	st.BatteryHistory = history(hour, 60, 59.6, 59.2, 58.8, 58.4, 58)
	if value, note, hint := with(st); value != "~6 d" || note != "early estimate" || hint != "5 h of data" {
		t.Fatalf("early estimate: %q %q %q", value, note, hint)
	}

	// A day of data makes the estimate a settled one: two points a day, 58
	// points left.
	var percents []float64
	for i := 0; i <= 24; i++ {
		percents = append(percents, 60-2*float64(i)/24)
	}
	st = battery
	st.BatteryHistory = history(hour, percents...)
	if value, note, hint := with(st); value != "~29 d" || note != "estimated" || hint != "24 h of data" {
		t.Fatalf("settled estimate: %q %q %q", value, note, hint)
	}

	// No measurable discharge yet: the tooltip names the data only.
	st = battery
	st.BatteryHistory = history(hour, 60, 59.98)
	if value, note, hint := with(st); value != "still collecting" || note != "" || hint != "1 h of data · no discharge yet" {
		t.Fatalf("collecting: %q %q %q", value, note, hint)
	}

	// A device on mains and hardware without a battery reading get no row.
	if value, _, _ := runtimeCard(powerMains, st); value != "" {
		t.Fatalf("a mains device must not show the runtime: %q", value)
	}
	if value, _, _ := with(deviceState{}); value != "" {
		t.Fatalf("a device without a battery reading must not show the runtime: %q", value)
	}
}

// TestDashboardRuntime checks that the card names the estimate and its data,
// and that a fresh device says that it is still collecting.
func TestDashboardRuntime(t *testing.T) {
	s, now := planServer(t, testPlan)
	dev := planDevice(t, s)
	ma, percent := 0.0, 60.0
	dev.req.Battery = &syncBattery{Volts: 3.9, Percent: &percent, MA: &ma}
	displayPage(t, s, dev)
	body := htmlRequest(t, s, "/ui").Body.String()
	for _, want := range []string{"<dt>Runtime</dt>", "still collecting", "waiting for the second sample"} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard misses %q: %s", want, body)
		}
	}

	for i := 0; i < 48; i++ {
		*now = now.Add(time.Hour)
		dev.req.Battery = &syncBattery{Volts: 3.9, Percent: &percent, MA: &ma}
		displayPage(t, s, dev)
		percent -= 1.0 / 24
	}
	body = htmlRequest(t, s, "/ui").Body.String()
	for _, want := range []string{"<dt>Runtime</dt>", "~58 d", "estimated", "2 d of data"} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard misses %q after two days: %s", want, body)
		}
	}
}
