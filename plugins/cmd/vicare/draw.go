package main

import (
	"fmt"
	"image"
	"math"
	"strconv"
	"time"

	"github.com/maehld/paperboy/sdk/design"
	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

// Render draws one widget on an image display.
func (v *vicare) Render(_ *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	b := img.Rect.Inset(w.Style.Padding)
	if b.Dx() < 40 || b.Dy() < 24 {
		return plugin.TooSmall(w)
	}
	s := design.NewScale(v.req.TextPx(w), b.Dy())
	rule := max(w.Style.Rule, 1)
	switch w.Widget {
	case "heating":
		return v.drawHeating(w, img, b, s)
	case "temperatures":
		return v.drawTemperatures(w, img, b, s)
	case "dhw":
		return v.drawDHW(w, img, b, s, rule)
	case "burner":
		return v.drawBurner(w, img, b, s)
	case "status":
		return v.drawStatus(w, img, b, s)
	case "overview":
		return v.drawOverview(w, img, b, s)
	case "curve":
		return v.drawCurve(w, img, b, s)
	}
	return fmt.Errorf("%w %q", plugin.ErrUnknownWidget, w.Widget)
}

// fact is one list line: the muted label on the left and the value on the
// right.
type fact struct{ label, value string }

// drawHeating shows the flow target with outside and room temperature, the
// program and what the heat generator does. A wide cell puts the value left
// and the facts right.
func (v *vicare) drawHeating(w plugin.WidgetRequest, img *image.Paletted, b image.Rectangle, s design.Scale) error {
	c := v.circuit(w.Arg().Int("circuit", 0))
	if c == nil {
		return v.notice(img, b, s, v.t("Keine Heizkreise", "No heating circuits"), w)
	}
	label, value := v.t("Vorlauf-Soll", "Flow target"), "–"
	if c.Target != nil {
		value = v.num(*c.Target, 1)
	} else if c.Supply != nil {
		label, value = v.t("Vorlauf", "Flow"), v.num(*c.Supply, 1)
	}
	hero := design.Hero{Label: label, Value: value, Unit: "°C"}

	facts := []fact{{v.t("Außen", "Outside"), v.tempText(v.cur.Outside)}}
	if w.Arg().Bool("room", true) {
		facts = append(facts, fact{v.t("Raum", "room"), v.tempText(c.Room)})
	}
	if c.Program != "" {
		facts = append(facts, fact{v.t("Programm", "program"), v.programName(c.Program)})
	}
	facts = keepKnown(facts)

	if wide(b) {
		left := image.Rect(b.Min.X, b.Min.Y, b.Min.X+b.Dx()*40/100, b.Max.Y)
		if !design.DrawHero(img, left, hero, s) {
			return plugin.TooSmall(w)
		}
		gap := max(int(s.Body*0.6), 4)
		v.factsSize(img, image.Rect(left.Max.X+gap, b.Min.Y, b.Max.X, b.Max.Y), facts, s, s.Detail)
		return nil
	}
	hero.Details = v.detailLines(facts)
	if !design.DrawHero(img, b, hero, s) {
		return plugin.TooSmall(w)
	}
	return nil
}

// drawTemperatures lists the temperatures that the device reports.
func (v *vicare) drawTemperatures(w plugin.WidgetRequest, img *image.Paletted, b image.Rectangle, s design.Scale) error {
	c := v.circuit(w.Arg().Int("circuit", 0))
	facts := []fact{{v.t("Außen", "Outside"), v.tempText(v.cur.Outside)}}
	if c != nil {
		facts = append(facts,
			fact{v.t("Vorlauf", "Flow"), v.tempText(c.Supply)},
			fact{v.t("Raum", "Room"), v.tempText(c.Room)})
	}
	if v.cur.DHW != nil {
		facts = append(facts, fact{v.t("Warmwasser", "DHW"), v.tempText(v.cur.DHW.Temp)})
	}
	facts = append(facts,
		fact{v.t("Rücklauf", "Return"), v.tempText(v.cur.Return)},
		fact{v.t("Kessel", "Boiler"), v.tempText(v.cur.Boiler)})
	facts = keepKnown(facts)
	if len(facts) == 0 {
		return v.notice(img, b, s, v.t("Keine Temperaturen", "No temperatures"), w)
	}
	if limit := w.Arg().Int("max", 6); limit >= 2 && len(facts) > limit {
		facts = facts[:limit]
	}
	v.factsIn(img, b, facts, s)
	return nil
}

// drawDHW shows the hot water storage with its target and charging state.
func (v *vicare) drawDHW(w plugin.WidgetRequest, img *image.Paletted, b image.Rectangle, s design.Scale, rule int) error {
	d := v.cur.DHW
	if d == nil || d.Temp == nil {
		return v.notice(img, b, s, v.t("Kein Warmwasser", "No DHW"), w)
	}
	caption := ""
	if d.Target != nil {
		caption = v.t("Soll ", "target ") + v.num(*d.Target, 0) + "°"
	}
	switch {
	case d.Mode == "off":
		caption = v.join(caption, v.t("aus", "off"))
	case d.Charging:
		caption = v.join(caption, v.t("lädt", "charging"))
	case d.Target != nil:
		caption = v.join(caption, v.t("bereit", "ready"))
	}
	hero := design.Hero{
		Label: v.t("Warmwasser", "DHW"), Value: v.num(*d.Temp, 1), Unit: "°C", Caption: caption,
	}
	if w.Arg().Bool("meter", true) {
		max := 60.0
		if d.Target != nil && *d.Target > 0 {
			max = *d.Target
		}
		hero.Meter = &design.Meter{Fraction: *d.Temp / max}
	}
	if !design.DrawHero(img, b, hero, s) {
		return plugin.TooSmall(w)
	}
	return nil
}

// drawBurner shows the burner of a boiler or the compressor of a heat pump.
func (v *vicare) drawBurner(w plugin.WidgetRequest, img *image.Paletted, b image.Rectangle, s design.Scale) error {
	label, active := "", false
	modulation, hours, starts := (*float64)(nil), (*float64)(nil), (*float64)(nil)
	other := ""
	switch {
	case v.cur.Burner != nil:
		label, active = v.t("Brenner", "Burner"), v.cur.Burner.Active
		modulation, hours, starts = v.cur.Burner.Modulation, v.cur.Burner.Hours, v.cur.Burner.Starts
		if name, state := v.compressorName(); name != "" {
			other = name + " " + state
		}
	case v.cur.Compressor != nil:
		label, active = v.t("Kompressor", "Compressor"), v.cur.Compressor.Active
		hours, starts = v.cur.Compressor.Hours, v.cur.Compressor.Starts
	default:
		return v.notice(img, b, s, v.t("Kein Brenner", "No burner"), w)
	}

	state := v.t("Aus", "Off")
	if active {
		state = v.t("An", "On")
	}
	y := b.Min.Y
	render.TextAt(img, label, b.Min.X, y+int(s.Detail*0.95), s.Detail, false, design.Muted(img), render.Left)
	y += int(s.Detail * 1.15)
	render.TextAt(img, state, b.Min.X, y+int(s.Title), s.Title, true, render.Black, render.Left)
	y += int(s.Title * 1.2)

	if w.Arg().Bool("stats", true) {
		lineH := int(s.Body * 1.25)
		add := func(label, value string) {
			if value == "" || y+lineH > b.Max.Y {
				return
			}
			v.row(img, image.Rect(b.Min.X, y, b.Max.X, y+lineH), label, value, s)
			y += lineH
		}
		if modulation != nil {
			add(v.t("Modulation", "Modulation"), v.num(*modulation, 0)+" %")
		}
		if hours != nil {
			add(v.t("Stunden", "Hours"), v.num(*hours, 0)+" h")
		}
		if starts != nil {
			add(v.t("Starts", "Starts"), v.num(*starts, 0))
		}
		if other != "" && y+lineH <= b.Max.Y {
			render.TextAt(img, render.Ellipsize(other, s.Detail, false, b.Dx()), b.Min.X, y+int(s.Detail*0.95),
				s.Detail, false, design.Muted(img), render.Left)
		}
	}
	return nil
}

// drawStatus shows the model, the age of the data, the programs and the first
// error.
func (v *vicare) drawStatus(w plugin.WidgetRequest, img *image.Paletted, b image.Rectangle, s design.Scale) error {
	var facts []fact
	if v.curModel != "" {
		facts = append(facts, fact{v.t("Anlage", "Device"), v.curModel})
	}
	if age := v.dataAge(); age != "" {
		facts = append(facts, fact{v.t("Stand", "Data"), age})
	}
	for i := range v.cur.Circuits {
		c := &v.cur.Circuits[i]
		name := c.Name
		if name == "" {
			name = v.t("Heizkreis ", "Circuit ") + strconv.Itoa(c.Index)
		}
		if program := v.programName(c.Program); program != "" {
			facts = append(facts, fact{name, program})
		}
	}
	if w.Arg().Bool("errors", true) {
		label, value := v.t("Störung", "Error"), v.t("keine", "none")
		if len(v.cur.Errors) > 0 {
			e := v.cur.Errors[0]
			value = e.Code
			if e.Description != "" {
				value = v.join(value, e.Description)
			}
		}
		facts = append(facts, fact{label, value})
	}
	if len(facts) == 0 {
		return v.notice(img, b, s, v.t("Keine Daten", "No data"), w)
	}
	v.factsIn(img, b, facts, s)
	return nil
}

// drawOverview packs outside, room, DHW and the heat generator into a small
// cell.
func (v *vicare) drawOverview(w plugin.WidgetRequest, img *image.Paletted, b image.Rectangle, s design.Scale) error {
	c := v.circuit(w.Arg().Int("circuit", 0))
	value := "–"
	if v.cur.Outside != nil {
		value = v.num(*v.cur.Outside, 1)
	}
	facts := []fact{}
	if c != nil {
		facts = append(facts, fact{v.t("Raum", "room"), v.tempText(c.Room)})
	}
	if v.cur.DHW != nil {
		facts = append(facts, fact{v.t("Wasser", "water"), v.tempText(v.cur.DHW.Temp)})
	}
	if name, state := v.burnerName(); name != "" {
		facts = append(facts, fact{name, state})
	}
	facts = keepKnown(facts)
	hero := design.Hero{Label: v.t("Außen", "Outside"), Value: value, Unit: "°C"}

	if wide(b) && len(facts) > 0 {
		left := image.Rect(b.Min.X, b.Min.Y, b.Min.X+b.Dx()*40/100, b.Max.Y)
		if !design.DrawHero(img, left, hero, s) {
			return plugin.TooSmall(w)
		}
		gap := max(int(s.Body*0.6), 4)
		v.factsSize(img, image.Rect(left.Max.X+gap, b.Min.Y, b.Max.X, b.Max.Y), facts, s, s.Detail)
		return nil
	}
	hero.Details = v.detailLines(facts)
	if !design.DrawHero(img, b, hero, s) {
		return plugin.TooSmall(w)
	}
	return nil
}

// ---- helpers ----

// wide reports whether a cell is better used in two columns.
func wide(b image.Rectangle) bool { return b.Dx() >= 500 && b.Dx() >= b.Dy()*2 }

// factsIn draws facts in one column, or in two on a wide cell.
func (v *vicare) factsIn(img *image.Paletted, b image.Rectangle, facts []fact, s design.Scale) {
	v.factsSize(img, b, facts, s, s.Body)
}

// factsSize draws facts at a given type size. A side column beside a hero uses
// the detail role, so a label and its value share the column.
func (v *vicare) factsSize(img *image.Paletted, b image.Rectangle, facts []fact, s design.Scale, size float64) {
	if len(facts) == 0 || b.Empty() {
		return
	}
	lineH := int(size * 1.3)
	if lineH < 1 {
		return
	}
	fit := max(b.Dy()/lineH, 1)
	gap := max(int(size*0.6), 4)
	cols := 1
	// Two columns need room beside each other: the longest label and its
	// value have to fit in one column.
	if b.Dx() >= 560 && len(facts) > 2 && fit >= 2 {
		need := 0
		for _, f := range facts {
			need = max(need, render.Measure(f.label, size, false)+gap+render.Measure(f.value, size, true))
		}
		if (b.Dx()-gap)/2 >= need {
			cols = 2
		}
	}
	per := (len(facts) + cols - 1) / cols
	if per > fit {
		per = fit
	}
	colW := (b.Dx() - gap*(cols-1)) / cols
	for i := 0; i < len(facts) && i < per*cols; i++ {
		col, row := i/per, i%per
		x := b.Min.X + col*(colW+gap)
		v.rowSize(img, image.Rect(x, b.Min.Y+row*lineH, x+colW, b.Min.Y+(row+1)*lineH), facts[i].label, facts[i].value, size)
	}
}

// detailLines joins the facts into the hero's detail lines, two per line.
func (v *vicare) detailLines(facts []fact) []string {
	var lines []string
	for i := 0; i < len(facts); i += 2 {
		part := func(f fact) string {
			if f.value == "" {
				return f.label
			}
			return f.label + " " + f.value
		}
		line := part(facts[i])
		if i+1 < len(facts) {
			line = v.join(line, part(facts[i+1]))
		}
		lines = append(lines, line)
	}
	return lines
}

// row draws one list line: the label muted on the left, the value bold on the
// right.
func (v *vicare) row(img *image.Paletted, r image.Rectangle, label, value string, s design.Scale) {
	v.rowSize(img, r, label, value, min(s.Body, float64(r.Dy())*0.75))
}

// rowSize draws one list line at a given type size.
func (v *vicare) rowSize(img *image.Paletted, r image.Rectangle, label, value string, size float64) {
	if r.Empty() || (label == "" && value == "") {
		return
	}
	if size < render.MinSize {
		return
	}
	gap := max(int(size*0.6), 2)
	valueW := render.Measure(value, size, true)
	label = render.Ellipsize(label, size, false, max(r.Dx()-valueW-gap, 0))
	base := r.Min.Y + int(size*0.95)
	render.TextAt(img, label, r.Min.X, base, size, false, design.Muted(img), render.Left)
	render.TextAt(img, value, r.Max.X, base, size, true, render.Black, render.Right)
}

// notice draws a short centered text and answers ErrTooSmall when even that
// does not fit.
func (v *vicare) notice(img *image.Paletted, b image.Rectangle, s design.Scale, text string, w plugin.WidgetRequest) error {
	if !design.Notice(img, b, text, s) {
		return plugin.TooSmall(w)
	}
	return nil
}

// tempText formats a temperature for a row, a dash when it is unknown.
func (v *vicare) tempText(f *float64) string {
	if f == nil {
		return "–"
	}
	return v.num(*f, 1) + "°"
}

// keepKnown drops the facts without a value.
func keepKnown(facts []fact) []fact {
	out := facts[:0]
	for _, f := range facts {
		if f.value != "" && f.value != "–" {
			out = append(out, f)
		}
	}
	return out
}

// join appends a part with the separator of the line.
func (v *vicare) join(parts ...string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += " · "
		}
		out += p
	}
	return out
}

// ---- heating curve ----

// curveTarget is the flow target of the heating curve. PyViCare found two
// shapes in the firmware: 1 for a boiler or a heat pump with several
// circuits, 2 for a heat pump with one circuit, which is 5 K higher.
func curveTarget(slope, shift, inside, outside float64, variant int) float64 {
	d := outside - inside
	target := inside + shift - slope*d*(1.4347+0.021*d+247.9e-6*d*d)
	if variant == 2 {
		target += 5
	}
	return target
}

// drawCurve draws the heating curve from slope and shift and marks the target
// at the current outside temperature.
func (v *vicare) drawCurve(w plugin.WidgetRequest, img *image.Paletted, b image.Rectangle, s design.Scale) error {
	c := v.circuit(w.Arg().Int("circuit", 0))
	if c == nil || c.Curve == nil {
		return v.notice(img, b, s, v.t("Keine Heizkurve", "No heating curve"), w)
	}
	args := w.Arg()
	low := float64(args.Int("outside_min", -20))
	high := float64(args.Int("outside_max", 20))
	if low > high {
		low, high = high, low
	}
	low = math.Max(low, -40)
	high = math.Min(high, 40)
	if high-low < 10 {
		low, high = -20, 20
	}
	variant := curveVariant(args.String("formula", "auto"), v.curRoles, len(v.cur.Circuits))

	ymin, ymax := 20.0, 70.0
	if c.TempMin != nil && c.TempMax != nil && *c.TempMax-*c.TempMin >= 10 {
		ymin, ymax = *c.TempMin, *c.TempMax
	}
	nowFlow := c.Target
	if nowFlow == nil && v.cur.Outside != nil {
		target := curveTarget(c.Curve.Slope, c.Curve.Shift, 20, *v.cur.Outside, variant)
		nowFlow = &target
	}

	label := v.t("Heizkurve", "Heating curve")
	slope := c.Curve.Slope
	shift := c.Curve.Shift
	curveText := v.num(slope, 1) + " · " + signed(shift, v.num(shift, 0)+" K")
	outsideText := v.t("außen ", "outside ") + "–"
	if v.cur.Outside != nil {
		outsideText = v.t("außen ", "outside ") + v.num(*v.cur.Outside, 1) + "°"
	}
	soll := ""
	if nowFlow != nil {
		soll = v.t("Soll ", "target ") + v.num(*nowFlow, 1) + "°"
	}

	// A wide cell names the slope and the shift before the outside
	// temperature; a short one keeps the title, the outside temperature and
	// the target, because a label at the mark would fall below the type floor.
	head := int(s.Detail * 0.95)
	y := b.Min.Y + head
	gap := int(s.Detail * 0.8)
	isWide := b.Dx() >= 500
	mid, right, rightBold := outsideText, soll, true
	if isWide {
		mid, right, rightBold = curveText, outsideText, false
	}
	lw := render.Measure(label, s.Detail, true)
	rw := render.Measure(right, s.Detail, rightBold)
	if right != "" && lw+rw+gap > b.Dx() {
		right = render.Ellipsize(right, s.Detail, rightBold, max(b.Dx()-lw-gap, 0))
		rw = render.Measure(right, s.Detail, rightBold)
	}
	cx := 0
	if mid != "" {
		mw := render.Measure(mid, s.Detail, false)
		cx = (b.Dx() - mw) / 2
		if cx < lw+gap || cx+mw > b.Dx()-rw-gap {
			mid = ""
		}
	}
	render.TextAt(img, label, b.Min.X, y, s.Detail, true, render.Black, render.Left)
	if mid != "" {
		render.TextAt(img, mid, b.Min.X+cx, y, s.Detail, false, design.Muted(img), render.Left)
	}
	if right != "" {
		render.TextAt(img, right, b.Max.X, y, s.Detail, rightBold, render.Black, render.Right)
	}

	top := b.Min.Y + int(s.Detail*1.35)
	plot := image.Rect(b.Min.X, top, b.Max.X, b.Max.Y)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(o float64) time.Time {
		return base.Add(time.Duration((o - low) * float64(time.Minute)))
	}
	var points []design.Point
	for o := low; o <= high; o += 0.5 {
		points = append(points, design.Point{At: at(o), Value: curveTarget(slope, shift, 20, o, variant)})
	}
	var ticks []time.Time
	const ticksCount = 4
	for i := 0; i <= ticksCount; i++ {
		o := low + (high-low)*float64(i)/ticksCount
		ticks = append(ticks, at(o))
	}
	trend := design.Trend{
		From: at(low), To: at(high),
		Range:      &design.Range{Min: ymin, Max: ymax},
		Axis:       true,
		Grid:       true,
		Series:     []design.Series{{Points: points, Style: design.Line, Width: 2}},
		Ticks:      ticks,
		TickFormat: func(t time.Time) string { return v.num(low+t.Sub(base).Minutes(), 0) + "°" },
		Format:     func(f float64) string { return v.num(f, 0) + "°" },
	}
	if v.cur.Outside != nil {
		trend.Now = at(*v.cur.Outside)
		if nowFlow != nil {
			mark := design.Mark{At: at(*v.cur.Outside), Value: *nowFlow}
			if isWide {
				mark.Label = v.num(*nowFlow, 0) + "°"
			}
			trend.Marks = []design.Mark{mark}
		}
	}
	if !design.DrawTrend(img, plot, trend, s) {
		return plugin.TooSmall(w)
	}
	return nil
}

// curveVariant picks the formula: variant 2 is the heat pump with one circuit,
// unless the widget names one.
func curveVariant(want string, roles []string, circuits int) int {
	switch want {
	case "heatpump":
		return 2
	case "boiler":
		return 1
	}
	if hasRole(roles, "type:heatpump") && circuits == 1 {
		return 2
	}
	return 1
}

// signed writes a number with an explicit plus, as a curve shift reads.
func signed(f float64, text string) string {
	if f > 0 {
		return "+" + text
	}
	return text
}

func hasRole(roles []string, want string) bool {
	for _, r := range roles {
		if r == want {
			return true
		}
	}
	return false
}
