// The sign widget of the dexcom plugin.

package main

import (
	"image"
	"image/color"
	"math"

	"github.com/maehld/paperboy/sdk/design"
	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

// signWidget draws one large sign for the state of the newest reading, so a
// child who cannot read sees whether everything is fine (ADR 0170). A low adds
// a sweet below the sign because fast carbs are the one action a child can
// take. A high has no action, because the correction belongs to an adult.
func (d *dexcom) signWidget(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	l, err := d.limits()
	if err != nil {
		return err
	}
	rs, loadErr, _ := d.snapshot()
	if loadErr != nil {
		return loadErr
	}
	b := img.Rect.Inset(w.Style.Padding)
	if b.Dx() < 48 || b.Dy() < 48 {
		return plugin.TooSmall(w)
	}
	last, ok := current(rs, req.Now)
	if !ok || req.Now.Sub(last.at) > staleAfter {
		drawUnknown(img, b)
		return nil
	}
	switch level(last.value, l) {
	case "urgent":
		sign, candy := signCandy(b)
		drawStop(img, sign, ink)
		drawSweet(img, candy, ink)
	case "low":
		sign, candy := signCandy(b)
		drawArrow(img, sign, false, ink)
		drawSweet(img, candy, ink)
	case "high":
		drawArrow(img, fullBox(b), true, ink)
	default:
		drawCheck(img, fullBox(b), ink)
	}
	return nil
}

// signCandy places the sign of a low and its sweet. A wide cell puts them side
// by side, so both grow with the height; a narrow cell stacks them.
func signCandy(b image.Rectangle) (image.Rectangle, image.Rectangle) {
	gap := b.Dy() / 10
	if b.Dx() >= b.Dy()*16/10 {
		s := min(b.Dy(), (b.Dx()-gap)/2)
		x0 := b.Min.X + (b.Dx()-(2*s+gap))/2
		return image.Rect(x0, b.Min.Y, x0+s, b.Max.Y),
			image.Rect(x0+s+gap, b.Min.Y, x0+2*s+gap, b.Max.Y)
	}
	return signBox(b), candyBox(b)
}

// fullBox is the largest square of the cell.
func fullBox(b image.Rectangle) image.Rectangle {
	s := min(b.Dx(), b.Dy())
	x0 := b.Min.X + (b.Dx()-s)/2
	y0 := b.Min.Y + (b.Dy()-s)/2
	return image.Rect(x0, y0, x0+s, y0+s)
}

// signBox is the square above the candy band that holds the sign itself.
func signBox(b image.Rectangle) image.Rectangle {
	top := image.Rect(b.Min.X, b.Min.Y, b.Max.X, b.Min.Y+int(float64(b.Dy())*0.70))
	s := min(top.Dx(), top.Dy())
	x0 := top.Min.X + (top.Dx()-s)/2
	y0 := top.Min.Y + (top.Dy()-s)/2
	return image.Rect(x0, y0, x0+s, y0+s)
}

// candyBox is the wide band below the sign that holds the sweet of a low.
func candyBox(b image.Rectangle) image.Rectangle {
	y0 := b.Min.Y + int(float64(b.Dy())*0.76)
	h := b.Max.Y - y0
	if h <= 0 {
		return image.Rectangle{}
	}
	w := min(int(float64(h)*2.4), b.Dx())
	x0 := b.Min.X + (b.Dx()-w)/2
	return image.Rect(x0, y0, x0+w, b.Max.Y)
}

// drawCheck draws the sign of a reading inside the target band.
func drawCheck(img *image.Paletted, r image.Rectangle, c color.Color) {
	if r.Empty() {
		return
	}
	at := func(x, y float64) fpt {
		return fpt{float64(r.Min.X) + x*float64(r.Dx()), float64(r.Min.Y) + y*float64(r.Dy())}
	}
	t := float64(r.Dx()) * 0.15
	corner := at(0.16, 0.52)
	bottom := at(0.42, 0.80)
	top := at(0.86, 0.20)
	thickLine(img, corner, bottom, t, c)
	thickLine(img, bottom, top, t, c)
}

// drawArrow draws a thick arrow up for a high reading or down for a low one.
// It is a road sign, not the thin trend arrow of the glucose widget.
func drawArrow(img *image.Paletted, r image.Rectangle, up bool, c color.Color) {
	if r.Empty() {
		return
	}
	at := func(x, y float64) fpt {
		return fpt{float64(r.Min.X) + x*float64(r.Dx()), float64(r.Min.Y) + y*float64(r.Dy())}
	}
	if up {
		fillPolygon(img, c, at(0.40, 0.38), at(0.60, 0.38), at(0.60, 0.86), at(0.40, 0.86))
		fillPolygon(img, c, at(0.50, 0.10), at(0.22, 0.42), at(0.78, 0.42))
		return
	}
	fillPolygon(img, c, at(0.40, 0.14), at(0.60, 0.14), at(0.60, 0.62), at(0.40, 0.62))
	fillPolygon(img, c, at(0.50, 0.90), at(0.22, 0.58), at(0.78, 0.58))
}

// drawStop draws a stop sign for the urgent low, with a white exclamation
// instead of a word.
func drawStop(img *image.Paletted, r image.Rectangle, c color.Color) {
	if r.Empty() {
		return
	}
	at := func(x, y float64) fpt {
		return fpt{float64(r.Min.X) + x*float64(r.Dx()), float64(r.Min.Y) + y*float64(r.Dy())}
	}
	octagon := make([]fpt, 0, 8)
	for i := 0; i < 8; i++ {
		a := math.Pi/8 + float64(i)*math.Pi/4
		octagon = append(octagon, at(0.5+0.46*math.Cos(a), 0.5+0.46*math.Sin(a)))
	}
	fillPolygon(img, c, octagon...)
	white := color.Gray{255}
	thickLine(img, at(0.50, 0.26), at(0.50, 0.58), float64(r.Dx())*0.13, white)
	p := at(0.50, 0.73)
	fillDisc(img, p.x, p.y, float64(r.Dx())*0.065, white)
}

// drawSweet draws a wrapped candy, the simplest sweet a child knows, below the
// sign of a low reading. The candy keeps its wide shape inside a square or a
// band.
func drawSweet(img *image.Paletted, r image.Rectangle, c color.Color) {
	if r.Empty() || r.Dy() < 8 || r.Dx() < 16 {
		return
	}
	w := min(r.Dx(), int(float64(r.Dy())*2.4))
	h := min(r.Dy(), int(float64(w)/2.4))
	x0 := r.Min.X + (r.Dx()-w)/2
	y0 := r.Min.Y + (r.Dy()-h)/2
	box := image.Rect(x0, y0, x0+w, y0+h)
	at := func(x, y float64) fpt {
		return fpt{float64(box.Min.X) + x*float64(box.Dx()), float64(box.Min.Y) + y*float64(box.Dy())}
	}
	fillPolygon(img, c, at(0.02, 0.50), at(0.34, 0.26), at(0.34, 0.74))
	fillPolygon(img, c, at(0.98, 0.50), at(0.66, 0.26), at(0.66, 0.74))
	thickLine(img, at(0.36, 0.50), at(0.64, 0.50), float64(box.Dy())*0.88, c)
}

// drawUnknown draws a muted question mark when there is no current reading.
// A stale reading never keeps its old sign: the child must not see a smile or
// a check for data that is gone.
func drawUnknown(img *image.Paletted, r image.Rectangle) {
	if r.Empty() {
		return
	}
	size := float64(r.Dy()) * 1.25
	for size > render.MinSize && render.Measure("?", size, true) > r.Dx() {
		size *= 0.9
	}
	if size < render.MinSize {
		return
	}
	baseline := r.Min.Y + r.Dy()/2 + int(math.Round(size*0.36))
	render.TextAt(img, "?", r.Min.X+r.Dx()/2, baseline, size, true, design.Muted(img), render.Center)
}

// fpt is a point with sub-pixel precision.
type fpt struct{ x, y float64 }

// fillDisc fills a circle.
func fillDisc(img *image.Paletted, cx, cy, rad float64, c color.Color) {
	if rad <= 0 {
		return
	}
	pts := make([]fpt, 0, 40)
	for i := 0; i < 40; i++ {
		a := 2 * math.Pi * float64(i) / 40
		pts = append(pts, fpt{cx + rad*math.Cos(a), cy + rad*math.Sin(a)})
	}
	fillPolygon(img, c, pts...)
}

// thickLine draws a line with round ends.
func thickLine(img *image.Paletted, a, b fpt, t float64, c color.Color) {
	if t <= 0 {
		return
	}
	d := fpt{b.x - a.x, b.y - a.y}
	l := math.Hypot(d.x, d.y)
	if l < 1 {
		fillDisc(img, a.x, a.y, t/2, c)
		return
	}
	nx, ny := -d.y/l*t/2, d.x/l*t/2
	fillPolygon(img, c,
		fpt{a.x + nx, a.y + ny}, fpt{b.x + nx, b.y + ny},
		fpt{b.x - nx, b.y - ny}, fpt{a.x - nx, a.y - ny})
	fillDisc(img, a.x, a.y, t/2, c)
	fillDisc(img, b.x, b.y, t/2, c)
}

// fillPolygon fills a convex polygon, sampling pixel centres.
func fillPolygon(img *image.Paletted, c color.Color, pts ...fpt) {
	if len(pts) < 3 {
		return
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, p := range pts {
		minX, maxX = math.Min(minX, p.x), math.Max(maxX, p.x)
		minY, maxY = math.Min(minY, p.y), math.Max(maxY, p.y)
	}
	for y := int(math.Floor(minY)); y <= int(math.Ceil(maxY)); y++ {
		for x := int(math.Floor(minX)); x <= int(math.Ceil(maxX)); x++ {
			if !image.Pt(x, y).In(img.Rect) {
				continue
			}
			fx, fy := float64(x)+0.5, float64(y)+0.5
			pos, neg := false, false
			for i, p := range pts {
				q := pts[(i+1)%len(pts)]
				d := (q.x-p.x)*(fy-p.y) - (q.y-p.y)*(fx-p.x)
				pos = pos || d > 1e-9
				neg = neg || d < -1e-9
			}
			if !(pos && neg) {
				img.Set(x, y, c)
			}
		}
	}
}
