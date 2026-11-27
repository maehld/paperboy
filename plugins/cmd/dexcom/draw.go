// Drawing of the dexcom widgets.

package main

import (
	"fmt"
	"math"
	"time"

	"github.com/maehld/paperboy/sdk/format"
	"github.com/maehld/paperboy/sdk/plugin"
)

// limits are the numbers the app uses: the urgent low, the target range, the
// tight range inside it and the scale of the chart.
type limits struct {
	unit                                             string
	urgent, low, high, veryHigh, tightLow, tightHigh float64
	min, max                                         float64
	decimals                                         int
}

// defaults are the consensus ranges, per unit: time in range (TIR) covers 70
// to 180 mg/dL or 3.9 to 10.0 mmol/L, time in tight range (TITR) 70 to 140
// mg/dL or 3.9 to 7.8 mmol/L.
func defaults(unit string) limits {
	if unit == unitMMOL {
		return limits{unit: unitMMOL, urgent: 3.1, low: 3.9, high: 10.0, veryHigh: 13.9,
			tightLow: 3.9, tightHigh: 7.8, min: 2.2, max: 16.7, decimals: 1}
	}
	return limits{unit: unitMGDL, urgent: 55, low: 70, high: 180, veryHigh: 250,
		tightLow: 70, tightHigh: 140, min: 40, max: 300}
}

// check verifies the order of the numbers: the tight range lies inside the
// target range.
func (l limits) check() error {
	switch {
	case l.min >= l.low:
		return fmt.Errorf("thresholds: min %g is not below low %g", l.min, l.low)
	case l.low > l.tightLow:
		return fmt.Errorf("thresholds: tight_low %g is below low %g", l.tightLow, l.low)
	case l.tightLow >= l.tightHigh:
		return fmt.Errorf("thresholds: tight_low %g is not below tight_high %g", l.tightLow, l.tightHigh)
	case l.tightHigh > l.high:
		return fmt.Errorf("thresholds: tight_high %g is above high %g", l.tightHigh, l.high)
	case l.high > l.max:
		return fmt.Errorf("thresholds: high %g is above max %g", l.high, l.max)
	case l.veryHigh != 0 && l.veryHigh <= l.high:
		return fmt.Errorf("thresholds: very_high %g is not above high %g", l.veryHigh, l.high)
	case l.veryHigh > l.max:
		return fmt.Errorf("thresholds: very_high %g is above max %g", l.veryHigh, l.max)
	case l.urgent != 0 && l.urgent >= l.low:
		return fmt.Errorf("thresholds: urgent %g is not below low %g", l.urgent, l.low)
	}
	return nil
}

// limits merges the configuration over the defaults of the account's unit.
func (d *dexcom) limits() (limits, error) {
	d.mu.Lock()
	unit, set := d.unit, make(map[string]float64, len(d.set))
	for k, v := range d.set {
		set[k] = v
	}
	var newest float64
	if len(d.readings) > 0 {
		newest = d.readings[len(d.readings)-1].value
	}
	d.mu.Unlock()
	if unit == "" {
		unit = detectUnit(newest)
	}
	l := apply(defaults(unit), set)
	return l, l.check()
}

// apply merges the configured thresholds over the defaults. A tight range the
// configuration does not name follows the target range: its lower edge stays
// the lower edge of the range, and its upper edge stays inside it.
func apply(l limits, set map[string]float64) limits {
	if v, ok := set["urgent"]; ok {
		l.urgent = v
	}
	if v, ok := set["low"]; ok {
		l.low = v
	}
	if v, ok := set["high"]; ok {
		l.high = v
	}
	if v, ok := set["very_high"]; ok {
		l.veryHigh = v
	} else if l.veryHigh <= l.high {
		// Without a threshold above the target range the report merges the
		// high and the very high readings.
		l.veryHigh = 0
	}
	if v, ok := set["tight_low"]; ok {
		l.tightLow = v
	} else {
		l.tightLow = l.low
	}
	if v, ok := set["tight_high"]; ok {
		l.tightHigh = v
	} else if l.tightHigh > l.high {
		l.tightHigh = l.high
	}
	if v, ok := set["min"]; ok {
		l.min = v
	}
	if v, ok := set["max"]; ok {
		l.max = v
	}
	return l
}

// detectUnit tells the units apart by magnitude: a reading in mmol/L stays
// below 30, one in mg/dL above 40.
func detectUnit(value float64) string {
	if value > 0 && value < 30 {
		return unitMMOL
	}
	return unitMGDL
}

// ---- data ----

// snapshot copies the readings, newest last.
func (d *dexcom) snapshot() ([]reading, error, time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]reading, len(d.readings))
	copy(out, d.readings)
	return out, d.loadErr, d.fetched
}

// current is the newest reading and its age.
func current(rs []reading, now time.Time) (reading, bool) {
	if len(rs) == 0 {
		return reading{}, false
	}
	last := rs[len(rs)-1]
	return last, true
}

// earlier is the reading closest to minutes before the newest one.
func earlier(rs []reading, minutes int) (reading, bool) {
	if len(rs) < 2 {
		return reading{}, false
	}
	want := rs[len(rs)-1].at.Add(-time.Duration(minutes) * time.Minute)
	best, bestGap := rs[0], absDuration(rs[0].at.Sub(want))
	for _, r := range rs {
		if gap := absDuration(r.at.Sub(want)); gap < bestGap {
			best, bestGap = r, gap
		}
	}
	return best, true
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// stats covers the stored readings.
type stats struct {
	min, max, avg, inBand, tight, cv, gmi float64
	count                                 int
}

func summarize(rs []reading, l limits) stats {
	var s stats
	if len(rs) == 0 {
		return s
	}
	sum, sumSq, in, tight := 0.0, 0.0, 0, 0
	s.min, s.max = rs[0].value, rs[0].value
	for _, r := range rs {
		sum += r.value
		sumSq += r.value * r.value
		if r.value < s.min {
			s.min = r.value
		}
		if r.value > s.max {
			s.max = r.value
		}
		if r.value >= l.low && r.value <= l.high {
			in++
		}
		if r.value >= l.tightLow && r.value <= l.tightHigh {
			tight++
		}
	}
	s.count = len(rs)
	s.avg = sum / float64(len(rs))
	s.inBand = float64(in) / float64(len(rs)) * 100
	s.tight = float64(tight) / float64(len(rs)) * 100
	variance := max(sumSq/float64(len(rs))-s.avg*s.avg, 0)
	if s.avg > 0 {
		s.cv = math.Sqrt(variance) / s.avg * 100
	}
	// The glucose management indicator follows the mean in mg/dL.
	mean := s.avg
	if l.unit == unitMMOL {
		mean *= 18.0182
	}
	s.gmi = 3.31 + 0.02392*mean
	return s
}

// level is the alarm state of a reading.
func level(v float64, l limits) string {
	switch {
	case l.urgent != 0 && v < l.urgent:
		return "urgent"
	case v < l.low:
		return "low"
	case v > l.high:
		return "high"
	}
	return ""
}

func (d *dexcom) value(req *plugin.Request, l limits, v float64) string {
	return format.Number(v, l.decimals, req.Lang())
}

// trendArrow is the ASCII arrow of a trend, for text displays.
func trendArrow(trend string) string {
	switch trend {
	case "DoubleUp":
		return "^^"
	case "SingleUp":
		return "^"
	case "FortyFiveUp":
		return "/"
	case "Flat":
		return ">"
	case "FortyFiveDown":
		return `\`
	case "SingleDown":
		return "v"
	case "DoubleDown":
		return "vv"
	}
	return "?"
}

// trendAngle is the angle of the drawn arrow, -90 up to +90 down.
func trendAngle(trend string) (float64, int, bool) {
	switch trend {
	case "DoubleUp":
		return -90, 2, true
	case "SingleUp":
		return -90, 1, true
	case "FortyFiveUp":
		return -45, 1, true
	case "Flat":
		return 0, 1, true
	case "FortyFiveDown":
		return 45, 1, true
	case "SingleDown":
		return 90, 1, true
	case "DoubleDown":
		return 90, 2, true
	}
	return 0, 0, false
}

func ageLabel(req *plugin.Request, d time.Duration) string {
	t := func(de, en string) string { return req.T(de, en) }
	switch {
	case d < 30*time.Second:
		return t("jetzt", "now")
	case d < time.Minute:
		return fmt.Sprintf("%s %ds", t("vor", ""), int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%s %dm", t("vor", ""), int(d.Minutes()))
	default:
		return fmt.Sprintf("%s %dh%02d", t("vor", ""), int(d.Hours()), int(d.Minutes())%60)
	}
}
