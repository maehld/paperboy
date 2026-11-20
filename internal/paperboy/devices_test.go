package paperboy

import (
	"net/http"
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

// TestDevicesAPINamesOnlyKnownFields keeps GET /api/devices an explicit field
// set and never exposes the device key or the pairing state (ADR 0139).
func TestDevicesAPINamesOnlyKnownFields(t *testing.T) {
	s, now := planServer(t, testPlan)
	planDevice(t, s)
	key, err := deviceStateKey(planDeviceMAC)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	st := s.state[key]
	st.PairCode, st.PairTries, st.PairExpiry, st.PairAuthorized = "ABC123", 2, now.Add(time.Minute), true
	apiKey := st.APIKey
	s.mu.Unlock()

	info := deviceInfoOf(t, s)
	allowed := map[string]bool{}
	for _, name := range []string{
		"id", "protocol", "mac", "display_type", "plan", "registered", "status",
		"last_page_url", "firmware_available", "firmware_status", "power", "problem",
		"page", "page_name", "first_seen", "last_seen", "client_ip",
		"battery_voltage", "battery_percent", "battery_mah", "battery_ma", "battery_charging",
		"rssi", "firmware", "hardware", "wake_reason", "battery_level", "rule",
		"charge", "last_charge", "battery_history", "reported_power", "reported_touch",
		"paired_at", "last_page", "last_page_kind", "last_page_hash", "last_pages",
		"scroll_hash", "last_page_time", "last_refresh", "last_interval", "last_error",
		"last_text", "last_event_id", "menu", "firmware_pending", "firmware_failures",
	} {
		allowed[name] = true
	}
	for name := range info {
		if !allowed[name] {
			t.Errorf("the devices API names an unlisted field %q", name)
		}
	}
	for _, secret := range []string{"api_key", "pair_code", "pair_expiry", "pair_tries", "pair_authorized"} {
		if _, ok := info[secret]; ok {
			t.Errorf("the devices API exposes %q", secret)
		}
	}
	rec := request(t, s, http.MethodGet, "/api/devices", nil, nil)
	for name, value := range map[string]string{"pair code": "ABC123", "device key": apiKey} {
		if strings.Contains(rec.Body.String(), value) {
			t.Errorf("the devices API leaks the %s", name)
		}
	}
}

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
	writePageFile(t, s.config().Pages+"/kaputt.yaml", "kind: image\ngrid: [1, 1]\ncells: [\n")
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
	writePageFile(t, s.config().Pages+"/kaputt.yaml", textPage("ok"))
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
