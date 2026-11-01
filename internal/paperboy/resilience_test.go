package paperboy

import (
	"net/http"
	"strings"
	"testing"
)

// TestDisplaySurvivesRenderingPanic makes a page render panic and checks that
// the device still gets an answer and the server keeps working. Regression test
// for a request hanging forever and taking every later request with it.
func TestDisplaySurvivesRenderingPanic(t *testing.T) {
	s, _ := planServer(t, "rules: [{name: tag, pages: [eins]}]\n")
	dev := planDevice(t, s)
	// A missing document makes the page renderer panic.
	doc := s.doc
	s.doc = nil
	out := displayPage(t, s, dev)
	s.doc = doc
	if got := pagePath(out); !strings.HasPrefix(got, "/hints/") {
		t.Fatalf("a panicking page must become a notice page: %q", got)
	}
	if rec := fetchImage(t, s, pagePath(out)); rec.Code != http.StatusOK {
		t.Fatalf("notice image: %d", rec.Code)
	}
	// The state lock must be free again: other requests answer immediately.
	if out := call(t, s, "/api/devices", nil); out["devices"] == nil {
		t.Fatalf("server wedged after a rendering panic: %v", out)
	}
	if code, body := dataCall(t, s, http.MethodPut, "/api/data/akku", `{"soc":50}`); code != http.StatusNoContent {
		t.Fatalf("data API after a rendering panic: %d %s", code, body)
	}
}

// TestDisplayPageErrorsKeepDeviceWorking checks that a broken template only
// affects its own page: the request still ends with a notice and the next
// request renders normally.
func TestDisplayPageErrorsKeepDeviceWorking(t *testing.T) {
	s, _ := planServer(t, "rules: [{name: tag, pages: [kaputt]}]\n")
	writePageFile(t, s.config().Pages+"/kaputt.small.yaml", "rows: [1]\nblocks: [\n")
	dev := planDevice(t, s)
	if got := pagePath(displayPage(t, s, dev)); !strings.HasPrefix(got, "/hints/") {
		t.Fatalf("broken template must show a notice: %q", got)
	}
	// Fixing the file serves the page again without a restart.
	writePageFile(t, s.config().Pages+"/kaputt.small.yaml", "rows: [1]\nblocks:\n  - text: \"ok\"\n")
	if got := pagePath(displayPage(t, s, dev)); !strings.HasPrefix(got, "/pages/small/kaputt/") {
		t.Fatalf("fixed template must render again: %q", got)
	}
}
