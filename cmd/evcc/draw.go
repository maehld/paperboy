package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

func (e *evcc) Render(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	switch w.Widget {
	case "site":
		e.site(w, img)
	case "loadpoint":
		lp, err := e.loadpoint(w)
		if err != nil {
			return err
		}
		e.drawLoadpoint(w, lp, img)
	case "forecast":
		e.forecast(w, img)
	default:
		return fmt.Errorf("%w %q", plugin.ErrUnknownWidget, w.Widget)
	}
	return nil
}

func (e *evcc) t(de, en string) string { return e.req.T(de, en) }

func (e *evcc) num(v float64, decimals int) string {
	s := strconv.FormatFloat(v, 'f', decimals, 64)
	if e.lang == "de" {
		s = strings.Replace(s, ".", ",", 1)
	}
	return s
}

// power formats watts as W below 1 kW and kW with one decimal above.
func (e *evcc) power(w float64) string {
	a := math.Abs(w)
	if a < 1000 {
		return e.num(math.Round(a), 0) + " W"
	}
	return e.num(a/1000, 1) + " kW"
}

func (e *evcc) energy(wh float64) string {
	if wh < 1000 {
		return e.num(math.Round(wh), 0) + " Wh"
	}
	return e.num(wh/1000, 1) + " kWh"
}

func (e *evcc) price(p float64) string {
	unit := " ct"
	if e.cur.Currency != "" && e.cur.Currency != "EUR" {
		unit = " " + e.cur.Currency + "/100"
	}
	return e.num(p*100, 1) + unit
}

func duration(d time.Duration, lang string) string {
	m := int(d.Round(time.Minute) / time.Minute)
	if m < 60 {
		return fmt.Sprintf("%d min", m)
	}
	if m%60 == 0 {
		return fmt.Sprintf("%d h", m/60)
	}
	return fmt.Sprintf("%d h %d min", m/60, m%60)
}

// tile is one label, value and optional detail.
type tile struct{ label, value, detail string }

func (e *evcc) siteTiles() []tile {
	s := e.cur
	grid := s.gridPower()
	gridWord := e.t("Bezug", "import")
	if grid < 0 {
		gridWord = e.t("Einspeisung", "export")
	}
	if math.Abs(grid) < 10 {
		gridWord = ""
	}
	tiles := []tile{
		{e.t("PV", "PV"), e.power(s.PVPower), ""},
		{e.t("Haus", "Home"), e.power(s.HomePower), ""},
		{e.t("Netz", "Grid"), e.power(grid), gridWord},
	}
	if s.Battery != nil && s.Battery.Capacity > 0 {
		word := ""
		switch {
		case s.Battery.Power < -10:
			word = e.t("lädt ", "charging ") + e.power(s.Battery.Power)
		case s.Battery.Power > 10:
			word = e.t("entlädt ", "discharging ") + e.power(s.Battery.Power)
		}
		tiles = append(tiles, tile{e.t("Akku", "Battery"), e.num(s.Battery.SoC, 0) + " %", word})
	} else if s.TariffGrid != nil {
		tiles = append(tiles, tile{e.t("Strompreis", "Grid price"), e.price(*s.TariffGrid), "/ kWh"})
	}
	return tiles
}

func (e *evcc) site(w plugin.WidgetRequest, img *image.Paletted) {
	tiles := e.siteTiles()
	cols := 2
	if w.Arg().Int("columns", 2) == 4 {
		cols = 4
	}
	drawTiles(img, tiles, cols, w.Style.Padding, w.Style.Rule, e.req.TextPx(w))
}

// drawTiles lays tiles out in a grid with thin separators, like the tiles of
// the other plugins: label above, value bold, detail below.
func drawTiles(img *image.Paletted, tiles []tile, cols, pad, rule int, base float64) {
	rows := (len(tiles) + cols - 1) / cols
	b := img.Rect
	cw, ch := b.Dx()/cols, b.Dy()/rows
	for i, t := range tiles {
		x0, y0 := b.Min.X+(i%cols)*cw, b.Min.Y+(i/cols)*ch
		x := x0 + pad
		width := cw - 2*pad
		label := min(float64(ch)*0.2, base*0.8)
		value := min(float64(ch)*0.34, base*1.6)
		detail := min(float64(ch)*0.17, base*0.7)
		render.Text(img, t.label, x, y0+pad+int(label), render.Style{Size: label, Shrink: true}, width, color.Black, render.Left)
		render.Text(img, t.value, x, y0+ch*66/100, render.Style{Size: value, Bold: true, Shrink: true}, width, color.Black, render.Left)
		if t.detail != "" {
			render.Text(img, t.detail, x, y0+ch-pad, render.Style{Size: detail, Shrink: true}, width, color.Black, render.Left)
		}
	}
	for c := 1; c < cols; c++ {
		render.VLine(img, b.Min.X+c*cw, b.Min.Y+pad, b.Max.Y-pad, rule)
	}
	for r := 1; r < rows; r++ {
		render.HLine(img, b.Min.X+pad, b.Min.Y+r*ch, b.Max.X-pad, rule)
	}
}

var modes = map[string][2]string{
	"off":   {"Aus", "Off"},
	"now":   {"Schnell", "Fast"},
	"minpv": {"Min+PV", "Min+Solar"},
	"pv":    {"PV", "Solar"},
}

func (e *evcc) mode(m string) string {
	if t, ok := modes[m]; ok {
		return e.t(t[0], t[1])
	}
	return m
}

func (e *evcc) status(lp *loadpoint) string {
	switch {
	case lp.Charging:
		return e.t("lädt", "charging")
	case lp.Connected:
		return e.t("verbunden", "connected")
	}
	return e.t("nicht verbunden", "not connected")
}

// drawLoadpoint shows the vehicle with a state-of-charge bar and a limit
// marker, the mode and status, and charge power and session energy.
func (e *evcc) drawLoadpoint(w plugin.WidgetRequest, lp *loadpoint, img *image.Paletted) {
	b := img.Rect.Inset(w.Style.Padding)
	if b.Empty() {
		return
	}
	base := e.req.TextPx(w)
	h := float64(b.Dy())
	title := min(h*0.16, base*1.2)
	text := min(h*0.12, base*0.85)
	name := lp.VehicleTitle
	if name == "" {
		name = lp.Title
	}
	y := b.Min.Y + int(title)
	render.Text(img, name, b.Min.X, y, render.Style{Size: title, Bold: true, Shrink: true}, b.Dx()*2/3, color.Black, render.Left)
	render.Text(img, e.mode(lp.Mode), b.Max.X, y, render.Style{Size: text, Shrink: true}, b.Dx()/3, color.Black, render.Right)

	// State of charge bar with the limit as a tick.
	barTop := y + int(title*0.6)
	barH := max(int(h*0.22), 8)
	bar := image.Rect(b.Min.X, barTop, b.Max.X, barTop+barH)
	render.HLine(img, bar.Min.X, bar.Min.Y, bar.Max.X, 2)
	render.HLine(img, bar.Min.X, bar.Max.Y-2, bar.Max.X, 2)
	render.VLine(img, bar.Min.X, bar.Min.Y, bar.Max.Y, 2)
	render.VLine(img, bar.Max.X-2, bar.Min.Y, bar.Max.Y, 2)
	inner := bar.Inset(4)
	soc := math.Max(0, math.Min(100, lp.VehicleSoC))
	fill := inner
	fill.Max.X = inner.Min.X + int(float64(inner.Dx())*soc/100)
	style := "shade"
	if lp.Charging {
		style = "solid"
	}
	render.Fill(img, fill, style)
	if lim := lp.EffectiveLimitSoC; lim > 0 && lim < 100 {
		x := inner.Min.X + int(float64(inner.Dx())*lim/100)
		render.VLine(img, x-1, bar.Min.Y-4, bar.Max.Y+4, 3)
	}
	socText := e.num(soc, 0) + " %"
	if lp.VehicleRange > 0 {
		socText += " · " + e.num(lp.VehicleRange, 0) + " km"
	}
	y = bar.Max.Y + int(text*1.4)
	render.Text(img, socText, b.Min.X, y, render.Style{Size: text, Bold: true, Shrink: true}, b.Dx()/2, color.Black, render.Left)
	if lim := lp.EffectiveLimitSoC; lim > 0 {
		render.Text(img, e.t("Ziel ", "limit ")+e.num(lim, 0)+" %", b.Max.X, y, render.Style{Size: text, Shrink: true}, b.Dx()/2, color.Black, render.Right)
	}

	status := e.status(lp)
	if lp.Charging {
		status += " " + e.power(lp.ChargePower)
		if lp.PhasesActive > 0 {
			status += fmt.Sprintf(" (%dp)", lp.PhasesActive)
		}
		if lp.ChargeRemainingDuration > 0 {
			status += " · " + e.t("noch ", "") + duration(time.Duration(lp.ChargeRemainingDuration), e.lang)
		}
	}
	y += int(text * 1.5)
	render.Text(img, status, b.Min.X, y, render.Style{Size: text, Shrink: true}, b.Dx(), color.Black, render.Left)
	if lp.SessionEnergy > 0 {
		session := e.t("Sitzung ", "session ") + e.energy(lp.SessionEnergy)
		if lp.SessionSolarPercentage > 0 {
			session += " · " + e.num(lp.SessionSolarPercentage, 0) + " % " + e.t("Sonne", "solar")
		}
		y += int(text * 1.5)
		if y <= b.Max.Y {
			render.Text(img, session, b.Min.X, y, render.Style{Size: text, Shrink: true}, b.Dx(), color.Black, render.Left)
		}
	}
}

// forecast draws the solar forecast as an area and the grid price as steps,
// from now to the configured number of hours ahead.
func (e *evcc) forecast(w plugin.WidgetRequest, img *image.Paletted) {
	b := img.Rect.Inset(w.Style.Padding)
	hours := min(max(w.Arg().Int("hours", 24), 6), 48)
	loc := e.req.Location()
	now := e.req.Now.In(loc)
	start, end := now, now.Add(time.Duration(hours)*time.Hour)
	label := max(min(e.req.TextPx(w)*0.6, float64(b.Dy())*0.12), 10)

	var solar [][2]float64
	maxW := 0.0
	if f := e.cur.Forecast; f != nil && f.Solar != nil {
		for _, p := range f.Solar.Timeseries {
			t := time.Unix(int64(p[0]), 0)
			if t.Before(start.Add(-time.Hour)) || t.After(end) {
				continue
			}
			solar = append(solar, p)
			maxW = max(maxW, p[1])
		}
	}
	var prices [][3]float64
	lo, hi := math.Inf(1), math.Inf(-1)
	if f := e.cur.Forecast; f != nil {
		for _, p := range f.Grid {
			if time.Unix(int64(p[1]), 0).Before(start) || time.Unix(int64(p[0]), 0).After(end) {
				continue
			}
			prices = append(prices, p)
			lo, hi = min(lo, p[2]), max(hi, p[2])
		}
	}
	title := e.t("Prognose", "Forecast")
	if f := e.cur.Forecast; f != nil && f.Solar != nil {
		title += fmt.Sprintf(" · %s %s · %s %s", e.t("heute", "today"), e.energy(f.Solar.Today.Energy),
			e.t("morgen", "tomorrow"), e.energy(f.Solar.Tomorrow.Energy))
	}
	render.Text(img, title, b.Min.X, b.Min.Y+int(label), render.Style{Size: label, Bold: true, Shrink: true}, b.Dx(), color.Black, render.Left)

	priceW := 0
	if len(prices) > 0 {
		priceW = render.Measure(e.price(hi), label, false) + 6
	}
	solarW := render.Measure("10 kW", label, false) + 6
	left, right := b.Min.X+solarW, b.Max.X-priceW
	top, bottom := b.Min.Y+int(label*1.8), b.Max.Y-int(label*1.5)
	if right-left < 40 || bottom-top < 20 {
		return
	}
	span := end.Sub(start).Seconds()
	x := func(ts float64) int {
		return left + int((ts-float64(start.Unix()))/span*float64(right-left))
	}
	clampX := func(v int) int { return min(max(v, left), right) }

	if maxW > 0 {
		scale := math.Ceil(maxW/1000) * 1000
		yS := func(v float64) int { return bottom - int(v/scale*float64(bottom-top)) }
		for i := 0; i+1 < len(solar); i++ {
			x0, x1 := clampX(x(solar[i][0])), clampX(x(solar[i+1][0]))
			if x1 <= x0 {
				continue
			}
			render.Fill(img, image.Rect(x0, yS(solar[i][1]), x1, bottom), "shade")
		}
		render.Text(img, e.num(scale/1000, 0)+" kW", left-4, top+int(label*0.35), render.Style{Size: label}, solarW, color.Black, render.Right)
	}
	if len(prices) > 0 {
		pad := max((hi-lo)*0.1, hi*0.1, 0.01)
		pl, ph := lo-pad, hi+pad
		yP := func(v float64) int { return bottom - int((v-pl)/(ph-pl)*float64(bottom-top)) }
		prevX, prevY := -1, 0
		for _, p := range prices {
			x0, x1, y := clampX(x(p[0])), clampX(x(p[1])), yP(p[2])
			if prevX >= 0 {
				render.Segment(img, x0, prevY, x0, y, 2, false)
			}
			render.Segment(img, x0, y, x1, y, 2, false)
			prevX, prevY = x1, y
		}
		render.Text(img, e.price(hi), right+4, yP(hi)+int(label*0.35), render.Style{Size: label}, priceW, color.Black, render.Left)
		if hi > lo {
			render.Text(img, e.price(lo), right+4, yP(lo)+int(label*0.35), render.Style{Size: label}, priceW, color.Black, render.Left)
		}
	}
	render.HLine(img, left, bottom, right, 1)
	step := 6 * time.Hour
	if hours <= 12 {
		step = 3 * time.Hour
	}
	for t := start.Truncate(time.Hour).Add(time.Hour); t.Before(end); t = t.Add(time.Hour) {
		if t.In(loc).Hour()%int(step.Hours()) != 0 {
			continue
		}
		xt := x(float64(t.Unix()))
		render.VLine(img, xt, bottom, bottom+3, 1)
		render.Text(img, t.In(loc).Format("15:04"), xt, b.Max.Y, render.Style{Size: label}, 60, color.Black, render.Center)
	}
}

// Text fills the widgets of a character display.
func (e *evcc) Text(req *plugin.Request, w plugin.WidgetRequest) ([]string, error) {
	switch w.Widget {
	case "site":
		var out []string
		for _, t := range e.siteTiles() {
			value := t.value
			if t.detail != "" && w.Width >= 24 {
				value += " " + t.detail
			}
			out = append(out, plugin.Columns(t.label, value, w.Width))
		}
		return out, nil
	case "loadpoint":
		lp, err := e.loadpoint(w)
		if err != nil {
			return nil, err
		}
		name := lp.VehicleTitle
		if name == "" {
			name = lp.Title
		}
		lines := []string{plugin.Columns(name, e.num(lp.VehicleSoC, 0)+"%", w.Width)}
		status := e.status(lp)
		if lp.Charging {
			status = e.power(lp.ChargePower)
		}
		lines = append(lines, plugin.Columns(e.mode(lp.Mode), status, w.Width))
		if lp.SessionEnergy > 0 {
			lines = append(lines, plugin.Columns(e.t("Sitzung", "Session"), e.energy(lp.SessionEnergy), w.Width))
		}
		return lines, nil
	}
	return nil, fmt.Errorf("%w %q for text displays", plugin.ErrUnknownWidget, w.Widget)
}
