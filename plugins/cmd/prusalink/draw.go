package main

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/maehld/paperboy/sdk/design"
	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

// Render draws one widget.
func (p *prusalink) Render(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	if !p.snap.loaded {
		return errors.New("the printer has not answered yet")
	}
	switch w.Widget {
	case "printer":
		return p.drawPrinter(req, w, img)
	case "progress":
		return p.drawProgress(req, w, img)
	case "temperatures":
		return p.drawTemperatures(req, w, img)
	}
	return fmt.Errorf("%w %q", plugin.ErrUnknownWidget, w.Widget)
}

// stateLabel names the printer state in the page's language.
func stateLabel(req *plugin.Request, state string) string {
	switch strings.ToUpper(state) {
	case "IDLE":
		return req.T("Bereit", "Idle")
	case "BUSY":
		return req.T("Beschäftigt", "Busy")
	case "PRINTING":
		return req.T("Druckt", "Printing")
	case "PAUSED":
		return req.T("Pausiert", "Paused")
	case "FINISHED":
		return req.T("Fertig", "Finished")
	case "STOPPED":
		return req.T("Gestoppt", "Stopped")
	case "ERROR":
		return req.T("Fehler", "Error")
	case "ATTENTION":
		return req.T("Handlung nötig", "Attention needed")
	case "READY":
		return req.T("Bereit", "Ready")
	}
	return req.T("Unbekannt", "Unknown")
}

// durationText shortens a duration to hours and minutes. A round value names
// no minutes.
func durationText(d time.Duration) string {
	m := int(d.Round(time.Minute) / time.Minute)
	if m < 1 {
		return "< 1 min"
	}
	if m < 60 {
		return fmt.Sprintf("%d min", m)
	}
	if m%60 == 0 {
		return fmt.Sprintf("%d h", m/60)
	}
	return fmt.Sprintf("%d h %d min", m/60, m%60)
}

// tempText writes the current temperature and, when it has a target, the
// target after a slash.
func tempText(cur, target *float64) string {
	if cur == nil {
		return "–"
	}
	s := strconv.Itoa(int(math.Round(*cur)))
	if target != nil && *target > 0 {
		s += "/" + strconv.Itoa(int(math.Round(*target)))
	}
	return s + " °C"
}

func (p *prusalink) drawPrinter(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	inner := img.Rect.Inset(max(w.Style.Padding, 0))
	base := max(req.TextPx(w), render.MinSize)
	if inner.Dx() < 150 || inner.Dy() < int(base*3.2) {
		return plugin.TooSmall(w)
	}
	rule := max(w.Style.Rule, 1)
	muted := design.Muted(img)
	scale := design.NewScale(req.TextPx(w), inner.Dy())
	d := p.snap
	running := d.running()

	y := inner.Min.Y
	headerSize := scale.Detail
	stateWidth := inner.Dx()
	if running {
		if rem, ok := d.remaining(); ok {
			txt := durationText(rem)
			remSize := scale.Tiny
			tw := render.Measure(txt, remSize, true)
			if tw+int(base*0.5) < inner.Dx()/2 {
				render.Text(img, txt, inner.Max.X, y+int(remSize), render.Style{Size: remSize, Bold: true}, tw+4, render.Black, render.Right)
				stateWidth -= tw + int(base*0.5)
			}
		}
	}
	render.Text(img, stateLabel(req, d.state()), inner.Min.X, y+int(headerSize), render.Style{Size: headerSize, Bold: true, Shrink: true}, stateWidth, render.Black, render.Left)
	y += int(headerSize * 1.4)

	small := scale.Tiny
	if msg := d.message(); msg != "" && inner.Max.Y-y > int(small*3) {
		render.Text(img, render.Ellipsize(msg, small, false, inner.Dx()), inner.Min.X, y+int(small), render.Style{Size: small}, inner.Dx(), muted, render.Left)
		y += int(small * 1.3)
	}
	if name := d.jobName(); name != "" && inner.Max.Y-y > int(small*4) {
		render.Text(img, render.Ellipsize(name, small, false, inner.Dx()), inner.Min.X, y+int(small), render.Style{Size: small}, inner.Dx(), muted, render.Left)
		y += int(small * 1.3)
	} else if !running && inner.Max.Y-y > int(small*4) {
		render.Text(img, req.T("kein Auftrag", "no job"), inner.Min.X, y+int(small), render.Style{Size: small}, inner.Dx(), muted, render.Left)
		y += int(small * 1.3)
	}

	// The temperatures sit at the bottom; the bar takes the middle of the
	// room that is left, so a tall cell does not collect blank space.
	rowH := int(base * 1.7)
	bottom := inner.Max.Y - rowH
	if bottom < y || rowH < int(base) {
		return nil
	}
	if pct, ok := d.progress(); ok && running {
		barH := max(int(base*0.22), 5)
		top := y + (bottom-y-barH)/2
		if top >= y {
			drawBar(img, image.Rect(inner.Min.X, top, inner.Max.X, top+barH), pct/100, rule)
		}
	}
	tempRow(img, image.Rect(inner.Min.X, bottom, inner.Min.X+inner.Dx()/2, inner.Max.Y), scale, muted,
		req.T("Düse", "Nozzle"), d.status.Printer.TempNozzle, d.status.Printer.TargetNozzle)
	tempRow(img, image.Rect(inner.Min.X+inner.Dx()/2, bottom, inner.Max.X, inner.Max.Y), scale, muted,
		req.T("Bett", "Bed"), d.status.Printer.TempBed, d.status.Printer.TargetBed)
	return nil
}

// tempRow draws one label with its temperature, either label above value or on
// one line when the widget is flat.
func tempRow(img *image.Paletted, r image.Rectangle, scale design.Scale, muted color.Color, label string, cur, target *float64) {
	if r.Dx() < 40 || r.Dy() < int(scale.Body) {
		return
	}
	labelSize := scale.Tiny
	valueSize := scale.Detail
	value := tempText(cur, target)
	if r.Dy() >= int(scale.Body*1.7) {
		render.Text(img, label, r.Min.X, r.Min.Y+int(labelSize), render.Style{Size: labelSize}, r.Dx(), muted, render.Left)
		render.Text(img, value, r.Min.X, r.Min.Y+int(labelSize)+int(valueSize*1.05), render.Style{Size: valueSize, Bold: true, Shrink: true}, r.Dx(), render.Black, render.Left)
		return
	}
	render.Text(img, label, r.Min.X, r.Min.Y+int(valueSize), render.Style{Size: labelSize}, r.Dx()/2, muted, render.Left)
	render.Text(img, value, r.Max.X, r.Min.Y+int(valueSize), render.Style{Size: valueSize, Bold: true, Shrink: true}, r.Dx()/2, render.Black, render.Right)
}

func (p *prusalink) drawProgress(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	inner := img.Rect.Inset(max(w.Style.Padding, 0))
	if inner.Dx() < 130 || inner.Dy() < 90 {
		return plugin.TooSmall(w)
	}
	d := p.snap
	muted := design.Muted(img)
	scale := design.NewScale(req.TextPx(w), inner.Dy())

	if !d.running() {
		bigSize := max(min(scale.Title, float64(inner.Dy())*0.32), render.MinSize)
		mid := inner.Min.Y + inner.Dy()/2
		render.Text(img, stateLabel(req, d.state()), inner.Min.X+inner.Dx()/2, mid,
			render.Style{Size: bigSize, Bold: true, Shrink: true}, inner.Dx(), render.Black, render.Center)
		sub := d.jobName()
		if sub == "" {
			sub = req.T("kein Auftrag", "no job")
		}
		subSize := scale.Tiny
		render.Text(img, render.Ellipsize(sub, subSize, false, inner.Dx()), inner.Min.X+inner.Dx()/2, mid+int(subSize*1.6),
			render.Style{Size: subSize}, inner.Dx(), muted, render.Center)
		return nil
	}

	pct, ok := d.progress()
	if !ok {
		pct = 0
	}
	size := min(inner.Dy(), inner.Dx()*3/5)
	diameter := float64(size)
	cx := float64(inner.Min.X) + diameter/2
	cy := float64(inner.Min.Y) + float64(inner.Dy())/2
	outer := diameter/2 - 2
	thickness := max(diameter*0.14, 4)
	if outer-thickness < 4 {
		return plugin.TooSmall(w)
	}
	if render.Levels(img.Palette) >= 4 {
		fillRing(img, cx, cy, outer-thickness, outer, -math.Pi/2, -math.Pi/2+2*math.Pi, render.LightGray)
	} else {
		fillRing(img, cx, cy, outer-2, outer, -math.Pi/2, -math.Pi/2+2*math.Pi, render.Black)
	}
	fillRing(img, cx, cy, outer-thickness, outer, -math.Pi/2, -math.Pi/2+2*math.Pi*float64(min(max(pct, 0), 100))/100, render.Black)
	if diameter >= 56 {
		pctSize := max(min(scale.Hero, diameter*0.28), render.MinSize)
		render.Text(img, strconv.Itoa(int(math.Round(pct))), int(cx), int(cy)+int(pctSize*0.28),
			render.Style{Size: pctSize, Bold: true, Shrink: true}, int(diameter*0.7), render.Black, render.Center)
		unitSize := max(min(scale.Detail, pctSize*0.5), render.MinSize)
		render.Text(img, "%", int(cx), int(cy)+int(pctSize*0.28)+int(unitSize*1.4),
			render.Style{Size: unitSize, Bold: true}, int(diameter*0.7), render.Black, render.Center)
	}

	x := inner.Min.X + int(diameter) + int(diameter*0.12)
	width := inner.Max.X - x
	if width < 50 {
		return nil
	}
	type line struct {
		text  string
		size  float64
		bold  bool
		color color.Color
	}
	lines := []line{{stateLabel(req, d.state()), scale.Detail, true, render.Black}}
	if rem, ok := d.remaining(); ok {
		lines = append(lines, line{durationText(rem), scale.Detail, true, render.Black})
	}
	if name := d.jobName(); name != "" {
		lines = append(lines, line{render.Ellipsize(name, scale.Tiny, false, width), scale.Tiny, false, muted})
	}
	height := 0
	for _, l := range lines {
		height += int(l.size * 1.35)
	}
	y := inner.Min.Y + (inner.Dy()-height)/2
	for _, l := range lines {
		y += int(l.size * 1.05)
		render.Text(img, l.text, x, y, render.Style{Size: l.size, Bold: l.bold}, width, l.color, render.Left)
		y += int(l.size * 0.3)
	}
	return nil
}

func (p *prusalink) drawTemperatures(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	inner := img.Rect.Inset(max(w.Style.Padding, 0))
	base := max(req.TextPx(w), render.MinSize)
	if inner.Dx() < 140 || inner.Dy() < int(base*2.6) {
		return plugin.TooSmall(w)
	}
	rule := max(w.Style.Rule, 1)
	muted := design.Muted(img)
	scale := design.NewScale(req.TextPx(w), inner.Dy())
	d := p.snap
	rows := []struct {
		label  string
		cur    *float64
		target *float64
		max    float64
	}{
		{req.T("Düse", "Nozzle"), d.status.Printer.TempNozzle, d.status.Printer.TargetNozzle, 300},
		{req.T("Bett", "Bed"), d.status.Printer.TempBed, d.status.Printer.TargetBed, 120},
	}
	rowH := inner.Dy() / len(rows)
	labelSize := scale.Detail
	valueSize := max(min(scale.Title, float64(rowH)*0.35), render.MinSize)
	barH := max(int(scale.Body*0.22), 5)
	for i, row := range rows {
		top := inner.Min.Y + i*rowH
		group := int(valueSize) + int(base*0.3) + barH
		start := top + max((rowH-group)/2, 0)
		render.Text(img, row.label, inner.Min.X, start+int(labelSize), render.Style{Size: labelSize}, inner.Dx(), muted, render.Left)
		render.Text(img, tempText(row.cur, row.target), inner.Max.X, start+int(valueSize), render.Style{Size: valueSize, Bold: true, Shrink: true}, inner.Dx(), render.Black, render.Right)
		barTop := start + int(valueSize) + int(base*0.3)
		var value, tick float64
		if row.cur != nil {
			value = *row.cur
		}
		if row.target != nil {
			tick = *row.target
		}
		drawGauge(img, image.Rect(inner.Min.X, barTop, inner.Max.X, barTop+barH), value, tick, row.max, rule)
	}
	return nil
}

// drawBar draws an outlined bar with its filled part. A zero ratio shows the
// empty track.
func drawBar(img *image.Paletted, r image.Rectangle, ratio float64, rule int) {
	if r.Dx() <= 2 || r.Dy() <= 2 {
		return
	}
	drawFrame(img, r, rule)
	inner := image.Rect(r.Min.X+rule, r.Min.Y+rule, r.Max.X-rule, r.Max.Y-rule)
	width := int(float64(inner.Dx()) * min(max(ratio, 0), 1))
	if width <= 0 {
		return
	}
	fillPart(img, image.Rect(inner.Min.X, inner.Min.Y, inner.Min.X+width, inner.Max.Y))
}

// drawGauge draws a scale from zero to max: the value fills the bar and the
// target is a tick, so a heating nozzle is visible before it reaches the
// target.
func drawGauge(img *image.Paletted, r image.Rectangle, value, target, full float64, rule int) {
	if r.Dx() <= 4 || r.Dy() <= 2 || full <= 0 {
		return
	}
	drawFrame(img, r, rule)
	inner := image.Rect(r.Min.X+rule, r.Min.Y+rule, r.Max.X-rule, r.Max.Y-rule)
	if inner.Dx() <= 0 || inner.Dy() <= 0 {
		return
	}
	if width := int(float64(inner.Dx()) * min(max(value/full, 0), 1)); width > 0 {
		fillPart(img, image.Rect(inner.Min.X, inner.Min.Y, inner.Min.X+width, inner.Max.Y))
	}
	if target > 0 {
		x := inner.Min.X + int(float64(inner.Dx())*min(max(target/full, 0), 1))
		x = min(max(x, inner.Min.X), inner.Max.X-max(rule, 2))
		render.FillColor(img, image.Rect(x, r.Min.Y, x+max(rule, 2), r.Max.Y), render.Black)
	}
}

func drawFrame(img *image.Paletted, r image.Rectangle, rule int) {
	render.HLine(img, r.Min.X, r.Min.Y, r.Max.X, rule)
	render.HLine(img, r.Min.X, r.Max.Y-rule, r.Max.X, rule)
	render.VLine(img, r.Min.X, r.Min.Y, r.Max.Y, rule)
	render.VLine(img, r.Max.X-rule, r.Min.Y, r.Max.Y, rule)
}

// fillPart fills the inside of a bar. A one-bit panel gets rows instead of a
// gray tone, so the filled part stays visible.
func fillPart(img *image.Paletted, r image.Rectangle) {
	if render.Levels(img.Palette) >= 4 {
		render.FillColor(img, r, render.DarkGray)
		return
	}
	render.Fill(img, r, "shade")
}

// fillRing fills the ring between two radii from one angle to another, in
// radians, clockwise on the screen. It draws no anti-aliasing: the palette of
// a display has no more than 16 tones.
func fillRing(img *image.Paletted, cx, cy, inner, outer, from, to float64, c color.Color) {
	if outer <= inner || to <= from {
		return
	}
	index := uint8(img.Palette.Index(c))
	x0 := max(int(math.Floor(cx-outer)), img.Rect.Min.X)
	x1 := min(int(math.Ceil(cx+outer)), img.Rect.Max.X-1)
	y0 := max(int(math.Floor(cy-outer)), img.Rect.Min.Y)
	y1 := min(int(math.Ceil(cy+outer)), img.Rect.Max.Y-1)
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			d := math.Hypot(dx, dy)
			if d < inner || d > outer {
				continue
			}
			a := math.Atan2(dy, dx)
			for a < from {
				a += 2 * math.Pi
			}
			for a >= from+2*math.Pi {
				a -= 2 * math.Pi
			}
			if a <= to {
				img.SetColorIndex(x, y, index)
			}
		}
	}
}
