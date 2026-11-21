package main

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

// stateValue is one value of /api/state the values widget and a text page can
// show. The keys are what a page selects; evcc's own numbers stay as they are.
type stateValue struct {
	key, de, en string
	unit        string // base unit the text page receives
	precision   int
	progression []plugin.UnitStep
}

// powerSteps and energySteps are the larger units Paperboy uses for powers and
// energies; the base units are W and Wh (ADR 0122).
var (
	powerSteps = []plugin.UnitStep{
		{Unit: "kW", Factor: 1000, Precision: 1},
		{Unit: "MW", Factor: 1000000, Precision: 2},
	}
	energySteps = []plugin.UnitStep{
		{Unit: "kWh", Factor: 1000, Precision: 1},
		{Unit: "MWh", Factor: 1000000, Precision: 2},
	}
)

var stateValues = []stateValue{
	{"pv", "PV", "PV", "W", 0, powerSteps},
	{"home", "Haus", "Home", "W", 0, powerSteps},
	{"grid", "Netz", "Grid", "W", 0, powerSteps},
	{"battery", "Akku", "Battery", "%", 0, nil},
	{"battery-power", "Akku-Leistung", "Battery power", "W", 0, powerSteps},
	{"grid-price", "Strompreis", "Grid price", "ct/kWh", 1, nil},
	{"feedin-price", "Einspeisevergütung", "Feed-in price", "ct/kWh", 1, nil},
	{"pv-energy", "PV gesamt", "PV total", "Wh", 0, energySteps},
	{"grid-import", "Netzbezug gesamt", "Grid import total", "Wh", 0, energySteps},
	{"grid-export", "Einspeisung gesamt", "Grid export total", "Wh", 0, energySteps},
	{"battery-charge", "Akku geladen", "Battery charged", "Wh", 0, energySteps},
	{"battery-discharge", "Akku entladen", "Battery discharged", "Wh", 0, energySteps},
	{"charge-total", "Geladen gesamt", "Charged total", "Wh", 0, energySteps},
	{"charge-total-solar", "Solaranteil gesamt", "Solar share total", "%", 0, nil},
	{"charge-total-price", "Ø Ladepreis gesamt", "Avg. charging price total", "ct/kWh", 1, nil},
	{"charge-year", "Geladen dieses Jahr", "Charged this year", "Wh", 0, energySteps},
	{"charge-year-solar", "Solaranteil dieses Jahr", "Solar share this year", "%", 0, nil},
	{"charge-year-price", "Ø Ladepreis dieses Jahr", "Avg. charging price this year", "ct/kWh", 1, nil},
	{"pv-today", "PV heute", "PV today", "Wh", 0, energySteps},
	{"home-today", "Haus heute", "Home today", "Wh", 0, energySteps},
	{"grid-import-today", "Netzbezug heute", "Grid import today", "Wh", 0, energySteps},
	{"grid-export-today", "Einspeisung heute", "Grid export today", "Wh", 0, energySteps},
	{"battery-charge-today", "Akku geladen heute", "Battery charged today", "Wh", 0, energySteps},
	{"battery-discharge-today", "Akku entladen heute", "Battery discharged today", "Wh", 0, energySteps},
	{"charge-today", "Geladen heute", "Charged today", "Wh", 0, energySteps},
	{"forecast-today", "Prognose heute", "Forecast today", "Wh", 0, energySteps},
	{"forecast-tomorrow", "Prognose morgen", "Forecast tomorrow", "Wh", 0, energySteps},
}

// dailyValue names the history series behind a daily value and the energy of
// that series it shows: return is what went out (grid export, battery
// discharge). The values read the cached history, so they are as current as
// the last history refresh.
type dailyValue struct {
	source, metric string
}

// dailyValues are the daily values of the energy history. charge-today is not
// one series: it sums the loadpoints of /api/state.
var dailyValues = map[string]dailyValue{
	"pv-today":                {"pv", "energy"},
	"home-today":              {"home", "energy"},
	"grid-import-today":       {"grid", "energy"},
	"grid-export-today":       {"grid", "return"},
	"battery-charge-today":    {"battery", "energy"},
	"battery-discharge-today": {"battery", "return"},
}

// isDailyValue reports whether name reads the day buckets of the history.
func isDailyValue(name string) bool {
	_, ok := dailyValues[name]
	return ok || name == "charge-today"
}

// dailyNumber resolves one daily value: today's bucket of its series, or the
// loadpoint of charge-today when one is asked. ok is false while the history
// has no such series or the loadpoint does not exist, which becomes a dash.
func (e *evcc) dailyNumber(name string, one bool, index int) (float64, bool) {
	if name == "charge-today" {
		return e.chargeToday(index, one)
	}
	d, ok := dailyValues[name]
	if !ok {
		return 0, false
	}
	return e.historyToday(d.source, d.metric)
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
	// The daily values come from the energy history, not from /api/state; the
	// widget sums the loadpoints of charge-today.
	if isDailyValue(key) {
		if v, ok := e.dailyNumber(key, false, 0); ok {
			return e.kwh(v)
		}
		return "–"
	}
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
	case "battery-power":
		if s.Battery == nil {
			return "–"
		}
		return e.signed(s.Battery.Power)
	case "grid-price":
		if s.TariffGrid == nil {
			return "–"
		}
		return e.price(*s.TariffGrid)
	case "feedin-price":
		if s.TariffFeedIn == nil {
			return "–"
		}
		return e.price(*s.TariffFeedIn)
	case "pv-energy":
		return e.kwh(s.PVEnergy)
	case "grid-import":
		if s.Grid == nil {
			return "–"
		}
		return e.kwh(s.Grid.Energy)
	case "grid-export":
		if s.Grid == nil {
			return "–"
		}
		return e.kwh(s.Grid.ReturnEnergy)
	case "battery-charge":
		if s.Battery == nil {
			return "–"
		}
		return e.kwh(s.Battery.Energy)
	case "battery-discharge":
		if s.Battery == nil {
			return "–"
		}
		return e.kwh(s.Battery.ReturnEnergy)
	case "charge-total", "charge-total-solar", "charge-total-price":
		st, ok := stats("total")
		if !ok {
			return "–"
		}
		return e.chargeValue(key, st)
	case "charge-year", "charge-year-solar", "charge-year-price":
		st, ok := stats("thisYear")
		if !ok {
			return "–"
		}
		return e.chargeValue(key, st)
	case "forecast-today":
		if s.Forecast == nil || s.Forecast.Solar == nil {
			return "–"
		}
		return e.energy(s.Forecast.Solar.Today.Energy)
	case "forecast-tomorrow":
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
	case "charge-total-solar", "charge-year-solar":
		return e.num(st.SolarPercentage, 0) + " %"
	case "charge-total-price", "charge-year-price":
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

// loadpointArg picks the charging point the loadpoint values describe
// (ADR 0120).
var loadpointArg = plugin.Arg{Type: "int", Default: 0, Description: "loadpoint, counted from 0"}

// valueSpecs is the declaration of every value the plugin answers: the state
// values followed by the values of the loadpoint named by the index argument.
func valueSpecs() []plugin.ValueSpec {
	out := make([]plugin.ValueSpec, 0, len(stateValues)+10)
	for _, v := range stateValues {
		spec := plugin.ValueSpec{Name: v.key, Title: v.en, Type: plugin.ValueNumber,
			Unit: v.unit, Precision: v.precision, Progression: v.progression}
		// charge-today follows the other loadpoint values and reads the index
		// of the loadpoint (ADR 0120); the other values take no argument.
		if v.key == "charge-today" {
			spec.Args = map[string]plugin.Arg{"index": loadpointArg}
		}
		out = append(out, spec)
	}
	lp := map[string]plugin.Arg{"index": loadpointArg}
	return append(out, []plugin.ValueSpec{
		{Name: "loadpoint", Title: "Charging point", Type: plugin.ValueText, Args: lp},
		{Name: "loadpoint-mode", Title: "Charge mode", Type: plugin.ValueText, Args: lp},
		{Name: "loadpoint-status", Title: "Status", Type: plugin.ValueText, Args: lp},
		{Name: "loadpoint-soc", Title: "State of charge", Type: plugin.ValueNumber, Unit: "%", Precision: 0, Args: lp},
		{Name: "loadpoint-limit", Title: "Charge limit", Type: plugin.ValueNumber, Unit: "%", Precision: 0, Args: lp},
		{Name: "loadpoint-range", Title: "Range", Type: plugin.ValueNumber, Unit: "km", Precision: 0, Args: lp},
		{Name: "loadpoint-power", Title: "Charge power", Type: plugin.ValueNumber, Unit: "W", Precision: 0, Progression: powerSteps, Args: lp},
		{Name: "session-energy", Title: "Session energy", Type: plugin.ValueNumber, Unit: "Wh", Precision: 0, Progression: energySteps, Args: lp},
		{Name: "session-solar", Title: "Solar share of the session", Type: plugin.ValueNumber, Unit: "%", Precision: 0, Args: lp},
		{Name: "remaining", Title: "Remaining charge time", Type: plugin.ValueDuration, Args: lp},
	}...)
}

// Values answers the declared values of a text page (ADR 0111). A value evcc
// does not report becomes a dash. The loadpoint values describe the loadpoint
// named by the index argument, the first one without it (ADR 0120).
func (e *evcc) Values(req *plugin.Request) (map[string]plugin.Value, error) {
	if err := req.ValueArgs.Check(map[string]plugin.Arg{"index": loadpointArg}); err != nil {
		return nil, err
	}
	dash := plugin.TextValue("–")
	var lp *loadpoint
	if i := req.ValueArgs.Int("index", 0); i >= 0 && i < len(e.cur.Loadpoints) {
		lp = &e.cur.Loadpoints[i]
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
			case "loadpoint-mode":
				out[name] = plugin.TextValue(e.mode(lp.Mode))
				continue
			case "loadpoint-status":
				out[name] = plugin.TextValue(e.status(lp))
				continue
			case "loadpoint-soc":
				out[name] = plugin.NumberValue(math.Max(0, math.Min(100, lp.VehicleSoC)))
				continue
			case "loadpoint-limit":
				if lp.EffectiveLimitSoC <= 0 {
					out[name] = dash
					continue
				}
				out[name] = plugin.NumberValue(lp.EffectiveLimitSoC)
				continue
			case "loadpoint-range":
				if lp.VehicleRange <= 0 {
					out[name] = dash
					continue
				}
				out[name] = plugin.NumberValue(lp.VehicleRange)
				continue
			case "loadpoint-power":
				out[name] = plugin.NumberValue(lp.ChargePower)
				continue
			case "session-energy":
				if lp.SessionEnergy <= 0 {
					out[name] = dash
					continue
				}
				out[name] = plugin.NumberValue(lp.SessionEnergy)
				continue
			case "session-solar":
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
				out[name] = plugin.DurationValue(seconds(lp.ChargeRemainingDuration))
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
		if isDailyValue(name) {
			// charge-today reads index like the other loadpoint values and
			// sums all loadpoints without it (ADR 0120).
			index, one := req.ValueArgs.Int("index", 0), false
			if name == "charge-today" {
				_, one = req.ValueArgs["index"]
			}
			if v, ok := e.dailyNumber(name, one, index); ok {
				// The history counts kWh; the values declare Wh (ADR 0122).
				out[name] = plugin.NumberValue(v * 1000)
				continue
			}
			out[name] = dash
			continue
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
	case "loadpoint", "loadpoint-mode", "loadpoint-status", "loadpoint-soc", "loadpoint-limit",
		"loadpoint-range", "loadpoint-power", "session-energy", "session-solar", "remaining":
		return true
	}
	return false
}

// stateNumber is one value of /api/state in the declared base unit: power in
// W, energy in Wh, prices in ct/kWh and the rest as evcc reports it (ADR 0122).
func (e *evcc) stateNumber(key string) (float64, bool) {
	s := e.cur
	switch key {
	case "pv":
		return s.PVPower, true
	case "home":
		return s.HomePower, true
	case "grid":
		if s.Grid == nil && s.GridPowerLegacy == nil {
			return 0, false
		}
		return s.gridPower(), true
	case "battery":
		if s.Battery == nil {
			return 0, false
		}
		return s.Battery.SoC, true
	case "battery-power":
		if s.Battery == nil {
			return 0, false
		}
		return s.Battery.Power, true
	case "grid-price":
		if s.TariffGrid == nil {
			return 0, false
		}
		return *s.TariffGrid * 100, true
	case "feedin-price":
		if s.TariffFeedIn == nil {
			return 0, false
		}
		return *s.TariffFeedIn * 100, true
	case "pv-energy":
		return s.PVEnergy * 1000, true
	case "grid-import":
		if s.Grid == nil {
			return 0, false
		}
		return s.Grid.Energy * 1000, true
	case "grid-export":
		if s.Grid == nil {
			return 0, false
		}
		return s.Grid.ReturnEnergy * 1000, true
	case "battery-charge":
		if s.Battery == nil {
			return 0, false
		}
		return s.Battery.Energy * 1000, true
	case "battery-discharge":
		if s.Battery == nil {
			return 0, false
		}
		return s.Battery.ReturnEnergy * 1000, true
	case "charge-total", "charge-total-solar", "charge-total-price":
		st, ok := s.Statistics["total"]
		return chargeNumber(key, st, ok)
	case "charge-year", "charge-year-solar", "charge-year-price":
		st, ok := s.Statistics["thisYear"]
		return chargeNumber(key, st, ok)
	case "forecast-today":
		if s.Forecast == nil || s.Forecast.Solar == nil {
			return 0, false
		}
		return s.Forecast.Solar.Today.Energy, true
	case "forecast-tomorrow":
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
	case "charge-total-solar", "charge-year-solar":
		return st.SolarPercentage, true
	case "charge-total-price", "charge-year-price":
		return st.AvgPrice * 100, true
	}
	return st.ChargedKWh * 1000, true
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
