package main

import (
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

// TestFlowHomeKeepsItsCentre checks the placement: the home card stays in the
// middle with one or three loadpoints, and moves up only when a row would
// cross the bottom padding.
func TestFlowHomeKeepsItsCentre(t *testing.T) {
	const (
		cy    = 240.0
		cardH = 100.0
		rowH  = 20.0
		gap   = 10.0
	)
	b := image.Rect(0, 0, 800, 480)
	one, rowsOne := flowPlacement(b, cy, cardH, rowH, gap, 1)
	three, rowsThree := flowPlacement(b, cy, cardH, rowH, gap, 3)
	if one != three || one != cy-cardH/2 {
		t.Errorf("home y = %v with one and %v with three, want %v", one, three, cy-cardH/2)
	}
	if len(rowsOne) != 1 || len(rowsThree) != 3 {
		t.Fatalf("rows = %d and %d, want 1 and 3", len(rowsOne), len(rowsThree))
	}
	if rowsThree[0] != one+cardH+gap {
		t.Errorf("first row y = %v, want %v", rowsThree[0], one+cardH+gap)
	}

	low := image.Rect(0, 0, 800, 260)
	top, rows := flowPlacement(low, 130, cardH, rowH, gap, 3)
	if last := rows[len(rows)-1] + rowH; last > float64(low.Max.Y) {
		t.Errorf("the last row ends at %v, below %v", last, low.Max.Y)
	}
	if top >= 130-cardH/2 {
		t.Errorf("the group did not move up: home y = %v", top)
	}
}

// TestFlowWidths checks that a tall cell gives PV and the battery the width of
// the side cards, while a short cell keeps them narrower.
func TestFlowWidths(t *testing.T) {
	tests := []struct {
		name             string
		W, H             float64
		side, mid, cardH float64
	}{
		{"short cell", 800, 240, 240, 192, 62.4},
		{"tall cell", 800, 480, 240, 240, 110},
		{"large cell", 1843, 674, 300, 300, 110},
	}
	for _, tt := range tests {
		side, mid, cardH := flowWidths(tt.W, tt.H)
		if math.Abs(side-tt.side) > 1e-9 || math.Abs(mid-tt.mid) > 1e-9 || math.Abs(cardH-tt.cardH) > 1e-9 {
			t.Errorf("%s: flowWidths(%v, %v) = %v, %v, %v; want %v, %v, %v",
				tt.name, tt.W, tt.H, side, mid, cardH, tt.side, tt.mid, tt.cardH)
		}
	}
}

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

func TestFlowValues(t *testing.T) {
	want := map[string][3]float64{
		"flow_export":              {6200, 400, -2100},
		"flow_import":              {0, 1500, 5200},
		"flow_battery_charging":    {3700, 700, 0},
		"flow_battery_discharging": {0, 1200, 0},
		"flow_no_battery":          {4600, 800, -100},
		"flow_no_loadpoint":        {2000, 900, -1100},
		"flow_two_loadpoints":      {5000, 600, 500},
		"flow_discharge_export":    {0, 800, -2200},
	}
	for name, exp := range want {
		e := flowFixture(t, name)
		values, err := e.Values(&plugin.Request{Values: []string{"pv", "home", "grid", "battery-power", "loadpoint-power"}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for i, key := range []string{"pv", "home", "grid"} {
			v := values[key]
			if v.Number == nil || math.Abs(*v.Number-exp[i]) > 0.001 {
				t.Errorf("%s/%s = %+v, want %v", name, key, v, exp[i])
			}
		}
	}
}

func TestFlowImage(t *testing.T) {
	for _, name := range []string{"flow_export", "flow_import", "flow_battery_charging", "flow_battery_discharging", "flow_no_battery", "flow_no_loadpoint", "flow_two_loadpoints", "flow_discharge_export"} {
		for _, levels := range []int{2, 4, 16} {
			for _, size := range [][3]int{{400, 300, 20}, {400, 180, 20}, {700, 500, 20}, {792, 232, 28}, {260, 230, 16}} {
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
		"flow_no_battery":          "pv>home 800|pv>lp0 3700|pv>grid 100",
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
