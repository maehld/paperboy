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

// An explicit empty list is the editor's "None": it shows nothing, unlike an
// absent arg, which keeps the default set.
func TestValuesExplicitEmpty(t *testing.T) {
	e, req := newTestPlugin(t)
	lines, err := e.Text(req, plugin.WidgetRequest{Widget: "values", Width: 20, Height: 2,
		Args: map[string]any{"values": []any{}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Fatalf("empty selection drew %q", lines)
	}
	img := render.NewCanvas(300, 120, 2)
	if err := e.Render(req, plugin.WidgetRequest{Widget: "values", Width: 300, Height: 120,
		Args: map[string]any{"values": []any{}}, Style: plugin.Style{Padding: 8, Rule: 1, TextPx: 20}}, img); err != nil {
		t.Fatal(err)
	}
	if hasInk(img) {
		t.Error("empty selection drew something")
	}
}

// The values arg names its keys in the configured language; the keys stay
// what a page stores.
func TestDescribeValueLabels(t *testing.T) {
	for lang, home := range map[string]string{"de": "Haus", "en": "Home"} {
		e := &evcc{}
		if err := e.Configure(plugin.Values{"url": "http://evcc:7070"}, &plugin.Request{Language: lang}); err != nil {
			t.Fatal(err)
		}
		var values plugin.Arg
		for _, w := range e.Describe().Widgets {
			if w.Widget == "values" {
				values = w.Args["values"]
			}
		}
		if values.Enum == nil || values.EnumLabels == nil {
			t.Fatalf("%s: values arg without enum or labels", lang)
		}
		if got := values.EnumLabels["home"]; got != home {
			t.Errorf("%s: home label %q, want %q", lang, got, home)
		}
		if len(values.Enum) != len(stateValues) {
			t.Errorf("%s: %d enum values, want %d", lang, len(values.Enum), len(stateValues))
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
