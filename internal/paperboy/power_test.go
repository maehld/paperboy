package paperboy

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestPowerFloor covers the refresh floor per effective power source
// (ADR 0051): auto with and without a report, explicit settings on the
// display type and the device, and min_refresh.
func TestPowerFloor(t *testing.T) {
	cfg := &Config{Timezone: time.UTC, Language: "en",
		Profiles: map[string]Profile{
			"epaper":  {Width: 800, Height: 480, PPI: 125},
			"slow":    {Width: 800, Height: 480, PPI: 125, MinRefresh: Duration(2 * time.Minute)},
			"fast":    {Width: 800, Height: 480, PPI: 125, MinRefresh: Duration(5 * time.Second)},
			"lcd":     {Kind: kindText, Width: 20, Height: 4},
			"usb":     {Width: 800, Height: 480, PPI: 125, Power: powerMains},
			"charged": {Kind: kindText, Width: 20, Height: 4, Power: powerBattery},
		}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		profile, device, reported string
		power                     string
		floor                     time.Duration
	}{
		{"epaper", "", "", powerBattery, time.Minute},                // old firmware: battery
		{"epaper", "", powerBattery, powerBattery, time.Minute},      // reported battery
		{"epaper", "", powerMains, powerMains, 10 * time.Second},     // reported mains: image default
		{"lcd", "", "", powerBattery, time.Minute},                   // text without report: battery floor
		{"lcd", "", powerMains, powerMains, time.Second},             // text on mains: one second
		{"slow", "", powerMains, powerMains, 2 * time.Minute},        // min_refresh wins on mains
		{"slow", "", "", powerBattery, 2 * time.Minute},              // and on battery above one minute
		{"fast", "", "", powerBattery, time.Minute},                  // battery never below one minute
		{"fast", "", powerMains, powerMains, 5 * time.Second},        // mains goes down to min_refresh
		{"usb", "", powerBattery, powerMains, 10 * time.Second},      // display type overrides the report
		{"charged", "", powerMains, powerBattery, time.Minute},       // display type battery wins too
		{"usb", powerBattery, powerMains, powerBattery, time.Minute}, // device overrides the display type
		{"charged", powerMains, "", powerMains, time.Second},         // device mains on a battery type
		{"usb", powerAuto, "", powerBattery, time.Minute},            // device auto ignores the type
		{"usb", powerAuto, powerMains, powerMains, 10 * time.Second}, // and follows the report
		{"epaper", powerMains, powerBattery, powerMains, mainsImageMinRefresh},
	} {
		d := &Device{ID: "x", Profile: tc.profile, Power: tc.device}
		if got := cfg.power(d, tc.reported); got != tc.power {
			t.Errorf("%s/%q/%q: power %s, want %s", tc.profile, tc.device, tc.reported, got, tc.power)
		}
		if got := cfg.minRefresh(d, tc.reported); got != tc.floor {
			t.Errorf("%s/%q/%q: floor %s, want %s", tc.profile, tc.device, tc.reported, got, tc.floor)
		}
	}
}

func TestPowerConfigValidation(t *testing.T) {
	base := "display_types:\n  e:\n    size: [800, 480]\n    ppi: 125\n%s\n"
	device := "display_type: e\nmac: \"44:1B:F6:85:AA:1C\"\n%s\n"
	for _, tc := range []struct {
		typ, dev, err string
	}{
		{"    power: mains", "power: battery", ""},
		{"    power: auto", "refresh: 5s", ""},
		{"    power: solar", "", "power \"solar\""},
		{"", "power: usb", "power \"usb\""},
		{"", "refresh: 500ms", "refresh must be at least 1s"},
		{"    min_refresh: 500ms", "", "min_refresh must be at least 1s"},
	} {
		text := strings.Replace(base, "%s", tc.typ, 1)
		devices := map[string]string{"d": strings.Replace(device, "%s", tc.dev, 1)}
		cfg, _, err := loadConfigWith(t, text, devices)
		switch {
		case tc.err == "" && err != nil:
			t.Errorf("%q %q: %v", tc.typ, tc.dev, err)
		case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
			t.Errorf("%q %q: error %v, want %q", tc.typ, tc.dev, err, tc.err)
		case tc.err == "" && cfg.Profiles["e"].Power == "":
			t.Errorf("%q: power not defaulted", tc.typ)
		}
	}
}

const fastPlan = `
rules:
  - name: immer
    when: { from: "00:00", to: "23:59" }
    pages: [eins]
    refresh: 5s
fallback: immer
`

// TestSyncPowerReport checks that the reported power source reaches the
// floor, the sleep flag, the state and the devices API, and that an explicit
// device setting wins.
func TestSyncPowerReport(t *testing.T) {
	s, _ := planServer(t, fastPlan)
	dev := planDevice(t, s)

	out := displayPage(t, s, dev)
	if out["refresh_s"].(float64) != 60 || out["sleep"] != true {
		t.Fatalf("old firmware: %v", out)
	}

	dev.req.Power = powerMains
	out = displayPage(t, s, dev)
	if out["refresh_s"].(float64) != 10 || out["sleep"] != false {
		t.Fatalf("mains: %v", out)
	}
	// The report sticks: a later sync without it keeps the mains floor.
	dev.req.Power = ""
	out = displayPage(t, s, dev)
	if out["refresh_s"].(float64) != 10 || out["sleep"] != false {
		t.Fatalf("remembered mains: %v", out)
	}
	info := call(t, s, "/api/devices", nil)["devices"].([]any)[0].(map[string]any)
	if info["reported_power"] != powerMains || info["power"] != powerMains {
		t.Fatalf("devices API: %v", info)
	}

	s.config().Devices[0].Power = powerBattery
	out = displayPage(t, s, dev)
	if out["refresh_s"].(float64) != 60 || out["sleep"] != true {
		t.Fatalf("device battery: %v", out)
	}
}

func TestSyncRejectsUnknownPower(t *testing.T) {
	s, _ := planServer(t, fastPlan)
	dev := planDevice(t, s)
	rec := syncRaw(t, s, dev.key, `{"reason":"timer","power":"auto"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("sync with power auto: %d", rec.Code)
	}
	rec = register(t, s, `{"mac":"`+planDeviceMAC+`","power":"solar"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("register with power solar: %d", rec.Code)
	}
	rec = register(t, s, `{"mac":"`+planDeviceMAC+`","power":"mains"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("register with power mains: %d", rec.Code)
	}
	info := call(t, s, "/api/devices", nil)["devices"].([]any)[0].(map[string]any)
	if info["reported_power"] != powerMains {
		t.Fatalf("register report: %v", info)
	}
}

// TestUnchangedPageReused checks that composing the same page again keeps the
// stored PNG and hash, so a short interval causes no redraw (ADR 0051).
func TestUnchangedPageReused(t *testing.T) {
	s, _ := planServer(t, fastPlan)
	dev := planDevice(t, s)
	dev.req.Power = powerMains
	first := displayPage(t, s, dev)
	s.mu.Lock()
	rp := s.rendered["small/eins"]
	s.mu.Unlock()
	second := displayPage(t, s, dev)
	s.mu.Lock()
	again := s.rendered["small/eins"]
	s.mu.Unlock()
	if rp == nil || rp != again {
		t.Fatal("unchanged page was encoded again")
	}
	if first["image"].(map[string]any)["hash"] != second["image"].(map[string]any)["hash"] {
		t.Fatalf("hash changed: %v %v", first["image"], second["image"])
	}
}
