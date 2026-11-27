// The stored history of the dexcom plugin.
//
// The Share service answers at most 24 hours, so the plugin keeps the readings
// it has seen for five weeks. The state stores them compactly per hour: the
// minute of the reading and its value in tenths of the account unit. The last
// 24 hours keep their verbose form with the trend, so a restart has the newest
// reading as the widgets need it (ADR 0175).

package main

import (
	"math"
	"sort"
	"time"
)

const (
	// retentionDays is how long the plugin keeps readings. A profile covers at
	// most 30 days; the extra days keep a rolling window of 30 days complete.
	retentionDays = 35
	// historyScale stores a value in tenths of the account unit, so mg/dL and
	// mmol/L share one format.
	historyScale = 10
)

// retention is how far back merge keeps readings.
const retention = retentionDays * 24 * time.Hour

// histHour is one hour of the stored history. Minutes are the minutes within
// the UTC hour; Values are the readings in tenths of the account unit. The
// parallel arrays keep the state small.
type histHour struct {
	Hour    int64 `json:"h"`
	Minutes []int `json:"m,omitempty"`
	Values  []int `json:"v"`
}

// toHistory groups the readings by UTC hour for the state.
func toHistory(rs []reading) []histHour {
	var out []histHour
	for _, r := range rs {
		at := r.at.UTC()
		hour := at.Truncate(time.Hour).Unix()
		if len(out) == 0 || out[len(out)-1].Hour != hour {
			out = append(out, histHour{Hour: hour})
		}
		last := &out[len(out)-1]
		last.Minutes = append(last.Minutes, at.Minute())
		last.Values = append(last.Values, scaleValue(r.value))
	}
	return out
}

// fromHistory rebuilds the readings. A missing minute list falls back to the
// five-minute grid of the sensor.
func fromHistory(list []histHour) []reading {
	var out []reading
	for _, h := range list {
		for i, v := range h.Values {
			minute := i * 5
			if i < len(h.Minutes) {
				minute = h.Minutes[i]
			}
			at := time.Unix(h.Hour+int64(minute)*60, 0).UTC()
			out = append(out, reading{at: at, value: unscaleValue(v)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	return out
}

// dayReadings is the last 24 hours. The state keeps more, so the summary values
// of the day filter the series.
func dayReadings(rs []reading, now time.Time) []reading {
	cut := now.Add(-24 * time.Hour)
	i := sort.Search(len(rs), func(i int) bool { return !rs[i].at.Before(cut) })
	return rs[i:]
}

func scaleValue(v float64) int {
	return int(math.Round(v * historyScale))
}

func unscaleValue(v int) float64 {
	return float64(v) / historyScale
}
