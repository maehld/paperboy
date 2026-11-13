package paperboy

import (
	"fmt"
	"strconv"
	"time"
)

// The runtime estimate (issue #207) learns the discharge rate from the device
// itself: one battery sample per day, at most a week, and the slope of the
// first and last sample while the device is discharging. A model of the cell
// would be guessing: sleep current, the panel's refresh energy and the gauge
// itself differ per hardware.
const (
	sampleInterval = 24 * time.Hour
	maxSamples     = 7
	// emptyVolts is the cell voltage the estimate counts down to. A Li-ion cell
	// is empty around 3.0 V; hardware without a fuel gauge has no better anchor.
	emptyVolts = 3.0
)

// batterySample is one daily reading for the runtime estimate.
type batterySample struct {
	Time    time.Time `json:"time"`
	Percent *float64  `json:"percent,omitempty"`
	Volts   string    `json:"volts,omitempty"`
}

// addSample keeps one reading per day: a sample younger than the interval
// replaces nothing, because the daily spacing is what the slope measures, and
// only the newest week stays.
func addSample(samples []batterySample, b *syncBattery, now time.Time) []batterySample {
	if n := len(samples); n > 0 && now.Sub(samples[n-1].Time) < sampleInterval {
		return samples
	}
	samples = append(samples, batterySample{
		Time:    now,
		Percent: copyFloat(b.Percent),
		Volts:   strconv.FormatFloat(b.Volts, 'f', 2, 64),
	})
	if len(samples) > maxSamples {
		samples = samples[len(samples)-maxSamples:]
	}
	return samples
}

// batteryRuntime estimates how long the battery lasts at the rate the samples
// show. It answers 0 when the history is too short (fewer than two samples over
// more than a day), when the device is not discharging, or when the estimate is
// beyond a year, where it would say nothing.
func batteryRuntime(st deviceState) time.Duration {
	samples := st.BatteryHistory
	if len(samples) < 2 {
		return 0
	}
	first, last := samples[0], samples[len(samples)-1]
	span := last.Time.Sub(first.Time)
	if span < sampleInterval {
		return 0
	}
	var drop, left float64 // per hour, and what is left of the scale
	switch {
	case first.Percent != nil && last.Percent != nil:
		drop = (*first.Percent - *last.Percent) / span.Hours()
		left = *last.Percent
	case first.Volts != "" && last.Volts != "":
		from, okFrom := parseVoltage(first.Volts)
		to, okTo := parseVoltage(last.Volts)
		if !okFrom || !okTo {
			return 0
		}
		drop = (from - to) / span.Hours()
		left = to - emptyVolts
	default:
		return 0
	}
	if drop <= 0 || left <= 0 {
		return 0
	}
	estimate := time.Duration(left / drop * float64(time.Hour))
	if estimate > 365*24*time.Hour {
		return 0
	}
	return estimate
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
