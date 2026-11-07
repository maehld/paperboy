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

// flowEdge is a flow between two nodes; a positive power runs from `from` to
// `to`, a negative one the other way.
type flowEdge struct {
	from, to *flowNode
	power    float64
}

// flow draws PV, grid, home, battery and active loadpoints as boxes around
// the home, joined by arrows whose width follows the power. Only shapes,
// widths and labels carry the information, so it reads in two gray levels.
//
// Two layouts: the classic cross (PV above the home, battery below, grid
// left, loadpoints right) and, when the cell is too low for vertical arrows,
// a row with all sources stacked left of the home. Either way the diagram is
// centred and empty columns or rows are left out.
func (e *evcc) flow(w plugin.WidgetRequest, img *image.Paletted) {
	s := e.cur
	b := img.Rect.Inset(w.Style.Padding)
	if b.Dx() < 40 || b.Dy() < 40 {
		return
	}
	base := e.req.TextPx(w)
	rule := max(w.Style.Rule, 1)

	grid := s.gridPower()
	gridLabel := e.t("Netz", "Grid")
	switch {
	case grid >= flowIdle:
		gridLabel = e.t("Netzbezug", "Grid import")
	case grid <= -flowIdle:
		gridLabel = e.t("Einspeisung", "Grid export")
	}
	pv := &flowNode{label: "PV", value: e.power(s.PVPower), idle: s.PVPower < flowIdle}
	home := &flowNode{label: e.t("Haus", "Home"), value: e.power(s.HomePower)}
	nGrid := &flowNode{label: gridLabel, value: e.power(grid), idle: math.Abs(grid) < flowIdle}
	nodes := []*flowNode{pv, home, nGrid}
	edges := []flowEdge{{pv, home, s.PVPower}, {nGrid, home, grid}}
	var bat *flowNode
	if s.hasBattery() {
		bat = &flowNode{label: e.t("Akku ", "Battery ") + e.num(s.Battery.SoC, 0) + " %", value: e.power(s.Battery.Power),
			idle: math.Abs(s.Battery.Power) < flowIdle}
		nodes = append(nodes, bat)
		edges = append(edges, flowEdge{bat, home, s.Battery.Power}) // discharging (positive) flows into the home
	}
	lps := s.activeLoadpoints()
	if len(lps) > 3 {
		lps = lps[:3]
	}
	var loads []*flowNode
	for _, lp := range lps {
		n := &flowNode{label: lpName(lp), value: e.power(lp.ChargePower), idle: lp.ChargePower < flowIdle}
		if lp.VehicleSoC > 0 {
			n.label += " " + e.num(lp.VehicleSoC, 0) + " %"
		}
		loads = append(loads, n)
		nodes = append(nodes, n)
		edges = append(edges, flowEdge{home, n, lp.ChargePower})
	}

	// Box height from the text it holds; scaled down when the cell is too low.
	labelSz := max(base*0.8, e.req.PtPx(5.8), render.MinSize)
	valueSz := max(base*1.2, e.req.PtPx(6.5), render.MinSize)
	boxH0 := flowTextHeight(labelSz, valueSz) + valueSz*0.9
	minArrow := max(e.req.MMPx(5), e.req.PtPx(7)*1.8, 12)
	vgap := max(e.req.MMPx(1.5), 4)
	sources := []*flowNode{pv, nGrid}
	if bat != nil {
		sources = append(sources, bat)
	}
	rows := 2
	if bat != nil {
		rows = 3
	}
	h := float64(b.Dy())
	fit := func(n int, gap float64) float64 {
		return math.Min(1, (h-float64(n-1)*gap)/(float64(n)*boxH0))
	}
	kCross := fit(rows, minArrow)
	kRow := fit(max(len(sources), len(loads)), vgap)
	row := kRow > kCross*1.1
	k := kCross
	if row {
		k = kRow
	}
	boxH := boxH0 * k
	labelSz, valueSz = max(labelSz*k, render.MinSize), max(valueSz*k, render.MinSize)

	cols := 2
	if len(loads) > 0 {
		cols = 3
	}
	padX := valueSz * 0.6
	needW := boxH * 2.2
	for _, n := range nodes {
		needW = math.Max(needW, float64(max(render.Measure(n.label, labelSz, false), render.Measure(n.value, valueSz, true)))+2*padX)
	}
	wd := float64(b.Dx())
	boxW := math.Min(needW*1.15, (wd-float64(cols-1)*minArrow)/float64(cols))
	hgap := 0.0
	if cols > 1 {
		hgap = math.Min((wd-float64(cols)*boxW)/float64(cols-1), 3*minArrow)
	}
	ox := float64(b.Min.X) + (wd-float64(cols)*boxW-float64(cols-1)*hgap)/2
	colX := func(c int) float64 { return ox + float64(c)*(boxW+hgap) }
	rect := func(x, y, w, h float64) image.Rectangle {
		return image.Rect(int(math.Round(x)), int(math.Round(y)), int(math.Round(x+w)), int(math.Round(y+h)))
	}
	// stack places nodes in column c, vertically centred on cy.
	stack := func(ns []*flowNode, c int, cy, total float64) {
		n := float64(len(ns))
		bh := math.Min(boxH, (total-(n-1)*vgap)/n)
		y := cy - (n*bh+(n-1)*vgap)/2
		for _, nd := range ns {
			nd.rect = rect(colX(c), y, boxW, bh)
			y += bh + vgap
		}
	}

	var totalH, oy float64
	if row {
		totalH = h
		oy = float64(b.Min.Y)
		cy := oy + totalH/2
		stack(sources, 0, cy, totalH)
		home.rect = rect(colX(1), cy-boxH/2, boxW, boxH)
	} else {
		vg := math.Min((h-float64(rows)*boxH)/float64(rows-1), 2.5*minArrow)
		totalH = float64(rows)*boxH + float64(rows-1)*vg
		oy = float64(b.Min.Y) + (h-totalH)/2
		pv.rect = rect(colX(1), oy, boxW, boxH)
		home.rect = rect(colX(1), oy+boxH+vg, boxW, boxH)
		nGrid.rect = rect(colX(0), oy+boxH+vg, boxW, boxH)
		if bat != nil {
			bat.rect = rect(colX(1), oy+2*(boxH+vg), boxW, boxH)
		}
	}
	if len(loads) > 0 {
		cy := float64(home.rect.Min.Y+home.rect.Max.Y) / 2
		stack(loads, 2, cy, totalH)
	}

	// Arrows.
	maxP := 0.0
	for _, ed := range edges {
		maxP = math.Max(maxP, math.Abs(ed.power))
	}
	gap := max(e.req.MMPx(0.8), 3)
	minStroke := math.Max(e.req.PtPx(1.0), 2)
	maxStroke := math.Max(e.req.PtPx(5.0), minStroke+2)
	head := math.Max(e.req.PtPx(7), 8)
	for _, ed := range edges {
		a, z := flowAnchors(ed.from, ed.to, edges, gap)
		p := ed.power
		if p < 0 {
			a, z, p = z, a, -p
		}
		if p < flowIdle || maxP <= 0 {
			dashedLine(img, a, z, float64(rule), math.Max(e.req.MMPx(1), 3))
			continue
		}
		stroke := minStroke + (maxStroke-minStroke)*p/maxP
		arrow(img, a, z, stroke, math.Max(head, stroke*2.6))
	}

	// Boxes.
	gray := render.Levels(img.Palette) == 4
	frame := max(rule, int(math.Round(e.req.PtPx(0.8))))
	dash := max(int(e.req.MMPx(1.2)), 3)
	for _, n := range nodes {
		r := n.rect
		ink := color.Color(color.Black)
		t := frame
		if n == home {
			t = frame * 2
		}
		if n.idle {
			dashedFrame(img, r, rule, dash)
			if gray {
				ink = render.DarkGray
			}
		} else {
			render.HLine(img, r.Min.X, r.Min.Y, r.Max.X, t)
			render.HLine(img, r.Min.X, r.Max.Y-t, r.Max.X, t)
			render.VLine(img, r.Min.X, r.Min.Y, r.Max.Y, t)
			render.VLine(img, r.Max.X-t, r.Min.Y, r.Max.Y, t)
		}
		lsz, vsz := labelSz, valueSz
		if f := float64(r.Dy()) / boxH; f < 0.999 {
			lsz, vsz = max(lsz*f, render.MinSize), max(vsz*f, render.MinSize)
		}
		inner := r.Dx() - 2*int(padX/2)
		cx := (r.Min.X + r.Max.X) / 2
		top := float64(r.Min.Y+r.Max.Y)/2 - flowTextHeight(lsz, vsz)/2
		lb := int(math.Round(top + lsz*0.72))
		vb := int(math.Round(top + flowTextHeight(lsz, vsz)))
		render.Text(img, n.label, cx, lb, render.Style{Size: lsz, Shrink: true}, inner, ink, render.Center)
		render.Text(img, n.value, cx, vb, render.Style{Size: vsz, Bold: true, Shrink: true}, inner, ink, render.Center)
	}
}

// flowTextHeight is the height from the label's cap line to the value's
// baseline.
func flowTextHeight(label, value float64) float64 { return label*0.72 + value*0.45 + value*0.72 }

// flowAnchors returns the points where an edge between a and b leaves and
// enters the boxes. Edges that meet the same side of a box are spread along it.
func flowAnchors(a, b *flowNode, edges []flowEdge, gap float64) (fpt, fpt) {
	type side int
	const (
		top side = iota
		bottom
		left
		right
	)
	sideOf := func(r, o image.Rectangle) side {
		switch {
		case o.Min.X >= r.Max.X:
			return right
		case o.Max.X <= r.Min.X:
			return left
		case o.Min.Y >= r.Max.Y:
			return bottom
		}
		return top
	}
	anchor := func(n, o *flowNode) fpt {
		r, sd := n.rect, sideOf(n.rect, o.rect)
		// Position of this edge among all edges on the same side.
		var others []*flowNode
		for _, ed := range edges {
			var m *flowNode
			switch n {
			case ed.from:
				m = ed.to
			case ed.to:
				m = ed.from
			default:
				continue
			}
			if sideOf(r, m.rect) == sd {
				others = append(others, m)
			}
		}
		i := 0
		for j, m := range others {
			if m == o {
				i = j
			}
		}
		off := 0.5
		if len(others) > 1 {
			off = 0.25 + 0.5*float64(i)/float64(len(others)-1)
		}
		x0, y0, x1, y1 := float64(r.Min.X), float64(r.Min.Y), float64(r.Max.X), float64(r.Max.Y)
		switch sd {
		case right:
			return fpt{x1 + gap, y0 + (y1-y0)*off}
		case left:
			return fpt{x0 - gap, y0 + (y1-y0)*off}
		case bottom:
			return fpt{x0 + (x1-x0)*off, y1 + gap}
		}
		return fpt{x0 + (x1-x0)*off, y0 - gap}
	}
	return anchor(a, b), anchor(b, a)
}

// fpt is a point with sub-pixel precision.
type fpt struct{ x, y float64 }

// arrow draws a shaft of the given thickness from a to b with a filled head
// at b, both symmetric around the line from a to b.
func arrow(img *image.Paletted, a, b fpt, thickness, head float64) {
	dx, dy := b.x-a.x, b.y-a.y
	l := math.Hypot(dx, dy)
	if l < 1 {
		return
	}
	head = math.Min(head, l*0.6)
	ux, uy := dx/l, dy/l
	nx, ny := -uy, ux
	base := fpt{b.x - ux*head, b.y - uy*head}
	t := thickness / 2
	// The shaft runs half a pixel into the head so no gap shows.
	end := fpt{base.x + ux*0.5, base.y + uy*0.5}
	fillPolygon(img, color.Black,
		fpt{a.x + nx*t, a.y + ny*t}, fpt{end.x + nx*t, end.y + ny*t},
		fpt{end.x - nx*t, end.y - ny*t}, fpt{a.x - nx*t, a.y - ny*t})
	half := math.Max(head*0.55, thickness*1.1)
	fillPolygon(img, color.Black, b, fpt{base.x + nx*half, base.y + ny*half}, fpt{base.x - nx*half, base.y - ny*half})
}

// dashedLine draws a dashed line of the given width centred on a–b.
func dashedLine(img *image.Paletted, a, b fpt, width, dash float64) {
	dx, dy := b.x-a.x, b.y-a.y
	l := math.Hypot(dx, dy)
	if l < 1 {
		return
	}
	ux, uy := dx/l, dy/l
	nx, ny := -uy*width/2, ux*width/2
	for s := 0.0; s < l; s += 2 * dash {
		e := math.Min(s+dash, l)
		p, q := fpt{a.x + ux*s, a.y + uy*s}, fpt{a.x + ux*e, a.y + uy*e}
		fillPolygon(img, color.Black, fpt{p.x + nx, p.y + ny}, fpt{q.x + nx, q.y + ny}, fpt{q.x - nx, q.y - ny}, fpt{p.x - nx, p.y - ny})
	}
}

// dashedFrame draws the outline of r with dashes of length dash.
func dashedFrame(img *image.Paletted, r image.Rectangle, t, dash int) {
	for x := r.Min.X; x < r.Max.X; x += 2 * dash {
		e := min(x+dash, r.Max.X)
		render.HLine(img, x, r.Min.Y, e, t)
		render.HLine(img, x, r.Max.Y-t, e, t)
	}
	for y := r.Min.Y; y < r.Max.Y; y += 2 * dash {
		e := min(y+dash, r.Max.Y)
		render.VLine(img, r.Min.X, y, e, t)
		render.VLine(img, r.Max.X-t, y, e, t)
	}
}

// fillPolygon fills a convex polygon, sampling pixel centres.
func fillPolygon(img *image.Paletted, c color.Color, pts ...fpt) {
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
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
