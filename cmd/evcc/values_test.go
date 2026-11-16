package main

import (
	"errors"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

func TestStateValues(t *testing.T) {
	e, _ := newTestPlugin(t)
	cases := []struct {
		key  string
		want float64
	}{
		{"pv", 4.2}, {"home", 0.8}, {"grid", -2.1}, {"battery", 81}, {"battery-power", -1.3},
		{"grid-price", 27}, {"pv-energy", 26009.5}, {"grid-import", 9697.5}, {"grid-export", 11141.6},
		{"battery-charge", 6013.7}, {"battery-discharge", 6362.4},
		{"charge-total", 1234.5}, {"charge-total-solar", 54.8}, {"charge-total-price", 17.05},
		{"charge-year", 145.3}, {"charge-year-solar", 55.1}, {"charge-year-price", 16.39},
		{"forecast-today", 12000}, {"forecast-tomorrow", 8600},
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
	values, err = e.Values(&plugin.Request{Values: []string{"feedin-price"}})
	if err != nil {
		t.Fatal(err)
	}
	if v := values["feedin-price"]; v.Text == nil || *v.Text != "–" {
		t.Errorf("feedin-price = %+v", v)
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

// The daily values read today's bucket of the cached energy history; a page
// and the history widget agree.
func TestDailyValues(t *testing.T) {
	e, _ := newHistoryPlugin(t)
	cases := map[string]float64{
		"pv-today": 5, "home-today": 20, "grid-import-today": 10, "grid-export-today": 2,
		"battery-charge-today": 1, "battery-discharge-today": 4,
		// All loadpoints of /api/state: the history also carries a series of a
		// loadpoint that evcc no longer has, which does not count.
		"charge-today": 1.5,
	}
	names := make([]string, 0, len(cases))
	for name := range cases {
		names = append(names, name)
	}
	values, err := e.Values(&plugin.Request{Values: names})
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range cases {
		if v := values[name]; v.Number == nil || *v.Number != want {
			t.Errorf("%s = %+v, want %v", name, v, want)
		}
	}
	// With index, charge-today reports one loadpoint and ignores the history
	// of the others; an index evcc does not have becomes a dash.
	one, err := e.Values(&plugin.Request{Values: []string{"charge-today"}, ValueArgs: map[string]any{"index": 0}})
	if err != nil {
		t.Fatal(err)
	}
	if v := one["charge-today"]; v.Number == nil || *v.Number != 1.5 {
		t.Errorf("charge-today index 0 = %+v, want 1.5", v)
	}
	values, err = e.Values(&plugin.Request{Values: []string{"charge-today"}, ValueArgs: map[string]any{"index": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if v := values["charge-today"]; v.Text == nil || *v.Text != "–" {
		t.Errorf("charge-today of a missing loadpoint = %+v", v)
	}
	// The widget formats the same numbers; its charge-today sums all
	// loadpoints.
	for name, want := range map[string]string{"pv-today": "5,0 kWh", "charge-today": "1,5 kWh"} {
		if got := e.valueText(name); got != want {
			t.Errorf("valueText(%s) = %q, want %q", name, got, want)
		}
	}
}

// charge-today sums all loadpoints without index and follows index with it, as
// the other loadpoint values do (ADR 0120).
func TestChargeToday(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 11, d, 0, 0, 0, 0, time.UTC) }
	e := &evcc{
		req: &plugin.Request{Timezone: "UTC", Now: time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)},
		cur: &state{Loadpoints: []loadpoint{{Title: "Garage"}, {Title: "Car"}, {Title: "Van"}, {Title: "New"}}},
		hist: map[string][]energyPoint{
			"loadpoint:Garage": {{at: day(12), energy: 2}},
			"loadpoint:Car":    {{at: day(12), energy: 3.5}, {at: day(11), energy: 9}},
			"loadpoint:Van":    {{at: day(11), energy: 7}},
		},
	}
	values, err := e.Values(&plugin.Request{Values: []string{"charge-today"}})
	if err != nil {
		t.Fatal(err)
	}
	if v := values["charge-today"]; v.Number == nil || *v.Number != 5.5 {
		t.Errorf("sum = %+v, want 5.5", v)
	}
	for _, c := range []struct {
		index int
		want  float64 // -1 means a dash
	}{
		{0, 2}, {1, 3.5}, {2, 0}, {3, -1}, {4, -1},
	} {
		values, err := e.Values(&plugin.Request{Values: []string{"charge-today"}, ValueArgs: map[string]any{"index": c.index}})
		if err != nil {
			t.Fatal(err)
		}
		v := values["charge-today"]
		if c.want < 0 {
			if v.Text == nil || *v.Text != "–" {
				t.Errorf("index %d = %+v, want a dash", c.index, v)
			}
			continue
		}
		if v.Number == nil || *v.Number != c.want {
			t.Errorf("index %d = %+v, want %v", c.index, v, c.want)
		}
	}
	// describe declares the index for charge-today, so the editor offers it.
	var spec plugin.ValueSpec
	for _, v := range e.Describe().Values {
		if v.Name == "charge-today" {
			spec = v
		}
	}
	if _, ok := spec.Args["index"]; !ok {
		t.Errorf("charge-today declares no index: %+v", spec)
	}
}

// An evcc without the energy database keeps the instant values: the daily
// values answer a dash instead of an error.
func TestDailyValuesWithoutHistory(t *testing.T) {
	e, _ := newStateOnlyPlugin(t)
	names := []string{"pv-today", "home-today", "grid-import-today", "grid-export-today",
		"battery-charge-today", "battery-discharge-today", "charge-today", "pv"}
	values, err := e.Values(&plugin.Request{Values: names})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names[:len(names)-1] {
		if v := values[name]; v.Text == nil || *v.Text != "–" {
			t.Errorf("%s = %+v, want a dash", name, v)
		}
		if got := e.valueText(name); got != "–" {
			t.Errorf("valueText(%s) = %q, want a dash", name, got)
		}
	}
	if v := values["pv"]; v.Number == nil || *v.Number != 4.2 {
		t.Errorf("pv = %+v, want 4.2", v)
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
