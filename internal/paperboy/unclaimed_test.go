package paperboy

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
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
	// The row itself asks before it opens the new device file (ADR 0080).
	if !strings.Contains(page, `data-claim="`+strings.ReplaceAll(claim, "&", "&amp;")+`"`) ||
		!strings.Contains(page, `data-mac="AA:BB:CC:DD:EE:99"`) {
		t.Fatalf("the unclaimed row does not carry its claim:\n%s", page)
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

// TestUnclaimedRegistrationIsBounded caps the hardware that registers without
// a device file: the state map and state/devices/ must not grow without limit
// (ADR 0136). An entry that exists keeps updating at the cap.
func TestUnclaimedRegistrationIsBounded(t *testing.T) {
	s, _ := testServer(t)
	registration := func(i int) string {
		return `{"mac":"aa:bb:cc:dd:ef:` + fmt.Sprintf("%02x", i) + `","hardware":"sticky"}`
	}
	for i := range maxUnclaimed {
		if rec := registerFrom(t, s, fmt.Sprintf("10.0.0.%d:5555", i+1), registration(i)); rec.Code != http.StatusForbidden {
			t.Fatalf("unknown device %d: %d %s", i, rec.Code, rec.Body)
		}
	}
	if got := len(s.unclaimedDevices(s.config())); got != maxUnclaimed {
		t.Fatalf("unclaimed: %d, want %d", got, maxUnclaimed)
	}
	// The next new MAC is refused and writes neither memory nor disk.
	if rec := registerFrom(t, s, "10.0.0.99:5555", registration(0xff)); rec.Code != http.StatusForbidden {
		t.Fatalf("registration above the cap: %d %s", rec.Code, rec.Body)
	}
	key, err := deviceStateKey("aa:bb:cc:dd:ef:ff")
	if err != nil {
		t.Fatal(err)
	}
	if s.state[key] != nil {
		t.Fatalf("state above the cap: %+v", s.state[key])
	}
	if _, err := os.Stat(deviceStateFile(s.devicesDir, key)); !os.IsNotExist(err) {
		t.Fatalf("state file above the cap: %v", err)
	}
	// An existing unclaimed entry still reports its new hardware.
	if rec := registerFrom(t, s, "10.0.0.1:5555", `{"mac":"aa:bb:cc:dd:ef:00","hardware":"e1001"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("existing unclaimed device: %d %s", rec.Code, rec.Body)
	}
	if st := s.state[mustStateKey(t, "aa:bb:cc:dd:ef:00")]; st == nil || st.Hardware != "e1001" {
		t.Fatalf("existing unclaimed state did not update: %+v", st)
	}
}

func mustStateKey(t *testing.T, mac string) string {
	t.Helper()
	key, err := deviceStateKey(mac)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// TestUnknownNoteLogsOnce keeps the journal quiet under a registration flood:
// one line per MAC and hour, and a bounded memory (ADR 0136).
func TestUnknownNoteLogsOnce(t *testing.T) {
	s, now := testServer(t)
	if !s.noteUnknownLocked("AABBCCDDEEFF", *now) {
		t.Fatal("the first registration must log")
	}
	if s.noteUnknownLocked("AABBCCDDEEFF", now.Add(30*time.Minute)) {
		t.Fatal("a second registration within the hour must not log")
	}
	if !s.noteUnknownLocked("AABBCCDDEEFF", now.Add(2*time.Hour)) {
		t.Fatal("a registration after the hour must log again")
	}
	for i := range unknownNotesMax + 10 {
		s.noteUnknownLocked(fmt.Sprintf("MAC%04d", i), *now)
	}
	if len(s.unknownNotes) > unknownNotesMax {
		t.Fatalf("the note map grew to %d entries, cap %d", len(s.unknownNotes), unknownNotesMax)
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
