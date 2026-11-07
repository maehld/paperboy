package main

import (
	"embed"
	"image"
	"image/color"
	"image/png"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

//go:embed icons/*.png
var iconFiles embed.FS

var (
	iconOnce  sync.Once
	iconMasks map[string]*render.Mask
)

// icon returns the mask of an embedded OpenMoji pictogram by name.
func icon(name string) *render.Mask {
	iconOnce.Do(func() {
		iconMasks = map[string]*render.Mask{}
		entries, _ := iconFiles.ReadDir("icons")
		for _, e := range entries {
			f, err := iconFiles.Open("icons/" + e.Name())
			if err != nil {
				continue
			}
			img, err := png.Decode(f)
			f.Close()
			if err == nil {
				iconMasks[strings.TrimSuffix(e.Name(), ".png")] = render.NewMask(img)
			}
		}
	})
	return iconMasks[name]
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
	icon, label, value string
	idle, small        bool
	rect               image.Rectangle
}

func (n *flowNode) centre() fpt {
	return fpt{float64(n.rect.Min.X+n.rect.Max.X) / 2, float64(n.rect.Min.Y+n.rect.Max.Y) / 2}
}

// flow draws the energy flow in the arrangement of SolarAI's dashboard: the
// house connection (hub) in the middle, PV above it, the battery below, the
// grid left and the home right, with the loadpoints as small nodes hanging off
// the home (below it, or right of it in wide cells). Each flow of splitFlows
// is an arrow from its source to its sink like in Home Assistant's energy
// distribution: between opposite nodes straight through the hub, between
// neighbours as an elbow around the hub's corner. Flows into loadpoints are
// drawn into the home and on to the loadpoint. Arrow widths follow the power;
// only shapes, widths and labels carry the information, so it reads in two
// gray levels.
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
	lps := s.activeLoadpoints()
	if len(lps) > 3 {
		lps = lps[:3]
	}
	nodes := make([]*flowNode, flowLP+len(lps))
	nodes[flowPV] = &flowNode{icon: "sun", label: "PV", value: e.power(s.PVPower), idle: s.PVPower < flowIdle}
	nodes[flowGrid] = &flowNode{icon: "grid", label: gridLabel, value: e.power(grid), idle: math.Abs(grid) < flowIdle}
	nodes[flowHome] = &flowNode{icon: "home", label: e.t("Haus", "Home"), value: e.power(s.HomePower), idle: s.HomePower < flowIdle}
	batPower := 0.0
	if s.hasBattery() {
		batPower = s.Battery.Power
		nodes[flowBattery] = &flowNode{icon: "battery", label: e.t("Akku ", "Battery ") + e.num(s.Battery.SoC, 0) + " %",
			value: e.power(batPower), idle: math.Abs(batPower) < flowIdle}
	}
	lpPower := make([]float64, len(lps))
	for i, lp := range lps {
		n := &flowNode{icon: "car", label: lpName(lp), value: e.power(lp.ChargePower), idle: lp.ChargePower < flowIdle, small: true}
		if lp.VehicleSoC > 0 {
			n.label += " " + e.num(lp.VehicleSoC, 0) + " %"
		}
		nodes[flowLP+i] = n
		lpPower[i] = lp.ChargePower
	}
	// Flows into loadpoints enter the home first.
	into := map[[2]int]float64{}
	for _, sh := range splitFlows(s.PVPower, grid, batPower, s.HomePower, lpPower) {
		to := sh.to
		if to >= flowLP {
			to = flowHome
		}
		into[[2]int{sh.from, to}] += sh.power
	}
	bat := nodes[flowBattery]
	hub := &flowNode{icon: "hub"}
	nl := float64(len(lps))

	// Sizes. The hub sits between PV and battery; d is the distance from the
	// hub's centre to the PV and battery boxes, long enough for an elbow and
	// an arrowhead.
	labelSz := max(base*0.8, e.req.PtPx(5.8), render.MinSize)
	valueSz := max(base*1.2, e.req.PtPx(6.5), render.MinSize)
	boxH0 := flowTextHeight(labelSz, valueSz) + valueSz*0.9
	const smallF = 0.72
	minArrow := max(e.req.MMPx(5), e.req.PtPx(7)*1.8, 12)
	vgap := max(e.req.MMPx(1.5), 4)
	gap := max(e.req.MMPx(0.8), 3)
	minStroke := math.Max(e.req.PtPx(1.0), 2)
	maxStroke := math.Max(e.req.PtPx(4.0), minStroke+2)
	head := math.Max(e.req.PtPx(7), 8)
	h, wd := float64(b.Dy()), float64(b.Dx())
	// q = 0.33 boxH + qc is the elbow offset from the hub's centre, d = q + dc.
	qc := gap + maxStroke/2
	dc := head * 1.6
	height := func(bh float64) float64 { // diagram height without loadpoints below
		d := 0.33*bh + qc + dc
		if bat != nil {
			return 2*bh + 2*d
		}
		return bh + d + bh/2
	}
	k := 1.0
	if t := height(boxH0); t > h {
		k = (h - height(0)) / (height(boxH0) - height(0))
	}
	k = math.Max(k, 0.2)
	boxH := boxH0 * k
	labelSz, valueSz = max(labelSz*k, render.MinSize), max(valueSz*k, render.MinSize)
	hubS := boxH * 0.66
	q := 0.33*boxH + qc
	d := q + dc
	// Spare height lengthens the vertical arrows a little.
	spare := h - height(boxH)
	if bat != nil {
		spare /= 2
	}
	grow := math.Max(0, math.Min(spare, 1.5*minArrow))
	d += grow
	padX := valueSz * 0.5
	needW := boxH * 2.2
	for _, n := range nodes {
		if n != nil && !n.small {
			needW = math.Max(needW, boxH*0.62+padX+float64(max(render.Measure(n.label, labelSz, false), render.Measure(n.value, valueSz, true)))+2*padX)
		}
	}
	// Loadpoints go below the home when they fit there, else into a column
	// right of it when the width allows, else below with less height.
	diagH := height(boxH) + grow
	if bat != nil {
		diagH += grow
	}
	below := diagH/2 - boxH/2 // from the home's bottom to the diagram's bottom
	lpH := smallF * boxH
	lpRight := false
	if nl > 0 && below < nl*(lpH+vgap) {
		if (wd-2*minArrow-head*3)/3.85 >= needW*0.8 {
			lpRight = true
		} else {
			extra := math.Max(0, nl*(lpH+vgap)-below)
			if diagH+extra > h {
				lpH = math.Max((below+h-diagH)/nl-vgap, valueSz*1.3)
			}
		}
	}
	// A loadpoint column right of the home is narrower and needs room for
	// the bus and the arrowheads.
	lpW, lpGap := 0.0, 0.0
	cols := 3.0
	if lpRight {
		lpW, lpGap = 0.85, head*2.4+maxStroke+2*gap
	}
	boxW := math.Min(needW*1.1, (wd-(cols-1)*minArrow-lpGap)/(cols+lpW))
	hgap := math.Min((wd-(cols+lpW)*boxW-lpGap)/(cols-1), 3*minArrow)
	ox := float64(b.Min.X) + (wd-(cols+lpW)*boxW-(cols-1)*hgap-lpGap)/2
	col := func(c float64) float64 { return ox + c*(boxW+hgap) }
	totalH := diagH
	if nl > 0 && !lpRight {
		totalH = math.Max(diagH, diagH/2+boxH/2+nl*(lpH+vgap))
	}
	oy := float64(b.Min.Y) + (h-totalH)/2
	rect := func(x, y, w, h float64) image.Rectangle {
		return image.Rect(int(math.Round(x)), int(math.Round(y)), int(math.Round(x+w)), int(math.Round(y+h)))
	}
	cy := oy + boxH + d // hub centre
	if bat == nil {
		cy = oy + diagH - boxH/2
	}
	cx := col(1) + boxW/2
	hc := fpt{cx, cy}
	nodes[flowPV].rect = rect(col(1), cy-d-boxH, boxW, boxH)
	if bat != nil {
		bat.rect = rect(col(1), cy+d, boxW, boxH)
	}
	nodes[flowGrid].rect = rect(col(0), cy-boxH/2, boxW, boxH)
	nodes[flowHome].rect = rect(col(2), cy-boxH/2, boxW, boxH)
	hub.rect = rect(cx-hubS/2, cy-hubS/2, hubS, hubS)
	home := nodes[flowHome].rect
	var lpPaths [][]fpt
	if lpRight {
		y := cy - (nl*lpH+(nl-1)*vgap)/2
		busX := float64(home.Max.X) + gap + maxStroke
		for _, n := range nodes[flowLP:] {
			n.rect = rect(float64(home.Max.X)+lpGap, y, lpW*boxW, lpH)
			ly := y + lpH/2
			lpPaths = append(lpPaths, []fpt{{float64(home.Max.X) + gap, cy}, {busX, cy}, {busX, ly}, {float64(n.rect.Min.X) - gap, ly}})
			y += lpH + vgap
		}
	} else {
		y := float64(home.Max.Y) + vgap
		indent := math.Max(boxW*0.18, head*2.2+maxStroke)
		busX := float64(home.Min.X) + maxStroke
		for _, n := range nodes[flowLP:] {
			n.rect = rect(float64(home.Min.X)+indent, y, boxW-indent, lpH)
			ly := y + lpH/2
			lpPaths = append(lpPaths, []fpt{{busX, float64(home.Max.Y) + gap}, {busX, ly}, {float64(n.rect.Min.X) - gap, ly}})
			y += lpH + vgap
		}
	}

	// Arrows. Each node lies in one direction from the hub.
	dir := map[int]fpt{flowPV: {0, -1}, flowGrid: {-1, 0}, flowBattery: {0, 1}, flowHome: {1, 0}}
	edge := func(n int, along fpt) fpt { // point on n's side facing the hub, at the given cross offset
		r := nodes[n].rect
		switch n {
		case flowPV:
			return fpt{along.x, float64(r.Max.Y) + gap}
		case flowBattery:
			return fpt{along.x, float64(r.Min.Y) - gap}
		case flowGrid:
			return fpt{float64(r.Max.X) + gap, along.y}
		}
		return fpt{float64(r.Min.X) - gap, along.y}
	}
	maxP := 0.0
	for _, p := range into {
		maxP = math.Max(maxP, p)
	}
	for _, p := range lpPower {
		maxP = math.Max(maxP, p)
	}
	strokeOf := func(p float64) float64 { return minStroke + (maxStroke-minStroke)*p/maxP }
	used := map[int]bool{}
	keys := make([][2]int, 0, len(into))
	for k := range into {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return into[keys[i]] < into[keys[j]] })
	for _, key := range keys {
		p := into[key]
		if p < flowIdle || nodes[key[0]] == nil || nodes[key[1]] == nil {
			continue
		}
		used[key[0]], used[key[1]] = true, true
		ds, dt := dir[key[0]], dir[key[1]]
		var path []fpt
		if ds.x+dt.x == 0 && ds.y+dt.y == 0 {
			path = []fpt{edge(key[0], hc), edge(key[1], hc)}
		} else {
			c := fpt{hc.x + q*(ds.x+dt.x), hc.y + q*(ds.y+dt.y)}
			path = []fpt{edge(key[0], c), c, edge(key[1], c)}
		}
		st := strokeOf(p)
		arrowPath(img, path, st, math.Max(head, st*2.6))
	}
	for i, path := range lpPaths {
		if lpPower[i] < flowIdle {
			dashedPath(img, path, float64(rule), math.Max(e.req.MMPx(1), 3))
			continue
		}
		st := strokeOf(lpPower[i])
		arrowPath(img, path, st, math.Max(head, st*2.6))
		used[flowHome] = true
	}
	// Nodes without any flow get a dashed link to the hub.
	for _, i := range []int{flowPV, flowGrid, flowBattery, flowHome} {
		if nodes[i] == nil || used[i] {
			continue
		}
		dashedLine(img, edge(i, hc), boxExit(hub.rect, nodes[i].centre(), 0), float64(rule), math.Max(e.req.MMPx(1), 3))
	}

	// Boxes, drawn over the lines.
	gray := render.Levels(img.Palette) == 4
	frame := max(rule, int(math.Round(e.req.PtPx(0.8))))
	dash := max(int(e.req.MMPx(1.2)), 3)
	render.FillColor(img, hub.rect, color.White)
	drawFrame(img, hub.rect, frame)
	icon("hub").Draw(img, hub.rect.Inset(max(hub.rect.Dy()/7, 1)), hub.rect.Dy()*5/7, render.Center)
	for i, n := range nodes {
		if n == nil {
			continue
		}
		r := n.rect
		render.FillColor(img, r, color.White)
		ink := color.Color(color.Black)
		if n.idle {
			dashedFrame(img, r, rule, dash)
			if gray {
				ink = render.DarkGray
			}
		} else {
			t := frame
			if i == flowHome {
				t = frame * 2
			}
			drawFrame(img, r, t)
		}
		lsz, vsz := labelSz, valueSz
		if f := float64(r.Dy()) / boxH; f < 0.999 {
			lsz, vsz = max(lsz*f, render.MinSize), max(vsz*f, render.MinSize)
		}
		th := flowTextHeight(lsz, vsz)
		twoLines := float64(r.Dy()) >= th+vsz*0.3 && lsz >= max(e.req.PtPx(5), render.MinSize+1)
		// Pictogram on the left when the text still fits beside it.
		tx0, tx1 := float64(r.Min.X)+padX/2, float64(r.Max.X)-padX/2
		iw := float64(r.Dy()) * 0.62
		textW := float64(render.Measure(n.value, vsz*0.9, true))
		if twoLines {
			textW = math.Max(textW, float64(render.Measure(n.label, lsz*0.9, false)))
		}
		if iw >= 10 && tx1-tx0-iw-padX >= textW {
			ir := image.Rect(r.Min.X+int(padX), r.Min.Y, r.Min.X+int(padX+iw), r.Max.Y)
			icon(n.icon).Draw(img, ir, int(iw), render.Center)
			tx0 = float64(ir.Max.X) + padX/2
		}
		tcx := int((tx0 + tx1) / 2)
		inner := int(tx1 - tx0)
		if !twoLines {
			vb := int(math.Round(float64(r.Min.Y+r.Max.Y)/2 + vsz*0.36))
			render.Text(img, n.value, tcx, vb, render.Style{Size: vsz, Bold: true, Shrink: true}, inner, ink, render.Center)
			continue
		}
		top := float64(r.Min.Y+r.Max.Y)/2 - th/2
		lb := int(math.Round(top + lsz*0.72))
		vb := int(math.Round(top + th))
		render.Text(img, n.label, tcx, lb, render.Style{Size: lsz, Shrink: true}, inner, ink, render.Center)
		render.Text(img, n.value, tcx, vb, render.Style{Size: vsz, Bold: true, Shrink: true}, inner, ink, render.Center)
	}
}

// dashedPath draws a dashed polyline.
func dashedPath(img *image.Paletted, pts []fpt, width, dash float64) {
	for i := 0; i+1 < len(pts); i++ {
		dashedLine(img, pts[i], pts[i+1], width, dash)
	}
}

// flowTextHeight is the height from the label's cap line to the value's
// baseline.
func flowTextHeight(label, value float64) float64 { return label*0.72 + value*0.45 + value*0.72 }

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

// boxExit returns the point where the ray from the centre of r towards p
// leaves r, moved gap further out.
func boxExit(r image.Rectangle, p fpt, gap float64) fpt {
	c := fpt{float64(r.Min.X+r.Max.X) / 2, float64(r.Min.Y+r.Max.Y) / 2}
	d := sub(p, c)
	hw, hh := float64(r.Dx())/2+gap, float64(r.Dy())/2+gap
	t := math.Inf(1)
	if d.x != 0 {
		t = math.Min(t, hw/math.Abs(d.x))
	}
	if d.y != 0 {
		t = math.Min(t, hh/math.Abs(d.y))
	}
	if math.IsInf(t, 1) {
		return c
	}
	return fpt{c.x + d.x*t, c.y + d.y*t}
}

func drawFrame(img *image.Paletted, r image.Rectangle, t int) {
	render.HLine(img, r.Min.X, r.Min.Y, r.Max.X, t)
	render.HLine(img, r.Min.X, r.Max.Y-t, r.Max.X, t)
	render.VLine(img, r.Min.X, r.Min.Y, r.Max.Y, t)
	render.VLine(img, r.Max.X-t, r.Min.Y, r.Max.Y, t)
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
