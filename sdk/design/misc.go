package design

import (
	"image"
	"image/color"

	"github.com/maehld/paperboy/sdk/render"
)

// Muted returns the tone of secondary text. It stays black on a one-bit
// panel, where a gray would disappear.
func Muted(dst *image.Paletted) color.Color {
	if render.Levels(dst.Palette) >= 4 {
		return render.DarkGray
	}
	return render.Black
}

// Notice draws a short centered text, for an empty or missing state.
func Notice(dst *image.Paletted, r image.Rectangle, text string, s Scale) bool {
	if text == "" || r.Empty() {
		return false
	}
	size := min(s.Body, float64(r.Dy())*0.5)
	if size < render.MinSize {
		return false
	}
	text = render.Ellipsize(text, size, false, r.Dx())
	if text == "" {
		return false
	}
	render.TextAt(dst, text, r.Min.X+r.Dx()/2, capBaseline(r.Min.Y, r.Dy(), size),
		size, false, render.Black, render.Center)
	return true
}

// Arrow draws a trend arrow: direction -1 up, 0 flat, 1 down. A strength of
// two draws a pair of arrows, as a strong rise or fall.
func Arrow(dst *image.Paletted, cx, cy, size, direction, strength int) {
	if size < render.MinSize || strength < 1 {
		return
	}
	if direction < -1 {
		direction = -1
	}
	if direction > 1 {
		direction = 1
	}
	if strength >= 2 {
		small := size * 2 / 3
		Arrow(dst, cx-size/3, cy, small, direction, 1)
		Arrow(dst, cx+size/3, cy, small, direction, 1)
		return
	}
	thickness := max(2, size/10)
	length := float64(size) * 0.72
	head := float64(size) * 0.3
	cxf, cyf := float64(cx), float64(cy)
	switch direction {
	case -1: // up
		tipY := cyf - length/2
		render.Segment(dst, cx, int(cyf+length/2), cx, int(tipY), thickness, false)
		render.Segment(dst, cx, int(tipY), int(cxf-head), int(tipY+head), thickness, false)
		render.Segment(dst, cx, int(tipY), int(cxf+head), int(tipY+head), thickness, false)
	case 1: // down
		tipY := cyf + length/2
		render.Segment(dst, cx, int(cyf-length/2), cx, int(tipY), thickness, false)
		render.Segment(dst, cx, int(tipY), int(cxf-head), int(tipY-head), thickness, false)
		render.Segment(dst, cx, int(tipY), int(cxf+head), int(tipY-head), thickness, false)
	default: // flat points right
		tipX := cxf + length/2
		render.Segment(dst, int(cxf-length/2), cy, int(tipX), cy, thickness, false)
		render.Segment(dst, int(tipX), cy, int(tipX-head), int(cyf-head), thickness, false)
		render.Segment(dst, int(tipX), cy, int(tipX-head), int(cyf+head), thickness, false)
	}
}
