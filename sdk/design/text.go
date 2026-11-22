package design

import (
	"image"
	"image/color"
	"math"

	"github.com/maehld/paperboy/sdk/render"
)

// drawLine draws one line at a role size. An overlong line gets an ellipsis
// instead of a smaller type size, so a role keeps one size per page.
func drawLine(dst *image.Paletted, s string, r image.Rectangle, top int, size float64, bold bool, c color.Color, align render.Align) {
	if s == "" || r.Dx() <= 0 {
		return
	}
	s = render.Ellipsize(s, size, bold, r.Dx())
	render.TextAt(dst, s, anchorX(r, align), baseline(top, size), size, bold, c, align)
}

// anchorX returns the x coordinate a text is anchored at.
func anchorX(r image.Rectangle, align render.Align) int {
	switch align {
	case render.Center:
		return r.Min.X + r.Dx()/2
	case render.Right:
		return r.Max.X
	}
	return r.Min.X
}

// valueWidth is the width of a value, its unit and an optional trend arrow.
func valueWidth(value, unit string, valueSize, unitSize float64, bold bool, trend, trendSize int) int {
	w := render.Measure(value, valueSize, bold)
	if unit != "" {
		w += gap(valueSize) + render.Measure(unit, unitSize, false)
	}
	if trend != 0 {
		w += gap(valueSize) + trendSize
	}
	return w
}

// drawValue draws a value with an optional unit and trend arrow as one line.
// The group shrinks with one factor until it fits; the unit stays smaller
// than the value. A value that still does not fit is shortened.
func drawValue(dst *image.Paletted, r image.Rectangle, top, height int, value, unit string,
	valueSize, unitSize float64, bold bool, c color.Color, align render.Align, trend int) bool {
	if value == "" || r.Dx() <= 0 || height <= 0 {
		return false
	}
	unitSize = min(unitSize, valueSize)
	trendSize := max(int(math.Round(unitSize*0.9)), render.MinSize)
	if trend == 0 {
		trendSize = 0
	}
	for valueSize > render.MinSize && valueWidth(value, unit, valueSize, unitSize, bold, trend, trendSize) > r.Dx() {
		valueSize *= 0.94
		unitSize = min(unitSize, valueSize)
		if trend != 0 {
			trendSize = max(int(math.Round(unitSize*0.9)), render.MinSize)
		}
	}
	if valueWidth(value, unit, valueSize, unitSize, bold, trend, trendSize) > r.Dx() {
		room := r.Dx()
		if unit != "" {
			room -= gap(valueSize) + render.Measure(unit, unitSize, false)
		}
		if trend != 0 {
			room -= gap(valueSize) + trendSize
		}
		value = render.Ellipsize(value, valueSize, bold, max(room, 1))
	}
	if value == "" {
		return false
	}
	total := valueWidth(value, unit, valueSize, unitSize, bold, trend, trendSize)
	x := r.Min.X
	switch align {
	case render.Center:
		x = r.Min.X + (r.Dx()-total)/2
	case render.Right:
		x = r.Max.X - total
	}
	base := capBaseline(top, height, valueSize)
	w := render.Measure(value, valueSize, bold)
	render.TextAt(dst, value, x, base, valueSize, bold, c, render.Left)
	x += w
	if unit != "" {
		x += gap(valueSize)
		render.TextAt(dst, unit, x, base, unitSize, false, c, render.Left)
		x += render.Measure(unit, unitSize, false)
	}
	if trend != 0 {
		x += gap(valueSize)
		Arrow(dst, x+trendSize/2, base-int(valueSize*0.34), trendSize, trend, 1)
	}
	return true
}

// fillRect fills r with a shared fill style. On a one-bit panel "shade" and
// "hatch" become row patterns; every fill stays a run of whole rows, so a
// page still compresses well to QOI.
func fillRect(dst *image.Paletted, r image.Rectangle, style string) {
	r = r.Intersect(dst.Rect)
	if r.Empty() {
		return
	}
	render.Fill(dst, r, style)
}
