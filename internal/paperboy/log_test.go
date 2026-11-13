package paperboy

import (
	"bytes"
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
	s, _ := planServer(t, "rules: [{name: tag, pages: [eins, zwei], rotate: true}]\n")
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
	dev.req.Battery = &syncBattery{Volts: 3.91, MA: &charging}
	displayPage(t, s, dev)
	lines := logLines(logs(), "display sync")
	if len(lines) == 0 || !strings.Contains(lines[len(lines)-1], "battery_ma=+413") {
		t.Fatalf("charge current must be logged with its sign: %q", lines)
	}
}

// TestSyncLogMarksNoticePages makes sure a notice is not disguised as a
// regular page.
func TestSyncLogMarksNoticePages(t *testing.T) {
	logs := captureLog(t)
	s, _ := planServer(t, "rules: [{name: tag, pages: [fehlt]}]\n")
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
		t.Fatalf("touch must not page but keep the device listening: %v", out)
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
