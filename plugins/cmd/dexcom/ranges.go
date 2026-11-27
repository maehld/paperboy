// The time in ranges: the stacked bar of the ambulatory glucose report.

package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"time"

	"github.com/maehld/paperboy/sdk/design"
	"github.com/maehld/paperboy/sdk/format"
	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

// rangeKind is one segment of the bar, from the very low to the very high end.
type rangeKind int

const (
	rangeVeryLow rangeKind = iota
	rangeLow
	rangeTarget
	rangeHigh
	rangeVeryHigh
)

// rangeShare is one segment of the time in ranges.
type rangeShare struct {
	label string
	share float64
	kind  rangeKind
}

// rangeShares counts the readings of the window in the ranges of the AGP
// report. A disabled urgent low or very high merges its range into the next
// one, so the shares always add up to the whole window.
func rangeShares(rs []reading, now time.Time, days int, l limits, lang string) ([]rangeShare, int) {
	start := now.Add(-time.Duration(days) * 24 * time.Hour)
	window := make([]reading, 0, len(rs))
	var total, veryLow, low, target, high, veryHigh int
	for _, r := range rs {
		if r.at.Before(start) || r.at.After(now) {
			continue
		}
		total++
		window = append(window, r)
		switch {
		case l.urgent != 0 && r.value < l.urgent:
			veryLow++
		case r.value < l.low:
			low++
		case r.value <= l.high:
			target++
		case l.veryHigh != 0 && r.value <= l.veryHigh:
			high++
		default:
			veryHigh++
		}
	}
	if total == 0 {
		return nil, 0
	}
	share := func(n int) float64 { return float64(n) / float64(total) * 100 }

	// The report labels a range with its first and its last value. The step is
	// the precision of the unit, so a mmol/L account gets 3.0–3.8 rather than
	// 3.0–3.9.
	step := 1.0
	if l.decimals > 0 {
		step = 0.1
	}
	num := func(v float64) string { return format.Number(v, l.decimals, lang) }
	covered := coveredDays(window, now, days)

	var out []rangeShare
	if l.urgent != 0 && l.urgent < l.low {
		out = append(out,
			rangeShare{label: "<" + num(l.urgent), share: share(veryLow), kind: rangeVeryLow},
			rangeShare{label: num(l.urgent) + "–" + num(l.low-step), share: share(low), kind: rangeLow})
	} else {
		out = append(out, rangeShare{label: "<" + num(l.low), share: share(low + veryLow), kind: rangeLow})
	}
	out = append(out, rangeShare{label: num(l.low) + "–" + num(l.high), share: share(target), kind: rangeTarget})
	if l.veryHigh != 0 {
		out = append(out,
			rangeShare{label: num(l.high+step) + "–" + num(l.veryHigh), share: share(high), kind: rangeHigh},
			rangeShare{label: ">" + num(l.veryHigh), share: share(veryHigh), kind: rangeVeryHigh})
	} else {
		out = append(out, rangeShare{label: ">" + num(l.high), share: share(high + veryHigh), kind: rangeHigh})
	}
	return out, covered
}

func (d *dexcom) rangesWidget(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	l, err := d.limits()
	if err != nil {
		return err
	}
	rs, loadErr, _ := d.snapshot()
	if loadErr != nil {
		return loadErr
	}
	b := img.Rect.Inset(w.Style.Padding)
	if b.Dx() < 160 || b.Dy() < 36 {
		return plugin.TooSmall(w)
	}
	days := w.Arg().Int("days", 14)
	if days < agpMinDays || days > agpMaxDays {
		return fmt.Errorf("days: %d is outside %d..%d", days, agpMinDays, agpMaxDays)
	}
	scale := design.NewScale(req.TextPx(w), b.Dy())
	shares, covered := rangeShares(rs, req.Now, days, l, req.Lang())
	if len(shares) == 0 {
		design.Notice(img, b, req.T("keine Daten", "no data"), scale)
		return nil
	}

	titleSize := max(min(scale.Detail, float64(b.Dy())*0.16), render.MinSize)
	labelSize := max(min(scale.Micro, float64(b.Dy())*0.22), render.MinSize)

	// A short cell keeps the bar and the legend; the page names the context.
	body := b
	if b.Dy() >= 78 {
		name := req.T("Zeit in Bereichen", "Time in ranges")
		if b.Dx() < 400 {
			name = req.T("Bereiche", "Ranges")
		}
		title := fmt.Sprintf("%s · %s", name, daysLabel(req, covered, days))
		unitW := render.Measure(l.unit, titleSize*0.8, false)
		fitted := textFit(img, title, b.Min.X, b.Min.Y+int(titleSize), titleSize, true, ink,
			render.Left, b.Dx()-unitW-int(titleSize*0.6))
		render.TextAt(img, l.unit, b.Max.X, b.Min.Y+int(titleSize), min(titleSize*0.8, fitted*0.8),
			false, mid, render.Right)
		body = image.Rect(b.Min.X, b.Min.Y+int(titleSize*1.7), b.Max.X, b.Max.Y)
	}

	// A wide cell lies the bar down, a tall one stands it up.
	if b.Dx() >= b.Dy() {
		rangesBar(img, body, shares, labelSize)
	} else {
		rangesColumn(img, body, shares, labelSize)
	}
	return nil
}

// rangesBar draws the ranges as a bar with the legend below it. The legend
// wraps when the cell is not wide enough for one row.
func rangesBar(img *image.Paletted, body image.Rectangle, shares []rangeShare, size float64) {
	cols := min(len(shares), max(body.Dx()/110, 1))
	rows := (len(shares) + cols - 1) / cols
	size = legendSize(shares, cols, body.Dx(), size)
	legendH := int(float64(rows) * size * 1.8)
	room := image.Rect(body.Min.X, body.Min.Y, body.Max.X, body.Max.Y-legendH)
	if room.Dy() < 12 {
		return
	}
	barH := min(max(int(float64(body.Dy())*0.32), 14), 44)
	barH = min(barH, room.Dy())
	top := room.Min.Y + (room.Dy()-barH)/2
	bar := image.Rect(body.Min.X, top, body.Max.X, top+barH)

	cum := 0.0
	x := bar.Min.X
	for i, s := range shares {
		cum += s.share
		x1 := bar.Max.X
		if i < len(shares)-1 {
			x1 = bar.Min.X + int(math.Round(float64(bar.Dx())*cum/100))
		}
		if x1 > x {
			fillRange(img, image.Rect(x, bar.Min.Y, x1, bar.Max.Y), s.kind)
		}
		x = x1
	}
	outline(img, bar, ink)

	// Each row of the legend is centered, with one gap between its entries.
	rowH := float64(legendH) / float64(rows)
	for row := 0; row < rows; row++ {
		list := shares[row*cols : min((row+1)*cols, len(shares))]
		x := body.Min.X + max((body.Dx()-legendRowWidth(list, size))/2, 0)
		baseline := body.Max.Y - legendH + int(float64(row)*rowH+rowH*0.72)
		for _, s := range list {
			rangeLegend(img, x, baseline, s, size)
			x += legendWidth(s, size) + legendEntryGap(size)
		}
	}
}

// rangesColumn draws the ranges as a column with the legend beside it. The
// high end sits on top, like a value axis.
func rangesColumn(img *image.Paletted, body image.Rectangle, shares []rangeShare, size float64) {
	barW := min(max(int(float64(body.Dx())*0.22), 16), 56)
	gap := max(int(size*0.8), 8)
	bar := image.Rect(body.Min.X, body.Min.Y, body.Min.X+barW, body.Max.Y)
	legend := image.Rect(bar.Max.X+gap, body.Min.Y, body.Max.X, body.Max.Y)
	if legend.Dx() < 40 || bar.Dy() < 5*len(shares) {
		return
	}

	cum := 0.0
	y := bar.Min.Y
	for i := len(shares) - 1; i >= 0; i-- {
		cum += shares[i].share
		y1 := bar.Max.Y
		if i > 0 {
			y1 = bar.Min.Y + int(math.Round(float64(bar.Dy())*cum/100))
		}
		if y1 > y {
			fillRange(img, image.Rect(bar.Min.X, y, bar.Max.X, y1), shares[i].kind)
		}
		y = y1
	}
	outline(img, bar, ink)

	rowH := float64(legend.Dy()) / float64(len(shares))
	size = legendSize(shares, 1, legend.Dx(), min(size, rowH*0.6))
	for i := len(shares) - 1; i >= 0; i-- {
		s := shares[i]
		row := len(shares) - 1 - i
		baseline := legend.Min.Y + int(float64(row)*rowH+rowH*0.72)
		rangeLegend(img, legend.Min.X, baseline, s, size)
	}
}

// legendSize returns the largest size at which every row of cols entries fits
// width, so all entries of one legend share a size.
func legendSize(shares []rangeShare, cols, width int, size float64) float64 {
	size = max(size, render.MinSize)
	cols = max(cols, 1)
	for size > render.MinSize {
		fits := true
		for from := 0; from < len(shares); from += cols {
			if legendRowWidth(shares[from:min(from+cols, len(shares))], size) > width {
				fits = false
				break
			}
		}
		if fits {
			break
		}
		size--
	}
	return max(size, render.MinSize)
}

// legendRowWidth is the width of one row of entries with the gaps between them.
func legendRowWidth(list []rangeShare, size float64) int {
	w := 0
	for i, s := range list {
		if i > 0 {
			w += legendEntryGap(size)
		}
		w += legendWidth(s, size)
	}
	return w
}

// legendWidth is the width of one legend entry at size.
func legendWidth(s rangeShare, size float64) int {
	return int(size) + legendSpace(size) +
		render.Measure(s.label, size, false) + legendTextGap(size) +
		render.Measure(legendPercent(s), size, true)
}

// legendSpace, legendTextGap and legendEntryGap keep the swatch, the label and
// the share apart, and the entries of a row.
func legendSpace(size float64) int    { return max(int(size*0.55), 4) }
func legendTextGap(size float64) int  { return max(int(size*0.45), 3) }
func legendEntryGap(size float64) int { return max(int(size*1.3), 12) }

func legendPercent(s rangeShare) string { return fmt.Sprintf("%.0f %%", s.share) }

// rangeLegend draws one legend entry: a swatch, the range and its share.
func rangeLegend(img *image.Paletted, x0, baseline int, s rangeShare, size float64) {
	size = max(size, render.MinSize)
	box := image.Rect(x0, baseline-int(size*0.9), x0+int(size), baseline)
	fillRange(img, box, s.kind)
	outline(img, box, ink)
	x := box.Max.X + legendSpace(size)
	render.TextAt(img, s.label, x, baseline, size, false, ink, render.Left)
	x += render.Measure(s.label, size, false) + legendTextGap(size)
	render.TextAt(img, legendPercent(s), x, baseline, size, true, ink, render.Left)
}

// fillRange fills one segment of the bar. The target range stays white, the
// ranges next to it take the light fill and the extreme ranges take black, so
// the bar reads from dark at both ends to white in the target range. A one-bit
// panel turns the light fill into every other row.
func fillRange(img *image.Paletted, r image.Rectangle, kind rangeKind) {
	switch kind {
	case rangeVeryLow, rangeVeryHigh:
		fill(img, r, ink)
	case rangeLow, rangeHigh:
		render.Fill(img, r, "shade")
	}
}

// outline draws a one-pixel frame around r.
func outline(img *image.Paletted, r image.Rectangle, c color.Color) {
	if r.Dx() < 2 || r.Dy() < 2 {
		return
	}
	fill(img, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+1), c)
	fill(img, image.Rect(r.Min.X, r.Max.Y-1, r.Max.X, r.Max.Y), c)
	fill(img, image.Rect(r.Min.X, r.Min.Y, r.Min.X+1, r.Max.Y), c)
	fill(img, image.Rect(r.Max.X-1, r.Min.Y, r.Max.X, r.Max.Y), c)
}
