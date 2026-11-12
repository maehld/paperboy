package main

import (
	"strings"
	"testing"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

func TestValuesText(t *testing.T) {
	e, req := newTestPlugin(t)
	cases := []struct {
		values []any
		want   [][2]string
	}{
		{nil, [][2]string{{"PV", "4,2 kW"}, {"Haus", "800 W"}, {"Netz", "-2,1 kW"}, {"Akku", "81 %"}}},
		{[]any{"pv_energy", "grid_import", "grid_export", "battery_charge", "battery_discharge"},
			[][2]string{{"PV gesamt", "26.010 kWh"}, {"Netzbezug gesamt", "9.698 kWh"},
				{"Einspeisung gesamt", "11.142 kWh"}, {"Akku geladen", "6.014 kWh"},
				{"Akku entladen", "6.362 kWh"}}},
		{[]any{"charge_year", "charge_year_solar", "charge_year_price"},
			[][2]string{{"Geladen dieses Jahr", "145 kWh"}, {"Solaranteil dieses Jahr", "55 %"},
				{"Ø Ladepreis dieses Jahr", "16,4 ct"}}},
		{[]any{"solar_today", "solar_tomorrow", "feedin_price"},
			[][2]string{{"Prognose heute", "12,0 kWh"}, {"Prognose morgen", "8,6 kWh"},
				{"Einspeisevergütung", "–"}}},
	}
	for _, c := range cases {
		var args map[string]any
		if c.values != nil {
			args = map[string]any{"values": c.values}
		}
		lines, err := e.Text(req, plugin.WidgetRequest{Widget: "values", Width: 26, Height: len(c.want), Args: args})
		if err != nil {
			t.Fatal(err)
		}
		var want []string
		for _, w := range c.want {
			want = append(want, plugin.Columns(w[0], w[1], 26))
		}
		if strings.Join(lines, "|") != strings.Join(want, "|") {
			t.Errorf("%v:\n%q\nwant\n%q", c.values, lines, want)
		}
	}
}

func TestValuesWidget(t *testing.T) {
	e, req := newTestPlugin(t)
	style := plugin.Style{Padding: 8, Rule: 1, TextPx: 20}
	if err := e.Render(req, plugin.WidgetRequest{Widget: "values", Width: 400, Height: 200,
		Args: map[string]any{"values": []any{"pv", "grid", "battery"}}, Style: style},
		render.NewCanvas(400, 200, 4)); err != nil {
		t.Fatal(err)
	}
	// The default values fill a widget without args.
	img := render.NewCanvas(300, 120, 2)
	if err := e.Render(req, plugin.WidgetRequest{Widget: "values", Width: 300, Height: 120, Style: style}, img); err != nil {
		t.Fatal(err)
	}
	if !hasInk(img) {
		t.Error("empty widget")
	}
	if _, err := e.Text(req, plugin.WidgetRequest{Widget: "values", Width: 20, Height: 2,
		Args: map[string]any{"values": []any{"nonsense"}}}); err == nil {
		t.Error("unknown value accepted")
	}
}
