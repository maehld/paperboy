package paperboy

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// captureLog redirects the default logger into a buffer for one test.
func captureLog(t *testing.T) func() string {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return func() string { return buf.String() }
}

// logLines returns the log lines that mention a marker.
func logLines(out, marker string) []string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, marker) {
			lines = append(lines, line)
		}
	}
	return lines
}

// TestSyncLogUsesPageName checks that the sync log names the page instead of
// its index, so the log can be read without opening the plan.
func TestSyncLogUsesPageName(t *testing.T) {
	logs := captureLog(t)
	s, _ := planServer(t, "pages: [eins, zwei]\non_refresh: cycle\n")
	dev := planDevice(t, s)
	displayPage(t, s, dev)
	displayPage(t, s, dev)
	out := logs()
	lines := logLines(out, "display sync")
	line := lines[len(lines)-1]
	if !strings.Contains(line, "page=zwei") || !strings.Contains(line, "index=1") || !strings.Contains(line, "pages=2") {
		t.Fatalf("rotation page and its position must be visible: %q", line)
	}
	if !strings.Contains(out, "page=eins") {
		t.Fatalf("the first request must name its page: %q", out)
	}
	press(t, s, dev, buttonPrevious, buttonHome)
	if line := logLines(logs(), "display sync"); !strings.Contains(line[len(line)-1], "buttons=previous,home") {
		t.Fatalf("buttons must be logged: %q", line)
	}
}

// TestSyncLogShowsBatteryCurrent checks that the fuel gauge current is logged
// with its sign, so charging is visible in the journal (issue #199).
func TestSyncLogShowsBatteryCurrent(t *testing.T) {
	logs := captureLog(t)
	s, _ := planServer(t, testPlan)
	dev := planDevice(t, s)
	charging := 412.6
	wantsCharge := true
	dev.req.Battery = &syncBattery{Volts: 3.91, MA: &charging, Charging: &wantsCharge}
	displayPage(t, s, dev)
	lines := logLines(logs(), "display sync")
	if len(lines) == 0 || !strings.Contains(lines[len(lines)-1], "battery_ma=+413") ||
		!strings.Contains(lines[len(lines)-1], "charger=charging") {
		t.Fatalf("charge current and charger state must be logged: %q", lines)
	}
}

// TestSyncLogsPowerChange checks that a change of the power source or of the
// charger state gets one journal line instead of repeating it on every sync
// (issue #200).
func TestSyncLogsPowerChange(t *testing.T) {
	logs := captureLog(t)
	s, _ := planServer(t, testPlan)
	dev := planDevice(t, s)
	ma, percent := 0.0, 50.0
	dev.req.Power = powerMains
	dev.req.Battery = &syncBattery{Volts: 3.8, Percent: &percent, MA: &ma}
	displayPage(t, s, dev)
	lines := logLines(logs(), "power changed")
	if len(lines) != 1 || !strings.Contains(lines[0], "power=mains") {
		t.Fatalf("the first reading must be logged once: %v", lines)
	}

	// The same reading on the next sync adds nothing.
	displayPage(t, s, dev)
	if lines = logLines(logs(), "power changed"); len(lines) != 1 {
		t.Fatalf("an unchanged reading must not log again: %v", lines)
	}

	// The gauge starts pushing current: the charger state changed.
	ma = 568
	displayPage(t, s, dev)
	lines = logLines(logs(), "power changed")
	if len(lines) != 2 || !strings.Contains(lines[1], "charger=charging") || !strings.Contains(lines[1], "battery_ma=+568") {
		t.Fatalf("a charging start must be logged: %v", lines)
	}

	// It stops again.
	ma = 0
	displayPage(t, s, dev)
	lines = logLines(logs(), "power changed")
	if len(lines) != 3 {
		t.Fatalf("the charger stopping must be logged: %v", lines)
	}

	// Unplugged: the power source changes, the rest stays.
	dev.req.Power = powerBattery
	displayPage(t, s, dev)
	lines = logLines(logs(), "power changed")
	if len(lines) != 4 || !strings.Contains(lines[3], "power=battery") {
		t.Fatalf("the power source change must be logged: %v", lines)
	}
}

// TestSyncLogMarksNoticePages makes sure a notice is not disguised as a
// regular page.
func TestSyncLogMarksNoticePages(t *testing.T) {
	logs := captureLog(t)
	s, _ := planServer(t, "pages: [fehlt]\n")
	dev := planDevice(t, s)
	displayPage(t, s, dev)
	out := logs()
	if !strings.Contains(out, "display sync") || !strings.Contains(out, "page=notice") {
		t.Fatalf("notice must be logged as such: %q", out)
	}
}

// TestDeviceLogFromSync checks that firmware log entries in a sync become one
// readable line per entry, with level, source and age.
func TestDeviceLogFromSync(t *testing.T) {
	logs := captureLog(t)
	s, now := planServer(t, testPlan)
	dev := planDevice(t, s)
	dev.req.Log = []deviceLogEntry{
		{Time: now.Add(-2 * time.Hour).Unix(), ID: 7, Level: "error", Message: "image download failed", Source: "http"},
		{Level: "warn", Message: "wifi weak"},
	}
	displayPage(t, s, dev)
	out := logs()
	first := logLines(out, "image download failed")
	if len(first) != 1 {
		t.Fatalf("one line per entry: %q", out)
	}
	for _, want := range []string{"level=ERROR", "device=test", "source=http", "age=2h0m0s"} {
		if !strings.Contains(first[0], want) {
			t.Fatalf("%q missing in %q", want, first[0])
		}
	}
	if second := logLines(out, "wifi weak"); len(second) != 1 || !strings.Contains(second[0], "level=WARN") {
		t.Fatalf("second entry: %q", second)
	}

	// A repeated delivery of a numbered entry is logged once; entries without
	// time and ID cannot be told apart and are logged every time.
	displayPage(t, s, dev)
	out = logs()
	if lines := logLines(out, "image download failed"); len(lines) != 1 {
		t.Fatalf("repeated entry must be logged once: %q", lines)
	}
	if lines := logLines(out, "wifi weak"); len(lines) != 2 {
		t.Fatalf("anonymous entries are logged each time: %q", lines)
	}
}

// TestDeviceLogKeepsTheNewest checks the bound of the stored log and that a
// repeated delivery does not grow it (ADR 0171).
func TestDeviceLogKeepsTheNewest(t *testing.T) {
	st := &deviceState{}
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	for i := 0; i < deviceLogLimit+5; i++ {
		st.addDeviceLog([]deviceLogEntry{{ID: i + 1, Time: int64(i + 1), Level: "info",
			Message: fmt.Sprintf("line %d", i)}}, now)
	}
	if len(st.Log) != deviceLogLimit {
		t.Fatalf("the log must hold %d lines, got %d", deviceLogLimit, len(st.Log))
	}
	if st.Log[0].Message != "line 5" || st.Log[len(st.Log)-1].Message != "line 104" {
		t.Fatalf("the log must hold the newest lines: %q ... %q", st.Log[0].Message, st.Log[len(st.Log)-1].Message)
	}
	// A retried sync sends the same entry again.
	st.addDeviceLog([]deviceLogEntry{{ID: 105, Time: 105, Level: "info", Message: "line 104"}}, now)
	if len(st.Log) != deviceLogLimit {
		t.Fatalf("a repeated delivery must not add a line: %d", len(st.Log))
	}
	// An entry without ID and time cannot be told apart, like in the journal.
	st.addDeviceLog([]deviceLogEntry{{Level: "warn", Message: "anonymous"}}, now)
	if len(st.Log) != deviceLogLimit || st.Log[len(st.Log)-1].Message != "anonymous" {
		t.Fatalf("an anonymous entry must be appended: %d %q", len(st.Log), st.Log[len(st.Log)-1].Message)
	}
}

// TestDeviceLogStoredFromSync checks that the lines of a sync reach the state
// that the device editor renders (ADR 0171).
func TestDeviceLogStoredFromSync(t *testing.T) {
	s, now := planServer(t, testPlan)
	dev := planDevice(t, s)
	dev.req.Log = []deviceLogEntry{
		{Time: now.Unix(), ID: 7, Level: "info", Message: "timing boot: 412 ms (+0)", Source: "firmware"},
		{Time: now.Unix(), ID: 8, Level: "warn", Message: "image download failed", Source: "firmware"},
	}
	displayPage(t, s, dev)
	st := s.state[s.config().Devices[0].stateKey()]
	if st == nil || len(st.Log) != 2 {
		t.Fatalf("the sync must store both lines: %+v", st)
	}
	if st.Log[0].ID != 7 || st.Log[0].Level != "info" || st.Log[1].Message != "image download failed" {
		t.Fatalf("stored lines: %+v", st.Log)
	}
	// The same sync repeated after a lost answer stores nothing twice.
	displayPage(t, s, dev)
	if len(st.Log) != 2 {
		t.Fatalf("a repeated sync must not duplicate the lines: %+v", st.Log)
	}
}

// TestSyncTouchAndHoldOnlyLogged checks that touches and long presses are
// recorded but do not page yet (ADR 0042).
func TestSyncTouchAndHoldOnlyLogged(t *testing.T) {
	logs := captureLog(t)
	s, _ := planServer(t, testPlan)
	dev := planDevice(t, s)
	displayPage(t, s, dev)
	dev.req.Reason = "touch"
	dev.req.Events = []syncEvent{{Type: "touch", X: 412, Y: 233}}
	out := displayPage(t, s, dev)
	if !strings.Contains(pagePath(out), "/eins/") || out["listen_s"].(float64) != listenAfterInput.Seconds() {
		t.Fatalf("a touch must not page and must keep the input window: %v", out)
	}
	if lines := logLines(logs(), "display touch"); len(lines) != 1 || !strings.Contains(lines[0], "x=412") {
		t.Fatalf("touch not logged: %q", lines)
	}
	dev.req.Reason = "button"
	dev.req.Events = []syncEvent{{Type: "button", Button: buttonNext, HoldMS: 1500}}
	displayPage(t, s, dev)
	if lines := logLines(logs(), "display button held"); len(lines) != 1 || !strings.Contains(lines[0], "hold_ms=1500") {
		t.Fatalf("hold not logged: %q", lines)
	}
}
