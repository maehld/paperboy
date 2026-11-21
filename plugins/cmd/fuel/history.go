package main

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"os"
	"time"

	"github.com/maehld/paperboy/sdk/design"
	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

// pricePoint is one change of the cheapest price. The price holds until the
// next point.
type pricePoint struct {
	At    int64   `json:"at"` // unix seconds
	Price float64 `json:"price"`
}

// history is the series of every fuel, keyed by the fuel name.
type history map[string][]pricePoint

// storedState is the plugin state Paperboy keeps (ADR 0081).
type storedState struct {
	Version int     `json:"version"`
	Key     string  `json:"key"`
	Series  history `json:"series,omitempty"`
}

// LoadState takes over the price history of the last run. A history of
// another key, position, radius or format is ignored.
func (f *fuel) LoadState(state json.RawMessage, blobs map[string][]byte) error {
	now := f.now()
	f.mu.Lock()
	f.loaded = true
	f.mu.Unlock()
	if len(state) == 0 {
		return nil
	}
	var s storedState
	if err := json.Unmarshal(state, &s); err != nil {
		fmt.Fprintln(os.Stderr, "fuel: ignoring the stored history:", err)
		return nil
	}
	if s.Version != stateVersion {
		fmt.Fprintf(os.Stderr, "fuel: ignoring a stored history of version %d\n", s.Version)
		return nil
	}
	if s.Key != f.fingerprint() {
		fmt.Fprintln(os.Stderr, "fuel: ignoring the stored history of another location or key")
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.series = history{}
	for name, list := range s.Series {
		if !isFuel(name) {
			continue
		}
		f.series[name] = list
	}
	f.trim(now)
	return nil
}

// State returns the history to store, nil when it did not change.
func (f *fuel) State() json.RawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.loaded || !f.pending {
		return nil
	}
	f.pending = false
	data, err := json.Marshal(storedState{Version: stateVersion, Key: f.fingerprint(), Series: f.series})
	if err != nil {
		return nil
	}
	return data
}

// record appends the cheapest price of every fuel when it changed and trims
// the series. It reports whether a series changed.
func (f *fuel) record(now time.Time, stations []station) bool {
	if f.series == nil {
		f.series = history{}
	}
	changed := false
	for _, name := range fuels {
		s, ok := cheapestStation(stations, name)
		if !ok {
			continue
		}
		v, _ := s.fuel(name)
		list := f.series[name]
		if n := len(list); n > 0 && list[n-1].Price == v {
			continue
		}
		f.series[name] = append(list, pricePoint{At: now.Unix(), Price: v})
		changed = true
	}
	f.trim(now)
	return changed
}

// trim drops the points before span and keeps one older point as the start of
// the step line. Callers hold f.mu.
func (f *fuel) trim(now time.Time) {
	cutoff := now.Add(-f.span).Unix()
	for name, list := range f.series {
		i := 0
		for i < len(list) && list[i].At < cutoff {
			i++
		}
		if i > 1 {
			list = list[i-1:]
		}
		if len(list) > maxPoints {
			list = list[len(list)-maxPoints:]
		}
		f.series[name] = append([]pricePoint(nil), list...)
	}
}

// copyHistory copies the series, so a render does not read the slices the
// background loop appends to.
func copyHistory(h history) history {
	out := make(history, len(h))
	for name, list := range h {
		out[name] = append([]pricePoint(nil), list...)
	}
	return out
}

// isFuel reports whether name is one of the three fuels.
func isFuel(name string) bool {
	for _, f := range fuels {
		if f == name {
			return true
		}
	}
	return false
}

// lastChange is the time of the newest stored price of a fuel.
func (f *fuel) lastChange(name string) time.Time {
	list := f.curHist[name]
	if len(list) == 0 {
		return time.Time{}
	}
	return time.Unix(list[len(list)-1].At, 0)
}

// step is one level of the stair line: the price holds from at to the next
// step.
type step struct {
	at    time.Time
	price float64
}

// steps turns the stored changes into the stair line of the window. A point
// before the window gives the price at its start; otherwise the line starts
// with the first point inside the window.
func (f *fuel) steps(name string, from, to time.Time) []step {
	list := f.curHist[name]
	var base *pricePoint
	var inside []pricePoint
	for i := range list {
		t := time.Unix(list[i].At, 0)
		if !t.After(from) {
			base = &list[i]
			continue
		}
		if t.After(to) {
			break
		}
		inside = append(inside, list[i])
	}
	var out []step
	if base != nil {
		out = append(out, step{from, base.Price})
	}
	for _, p := range inside {
		out = append(out, step{time.Unix(p.At, 0), p.Price})
	}
	if len(out) == 0 {
		return nil
	}
	out = append(out, step{to, out[len(out)-1].price})
	return out
}

// historyWidget draws the price of one fuel as a stair line. The left axis
// names the lowest and the highest price, the lower edge the time.
func (f *fuel) historyWidget(w plugin.WidgetRequest, img *image.Paletted) error {
	b := img.Rect.Inset(w.Style.Padding)
	if b.Empty() {
		return plugin.TooSmall(w)
	}
	name, err := fuelFrom(w.Arg())
	if err != nil {
		return err
	}
	span, err := w.Arg().Duration("span", 24*time.Hour)
	if err != nil {
		return err
	}
	if span < minSpan || span > maxSpan {
		return fmt.Errorf("span must be within %s..%s", minSpan, maxSpan)
	}
	// The chart never shows more than the stored history.
	span = min(span, f.span)
	now := f.curAt
	if now.IsZero() {
		now = f.req.Now
	}
	now = now.In(f.loc)
	from := now.Add(-span)
	list := f.steps(name, from, now)
	if len(list) == 0 {
		return f.message(w, img, f.t("Noch kein Verlauf", "No history yet"))
	}

	base := f.req.TextPx(w)
	s := design.NewScale(base, b.Dy())
	lo, hi := list[0].price, list[0].price
	for _, step := range list {
		lo, hi = min(lo, step.price), max(hi, step.price)
	}
	if hi-lo < 0.002 {
		lo, hi = lo-0.005, hi+0.005
	}
	lo = math.Floor(lo*100) / 100
	hi = math.Ceil(hi*100) / 100
	if hi == lo {
		hi = lo + 0.01
	}
	points := make([]design.Point, len(list))
	for i, step := range list {
		points[i] = design.Point{At: step.at, Value: step.price}
	}
	ticks := []time.Time{from, now}
	if b.Dx() > 4*render.Measure("00:00", s.Micro, false) {
		ticks = []time.Time{from, from.Add(span / 2), now}
	}
	format := "15:04"
	if span > 24*time.Hour {
		format = "02.01."
	}
	if !design.DrawTrend(img, b, design.Trend{
		From:   from,
		To:     now,
		Series: []design.Series{{Points: points, Style: design.Step, Width: 2}},
		Range:  &design.Range{Min: lo, Max: hi},
		Axis:   true,
		Grid:   true,
		Ticks:  ticks,
		TickFormat: func(t time.Time) string {
			return t.In(f.loc).Format(format)
		},
		Label:   fuelLabel(name) + " · €/l",
		Current: f.money(list[len(list)-1].price),
		Format:  f.money,
	}, s) {
		return plugin.TooSmall(w)
	}
	return nil
}
