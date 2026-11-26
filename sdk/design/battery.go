package design

import (
	"image"
	"image/color"
	"math"
	"sort"

	"github.com/maehld/paperboy/sdk/render"
)

// Battery is a segmented battery gauge: an outline with a terminal, the charge
// in four or five segments and a slash for a missing reading. Level thickens
// the outline without a number, so a low battery reads at a glance. While
// charging below 100 % the body is empty and a bolt stands over it; at 100 %
// the full gauge returns.
type Battery struct {
	Fraction float64 // charge from 0 to 1
	Missing  bool    // no reading: draw the slash instead of a fill
	Charging bool    // draw the bolt over the empty body below 100 %
	Level    string  // "", "warn" or "critical": thickens the outline
}

// fullCharge is the fraction that shows the full gauge. The displayed value
// rounds to 100 % from 99.5 %, so the bolt goes when the page says 100 %.
const fullCharge = 0.995

// DrawBattery draws the gauge into r. A stroke of zero follows the height of r.
// It returns false when r has no room for a gauge.
//
// The fill never paints over the outer line. On a tall gauge the outline is
// drawn after the fill and a small gap separates the two. On a small gauge the
// fill is drawn over the inner edge, so it sits on the wall. While charging
// below 100 %, the body is empty and a bolt stands over it.
func DrawBattery(dst *image.Paletted, r image.Rectangle, b Battery, stroke int) bool {
	charging := b.Charging && !b.Missing && b.Fraction < fullCharge
	f, ok := frameBattery(r, b, stroke)
	if !ok {
		return false
	}
	if charging {
		// The body makes room for the bolt that stands over it. The frame
		// keeps the line, the radius and the cap of the full gauge.
		over := max(1, r.Dy()*12/100)
		f.body = image.Rect(f.body.Min.X, r.Min.Y+over, f.body.Max.X, r.Max.Y-over)
		f.inner = f.body.Inset(f.line + f.gap)
		if f.inner.Empty() {
			return false
		}
		outlineRounded(dst, f.body, f.rad, f.line)
		drawBatteryCap(dst, f.body, r)
		drawChargingBolt(dst, r, f)
		return true
	}
	// On a small gauge the fill sits on the wall. Draw the outline first and
	// the fill over its inner edge, so no white pixel remains between the two.
	// A tall gauge keeps the gap and draws the outline last.
	small := f.gap == 0
	if small {
		outlineRounded(dst, f.body, f.rad, f.line)
		drawBatteryCap(dst, f.body, r)
	}
	if !b.Missing {
		fillBattery(dst, f.inner, f.line, clamp01(b.Fraction))
		if f.gap > 0 {
			clipCorners(dst, f.inner, math.Max(f.rad-float64(f.line+f.gap), 1))
		}
	}
	if b.Missing {
		drawBatterySlash(dst, f.inner, f.line)
	}
	if !small {
		outlineRounded(dst, f.body, f.rad, f.line)
		drawBatteryCap(dst, f.body, r)
	}
	return true
}

// drawBatteryCap draws the narrow terminal. Its left edge is flat and sits on
// the body; only the right side is rounded.
func drawBatteryCap(dst *image.Paletted, body, r image.Rectangle) {
	nubH := max(4, r.Dy()*2/5)
	nub := image.Rect(body.Max.X, r.Min.Y+(r.Dy()-nubH)/2, r.Max.X, r.Min.Y+(r.Dy()+nubH)/2)
	fillRightRounded(dst, nub, math.Min(float64(nub.Dx()), float64(nubH)/3))
}

// batteryFrame is the geometry of one gauge.
type batteryFrame struct {
	body  image.Rectangle
	inner image.Rectangle
	line  int
	nub   int
	gap   int
	rad   float64
}

// frameBattery computes the body and the inner area of a gauge. The cap takes
// its width from the body, never more than a third of the height. The warning
// level thickens the outline. A gauge shorter than 32 pixels keeps no gap
// between fill and wall, because the gap would eat the fill.
func frameBattery(r image.Rectangle, b Battery, stroke int) (batteryFrame, bool) {
	if r.Dx() < 4 || r.Dy() < 3 {
		return batteryFrame{}, false
	}
	line := stroke
	if line <= 0 {
		line = int(math.Round(float64(r.Dy()) / 16))
	}
	line = max(line, 1)
	switch b.Level {
	case "warn":
		line++
	case "critical":
		line += 2
	}
	line = min(line, max(1, r.Dy()/8))
	nub := max(2, min(r.Dx()/16, r.Dy()/3))
	body := image.Rect(r.Min.X, r.Min.Y, r.Max.X-nub, r.Max.Y)
	rad := math.Min(float64(r.Dy())*0.22, float64(r.Dy())/2)
	gap := max(1, line/2)
	if r.Dy() < 32 {
		gap = 0
	}
	inner := body.Inset(line + gap)
	if inner.Empty() {
		return batteryFrame{}, false
	}
	return batteryFrame{body: body, inner: inner, line: line, nub: nub, gap: gap, rad: rad}, true
}

// fillBattery fills the charge in four or five segments. A step is wider than
// one pixel, so 84 % and 100 % differ at a glance. A positive charge always
// shows one segment, so a nearly empty battery does not look empty.
func fillBattery(dst *image.Paletted, inner image.Rectangle, line int, fraction float64) {
	segs := 5
	if inner.Dx() < 20 {
		segs = 4
	}
	if inner.Dx() < 8 {
		segs = 1
	}
	gap := max(1, min(line, inner.Dx()/32))
	if inner.Dx() < segs*3 {
		gap = 0
	}
	filled := int(float64(segs)*fraction + 0.5)
	if filled == 0 && fraction > 0 {
		filled = 1
	}
	segW := (inner.Dx() - (segs-1)*gap) / segs
	if segW < 1 {
		segs, filled, segW, gap = 1, 1, inner.Dx(), 0
	}
	for i := 0; i < filled; i++ {
		x0 := inner.Min.X + i*(segW+gap)
		x1 := x0 + segW
		if i == segs-1 {
			x1 = inner.Max.X
		}
		render.FillSolid(dst, image.Rect(x0, inner.Min.Y, x1, inner.Max.Y))
	}
}

// clipCorners clears the pixels of r outside its rounded corners, so the fill
// follows the wall instead of squaring off the corners.
func clipCorners(dst *image.Paletted, r image.Rectangle, rad float64) {
	if rad <= 0 {
		return
	}
	rad = math.Min(rad, math.Min(float64(r.Dx()), float64(r.Dy()))/2)
	corners := [4][2]float64{
		{float64(r.Min.X) + rad, float64(r.Min.Y) + rad},
		{float64(r.Max.X) - rad, float64(r.Min.Y) + rad},
		{float64(r.Min.X) + rad, float64(r.Max.Y) - rad},
		{float64(r.Max.X) - rad, float64(r.Max.Y) - rad},
	}
	for i, c := range corners {
		x0, x1 := r.Min.X, r.Min.X+int(rad)+1
		if i%2 == 1 {
			x0, x1 = r.Max.X-int(rad)-1, r.Max.X
		}
		y0, y1 := r.Min.Y, r.Min.Y+int(rad)+1
		if i >= 2 {
			y0, y1 = r.Max.Y-int(rad)-1, r.Max.Y
		}
		for y := max(y0, r.Min.Y); y < min(y1, r.Max.Y); y++ {
			for x := max(x0, r.Min.X); x < min(x1, r.Max.X); x++ {
				dx, dy := float64(x)+0.5-c[0], float64(y)+0.5-c[1]
				if dx*dx+dy*dy > rad*rad {
					render.FillColor(dst, image.Rect(x, y, x+1, y+1), render.White)
				}
			}
		}
	}
}

// insideRounded reports whether the point lies inside the rounded rectangle.
func insideRounded(px, py float64, r image.Rectangle, rad float64) bool {
	hx := float64(r.Dx())/2 - rad
	hy := float64(r.Dy())/2 - rad
	qx := math.Abs(px - (float64(r.Min.X)+float64(r.Max.X))/2)
	qy := math.Abs(py - (float64(r.Min.Y)+float64(r.Max.Y))/2)
	dx := math.Max(qx-hx, 0)
	dy := math.Max(qy-hy, 0)
	return dx*dx+dy*dy <= rad*rad
}

// outlineRounded draws only the stroke of a rounded rectangle.
func outlineRounded(dst *image.Paletted, r image.Rectangle, rad float64, t int) {
	inner := r.Inset(t)
	radIn := math.Max(rad-float64(t), 0)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			if insideRounded(px, py, r, rad) && !insideRounded(px, py, inner, radIn) {
				render.FillSolid(dst, image.Rect(x, y, x+1, y+1))
			}
		}
	}
}

// fillRightRounded fills a rectangle whose right corners are rounded and whose
// left edge is straight. The cap sits on the body, so its left side must not
// round.
func fillRightRounded(dst *image.Paletted, r image.Rectangle, rad float64) {
	rad = math.Min(rad, math.Min(float64(r.Dx()), float64(r.Dy())/2))
	for y := r.Min.Y; y < r.Max.Y; y++ {
		cy := float64(r.Min.Y) + float64(r.Dy())/2
		dy := math.Abs(float64(y) + 0.5 - cy)
		inset := 0.0
		if dy > float64(r.Dy())/2-rad {
			d := dy - (float64(r.Dy())/2 - rad)
			inset = rad - math.Sqrt(math.Max(rad*rad-d*d, 0))
		}
		x1 := r.Max.X - int(math.Round(inset))
		render.FillSolid(dst, image.Rect(r.Min.X, y, x1, y+1))
	}
}

// drawBatterySlash marks a gauge without a reading: one diagonal line inside
// the outline.
func drawBatterySlash(dst *image.Paletted, inner image.Rectangle, line int) {
	m := max(1, inner.Dx()/8)
	render.Segment(dst, inner.Min.X+m, inner.Max.Y-m-1, inner.Max.X-m-1, inner.Min.Y+m, line, false)
}

// boltShape is the charging mark as a polygon in a unit box.
var boltShape = [6][2]float64{
	{0.54, 0.03}, {0.12, 0.58}, {0.42, 0.58}, {0.36, 0.97}, {0.84, 0.42}, {0.54, 0.42},
}

// drawChargingBolt draws the mark over the empty body. The top and bottom wall
// stay open around the intersections, so the bolt passes through the wall. A
// white gap follows the bolt shape, so the mark never touches the wall.
func drawChargingBolt(dst *image.Paletted, r image.Rectangle, f batteryFrame) {
	bw := r.Dx() * 36 / 100
	if bw < 3 || r.Dy() < 4 {
		return
	}
	box := image.Rect(r.Min.X+(r.Dx()-bw)/2, r.Min.Y, r.Min.X+(r.Dx()+bw)/2, r.Max.Y)
	gap := max(2, r.Dy()/16)
	for _, edge := range []int{f.body.Min.Y, f.body.Max.Y - f.line} {
		x0, x1, ok := boltSpan(box, edge+f.line/2)
		if !ok {
			continue
		}
		render.FillColor(dst, image.Rect(x0-gap, edge-1, x1+gap, edge+f.line+1), render.White)
	}
	fillBolt(dst, box.Inset(-1).Intersect(r), render.White)
	fillBolt(dst, box, render.Black)
}

// boltSpan returns the left and right edge of the bolt shape at y.
func boltSpan(box image.Rectangle, y int) (int, int, bool) {
	xs := boltCrossings(box, y)
	if len(xs) < 2 {
		return 0, 0, false
	}
	return int(xs[0] + 0.5), int(xs[len(xs)-1] + 0.5), true
}

// boltCrossings returns the sorted x positions where y cuts the bolt.
func boltCrossings(box image.Rectangle, y int) []float64 {
	pts := make([]image.Point, len(boltShape))
	for i, p := range boltShape {
		pts[i] = image.Point{X: box.Min.X + int(p[0]*float64(box.Dx())), Y: box.Min.Y + int(p[1]*float64(box.Dy()))}
	}
	yc := float64(y) + 0.5
	var xs []float64
	for i, p := range pts {
		q := pts[(i+1)%len(pts)]
		if p.Y == q.Y || yc < float64(min(p.Y, q.Y)) || yc >= float64(max(p.Y, q.Y)) {
			continue
		}
		xs = append(xs, float64(p.X)+(yc-float64(p.Y))/float64(q.Y-p.Y)*float64(q.X-p.X))
	}
	sort.Float64s(xs)
	return xs
}

// fillBolt fills the bolt shape scaled into box.
func fillBolt(dst *image.Paletted, box image.Rectangle, c color.Color) {
	if box.Dx() < 2 || box.Dy() < 3 {
		return
	}
	for y := box.Min.Y; y < box.Max.Y; y++ {
		xs := boltCrossings(box, y)
		for i := 0; i+1 < len(xs); i += 2 {
			x1, x2 := int(xs[i]+0.5), int(xs[i+1]+0.5)
			if x2 < x1+1 {
				x2 = x1 + 1
			}
			render.FillColor(dst, image.Rect(x1, y, x2, y+1), c)
		}
	}
}

func clamp01(v float64) float64 {
	return math.Max(0, math.Min(1, v))
}
