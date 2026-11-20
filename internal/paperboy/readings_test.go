package paperboy

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// at returns a pointer to a measured value, as a sync reading carries it.
func at(v float64) *float64 { return &v }

// deviceStateOf returns the in-memory state of the test device.
func deviceStateOf(t *testing.T, s *Server) *deviceState {
	t.Helper()
	st := s.state[s.config().Devices[0].stateKey()]
	if st == nil {
		t.Fatal("no device state")
	}
	return st
}

// TestSyncReadings checks that a sync stores the measured values with their
// time and that the dashboard names them (ADR 0134).
func TestSyncReadings(t *testing.T) {
	s, now := planServer(t, testPlan)
	dev := planDevice(t, s)
	dev.req.Readings = []syncReading{
		{Name: "humidity", Value: at(45.2)},
		{Name: "temperature", Value: at(21.53)},
	}
	displayPage(t, s, dev)

	st := deviceStateOf(t, s)
	temp := st.Readings["temperature"]
	if temp.Value != 21.53 || !temp.At.Equal(*now) {
		t.Fatalf("temperature: %+v", temp)
	}
	if hum := st.Readings["humidity"]; hum.Value != 45.2 {
		t.Fatalf("humidity: %+v", hum)
	}

	body := htmlRequest(t, s, "/ui").Body.String()
	for _, want := range []string{
		"<dt>Readings</dt>",
		`title="temperature, humidity"`,
		"21.5 °C · 45 %",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard misses %q: %s", want, body)
		}
	}

	// The state file keeps the readings across a restart.
	restarted, err := loadDeviceStates(filepath.Join(s.config().Dir, "state", "devices"))
	if err != nil {
		t.Fatal(err)
	}
	saved := restarted[strings.ToUpper(strings.ReplaceAll(planDeviceMAC, ":", ""))]
	if saved == nil || saved.Readings["temperature"].Value != 21.53 ||
		!saved.Readings["temperature"].At.Equal(*now) {
		t.Fatalf("state round trip: %+v", saved)
	}
}

// TestSyncReadingsDiscard checks that a reading the catalog does not know, a
// missing value and an implausible number neither fail the sync nor replace the
// last good value (ADR 0134).
func TestSyncReadingsDiscard(t *testing.T) {
	s, now := planServer(t, testPlan)
	dev := planDevice(t, s)
	dev.req.Readings = []syncReading{{Name: "temperature", Value: at(21.5)}}
	displayPage(t, s, dev)
	first := deviceStateOf(t, s).Readings["temperature"]

	*now = now.Add(10 * time.Minute)
	dev.req.Readings = []syncReading{
		{Name: "pressure", Value: at(1013)},     // unknown to the catalog
		{Name: "humidity"},                      // the measurement failed
		{Name: "temperature", Value: at(200.0)}, // outside the sensor range
	}
	if out := displayPage(t, s, dev); out["image"] == nil {
		t.Fatalf("sync must still serve a page: %v", out)
	}

	st := deviceStateOf(t, s)
	if got := st.Readings["temperature"]; got != first {
		t.Fatalf("an implausible value must not replace the last one: %+v", got)
	}
	if _, ok := st.Readings["pressure"]; ok {
		t.Fatal("an unknown reading must not be stored")
	}
	if _, ok := st.Readings["humidity"]; ok {
		t.Fatal("a reading without a value must not be stored")
	}
}

// TestDashboardReadingsStale checks that a reading the last sync did not bring
// shows its age instead of looking fresh.
func TestDashboardReadingsStale(t *testing.T) {
	s, now := planServer(t, testPlan)
	dev := planDevice(t, s)
	dev.req.Readings = []syncReading{{Name: "temperature", Value: at(21.5)}}
	displayPage(t, s, dev)
	if body := htmlRequest(t, s, "/ui").Body.String(); strings.Contains(body, "since") {
		t.Fatalf("a fresh reading must carry no age: %s", body)
	}

	*now = now.Add(2 * time.Hour)
	dev.req.Readings = nil
	displayPage(t, s, dev)
	body := htmlRequest(t, s, "/ui").Body.String()
	for _, want := range []string{"21.5 °C", "since 2 h ago"} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard misses %q: %s", want, body)
		}
	}
}

// TestReadingsText checks the card's texts without a server.
func TestReadingsText(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	st := deviceState{
		LastSeen: now,
		Readings: map[string]deviceReading{
			"humidity":    {Value: 45.2, At: now},
			"temperature": {Value: 21.53, At: now},
		},
	}
	text, keys, since := readingsText(st, now)
	if text != "21.5 °C · 45 %" || keys != "temperature, humidity" || since != "" {
		t.Fatalf("fresh: %q %q %q", text, keys, since)
	}

	st.Readings["temperature"] = deviceReading{Value: 21.53, At: now.Add(-2 * time.Hour)}
	st.LastSeen = now
	text, _, since = readingsText(st, now)
	if text != "21.5 °C · 45 %" || since != "since 2 h ago" {
		t.Fatalf("stale: %q %q", text, since)
	}

	if text, keys, since := readingsText(deviceState{}, now); text != "" || keys != "" || since != "" {
		t.Fatalf("no readings: %q %q %q", text, keys, since)
	}
}
