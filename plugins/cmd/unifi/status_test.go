package main

import (
	"testing"
)

func online(name string) device {
	return device{Name: name, State: "ONLINE"}
}

func int64Ptr(v int64) *int64 { return &v }

func TestStateKey(t *testing.T) {
	wanOK := healthSubsystem{Status: "ok", ISPName: "Deutsche Telekom AG"}
	wanDown := healthSubsystem{Status: "down"}
	wwwOK := healthSubsystem{Status: "ok"}
	wwwDown := healthSubsystem{Status: "down"}

	cases := []struct {
		name       string
		snap       snapshot
		wanEnabled bool
		want       string
	}{
		{"not loaded", snapshot{}, true, ""},
		{"all fine", snapshot{loaded: true, wan: wanOK, www: wwwOK, devices: []device{online("AP")}}, true, "ok"},
		{"wan down", snapshot{loaded: true, wan: wanDown, www: wwwOK, devices: []device{online("AP")}}, true, "degraded"},
		{"wan down disabled", snapshot{loaded: true, wan: wanDown, www: wwwOK, devices: []device{online("AP")}}, false, "ok"},
		{"www down", snapshot{loaded: true, wan: wanOK, www: wwwDown, devices: []device{online("AP")}}, false, "degraded"},
		{"offline device", snapshot{loaded: true, wan: wanOK, www: wwwOK,
			devices: []device{online("AP"), {Name: "Keller-AP", State: "OFFLINE"}}}, true, "degraded"},
		{"firmware update", snapshot{loaded: true, wan: wanOK, www: wwwOK,
			devices: []device{{Name: "AP", State: "ONLINE", FirmwareUpdatable: true}}}, true, "attention"},
		{"updating", snapshot{loaded: true, wan: wanOK, www: wwwOK,
			devices: []device{{Name: "AP", State: "UPDATING"}}}, true, "attention"},
		{"getting ready", snapshot{loaded: true, wan: wanOK, www: wwwOK,
			devices: []device{{Name: "AP", State: "GETTING_READY"}}}, true, "attention"},
		{"offline wins over update", snapshot{loaded: true, wan: wanOK, www: wwwOK, devices: []device{
			{Name: "A", State: "OFFLINE"},
			{Name: "B", State: "ONLINE", FirmwareUpdatable: true},
		}}, true, "degraded"},
		{"empty wan status", snapshot{loaded: true, wan: healthSubsystem{}, www: wwwOK,
			devices: []device{online("AP")}}, true, "ok"},
		{"isolation", snapshot{loaded: true, wan: wanOK, www: wwwOK,
			devices: []device{online("AP"), {Name: "AP2", State: "ISOLATED"}}}, true, "degraded"},
	}
	for _, c := range cases {
		if got := c.snap.stateKey(c.wanEnabled); got != c.want {
			t.Errorf("%s: state = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestStatusLineOrder(t *testing.T) {
	base := snapshot{
		loaded: true,
		wan:    healthSubsystem{Status: "ok", ISPName: "Deutsche Telekom AG"},
		www:    healthSubsystem{Status: "ok", Latency: int64Ptr(10)},
	}

	// An offline device wins over firmware updates and the WAN summary.
	snap := base
	snap.devices = []device{
		{Name: "Keller-AP", State: "OFFLINE"},
		{Name: "OG-AP", State: "ONLINE", FirmwareUpdatable: true},
	}
	de := func(de, en string) string { return de }
	if got := snap.statusLine(true, de); got != "Keller-AP offline" {
		t.Errorf("offline line = %q, want %q", got, "Keller-AP offline")
	}

	// Then the number of firmware updates.
	snap = base
	snap.devices = []device{{Name: "OG-AP", State: "ONLINE", FirmwareUpdatable: true}}
	if got := snap.statusLine(true, de); got != "1 Firmware-Update" {
		t.Errorf("one update line = %q", got)
	}
	en := func(de, en string) string { return en }
	if got := snap.statusLine(true, en); got != "1 firmware update" {
		t.Errorf("one update line (en) = %q", got)
	}
	snap.devices = []device{
		{Name: "A", State: "ONLINE", FirmwareUpdatable: true},
		{Name: "B", State: "UPDATING"},
	}
	if got := snap.statusLine(true, de); got != "2 Firmware-Updates" {
		t.Errorf("two updates line = %q", got)
	}
	if got := snap.statusLine(true, en); got != "2 firmware updates" {
		t.Errorf("two updates line (en) = %q", got)
	}

	// Then the WAN summary.
	snap = base
	snap.devices = []device{online("AP")}
	if got := snap.statusLine(true, de); got != "Deutsche Telekom AG - 10 ms" {
		t.Errorf("wan line = %q", got)
	}

	// Without the WAN there is no summary, and nothing wrong means nothing.
	if got := snap.statusLine(false, de); got != "" {
		t.Errorf("wan line with wan off = %q, want empty", got)
	}
	snap = snapshot{loaded: true, devices: []device{online("AP")}}
	if got := snap.statusLine(true, de); got != "" {
		t.Errorf("nothing wrong = %q, want empty", got)
	}
}

func TestSignatureCoversISPName(t *testing.T) {
	base := snapshot{
		loaded: true, site: siteInfo{ID: "site"},
		wan: healthSubsystem{Status: "ok", ISPName: "Telekom"},
		www: healthSubsystem{Status: "ok", Latency: int64Ptr(10)},
	}
	other := base
	other.wan.ISPName = "Vodafone"
	if base.signature() == other.signature() {
		t.Error("the signature ignores the ISP name of the WAN line")
	}
}

func TestOfflineCount(t *testing.T) {
	snap := snapshot{loaded: true, devices: []device{
		online("A"),
		{Name: "B", State: "OFFLINE"},
		{Name: "C", State: "CONNECTION_INTERRUPTED"},
		{Name: "D", State: "UPDATING"},
		{Name: "E", State: "GETTING_READY"},
	}}
	if got := snap.offline(); got != 2 {
		t.Errorf("offline = %d, want 2", got)
	}
}

func TestSpeedNeedsASpeedtest(t *testing.T) {
	var sn snapshot
	if sn.speed(true) != nil || sn.speed(false) != nil {
		t.Fatal("a snapshot without a speedtest answers a rate")
	}
	last, down, up := int64(1795810000), 250.5, 50.2
	sn.www.SpeedtestLastRun, sn.www.XputDown, sn.www.XputUp = &last, &down, &up
	if v := sn.speed(true); v == nil || *v != down {
		t.Fatalf("download = %v, want %v", v, down)
	}
	if v := sn.speed(false); v == nil || *v != up {
		t.Fatalf("upload = %v, want %v", v, up)
	}
	zero := int64(0)
	sn.www.SpeedtestLastRun = &zero
	if sn.speed(true) != nil {
		t.Fatal("a zero lastrun answers a rate")
	}
}

func TestWANState(t *testing.T) {
	sn := snapshot{loaded: true,
		wan: healthSubsystem{Status: "ok"}, www: healthSubsystem{Status: "ok"}}
	if got := sn.wanStateKey(true); got != wanOK {
		t.Errorf("healthy = %q, want ok", got)
	}
	sn.www.Status = "warning"
	if got := sn.wanStateKey(true); got != wanWarning {
		t.Errorf("warning = %q", got)
	}
	sn.www.Status = "error"
	if got := sn.wanStateKey(true); got != wanError {
		t.Errorf("internet error = %q", got)
	}
	sn.www.Status, sn.wan.Status = "ok", "error"
	if got := sn.wanStateKey(true); got != wanError {
		t.Errorf("wan error = %q", got)
	}
	if got := sn.wanStateKey(false); got != "" {
		t.Errorf("wan off = %q, want empty", got)
	}
	if got := (snapshot{}).wanStateKey(true); got != "" {
		t.Errorf("no data = %q, want empty", got)
	}
}
