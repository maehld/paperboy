package main

import (
	"errors"
	"testing"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

func TestStateValues(t *testing.T) {
	e, _ := newTestPlugin(t)
	cases := []struct {
		key  string
		want float64
	}{
		{"pv", 4.2}, {"home", 0.8}, {"grid", -2.1}, {"battery", 81}, {"battery_power", -1.3},
		{"grid_price", 27}, {"pv_energy", 26009.5}, {"grid_import", 9697.5}, {"grid_export", 11141.6},
		{"battery_charge", 6013.7}, {"battery_discharge", 6362.4},
		{"charge_total", 1234.5}, {"charge_total_solar", 54.8}, {"charge_total_price", 17.05},
		{"charge_year", 145.3}, {"charge_year_solar", 55.1}, {"charge_year_price", 16.39},
		{"solar_today", 12000}, {"solar_tomorrow", 8600},
	}
	names := make([]string, len(cases))
	for i, c := range cases {
		names[i] = c.key
	}
	values, err := e.Values(&plugin.Request{Values: names})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		v := values[c.key]
		if v.Number == nil || *v.Number != c.want {
			t.Errorf("%s = %+v, want %v", c.key, v, c.want)
		}
	}
	// A value evcc does not report becomes a dash.
	values, err = e.Values(&plugin.Request{Values: []string{"feedin_price"}})
	if err != nil {
		t.Fatal(err)
	}
	if v := values["feedin_price"]; v.Text == nil || *v.Text != "–" {
		t.Errorf("feedin_price = %+v", v)
	}
	if _, err := e.Values(&plugin.Request{Values: []string{"nope"}}); !errors.Is(err, plugin.ErrUnknownValue) {
		t.Errorf("unknown value: %v", err)
	}
}

// An explicit empty list is the editor's "None": it shows nothing, unlike an
// absent arg, which keeps the default set.
func TestValuesExplicitEmpty(t *testing.T) {
	e, req := newTestPlugin(t)
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
	if _, err := e.Values(&plugin.Request{Values: []string{"nonsense"}}); err == nil {
		t.Error("unknown value accepted")
	}
}
