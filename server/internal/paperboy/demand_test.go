package paperboy

import (
	"testing"
	"time"
)

// demandFixture writes a plan with one page that uses weather and the built-in
// header, plus a dexcom trigger.
func demandFixture(t *testing.T, s *Server) {
	t.Helper()
	writePlan(t, s, "plan", `pages: [seite]
triggers:
  - when: { value: dexcom/state, is: low }
    page: seite
`)
	writePage(t, s, "seite", "", `kind: image
grid: [2, 1]
cells:
  - { at: [0, 0], span: [1, 1], widget: weather/current }
  - { at: [1, 0], span: [1, 1], widget: paperboy/header }
`)
}

func TestPluginDemand(t *testing.T) {
	s, now := testServer(t)
	cfg := s.config()
	cfg.Devices[0].Plan = "plan"
	demandFixture(t, s)
	key := cfg.Devices[0].stateKey()
	s.state[key] = &deviceState{LastSeen: *now, LastRefresh: 1800}

	demand := s.pluginDemand()
	want := now.Add(30 * time.Minute)
	if len(demand) != 2 {
		t.Fatalf("demand = %v, want weather and dexcom", demand)
	}
	for _, name := range []string{"weather", "dexcom"} {
		if at, ok := demand[name]; !ok || !at.Equal(want) {
			t.Errorf("%s = %v, %v; want %v", name, at, ok, want)
		}
	}
	if _, ok := demand["paperboy"]; ok {
		t.Error("the built-in plugin must add no demand")
	}
}

func TestPluginDemandLateAndNeverSeen(t *testing.T) {
	s, now := testServer(t)
	cfg := s.config()
	cfg.Devices[0].Plan = "plan"
	demandFixture(t, s)
	key := cfg.Devices[0].stateKey()

	// 59 s past the expected contact: still inside the one-minute grace.
	s.state[key] = &deviceState{LastSeen: now.Add(-(1800 + 59) * time.Second), LastRefresh: 1800}
	if demand := s.pluginDemand(); len(demand) != 2 {
		t.Fatalf("within the grace: %v", demand)
	}
	// 61 s past it: the device is late and adds no demand.
	s.state[key].LastSeen = now.Add(-(1800 + 61) * time.Second)
	if demand := s.pluginDemand(); len(demand) != 0 {
		t.Fatalf("a late device still creates demand: %v", demand)
	}
	// Never seen: no demand. The pages of a plan are static, so a device
	// that has synced keeps its demand even before a trigger matches.
	s.state[key] = &deviceState{}
	if demand := s.pluginDemand(); len(demand) != 0 {
		t.Fatalf("a never-seen device creates demand: %v", demand)
	}
	s.state[key] = &deviceState{LastSeen: *now, LastRefresh: 1800}
	if demand := s.pluginDemand(); len(demand) != 2 {
		t.Fatalf("a synced device must create demand: %v", demand)
	}
}

func TestPluginDemandEarliest(t *testing.T) {
	s, now := testServer(t)
	cfg := s.config()
	cfg.Devices[0].Plan = "plan"
	demandFixture(t, s)
	s.state[cfg.Devices[0].stateKey()] = &deviceState{LastSeen: *now, LastRefresh: 1800}

	second := Device{ID: "zwei", MAC: "aa:bb:cc:dd:ee:02", Profile: "small", Plan: "plan",
		Refresh: Duration(10 * time.Minute)}
	cfg.Devices = append(cfg.Devices, second)
	s.state[second.stateKey()] = &deviceState{LastSeen: now.Add(-10 * time.Minute), LastRefresh: 600}

	demand := s.pluginDemand()
	if at := demand["weather"]; !at.Equal(*now) {
		t.Fatalf("the earliest contact wins: %v", demand)
	}
}

func TestPluginDemandTextValues(t *testing.T) {
	s, now := testServer(t)
	cfg := s.config()
	cfg.Profiles["lcd"] = Profile{Kind: kindText, Charset: "ascii", Width: 20, Height: 4}
	text := Device{ID: "lcd", MAC: "aa:bb:cc:dd:ee:03", Profile: "lcd", Plan: "textplan",
		Refresh: Duration(time.Second)}
	cfg.Devices = append(cfg.Devices, text)
	writePlan(t, s, "textplan", "pages: [textseite]\n")
	writePage(t, s, "textseite", "", `kind: text
cells:
  - { at: [0, 0], span: [20, 1], value: evcc/pv }
`)
	s.state[text.stateKey()] = &deviceState{LastSeen: *now, LastRefresh: 1}

	demand := s.pluginDemand()
	want := now.Add(time.Second)
	if at := demand["evcc"]; !at.Equal(want) {
		t.Fatalf("text value demand = %v, want %v", demand, want)
	}
	if _, ok := demand["paperboy"]; ok {
		t.Error("the built-in plugin must add no demand")
	}
}

func TestPluginDemandSkipsPagesThatDoNotFit(t *testing.T) {
	s, now := testServer(t)
	cfg := s.config()
	cfg.Devices[0].Plan = "plan"
	writePlan(t, s, "plan", "pages: [textseite]\n")
	writePage(t, s, "textseite", "", `kind: text
cells:
  - { at: [0, 0], span: [20, 1], value: evcc/pv }
`)
	s.state[cfg.Devices[0].stateKey()] = &deviceState{LastSeen: *now, LastRefresh: 1800}
	if demand := s.pluginDemand(); len(demand) != 0 {
		t.Fatalf("a text page on an image display creates demand: %v", demand)
	}
}
