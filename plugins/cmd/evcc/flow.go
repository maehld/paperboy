package main

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

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

// flowIdleEnergy is the energy in kWh below which a period shows no flow.
const flowIdleEnergy = 0.05

func lpName(lp *loadpoint) string {
	if lp.VehicleTitle != "" {
		return lp.VehicleTitle
	}
	return lp.Title
}

// hasBattery reports whether evcc knows a home battery.
func (s *state) hasBattery() bool { return s.Battery != nil && s.Battery.Capacity > 0 }

// Node indices of the flow model; loadpoint i is flowLP+i.
const (
	flowPV = iota
	flowGrid
	flowBattery
	flowHome
	flowLP
)

// flowShare is the power flowing from one node to another.
type flowShare struct {
	from, to int
	power    float64
}

// splitFlows divides evcc's measurements into flows between pairs of nodes,
// the way Home Assistant's energy distribution does. Sources are PV, grid
// import (grid > 0) and battery discharge (battery > 0); sinks are the home,
// the loadpoints, battery charging and grid export. The sources are spent in
// this order, each filling the sinks in the order given:
//
//  1. PV: home, loadpoints, battery, grid (export).
//  2. Battery discharge: home, loadpoints, grid (export).
//  3. Grid import: home, loadpoints, battery.
//
// So PV serves the consumers first and charges the battery or exports only
// what is left, and a deficit is covered by the battery before the grid.
// Measurements rarely balance exactly; a remainder on either side is dropped.
func splitFlows(pv, grid, battery, home float64, lps []float64) []flowShare {
	need := make([]float64, flowLP+len(lps))
	need[flowHome] = max(home, 0)
	need[flowBattery] = max(-battery, 0)
	need[flowGrid] = max(-grid, 0)
	consumers := []int{flowHome}
	for i, p := range lps {
		need[flowLP+i] = max(p, 0)
		consumers = append(consumers, flowLP+i)
	}
	var out []flowShare
	spend := func(from int, have float64, extra ...int) {
		for _, to := range append(append([]int{}, consumers...), extra...) {
			p := math.Min(have, need[to])
			if p > 0 {
				out = append(out, flowShare{from, to, p})
				have -= p
				need[to] -= p
			}
		}
	}
	spend(flowPV, max(pv, 0), flowBattery, flowGrid)
	spend(flowBattery, max(battery, 0), flowGrid)
	spend(flowGrid, max(grid, 0), flowBattery)
	return out
}

// flowNode is one box of the flow diagram.
type flowNode struct {
	icon, label, short, value string
	net                       float64 // positive flows into the hub
	idle                      bool
	rect                      image.Rectangle
}

// flowModel is the topology and the values of one flow: current power or the
// energy of a period.
type flowModel struct {
	nodes   []*flowNode
	battery int // index of the battery, or -1
	lps     int // number of loadpoints at the end of nodes
	energy  bool
}

// flow draws the energy flow: the hub in the middle, PV above, the battery
// below, the grid left and the home right. Loadpoints are text lines below the
// home and hang on a thin tree. A link is drawn only while power or energy
// flows; an idle node keeps its card and value, but has no line.
func (e *evcc) flow(w plugin.WidgetRequest, img *image.Paletted) error {
	m, err := e.flowModel(w)
	if err != nil {
		return err
	}
	if img.Rect.Dx() < 240 || img.Rect.Dy() < 180 {
		return plugin.TooSmall(w)
	}
	b := img.Rect.Inset(w.Style.Padding)
	base := e.req.TextPx(w)
	rule := max(w.Style.Rule, 1)
	W, H := float64(b.Dx()), float64(b.Dy())
	gap := math.Max(e.req.MMPx(1.5), 5)
	hubD := math.Min(math.Min(H*0.24, W*0.10), 100)
	sideW := math.Min(W*0.30, 300)
	midW := math.Min(W*0.24, 240)
	cardH := math.Min(H*0.26, 110)
	cx := float64(b.Min.X) + W/2
	cy := float64(b.Min.Y) + H/2

	nlp := m.lps
	nodes := m.nodes
	pv, grid := nodes[0], nodes[1]
	home := nodes[len(nodes)-nlp-1]
	var bat *flowNode
	if m.battery >= 0 {
		bat = nodes[m.battery]
	}
	lpn := nodes[len(nodes)-nlp:]

	pv.rect = rectAt(cx-midW/2, float64(b.Min.Y), midW, cardH)
	if bat != nil {
		bat.rect = rectAt(cx-midW/2, float64(b.Max.Y)-cardH, midW, cardH)
	}
	grid.rect = rectAt(float64(b.Min.X), cy-cardH/2, sideW, cardH)

	// The chargers are text lines, so they need far less height than cards. A
	// group that does not fit shrinks its rows before the home card moves.
	rowH := math.Min(cardH*0.40, 30)
	groupH := cardH + float64(nlp)*rowH + float64(nlp)*gap
	if groupH > H && nlp > 0 {
		rowH = math.Max((H-cardH-float64(nlp)*gap)/float64(nlp), 10)
	}
	homeTop, rowY := flowPlacement(b, cy, cardH, rowH, gap, nlp)
	home.rect = rectAt(float64(b.Max.X)-sideW, homeTop, sideW, cardH)
	for i, n := range lpn {
		n.rect = rectAt(float64(b.Max.X)-sideW, rowY[i], sideW, rowH)
	}

	stroke := math.Max(e.req.PtPx(1.2), 1)
	head := math.Max(e.req.PtPx(4.5), 7)
	hubTop, hubBottom := cy-hubD/2, cy+hubD/2
	hubLeft, hubRight := cx-hubD/2, cx+hubD/2

	links := make([][]fpt, len(nodes))
	links[flowPV] = []fpt{{cx, float64(pv.rect.Max.Y) + gap}, {cx, hubTop - 2}}
	if bat != nil {
		links[m.battery] = []fpt{{cx, float64(bat.rect.Min.Y) - gap}, {cx, hubBottom + 2}}
	}
	links[flowGrid] = []fpt{{float64(grid.rect.Max.X) + gap, cy}, {hubLeft - 2, cy}}
	homeY := float64(home.rect.Min.Y+home.rect.Max.Y) / 2
	laneX := (hubRight + float64(home.rect.Min.X)) / 2
	links[flowHome] = []fpt{{float64(home.rect.Min.X) - 1, homeY}, {laneX, homeY}, {laneX, cy}, {hubRight + 2, cy}}

	for i, p := range links {
		if p == nil {
			continue
		}
		e.flowArrow(img, p, nodes[i].net, stroke, head, nodes[i].idle)
	}

	// Hub: a circle with the inverter bolt.
	fillDisc(img, cx, cy, hubD/2, color.Black)
	fillDisc(img, cx, cy, hubD/2-3, color.White)
	drawFlowIcon(img, "hub", rectAt(cx-hubD*0.40, cy-hubD*0.40, hubD*0.80, hubD*0.80), color.Black)

	for i, n := range nodes {
		if n == nil || i >= len(nodes)-nlp {
			continue
		}
		e.flowCard(img, n, base, rule)
	}

	// The chargers hang on a thin tree below the home. Only active chargers
	// get a branch.
	if nlp > 0 {
		lastActive := -1
		for i, n := range lpn {
			if !n.idle {
				lastActive = i
			}
		}
		if lastActive >= 0 {
			tx0 := int(float64(home.rect.Min.X) + sideW*0.045)
			tx1 := int(float64(home.rect.Min.X) + sideW*0.085)
			end := int(math.Round(float64(lpn[lastActive].rect.Min.Y+lpn[lastActive].rect.Max.Y) / 2))
			render.VLine(img, tx0, home.rect.Max.Y, end, 1)
			for _, n := range lpn {
				if n.idle {
					continue
				}
				ly := int(math.Round(float64(n.rect.Min.Y+n.rect.Max.Y) / 2))
				arrowPath(img, []fpt{{float64(tx0), float64(ly)}, {float64(tx1 + 5), float64(ly)}}, 1, math.Max(base*0.16, 3.5))
			}
		}
	}
	for _, n := range lpn {
		e.flowTextRow(img, n, home.rect.Min.X+int(sideW*0.10), home.rect.Max.X, base)
	}
	return nil
}

// flowPlacement keeps the home card at the vertical centre. The loadpoint rows
// sit below it, and the whole group moves up only when the last row would
// cross the bottom padding.
func flowPlacement(b image.Rectangle, cy, cardH, rowH, gap float64, n int) (float64, []float64) {
	top := cy - cardH/2
	groupH := cardH + float64(n)*(rowH+gap)
	switch {
	case groupH > float64(b.Dy()):
		top = float64(b.Min.Y)
	case top+groupH > float64(b.Max.Y):
		top -= top + groupH - float64(b.Max.Y)
	}
	rows := make([]float64, n)
	for i := range rows {
		rows[i] = top + cardH + gap + float64(i)*(rowH+gap)
	}
	return top, rows
}

// flowArrow draws the link of one node. Every path runs from the node to the
// hub; negative power leaves the hub, so the arrowhead belongs at the node.
func (e *evcc) flowArrow(img *image.Paletted, path []fpt, net, stroke, head float64, idle bool) {
	if idle || len(path) < 2 {
		return
	}
	if net < 0 {
		r := make([]fpt, len(path))
		for i := range path {
			r[i] = path[len(path)-1-i]
		}
		path = r
	}
	// Drop repeated points: arrowPath takes the head from the last segment.
	clean := path[:0]
	for _, p := range path {
		if len(clean) == 0 || math.Hypot(clean[len(clean)-1].x-p.x, clean[len(clean)-1].y-p.y) > 0.5 {
			clean = append(clean, p)
		}
	}
	if len(clean) < 2 {
		return
	}
	arrowPath(img, clean, stroke, head)
}

// flowModel builds the nodes for the widget's period.
func (e *evcc) flowModel(w plugin.WidgetRequest) (*flowModel, error) {
	period := w.Arg().String("period", "now")
	if period == "now" {
		return e.flowNow(), nil
	}
	return e.flowPeriod(period)
}

// flowNow reads the current power from /api/state.
func (e *evcc) flowNow() *flowModel {
	s := e.cur
	grid := s.gridPower()
	gridLabel := e.t("Netz", "Grid")
	switch {
	case grid >= flowIdle:
		gridLabel = e.t("Netzbezug", "Grid import")
	case grid <= -flowIdle:
		gridLabel = e.t("Einspeisung", "Grid export")
	}
	lps := s.activeLoadpoints()
	if len(lps) > 3 {
		lps = lps[:3]
	}
	m := &flowModel{battery: -1, lps: len(lps)}
	m.nodes = append(m.nodes,
		&flowNode{icon: "sun", label: "PV", short: "PV", value: e.power(s.PVPower), net: s.PVPower},
		&flowNode{icon: "grid", label: gridLabel, short: e.t("Netz", "Grid"), value: e.power(grid), net: grid},
	)
	if s.hasBattery() {
		m.battery = len(m.nodes)
		m.nodes = append(m.nodes, &flowNode{icon: "battery",
			label: e.t("Akku ", "Battery ") + e.num(s.Battery.SoC, 0) + " %",
			short: e.t("Akku", "Battery"),
			value: e.power(s.Battery.Power), net: s.Battery.Power})
	}
	m.nodes = append(m.nodes, &flowNode{icon: "home", label: e.t("Haus", "Home"), short: e.t("Haus", "Home"), value: e.power(s.HomePower), net: -s.HomePower})
	for _, lp := range lps {
		n := &flowNode{icon: "car", label: lpName(lp), short: lpName(lp), value: e.power(lp.ChargePower), net: -lp.ChargePower}
		if lp.VehicleSoC > 0 {
			n.label += " " + e.num(lp.VehicleSoC, 0) + " %"
		}
		m.nodes = append(m.nodes, n)
	}
	for _, n := range m.nodes {
		n.idle = math.Abs(n.net) < flowIdle
	}
	return m
}

// flowPeriod sums the energy history of a period. It uses the same categories
// as the history widget and the same day buckets.
func (e *evcc) flowPeriod(period string) (*flowModel, error) {
	from, to, ok := e.periodRange(period)
	if !ok {
		return nil, fmt.Errorf("unknown period %q", period)
	}
	days, _, histErr := e.historyData()
	if days == nil {
		if histErr != nil {
			return nil, fmt.Errorf("evcc history: %w", histErr)
		}
		return nil, errors.New("evcc history is still loading")
	}
	row := historyRow{from: from, to: to}
	sum := func(key, metric string) float64 {
		list, ok := days[key]
		if !ok {
			return 0
		}
		return e.historySum(list, row, metric)
	}
	imported, exported := sum("grid", "energy"), sum("grid", "return")
	charged, discharged := sum("battery", "energy"), sum("battery", "return")
	pv, home := sum("pv", "energy"), sum("home", "energy")
	grid := imported - exported
	battery := discharged - charged

	s := e.cur
	var lps []*loadpoint
	for i := range s.Loadpoints {
		if i >= 3 {
			break
		}
		lp := &s.Loadpoints[i]
		if sum(loadpointKey(lp.Title), "energy") < flowIdleEnergy {
			continue
		}
		lps = append(lps, lp)
	}
	m := &flowModel{battery: -1, lps: len(lps), energy: true}
	gridLabel := e.t("Netz", "Grid")
	switch {
	case grid >= flowIdleEnergy:
		gridLabel = e.t("Netzbezug", "Grid import")
	case grid <= -flowIdleEnergy:
		gridLabel = e.t("Einspeisung", "Grid export")
	}
	m.nodes = append(m.nodes,
		&flowNode{icon: "sun", label: "PV", short: "PV", value: e.kwhShort(pv), net: pv},
		&flowNode{icon: "grid", label: gridLabel, short: e.t("Netz", "Grid"), value: e.kwhShort(math.Abs(grid)), net: grid},
	)
	if s.hasBattery() {
		m.battery = len(m.nodes)
		m.nodes = append(m.nodes, &flowNode{icon: "battery", label: e.t("Akku", "Battery"), short: e.t("Akku", "Battery"),
			value: e.kwhShort(math.Abs(battery)), net: battery})
	}
	m.nodes = append(m.nodes, &flowNode{icon: "home", label: e.t("Haus", "Home"), short: e.t("Haus", "Home"), value: e.kwhShort(math.Abs(home)), net: -home})
	for _, lp := range lps {
		energy := sum(loadpointKey(lp.Title), "energy")
		m.nodes = append(m.nodes, &flowNode{icon: "car", label: lpName(lp), short: lpName(lp), value: e.kwhShort(math.Abs(energy)), net: -energy})
	}
	for _, n := range m.nodes {
		n.idle = math.Abs(n.net) < flowIdleEnergy
	}
	return m, nil
}

// kwhShort formats an energy of the flow. Above 10 kWh the decimal would only
// cost room in a small cell.
func (e *evcc) kwhShort(v float64) string {
	if math.Abs(v) >= 10 {
		return e.grouped(v) + " kWh"
	}
	return e.num(v, 1) + " kWh"
}

// flowCard draws one node as a rounded card with an icon, label and value.
func (e *evcc) flowCard(img *image.Paletted, n *flowNode, base float64, rule int) {
	r := n.rect
	if r.Empty() {
		return
	}
	pad := max(int(base*0.30), 5)
	rad := math.Min(float64(r.Dy())*0.18, 10)
	fillRoundRect(img, r, rad, color.White)
	roundFrame(img, r, rad, max(rule, 1))
	innerH := r.Dy() - 2*pad
	labelSz := min(float64(innerH)*0.30, base*0.80)
	valueSz := min(float64(innerH)*0.56, base*1.40)
	iw := float64(innerH) * 0.60
	if iw < 10 || float64(r.Dx()) < iw+3*float64(pad) {
		iw = 0
	}
	tx0 := r.Min.X + pad
	if iw > 0 {
		drawFlowIcon(img, n.icon, rectAt(float64(r.Min.X+pad), float64(r.Min.Y+pad), iw, float64(innerH)), color.Black)
		tx0 += int(iw) + pad/2
	}
	textW := r.Max.X - pad - tx0
	label := n.label
	if n.short != "" && render.Measure(label, labelSz, false) > textW && render.Measure(n.short, labelSz, false) <= textW {
		label = n.short
	}
	tcx := tx0 + textW/2
	th := labelSz*0.72 + valueSz*0.45 + valueSz*0.62
	top := float64(r.Min.Y+r.Max.Y)/2 - th/2
	render.Text(img, label, tcx, int(math.Round(top+labelSz*0.72)), render.Style{Size: labelSz, Shrink: true}, textW, color.Black, render.Center)
	render.Text(img, n.value, tcx, int(math.Round(top+th)), render.Style{Size: valueSz, Bold: true, Shrink: true}, textW, color.Black, render.Center)
}

// flowTextRow draws one charger below the home.
func (e *evcc) flowTextRow(img *image.Paletted, n *flowNode, x0, x1 int, base float64) {
	r := n.rect
	labelSz := math.Min(float64(r.Dy())*0.52, base*0.88)
	valueSz := math.Min(float64(r.Dy())*0.62, base)
	cy := float64(r.Min.Y+r.Max.Y) / 2
	valueW := min(render.Measure(n.value, valueSz, true), int(float64(x1-x0)*0.60))
	labelW := x1 - x0 - valueW - 6
	label := n.label
	if n.short != "" && render.Measure(label, labelSz, false) > labelW && render.Measure(n.short, labelSz, false) <= labelW {
		label = n.short
	}
	render.Text(img, label, x0, int(math.Round(cy+labelSz*0.36)), render.Style{Size: labelSz, Shrink: true}, labelW, color.Black, render.Left)
	render.Text(img, n.value, x1, int(math.Round(cy+valueSz*0.36)), render.Style{Size: valueSz, Bold: true, Shrink: true}, valueW, color.Black, render.Right)
}

// fpt is a point with sub-pixel precision.
type fpt struct{ x, y float64 }

func sub(a, b fpt) fpt { return fpt{a.x - b.x, a.y - b.y} }

func unit(p fpt) fpt {
	l := math.Hypot(p.x, p.y)
	if l == 0 {
		return fpt{}
	}
	return fpt{p.x / l, p.y / l}
}

// arrowPath draws a polyline of the given thickness with a filled head at its
// last point; every segment is a polygon symmetric around its centre line and
// the joints are filled, so the line has no gaps.
func arrowPath(img *image.Paletted, pts []fpt, thickness, head float64) {
	n := len(pts)
	last := sub(pts[n-1], pts[n-2])
	l := math.Hypot(last.x, last.y)
	if l < 1 {
		return
	}
	head = math.Min(head, l*0.6)
	u := unit(last)
	base := fpt{pts[n-1].x - u.x*head, pts[n-1].y - u.y*head}
	shaft := append(append([]fpt{}, pts[:n-1]...), fpt{base.x + u.x*0.5, base.y + u.y*0.5})
	t := thickness / 2
	for i := 0; i+1 < len(shaft); i++ {
		a, z := shaft[i], shaft[i+1]
		d := unit(sub(z, a))
		nx, ny := -d.y*t, d.x*t
		fillPolygon(img, color.Black, fpt{a.x + nx, a.y + ny}, fpt{z.x + nx, z.y + ny}, fpt{z.x - nx, z.y - ny}, fpt{a.x - nx, a.y - ny})
		if i > 0 {
			var joint []fpt
			for k := 0; k < 8; k++ {
				ang := float64(k) * math.Pi / 4
				joint = append(joint, fpt{a.x + t*math.Cos(ang), a.y + t*math.Sin(ang)})
			}
			fillPolygon(img, color.Black, joint...)
		}
	}
	half := math.Max(head*0.55, thickness*1.1)
	nx, ny := -u.y, u.x
	fillPolygon(img, color.Black, pts[n-1], fpt{base.x + nx*half, base.y + ny*half}, fpt{base.x - nx*half, base.y - ny*half})
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
