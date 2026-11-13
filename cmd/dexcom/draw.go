// Drawing of the dexcom widgets.

package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

// limits are the four numbers the app uses: the urgent low, the target band
// and the scale of the chart.
type limits struct {
	unit                        string
	urgent, low, high, min, max float64
	decimals                    int
}

// defaults are the values of the Dexcom app, per unit.
func defaults(unit string) limits {
	if unit == unitMMOL {
		return limits{unit: unitMMOL, urgent: 3.1, low: 4.4, high: 15.5, min: 2.2, max: 16.7, decimals: 1}
	}
	return limits{unit: unitMGDL, urgent: 55, low: 80, high: 280, min: 40, max: 300}
}

// check verifies the order of the numbers.
func (l limits) check() error {
	switch {
	case l.min >= l.low:
		return fmt.Errorf("thresholds: min %g is not below low %g", l.min, l.low)
	case l.low >= l.high:
		return fmt.Errorf("thresholds: low %g is not below high %g", l.low, l.high)
	case l.high > l.max:
		return fmt.Errorf("thresholds: high %g is above max %g", l.high, l.max)
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
	l := defaults(unit)
	for k, v := range set {
		switch k {
		case "urgent":
			l.urgent = v
		case "low":
			l.low = v
		case "high":
			l.high = v
		case "min":
			l.min = v
		case "max":
			l.max = v
		}
	}
	return l, l.check()
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

// stats covers the readings of the last day.
type stats struct {
	min, max, avg, inBand float64
	count                 int
}

func summarize(rs []reading, l limits) stats {
	var s stats
	if len(rs) == 0 {
		return s
	}
	sum, in := 0.0, 0
	s.min, s.max = rs[0].value, rs[0].value
	for _, r := range rs {
		sum += r.value
		if r.value < s.min {
			s.min = r.value
		}
		if r.value > s.max {
			s.max = r.value
		}
		if r.value >= l.low && r.value <= l.high {
			in++
		}
	}
	s.count = len(rs)
	s.avg = sum / float64(len(rs))
	s.inBand = float64(in) / float64(len(rs)) * 100
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

func (d *dexcom) num(req *plugin.Request, v float64, decimals int) string {
	s := strconv.FormatFloat(v, 'f', decimals, 64)
	if req.Lang() == "de" {
		s = strings.Replace(s, ".", ",", 1)
	}
	return s
}

func (d *dexcom) value(req *plugin.Request, l limits, v float64) string {
	return d.num(req, v, l.decimals)
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
