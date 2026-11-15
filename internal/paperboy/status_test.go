package paperboy

import (
	"testing"
	"time"

	"github.com/maehld/paperboy/internal/page"
	"github.com/maehld/paperboy/pkg/plugin"
)

func ptrFloat(v float64) *float64 { return &v }

func statusTestConfig() *Config {
	return &Config{
		Profiles: map[string]Profile{"small": {Kind: kindImage, Width: 400, Height: 300, Power: powerBattery}},
		Devices: []Device{
			{ID: "kueche", MAC: "AA:BB:CC:DD:EE:01", Profile: "small", Power: powerBattery, Refresh: Duration(5 * time.Minute)},
			{ID: "wohnzimmer", MAC: "AA:BB:CC:DD:EE:02", Profile: "small", Power: powerBattery, Refresh: Duration(10 * time.Minute)},
		},
	}
}

func TestStatusSnapshot(t *testing.T) {
	now := time.Date(2026, 11, 15, 9, 5, 0, 0, time.UTC)
	cfg := statusTestConfig()
	// stateKey strips the separators; build the key the same way.
	key, err := deviceStateKey(cfg.Devices[0].MAC)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{state: map[string]*deviceState{
		key: {
			APIKey:         "k",
			LastSeen:       now.Add(-30 * time.Second),
			BatteryVoltage: "3.75",
			BatteryPercent: ptrFloat(84),
			BatteryMA:      ptrFloat(-12),
			BatteryHistory: []batterySample{
				{Time: now.Add(-48 * time.Hour), Percent: ptrFloat(80)},
				{Time: now.Add(-24 * time.Hour), Percent: ptrFloat(70)},
				{Time: now, Percent: ptrFloat(60)},
			},
		},
	}}

	got := s.statusSnapshot(cfg, "kueche", []string{"wohnzimmer", "fehlt"}, now)
	if got.CurrentDeviceStatus == nil || got.CurrentDeviceStatus.ID != "kueche" {
		t.Fatalf("current: %+v", got.CurrentDeviceStatus)
	}
	if got.CurrentDeviceStatus.SeenS != 30 || got.CurrentDeviceStatus.Status != "ok" {
		t.Errorf("current state: %+v", got.CurrentDeviceStatus)
	}
	if got.CurrentDeviceStatus.Battery == nil || got.CurrentDeviceStatus.Battery.Percent == nil ||
		*got.CurrentDeviceStatus.Battery.Percent != 84 {
		t.Errorf("battery: %+v", got.CurrentDeviceStatus.Battery)
	}
	if got.CurrentDeviceStatus.RuntimeS <= 0 {
		t.Errorf("runtime missing: %+v", got.CurrentDeviceStatus)
	}
	if len(got.SelectedDevicesStatus) != 2 {
		t.Fatalf("selected: %+v", got.SelectedDevicesStatus)
	}
	if dev := got.SelectedDevicesStatus["wohnzimmer"]; dev.Status != "unregistered" {
		t.Errorf("selected device: %+v", dev)
	}
	if dev := got.SelectedDevicesStatus["fehlt"]; dev.Status != "unknown" || dev.SeenS != -1 {
		t.Errorf("unknown device: %+v", dev)
	}
}

func TestDeviceStatusOnMainsHasNoRuntime(t *testing.T) {
	now := time.Date(2026, 11, 15, 9, 5, 0, 0, time.UTC)
	cfg := statusTestConfig()
	d := &Device{ID: "kueche", MAC: "AA:BB:CC:DD:EE:01", Profile: "small", Power: powerMains, Refresh: Duration(5 * time.Minute)}
	st := deviceState{APIKey: "k", LastSeen: now, BatteryVoltage: "3.75",
		BatteryHistory: []batterySample{
			{Time: now.Add(-48 * time.Hour), Volts: "4.10"},
			{Time: now, Volts: "3.75"},
		}}
	got := statusOfDevice(cfg, d, st, now)
	if got.Power != powerMains || got.RuntimeS != 0 {
		t.Errorf("mains: %+v", got)
	}
}

func TestSelectedDeviceIDsAndScope(t *testing.T) {
	tmpl := &page.Template{Cells: []page.Cell{
		{Widget: "paperboy/device-status"},
		{Widget: "paperboy/device-status", Args: map[string]any{"devices": []any{"a", "b"}}},
		{Widget: "paperboy/battery", Args: map[string]any{"device": "c"}},
	}}
	got := selectedDeviceIDs(tmpl)
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("selected ids: %v", got)
	}
	if !deviceScoped(tmpl) {
		t.Error("the self-only device-status cell makes the page device-scoped")
	}

	selected := &page.Template{Cells: []page.Cell{
		{Widget: "paperboy/device-status", Args: map[string]any{"devices": []any{"a"}}},
		{Widget: "paperboy/battery", Args: map[string]any{"device": "a"}},
	}}
	if deviceScoped(selected) {
		t.Error("a page that names its devices is not device-scoped")
	}
	empty := &page.Template{Cells: []page.Cell{{Widget: "paperboy/battery", Args: map[string]any{"device": ""}}}}
	if !deviceScoped(empty) {
		t.Error("an empty device means the current device")
	}
	status := &plugin.ServerStatus{CurrentDeviceStatus: &plugin.DeviceStatus{ID: "kueche"}}
	if got := statusKey("small", "status", tmpl, status); got != "small/status/kueche" {
		t.Errorf("device-scoped cache key: %q", got)
	}
	if got := statusKey("small", "status", selected, status); got != "small/status" {
		t.Errorf("shared cache key: %q", got)
	}
}

func TestRenderedPagesByHash(t *testing.T) {
	s := &Server{rendered: map[string]*renderedPage{}, renderedHash: map[string]*renderedPage{}}
	a := &renderedPage{key: "small/eins/kueche", hash: "aaa"}
	b := &renderedPage{key: "small/eins/wohnzimmer", hash: "bbb"}
	s.storePage("small", "eins", a)
	s.storePage("small", "eins", b)
	if s.renderedPageLocked("small", "eins", "aaa") != a || s.renderedPageLocked("small", "eins", "bbb") != b {
		t.Fatal("both device renders must stay reachable by hash")
	}
	c := &renderedPage{key: "small/eins/kueche", hash: "ccc"}
	s.storePage("small", "eins", c)
	if s.renderedPageLocked("small", "eins", "aaa") != nil {
		t.Error("the replaced render of kueche must go")
	}
	if s.renderedPageLocked("small", "eins", "bbb") != b || s.renderedPageLocked("small", "eins", "ccc") != c {
		t.Error("the other device's render must stay")
	}
}
