package paperboy

import (
	"fmt"
	"strconv"
	"time"
)

// The runtime estimate (issue #207, issue #236) learns the discharge rate from
// the device itself: one battery sample per hour, a week of them, and the slope
// of the series while the device is discharging. A model of the cell would be
// guessing: sleep current, the panel's refresh energy and the gauge itself
// differ per hardware.
const (
	// sampleInterval is the spacing of the samples: the sync's reading, at most
	// once an hour. Hourly samples show a discharge within hours instead of
	// days, and they still average the daily rhythm of the display.
	sampleInterval = time.Hour
	// retention is how much history stays in the state file: a week, which is
	// at most 168 hourly samples.
	retention = 7 * 24 * time.Hour
	// emptyVolts is the cell voltage the estimate counts down to. A Li-ion cell
	// is empty around 3.0 V; hardware without a fuel gauge has no better anchor.
	emptyVolts = 3.0
	// The minDrop values are the drop that counts as a discharge rather than as
	// gauge resolution or noise. Capacities and percentages are coulomb counts
	// and move early; the voltage only leaves its band of about 0.02 V after a
	// long while.
	minDropMAH     = 2
	minDropPercent = 1
	minDropVolts   = 0.05
	// settledAfter is the data a full estimate needs: a day covers the daily
	// rhythm of the display, so anything shorter is marked as an early one.
	settledAfter = 24 * time.Hour
	// maxRuntime hides a projection beyond it: at such a rate the series says
	// more about the gauge than about the cell.
	maxRuntime = 365 * 24 * time.Hour
	// maxWait hides an expected estimate beyond it: two days of collecting is
	// no promise worth naming.
	maxWait = 48 * time.Hour
)

// batterySample is one hourly reading for the runtime estimate.
type batterySample struct {
	Time    time.Time `json:"time"`
	MAH     *float64  `json:"mah,omitempty"`     // remaining capacity from the fuel gauge
	Percent *float64  `json:"percent,omitempty"` // state of charge from the fuel gauge
	Volts   string    `json:"volts,omitempty"`   // cell voltage, for hardware without a gauge
}

// addSample keeps one reading per sampleInterval and drops what is older than
// the retained week: a sync within the interval replaces nothing, because the
// spacing is what the slope measures.
func addSample(samples []batterySample, b *syncBattery, now time.Time) []batterySample {
	if n := len(samples); n > 0 && now.Sub(samples[n-1].Time) < sampleInterval {
		return samples
	}
	samples = append(samples, batterySample{
		Time:    now,
		MAH:     copyFloat(b.MAH),
		Percent: copyFloat(b.Percent),
		Volts:   strconv.FormatFloat(b.Volts, 'f', 2, 64),
	})
	for len(samples) > 1 && now.Sub(samples[0].Time) > retention {
		samples = samples[1:]
	}
	return samples
}

// batteryEstimate is the state of the runtime estimate for one device: the
// projection, the data behind it and, while there is none, how long until the
// series can give one.
type batteryEstimate struct {
	Samples int           // readings of the series the estimate follows
	Runtime time.Duration // how long the battery lasts; 0 while there is no estimate
	Span    time.Duration // the time the samples cover
	Wait    time.Duration // until the drop is measurable; 0 when unknown
}

// sampleSeries is one scale the samples can be followed on and its trend.
type sampleSeries struct {
	samples int
	span    time.Duration
	drop    float64 // value lost per hour, negative while rising
	left    float64 // value left above empty
	enough  float64 // drop that counts as a discharge on this scale
}

// estimateBattery projects how long the battery lasts at the rate the samples
// show. It answers no estimate until the drop leaves the noise of the scale,
// while the series rises and beyond a year, where the projection would say
// nothing.
func estimateBattery(st deviceState) batteryEstimate {
	series, ok := runtimeSeries(st.BatteryHistory)
	if !ok {
		e := batteryEstimate{Samples: len(st.BatteryHistory)}
		if n := len(st.BatteryHistory); n > 1 {
			e.Span = st.BatteryHistory[n-1].Time.Sub(st.BatteryHistory[0].Time)
		}
		return e
	}
	e := batteryEstimate{Samples: series.samples, Span: series.span}
	if series.drop <= 0 {
		return e
	}
	if drop := series.drop * series.span.Hours(); drop < series.enough {
		e.Wait = waitForDrop(series.enough-drop, series.drop)
		return e
	}
	if series.left <= 0 {
		return e
	}
	if estimate := time.Duration(series.left / series.drop * float64(time.Hour)); estimate <= maxRuntime {
		e.Runtime = estimate
	}
	return e
}

// runtimeSeries picks the scale the estimate follows: the fuel gauge's
// remaining capacity first, then its percentage, then the voltage. Capacities
// and percentages are the same coulomb count, just finer and coarser, and they
// resolve a discharge long before the voltage leaves its measurement band.
func runtimeSeries(samples []batterySample) (sampleSeries, bool) {
	if s, ok := trend(samples, func(s batterySample) (float64, bool) {
		return floatOr(s.MAH)
	}); ok {
		s.enough = minDropMAH
		return s, true
	}
	if s, ok := trend(samples, func(s batterySample) (float64, bool) {
		return floatOr(s.Percent)
	}); ok {
		s.enough = minDropPercent
		return s, true
	}
	if s, ok := trend(samples, func(s batterySample) (float64, bool) {
		if s.Volts == "" {
			return 0, false
		}
		return parseVoltage(s.Volts)
	}); ok {
		s.enough = minDropVolts
		s.left -= emptyVolts
		return s, true
	}
	return sampleSeries{}, false
}

// trend fits a straight line through the values that pick answers and reports
// its slope, the value that is left and the data it used. Least squares over
// every sample instead of first minus last: hourly readings carry the gauge's
// noise, and one wandering value must not decide the estimate.
func trend(samples []batterySample, pick func(batterySample) (float64, bool)) (sampleSeries, bool) {
	var times, values []float64
	start := time.Time{}
	for _, sample := range samples {
		value, ok := pick(sample)
		if !ok {
			continue
		}
		if start.IsZero() {
			start = sample.Time
		}
		times = append(times, sample.Time.Sub(start).Hours())
		values = append(values, value)
	}
	if len(times) < 2 {
		return sampleSeries{}, false
	}
	var meanTime, meanValue float64
	for i := range times {
		meanTime += times[i]
		meanValue += values[i]
	}
	meanTime /= float64(len(times))
	meanValue /= float64(len(values))
	var covariance, variance float64
	for i := range times {
		covariance += (times[i] - meanTime) * (values[i] - meanValue)
		variance += (times[i] - meanTime) * (times[i] - meanTime)
	}
	if variance == 0 {
		return sampleSeries{}, false
	}
	last := len(times) - 1
	return sampleSeries{
		samples: len(times),
		span:    time.Duration(times[last] * float64(time.Hour)),
		drop:    -covariance / variance,
		left:    values[last],
	}, true
}

// floatOr answers the value behind an optional reading.
func floatOr(v *float64) (float64, bool) {
	if v == nil {
		return 0, false
	}
	return *v, true
}

// waitForDrop estimates how long the series needs to show a drop of missing at
// rate, rounded up to a whole hour. Beyond maxWait it answers 0: an estimate
// that far away is no promise.
func waitForDrop(missing, rate float64) time.Duration {
	if rate <= 0 {
		return 0
	}
	wait := time.Duration(missing / rate * float64(time.Hour))
	if wait > maxWait {
		return 0
	}
	return ((wait + time.Hour - 1) / time.Hour) * time.Hour
}

// runtimeText formats the estimate as "~12 d" or, below two days, "~18 h". The
// tilde says that it is an estimate, not a promise.
func runtimeText(d time.Duration) string {
	switch {
	case d <= 0:
		return ""
	case d < 48*time.Hour:
		hours := int(d.Round(time.Hour) / time.Hour)
		if hours < 1 {
			hours = 1
		}
		return fmt.Sprintf("~%d h", hours)
	}
	return fmt.Sprintf("~%d d", int(d.Round(24*time.Hour)/(24*time.Hour)))
}

// spanText names the data behind the estimate: "under an hour", "6 h" or "3 d".
func dataSpanText(d time.Duration) string {
	switch {
	case d < time.Hour:
		return "under an hour"
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h", int(d.Round(time.Hour)/time.Hour))
	}
	return fmt.Sprintf("%d d", int(d.Round(24*time.Hour)/(24*time.Hour)))
}
