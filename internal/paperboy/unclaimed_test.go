package paperboy

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
)

// TestUnclaimedDeviceInEditor registers hardware whose MAC no device file uses:
// its state file shows up in the devices tab and can be claimed or forgotten
// (ADR 0080).
func TestUnclaimedDeviceInEditor(t *testing.T) {
	s, _ := testServer(t)
	if rec := register(t, s, `{"mac":"aa:bb:cc:dd:ee:99","hardware":"sticky","firmware":"0.5.0"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("unknown device: %d %s", rec.Code, rec.Body)
	}
	page := htmlRequest(t, s, "/ui/editor?tab=devices").Body.String()
	if !strings.Contains(page, "Unclaimed hardware") || !strings.Contains(page, "AA:BB:CC:DD:EE:99") ||
		!strings.Contains(page, "sticky") || !strings.Contains(page, "0.5.0") {
		t.Fatalf("unclaimed device missing:\n%s", page)
	}
	claim := "/ui/editor/device?" + url.Values{"new": {"1"}, "id": {"sticky-ee99"}, "mac": {"AA:BB:CC:DD:EE:99"}}.Encode()
	if !strings.Contains(page, "id=\"unclaimed-table\"") || !strings.Contains(page, "/ui/editor/device?") {
		t.Fatalf("claim link missing:\n%s", page)
	}
	// The claim link opens a new device with the MAC already filled in.
	editor := htmlRequest(t, s, claim).Body.String()
	if !strings.Contains(editor, `"mac":"AA:BB:CC:DD:EE:99"`) {
		t.Fatalf("the editor does not prefill the MAC:\n%s", editor)
	}
	// Forgetting the state deletes the file; the hardware disappears.
	rec := request(t, s, http.MethodDelete, "/api/state/devices/AA:BB:CC:DD:EE:99", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("forget: %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(deviceStateFile(s.devicesDir, "AABBCCDDEE99")); !os.IsNotExist(err) {
		t.Fatalf("state file survived: %v", err)
	}
	if strings.Contains(htmlRequest(t, s, "/ui/editor?tab=devices").Body.String(), "AA:BB:CC:DD:EE:99") {
		t.Fatal("the unclaimed row survived the forget")
	}
	if rec := request(t, s, http.MethodDelete, "/api/state/devices/nope", nil, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid MAC: %d %s", rec.Code, rec.Body)
	}
}

// TestUnclaimedClaimKeepsState claims a state file by adding the device file:
// the state, including the hardware it reports, stays with the MAC.
func TestUnclaimedClaimKeepsState(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{Dir: dir, Profiles: map[string]Profile{"small": {Width: 40, Height: 20, PPI: 125}}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if rec := register(t, s, `{"mac":"aa:bb:cc:dd:ee:07","hardware":"e1001"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("unknown device: %d %s", rec.Code, rec.Body)
	}
	if len(s.unclaimedDevices(s.config())) != 1 {
		t.Fatalf("unclaimed: %+v", s.unclaimedDevices(s.config()))
	}
	// Claiming adds the device file; the next server loads both and lists the
	// hardware as claimed.
	writeDeviceFile(t, dir, "flur", "display_type: small\nmac: \"aa:bb:cc:dd:ee:07\"\n")
	claimedCfg := *cfg
	claimedCfg.Devices = []Device{{ID: "flur", MAC: "AA:BB:CC:DD:EE:07", Profile: "small"}}
	claimed, err := New(&claimedCfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := claimed.unclaimedDevices(claimed.config()); len(got) != 0 {
		t.Fatalf("a claimed device is still listed: %+v", got)
	}
	if st := claimed.state["AABBCCDDEE07"]; st == nil || st.Hardware != "e1001" {
		t.Fatalf("the claimed state was lost: %+v", claimed.state)
	}
}
