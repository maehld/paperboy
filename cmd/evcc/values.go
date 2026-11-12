package main

import (
	"fmt"
	"image"
	"image/color"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

// stateValue is one value of /api/state the values widget can show. The keys
// are what a page selects; evcc's own numbers stay as they are.
type stateValue struct {
	key, de, en string
}

var stateValues = []stateValue{
	{"pv", "PV", "PV"},
	{"home", "Haus", "Home"},
	{"grid", "Netz", "Grid"},
	{"battery", "Akku", "Battery"},
	{"battery_power", "Akku-Leistung", "Battery power"},
	{"grid_price", "Strompreis", "Grid price"},
	{"feedin_price", "Einspeisevergütung", "Feed-in price"},
	{"pv_energy", "PV gesamt", "PV total"},
	{"grid_import", "Netzbezug gesamt", "Grid import total"},
	{"grid_export", "Einspeisung gesamt", "Grid export total"},
	{"battery_charge", "Akku geladen", "Battery charged"},
	{"battery_discharge", "Akku entladen", "Battery discharged"},
	{"charge_total", "Geladen gesamt", "Charged total"},
	{"charge_total_solar", "Solaranteil gesamt", "Solar share total"},
	{"charge_total_price", "Ø Ladepreis gesamt", "Avg. charging price total"},
	{"charge_year", "Geladen dieses Jahr", "Charged this year"},
	{"charge_year_solar", "Solaranteil dieses Jahr", "Solar share this year"},
	{"charge_year_price", "Ø Ladepreis dieses Jahr", "Avg. charging price this year"},
	{"solar_today", "Prognose heute", "Forecast today"},
	{"solar_tomorrow", "Prognose morgen", "Forecast tomorrow"},
}

var stateValueByKey = func() map[string]stateValue {
	m := make(map[string]stateValue, len(stateValues))
	for _, v := range stateValues {
		m[v.key] = v
	}
	return m
}()

// valueEnum lists the keys for the arg schema, so the editor offers a check
// box per value.
func valueEnum() []any {
	list := make([]any, len(stateValues))
	for i, v := range stateValues {
		list[i] = v.key
	}
	return list
}

func (e *evcc) valueLabel(v stateValue) string {
	if e.lang == "de" {
		return v.de
	}
	return v.en
}

// valueText formats one value of the last /api/state; a value evcc does not
// report becomes a dash.
func (e *evcc) valueText(key string) string {
	s := e.cur
	stats := func(window string) (statistics, bool) {
		st, ok := s.Statistics[window]
		return st, ok
	}
	switch key {
	case "pv":
		return e.power(s.PVPower)
	case "home":
		return e.power(s.HomePower)
	case "grid":
		if s.Grid == nil && s.GridPowerLegacy == nil {
			return "–"
		}
		return e.signed(s.gridPower())
	case "battery":
		if s.Battery == nil {
			return "–"
		}
		return e.num(s.Battery.SoC, 0) + " %"
	case "battery_power":
		if s.Battery == nil {
			return "–"
		}
		return e.signed(s.Battery.Power)
	case "grid_price":
		if s.TariffGrid == nil {
			return "–"
		}
		return e.price(*s.TariffGrid)
	case "feedin_price":
		if s.TariffFeedIn == nil {
			return "–"
		}
		return e.price(*s.TariffFeedIn)
	case "pv_energy":
		return e.kwh(s.PVEnergy)
	case "grid_import":
		if s.Grid == nil {
			return "–"
		}
		return e.kwh(s.Grid.Energy)
	case "grid_export":
		if s.Grid == nil {
			return "–"
		}
		return e.kwh(s.Grid.ReturnEnergy)
	case "battery_charge":
		if s.Battery == nil {
			return "–"
		}
		return e.kwh(s.Battery.Energy)
	case "battery_discharge":
		if s.Battery == nil {
			return "–"
		}
		return e.kwh(s.Battery.ReturnEnergy)
	case "charge_total", "charge_total_solar", "charge_total_price":
		st, ok := stats("total")
		if !ok {
			return "–"
		}
		return e.chargeValue(key, st)
	case "charge_year", "charge_year_solar", "charge_year_price":
		st, ok := stats("thisYear")
		if !ok {
			return "–"
		}
		return e.chargeValue(key, st)
	case "solar_today":
		if s.Forecast == nil || s.Forecast.Solar == nil {
			return "–"
		}
		return e.energy(s.Forecast.Solar.Today.Energy)
	case "solar_tomorrow":
		if s.Forecast == nil || s.Forecast.Solar == nil {
			return "–"
		}
		return e.energy(s.Forecast.Solar.Tomorrow.Energy)
	}
	return "–"
}

// chargeValue picks the field of the statistics of a window.
func (e *evcc) chargeValue(key string, st statistics) string {
	switch key {
	case "charge_total_solar", "charge_year_solar":
		return e.num(st.SolarPercentage, 0) + " %"
	case "charge_total_price", "charge_year_price":
		return e.price(st.AvgPrice)
	}
	return e.kwh(st.ChargedKWh)
}

// valuesWidget lists the selected values: label left, value right, a thin rule
// between the rows. "columns" puts several of them next to each other for a
// wide, flat cell; entries that do not fit are left out.
func (e *evcc) valuesWidget(w plugin.WidgetRequest, img *image.Paletted) error {
	keys := w.Arg().Strings("values")
	if len(keys) == 0 {
		keys = []string{"pv", "home", "grid", "battery"}
	}
	cols := w.Arg().Int("columns", 1)
	if cols < 1 || cols > 4 {
		cols = 1
	}
	b := img.Rect.Inset(w.Style.Padding)
	if b.Empty() {
		return nil
	}
	base := e.req.TextPx(w)
	n := len(keys)
	rows := (n + cols - 1) / cols
	// At least a readable line per row, otherwise the last values are left
	// out.
	for rows > 1 && float64(b.Dy())/float64(rows) < render.MinSize*1.8 {
		rows--
		n = min(n, rows*cols)
	}
	rowH := float64(b.Dy()) / float64(rows)
	cellW := float64(b.Dx()) / float64(cols)
	label := render.Style{Size: min(rowH*0.5, base*0.9), Shrink: true}
	value := render.Style{Size: min(rowH*0.58, base*1.0), Bold: true, Shrink: true}
	gap := max(w.Style.Padding, 4)
	rule := max(w.Style.Rule, 1)
	// Beside another column the text keeps its distance from the divider.
	inset := 0
	if cols > 1 {
		inset = max(w.Style.Padding/2, 4)
	}
	for i := 0; i < n; i++ {
		v, ok := stateValueByKey[keys[i]]
		if !ok {
			return fmt.Errorf("unknown value %q", keys[i])
		}
		row, col := i/cols, i%cols
		x := b.Min.X + int(float64(col)*cellW)
		rowY := b.Min.Y + int(float64(row)*rowH)
		baseline := rowY + int(rowH*0.72)
		text := e.valueText(v.key)
		valueW := render.Measure(text, value.Size, true)
		width := int(cellW) - 2*inset
		render.Text(img, e.valueLabel(v), x+inset, baseline, label, width-valueW-gap, color.Black, render.Left)
		render.Text(img, text, x+inset+width-1, baseline, value, valueW, color.Black, render.Right)
		if row > 0 && col == 0 {
			render.HLine(img, b.Min.X, rowY, b.Max.X, rule)
		}
		if col > 0 {
			render.VLine(img, x, rowY, rowY+int(rowH), rule)
		}
	}
	return nil
}

// valuesText puts one value per line.
func (e *evcc) valuesText(w plugin.WidgetRequest) ([]string, error) {
	keys := w.Arg().Strings("values")
	if len(keys) == 0 {
		keys = []string{"pv", "home", "grid", "battery"}
	}
	var lines []string
	for _, key := range keys {
		v, ok := stateValueByKey[key]
		if !ok {
			return nil, fmt.Errorf("unknown value %q", key)
		}
		if len(lines) == w.Height {
			break
		}
		lines = append(lines, plugin.Columns(e.valueLabel(v), e.valueText(v.key), w.Width))
	}
	return lines, nil
}
