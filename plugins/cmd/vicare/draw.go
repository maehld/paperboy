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

// drawHeating shows the flow target with outside and room temperature, the
// program and what the heat generator does.
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

	var details []string
	line := ""
	add := func(text string) {
		if text == "" {
			return
		}
		if line != "" {
			line += " · "
		}
		line += text
	}
	add(v.tempLabel(v.t("außen", "outside"), v.cur.Outside))
	if w.Arg().Bool("room", true) {
		add(v.tempLabel(v.t("Raum", "room"), c.Room))
	}
	if line != "" {
		details = append(details, line)
	}
	second := ""
	if c.Program != "" {
		second = v.programName(c.Program)
	}
	if state := v.burnerState(); state != "" {
		if second != "" {
			second += " · "
		}
		second += state
	}
	if second != "" {
		details = append(details, second)
	}

	if !design.DrawHero(img, b, design.Hero{
		Label: label, Value: value, Unit: "°C", Details: details,
	}, s) {
		return plugin.TooSmall(w)
	}
	return nil
}

// drawTemperatures lists the temperatures that the device reports.
func (v *vicare) drawTemperatures(w plugin.WidgetRequest, img *image.Paletted, b image.Rectangle, s design.Scale) error {
	c := v.circuit(w.Arg().Int("circuit", 0))
	type row struct{ label, value string }
	var rows []row
	add := func(label string, f *float64) {
		if f != nil {
			rows = append(rows, row{label, v.num(*f, 1) + "°"})
		}
	}
	add(v.t("Außen", "Outside"), v.cur.Outside)
	if c != nil {
		add(v.t("Vorlauf", "Flow"), c.Supply)
		add(v.t("Raum", "Room"), c.Room)
	}
	if v.cur.DHW != nil {
		add(v.t("Warmwasser", "DHW"), v.cur.DHW.Temp)
	}
	add(v.t("Rücklauf", "Return"), v.cur.Return)
	add(v.t("Kessel", "Boiler"), v.cur.Boiler)

	if len(rows) == 0 {
		return v.notice(img, b, s, v.t("Keine Temperaturen", "No temperatures"), w)
	}
	limit := w.Arg().Int("max", 6)
	if limit < 2 {
		limit = 2
	}
	lineH := int(s.Body*1.25) + max(int(s.Body*0.15), 1)
	if fit := max(b.Dy()/lineH, 1); fit < len(rows) {
		rows = rows[:fit]
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	for i, r := range rows {
		v.row(img, image.Rect(b.Min.X, b.Min.Y+i*lineH, b.Max.X, b.Min.Y+(i+1)*lineH), r.label, r.value, s)
	}
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
		caption = v.t("Soll ", "target ") + v.num(*d.Target, 1) + "°"
	}
	if d.Charging {
		caption = v.join(caption, v.t("lädt", "charging"))
	} else if d.Target != nil {
		caption = v.join(caption, v.t("bereit", "ready"))
	}
	if d.Mode != "" {
		caption = v.join(caption, v.dhwMode(d.Mode))
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
		other = v.compressorLine()
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

// compressorLine names a second heat generator, so a hybrid device shows both.
func (v *vicare) compressorLine() string {
	if v.cur.Compressor == nil {
		return ""
	}
	state := v.t("Kompressor aus", "Compressor off")
	if v.cur.Compressor.Active {
		state = v.t("Kompressor an", "Compressor on")
	}
	return state
}

// drawStatus shows the model, the age of the data, the programs and the first
// error.
func (v *vicare) drawStatus(w plugin.WidgetRequest, img *image.Paletted, b image.Rectangle, s design.Scale) error {
	type row struct{ label, value string }
	var rows []row
	if v.model != "" {
		rows = append(rows, row{v.t("Anlage", "Device"), v.model})
	}
	if age := v.dataAge(); age != "" {
		rows = append(rows, row{v.t("Stand", "Data"), age})
	}
	for i := range v.cur.Circuits {
		c := &v.cur.Circuits[i]
		name := c.Name
		if name == "" {
			name = v.t("Heizkreis ", "Circuit ") + strconv.Itoa(c.Index)
		}
		program := v.programName(c.Program)
		if program != "" {
			rows = append(rows, row{name, program})
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
		rows = append(rows, row{label, value})
	}
	if len(rows) == 0 {
		return v.notice(img, b, s, v.t("Keine Daten", "No data"), w)
	}
	lineH := int(s.Body * 1.25)
	if fit := max(b.Dy()/lineH, 1); fit < len(rows) {
		rows = rows[:fit]
	}
	for i, r := range rows {
		v.row(img, image.Rect(b.Min.X, b.Min.Y+i*lineH, b.Max.X, b.Min.Y+(i+1)*lineH), r.label, r.value, s)
	}
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
	var details []string
	if c != nil && c.Room != nil {
		details = append(details, v.tempLabel(v.t("Raum", "room"), c.Room))
	}
	if v.cur.DHW != nil && v.cur.DHW.Temp != nil {
		details = append(details, v.tempLabel(v.t("Wasser", "water"), v.cur.DHW.Temp))
	}
	if state := v.burnerState(); state != "" {
		details = append(details, state)
	}
	if !design.DrawHero(img, b, design.Hero{
		Label: v.t("Außen", "Outside"), Value: value, Unit: "°C", Details: details,
	}, s) {
		return plugin.TooSmall(w)
	}
	return nil
}

// ---- helpers ----

// row draws one list line: the label muted on the left, the value bold on the
// right.
func (v *vicare) row(img *image.Paletted, r image.Rectangle, label, value string, s design.Scale) {
	if r.Empty() || (label == "" && value == "") {
		return
	}
	size := min(s.Body, float64(r.Dy())*0.75)
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

// tempLabel formats a named temperature, empty when it is unknown.
func (v *vicare) tempLabel(label string, f *float64) string {
	if f == nil {
		return ""
	}
	return label + " " + v.num(*f, 1) + "°"
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
	variant := curveVariant(args.String("formula", "auto"), v.roles, len(v.cur.Circuits))

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
	curveText := v.num(slope, 1) + " · " + signed(shift, v.num(shift, 0))
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
	wide := b.Dx() >= 500
	mid, right, rightBold := outsideText, soll, true
	if wide {
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
			if wide {
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

// signed writes a number with an explicit plus, as a curve shift reads.
func signed(f float64, text string) string {
	if f > 0 {
		return "+" + text
	}
	return text
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

func hasRole(roles []string, want string) bool {
	for _, r := range roles {
		if r == want {
			return true
		}
	}
	return false
}
