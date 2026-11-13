package paperboy

import (
	"strings"
	"testing"
	"time"
)

// deviceInfoOf returns the devices API entry of the single test device.
func deviceInfoOf(t *testing.T, s *Server) map[string]any {
	t.Helper()
	devices := call(t, s, "/api/devices", nil)["devices"].([]any)
	if len(devices) != 1 {
		t.Fatalf("expected one device: %v", devices)
	}
	return devices[0].(map[string]any)
}

// TestDevicesAPIShowsServedPage checks that the devices API names the page the
// device was last pointed at, including its URL and refresh.
func TestDevicesAPIShowsServedPage(t *testing.T) {
	s, now := planServer(t, testPlan)
	dev := planDevice(t, s)
	out := displayPage(t, s, dev)
	path := pagePath(out) // /pages/small/eins/<hash>.png
	hash := imageOf(out)["hash"].(string)

	info := deviceInfoOf(t, s)
	if info["last_page"] != "eins" || info["last_page_kind"] != "page" {
		t.Fatalf("last page: %v", info)
	}
	if info["last_page_hash"] != hash || !strings.HasSuffix(path, "/"+hash+".png") {
		t.Fatalf("hash %v, want %q in %q", info["last_page_hash"], hash, path)
	}
	// The card reads the last served image through the UI, because the page
	// route itself needs the device key (ADR 0097).
	if info["last_page_url"] != "/api/devices/test/image" {
		t.Fatalf("page url %v", info["last_page_url"])
	}
	if info["last_refresh"] != float64(600) || info["status"] != "ok" {
		t.Fatalf("refresh/status: %v", info)
	}
	if _, ok := info["last_error"]; ok {
		t.Fatalf("a rendered page must not report an error: %v", info)
	}
	if info["last_page_time"] == nil {
		t.Fatal("last_page_time missing")
	}
	// The device is late after twice its refresh interval without a request.
	*now = now.Add(21 * time.Minute)
	if info := deviceInfoOf(t, s); info["status"] != "late" {
		t.Fatalf("status after a long silence: %v", info)
	}
}

// TestDevicesAPIReportsNoticeKeepsError checks that a broken page is reported
// as a notice page together with the render error, so the API never claims a
// page that was not drawn.
func TestDevicesAPIReportsNoticeKeepsError(t *testing.T) {
	s, _ := planServer(t, "rules: [{name: tag, pages: [kaputt]}]\n")
	writePageFile(t, s.config().Pages+"/kaputt.small.yaml", "grid: [1, 1]\ncells: [\n")
	dev := planDevice(t, s)
	displayPage(t, s, dev)

	info := deviceInfoOf(t, s)
	if info["last_page_kind"] != "notice" {
		t.Fatalf("broken page must be a notice: %v", info)
	}
	if _, ok := info["last_page"]; ok {
		t.Fatalf("a notice has no page name: %v", info)
	}
	if url, _ := info["last_page_url"].(string); url != "/api/devices/test/image" {
		t.Fatalf("notice url: %q", url)
	}
	if err, _ := info["last_error"].(string); !strings.Contains(err, "kaputt") {
		t.Fatalf("render error not reported: %q", err)
	}
	// Fixing the template clears the error with the next request.
	writePageFile(t, s.config().Pages+"/kaputt.small.yaml", textPage("ok"))
	displayPage(t, s, dev)
	if info := deviceInfoOf(t, s); info["last_page_kind"] != "page" || info["last_error"] != nil {
		t.Fatalf("fixed page must clear the error: %v", info)
	}
}

// TestDevicesAPIStatusLifecycle covers the derived status values.
func TestDevicesAPIStatusLifecycle(t *testing.T) {
	s, _ := planServer(t, testPlan)
	if info := deviceInfoOf(t, s); info["status"] != "unregistered" {
		t.Fatalf("before setup: %v", info)
	}
	dev := planDevice(t, s)
	if info := deviceInfoOf(t, s); info["status"] != "never" {
		t.Fatalf("after registration without a sync: %v", info)
	}
	displayPage(t, s, dev)
	if info := deviceInfoOf(t, s); info["status"] != "ok" {
		t.Fatalf("after the first sync: %v", info)
	}
}
