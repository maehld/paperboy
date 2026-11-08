package main

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

func renderWidget(t *testing.T, m *myskoda, name string, width, height, levels int, args map[string]any) *image.Paletted {
	t.Helper()
	img := render.NewCanvas(width, height, levels)
	w := plugin.WidgetRequest{Widget: name, Width: width, Height: height, Args: args,
		Style: plugin.Style{Padding: 5, Rule: 2, TextPx: 16}}
	if err := m.Render(m.req, w, img); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return img
}

// ink is the share of pixels that are not white.
func ink(img *image.Paletted) float64 {
	white := uint8(len(img.Palette) - 1)
	marked := 0
	for _, p := range img.Pix {
		if p != white {
			marked++
		}
	}
	return float64(marked) / float64(len(img.Pix))
}

func TestRenderWidgets(t *testing.T) {
	for _, fixture := range []string{"enyaq.json", "partial.json", "motion.json"} {
		m := testPlugin(t, fixture)
		for _, name := range []string{"battery", "charging", "profiles", "location", "climate", "car"} {
			for _, size := range []image.Point{{X: 400, Y: 300}, {X: 200, Y: 120}, {X: 260, Y: 200}} {
				for _, levels := range []int{2, 4} {
					img := renderWidget(t, m, name, size.X, size.Y, levels, nil)
					if got := ink(img); got < 0.001 {
						t.Errorf("%s %s %v %d levels: nothing drawn", fixture, name, size, levels)
					}
				}
			}
		}
	}
}

func TestRenderCardWithoutImage(t *testing.T) {
	m := testPlugin(t, "partial.json")
	img := renderWidget(t, m, "card", 400, 300, 4, nil)
	if got := ink(img); got < 0.001 {
		t.Fatal("card without image drew nothing")
	}
}

func TestRenderCardWithImage(t *testing.T) {
	m := testPlugin(t, "enyaq.json")
	src := image.NewGray(image.Rect(0, 0, 120, 60))
	for y := 0; y < 60; y++ {
		for x := 0; x < 120; x++ {
			src.SetGray(x, y, color.Gray{Y: uint8(x * 2)})
		}
	}
	m.img = &supplement{img: src, url: m.cur.RenderURL}
	img := renderWidget(t, m, "card", 400, 300, 4, nil)
	if got := ink(img); got < 0.05 {
		t.Fatalf("card with image: ink = %.3f", got)
	}
}

func TestRenderCardArgs(t *testing.T) {
	m := testPlugin(t, "enyaq.json")
	withoutText := renderWidget(t, m, "card", 400, 300, 2, map[string]any{"name": false, "state": false})
	if got := ink(withoutText); got != 0 {
		t.Fatalf("card without text and image: ink = %.3f", got)
	}
}

func TestRenderUnknownWidget(t *testing.T) {
	m := testPlugin(t, "enyaq.json")
	img := render.NewCanvas(200, 100, 2)
	err := m.Render(m.req, plugin.WidgetRequest{Widget: "nope", Width: 200, Height: 100}, img)
	if err == nil {
		t.Fatal("unknown widget accepted")
	}
}

func TestTextWidgets(t *testing.T) {
	for _, fixture := range []string{"enyaq.json", "partial.json", "motion.json"} {
		m := testPlugin(t, fixture)
		for _, name := range []string{"battery", "charging", "profiles", "location", "climate", "car"} {
			lines, err := m.Text(m.req, plugin.WidgetRequest{Widget: name, Width: 20, Height: 4})
			if err != nil {
				t.Fatalf("%s/%s: %v", fixture, name, err)
			}
			if len(lines) == 0 {
				t.Errorf("%s/%s: no lines", fixture, name)
			}
			for _, line := range lines {
				if len([]rune(line)) > 20 {
					t.Errorf("%s/%s: %q is wider than 20", fixture, name, line)
				}
			}
		}
	}
}

// TestChargingDoesNotRepeatTheCable: the charging state already names whether
// the cable is connected, so no line repeats plugConnectionState.
func TestChargingDoesNotRepeatTheCable(t *testing.T) {
	for _, state := range []string{"CONNECT_CABLE", "READY_FOR_CHARGING", "CHARGING"} {
		m := testPlugin(t, "enyaq.json")
		s := state
		connected, locked := "CONNECTED", "LOCKED"
		m.cur.Charging.Status.State = &s
		m.cur.Charging.Status.PlugState = &connected
		m.cur.Charging.Status.PlugLock = &locked
		lines, err := m.Text(m.req, plugin.WidgetRequest{Widget: "charging", Width: 20, Height: 4})
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range lines {
			if strings.Contains(line, "Kabel") {
				t.Errorf("%s: line repeats the cable: %q", state, line)
			}
		}
	}
}

func TestTextBatteryContent(t *testing.T) {
	m := testPlugin(t, "enyaq.json")
	lines, err := m.Text(m.req, plugin.WidgetRequest{Widget: "battery", Width: 20, Height: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) < 3 {
		t.Fatalf("lines = %q", lines)
	}
	if got := lines[0]; got != "Akku            71 %" {
		t.Fatalf("line 0 = %q", got)
	}
	if got := lines[1]; got != "Lädt 20,2 kW  249 km" {
		t.Fatalf("line 1 = %q", got)
	}
}

func TestTextUnknownWidget(t *testing.T) {
	m := testPlugin(t, "enyaq.json")
	if _, err := m.Text(m.req, plugin.WidgetRequest{Widget: "nope", Width: 20, Height: 4}); err == nil {
		t.Fatal("unknown widget accepted")
	}
	if _, err := m.Text(m.req, plugin.WidgetRequest{Widget: "card", Width: 20, Height: 4}); err == nil {
		t.Fatal("card has no text variant")
	}
}

func TestTinyCellsDoNotPanic(t *testing.T) {
	m := testPlugin(t, "enyaq.json")
	for _, name := range []string{"battery", "charging", "profiles", "location", "climate", "card", "car"} {
		img := renderWidget(t, m, name, 12, 8, 2, nil)
		if got := ink(img); got != 0 {
			t.Errorf("%s: tiny cell drew something", name)
		}
	}
}

func TestProfilesEmpty(t *testing.T) {
	m := testPlugin(t, "enyaq.json")
	m.cur.ChargingProfiles = nil
	lines, err := m.Text(m.req, plugin.WidgetRequest{Widget: "profiles", Width: 20, Height: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0] != "Keine Ladepläne" {
		t.Fatalf("lines = %q", lines)
	}
	img := renderWidget(t, m, "profiles", 200, 100, 4, nil)
	if ink(img) == 0 {
		t.Fatal("empty profiles drew nothing")
	}
}

func TestProfileListLimit(t *testing.T) {
	m := testPlugin(t, "enyaq.json")
	if got := len(m.profileList(1)); got != 1 {
		t.Fatalf("max 1: %d", got)
	}
	if got := len(m.profileList(0)); got != 1 {
		t.Fatalf("max 0 clamps to 1: %d", got)
	}
	if got := len(m.profileList(99)); got != 2 {
		t.Fatalf("max 99: %d", got)
	}
}

func TestWrapAndCut(t *testing.T) {
	lines := wrapRunes("Prinzipalmarkt 1, 48143 Münster, Germany", 12, 2)
	if len(lines) != 2 {
		t.Fatalf("lines = %q", lines)
	}
	for _, line := range lines {
		if len([]rune(line)) > 12 {
			t.Fatalf("%q is too wide", line)
		}
	}
	if got := cut("abcdef", 4); got != "abc…" {
		t.Fatalf("cut = %q", got)
	}
	if got := cut("abc", 4); got != "abc" {
		t.Fatalf("cut = %q", got)
	}
}
