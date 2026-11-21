package design

import (
	"image"
	"math"

	"github.com/maehld/paperboy/sdk/render"
)

// Meter is an outlined bar that shows a fraction from 0 to 1. Missing marks a
// bar without a reading with a slash, so it does not read as empty.
type Meter struct {
	Fraction float64
	Fill     string // solid (default), shade or hatch
	Missing  bool
}

// DrawMeter draws the meter into r. It returns false when r has no room.
func DrawMeter(dst *image.Paletted, r image.Rectangle, m Meter, rule int) bool {
	if r.Dx() < 4 || r.Dy() < 3 {
		return false
	}
	t := min(max(rule, 1), max(r.Dy()/3, 1))
	render.FillSolid(dst, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+t))
	render.FillSolid(dst, image.Rect(r.Min.X, r.Max.Y-t, r.Max.X, r.Max.Y))
	render.FillSolid(dst, image.Rect(r.Min.X, r.Min.Y, r.Min.X+t, r.Max.Y))
	render.FillSolid(dst, image.Rect(r.Max.X-t, r.Min.Y, r.Max.X, r.Max.Y))

	inner := r.Inset(t + 1)
	if inner.Empty() {
		return true
	}
	if m.Missing {
		render.Segment(dst, inner.Min.X, inner.Max.Y-1, inner.Max.X-1, inner.Min.Y, max(t, 2), false)
		return true
	}
	frac := min(max(m.Fraction, 0), 1)
	width := int(math.Round(float64(inner.Dx()) * frac))
	if width > 0 {
		fill := m.Fill
		if fill == "" {
			fill = "solid"
		}
		render.Fill(dst, image.Rect(inner.Min.X, inner.Min.Y, inner.Min.X+width, inner.Max.Y), fill)
	}
	return true
}
