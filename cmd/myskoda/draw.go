package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strconv"
	"strings"

	xdraw "golang.org/x/image/draw"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

// Render draws one widget on an image display.
func (m *myskoda) Render(_ *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	switch w.Widget {
	case "battery":
		m.drawBattery(w, img)
	case "charging":
		m.drawCharging(w, img)
	case "profiles":
		m.drawProfiles(w, img)
	case "location":
		m.drawLocation(w, img)
	case "climate":
		m.drawClimate(w, img)
	case "card":
		m.drawCard(w, img)
	case "car":
		m.drawCar(w, img)
	default:
		return fmt.Errorf("%w %q", plugin.ErrUnknownWidget, w.Widget)
	}
	return nil
}

// fact is one line of a widget: the text and how it is drawn.
type fact struct {
	text  string
	size  float64
	bold  bool
	right string // right-aligned addition on the same line
}

// drawFacts writes facts top-down into r, shrinking each line to the width and
// stopping when the height ends.
func drawFacts(img *image.Paletted, r image.Rectangle, facts []fact, gap int) int {
	y := r.Min.Y
	for _, f := range facts {
		if f.text == "" && f.right == "" {
			continue
		}
		size := max(f.size, render.MinSize)
		line := int(size*1.15) + gap
		if y+line > r.Max.Y {
			break
		}
		rightW := 0
		if f.right != "" {
			rightW = render.Measure(f.right, size, f.bold) + gap*2
			render.TextAt(img, f.right, r.Max.X, y+int(size), size, f.bold, color.Black, render.Right)
		}
		render.Text(img, f.text, r.Min.X, y+int(size), render.Style{Size: size, Bold: f.bold, Shrink: true},
			r.Dx()-rightW, color.Black, render.Left)
		y += line
	}
	return y
}

// drawBattery draws the state of charge with a bar and the facts around it.
func (m *myskoda) drawBattery(w plugin.WidgetRequest, img *image.Paletted) {
	b := img.Rect.Inset(w.Style.Padding)
	if b.Dx() < 40 || b.Dy() < 30 {
		return
	}
	base := m.req.TextPx(w)
	rule := max(w.Style.Rule, 1)
	gap := max(int(m.req.MMPx(0.8)+0.5), 2)
	soc, hasSoC := m.soc()

	facts := m.batteryFacts(w, base)
	factsH := factsHeight(facts, gap)
	labelH := int(base * 0.9)
	barH := max(int(base*0.4), rule*2+4)
	wide := b.Dx() >= b.Dy()*13/10

	value := "–"
	if hasSoC {
		value = strconv.Itoa(soc) + " %"
	}
	if wide {
		// Meter on the left, facts on the right, both vertically centred.
		meterW := b.Dx() * 45 / 100
		big := max(min(float64(b.Dy())*0.55, float64(meterW)*0.48), base*0.9)
		groupH := labelH + int(big) + gap + barH
		y := b.Min.Y + max((b.Dy()-groupH)/2, 0)
		render.Text(img, m.t("Akku", "Battery"), b.Min.X, y+int(base*0.9), render.Style{Size: base * 0.8}, meterW, color.Black, render.Left)
		render.Text(img, value, b.Min.X, y+labelH+int(big), render.Style{Size: big, Bold: true, Shrink: true},
			meterW, color.Black, render.Left)
		if hasSoC {
			bar := image.Rect(b.Min.X, y+labelH+int(big)+gap, b.Min.X+meterW, y+labelH+int(big)+gap+barH)
			drawMeter(img, bar, float64(soc)/100, rule)
		}
		right := image.Rect(b.Min.X+meterW+gap*2, b.Min.Y+(b.Dy()-factsH)/2, b.Max.X, b.Max.Y)
		drawFacts(img, right, facts, gap)
		return
	}

	// Tall cell: meter above the facts.
	big := max(min(float64(b.Dy())*0.35, float64(b.Dx())*0.55), base*0.9)
	groupH := labelH + int(big) + gap + barH + gap + factsH
	y := b.Min.Y + max((b.Dy()-groupH)/2, 0)
	render.Text(img, m.t("Akku", "Battery"), b.Min.X, y+int(base*0.9), render.Style{Size: base * 0.8}, b.Dx(), color.Black, render.Left)
	render.Text(img, value, b.Min.X, y+labelH+int(big), render.Style{Size: big, Bold: true, Shrink: true},
		b.Dx(), color.Black, render.Left)
	top := y + labelH + int(big) + gap
	if hasSoC {
		drawMeter(img, image.Rect(b.Min.X, top, b.Max.X, top+barH), float64(soc)/100, rule)
	}
	top += barH + gap
	drawFacts(img, image.Rect(b.Min.X, top, b.Max.X, b.Max.Y), facts, gap)
}

// factsHeight is the height of the facts stack drawFacts needs.
func factsHeight(facts []fact, gap int) int {
	h := 0
	for _, f := range facts {
		if f.text == "" && f.right == "" {
			continue
		}
		h += int(max(f.size, render.MinSize)*1.15) + gap
	}
	return max(h-gap, 0)
}

// batteryFacts are the lines next to the meter.
func (m *myskoda) batteryFacts(w plugin.WidgetRequest, base float64) []fact {
	facts := []fact{{text: m.chargeState(), size: base, bold: true}}
	if p, ok := m.power(); ok {
		facts[0].right = m.num(p, 1) + " kW"
	}
	if rangeKM, ok := m.rangeKM(); ok && w.Arg().Bool("range", true) {
		line := fmt.Sprintf("%d km", rangeKM)
		if r := m.remaining(); r != "" {
			line += " · " + r
		}
		facts = append(facts, fact{text: line, size: base * 0.85})
	} else if r := m.remaining(); r != "" {
		facts = append(facts, fact{text: r, size: base * 0.85})
	}
	if target, ok := m.targetSoC(); ok {
		facts = append(facts, fact{text: fmt.Sprintf("%s %d %%", m.t("Ziel", "Target"), target), size: base * 0.8})
	}
	if c := m.cur.Charging; c != nil {
		if age := m.captured(c.Captured); age != "" {
			facts = append(facts, fact{text: age, size: base * 0.7})
		}
	}
	return facts
}

// drawCharging draws the charging state with power, target, plug and profile.
func (m *myskoda) drawCharging(w plugin.WidgetRequest, img *image.Paletted) {
	b := img.Rect.Inset(w.Style.Padding)
	if b.Dx() < 40 || b.Dy() < 24 {
		return
	}
	base := m.req.TextPx(w)
	gap := max(int(m.req.MMPx(0.8)+0.5), 2)
	facts := []fact{{text: m.chargeState(), size: base * 1.1, bold: true}}
	if p, ok := m.power(); ok {
		facts[0].right = m.num(p, 1) + " kW"
	}
	if soc, ok := m.soc(); ok {
		line := fmt.Sprintf("%d %%", soc)
		if target, ok := m.targetSoC(); ok {
			line = fmt.Sprintf("%d %% → %d %%", soc, target)
		}
		facts = append(facts, fact{text: line, size: base * 0.9})
	}
	if r := m.remaining(); r != "" {
		facts = append(facts, fact{text: m.t("Noch ", "Left ") + r, size: base * 0.85})
	}
	if w.Arg().Bool("profile", true) {
		if p := m.currentProfile(); p != nil {
			line := p.Name
			if p.NextChargingTime != "" {
				line += " · " + p.NextChargingTime
			}
			facts = append(facts, fact{text: line, size: base * 0.8})
		}
	}
	if c := m.cur.Charging; c != nil {
		if age := m.captured(c.Captured); age != "" {
			facts = append(facts, fact{text: age, size: base * 0.65})
		}
	}
	drawFacts(img, b, facts, gap)
}

// drawProfiles lists the saved charging profiles with their target and times.
func (m *myskoda) drawProfiles(w plugin.WidgetRequest, img *image.Paletted) {
	b := img.Rect.Inset(w.Style.Padding)
	if b.Dx() < 40 || b.Dy() < 20 {
		return
	}
	list := m.profileList(w.Arg().Int("max", 2))
	if len(list) == 0 {
		render.Text(img, m.t("Keine Ladepläne", "No charging plans"), b.Min.X, b.Min.Y+int(m.req.TextPx(w)),
			render.Style{Size: m.req.TextPx(w), Shrink: true}, b.Dx(), color.Black, render.Left)
		return
	}
	base := m.req.TextPx(w)
	gap := max(int(m.req.MMPx(0.4)), 1)
	rule := max(w.Style.Rule, 1)
	y := b.Min.Y
	nameSize := base * 0.9
	timeSize := base * 0.7
	current := m.currentProfile()
	for _, p := range list {
		lineH := int(nameSize*1.15) + int(timeSize*1.15) + gap
		if y+lineH > b.Max.Y+gap {
			break
		}
		name := p.Name
		if current != nil && current.Name == p.Name {
			name = "* " + name
		}
		target := ""
		if p.Settings.TargetSoC != nil {
			target = fmt.Sprintf("%d %%", *p.Settings.TargetSoC)
		}
		rightW := 0
		if target != "" {
			rightW = render.Measure(target, nameSize, true) + gap*2
			render.TextAt(img, target, b.Max.X, y+int(nameSize), nameSize, true, color.Black, render.Right)
		}
		render.Text(img, name, b.Min.X, y+int(nameSize), render.Style{Size: nameSize, Bold: true, Shrink: true},
			b.Dx()-rightW, color.Black, render.Left)
		y += int(nameSize*1.15) + gap
		times := m.profileTimes(p)
		render.Text(img, times, b.Min.X, y+int(timeSize), render.Style{Size: timeSize, Shrink: true},
			b.Dx(), color.Black, render.Left)
		y += int(timeSize*1.15) + gap
		if rule > 0 {
			render.HLine(img, b.Min.X, y-gap/2-rule/2, b.Max.X, rule)
		}
	}
}

// profileList returns at most n profiles.
func (m *myskoda) profileList(n int) []chargingProfile {
	n = min(max(n, 1), 5)
	if m.cur.ChargingProfiles == nil {
		return nil
	}
	return m.cur.ChargingProfiles.Profiles[:min(n, len(m.cur.ChargingProfiles.Profiles))]
}

// profileTimes joins the enabled preferred charging times and timers.
func (m *myskoda) profileTimes(p chargingProfile) string {
	var parts []string
	for _, t := range p.PreferredChargingTimes {
		if t.Enabled {
			parts = append(parts, t.Start+"–"+t.End)
		}
	}
	for _, t := range p.Timers {
		if !t.Enabled {
			continue
		}
		line := t.Time
		switch {
		case t.Type == "RECURRING" && len(t.RecurringOn) > 0:
			line = dayShort(t.RecurringOn, m.lang) + " " + line
		case t.OneOffDay != "":
			line = dayShort([]string{t.OneOffDay}, m.lang) + " " + line
		}
		parts = append(parts, line)
	}
	if len(parts) == 0 {
		return m.t("keine Ladezeiten", "no charging times")
	}
	return strings.Join(parts, ", ")
}

var dayNames = map[string][7]string{
	"de": {"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"},
	"en": {"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
}

// dayShort names weekdays, joined with commas.
func dayShort(days []string, lang string) string {
	names, ok := dayNames[lang]
	if !ok {
		names = dayNames["en"]
	}
	short := [7]string{"SUNDAY", "MONDAY", "TUESDAY", "WEDNESDAY", "THURSDAY", "FRIDAY", "SATURDAY"}
	var out []string
	for _, d := range days {
		for i, full := range short {
			if d == full {
				out = append(out, names[i])
				break
			}
		}
	}
	return strings.Join(out, " ")
}

// drawLocation draws the parking position with its address.
func (m *myskoda) drawLocation(w plugin.WidgetRequest, img *image.Paletted) {
	b := img.Rect.Inset(w.Style.Padding)
	if b.Dx() < 40 || b.Dy() < 20 {
		return
	}
	base := m.req.TextPx(w)
	gap := max(int(m.req.MMPx(0.6)+0.5), 2)
	p := m.cur.ParkingPosition
	if p == nil {
		render.Text(img, m.t("Keine Position", "No position"), b.Min.X, b.Min.Y+int(base),
			render.Style{Size: base, Shrink: true}, b.Dx(), color.Black, render.Left)
		return
	}
	y := b.Min.Y
	if p.State == "IN_MOTION" {
		render.Text(img, m.t("Unterwegs", "In motion"), b.Min.X, y+int(base*1.1),
			render.Style{Size: base * 1.1, Bold: true}, b.Dx(), color.Black, render.Left)
		y += int(base * 1.4)
	} else {
		address := p.Address
		if address == "" {
			address = m.t("Geparkt", "Parked")
		}
		size := base
		step := int(size * 1.15)
		room := max((b.Dy()-y+b.Min.Y-gap*2)/max(step, 1), 1)
		if room > 3 {
			room = 3
		}
		lines := wrapText(address, size, b.Dx())
		if len(lines) > room {
			lines = lines[:room]
			lines[room-1] = render.Ellipsize(lines[room-1]+"…", size, false, b.Dx())
		}
		for i, line := range lines {
			render.Text(img, line, b.Min.X, y+int(size)+i*step, render.Style{Size: size, Shrink: true},
				b.Dx(), color.Black, render.Left)
		}
		y += len(lines)*step + gap
	}
	if gps := p.GPS; gps != nil && w.Arg().Bool("coords", true) {
		coords := m.num(gps.Latitude, 4) + ", " + m.num(gps.Longitude, 4)
		render.Text(img, coords, b.Min.X, y+int(base*0.75), render.Style{Size: base * 0.7, Shrink: true},
			b.Dx(), color.Black, render.Left)
		y += int(base * 0.9)
	}
	if age := m.captured(p.Captured); age != "" {
		render.Text(img, age, b.Min.X, y+int(base*0.7), render.Style{Size: base * 0.65, Shrink: true},
			b.Dx(), color.Black, render.Left)
	}
}

// drawClimate draws the air conditioning with its target and auxiliary states.
func (m *myskoda) drawClimate(w plugin.WidgetRequest, img *image.Paletted) {
	b := img.Rect.Inset(w.Style.Padding)
	if b.Dx() < 40 || b.Dy() < 20 {
		return
	}
	base := m.req.TextPx(w)
	gap := max(int(m.req.MMPx(0.8)+0.5), 2)
	if m.cur.AirConditioning == nil {
		render.Text(img, m.t("Keine Klimadaten", "No climate data"), b.Min.X, b.Min.Y+int(base),
			render.Style{Size: base, Shrink: true}, b.Dx(), color.Black, render.Left)
		return
	}
	c := m.cur.AirConditioning
	line := m.climateState()
	if temp := m.temperature(c.Target); temp != "" {
		line += " · " + temp
	}
	facts := []fact{{text: line, size: base * 1.1, bold: true}}
	if t, ok := m.time(c.ReachAt); ok {
		facts = append(facts, fact{text: m.t("erreicht ", "reached ") + t.Format("15:04"), size: base * 0.85})
	}
	if heat := m.windowHeating(c); heat != "" {
		facts = append(facts, fact{text: heat, size: base * 0.8})
	}
	if a := m.cur.AuxiliaryHeating; a != nil && a.State != nil && *a.State != "OFF" {
		facts = append(facts, fact{text: m.t("Standheizung: ", "Auxiliary: ") + m.auxState(*a.State), size: base * 0.8})
	}
	if v := m.cur.ActiveVentilation; v != nil && v.State != nil && *v.State != "OFF" {
		facts = append(facts, fact{text: m.t("Lüftung: ", "Ventilation: ") + m.climateToken(*v.State), size: base * 0.8})
	}
	if age := m.captured(c.Captured); age != "" {
		facts = append(facts, fact{text: age, size: base * 0.65})
	}
	drawFacts(img, b, facts, gap)
}

// windowHeating names the window heating that is on.
func (m *myskoda) windowHeating(c *climate) string {
	var on []string
	if c.WindowHeating.Front != nil && *c.WindowHeating.Front == "ON" {
		on = append(on, m.t("Frontscheibe", "Front window"))
	}
	if c.WindowHeating.Rear != nil && *c.WindowHeating.Rear == "ON" {
		on = append(on, m.t("Heckscheibe", "Rear window"))
	}
	if len(on) == 0 {
		return ""
	}
	if len(on) == 2 {
		return m.t("Scheibenheizung an", "Window heating on")
	}
	return on[0] + m.t(" an", " on")
}

// auxState names auxiliaryHeating.state.
func (m *myskoda) auxState(s string) string {
	switch s {
	case "PREHEATING":
		return m.t("Vorheizen", "Preheating")
	case "HEATING_AUXILIARY":
		return m.t("Heizt", "Heating")
	case "VENTILATION":
		return m.t("Lüftet", "Ventilating")
	case "OFF":
		return m.t("Aus", "Off")
	}
	return m.climateToken(s)
}

// climateToken names states shared by ventilation and climate.
func (m *myskoda) climateToken(s string) string {
	switch s {
	case "VENTILATION":
		return m.t("Lüftet", "Ventilating")
	case "PREHEATING":
		return m.t("Vorheizen", "Preheating")
	case "OFF":
		return m.t("Aus", "Off")
	}
	return s
}

// drawCard draws the vehicle render image with its name and state.
func (m *myskoda) drawCard(w plugin.WidgetRequest, img *image.Paletted) {
	b := img.Rect.Inset(w.Style.Padding)
	if b.Dx() < 40 || b.Dy() < 30 {
		return
	}
	base := m.req.TextPx(w)
	gap := max(int(m.req.MMPx(0.8)+0.5), 2)
	args := w.Arg()

	// Text below the image: name and one state line.
	bottom := b.Max.Y
	var lines []fact
	if args.Bool("name", true) && m.cur.Name != "" {
		lines = append(lines, fact{text: m.cur.Name, size: base * 1.1, bold: true})
	}
	if args.Bool("state", true) {
		var parts []string
		if soc, ok := m.soc(); ok {
			parts = append(parts, fmt.Sprintf("%d %%", soc))
		}
		if r, ok := m.rangeKM(); ok {
			parts = append(parts, fmt.Sprintf("%d km", r))
		}
		parts = append(parts, m.chargeState())
		lines = append(lines, fact{text: strings.Join(parts, " · "), size: base * 0.8})
	}
	if len(lines) > 0 {
		height := 0
		for _, f := range lines {
			height += int(f.size*1.15) + gap
		}
		if height < b.Dy()/2 {
			bottom = b.Max.Y - height
		}
	}
	area := image.Rect(b.Min.X, b.Min.Y, b.Max.X, bottom-gap)
	if m.img != nil && m.img.img != nil && area.Dx() > 8 && area.Dy() > 8 {
		drawPicture(img, area, m.img.img)
	}
	if len(lines) > 0 {
		drawFacts(img, image.Rect(b.Min.X, bottom, b.Max.X, b.Max.Y), lines, gap)
	}
}

// drawCar draws a compact overview for small cells.
func (m *myskoda) drawCar(w plugin.WidgetRequest, img *image.Paletted) {
	b := img.Rect.Inset(w.Style.Padding)
	if b.Dx() < 40 || b.Dy() < 20 {
		return
	}
	base := m.req.TextPx(w)
	gap := max(int(m.req.MMPx(0.6)+0.5), 2)
	args := w.Arg()
	name := m.cur.Name
	if name == "" {
		name = m.cur.VIN
	}
	facts := []fact{{text: name, size: base, bold: true}}
	if args.Bool("plate", false) && m.cur.LicensePlate != "" {
		facts[0].right = m.cur.LicensePlate
	}
	if soc, ok := m.soc(); ok {
		line := fmt.Sprintf("%d %%", soc)
		if r, ok := m.rangeKM(); ok {
			line += fmt.Sprintf(" · %d km", r)
		}
		facts = append(facts, fact{text: line, size: base * 0.9})
	} else if r, ok := m.rangeKM(); ok {
		facts = append(facts, fact{text: fmt.Sprintf("%d km", r), size: base * 0.9})
	}
	facts = append(facts, fact{text: m.chargeState(), size: base * 0.85})
	facts = append(facts, fact{text: m.lockState(), size: base * 0.85})
	if o := m.cur.Odometer; o != nil && o.KM != nil {
		facts = append(facts, fact{text: m.thousands(*o.KM) + " km", size: base * 0.8})
	}
	area := b
	if soc, ok := m.soc(); ok {
		barH := max(int(base*0.35), 3)
		if b.Dy() > barH*3 {
			bar := image.Rect(b.Min.X, b.Max.Y-barH, b.Max.X, b.Max.Y)
			drawMeter(img, bar, float64(soc)/100, max(w.Style.Rule, 1))
			area.Max.Y = bar.Min.Y - gap
		}
	}
	drawFacts(img, area, facts, gap)
}

// drawMeter draws an outlined bar filled to frac.
func drawMeter(img *image.Paletted, r image.Rectangle, frac float64, rule int) {
	if r.Dx() < 6 || r.Dy() < 3 {
		return
	}
	t := min(max(rule, 1), max(r.Dy()/3, 1))
	render.HLine(img, r.Min.X, r.Min.Y, r.Max.X, t)
	render.HLine(img, r.Min.X, r.Max.Y-t, r.Max.X, t)
	render.VLine(img, r.Min.X, r.Min.Y, r.Max.Y, t)
	render.VLine(img, r.Max.X-t, r.Min.Y, r.Max.Y, t)
	frac = min(max(frac, 0), 1)
	inner := r.Inset(t + 1)
	inner.Max.X = inner.Min.X + int(float64(inner.Dx())*frac+0.5)
	if !inner.Empty() {
		render.FillSolid(img, inner)
	}
}

// drawPicture scales src into r keeping its proportions and dithers it onto the
// palette's gray levels; a smooth picture would otherwise collapse into flat
// areas at 4 levels.
func drawPicture(img *image.Paletted, r image.Rectangle, src image.Image) {
	sb := src.Bounds()
	if sb.Empty() || r.Empty() {
		return
	}
	w, h := r.Dx(), r.Dy()
	if sb.Dx()*h > sb.Dy()*w {
		h = max(sb.Dy()*w/sb.Dx(), 1)
	} else {
		w = max(sb.Dx()*h/sb.Dy(), 1)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Rect, image.NewUniform(color.White), image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Rect, src, sb, xdraw.Over, nil)
	at := image.Rect(0, 0, w, h).Add(image.Pt(r.Min.X+(r.Dx()-w)/2, r.Min.Y+(r.Dy()-h)/2))
	dither(img, at, dst)
}

// dither writes src into img at r (same size) with Floyd–Steinberg error
// diffusion onto the palette's gray levels.
func dither(img *image.Paletted, r image.Rectangle, src *image.RGBA) {
	levels := render.Levels(img.Palette)
	w, h := r.Dx(), r.Dy()
	lum := make([]float64, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := src.RGBAAt(x, y)
			lum[y*w+x] = 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)
		}
	}
	step := 255.0 / float64(levels-1)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			old := lum[y*w+x]
			idx := int(min(max(old/step+0.5, 0), float64(levels-1)))
			img.SetColorIndex(r.Min.X+x, r.Min.Y+y, uint8(idx))
			e := old - float64(idx)*step
			spread := func(dx, dy int, f float64) {
				if x+dx >= 0 && x+dx < w && y+dy >= 0 && y+dy < h {
					lum[(y+dy)*w+x+dx] += e * f
				}
			}
			spread(1, 0, 7.0/16)
			spread(-1, 1, 3.0/16)
			spread(0, 1, 5.0/16)
			spread(1, 1, 1.0/16)
		}
	}
}

// wrapText breaks text into lines of at most maxWidth pixels.
func wrapText(text string, size float64, maxWidth int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		if line == "" {
			line = word
			continue
		}
		try := line + " " + word
		if render.Measure(try, size, false) > maxWidth {
			lines = append(lines, line)
			line = word
			continue
		}
		line = try
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// thousands writes a number with the separator of the language: a dot in
// German, a comma in English.
func (m *myskoda) thousands(v int64) string {
	s := strconv.FormatInt(v, 10)
	sep := ","
	if m.lang == "de" {
		sep = "."
	}
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var out []byte
	for i := 0; i < len(s); i++ {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, sep...)
		}
		out = append(out, s[i])
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}
