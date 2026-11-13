package paperboy

import (
	"strings"
	"testing"
	"time"
)

// history builds daily samples from the given percents.
func history(start time.Time, step time.Duration, percents ...float64) []batterySample {
	samples := make([]batterySample, 0, len(percents))
	for i, percent := range percents {
		value := percent
		samples = append(samples, batterySample{
			Time:    start.Add(time.Duration(i) * step),
			Percent: &value,
			Volts:   "3.90",
		})
	}
	return samples
}

// TestBatteryRuntimeEstimate checks the slope estimate: too little history, a
// flat or rising series and a percentage device.
func TestBatteryRuntimeEstimate(t *testing.T) {
	day := 24 * time.Hour
	start := time.Date(2026, time.October, 1, 8, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name    string
		samples []batterySample
		want    time.Duration
	}{
		{"no history", nil, 0},
		{"one sample", history(start, day, 60), 0},
		{"less than a day", history(start, 12*time.Hour, 60, 59), 0},
		{"one percent per day", history(start, day, 60, 59, 58, 57, 56, 55, 54), 54 * day},
		{"flat", history(start, day, 50, 50, 50), 0},
		{"rising", history(start, day, 50, 52, 54), 0},
		{"beyond a year", history(start, day, 60, 60, 59.9), 0},
	} {
		st := deviceState{BatteryHistory: c.samples}
		if got := batteryRuntime(st); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// TestBatteryRuntimeFromVoltage checks the estimate on hardware without a fuel
// gauge: the slope of the voltage down to an empty cell.
func TestBatteryRuntimeFromVoltage(t *testing.T) {
	day := 24 * time.Hour
	start := time.Date(2026, time.October, 1, 8, 0, 0, 0, time.UTC)
	volts := func(values ...string) []batterySample {
		samples := make([]batterySample, 0, len(values))
		for i, v := range values {
			samples = append(samples, batterySample{Time: start.Add(time.Duration(i) * day), Volts: v})
		}
		return samples
	}
	// 3.90 V to 3.60 V over six days is 0.05 V per day; 0.6 V above the empty
	// cell at 3.0 V lasts twelve days.
	if got, want := batteryRuntime(deviceState{BatteryHistory: volts(
		"3.90", "3.85", "3.80", "3.75", "3.70", "3.65", "3.60")}), 12*day; got != want {
		t.Fatalf("voltage estimate: %v, want %v", got, want)
	}
	if got := batteryRuntime(deviceState{BatteryHistory: volts("3.20", "3.10", "2.90")}); got != 0 {
		t.Fatalf("a cell below the empty voltage has no estimate: %v", got)
	}
	if got := batteryRuntime(deviceState{BatteryHistory: volts("3.90", "3.95", "4.00")}); got != 0 {
		t.Fatalf("a rising voltage has no estimate: %v", got)
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

// TestBatteryHistorySamplesOncePerDay checks that only the first sync of a day
// adds a sample and that the history keeps a week.
func TestBatteryHistorySamplesOncePerDay(t *testing.T) {
	now := time.Date(2026, time.October, 1, 8, 0, 0, 0, time.UTC)
	st := &deviceState{}
	ma, percent := 0.0, 60.0
	for i := 0; i < 10; i++ {
		for _, at := range []time.Time{now, now.Add(3 * time.Hour)} {
			trackBattery(st, &syncBattery{Volts: 3.9, Percent: &percent, MA: &ma}, at)
		}
		now = now.Add(24 * time.Hour)
		percent--
	}
	if len(st.BatteryHistory) != maxSamples {
		t.Fatalf("%d samples, want %d", len(st.BatteryHistory), maxSamples)
	}
	first, last := st.BatteryHistory[0], st.BatteryHistory[maxSamples-1]
	if span := last.Time.Sub(first.Time); span != 6*24*time.Hour {
		t.Fatalf("history spans %v, want six days", span)
	}
}

// TestBatteryHistoryClearedByCharge checks that a charge starts a new series,
// so it does not look like a slow discharge, and that one sample is not enough
// for an estimate.
func TestBatteryHistoryClearedByCharge(t *testing.T) {
	now := time.Date(2026, time.October, 1, 8, 0, 0, 0, time.UTC)
	st := &deviceState{}
	ma, percent := 0.0, 60.0
	for i := 0; i < 4; i++ {
		trackBattery(st, &syncBattery{Volts: 3.9, Percent: &percent, MA: &ma}, now)
		now = now.Add(24 * time.Hour)
		percent--
	}
	if batteryRuntime(*st) == 0 {
		t.Fatal("four days of discharge must give an estimate")
	}

	ma = 568
	trackBattery(st, &syncBattery{Volts: 3.7, Percent: &percent, MA: &ma}, now)
	if len(st.BatteryHistory) != 0 || batteryRuntime(*st) != 0 {
		t.Fatalf("a charge must clear the history: %+v", st.BatteryHistory)
	}

	ma = 0
	trackBattery(st, &syncBattery{Volts: 4.0, Percent: &percent, MA: &ma}, now.Add(2*time.Hour))
	if got := batteryRuntime(*st); got != 0 {
		t.Fatalf("one sample after a charge is no estimate: %v", got)
	}
}

// TestDashboardRuntime checks that the card's details name the estimate.
func TestDashboardRuntime(t *testing.T) {
	s, now := planServer(t, testPlan)
	dev := planDevice(t, s)
	ma, percent := 0.0, 60.0
	for i := 0; i < 7; i++ {
		dev.req.Battery = &syncBattery{Volts: 3.9, Percent: &percent, MA: &ma}
		displayPage(t, s, dev)
		*now = now.Add(24 * time.Hour)
		percent--
	}
	body := htmlRequest(t, s, "/ui").Body.String()
	for _, want := range []string{"<dt>Runtime</dt>", "~54 d", "estimated"} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard misses %q: %s", want, body)
		}
	}
}
