// The ambulatory glucose profile (AGP): the readings of several days folded
// onto one 24-hour grid.

package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"sort"
	"time"

	"github.com/maehld/paperboy/sdk/design"
	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

const (
	// agpBins is the number of half-hour slots of the profile.
	agpBins = 48
	// agpBinMinutes is the width of one slot in minutes of the day.
	agpBinMinutes = 24 * 60 / agpBins
	// agpBandDays is the number of days a slot needs to draw its percentiles.
	agpBandDays = 3
	// agpMinDays and agpMaxDays bound the window the widget accepts.
	agpMinDays = 1
	agpMaxDays = 30
)

// agpSlot is one half-hour slot of the profile, folded over the days.
type agpSlot struct {
	p5, q1, median, q3, p95 float64
	days                    int
}

// agpProfile is the folded series and the days it covers.
type agpProfile struct {
	slots    [agpBins]agpSlot
	days     int // days the readings cover, not the calendar dates
	readings int
}

// buildAGP folds the readings of the last days onto the 24-hour grid. A slot
// with readings of fewer than agpMinDays days stays empty, so a short history
// draws no invented band.
func buildAGP(rs []reading, now time.Time, days int, loc *time.Location, l limits) agpProfile {
	var p agpProfile
	start := now.Add(-time.Duration(days) * 24 * time.Hour)
	type bucket struct {
		values      []float64
		first, last time.Time
	}
	var buckets [agpBins]bucket
	window := make([]reading, 0, len(rs))
	for _, r := range rs {
		if r.at.Before(start) || r.at.After(now) {
			continue
		}
		window = append(window, r)
		local := r.at.In(loc)
		slot := min((local.Hour()*60+local.Minute())*agpBins/(24*60), agpBins-1)
		b := &buckets[slot]
		b.values = append(b.values, r.value)
		if b.first.IsZero() || r.at.Before(b.first) {
			b.first = r.at
		}
		if r.at.After(b.last) {
			b.last = r.at
		}
	}
	p.readings = len(window)
	p.days = coveredDays(window, now, days)
	for i := range buckets {
		b := &buckets[i]
		if len(b.values) == 0 {
			continue
		}
		days := 1 + int(b.last.Sub(b.first)/(24*time.Hour))
		sort.Float64s(b.values)
		p.slots[i] = agpSlot{
			p5:     percentile(b.values, 0.05),
			q1:     percentile(b.values, 0.25),
			median: percentile(b.values, 0.50),
			q3:     percentile(b.values, 0.75),
			p95:    percentile(b.values, 0.95),
			days:   days,
		}
	}
	return p
}

// percentile takes the linearly interpolated percentile of a sorted series.
func percentile(sorted []float64, p float64) float64 {
	switch len(sorted) {
	case 0:
		return 0
	case 1:
		return sorted[0]
	}
	pos := p * float64(len(sorted)-1)
	i := int(math.Floor(pos))
	if i+1 >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	return sorted[i] + (pos-float64(i))*(sorted[i+1]-sorted[i])
}

// coveredDays counts the 24-hour periods of the window that have a reading, so
// a profile with gaps names the days it really covers.
func coveredDays(rs []reading, now time.Time, days int) int {
	seen := make([]bool, days)
	for _, r := range rs {
		if i := int(now.Sub(r.at) / (24 * time.Hour)); i >= 0 && i < days {
			seen[i] = true
		}
	}
	n := 0
	for _, ok := range seen {
		if ok {
			n++
		}
	}
	return n
}

// hasReadings reports whether the slot has a median to draw.
func (s agpSlot) hasReadings() bool { return s.days > 0 }

// hasBand reports whether the slot has enough days for percentiles.
func (s agpSlot) hasBand() bool { return s.days >= agpBandDays }

func (d *dexcom) agpWidget(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	l, err := d.limits()
	if err != nil {
		return err
	}
	rs, loadErr, _ := d.snapshot()
	if loadErr != nil {
		return loadErr
	}
	b := img.Rect.Inset(w.Style.Padding)
	if b.Dx() < 200 || b.Dy() < 110 {
		return plugin.TooSmall(w)
	}
	days := w.Arg().Int("days", 14)
	if days < agpMinDays || days > agpMaxDays {
		return fmt.Errorf("days: %d is outside %d..%d", days, agpMinDays, agpMaxDays)
	}
	scale := design.NewScale(req.TextPx(w), b.Dy())
	p := buildAGP(rs, req.Now, days, req.Location(), l)
	if p.readings == 0 {
		design.Notice(img, b, req.T("keine Daten", "no data"), scale)
		return nil
	}

	titleSize := max(min(scale.Detail, float64(b.Dy())*0.15), render.MinSize)
	labelSize := max(min(scale.Micro, titleSize*0.75), render.MinSize)
	left := render.Measure(fmt.Sprintf("%.0f", l.max), labelSize, false) + 6
	plot := image.Rect(b.Min.X+left, b.Min.Y+int(titleSize*1.6),
		b.Max.X, b.Max.Y-int(labelSize*1.9))
	if plot.Dx() < 60 || plot.Dy() < 40 {
		return plugin.TooSmall(w)
	}

	xAt := func(minute int) int {
		return plot.Min.X + minute*(plot.Dx()-1)/(24*60)
	}
	yAt := func(v float64) int {
		f := (v - l.min) / (l.max - l.min)
		f = min(max(f, 0), 1)
		return plot.Max.Y - 1 - int(math.Round(f*float64(plot.Dy()-1)))
	}

	// The reference lines sit below the bands: the profile stays the subject.
	hline(img, plot.Min.X, plot.Max.X, yAt(l.low), 1, mid)
	hline(img, plot.Min.X, plot.Max.X, yAt(l.high), 1, mid)
	if l.tightLow > l.low {
		dashed(img, plot.Min.X, plot.Max.X, yAt(l.tightLow), mid)
	}
	if l.tightHigh < l.high {
		dashed(img, plot.Min.X, plot.Max.X, yAt(l.tightHigh), mid)
	}

	// One column per slot: the outer band first, the interquartile band over
	// it. A slot with fewer than agpBandDays days has no band.
	for i, s := range p.slots {
		if !s.hasBand() {
			continue
		}
		x0 := xAt(i * agpBinMinutes)
		x1 := xAt((i + 1) * agpBinMinutes)
		if i == agpBins-1 {
			x1 = plot.Max.X
		}
		x1 = max(x1, x0+1)
		bandFill(img, image.Rect(x0, yAt(s.p95), x1, yAt(s.p5)+1), false)
		bandFill(img, image.Rect(x0, yAt(s.q3), x1, yAt(s.q1)+1), true)
	}

	// The median as one line through the middle of the slots; a gap breaks it.
	// With one day it is the curve of that day, without bands. A one-bit panel
	// gets a white edge, so the line stays apart from the rows of the inner
	// band.
	width := max(2, plot.Dy()/160)
	prevX, prevY, joined := 0, 0, false
	for i, s := range p.slots {
		if !s.hasReadings() {
			joined = false
			continue
		}
		cx, cy := xAt(i*agpBinMinutes+agpBinMinutes/2), yAt(s.median)
		if joined {
			if len(img.Palette) < 4 {
				line(img, prevX, prevY, cx, cy, width+2, color.Gray{255})
			}
			line(img, prevX, prevY, cx, cy, width, ink)
		}
		prevX, prevY, joined = cx, cy, true
	}

	// Axis, scale labels and hour marks.
	hline(img, plot.Min.X, plot.Max.X, plot.Max.Y, 1, ink)
	render.TextAt(img, d.value(req, l, l.low), plot.Min.X-4, yAt(l.low)+int(labelSize*0.4),
		labelSize, false, mid, render.Right)
	render.TextAt(img, d.value(req, l, l.high), plot.Min.X-4, yAt(l.high)+int(labelSize*0.4),
		labelSize, false, mid, render.Right)
	for hour := 0; hour <= 24; hour += 6 {
		x := min(max(xAt(hour*60), plot.Min.X), plot.Max.X-1)
		fill(img, image.Rect(x, plot.Max.Y+2, x+1, plot.Max.Y+6), mid)
		render.TextAt(img, fmt.Sprintf("%02d", hour), x, plot.Max.Y+int(labelSize)+4,
			labelSize, false, mid, render.Center)
	}

	title := fmt.Sprintf("AGP · %s", daysLabel(req, p.days, days))
	render.TextAt(img, title, b.Min.X, b.Min.Y+int(titleSize), titleSize, true, ink, render.Left)
	return nil
}

// daysLabel names the days a profile covers out of its window, so a gap shows
// as 10/14 rather than as fourteen days.
func daysLabel(req *plugin.Request, covered, window int) string {
	switch {
	case window == 1:
		return req.T("1 Tag", "1 day")
	case covered >= window:
		return fmt.Sprintf(req.T("%d Tage", "%d days"), window)
	}
	return fmt.Sprintf(req.T("%d/%d Tage", "%d/%d days"), covered, window)
}

// bandFill fills one percentile band. The four-tone palette takes the two
// grays; a one-bit panel takes row patterns, so the bands keep their order.
func bandFill(img *image.Paletted, r image.Rectangle, inner bool) {
	if r.Empty() {
		return
	}
	if len(img.Palette) >= 4 {
		c := light
		if inner {
			c = mid
		}
		render.FillColor(img, r, c)
		return
	}
	step := 2
	if !inner {
		step = 4
	}
	for y := r.Min.Y; y < r.Max.Y; y += step {
		fill(img, image.Rect(r.Min.X, y, r.Max.X, y+1), ink)
	}
}

// dashed draws a horizontal line of short dashes.
func dashed(img *image.Paletted, x0, x1, y int, c color.Color) {
	for x := x0; x < x1; x += 6 {
		fill(img, image.Rect(x, y, min(x+3, x1), y+1), c)
	}
}
