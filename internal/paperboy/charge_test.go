package paperboy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestChargeEpisodeStartAndEnd checks that a charging episode opens with the
// first gauge current above the idle threshold and closes with the next one
// below it, keeping the charge of both ends (issue #205).
func TestChargeEpisodeStartAndEnd(t *testing.T) {
	s, now := planServer(t, testPlan)
	dev := planDevice(t, s)
	start := *now

	ma, percent := 568.0, 41.0
	dev.req.Battery = &syncBattery{Volts: 3.72, Percent: &percent, MA: &ma}
	displayPage(t, s, dev)

	key, err := deviceStateKey(planDeviceMAC)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	st := s.state[key]
	episode, last := st.Charge, st.LastCharge
	s.mu.Unlock()
	if episode == nil || !episode.Start.Equal(start) || !episode.End.IsZero() {
		t.Fatalf("running episode: %+v", episode)
	}
	if episode.From == nil || episode.From.Percent == nil || *episode.From.Percent != 41 {
		t.Fatalf("charge at the start: %+v", episode.From)
	}
	if last != nil {
		t.Fatalf("no episode has finished yet: %+v", last)
	}

	// A charge current that stays high keeps the same episode open.
	*now = now.Add(time.Hour)
	percent = 46
	displayPage(t, s, dev)
	s.mu.Lock()
	episode = s.state[key].Charge
	s.mu.Unlock()
	if episode == nil || !episode.Start.Equal(start) {
		t.Fatalf("an ongoing charge must keep its start: %+v", episode)
	}

	// The charger terminates: the episode ends with this sync's reading.
	*now = now.Add(time.Hour)
	ma, percent = 0, 50
	displayPage(t, s, dev)
	s.mu.Lock()
	st = s.state[key]
	episode, last = st.Charge, st.LastCharge
	s.mu.Unlock()
	if episode != nil {
		t.Fatalf("the episode must be closed: %+v", episode)
	}
	if last == nil || !last.Start.Equal(start) || !last.End.Equal(*now) {
		t.Fatalf("finished episode: %+v", last)
	}
	if last.From == nil || last.From.Percent == nil || *last.From.Percent != 41 {
		t.Fatalf("charge at the start: %+v", last.From)
	}
	if last.To == nil || last.To.Percent == nil || *last.To.Percent != 50 {
		t.Fatalf("charge at the end: %+v", last.To)
	}
	if got, want := chargeText(*st, s.now()), "just now · 41 → 50 %"; got != want {
		t.Fatalf("details line %q, want %q", got, want)
	}
}

// TestChargeEpisodeNeedsGaugeCurrent checks that only the fuel gauge current
// starts an episode: a charger state without a gauge tells nothing about how
// far the cell got.
func TestChargeEpisodeNeedsGaugeCurrent(t *testing.T) {
	now := time.Date(2026, time.October, 21, 8, 0, 0, 0, time.UTC)
	yes := true
	for _, b := range []*syncBattery{
		nil,
		{Volts: 3.9, Charging: &yes},
		{Volts: 3.9, MA: new(float64)},
	} {
		st := &deviceState{}
		trackCharge(st, b, now)
		if st.Charge != nil || st.LastCharge != nil {
			t.Fatalf("%+v must not open an episode", b)
		}
	}
}

// TestChargeText checks the details line: only what the hardware reported is
// shown, so a gauge without a percentage falls back to the voltage.
func TestChargeText(t *testing.T) {
	now := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)
	start, end := now.Add(-3*time.Hour), now.Add(-2*time.Hour)
	low, high := 41.0, 50.0
	episode := func(from, to *chargeReading) *chargeEpisode {
		return &chargeEpisode{Start: start, From: from, End: end, To: to}
	}
	for _, c := range []struct {
		name string
		st   deviceState
		want string
	}{
		{"percent", deviceState{LastCharge: episode(
			&chargeReading{Percent: &low, Volts: "3.72"},
			&chargeReading{Percent: &high, Volts: "4.02"})}, "2 h ago · 41 → 50 %"},
		{"voltage without a percentage", deviceState{LastCharge: episode(
			&chargeReading{Volts: "3.72"}, &chargeReading{Volts: "4.02"})}, "2 h ago · 3.72 → 4.02 V"},
		{"no reading", deviceState{LastCharge: episode(nil, nil)}, "2 h ago"},
		{"never charged", deviceState{}, ""},
	} {
		if got := chargeText(c.st, now); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// TestChargeEpisodeSurvivesRestart checks that an episode in the state file is
// still there after a restart, so a charge that ends while Paperboy is down is
// counted at the next sync.
func TestChargeEpisodeSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, time.October, 21, 8, 0, 0, 0, time.UTC)
	percent, ma := 41.0, 568.0
	st := &deviceState{APIKey: "key-1"}
	trackCharge(st, &syncBattery{Volts: 3.72, Percent: &percent, MA: &ma}, now)
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AABBCCDDEE01.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadDeviceStates(dir)
	if err != nil {
		t.Fatal(err)
	}
	restarted := loaded["AABBCCDDEE01"]
	if restarted == nil || restarted.Charge == nil || !restarted.Charge.Start.Equal(now) {
		t.Fatalf("episode after restart: %+v", restarted)
	}
	// The charge ended while the service was down: the next sync closes it.
	trackCharge(restarted, &syncBattery{Volts: 4.02, Percent: &percent, MA: new(float64)}, now.Add(6*time.Hour))
	if restarted.Charge != nil || restarted.LastCharge == nil || !restarted.LastCharge.End.Equal(now.Add(6*time.Hour)) {
		t.Fatalf("episode closed after restart: %+v %+v", restarted.Charge, restarted.LastCharge)
	}
}

// TestDashboardLastCharge checks that the card's details name the last charge.
func TestDashboardLastCharge(t *testing.T) {
	s, now := planServer(t, testPlan)
	dev := planDevice(t, s)
	ma, percent := 568.0, 41.0
	dev.req.Battery = &syncBattery{Volts: 3.72, Percent: &percent, MA: &ma}
	displayPage(t, s, dev)

	body := htmlRequest(t, s, "/ui").Body.String()
	if strings.Contains(body, "Last charge") {
		t.Fatalf("a running charge adds no line: %s", body)
	}

	*now = now.Add(2 * time.Hour)
	ma, percent = 0, 50
	displayPage(t, s, dev)
	body = htmlRequest(t, s, "/ui").Body.String()
	for _, want := range []string{"<dt>Last charge</dt>", "just now · 41 → 50 %"} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard misses %q: %s", want, body)
		}
	}
}
