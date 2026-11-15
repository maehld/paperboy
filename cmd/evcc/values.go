package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

// stateValue is one value of /api/state the values widget and a text page can
// show. The keys are what a page selects; evcc's own numbers stay as they are.
type stateValue struct {
	key, de, en string
	unit        string
	precision   int
}

var stateValues = []stateValue{
	{"pv", "PV", "PV", "kW", 1},
	{"home", "Haus", "Home", "kW", 1},
	{"grid", "Netz", "Grid", "kW", 1},
	{"battery", "Akku", "Battery", "%", 0},
	{"battery_power", "Akku-Leistung", "Battery power", "kW", 1},
	{"grid_price", "Strompreis", "Grid price", "ct/kWh", 1},
	{"feedin_price", "Einspeisevergütung", "Feed-in price", "ct/kWh", 1},
	{"pv_energy", "PV gesamt", "PV total", "kWh", 1},
	{"grid_import", "Netzbezug gesamt", "Grid import total", "kWh", 1},
	{"grid_export", "Einspeisung gesamt", "Grid export total", "kWh", 1},
	{"battery_charge", "Akku geladen", "Battery charged", "kWh", 1},
	{"battery_discharge", "Akku entladen", "Battery discharged", "kWh", 1},
	{"charge_total", "Geladen gesamt", "Charged total", "kWh", 1},
	{"charge_total_solar", "Solaranteil gesamt", "Solar share total", "%", 0},
	{"charge_total_price", "Ø Ladepreis gesamt", "Avg. charging price total", "ct/kWh", 1},
	{"charge_year", "Geladen dieses Jahr", "Charged this year", "kWh", 1},
	{"charge_year_solar", "Solaranteil dieses Jahr", "Solar share this year", "%", 0},
	{"charge_year_price", "Ø Ladepreis dieses Jahr", "Avg. charging price this year", "ct/kWh", 1},
	{"solar_today", "Prognose heute", "Forecast today", "kWh", 1},
	{"solar_tomorrow", "Prognose morgen", "Forecast tomorrow", "kWh", 1},
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

// valueLabels names the keys for the editor in the configured language. The
// page keeps the keys, so the labels are only display text.
func valueLabels(lang string) map[string]string {
	labels := make(map[string]string, len(stateValues))
	for _, v := range stateValues {
		if lang == "de" {
			labels[v.key] = v.de
		} else {
			labels[v.key] = v.en
		}
	}
	return labels
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

// valueKeys reads the selected values: an absent arg means the default set,
// while an explicit empty list (the editor's "None") stays empty.
func (e *evcc) valueKeys(w plugin.WidgetRequest) []string {
	if keys := w.Arg().Strings("values"); len(keys) > 0 {
		return keys
	}
	if _, set := w.Arg()["values"]; set {
		return nil
	}
	return []string{"pv", "home", "grid", "battery"}
}

// ---- values for text pages (ADR 0111) ----

// valueSpecs is the declaration of every value the plugin answers: the state
// values followed by the values of the first loadpoint.
func valueSpecs() []plugin.ValueSpec {
	out := make([]plugin.ValueSpec, 0, len(stateValues)+10)
	for _, v := range stateValues {
		out = append(out, plugin.ValueSpec{Name: v.key, Title: v.en, Type: plugin.ValueNumber, Unit: v.unit, Precision: v.precision})
	}
	return append(out, []plugin.ValueSpec{
		{Name: "loadpoint", Title: "Charging point", Type: plugin.ValueText},
		{Name: "loadpoint_mode", Title: "Charge mode", Type: plugin.ValueText},
		{Name: "loadpoint_status", Title: "Status", Type: plugin.ValueText},
		{Name: "loadpoint_soc", Title: "State of charge", Type: plugin.ValueNumber, Unit: "%", Precision: 0},
		{Name: "loadpoint_limit", Title: "Charge limit", Type: plugin.ValueNumber, Unit: "%", Precision: 0},
		{Name: "loadpoint_range", Title: "Range", Type: plugin.ValueNumber, Unit: "km", Precision: 0},
		{Name: "loadpoint_power", Title: "Charge power", Type: plugin.ValueNumber, Unit: "kW", Precision: 1},
		{Name: "session_energy", Title: "Session energy", Type: plugin.ValueNumber, Unit: "kWh", Precision: 1},
		{Name: "session_solar", Title: "Solar share of the session", Type: plugin.ValueNumber, Unit: "%", Precision: 0},
		{Name: "remaining", Title: "Remaining charge time", Type: plugin.ValueDuration},
	}...)
}

// Values answers the declared values of a text page (ADR 0111). A value evcc
// does not report becomes a dash. The loadpoint values describe the first
// loadpoint.
func (e *evcc) Values(req *plugin.Request) (map[string]plugin.Value, error) {
	dash := plugin.TextValue("–")
	var lp *loadpoint
	if len(e.cur.Loadpoints) > 0 {
		lp = &e.cur.Loadpoints[0]
	}
	out := make(map[string]plugin.Value, len(req.Values))
	for _, name := range req.Values {
		if lp != nil {
			switch name {
			case "loadpoint":
				title := lp.VehicleTitle
				if title == "" {
					title = lp.Title
				}
				out[name] = plugin.TextValue(title)
				continue
			case "loadpoint_mode":
				out[name] = plugin.TextValue(e.mode(lp.Mode))
				continue
			case "loadpoint_status":
				out[name] = plugin.TextValue(e.status(lp))
				continue
			case "loadpoint_soc":
				out[name] = plugin.NumberValue(math.Max(0, math.Min(100, lp.VehicleSoC)))
				continue
			case "loadpoint_limit":
				if lp.EffectiveLimitSoC <= 0 {
					out[name] = dash
					continue
				}
				out[name] = plugin.NumberValue(lp.EffectiveLimitSoC)
				continue
			case "loadpoint_range":
				if lp.VehicleRange <= 0 {
					out[name] = dash
					continue
				}
				out[name] = plugin.NumberValue(lp.VehicleRange)
				continue
			case "loadpoint_power":
				out[name] = plugin.NumberValue(lp.ChargePower / 1000)
				continue
			case "session_energy":
				if lp.SessionEnergy <= 0 {
					out[name] = dash
					continue
				}
				out[name] = plugin.NumberValue(lp.SessionEnergy / 1000)
				continue
			case "session_solar":
				if lp.SessionEnergy <= 0 || lp.SessionSolarPercentage <= 0 {
					out[name] = dash
					continue
				}
				out[name] = plugin.NumberValue(lp.SessionSolarPercentage)
				continue
			case "remaining":
				if lp.ChargeRemainingDuration <= 0 {
					out[name] = dash
					continue
				}
				out[name] = plugin.DurationValue(time.Duration(lp.ChargeRemainingDuration))
				continue
			}
		}
		if _, ok := stateValueByKey[name]; !ok {
			if isLoadpointValue(name) {
				out[name] = dash // no loadpoint configured
				continue
			}
			return nil, fmt.Errorf("%w %q", plugin.ErrUnknownValue, name)
		}
		if v, ok := e.stateNumber(name); ok {
			out[name] = plugin.NumberValue(v)
			continue
		}
		out[name] = dash
	}
	return out, nil
}

// isLoadpointValue reports whether name belongs to the loadpoint block.
func isLoadpointValue(name string) bool {
	switch name {
	case "loadpoint", "loadpoint_mode", "loadpoint_status", "loadpoint_soc", "loadpoint_limit",
		"loadpoint_range", "loadpoint_power", "session_energy", "session_solar", "remaining":
		return true
	}
	return false
}

// stateNumber is one value of /api/state as a number in the declared unit:
// power in kW, prices in ct/kWh and the rest as evcc reports it.
// stateNumber is one value of /api/state as a number in the declared unit:
// power in kW, prices in ct/kWh and the rest as evcc reports it.
func (e *evcc) stateNumber(key string) (float64, bool) {
	s := e.cur
	switch key {
	case "pv":
		return s.PVPower / 1000, true
	case "home":
		return s.HomePower / 1000, true
	case "grid":
		if s.Grid == nil && s.GridPowerLegacy == nil {
			return 0, false
		}
		return s.gridPower() / 1000, true
	case "battery":
		if s.Battery == nil {
			return 0, false
		}
		return s.Battery.SoC, true
	case "battery_power":
		if s.Battery == nil {
			return 0, false
		}
		return s.Battery.Power / 1000, true
	case "grid_price":
		if s.TariffGrid == nil {
			return 0, false
		}
		return *s.TariffGrid * 100, true
	case "feedin_price":
		if s.TariffFeedIn == nil {
			return 0, false
		}
		return *s.TariffFeedIn * 100, true
	case "pv_energy":
		return s.PVEnergy, true
	case "grid_import":
		if s.Grid == nil {
			return 0, false
		}
		return s.Grid.Energy, true
	case "grid_export":
		if s.Grid == nil {
			return 0, false
		}
		return s.Grid.ReturnEnergy, true
	case "battery_charge":
		if s.Battery == nil {
			return 0, false
		}
		return s.Battery.Energy, true
	case "battery_discharge":
		if s.Battery == nil {
			return 0, false
		}
		return s.Battery.ReturnEnergy, true
	case "charge_total", "charge_total_solar", "charge_total_price":
		st, ok := s.Statistics["total"]
		return chargeNumber(key, st, ok)
	case "charge_year", "charge_year_solar", "charge_year_price":
		st, ok := s.Statistics["thisYear"]
		return chargeNumber(key, st, ok)
	case "solar_today":
		if s.Forecast == nil || s.Forecast.Solar == nil {
			return 0, false
		}
		return s.Forecast.Solar.Today.Energy, true
	case "solar_tomorrow":
		if s.Forecast == nil || s.Forecast.Solar == nil {
			return 0, false
		}
		return s.Forecast.Solar.Tomorrow.Energy, true
	}
	return 0, false
}

// chargeNumber picks one number of the charging statistics of a window.
func chargeNumber(key string, st statistics, ok bool) (float64, bool) {
	if !ok {
		return 0, false
	}
	switch key {
	case "charge_total_solar", "charge_year_solar":
		return st.SolarPercentage, true
	case "charge_total_price", "charge_year_price":
		return st.AvgPrice * 100, true
	}
	return st.ChargedKWh, true
}

// between the rows. "columns" puts several of them next to each other for a
// wide, flat cell; entries that do not fit are left out.
// valuesWidget lists the selected values: label left, value right, a thin rule
// between the rows. "columns" puts several of them next to each other for a
// wide, flat cell; entries that do not fit are left out.
func (e *evcc) valuesWidget(w plugin.WidgetRequest, img *image.Paletted) error {
	keys := e.valueKeys(w)
	if len(keys) == 0 {
		return nil
	}
	cols := w.Arg().Int("columns", 1)
	if cols < 1 || cols > 4 {
		cols = 1
	}
	b := img.Rect.Inset(w.Style.Padding)
	if b.Empty() {
		return plugin.TooSmall(w)
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
