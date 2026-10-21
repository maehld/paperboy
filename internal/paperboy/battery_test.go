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
	s, _ := planServer(t, `timezone: UTC
rules:
  - name: tag
    pages: [eins, zwei]
`)
	s.cfg.Battery = &Battery{WarnVolts: 3.45, CriticalVolts: 3.3, HysteresisVolts: 0.1}
	dev := planDevice(t, s)
	dev["Battery-Voltage"] = "3.74"
	first := pagePath(call(t, s, "/api/display", dev))
	if strings.HasPrefix(first, "hints/") || blackRow(t, s, "http://example.com/"+first+".png") > 0 {
		t.Fatalf("full battery must show the plain page: %s", first)
	}

	dev["Battery-Voltage"] = "3.40"
	warn := pagePath(call(t, s, "/api/display", dev))
	if warn == first || !strings.Contains(warn, "/eins/") {
		t.Fatalf("warn level must add the bar to the page: %s (%s)", warn, first)
	}
	if blackRow(t, s, "http://example.com/"+warn+".png") < 10 {
		t.Fatal("battery bar missing")
	}
	dev["Update-Source"] = "EXT0"
	if next := pagePath(call(t, s, "/api/display", dev)); !strings.Contains(next, "/zwei/") {
		t.Fatalf("warn level must keep paging: %s", next)
	}

	dev["Battery-Voltage"] = "3.25"
	var critical string
	for range 2 { // buttons do not page away from the critical screen
		critical = pagePath(call(t, s, "/api/display", dev))
		if !strings.HasPrefix(critical, "hints/") {
			t.Fatalf("critical level must show a full-screen notice: %s", critical)
		}
	}
	if blackPixels(t, s, "http://example.com/"+critical+".png") == 0 {
		t.Fatal("full screen empty")
	}

	dev["Battery-Voltage"] = "4.12"
	dev["Update-Source"] = "timer"
	if name := pagePath(call(t, s, "/api/display", dev)); strings.HasPrefix(name, "hints/") || !strings.Contains(name, "/eins/") {
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
