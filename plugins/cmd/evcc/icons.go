package main

import (
	"image"
	"image/color"
	"math"

	"github.com/maehld/paperboy/sdk/design"
	"github.com/maehld/paperboy/sdk/render"
)

// The flow and balance widgets draw their own icons with one stroke width, so
// the pictograms of a diagram look alike. They are vector shapes, not images.

// fillDisc fills a circle.
func fillDisc(img *image.Paletted, cx, cy, r float64, c color.Color) {
	if r <= 0 {
		return
	}
	pts := make([]fpt, 0, 40)
	for i := 0; i < 40; i++ {
		a := 2 * math.Pi * float64(i) / 40
		pts = append(pts, fpt{cx + r*math.Cos(a), cy + r*math.Sin(a)})
	}
	fillPolygon(img, c, pts...)
}

// thickLine draws a line of the given thickness between a and b.
func thickLine(img *image.Paletted, a, b fpt, t float64, c color.Color) {
	d := unit(sub(b, a))
	n := fpt{-d.y * t / 2, d.x * t / 2}
	fillPolygon(img, c, fpt{a.x + n.x, a.y + n.y}, fpt{b.x + n.x, b.y + n.y}, fpt{b.x - n.x, b.y - n.y}, fpt{a.x - n.x, a.y - n.y})
	fillDisc(img, a.x, a.y, t/2, c)
	fillDisc(img, b.x, b.y, t/2, c)
}

// polyline draws connected thick lines.
func polyline(img *image.Paletted, pts []fpt, t float64, c color.Color) {
	for i := 0; i+1 < len(pts); i++ {
		thickLine(img, pts[i], pts[i+1], t, c)
	}
}

// ring draws a circle outline.
func ring(img *image.Paletted, cx, cy, r, t float64, c color.Color) {
	fillDisc(img, cx, cy, r, c)
	fillDisc(img, cx, cy, r-t, color.White)
}

// roundRectPoints walks the outline of a rounded rectangle clockwise.
func roundRectPoints(r image.Rectangle, rad float64) []fpt {
	x0, y0 := float64(r.Min.X), float64(r.Min.Y)
	x1, y1 := float64(r.Max.X), float64(r.Max.Y)
	rad = math.Min(rad, math.Min((x1-x0)/2, (y1-y0)/2))
	out := make([]fpt, 0, 36)
	arc := func(cx, cy, a0 float64) {
		for i := 0; i <= 8; i++ {
			a := a0 + float64(i)*math.Pi/2/8
			out = append(out, fpt{cx + rad*math.Cos(a), cy + rad*math.Sin(a)})
		}
	}
	arc(x1-rad, y0+rad, -math.Pi/2)
	arc(x1-rad, y1-rad, 0)
	arc(x0+rad, y1-rad, math.Pi/2)
	arc(x0+rad, y0+rad, math.Pi)
	return out
}

// fillRoundRect fills a rectangle with rounded corners.
func fillRoundRect(img *image.Paletted, r image.Rectangle, rad float64, c color.Color) {
	rad = math.Min(rad, math.Min(float64(r.Dx()), float64(r.Dy()))/2)
	render.FillColor(img, image.Rect(r.Min.X+int(rad), r.Min.Y, r.Max.X-int(rad), r.Max.Y), c)
	render.FillColor(img, image.Rect(r.Min.X, r.Min.Y+int(rad), r.Max.X, r.Max.Y-int(rad)), c)
	fillDisc(img, float64(r.Min.X)+rad, float64(r.Min.Y)+rad, rad, c)
	fillDisc(img, float64(r.Max.X)-rad, float64(r.Min.Y)+rad, rad, c)
	fillDisc(img, float64(r.Min.X)+rad, float64(r.Max.Y)-rad, rad, c)
	fillDisc(img, float64(r.Max.X)-rad, float64(r.Max.Y)-rad, rad, c)
}

// roundFrame draws the outline of a rounded rectangle with thickness t.
func roundFrame(img *image.Paletted, r image.Rectangle, rad float64, t int) {
	fillRoundRect(img, r, rad, color.Black)
	inner := image.Rect(r.Min.X+t, r.Min.Y+t, r.Max.X-t, r.Max.Y-t)
	fillRoundRect(img, inner, math.Max(rad-float64(t), 0), color.White)
}

// drawFlowBattery draws the state of charge with the shared gauge. The flow
// shows charging and discharging with its arrows and the power value, so the
// icon carries no bolt and no level.
func drawFlowBattery(img *image.Paletted, r image.Rectangle, soc float64) {
	s := math.Min(float64(r.Dx()), float64(r.Dy()))
	if s < 10 {
		return
	}
	body := rectAt(float64(r.Min.X)+s*0.16, float64(r.Min.Y)+s*0.32, s*0.72, s*0.36)
	design.DrawBattery(img, body, design.Battery{Fraction: soc / 100}, max(1, int(math.Round(s*0.075))))
}

// drawFlowIcon draws one pictogram of the diagram. The caller gives the box the
// icon must fill; the stroke width follows the box, so all icons look alike.
func drawFlowIcon(img *image.Paletted, name string, r image.Rectangle, c color.Color) {
	s := math.Min(float64(r.Dx()), float64(r.Dy()))
	if s < 10 {
		return
	}
	x0, y0 := float64(r.Min.X), float64(r.Min.Y)
	at := func(x, y float64) fpt { return fpt{x0 + x*s, y0 + y*s} }
	t := math.Max(s*0.075, 1)
	switch name {
	case "sun":
		ring(img, x0+s/2, y0+s/2, s*0.16, t, c)
		for i := 0; i < 8; i++ {
			a := float64(i) * math.Pi / 4
			thickLine(img, at(0.5+0.27*math.Cos(a), 0.5+0.27*math.Sin(a)), at(0.5+0.43*math.Cos(a), 0.5+0.43*math.Sin(a)), t, c)
		}
	case "grid":
		// Pylon: two legs, two crossarms and a rung.
		thickLine(img, at(0.50, 0.08), at(0.24, 0.90), t, c)
		thickLine(img, at(0.50, 0.08), at(0.76, 0.90), t, c)
		thickLine(img, at(0.31, 0.28), at(0.69, 0.28), t, c)
		thickLine(img, at(0.25, 0.44), at(0.75, 0.44), t, c)
		thickLine(img, at(0.31, 0.62), at(0.69, 0.62), t*0.8, c)
		thickLine(img, at(0.50, 0.44), at(0.37, 0.62), t*0.7, c)
	case "battery":
		roundFrame(img, rectAt(x0+s*0.16, y0+s*0.32, s*0.64, s*0.36), s*0.08, int(math.Max(t, 1)))
		render.FillColor(img, rectAt(x0+s*0.80, y0+s*0.43, s*0.08, s*0.14), c)
		render.FillColor(img, rectAt(x0+s*0.26, y0+s*0.43, s*0.10, s*0.14), c)
		render.FillColor(img, rectAt(x0+s*0.40, y0+s*0.43, s*0.10, s*0.14), c)
	case "home":
		polyline(img, []fpt{at(0.12, 0.48), at(0.50, 0.16), at(0.88, 0.48)}, t, c)
		polyline(img, []fpt{at(0.24, 0.44), at(0.24, 0.86), at(0.76, 0.86), at(0.76, 0.44)}, t, c)
		roundFrame(img, rectAt(x0+s*0.43, y0+s*0.60, s*0.14, s*0.26), 0, int(math.Max(t, 1)))
	case "car":
		polyline(img, []fpt{at(0.12, 0.66), at(0.17, 0.55), at(0.30, 0.53), at(0.39, 0.38), at(0.61, 0.38), at(0.71, 0.53), at(0.85, 0.56), at(0.88, 0.66)}, t, c)
		ring(img, x0+s*0.30, y0+s*0.70, s*0.10, t, c)
		ring(img, x0+s*0.70, y0+s*0.70, s*0.10, t, c)
		thickLine(img, at(0.42, 0.52), at(0.60, 0.52), t, c)
	case "hub":
		// Two triangles form the bolt; fillPolygon only fills convex shapes.
		fillPolygon(img, c, at(0.64, 0.12), at(0.30, 0.56), at(0.50, 0.56))
		fillPolygon(img, c, at(0.36, 0.88), at(0.70, 0.44), at(0.50, 0.44))
	}
}

// rectAt rounds a float rectangle to pixels.
func rectAt(x, y, w, h float64) image.Rectangle {
	return image.Rect(int(math.Round(x)), int(math.Round(y)), int(math.Round(x+w)), int(math.Round(y+h)))
}
