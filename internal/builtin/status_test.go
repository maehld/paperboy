package builtin

import (
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
