package main

import (
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

func energyDay(year int, month time.Month, day int, energy, returned float64) energyPoint {
	return energyPoint{at: time.Date(year, month, day, 0, 0, 0, 0, time.UTC), energy: energy, returnEnergy: returned}
}

func periodFixture(t *testing.T) *evcc {
	t.Helper()
	e := flowFixture(t, "flow_two_loadpoints")
	e.req.Now = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	e.req.Language = "de"
	e.lang = "de"
	e.hist = map[string][]energyPoint{
		"pv":                {energyDay(2026, 11, 18, 12, 0), energyDay(2026, 11, 17, 5, 0)},
		"grid":              {energyDay(2026, 11, 18, 3, 1)},
		"battery":           {energyDay(2026, 11, 18, 2, 4)},
		"home":              {energyDay(2026, 11, 18, 6, 0)},
		"loadpoint:Garage":  {energyDay(2026, 11, 18, 4, 0)},
		"loadpoint:Carport": {energyDay(2026, 11, 18, 0, 0)},
	}
	return e
}

func TestFlowPeriodValues(t *testing.T) {
	e := periodFixture(t)
	m, err := e.flowModel(plugin.WidgetRequest{Args: map[string]any{"period": "today"}})
	if err != nil {
		t.Fatal(err)
	}
	if m.lps != 1 || m.battery != 2 {
		t.Fatalf("model = %d lps, battery %d", m.lps, m.battery)
	}
	want := []struct {
		label, value string
		net          float64
		idle         bool
	}{
		{"PV", "12 kWh", 12, false},
		{"Netzbezug", "2,0 kWh", 2, false},
		{"Akku", "2,0 kWh", 2, false},
		{"Haus", "6,0 kWh", -6, false},
		{"Octavia", "4,0 kWh", -4, false},
	}
	if len(m.nodes) != len(want) {
		t.Fatalf("nodes = %d, want %d", len(m.nodes), len(want))
	}
	for i, w := range want {
		n := m.nodes[i]
		if n.label != w.label || n.value != w.value || n.net != w.net || n.idle != w.idle {
			t.Errorf("node %d = %+v, want %+v", i, n, w)
		}
	}
}

func TestFlowPeriodExport(t *testing.T) {
	e := periodFixture(t)
	e.hist["grid"] = []energyPoint{energyDay(2026, 11, 18, 1, 9)}
	m, err := e.flowModel(plugin.WidgetRequest{Args: map[string]any{"period": "today"}})
	if err != nil {
		t.Fatal(err)
	}
	grid := m.nodes[flowGrid]
	if grid.label != "Einspeisung" || grid.value != "8,0 kWh" || grid.net != -8 || grid.idle {
		t.Fatalf("grid = %+v", grid)
	}
}

func TestFlowPeriodRange(t *testing.T) {
	e := flowFixture(t, "flow_export")
	e.req.Now = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC) // a Wednesday
	want := map[string][2]string{
		"today":      {"2026-11-18", "2026-11-19"},
		"yesterday":  {"2026-11-17", "2026-11-18"},
		"week":       {"2026-11-16", "2026-11-19"},
		"last_week":  {"2026-11-09", "2026-11-16"},
		"month":      {"2026-11-01", "2026-11-19"},
		"last_month": {"2026-10-01", "2026-11-01"},
		"year":       {"2026-01-01", "2026-11-19"},
		"last_year":  {"2025-01-01", "2026-01-01"},
	}
	for period, exp := range want {
		from, to, ok := e.periodRange(period)
		if !ok || from != exp[0] || to != exp[1] {
			t.Errorf("%s = %q..%q (%v), want %q..%q", period, from, to, ok, exp[0], exp[1])
		}
	}
	if _, _, ok := e.periodRange("tomorrow"); ok {
		t.Error("unknown period accepted")
	}
}

func TestFlowPeriodWithoutHistory(t *testing.T) {
	e := flowFixture(t, "flow_export")
	e.req.Now = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	if _, err := e.flowModel(plugin.WidgetRequest{Args: map[string]any{"period": "today"}}); err == nil {
		t.Fatal("a missing history was accepted")
	}
}

func TestFlowPeriodImage(t *testing.T) {
	for _, name := range []string{"flow_export", "flow_two_loadpoints"} {
		for _, levels := range []int{2, 4, 16} {
			e := flowFixture(t, name)
			e.req.Now = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
			e.hist = map[string][]energyPoint{
				"pv":                {energyDay(2026, 11, 18, 30, 0)},
				"grid":              {energyDay(2026, 11, 18, 3, 5)},
				"battery":           {energyDay(2026, 11, 18, 2, 4)},
				"home":              {energyDay(2026, 11, 18, 8, 0)},
				"loadpoint:Garage":  {energyDay(2026, 11, 18, 12, 0)},
				"loadpoint:Carport": {energyDay(2026, 11, 18, 6, 0)},
				"loadpoint:Street":  {energyDay(2026, 11, 18, 0, 0)},
			}
			img := render.NewCanvas(792, 232, levels)
			w := plugin.WidgetRequest{Widget: "flow", Width: 792, Height: 232,
				Style: plugin.Style{Padding: 8, Rule: 1, TextPx: 28}, Args: map[string]any{"period": "today"}}
			if err := e.Render(e.req, w, img); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if !hasInk(img) {
				t.Errorf("%s: empty", name)
			}
			if dir := os.Getenv("FLOW_PNG_DIR"); dir != "" {
				f, _ := os.Create(filepath.Join(dir, name+"-period-"+strconv.Itoa(levels)+".png"))
				png.Encode(f, img)
				f.Close()
			}
		}
	}
}

func TestBalanceRows(t *testing.T) {
	e := flowFixture(t, "flow_two_loadpoints")
	left, right := e.balanceRows(e.balanceNow())
	if len(left) != 3 || len(right) != 4 {
		t.Fatalf("rows = %d/%d, want 3/4", len(left), len(right))
	}
	if left[2].label != "Battery discharge" || right[1].label != "Charging (2)" {
		t.Errorf("labels = %q, %q", left[2].label, right[1].label)
	}
	if right[3].label != "Grid export" || !right[3].idle || right[3].value != "–" {
		t.Errorf("export = %+v", right[3])
	}

	e = flowFixture(t, "flow_battery_charging")
	left, right = e.balanceRows(e.balanceNow())
	if left[2].value != "–" || !left[2].idle {
		t.Errorf("discharge = %+v", left[2])
	}
	if right[2].label != "Battery charge" || right[2].value != "3.0 kW" {
		t.Errorf("charge = %+v", right[2])
	}

	e = flowFixture(t, "flow_no_battery")
	left, right = e.balanceRows(e.balanceNow())
	if len(left) != 2 || len(right) != 3 {
		t.Errorf("without battery: %d/%d rows", len(left), len(right))
	}
}

func TestBalanceImage(t *testing.T) {
	for _, name := range []string{"flow_export", "flow_two_loadpoints", "flow_battery_charging", "flow_no_battery", "flow_import"} {
		for _, levels := range []int{2, 4, 16} {
			for _, size := range [][3]int{{400, 300, 20}, {792, 232, 28}, {260, 230, 16}} {
				e := flowFixture(t, name)
				img := render.NewCanvas(size[0], size[1], levels)
				w := plugin.WidgetRequest{Widget: "balance", Width: size[0], Height: size[1],
					Style: plugin.Style{Padding: 8, Rule: 1, TextPx: float64(size[2])}}
				if err := e.Render(e.req, w, img); err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				if !hasInk(img) {
					t.Errorf("%s: empty", name)
				}
				if dir := os.Getenv("FLOW_PNG_DIR"); dir != "" {
					f, _ := os.Create(filepath.Join(dir, "balance-"+name+"-"+strconv.Itoa(levels)+"-"+strconv.Itoa(size[0])+"x"+strconv.Itoa(size[1])+".png"))
					png.Encode(f, img)
					f.Close()
				}
			}
		}
	}
}

func TestBalancePeriodValues(t *testing.T) {
	e := periodFixture(t)
	totals, err := e.balancePeriod("today")
	if err != nil {
		t.Fatal(err)
	}
	left, right := e.balanceRows(totals)
	wantLeft := []string{"12 kWh", "3,0 kWh", "4,0 kWh"}
	wantRight := []string{"6,0 kWh", "4,0 kWh", "2,0 kWh", "1,0 kWh"}
	if len(left) != len(wantLeft) || len(right) != len(wantRight) {
		t.Fatalf("rows = %d/%d, want %d/%d", len(left), len(right), len(wantLeft), len(wantRight))
	}
	for i, want := range wantLeft {
		if left[i].value != want {
			t.Errorf("left %d = %q, want %q", i, left[i].value, want)
		}
	}
	for i, want := range wantRight {
		if right[i].value != want {
			t.Errorf("right %d = %q, want %q", i, right[i].value, want)
		}
	}
	if right[1].label != "Laden" || right[1].idle {
		t.Errorf("charging = %+v", right[1])
	}
}

func TestBalancePeriodRange(t *testing.T) {
	e := periodFixture(t)
	// The fixture's PV has 5 kWh yesterday, which belongs to this week.
	totals, err := e.balancePeriod("week")
	if err != nil {
		t.Fatal(err)
	}
	if totals.pv != 17 {
		t.Fatalf("pv = %v, want 17 kWh", totals.pv)
	}
	totals, err = e.balancePeriod("yesterday")
	if err != nil {
		t.Fatal(err)
	}
	left, right := e.balanceRows(totals)
	if left[0].value != "5,0 kWh" || left[1].value != "–" || right[0].value != "–" {
		t.Errorf("yesterday = left %q, %q, right %q", left[0].value, left[1].value, right[0].value)
	}
}

func TestBalancePeriodUnknown(t *testing.T) {
	e := periodFixture(t)
	if _, err := e.balancePeriod("tomorrow"); err == nil {
		t.Fatal("unknown period accepted")
	}
}

func TestBalancePeriodWithoutHistory(t *testing.T) {
	e := flowFixture(t, "flow_export")
	e.req.Now = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	if _, err := e.balancePeriod("today"); err == nil {
		t.Fatal("a missing history was accepted")
	}
}

func TestBalancePeriodImage(t *testing.T) {
	for _, name := range []string{"flow_export", "flow_two_loadpoints"} {
		for _, levels := range []int{2, 4, 16} {
			for _, size := range [][3]int{{400, 300, 20}, {792, 232, 28}} {
				e := flowFixture(t, name)
				e.req.Now = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
				e.hist = map[string][]energyPoint{
					"pv":                {energyDay(2026, 11, 18, 30, 0)},
					"grid":              {energyDay(2026, 11, 18, 3, 5)},
					"battery":           {energyDay(2026, 11, 18, 2, 4)},
					"home":              {energyDay(2026, 11, 18, 8, 0)},
					"loadpoint:Garage":  {energyDay(2026, 11, 18, 12, 0)},
					"loadpoint:Carport": {energyDay(2026, 11, 18, 6, 0)},
				}
				img := render.NewCanvas(size[0], size[1], levels)
				w := plugin.WidgetRequest{Widget: "balance", Width: size[0], Height: size[1],
					Style: plugin.Style{Padding: 8, Rule: 1, TextPx: float64(size[2])},
					Args:  map[string]any{"period": "today"}}
				if err := e.Render(e.req, w, img); err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				if !hasInk(img) {
					t.Errorf("%s: empty", name)
				}
				if dir := os.Getenv("FLOW_PNG_DIR"); dir != "" {
					f, _ := os.Create(filepath.Join(dir, "balance-"+name+"-period-"+strconv.Itoa(levels)+"-"+strconv.Itoa(size[0])+"x"+strconv.Itoa(size[1])+".png"))
					png.Encode(f, img)
					f.Close()
				}
			}
		}
	}
}

func TestBalanceGerman(t *testing.T) {
	e := flowFixture(t, "flow_two_loadpoints")
	e.req.Language = "de"
	e.lang = "de"
	left, right := e.balanceRows(e.balanceNow())
	if left[1].label != "Netzbezug" || right[1].label != "Laden (2)" || right[3].label != "Einspeisung" {
		t.Errorf("labels = %q, %q, %q", left[1].label, right[1].label, right[3].label)
	}
}
