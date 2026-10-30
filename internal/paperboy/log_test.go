package paperboy

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

// TestDisplayLogUsesPageName checks that the request log names the page
// instead of its index, so the log can be read without opening the plan.
func TestDisplayLogUsesPageName(t *testing.T) {
	logs := captureLog(t)
	s, _ := planServer(t, "rules: [{name: tag, pages: [eins, zwei], rotate: true}]\n")
	dev := planDevice(t, s)
	displayPage(t, s, dev)
	displayPage(t, s, dev)
	out := logs()
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "display request") {
			line = l
		}
	}
	if !strings.Contains(line, "page=zwei") || !strings.Contains(line, "index=1") || !strings.Contains(line, "pages=2") {
		t.Fatalf("rotation page and its position must be visible: %q", line)
	}
	if !strings.Contains(out, "page=eins") {
		t.Fatalf("the first request must name its page: %q", out)
	}
}

// TestDisplayLogMarksNoticePages makes sure a notice is not disguised as a
// regular page.
func TestDisplayLogMarksNoticePages(t *testing.T) {
	logs := captureLog(t)
	s, _ := planServer(t, "rules: [{name: tag, pages: [fehlt]}]\n")
	dev := planDevice(t, s)
	displayPage(t, s, dev)
	out := logs()
	if !strings.Contains(out, "display request") || !strings.Contains(out, "page=notice") {
		t.Fatalf("notice must be logged as such: %q", out)
	}
}

// postLog delivers a log payload the way the firmware does.
func postLog(t *testing.T, s *Server, mac, body string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/log", strings.NewReader(body))
	req.Header.Set("ID", mac)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("log response: %d", rec.Code)
	}
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

// TestDeviceLogSplitsBatch checks that a buffered batch becomes one readable
// line per entry, with firmware level, source and age, instead of one raw JSON
// line.
func TestDeviceLogSplitsBatch(t *testing.T) {
	logs := captureLog(t)
	s, now := planServer(t, testPlan)
	body := fmt.Sprintf(`{"logs":[
		{"created_at":%d,"id":2529,"message":"File open ERROR","source_line":202,
		 "source_path":"src/filesystem.cpp","wifi_status":"no_shield","level":"error","retry":3},
		{"created_at":%d,"id":2532,"message":"connection refused","source_line":52,
		 "source_path":"src/services/http_retry_request.cpp","wifi_status":"connected","level":"warn"}]}`,
		now.Add(-2*time.Hour).Unix(), now.Unix())
	postLog(t, s, planDeviceMAC, body)
	out := logs()

	first := logLines(out, "File open ERROR")
	if len(first) != 1 {
		t.Fatalf("one line per entry: %q", out)
	}
	for _, want := range []string{"level=ERROR", "device=test", "source=src/filesystem.cpp:202",
		"wifi=no_shield", "retry=3", "age=2h0m0s"} {
		if !strings.Contains(first[0], want) {
			t.Fatalf("%q missing in %q", want, first[0])
		}
	}
	second := logLines(out, "connection refused")
	if len(second) != 1 || !strings.Contains(second[0], "level=WARN") ||
		!strings.Contains(second[0], "source=src/services/http_retry_request.cpp:52") {
		t.Fatalf("second entry: %q", second)
	}
	if strings.Contains(out, `"logs"`) {
		t.Fatalf("the raw batch must not be logged: %q", out)
	}
}

// TestDeviceLogSkipsRetriedBatch makes sure a batch the firmware delivers again
// is not logged a second time.
func TestDeviceLogSkipsRetriedBatch(t *testing.T) {
	logs := captureLog(t)
	s, now := planServer(t, testPlan)
	body := fmt.Sprintf(`{"logs":[{"created_at":%d,"id":1,"message":"wifi lost","level":"warn"}]}`, now.Unix())
	postLog(t, s, planDeviceMAC, body)
	postLog(t, s, planDeviceMAC, body)
	if lines := logLines(logs(), "wifi lost"); len(lines) != 1 {
		t.Fatalf("duplicate delivery must be logged once: %q", lines)
	}
}

// TestDeviceLogKeepsUnknownBody makes sure a payload that is no log batch is
// still visible in the log.
func TestDeviceLogKeepsUnknownBody(t *testing.T) {
	logs := captureLog(t)
	s, _ := planServer(t, testPlan)
	postLog(t, s, planDeviceMAC, `not a log batch`)
	out := logs()
	lines := logLines(out, "display log")
	if len(lines) != 1 || !strings.Contains(lines[0], "not a log batch") ||
		!strings.Contains(lines[0], "level=WARN") {
		t.Fatalf("unknown body must be kept: %q", out)
	}
}
