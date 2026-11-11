package main

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

// draw paints one countdown into the widget's cell: the label and the
// percentage on top, the value or the passed tick in the middle, the bar and
// the target date below. Rows are dropped in that order when the cell is too
// small; the value stays.
func draw(a *args, req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) {
	st := a.evaluate()
	inner := img.Rect.Inset(w.Style.Padding)
	if inner.Empty() {
		return
	}
	base := req.TextPx(w)
	rule := max(w.Style.Rule, 1)
	gap := max(int(math.Round(base*0.25)), 2)

	value := a.value(req, st)
	date := a.dateText(req)
	percent := ""
	if st.bar {
		percent = fmt.Sprintf("%d", int(math.Round(st.fraction*100)))
		if req.Lang() == "de" {
			percent += " %"
		} else {
			percent += "%"
		}
	}

	headerH := 0
	if a.label != "" || percent != "" {
		headerH = int(math.Round(base * 1.15))
	}
	barH := 0
	if st.bar {
		barH = max(int(math.Round(base*0.35)), rule*2, 5)
	}
	dateH := 0
	if date != "" {
		dateH = int(math.Round(base * 0.8))
	}

	dateOn, headerOn, barOn := date != "", headerH > 0, st.bar
	mainH := func() int {
		h := inner.Dy()
		if headerOn {
			h -= headerH + gap/2
		}
		if barOn {
			h -= barH + gap/2
		}
		if dateOn {
			h -= dateH + gap/2
		}
		return h
	}
	minMain := int(math.Round(base * 1.05))
	droppedHeader := false
	for mainH() < minMain {
		switch {
		case dateOn:
			dateOn = false
		case headerOn:
			headerOn = false
			droppedHeader = true
		case barOn:
			barOn = false
		default:
			// A cell too small for anything but a shrunk value.
		}
		if !dateOn && !headerOn && !barOn {
			break
		}
	}
	mainSpace := mainH()

	// A one-row cell has no room for a row of its own, but the label and the
	// value fit side by side.
	if droppedHeader && !barOn && !dateOn && a.label != "" {
		drawInline(img, inner, a.label, value, base, gap)
		return
	}

	dateTop := inner.Max.Y
	if dateOn {
		dateTop -= dateH
	}
	barTop := inner.Max.Y
	if dateOn {
		barTop = dateTop - gap/2
	}
	barTop -= barH
	mainBottom := inner.Max.Y
	if barOn {
		mainBottom = barTop - gap/2
	} else if dateOn {
		mainBottom = dateTop - gap/2
	}

	y := inner.Min.Y
	if headerOn {
		drawHeader(img, inner, a.label, percent, base, gap)
		y += headerH + gap/2
	}
	if y > mainBottom {
		y = mainBottom
	}
	if mainSpace > 0 {
		size := math.Min(float64(mainBottom-y)*0.95, base*2.6)
		if st.passed {
			drawPassed(img, image.Rect(inner.Min.X, y, inner.Max.X, mainBottom), value, size, rule, gap)
		} else {
			baseline := (y+mainBottom)/2 + int(math.Round(size*0.36))
			render.Text(img, value, inner.Min.X+inner.Dx()/2, baseline,
				render.Style{Size: size, Bold: true, Shrink: true}, inner.Dx(), color.Black, render.Center)
		}
	}
	if barOn {
		drawBar(img, image.Rect(inner.Min.X, barTop, inner.Max.X, barTop+barH), st.fraction, rule)
	}
	if dateOn {
		size := math.Min(base*0.72, float64(dateH)*0.8)
		baseline := dateTop + int(math.Round(size))
		render.Text(img, date, inner.Max.X, baseline,
			render.Style{Size: size, Shrink: true}, inner.Dx(), color.Black, render.Right)
	}
}

// drawInline puts the label on the left and the value on the right of a cell
// with a single row.
func drawInline(img *image.Paletted, r image.Rectangle, label, value string, base float64, gap int) {
	if r.Empty() {
		return
	}
	size := math.Min(base*1.3, float64(r.Dy())*0.9)
	if size < render.MinSize {
		size = render.MinSize
	}
	baseline := r.Min.Y + (r.Dy()+int(math.Round(size*0.72)))/2
	vw := render.Measure(value, size, true)
	render.Text(img, value, r.Max.X, baseline, render.Style{Size: size, Bold: true, Shrink: true}, r.Dx(), color.Black, render.Right)
	if room := r.Dx() - vw - gap; room > render.MinSize {
		render.Text(img, label, r.Min.X, baseline, render.Style{Size: base, Shrink: true}, room, color.Black, render.Left)
	}
}

// drawHeader puts the label on the left and the passed percentage on the
// right of the first line.
func drawHeader(img *image.Paletted, inner image.Rectangle, label, percent string, base float64, gap int) {
	baseline := inner.Min.Y + int(math.Round(base*0.85))
	rightW := 0
	if percent != "" {
		rightW = render.Measure(percent, base, false)
		render.Text(img, percent, inner.Max.X, baseline, render.Style{Size: base}, rightW+1, color.Black, render.Right)
	}
	if label == "" {
		return
	}
	room := inner.Dx() - rightW
	if rightW > 0 {
		room -= gap
	}
	render.Text(img, label, inner.Min.X, baseline, render.Style{Size: base, Shrink: true}, room, color.Black, render.Left)
}

// drawPassed centers the passed text in r and puts a tick in front of it when
// there is room.
func drawPassed(img *image.Paletted, r image.Rectangle, text string, size float64, rule, gap int) {
	if r.Empty() || size < render.MinSize {
		return
	}
	baseline := (r.Min.Y+r.Max.Y)/2 + int(math.Round(size*0.36))
	tw := render.Measure(text, size, true)
	if tw+int(size*0.9)+gap > r.Dx() {
		render.Text(img, text, r.Min.X+r.Dx()/2, baseline,
			render.Style{Size: size, Bold: true, Shrink: true}, r.Dx(), color.Black, render.Center)
		return
	}
	x := r.Min.X + (r.Dx()-tw)/2
	render.Text(img, text, x, baseline, render.Style{Size: size, Bold: true}, tw+1, color.Black, render.Left)
	ch := int(math.Round(size * 0.9))
	top := baseline - ch + ch/5
	checkmark(img, image.Rect(x-ch-gap, top, x-gap, top+ch), max(rule, int(math.Round(size*0.12))))
}

// checkmark draws a tick in r with the given stroke thickness.
func checkmark(img *image.Paletted, r image.Rectangle, thickness int) {
	if r.Empty() {
		return
	}
	w, h := r.Dx(), r.Dy()
	th := max(thickness, 2)
	x1, y1 := r.Min.X+w*15/100, r.Min.Y+h*55/100
	x2, y2 := r.Min.X+w*42/100, r.Min.Y+h*92/100
	x3, y3 := r.Max.X, r.Min.Y+h*8/100
	render.Segment(img, x1, y1, x2, y2, th, false)
	render.Segment(img, x2, y2, x3, y3, th, false)
}

// drawBar outlines the bar and fills the passed part.
func drawBar(img *image.Paletted, r image.Rectangle, fraction float64, rule int) {
	if r.Dx() < 3 || r.Dy() < 2 {
		return
	}
	render.FillSolid(img, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+rule))
	render.FillSolid(img, image.Rect(r.Min.X, r.Max.Y-rule, r.Max.X, r.Max.Y))
	render.FillSolid(img, image.Rect(r.Min.X, r.Min.Y, r.Min.X+rule, r.Max.Y))
	render.FillSolid(img, image.Rect(r.Max.X-rule, r.Min.Y, r.Max.X, r.Max.Y))
	width := r.Dx() - 2*rule
	fill := min(int(math.Round(float64(width)*fraction)), width)
	if fill > 0 {
		render.FillSolid(img, image.Rect(r.Min.X+rule, r.Min.Y+rule, r.Min.X+rule+fill, r.Max.Y-rule))
	}
}

// value is the main text: the days with their unit, hours and minutes on the
// last day, or heute/vorbei.
func (a *args) value(req *plugin.Request, st state) string {
	switch {
	case st.passed:
		return req.T("vorbei", "passed")
	case st.today:
		return req.T("heute", "today")
	case st.timed:
		h, m := int(st.remain.Hours()), int(st.remain.Minutes())%60
		switch {
		case h > 0 && m > 0:
			return fmt.Sprintf("%d %s %d %s", h, req.T("Std", "h"), m, req.T("Min", "min"))
		case h > 0:
			return fmt.Sprintf("%d %s", h, req.T("Std", "h"))
		default:
			return fmt.Sprintf("%d %s", max(m, 1), req.T("Min", "min"))
		}
	case st.days == 1:
		return fmt.Sprintf("1 %s", req.T("Tag", "day"))
	}
	return fmt.Sprintf("%d %s", st.days, req.T("Tage", "days"))
}
