package paperboy

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
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
