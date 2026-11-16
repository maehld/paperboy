package builtin

import (
	"image"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
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

func TestDeviceStatusText(t *testing.T) {
	req := &plugin.Request{Language: "de", ServerStatus: testStatus()}
	lines := statusLines(req, plugin.Values{}, 20)
	if len(lines) != 1 || !strings.Contains(lines[0], "kueche") || !strings.Contains(lines[0], "84 %") {
		t.Fatalf("current device line: %q", lines)
	}
	if !strings.Contains(lines[0], "~12 d") || strings.Contains(lines[0], "jetzt") {
		t.Errorf("20 columns keep the runtime and drop the age: %q", lines[0])
	}

	lines = statusLines(req, plugin.Values{"devices": []any{"wohnzimmer", "flur", "fehlt"}}, 24)
	if len(lines) != 3 {
		t.Fatalf("selected lines: %q", lines)
	}
	// Unknown and never-seen devices sort before the last contact.
	if !strings.HasPrefix(lines[0], "? ") || !strings.HasPrefix(lines[1], "? ") {
		t.Errorf("stale and unknown first: %q", lines)
	}
	if !strings.Contains(lines[2], "wohnzimmer") || !strings.Contains(lines[2], "21 %") {
		t.Errorf("selected device line: %q", lines[2])
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

func TestHeaderBattery(t *testing.T) {
	req := &plugin.Request{Language: "de", Now: testStatusTime, Page: plugin.PageInfo{Title: "Seite"}, ServerStatus: testStatus()}
	with := plugin.WidgetRequest{Widget: "header", Width: 40, Args: map[string]any{"clock": false, "battery": true}}
	lines, err := (Plugin{}).Text(req, with)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lines[0], "84 %") {
		t.Errorf("header without battery: %q", lines[0])
	}
	without := plugin.WidgetRequest{Widget: "header", Width: 40, Args: map[string]any{"clock": false}}
	if lines, _ := (Plugin{}).Text(req, without); strings.Contains(lines[0], "84 %") {
		t.Errorf("header without the flag shows the battery: %q", lines[0])
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

// batteryInner is the area inside the outline of a gauge without a value.
func batteryInner(b image.Rectangle, status *plugin.BatteryStatus) (image.Rectangle, image.Rectangle) {
	icon := layoutBattery(b, "", render.Center).icon
	body, line, _ := batteryFrame(icon, status)
	return icon, body.Inset(line + 1)
}

func TestBatteryGaugeSegments(t *testing.T) {
	const w, h = 240, 80
	cell := image.Rect(0, 0, w, h).Inset(4)
	ink := func(percent float64) (int, image.Rectangle) {
		_, inner := batteryInner(cell, &plugin.BatteryStatus{Percent: &percent})
		img, _ := renderBattery(t, batteryRequest(&percent, "", false), w, h, map[string]any{"label": "none"})
		return inkCount(img, inner), inner
	}
	zero, inner := ink(0)
	low, _ := ink(4)
	mid, _ := ink(84)
	full, _ := ink(100)
	if zero != 0 {
		t.Errorf("0 %% draws %d pixels of fill", zero)
	}
	if low == 0 {
		t.Error("a low charge shows no segment")
	}
	if mid <= low || full <= mid {
		t.Errorf("the fill does not step up: %d, %d, %d", low, mid, full)
	}
	if want := inner.Dx() * inner.Dy(); full < want*4/5 {
		t.Errorf("100 %% fills %d of %d pixels", full, want)
	}
	// 84 % fills four of the five segments, not the whole area.
	if want := full * 4 / 5; mid < want*9/10 || mid > want*11/10 {
		t.Errorf("84 %% fills %d pixels, four segments are about %d", mid, want)
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
	status := &plugin.BatteryStatus{Percent: &percent, Charging: true}
	_, inner := batteryInner(cell, status)
	plain, _ := renderBattery(t, batteryRequest(&percent, "", false), w, h, map[string]any{"label": "none"})
	charging, _ := renderBattery(t, batteryRequest(&percent, "", true), w, h, map[string]any{"label": "none"})
	if inkCount(charging, inner) >= inkCount(plain, inner) {
		t.Error("the charging mark does not knock a bolt out of the fill")
	}
	// No band: the segments leave gaps of the outline's width, so a wider run
	// of empty columns would mean that the mark cut the fill in two.
	icon, _ := batteryInner(cell, status)
	_, line, _ := batteryFrame(icon, status)
	run := 0
	for x := inner.Min.X; x < inner.Max.X; x++ {
		if inkCount(charging, image.Rect(x, inner.Min.Y, x+1, inner.Max.Y)) == 0 {
			run++
			if run > line+1 {
				t.Fatalf("columns %d to %d are empty: the mark cuts a band", x-run+1, x)
			}
			continue
		}
		run = 0
	}
}

func TestBatteryMissingReading(t *testing.T) {
	zero := 0.0
	const w, h = 240, 80
	cell := image.Rect(0, 0, w, h).Inset(4)
	_, inner := batteryInner(cell, nil)
	empty, _ := renderBattery(t, batteryRequest(&zero, "", false), w, h, map[string]any{"label": "none"})
	missing, _ := renderBattery(t, batteryRequest(nil, "", false), w, h, map[string]any{"label": "none"})
	if inkCount(empty, inner) != 0 {
		t.Error("0 % fills the gauge")
	}
	if inkCount(missing, inner) == 0 {
		t.Error("a missing reading looks like an empty battery")
	}
}

func TestBatteryLevelMark(t *testing.T) {
	percent := 5.0
	const w, h = 240, 80
	cell := image.Rect(0, 0, w, h).Inset(4)
	thickness := func(level string) int {
		img, _ := renderBattery(t, batteryRequest(&percent, level, false), w, h, map[string]any{"label": "none"})
		body, _, _ := batteryFrame(layoutBattery(cell, "", render.Center).icon,
			&plugin.BatteryStatus{Percent: &percent, Level: level})
		y := body.Min.Y + body.Dy()/2
		n := 0
		for x := body.Min.X; x < body.Max.X; x++ {
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

func TestHeaderBatteryIconOnly(t *testing.T) {
	req := &plugin.Request{Language: "de", Now: testStatusTime, Page: plugin.PageInfo{Title: ""}, ServerStatus: testStatus()}
	w := plugin.WidgetRequest{Widget: "header", Width: 400, Height: 60, Style: plugin.Style{Rule: 1, TextPx: 16},
		Args: map[string]any{"title": "", "clock": false, "battery": true}}
	img := render.NewCanvas(w.Width, w.Height, 2)
	if err := (Plugin{}).Render(req, w, img); err != nil {
		t.Fatal(err)
	}
	// The rule of the header is the only other ink.
	box, ok := inkBox(img, image.Rect(0, 0, w.Width, w.Height-1))
	if !ok {
		t.Fatal("nothing drawn")
	}
	if box.Max.X != w.Width {
		t.Errorf("the gauge does not reach the right edge: %v", box)
	}
	if box.Max.Y != w.Height-1 {
		t.Errorf("the gauge does not sit on the text baseline: %v", box)
	}
	if box.Dx() > 2*box.Dy()+2 {
		t.Errorf("the header draws a value beside the gauge: ink %v", box)
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
