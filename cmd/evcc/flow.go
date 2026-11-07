package main

import (
	"image"
	"image/color"
	"math"
	"strings"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

// flowNode is one box of the flow diagram.
type flowNode struct {
	label, value string
	rect         image.Rectangle
	idle         bool
}

// activeLoadpoints are the charging points that take part in the flow: the ones
// that draw power or have a vehicle connected.
func (s *state) activeLoadpoints() []*loadpoint {
	var out []*loadpoint
	for i := range s.Loadpoints {
		lp := &s.Loadpoints[i]
		if lp.Charging || lp.ChargePower > flowIdle || lp.Connected {
			out = append(out, lp)
		}
	}
	return out
}

// flowIdle is the power in watts below which a flow counts as no flow.
const flowIdle = 10

func lpName(lp *loadpoint) string {
	if lp.VehicleTitle != "" {
		return lp.VehicleTitle
	}
	return lp.Title
}

// hasBattery reports whether evcc knows a home battery.
func (s *state) hasBattery() bool { return s.Battery != nil && s.Battery.Capacity > 0 }

// flowEdge is a flow between two points; power is the amount, the direction
// runs from `from` to `to`.
type flowEdge struct {
	from, to image.Point
	power    float64
}

// flow draws PV, grid, home, battery and active loadpoints as boxes around
// the home, joined by arrows whose width follows the power. Only shapes and
// widths carry the information, so it reads in two gray levels.
func (e *evcc) flow(w plugin.WidgetRequest, img *image.Paletted) {
	s := e.cur
	b := img.Rect.Inset(w.Style.Padding)
	if b.Dx() < 40 || b.Dy() < 40 {
		return
	}
	base := e.req.TextPx(w)
	rule := max(w.Style.Rule, 1)
	gap := max(int(e.req.MMPx(0.6)), 2)

	colW, rowH := b.Dx()/3, b.Dy()/3
	nodeW, nodeH := colW*68/100, rowH*62/100
	box := func(col int, cy float64) image.Rectangle {
		cx := b.Min.X + colW*col + colW/2
		y := b.Min.Y + int(cy*float64(rowH))
		return image.Rect(cx-nodeW/2, y-nodeH/2, cx+nodeW/2, y+nodeH/2)
	}
	grid := s.gridPower()
	battery := s.hasBattery()

	pv := flowNode{label: "PV", value: e.power(s.PVPower), rect: box(1, 0.5), idle: s.PVPower < flowIdle}
	home := flowNode{label: e.t("Haus", "Home"), value: e.power(s.HomePower), rect: box(1, 1.5)}
	gridLabel := e.t("Netz", "Grid")
	nodeGrid := flowNode{label: gridLabel, value: e.power(grid), rect: box(0, 1.5), idle: math.Abs(grid) < flowIdle}
	nodes := []flowNode{pv, home, nodeGrid}

	var edges []flowEdge
	top := func(r image.Rectangle) image.Point { return image.Pt((r.Min.X+r.Max.X)/2, r.Min.Y-gap) }
	bottom := func(r image.Rectangle) image.Point { return image.Pt((r.Min.X+r.Max.X)/2, r.Max.Y+gap) }
	left := func(r image.Rectangle) image.Point { return image.Pt(r.Min.X-gap, (r.Min.Y+r.Max.Y)/2) }
	right := func(r image.Rectangle) image.Point { return image.Pt(r.Max.X+gap, (r.Min.Y+r.Max.Y)/2) }
	add := func(a, z image.Point, p float64) {
		if p >= 0 {
			edges = append(edges, flowEdge{a, z, p})
		} else {
			edges = append(edges, flowEdge{z, a, -p})
		}
	}
	add(bottom(pv.rect), top(home.rect), s.PVPower)
	add(left(home.rect), right(nodeGrid.rect), -grid) // import flows towards home

	if battery {
		bat := s.Battery
		n := flowNode{label: e.t("Akku ", "Battery ") + e.num(bat.SoC, 0) + " %", value: e.power(bat.Power),
			rect: box(1, 2.5), idle: math.Abs(bat.Power) < flowIdle}
		nodes = append(nodes, n)
		add(bottom(home.rect), top(n.rect), -bat.Power) // charging (negative) flows into the battery
	}

	lps := s.activeLoadpoints()
	if len(lps) > 3 {
		lps = lps[:3]
	}
	for i, lp := range lps {
		cy := 1.5 + (float64(i)-float64(len(lps)-1)/2)*0.95
		if len(lps) == 1 {
			cy = 1.5
		}
		n := flowNode{label: lpName(lp), value: e.power(lp.ChargePower), rect: box(2, cy), idle: lp.ChargePower < flowIdle}
		if lp.VehicleSoC > 0 {
			n.label += " " + e.num(lp.VehicleSoC, 0) + " %"
		}
		if len(lps) > 1 {
			n.rect = shrinkHeight(n.rect, 0.9)
		}
		nodes = append(nodes, n)
		add(right(home.rect), left(n.rect), lp.ChargePower)
	}

	maxP := 0.0
	for _, ed := range edges {
		maxP = math.Max(maxP, ed.power)
	}
	minStroke := math.Max(e.req.PtPx(1.0), 2)
	maxStroke := math.Max(e.req.PtPx(5.0), minStroke+2)
	for _, ed := range edges {
		if ed.power < flowIdle || maxP <= 0 {
			render.Segment(img, ed.from.X, ed.from.Y, ed.to.X, ed.to.Y, rule, true)
			continue
		}
		stroke := int(math.Round(minStroke + (maxStroke-minStroke)*ed.power/maxP))
		arrow(img, ed.from, ed.to, stroke, max(int(e.req.PtPx(7)), stroke*3))
	}

	labelSz := max(math.Min(base*0.8, float64(nodeH)*0.28), e.req.PtPx(5.8), render.MinSize)
	valueSz := max(math.Min(base*1.3, float64(nodeH)*0.38), e.req.PtPx(6.5), render.MinSize)
	for _, n := range nodes {
		r := n.rect
		t := rule
		if n.idle {
			t = max(rule/2, 1)
		}
		render.HLine(img, r.Min.X, r.Min.Y, r.Max.X, t)
		render.HLine(img, r.Min.X, r.Max.Y-t, r.Max.X, t)
		render.VLine(img, r.Min.X, r.Min.Y, r.Max.Y, t)
		render.VLine(img, r.Max.X-t, r.Min.Y, r.Max.Y, t)
		inner := r.Dx() - 2*max(int(e.req.MMPx(0.8)), 3)
		cx := (r.Min.X + r.Max.X) / 2
		render.Text(img, n.label, cx, r.Min.Y+r.Dy()*38/100, render.Style{Size: labelSz, Shrink: true}, inner, color.Black, render.Center)
		render.Text(img, n.value, cx, r.Min.Y+r.Dy()*82/100, render.Style{Size: valueSz, Bold: true, Shrink: true}, inner, color.Black, render.Center)
	}
}

func shrinkHeight(r image.Rectangle, f float64) image.Rectangle {
	cy := (r.Min.Y + r.Max.Y) / 2
	h := int(float64(r.Dy()) * f)
	return image.Rect(r.Min.X, cy-h/2, r.Max.X, cy+h/2)
}

// arrow draws a line of the given thickness from a to b with a filled head at b.
func arrow(img *image.Paletted, a, b image.Point, thickness, head int) {
	dx, dy := float64(b.X-a.X), float64(b.Y-a.Y)
	l := math.Hypot(dx, dy)
	if l < 1 {
		return
	}
	head = min(head, int(l*0.6))
	ux, uy := dx/l, dy/l
	bx, by := float64(b.X)-ux*float64(head), float64(b.Y)-uy*float64(head)
	render.Segment(img, a.X, a.Y, int(math.Round(bx+ux*2)), int(math.Round(by+uy*2)), thickness, false)
	half := math.Max(float64(head)*0.5, float64(thickness)*0.9)
	p1 := [2]float64{bx - uy*half, by + ux*half}
	p2 := [2]float64{bx + uy*half, by - ux*half}
	tip := [2]float64{float64(b.X), float64(b.Y)}
	fillTriangle(img, tip, p1, p2)
}

func fillTriangle(img *image.Paletted, a, b, c [2]float64) {
	minX, maxX := math.Min(a[0], math.Min(b[0], c[0])), math.Max(a[0], math.Max(b[0], c[0]))
	minY, maxY := math.Min(a[1], math.Min(b[1], c[1])), math.Max(a[1], math.Max(b[1], c[1]))
	side := func(p, q [2]float64, x, y float64) float64 { return (q[0]-p[0])*(y-p[1]) - (q[1]-p[1])*(x-p[0]) }
	for y := int(math.Floor(minY)); y <= int(math.Ceil(maxY)); y++ {
		for x := int(math.Floor(minX)); x <= int(math.Ceil(maxX)); x++ {
			if !image.Pt(x, y).In(img.Rect) {
				continue
			}
			fx, fy := float64(x)+0.5, float64(y)+0.5
			d1, d2, d3 := side(a, b, fx, fy), side(b, c, fx, fy), side(c, a, fx, fy)
			if (d1 >= 0 && d2 >= 0 && d3 >= 0) || (d1 <= 0 && d2 <= 0 && d3 <= 0) {
				img.Set(x, y, color.Black)
			}
		}
	}
}

// flowText lists the flows compactly: PV, home, active vehicles, grid (+ import,
// - export) and battery (state of charge, + discharging, - charging).
func (e *evcc) flowText(w plugin.WidgetRequest) []string {
	s := e.cur
	signed := func(v float64) string {
		if math.Abs(v) < flowIdle {
			return e.power(0)
		}
		sign := "+"
		if v < 0 {
			sign = "-"
		}
		return sign + e.power(v)
	}
	line := func(l, v string) string { return plugin.Columns(l, v, w.Width) }
	out := []string{line("PV", e.power(s.PVPower)), line(e.t("Haus", "Home"), e.power(s.HomePower))}
	for _, lp := range s.activeLoadpoints() {
		out = append(out, line(e.t("Auto", "Car"), e.power(lp.ChargePower)))
	}
	out = append(out, line(e.t("Netz", "Grid"), signed(s.gridPower())))
	if s.hasBattery() {
		v := e.num(s.Battery.SoC, 0) + "% " + signed(s.Battery.Power)
		if w.Width < 20 {
			v = e.num(s.Battery.SoC, 0) + "%"
		}
		out = append(out, line(e.t("Akku", "Batt"), v))
	}
	return strings.Split(strings.Join(out, "\n"), "\n")
}
