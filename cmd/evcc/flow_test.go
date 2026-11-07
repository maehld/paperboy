package main

import (
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
	for _, name := range []string{"flow_export", "flow_import", "flow_battery_charging", "flow_battery_discharging", "flow_no_battery", "flow_no_loadpoint"} {
		for _, levels := range []int{2, 4} {
			for _, size := range [][2]int{{400, 300}, {700, 500}} {
				e := flowFixture(t, name)
				img := render.NewCanvas(size[0], size[1], levels)
				w := plugin.WidgetRequest{Widget: "flow", Width: size[0], Height: size[1], Style: plugin.Style{Padding: 8, Rule: 1, TextPx: 20}}
				if err := e.Render(e.req, w, img); err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				if !hasInk(img) {
					t.Errorf("%s: empty", name)
				}
				if dir := os.Getenv("FLOW_PNG_DIR"); dir != "" {
					f, _ := os.Create(filepath.Join(dir, name+"-"+string(rune('0'+levels))+"-"+string(rune('a'+size[0]/350))+".png"))
					png.Encode(f, img)
					f.Close()
				}
			}
		}
	}
}
