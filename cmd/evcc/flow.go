package main

import (
	"embed"
	"image"
	"image/color"
	"image/png"
	"math"
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
	icon, label, short, value string
	idle, small               bool
	rect                      image.Rectangle
}

// flow draws the energy flow in the arrangement of SolarAI's dashboard: the
// house connection (hub) in the middle, PV above it, the battery below, the
// grid left and the home right. Loadpoints sit in the free corners right of
// the hub (below the home first, then above it). Like Home Assistant's energy
// distribution the topology is fixed: every node has exactly one link to the
// hub, drawn whether or not power flows. An idle link is a thin dashed line;
// an active one is a solid line of the same width everywhere, with an
// arrowhead in the direction of the node's net power (into the hub for PV,
// grid import and battery discharge, out of it for the home, the loadpoints,
// export and charging). Solid vs dashed, the direction and the labels carry
// the information, so it reads in two gray levels.
func (e *evcc) flow(w plugin.WidgetRequest, img *image.Paletted) error {
	s := e.cur
	b := img.Rect.Inset(w.Style.Padding)
	if b.Dx() < 40 || b.Dy() < 40 {
		return plugin.TooSmall(w)
	}
	base := e.req.TextPx(w)
	rule := max(w.Style.Rule, 1)

	grid := s.gridPower()
	gridLabel, gridShort := e.t("Netz", "Grid"), e.t("Netz", "Grid")
	switch {
	case grid >= flowIdle:
		gridLabel, gridShort = e.t("Netzbezug", "Grid import"), e.t("Bezug", "Import")
	case grid <= -flowIdle:
		gridLabel, gridShort = e.t("Einspeisung", "Grid export"), e.t("Einsp.", "Export")
	}
	lps := s.activeLoadpoints()
	if len(lps) > 3 {
		lps = lps[:3]
	}
	nodes := make([]*flowNode, flowLP+len(lps))
	// net is each node's power towards the hub: positive flows into it.
	net := make([]float64, len(nodes))
	nodes[flowPV] = &flowNode{icon: "sun", label: "PV", short: "PV", value: e.power(s.PVPower)}
	net[flowPV] = s.PVPower
	nodes[flowGrid] = &flowNode{icon: "grid", label: gridLabel, short: gridShort, value: e.power(grid)}
	net[flowGrid] = grid
	nodes[flowHome] = &flowNode{icon: "home", label: e.t("Haus", "Home"), short: e.t("Haus", "Home"), value: e.power(s.HomePower)}
	net[flowHome] = -s.HomePower
	if s.hasBattery() {
		nodes[flowBattery] = &flowNode{icon: "battery", label: e.t("Akku ", "Battery ") + e.num(s.Battery.SoC, 0) + " %",
			short: e.t("Akku", "Battery"), value: e.power(s.Battery.Power)}
		net[flowBattery] = s.Battery.Power
	}
	for i, lp := range lps {
		n := &flowNode{icon: "car", label: lpName(lp), short: lpName(lp), value: e.power(lp.ChargePower), small: true}
		if lp.VehicleSoC > 0 {
			n.label += " " + e.num(lp.VehicleSoC, 0) + " %"
		}
		nodes[flowLP+i] = n
		net[flowLP+i] = -lp.ChargePower
	}
	for i, n := range nodes {
		if n != nil {
			n.idle = math.Abs(net[i]) < flowIdle
		}
	}
	bat := nodes[flowBattery]
	hub := &flowNode{icon: "hub"}
	// Loadpoints below the home first, then above it.
	var lpBelow, lpAbove []*flowNode
	for i, n := range nodes[flowLP:] {
		if i == 1 {
			lpAbove = append(lpAbove, n)
		} else {
			lpBelow = append(lpBelow, n)
		}
	}

	// Sizes. d is the distance from the hub's centre to the PV and battery
	// boxes, long enough for a visible arrow.
	labelSz := max(base*0.8, e.req.PtPx(5.8), render.MinSize)
	valueSz := max(base*1.2, e.req.PtPx(6.5), render.MinSize)
	boxH0 := flowTextHeight(labelSz, valueSz) + valueSz*0.9
	minArrow := max(e.req.MMPx(5), e.req.PtPx(7)*1.8, 12)
	vgap := max(e.req.MMPx(1.5), 4)
	gap := max(e.req.MMPx(0.8), 3)
	stroke := math.Max(e.req.PtPx(1.2), 1)
	head := math.Max(e.req.PtPx(4.5), 7)
	h, wd := float64(b.Dy()), float64(b.Dx())
	dc := 2*gap + head*2.2
	full := bat != nil || len(lps) > 0 // a bottom row is needed
	height := func(bh float64) float64 {
		d := 0.33*bh + dc
		if full {
			return 2*bh + 2*d
		}
		return bh + d + bh/2
	}
	k := 1.0
	if height(boxH0) > h {
		k = (h - height(0)) / (height(boxH0) - height(0))
	}
	k = math.Max(k, 0.2)
	boxH := boxH0 * k
	labelSz, valueSz = max(labelSz*k, render.MinSize), max(valueSz*k, render.MinSize)
	hubS := boxH * 0.66
	d := hubS/2 + dc
	spare := h - height(boxH)
	if full {
		spare /= 2
	}
	grow := math.Max(0, math.Min(spare, 1.5*minArrow))
	d += grow
	diagH := height(boxH) + grow
	if full {
		diagH += grow
	}
	padX := valueSz * 0.5
	needW := boxH * 2.2
	for _, n := range nodes {
		if n != nil && !n.small {
			needW = math.Max(needW, boxH*0.62+padX+float64(max(render.Measure(n.label, labelSz, false), render.Measure(n.value, valueSz, true)))+2*padX)
		}
	}
	// The gap right of the hub column carries one vertical lane per
	// loadpoint above or below the home.
	laneW := stroke + gap
	minGapR := minArrow
	if len(lps) > 0 {
		minGapR = math.Max(minArrow, float64(len(lpBelow))*laneW+2*gap)
	}
	boxW := math.Min(needW*1.1, (wd-minArrow-minGapR)/3)
	hgap := math.Min((wd-3*boxW-minGapR+minArrow)/2, 3*minArrow)
	hgapR := math.Max(hgap, minGapR)
	ox := float64(b.Min.X) + (wd-3*boxW-hgap-hgapR)/2
	oy := float64(b.Min.Y) + (h-diagH)/2
	rect := func(x, y, w, h float64) image.Rectangle {
		return image.Rect(int(math.Round(x)), int(math.Round(y)), int(math.Round(x+w)), int(math.Round(y+h)))
	}
	cy := oy + boxH + d
	if !full {
		cy = oy + diagH - boxH/2
	}
	col1 := ox + boxW + hgap
	col2 := col1 + boxW + hgapR
	cx := col1 + boxW/2
	nodes[flowPV].rect = rect(col1, cy-d-boxH, boxW, boxH)
	if bat != nil {
		bat.rect = rect(col1, cy+d, boxW, boxH)
	}
	nodes[flowGrid].rect = rect(ox, cy-boxH/2, boxW, boxH)
	nodes[flowHome].rect = rect(col2, cy-boxH/2, boxW, boxH)
	hub.rect = rect(cx-hubS/2, cy-hubS/2, hubS, hubS)

	// Links: path from the node to the hub.
	links := make([][]fpt, len(nodes))
	hubR := hub.rect
	links[flowPV] = []fpt{{cx, float64(nodes[flowPV].rect.Max.Y) + gap}, {cx, float64(hubR.Min.Y) - gap}}
	links[flowGrid] = []fpt{{float64(nodes[flowGrid].rect.Max.X) + gap, cy}, {float64(hubR.Min.X) - gap, cy}}
	links[flowHome] = []fpt{{float64(nodes[flowHome].rect.Min.X) - gap, cy}, {float64(hubR.Max.X) + gap, cy}}
	if bat != nil {
		links[flowBattery] = []fpt{{cx, float64(bat.rect.Min.Y) - gap}, {cx, float64(hubR.Max.Y) + gap}}
	}
	// Loadpoint slots: the corner regions right of the hub column. Each link
	// leaves the hub's right side above or below the home link, turns in the
	// gap and enters the loadpoint from the left.
	indent := head*1.6 + gap
	place := func(ns []*flowNode, top, bottom float64, sign float64) {
		if len(ns) == 0 {
			return
		}
		n := float64(len(ns))
		lh := math.Min(0.72*boxH, (bottom-top-(n-1)*vgap)/n)
		y := top + (bottom-top-(n*lh+(n-1)*vgap))/2
		for j, nd := range ns {
			nd.rect = rect(col2+indent, y, boxW-indent, lh)
			ly := y + lh/2
			off := hubS * (0.3 + 0.18*float64(j)) * sign
			laneX := col1 + boxW + hgapR - gap - (float64(j)+0.5)*laneW
			if sign < 0 {
				laneX = col1 + boxW + hgapR - gap - 0.5*laneW
			}
			idx := 0
			for i, m := range nodes {
				if m == nd {
					idx = i
				}
			}
			links[idx] = []fpt{{float64(nd.rect.Min.X) - gap, ly}, {laneX, ly}, {laneX, cy + off}, {float64(hubR.Max.X) + gap, cy + off}}
			y += lh + vgap
		}
	}
	if len(lpBelow) > 0 {
		top := cy + boxH/2 + vgap
		bottom := oy + diagH
		if len(lpBelow) == 1 {
			top = math.Max(top, cy+d+boxH/2-0.36*boxH)
		}
		place(lpBelow, top, bottom, 1)
	}
	if len(lpAbove) > 0 {
		place(lpAbove, math.Min(oy, cy-d-boxH), cy-boxH/2-vgap, -1)
	}

	for i, path := range links {
		if path == nil {
			continue
		}
		p := net[i]
		if math.Abs(p) < flowIdle {
			dashedPath(img, path, float64(rule), math.Max(e.req.MMPx(1), 3))
			continue
		}
		if p < 0 { // out of the hub: reverse the path
			r := make([]fpt, len(path))
			for j := range path {
				r[j] = path[len(path)-1-j]
			}
			path = r
		}
		arrowPath(img, path, stroke, head)
	}

	// Boxes, drawn over the lines.
	gray := render.Levels(img.Palette) > 2
	frame := max(rule, int(math.Round(e.req.PtPx(0.8))))
	dash := max(int(e.req.MMPx(1.2)), 3)
	render.FillColor(img, hub.rect, color.White)
	drawFrame(img, hub.rect, frame)
	icon("hub").Draw(img, hub.rect.Inset(max(hub.rect.Dy()/7, 1)), hub.rect.Dy()*5/7, render.Center)
	minLabel := max(e.req.PtPx(5.5), render.MinSize)
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
			lsz, vsz = max(lsz*f, minLabel), max(vsz*f, minLabel*1.2)
		}
		th := flowTextHeight(lsz, vsz)
		inner0 := float64(r.Dx()) - padX
		// Label: the full one, then without the state of charge, then none.
		label := ""
		for _, l := range []string{n.label, n.short} {
			if l != "" && float64(render.Measure(l, lsz, false)) <= inner0 {
				label = l
				break
			}
		}
		if float64(r.Dy()) < th+vsz*0.2 {
			label = ""
		}
		// Pictogram on the left only when the text still fits beside it.
		tx0, tx1 := float64(r.Min.X)+padX/2, float64(r.Max.X)-padX/2
		iw := float64(r.Dy()) * 0.62
		textW := math.Max(float64(render.Measure(n.value, vsz, true)), float64(render.Measure(label, lsz, false)))
		if iw >= 12 && tx1-tx0-iw-padX >= textW {
			ir := image.Rect(r.Min.X+int(padX), r.Min.Y, r.Min.X+int(padX+iw), r.Max.Y)
			icon(n.icon).Draw(img, ir, int(iw), render.Center)
			tx0 = float64(ir.Max.X) + padX/2
		}
		tcx := int((tx0 + tx1) / 2)
		inner := int(tx1 - tx0)
		if label == "" {
			vb := int(math.Round(float64(r.Min.Y+r.Max.Y)/2 + vsz*0.36))
			render.Text(img, n.value, tcx, vb, render.Style{Size: vsz, Bold: true, Shrink: true}, inner, ink, render.Center)
			continue
		}
		top := float64(r.Min.Y+r.Max.Y)/2 - th/2
		lb := int(math.Round(top + lsz*0.72))
		vb := int(math.Round(top + th))
		render.Text(img, label, tcx, lb, render.Style{Size: lsz, Shrink: true}, inner, ink, render.Center)
		render.Text(img, n.value, tcx, vb, render.Style{Size: vsz, Bold: true, Shrink: true}, inner, ink, render.Center)
	}
	return nil
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
	signed := e.signed
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
