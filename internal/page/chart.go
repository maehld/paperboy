package page

import (
	"image"
	"image/color"
)

// chart draws one or more series from a list of points. The implementation is
// replaced by the time-axis chart of ADR 0034 in a follow-up step.
func (r *renderer) chart(img *image.Paletted, rc image.Rectangle, c *Chart, padding *int) {
	points, _ := lookupList(r.root, c.Points)
	if len(points) < 2 {
		drawStyled(img, text(r.language, "keine Verlaufsdaten", "no chart data"),
			rc.Min.X+rc.Dx()/2, rc.Min.Y+rc.Dy()/2,
			textStyle{Size: float64(rc.Dy()) / 8, Bold: false, Shrink: true},
			rc.Dx()-8, color.Black, alignCenter)
		return
	}
	left := rc.Min.X + rc.Dx()/20
	right := rc.Max.X - rc.Dx()/20
	top := rc.Min.Y + rc.Dy()/4
	bottom := rc.Max.Y - rc.Dy()/8
	series := make([][]float64, len(c.Series))
	maxV := 0.0
	for si, s := range c.Series {
		series[si] = make([]float64, len(points))
		for i, p := range points {
			v, ok := seriesValue(p, s.Key)
			if !ok {
				continue
			}
			series[si][i] = v
			if v > maxV {
				maxV = v
			}
		}
	}
	hline(img, left, bottom, right, 1)
	if maxV > 0 {
		label := r.f.format(maxV, unitFormat(c.Unit))
		if unitFormat(c.Unit) == "" && c.Unit != "" {
			label = r.f.trim(maxV) + " " + c.Unit
		}
		drawStyled(img, label, left, top, textStyle{Size: float64(rc.Dy()) / 14, Shrink: true},
			rc.Dx()/3, color.Black, alignLeft)
	}
	for si := range series {
		px, py, has := 0, 0, false
		for i, v := range series[si] {
			x := left + (right-left)*i/(len(points)-1)
			y := bottom - int(float64(bottom-top)*v/maxV)
			if has {
				segment(img, px, py, x, y, 1, si%2 == 1)
			}
			px, py, has = x, y, true
		}
	}
}
