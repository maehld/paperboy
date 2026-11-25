package builtin

import (
	"image"
	"slices"
	"testing"
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

var testStatusTime = time.Date(2026, 11, 15, 9, 5, 0, 0, time.UTC)

func testStatus() *plugin.ServerStatus {
	percent := 84.0
	low := 21.0
	return &plugin.ServerStatus{
		CurrentDeviceStatus: &plugin.DeviceStatus{ID: "kueche", Status: "ok", SeenS: 30,
			Battery: &plugin.BatteryStatus{Volts: 3.75, Percent: &percent}, RuntimeS: 12 * 24 * 3600},
		SelectedDevicesStatus: map[string]plugin.DeviceStatus{
			"wohnzimmer": {ID: "wohnzimmer", Status: "late", SeenS: 7440,
				Battery: &plugin.BatteryStatus{Volts: 3.41, Percent: &low}},
			"flur": {ID: "flur", Status: "never", SeenS: -1},
		},
	}
}

func TestBatteryTextFollowsLanguage(t *testing.T) {
	percent := 84.0
	dev := &plugin.DeviceStatus{Battery: &plugin.BatteryStatus{Volts: 3.75, Percent: &percent}}
	for _, c := range []struct {
		lang, label, want string
	}{
		{"de", "auto", "84 %"},
		{"en", "auto", "84%"},
		{"de", "volts", "3,75 V"},
		{"en", "volts", "3.75 V"},
		{"de", "none", ""},
	} {
		req := &plugin.Request{Language: c.lang}
		if got := batteryText(req, dev, c.label); got != c.want {
			t.Errorf("%s/%s: got %q, want %q", c.lang, c.label, got, c.want)
		}
	}
	if got := batteryText(&plugin.Request{Language: "de"}, nil, "auto"); got != "-" {
		t.Errorf("missing battery: %q", got)
	}
}

func TestDeviceStatusAndBatteryDraw(t *testing.T) {
	for _, c := range []struct {
		name   string
		widget string
		args   map[string]any
		w, h   int
	}{
		{"status", "device-status", nil, 300, 100},
		{"status-selected", "device-status", map[string]any{"devices": []any{"wohnzimmer", "flur"}}, 300, 100},
		{"battery", "battery", nil, 120, 40},
		{"battery-icon", "battery", map[string]any{"label": "none"}, 80, 40},
	} {
		req := &plugin.Request{Language: "de", ServerStatus: testStatus()}
		w := plugin.WidgetRequest{Widget: c.widget, Width: c.w, Height: c.h, Args: c.args,
			Style: plugin.Style{Padding: 4, Rule: 1, TextPx: 16}}
		img := render.NewCanvas(c.w, c.h, 2)
		if err := (Plugin{}).Render(req, w, img); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		ink := false
		for _, p := range img.Pix {
			if p != 1 {
				ink = true
				break
			}
		}
		if !ink {
			t.Errorf("%s: nothing drawn", c.name)
		}
	}
}

// batteryRequest returns a request that shows one device with the given
// charge. A nil percent is a device without a battery reading.
func batteryRequest(percent *float64, level string, charging bool) *plugin.Request {
	dev := &plugin.DeviceStatus{ID: "kueche", Status: "ok"}
	if percent != nil {
		dev.Battery = &plugin.BatteryStatus{Volts: 3.75, Percent: percent, Level: level, Charging: charging}
	}
	return &plugin.Request{Language: "de", ServerStatus: &plugin.ServerStatus{CurrentDeviceStatus: dev}}
}

// renderBattery draws the battery widget and returns the canvas and the padded
// cell, the area the widget must stay inside.
func renderBattery(t *testing.T, req *plugin.Request, w, h int, args map[string]any) (*image.Paletted, image.Rectangle) {
	t.Helper()
	style := plugin.Style{Padding: 4, Rule: 1, TextPx: 16}
	img := render.NewCanvas(w, h, 2)
	wr := plugin.WidgetRequest{Widget: "battery", Width: w, Height: h, Args: args, Style: style}
	if err := (Plugin{}).Render(req, wr, img); err != nil {
		t.Fatalf("battery %dx%d: %v", w, h, err)
	}
	return img, image.Rect(0, 0, w, h).Inset(style.Padding)
}

// inkCount counts the black pixels in r.
func inkCount(img *image.Paletted, r image.Rectangle) int {
	r = r.Intersect(img.Rect)
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if img.ColorIndexAt(x, y) == 0 {
				n++
			}
		}
	}
	return n
}

// inkBox is the bounding box of the black pixels in r.
func inkBox(img *image.Paletted, r image.Rectangle) (image.Rectangle, bool) {
	r = r.Intersect(img.Rect)
	var box image.Rectangle
	found := false
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if img.ColorIndexAt(x, y) != 0 {
				continue
			}
			if !found {
				box, found = image.Rect(x, y, x+1, y+1), true
				continue
			}
			box = box.Union(image.Rect(x, y, x+1, y+1))
		}
	}
	return box, found
}

func TestBatteryGaugeSegments(t *testing.T) {
	const w, h = 240, 80
	cell := image.Rect(0, 0, w, h).Inset(4)
	icon := layoutBattery(cell, "", render.Center).icon
	ink := func(percent float64) int {
		img, _ := renderBattery(t, batteryRequest(&percent, "", false), w, h, map[string]any{"label": "none"})
		return inkCount(img, icon)
	}
	zero, low, mid, full := ink(0), ink(4), ink(84), ink(100)
	if low <= zero {
		t.Errorf("a low charge adds no segment: %d, %d", zero, low)
	}
	if mid <= low || full <= mid {
		t.Errorf("the fill does not step up: %d, %d, %d", low, mid, full)
	}
}

func TestBatteryStaysInPadding(t *testing.T) {
	percent := 84.0
	for _, c := range []struct {
		name string
		w, h int
		args map[string]any
	}{
		{"3x1", 200, 50, nil},
		{"1x1-volts", 103, 30, map[string]any{"label": "volts"}},
		{"1x1", 312, 175, nil},
		{"2x2", 624, 348, nil},
		{"2x2-volts", 133, 75, map[string]any{"label": "volts"}},
		{"narrow", 60, 40, map[string]any{"label": "volts"}},
		{"3x1-right", 200, 50, map[string]any{"align": "right"}},
		{"icon-only-square", 120, 120, map[string]any{"label": "none"}},
	} {
		img, cell := renderBattery(t, batteryRequest(&percent, "", false), c.w, c.h, c.args)
		box, ok := inkBox(img, img.Rect)
		if !ok {
			t.Fatalf("%s: nothing drawn", c.name)
		}
		if !box.In(cell) {
			t.Errorf("%s: ink %v leaves the padded cell %v", c.name, box, cell)
		}
	}
}

func TestBatteryKeepsValue(t *testing.T) {
	percent := 84.0
	const w, h = 103, 30
	img, cell := renderBattery(t, batteryRequest(&percent, "", false), w, h, map[string]any{"label": "volts"})
	bl := layoutBattery(cell, "3,75 V", render.Center)
	if bl.icon.Empty() {
		t.Fatal("no gauge in a narrow cell")
	}
	if bl.icon.Dy() > cell.Dy() {
		t.Errorf("gauge %v is taller than the cell %v", bl.icon, cell)
	}
	if bl.icon.Dx() > 2*bl.icon.Dy()+1 {
		t.Errorf("gauge %v does not keep its shape", bl.icon)
	}
	if inkCount(img, image.Rect(bl.icon.Max.X+1, cell.Min.Y, cell.Max.X, cell.Max.Y)) == 0 {
		t.Error("the value is missing beside the gauge")
	}

	// A cell that is too narrow for both keeps the value and drops the gauge.
	if bl := layoutBattery(image.Rect(0, 0, 4, 60), "3,75 V", render.Center); !bl.icon.Empty() || bl.text == "" {
		t.Errorf("narrow cell: gauge %v, value %q", bl.icon, bl.text)
	}
}

func TestBatteryChargingBolt(t *testing.T) {
	percent := 100.0
	const w, h = 240, 80
	cell := image.Rect(0, 0, w, h).Inset(4)
	icon := layoutBattery(cell, "", render.Center).icon
	plain, _ := renderBattery(t, batteryRequest(&percent, "", false), w, h, map[string]any{"label": "none"})
	charging, _ := renderBattery(t, batteryRequest(&percent, "", true), w, h, map[string]any{"label": "none"})
	if inkCount(charging, icon) >= inkCount(plain, icon) {
		t.Error("the charging mark does not knock a bolt out of the fill")
	}
}

func TestBatteryMissingReading(t *testing.T) {
	zero := 0.0
	const w, h = 240, 80
	cell := image.Rect(0, 0, w, h).Inset(4)
	icon := layoutBattery(cell, "", render.Center).icon
	empty, _ := renderBattery(t, batteryRequest(&zero, "", false), w, h, map[string]any{"label": "none"})
	missing, _ := renderBattery(t, batteryRequest(nil, "", false), w, h, map[string]any{"label": "none"})
	if missingInk, emptyInk := inkCount(missing, icon), inkCount(empty, icon); missingInk <= emptyInk {
		t.Errorf("a missing reading adds no mark: %d, %d", missingInk, emptyInk)
	}
}

func TestBatteryLevelMark(t *testing.T) {
	percent := 5.0
	const w, h = 240, 80
	cell := image.Rect(0, 0, w, h).Inset(4)
	icon := layoutBattery(cell, "", render.Center).icon
	y := icon.Min.Y + icon.Dy()/2
	thickness := func(level string) int {
		img, _ := renderBattery(t, batteryRequest(&percent, level, false), w, h, map[string]any{"label": "none"})
		n := 0
		for x := icon.Min.X; x < icon.Max.X; x++ {
			if img.ColorIndexAt(x, y) != 0 {
				break
			}
			n++
		}
		return n
	}
	ok, warn, critical := thickness(""), thickness("warn"), thickness("critical")
	if ok >= warn || warn >= critical {
		t.Errorf("outline thickness: ok %d, warn %d, critical %d", ok, warn, critical)
	}
}

func TestBandBatteryIconOnly(t *testing.T) {
	req := &plugin.Request{Language: "de", Now: testStatusTime, Page: plugin.PageInfo{Title: ""}, ServerStatus: testStatus()}
	w := plugin.WidgetRequest{Widget: "band", Width: 400, Height: 60, Style: plugin.Style{Rule: 1, TextPx: 16},
		Args: map[string]any{"page_indicator": false, "clock": "none", "battery": true}}
	img := render.NewCanvas(w.Width, w.Height, 2)
	if err := (Plugin{}).Render(req, w, img); err != nil {
		t.Fatal(err)
	}
	// The band draws only the gauge; the rule comes from the page.
	box, ok := inkBox(img, image.Rect(0, 0, w.Width, w.Height))
	if !ok {
		t.Fatal("nothing drawn")
	}
	if box.Max.X != w.Width {
		t.Errorf("the gauge does not reach the right edge: %v", box)
	}
	if want := w.Height/2 + int(float64(w.Height)*0.5*0.36); box.Max.Y != want {
		t.Errorf("the gauge does not sit on the text baseline: %v, want bottom %d", box, want)
	}
	if box.Dx() > 2*box.Dy()+2 {
		t.Errorf("the header draws a value beside the gauge: ink %v", box)
	}
}

// TestBandBatteryMatchesTheText keeps the gauge at the height of a capital
// letter when the band is tall, as on the E1003.
func TestBandBatteryMatchesTheText(t *testing.T) {
	// The E1003's header: one row of 1404 / 8 px at 227 ppi.
	style := plugin.Style{Padding: 14, Rule: 2, TextPx: 16 * 227.0 / 72}
	req := &plugin.Request{Language: "de", Now: testStatusTime,
		Page:         plugin.PageInfo{Title: "", Width: 1872, Height: 1404, PPI: 227},
		ServerStatus: testStatus()}
	w := plugin.WidgetRequest{Widget: "band", Width: 1872, Height: 1404 / 8, Style: style,
		Args: map[string]any{"page_indicator": false, "clock": "none", "battery": true}}
	img := render.NewCanvas(w.Width, w.Height, 4)
	if err := (Plugin{}).Render(req, w, img); err != nil {
		t.Fatal(err)
	}
	box, ok := inkBox(img, image.Rect(0, 0, w.Width, w.Height-style.Rule))
	if !ok {
		t.Fatal("nothing drawn")
	}
	textSize := float64(w.Height) * 0.5
	capH := int(render.Face(textSize, false).Metrics().CapHeight.Round())
	if box.Dy() > capH+1 {
		t.Errorf("the gauge is %d px high, a capital letter is %d", box.Dy(), capH)
	}
}

func TestBatterySpans(t *testing.T) {
	want := []plugin.Span{{Columns: 1, Rows: 1}, {Columns: 2, Rows: 1}, {Columns: 3, Rows: 1}, {Columns: 2, Rows: 2}}
	for _, d := range (Plugin{}).Describe().Widgets {
		if d.Widget != "battery" {
			continue
		}
		if !slices.Equal(d.Sizes, want) {
			t.Errorf("battery spans: %v, want %v", d.Sizes, want)
		}
		return
	}
	t.Fatal("the battery widget is not described")
}
