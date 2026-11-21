package paperboy

import (
	"image"
	"image/png"
	"net/http"
	"strings"
	"testing"
)

func TestNextBatteryLevel(t *testing.T) {
	b := &Battery{WarnVolts: 3.45, CriticalVolts: 3.3}
	if err := b.validate("test"); err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		volts, want string
	}{
		{"3.75", batteryOK},
		{"3.44", batteryWarn},
		{"3.50", batteryWarn}, // within hysteresis
		{"", batteryWarn},     // no reading keeps the stage
		{"-1", batteryWarn},
		{"abc", batteryWarn},
		{"3.29", batteryCritical},
		{"3.35", batteryCritical}, // within hysteresis of critical
		{"3.41", batteryWarn},
		{"3.56", batteryOK},
		{"3.20", batteryCritical}, // drops straight to critical
		{"4.10", batteryOK},       // charged
	}
	level := batteryOK
	for _, s := range steps {
		level = nextBatteryLevel(b, level, s.volts)
		if level != s.want {
			t.Fatalf("%s V: level %q, want %q", s.volts, level, s.want)
		}
	}
	if nextBatteryLevel(nil, batteryWarn, "3.0") != batteryOK {
		t.Fatal("no warning without configuration")
	}
}

func TestBatteryConfig(t *testing.T) {
	if err := (&Battery{WarnVolts: 3.3, CriticalVolts: 3.45}).validate("x"); err == nil {
		t.Fatal("critical threshold above warn threshold must be rejected")
	}
}

func TestBatteryWarningFlow(t *testing.T) {
	s, _ := planServer(t, `rules:
  - name: tag
    pages: [eins, zwei]
`)
	s.config().Battery = &Battery{WarnVolts: 3.45, CriticalVolts: 3.3, HysteresisVolts: 0.1}
	dev := planDevice(t, s)
	setVolts := func(v float64) { dev.req.Battery = &syncBattery{Volts: v} }
	setVolts(3.74)
	first := pagePath(displayPage(t, s, dev))
	if strings.HasPrefix(first, "/hints/") || blackRow(t, s, first) > 0 {
		t.Fatalf("full battery must show the plain page: %s", first)
	}

	setVolts(3.40)
	warn := pagePath(displayPage(t, s, dev))
	if warn == first || !strings.Contains(warn, "/eins/") {
		t.Fatalf("warn level must add the bar to the page: %s (%s)", warn, first)
	}
	if blackRow(t, s, warn) < 10 {
		t.Fatal("battery bar missing")
	}
	if next := pagePath(press(t, s, dev, buttonNext)); !strings.Contains(next, "/zwei/") {
		t.Fatalf("warn level must keep paging: %s", next)
	}

	setVolts(3.25)
	var critical string
	for range 2 { // buttons do not page away from the critical screen
		critical = pagePath(press(t, s, dev, buttonNext))
		if !strings.HasPrefix(critical, "/hints/") {
			t.Fatalf("critical level must show a full-screen notice: %s", critical)
		}
	}
	if blackPixels(t, s, critical) == 0 {
		t.Fatal("full screen empty")
	}
	if info := deviceInfoOf(t, s); info["last_page_kind"] != "battery" || info["battery_level"] != "critical" {
		t.Fatalf("critical level in the devices API: %v", info)
	}

	setVolts(4.12)
	if name := pagePath(displayPage(t, s, dev)); strings.HasPrefix(name, "/hints/") || !strings.Contains(name, "/eins/") {
		t.Fatalf("after charging the normal page must return: %s", name)
	}
}

// blackRow counts black pixels in the bottom row of a served image (banner).
func blackRow(t *testing.T, s *Server, imageURL string) int {
	t.Helper()
	img := decodeImage(t, s, imageURL)
	b := img.Bounds()
	black := 0
	for x := b.Min.X; x < b.Max.X; x++ {
		if r, _, _, _ := img.At(x, b.Max.Y-1).RGBA(); r == 0 {
			black++
		}
	}
	return black
}

func blackPixels(t *testing.T, s *Server, imageURL string) int {
	t.Helper()
	img := decodeImage(t, s, imageURL)
	b := img.Bounds()
	black := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if r, _, _, _ := img.At(x, y).RGBA(); r == 0 {
				black++
			}
		}
	}
	return black
}

func decodeImage(t *testing.T, s *Server, imageURL string) image.Image {
	t.Helper()
	rec := fetchImage(t, s, imageURL)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: %d", imageURL, rec.Code)
	}
	img, err := png.Decode(rec.Body)
	if err != nil {
		t.Fatalf("%s: %v", imageURL, err)
	}
	return img
}
