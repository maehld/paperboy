package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"time"

	"github.com/maehld/paperboy/sdk/design"
	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

// sun draws the daylight window of today: an arc from sunrise to sunset, the
// sun glyphs at both ends, the local times below them and the day length above.
// A dot marks now. Outside daylight the day length gives way to a short note.
func (w *weather) sun(wr plugin.WidgetRequest, img *image.Paletted) error {
	b := img.Rect.Inset(wr.Style.Padding)
	if b.Empty() {
		return plugin.TooSmall(wr)
	}
	scale := design.NewScale(w.req.TextPx(wr), b.Dy())
	rise, set, ok := w.sunTimes()
	if !ok || !set.After(rise) {
		// A polar day or night has no window to draw.
		note := w.t("Polarnacht", "Polar night")
		if w.cur.Current.IsDay {
			note = w.t("Polartag", "Midnight sun")
		}
		return w.centredLine(img, b, note, scale)
	}

	timeSize := max(scale.Detail, render.MinSize)
	glyphH := max(int(min(float64(b.Dy())*0.30, scale.Hero*0.55)), 8)
	timeW := render.Measure("00:00", timeSize, false)
	inset := max(glyphH/2, timeW/2+2)
	x0, x1 := b.Min.X+inset, b.Max.X-inset
	baseline := b.Max.Y - int(timeSize*1.1)
	top := b.Min.Y + int(max(scale.Detail, render.MinSize)) + glyphH/3
	if x1-x0 < 8 || baseline-top < 4 {
		return plugin.TooSmall(wr)
	}
	ry := baseline - top

	// The half-ellipse from the left end to the right end over the baseline.
	steps := 64
	prevX, prevY := x0, baseline
	for i := 1; i <= steps; i++ {
		f := float64(i) / float64(steps)
		x := x0 + int(math.Round(float64(x1-x0)*f))
		y := baseline - int(math.Round(float64(ry)*math.Sin(math.Pi*f)))
		render.Segment(img, prevX, prevY, x, y, max(1, wr.Style.Rule), false)
		prevX, prevY = x, y
	}

	// The glyphs sit above the ends, the local times below them.
	for _, g := range []struct {
		name string
		x    int
		at   time.Time
	}{{"sunrise", x0, rise}, {"sunset", x1, set}} {
		r := image.Rect(g.x-glyphH/2, baseline-glyphH, g.x+glyphH/2, baseline)
		icon(g.name).Draw(img, r, glyphH, render.Center)
		render.Text(img, g.at.Format("15:04"), g.x, b.Max.Y, render.Style{Size: timeSize, Shrink: true},
			max(timeW*2, 1), color.Black, render.Center)
	}

	label := w.daylightText(set.Sub(rise))
	if w.req.Now.Before(rise) || w.req.Now.After(set) {
		label = w.t("Nacht", "Night")
	} else if span := set.Sub(rise); span > 0 {
		f := float64(w.req.Now.Sub(rise)) / float64(span)
		x := x0 + int(math.Round(float64(x1-x0)*f))
		y := baseline - int(math.Round(float64(ry)*math.Sin(math.Pi*f)))
		fillDot(img, x, y, max(2, glyphH/8))
	}
	render.Text(img, label, b.Min.X+b.Dx()/2, b.Min.Y+int(scale.Detail), render.Style{
		Size: max(scale.Detail, render.MinSize), Bold: true, Shrink: true,
	}, b.Dx(), color.Black, render.Center)
	return nil
}

// centredLine draws one localised line in the middle of the cell, for a polar
// day or night that has no sunrise and sunset to show.
func (w *weather) centredLine(img *image.Paletted, b image.Rectangle, text string, scale design.Scale) error {
	size := max(scale.Title, render.MinSize)
	render.Text(img, text, b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2+int(size*0.35), render.Style{
		Size: size, Bold: true, Shrink: true,
	}, b.Dx(), color.Black, render.Center)
	return nil
}

// sunTimes returns today's sunrise and sunset in the request location.
func (w *weather) sunTimes() (time.Time, time.Time, bool) {
	rise, errR := time.Parse(time.RFC3339, w.cur.Today.Sunrise)
	set, errE := time.Parse(time.RFC3339, w.cur.Today.Sunset)
	if errR != nil || errE != nil {
		return time.Time{}, time.Time{}, false
	}
	return rise.In(w.loc), set.In(w.loc), true
}

// daylight returns the length of today's daylight. A polar day counts as 24
// hours and a polar night as none.
func (w *weather) daylight() time.Duration {
	rise, set, ok := w.sunTimes()
	if ok && set.After(rise) {
		return set.Sub(rise)
	}
	if w.cur.Current.IsDay {
		return 24 * time.Hour
	}
	return 0
}

// daylightText writes a duration as "10 Std. 30 Min." or "10 h 30 min".
func (w *weather) daylightText(d time.Duration) string {
	mins := max(int(math.Round(d.Minutes())), 0)
	h, m := mins/60, mins%60
	switch {
	case h == 0:
		return fmt.Sprintf("%d %s", m, w.t("Min.", "min"))
	case m == 0:
		return fmt.Sprintf("%d %s", h, w.t("Std.", "h"))
	}
	return fmt.Sprintf("%d %s %02d %s", h, w.t("Std.", "h"), m, w.t("Min.", "min"))
}

// fillDot draws a filled circle of radius r around x, y.
func fillDot(img *image.Paletted, x, y, r int) {
	for dy := -r; dy <= r; dy++ {
		dx := int(math.Sqrt(float64(r*r - dy*dy)))
		render.FillSolid(img, image.Rect(x-dx, y+dy, x+dx+1, y+dy+1))
	}
}
