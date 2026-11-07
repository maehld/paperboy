package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

func flowFixture(t *testing.T, name string) *evcc {
	body, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := parseState(body)
	if err != nil {
		t.Fatal(err)
	}
	req := &plugin.Request{Language: "en", Timezone: "UTC", Page: plugin.PageInfo{Height: 480, PPI: 125}}
	return &evcc{cur: s, req: req, lang: "en"}
}

func TestFlowText(t *testing.T) {
	want := map[string]string{
		"flow_export":              "PV 6.2 kW|Home 400 W|Car 3.7 kW|Grid -2.1 kW|Batt 100% 0 W",
		"flow_import":              "PV 0 W|Home 1.5 kW|Car 3.7 kW|Grid +5.2 kW|Batt 8% 0 W",
		"flow_battery_charging":    "PV 5.0 kW|Home 700 W|Grid 0 W|Batt 55% -3.0 kW",
		"flow_battery_discharging": "PV 0 W|Home 1.2 kW|Grid 0 W|Batt 72% +1.2 kW",
		"flow_no_battery":          "PV 4.2 kW|Home 800 W|Car 3.7 kW|Grid -100 W",
		"flow_no_loadpoint":        "PV 2.0 kW|Home 900 W|Grid -1.1 kW|Batt 50% 0 W",
		"flow_two_loadpoints":      "PV 5.0 kW|Home 600 W|Car 3.7 kW|Car 2.0 kW|Car 0 W|Grid +500 W|Batt 40% +800 W",
		"flow_discharge_export":    "PV 0 W|Home 800 W|Grid -2.2 kW|Batt 90% +3.0 kW",
	}
	for name, exp := range want {
		e := flowFixture(t, name)
		lines := e.flowText(plugin.WidgetRequest{Widget: "flow", Width: 24, Height: 6})
		for i, l := range lines {
			lines[i] = strings.Join(strings.Fields(l), " ")
		}
		if got := strings.Join(lines, "|"); got != exp {
			t.Errorf("%s: got %q want %q", name, got, exp)
		}
	}
}

func TestFlowImage(t *testing.T) {
	for _, name := range []string{"flow_export", "flow_import", "flow_battery_charging", "flow_battery_discharging", "flow_no_battery", "flow_no_loadpoint", "flow_two_loadpoints", "flow_discharge_export"} {
		for _, levels := range []int{2, 4} {
			for _, size := range [][3]int{{400, 300, 20}, {700, 500, 20}, {792, 232, 28}, {260, 230, 16}} {
				e := flowFixture(t, name)
				img := render.NewCanvas(size[0], size[1], levels)
				w := plugin.WidgetRequest{Widget: "flow", Width: size[0], Height: size[1], Style: plugin.Style{Padding: 8, Rule: 1, TextPx: float64(size[2])}}
				if err := e.Render(e.req, w, img); err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				if !hasInk(img) {
					t.Errorf("%s: empty", name)
				}
				if dir := os.Getenv("FLOW_PNG_DIR"); dir != "" {
					f, _ := os.Create(filepath.Join(dir, fmt.Sprintf("%s-%d-%dx%d.png", name, levels, size[0], size[1])))
					png.Encode(f, img)
					f.Close()
				}
			}
		}
	}
}

func TestSplitFlows(t *testing.T) {
	names := map[int]string{flowPV: "pv", flowGrid: "grid", flowBattery: "battery", flowHome: "home"}
	want := map[string]string{
		"flow_export":              "pv>home 400|pv>lp0 3700|pv>grid 2100",
		"flow_import":              "grid>home 1500|grid>lp0 3700",
		"flow_battery_charging":    "pv>home 700|pv>battery 3000",
		"flow_battery_discharging": "battery>home 1200",
		"flow_no_battery":          "pv>home 800|pv>lp0 3400",
		"flow_no_loadpoint":        "pv>home 900|pv>grid 1100",
		"flow_two_loadpoints":      "pv>home 600|pv>lp0 3700|pv>lp1 700|battery>lp1 800|grid>lp1 500",
		"flow_discharge_export":    "battery>home 800|battery>grid 2200",
	}
	name := func(i int) string {
		if n, ok := names[i]; ok {
			return n
		}
		return fmt.Sprintf("lp%d", i-flowLP)
	}
	for fixture, exp := range want {
		s := flowFixture(t, fixture).cur
		var lps []float64
		for _, lp := range s.activeLoadpoints() {
			lps = append(lps, lp.ChargePower)
		}
		bat := 0.0
		if s.hasBattery() {
			bat = s.Battery.Power
		}
		var got []string
		for _, sh := range splitFlows(s.PVPower, s.gridPower(), bat, s.HomePower, lps) {
			got = append(got, fmt.Sprintf("%s>%s %.0f", name(sh.from), name(sh.to), sh.power))
		}
		if g := strings.Join(got, "|"); g != exp {
			t.Errorf("%s: got %q want %q", fixture, g, exp)
		}
	}
}
