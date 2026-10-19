package displayd

import (
	"image/png"
	"net/http"
	"net/http/httptest"
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
			t.Fatalf("%s V: Stufe %q, erwartet %q", s.volts, level, s.want)
		}
	}
	if nextBatteryLevel(nil, batteryWarn, "3.0") != batteryOK {
		t.Fatal("ohne Konfiguration keine Warnung")
	}
}

func TestBatteryConfig(t *testing.T) {
	if err := (&Battery{WarnVolts: 3.3, CriticalVolts: 3.45}).validate("x"); err == nil {
		t.Fatal("kritische Schwelle über Warnschwelle muss abgelehnt werden")
	}
}

func TestBatteryWarningFlow(t *testing.T) {
	s, _ := testServer(t)
	s.cfg.Battery = &Battery{WarnVolts: 3.45, CriticalVolts: 3.3, HysteresisVolts: 0.1}
	upload(t, s, "0", 255)
	upload(t, s, "1", 255)
	key := call(t, s, "/api/setup", map[string]string{"ID": "AA:BB:CC:DD:EE:01"})["api_key"].(string)
	dev := map[string]string{"ID": "AA:BB:CC:DD:EE:01", "Access-Token": key, "Update-Source": "timer", "Battery-Voltage": "3.74"}
	if name := call(t, s, "/api/display", dev)["filename"].(string); strings.Contains(name, "low") {
		t.Fatalf("voller Akku ohne Warnung: %s", name)
	}

	dev["Battery-Voltage"] = "3.40"
	name := call(t, s, "/api/display", dev)["filename"].(string)
	if !strings.HasSuffix(name, "-low34") {
		t.Fatalf("Warnstufe muss Dateinamen ändern: %s", name)
	}
	if blackRow(t, s, name) < 10 {
		t.Fatal("Akkubalken fehlt")
	}
	dev["Update-Source"] = "EXT0"
	if name := call(t, s, "/api/display", dev)["filename"].(string); name[:7] != "small-1" {
		t.Fatalf("in der Warnstufe wird weiter geblättert: %s", name)
	}

	dev["Battery-Voltage"] = "3.25"
	for range 2 { // buttons do not page away from the critical screen
		name = call(t, s, "/api/display", dev)["filename"].(string)
		if name != "small-battery-33" {
			t.Fatalf("kritische Stufe muss Vollbild zeigen: %s", name)
		}
	}
	if blackRow(t, s, name) == 0 {
		t.Fatal("Vollbild leer")
	}

	dev["Battery-Voltage"] = "4.12"
	dev["Update-Source"] = "timer"
	if name := call(t, s, "/api/display", dev)["filename"].(string); name[:7] != "small-0" || strings.Contains(name, "low") {
		t.Fatalf("nach dem Laden wieder normale Übersicht: %s", name)
	}
}

// blackRow fetches an image and counts black pixels in its bottom row
// (banner) or, if none, anywhere.
func blackRow(t *testing.T, s *Server, name string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/images/"+name+".png", nil))
	img, err := png.Decode(rec.Body)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	b := img.Bounds()
	black := 0
	for y := b.Max.Y - 1; y >= b.Min.Y && black == 0; y-- {
		for x := b.Min.X; x < b.Max.X; x++ {
			if r, _, _, _ := img.At(x, y).RGBA(); r == 0 {
				black++
			}
		}
	}
	return black
}
