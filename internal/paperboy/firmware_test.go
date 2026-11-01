package paperboy

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFirmwareVersions(t *testing.T) {
	a, _ := versionParts("0.10.0")
	b, _ := versionParts("0.9.2")
	if compareVersions(a, b) <= 0 {
		t.Fatal("0.10.0 must be higher than 0.9.2")
	}
	c, _ := versionParts("1.2")
	d, _ := versionParts("1.2.0")
	if compareVersions(c, d) != 0 {
		t.Fatal("missing parts count as zero")
	}
	for _, name := range []string{"sticky-0.4.0.ota.bin", "sticky-v1.bin", "e1001-1.0.bin", "sticky-.bin", "sticky-1..2.bin", "sticky-1.0.bin.txt"} {
		if _, _, ok := parseFirmwareName("sticky", name); ok {
			t.Fatalf("%s must be rejected", name)
		}
	}
	if v, _, ok := parseFirmwareName("sticky", "sticky-0.10.0.bin"); !ok || v != "0.10.0" {
		t.Fatalf("valid name: %q %v", v, ok)
	}
}

// firmwareServer is a plan server with updates on and three files in
// firmware/sticky: two images and one ignored name.
func firmwareServer(t *testing.T) (*Server, *testDevice) {
	t.Helper()
	s, _ := planServer(t, testPlan)
	s.config().Firmware = FirmwareConfig{Update: firmwareAuto, MinBatteryVolts: 3.6}
	dir := filepath.Join(s.config().FirmwareDir, "sticky")
	writePageFile(t, filepath.Join(dir, "sticky-0.9.2.bin"), "old image")
	writePageFile(t, filepath.Join(dir, "sticky-0.10.0.bin"), "new image")
	writePageFile(t, filepath.Join(dir, "notes.txt"), "not an image")
	dev := planDevice(t, s)
	dev.req.Battery = &syncBattery{Volts: 3.9}
	return s, dev
}

func offerOf(out map[string]any) map[string]any {
	offer, _ := out["firmware"].(map[string]any)
	return offer
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestFirmwareOfferAndDownload(t *testing.T) {
	logs := captureLog(t)
	s, dev := firmwareServer(t)
	offer := offerOf(displayPage(t, s, dev))
	if offer == nil || offer["version"] != "0.10.0" || offer["path"] != "/firmware/sticky/sticky-0.10.0.bin" ||
		offer["md5"] != md5Hex("new image") {
		t.Fatalf("offer: %v", offer)
	}
	rec := fetchImage(t, s, offer["path"].(string))
	if rec.Code != 200 || rec.Body.String() != "new image" {
		t.Fatalf("download: %d %q", rec.Code, rec.Body)
	}
	for _, path := range []string{"/firmware/sticky/notes.txt", "/firmware/sticky/e1001-1.0.bin",
		"/firmware/sticky/sticky-9.9.bin", "/firmware/Sticky/sticky-0.10.0.bin", "/firmware/sticky/..%2fsticky-0.10.0.bin"} {
		if rec := fetchImage(t, s, path); rec.Code != 404 {
			t.Fatalf("%s: %d", path, rec.Code)
		}
	}
	info := deviceInfoOf(t, s)
	if info["firmware_status"] != fwOffered || info["firmware_available"] != "0.10.0" || info["firmware"] != "0.4.0" {
		t.Fatalf("devices API: %v", info)
	}
	if !strings.Contains(logs(), "firmware file ignored") {
		t.Fatal("ignored file must be logged")
	}
	if body := htmlRequest(t, s, "/ui").Body.String(); !strings.Contains(body, "sticky/notes.txt") ||
		!strings.Contains(body, "0.4.0 → 0.10.0 · offered") {
		t.Fatalf("dashboard: %s", body)
	}

	// The device reboots into the new version: the offer is settled.
	dev.req.Firmware = "0.10.0"
	if offer := offerOf(displayPage(t, s, dev)); offer != nil {
		t.Fatalf("no offer for the current version: %v", offer)
	}
	if info := deviceInfoOf(t, s); info["firmware_status"] != fwCurrent || info["firmware_pending"] != nil {
		t.Fatalf("after the update: %v", info)
	}
	if !strings.Contains(logs(), "firmware updated") {
		t.Fatal("successful update must be logged")
	}
}

func TestFirmwareStopsAfterFailures(t *testing.T) {
	logs := captureLog(t)
	s, dev := firmwareServer(t)
	for attempt := 1; attempt <= firmwareFailureLimit; attempt++ {
		if offerOf(displayPage(t, s, dev)) == nil {
			t.Fatalf("attempt %d: offer expected", attempt)
		}
	}
	// The third offer is answered with the old version again: three failures.
	if offer := offerOf(displayPage(t, s, dev)); offer != nil {
		t.Fatalf("offer after %d failures: %v", firmwareFailureLimit, offer)
	}
	info := deviceInfoOf(t, s)
	if info["firmware_status"] != fwFailed || info["firmware_failures"] != float64(firmwareFailureLimit) {
		t.Fatalf("failed status: %v", info)
	}
	if !strings.Contains(logs(), "firmware update failed") || !strings.Contains(logs(), "failed 3 times") {
		t.Fatalf("failures must be logged: %s", logs())
	}
	// A changed image is offered again.
	path := filepath.Join(s.config().FirmwareDir, "sticky", "sticky-0.10.0.bin")
	writePageFile(t, path, "fixed image")
	stamp := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if offer := offerOf(displayPage(t, s, dev)); offer == nil || offer["md5"] != md5Hex("fixed image") {
		t.Fatalf("changed image must be offered: %v", offer)
	}
}

func TestFirmwareOfferConditions(t *testing.T) {
	s, dev := firmwareServer(t)

	s.config().Firmware.Update = firmwareOff
	if offerOf(displayPage(t, s, dev)) != nil {
		t.Fatal("no offer with updates off")
	}
	if info := deviceInfoOf(t, s); info["firmware_status"] != fwAvailable {
		t.Fatalf("updates off: %v", info)
	}
	s.config().Firmware.Update = firmwareAuto

	s.config().Devices[0].Hold = true
	if offerOf(displayPage(t, s, dev)) != nil {
		t.Fatal("no offer for a held device")
	}
	if info := deviceInfoOf(t, s); info["firmware_status"] != fwHeld {
		t.Fatalf("held: %v", info)
	}
	s.config().Devices[0].Hold = false

	dev.req.Battery = &syncBattery{Volts: 3.5}
	if offerOf(displayPage(t, s, dev)) != nil {
		t.Fatal("no offer below min_battery_volts")
	}
	dev.req.Battery = nil
	if offerOf(displayPage(t, s, dev)) != nil {
		t.Fatal("no offer without a battery reading")
	}
	s.config().Battery = &Battery{WarnVolts: 4.0, CriticalVolts: 3.3, HysteresisVolts: 0.1}
	dev.req.Battery = &syncBattery{Volts: 3.9}
	if offerOf(displayPage(t, s, dev)) != nil {
		t.Fatal("no offer at battery warning level")
	}
	s.config().Battery = nil
	if offerOf(displayPage(t, s, dev)) == nil {
		t.Fatal("offer once every condition holds")
	}

	// A lower version is a deliberate rollback and offered as well.
	if err := os.Remove(filepath.Join(s.config().FirmwareDir, "sticky", "sticky-0.10.0.bin")); err != nil {
		t.Fatal(err)
	}
	dev.req.Firmware = "0.10.0"
	if offer := offerOf(displayPage(t, s, dev)); offer == nil || offer["version"] != "0.9.2" {
		t.Fatalf("rollback offer: %v", offer)
	}
	// Hardware without images offers nothing.
	if err := os.RemoveAll(filepath.Join(s.config().FirmwareDir, "sticky")); err != nil {
		t.Fatal(err)
	}
	if offerOf(displayPage(t, s, dev)) != nil {
		t.Fatal("no offer without images")
	}
	if info := deviceInfoOf(t, s); info["firmware_status"] != fwNone {
		t.Fatalf("no images: %v", info)
	}
}
