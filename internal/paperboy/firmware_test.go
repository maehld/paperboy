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
	for _, name := range []string{"sticky-0.4.0.ota.bin", "e1001-1.0.bin", "sticky-.bin", "sticky-1..2.bin", "sticky-1.0.bin.txt", "sticky-v1.0.bin.old"} {
		if _, _, ok := parseFirmwareName("sticky", name); ok {
			t.Fatalf("%s must be rejected", name)
		}
	}
	// The version keeps the v of the tag; the old spelling without it stays valid.
	for _, name := range []string{"sticky-v0.10.0.bin", "sticky-0.10.0.bin"} {
		if v, _, ok := parseFirmwareName("sticky", name); !ok || v != "0.10.0" {
			t.Fatalf("%s: %q %v", name, v, ok)
		}
	}
	if hw, ok := splitFirmwareName("sticky-v0.10.0.bin"); !ok || hw != "sticky" {
		t.Fatalf("hardware split: %q %v", hw, ok)
	}
	if _, ok := splitFirmwareName("notes.txt"); ok {
		t.Fatal("notes.txt is no image")
	}
	// A pin names the version with or without the v.
	for _, pin := range []string{"v0.10.0", "0.10.0"} {
		if p := pinVersion(pin); p == nil || compareVersions(p, a) != 0 {
			t.Fatalf("pin %q: %v", pin, p)
		}
	}
	if pinVersion("") != nil || pinVersion("nope") != nil {
		t.Fatal("empty and invalid pins must be ignored")
	}
}

// firmwareServer is a plan server with updates on and three files in the flat
// firmware directory: two images and one ignored name.
func firmwareServer(t *testing.T) (*Server, *testDevice) {
	t.Helper()
	s, _ := planServer(t, testPlan)
	s.config().Firmware = FirmwareConfig{Update: firmwareAuto, MinBatteryVolts: 3.6}
	dir := s.config().FirmwareDir
	writePageFile(t, filepath.Join(dir, "sticky-v0.9.2.bin"), "old image")
	writePageFile(t, filepath.Join(dir, "sticky-v0.10.0.bin"), "new image")
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
	if offer == nil || offer["version"] != "0.10.0" || offer["path"] != "/firmware/sticky-v0.10.0.bin" ||
		offer["md5"] != md5Hex("new image") {
		t.Fatalf("offer: %v", offer)
	}
	rec := fetchImage(t, s, offer["path"].(string))
	if rec.Code != 200 || rec.Body.String() != "new image" {
		t.Fatalf("download: %d %q", rec.Code, rec.Body)
	}
	for _, path := range []string{"/firmware/notes.txt", "/firmware/sticky-v9.9.bin",
		"/firmware/Sticky-v0.10.0.bin", "/firmware/..%2fsticky-v0.10.0.bin"} {
		if rec := fetchImage(t, s, path); rec.Code != 404 {
			t.Fatalf("%s: %d", path, rec.Code)
		}
	}
	info := deviceInfoOf(t, s)
	if info["firmware_status"] != fwOffered || info["firmware_available"] != "0.10.0" || info["firmware"] != "0.4.0" {
		t.Fatalf("devices API: %v", info)
	}
	if body := htmlRequest(t, s, "/ui/downloads").Body.String(); !strings.Contains(body, "notes.txt") {
		t.Fatalf("downloads page: %s", body)
	}
	if body := htmlRequest(t, s, "/ui").Body.String(); !strings.Contains(body, "0.4.0 → 0.10.0 · offered") {
		t.Fatalf("dashboard: %s", body)
	}
	if !strings.Contains(logs(), "firmware file ignored") {
		t.Fatal("ignored file must be logged")
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

// TestFirmwareOldLayout verifies that an instance with firmware/<hardware>/
// keeps working and that a flat file wins the same version (ADR 0060).
func TestFirmwareOldLayout(t *testing.T) {
	s, dev := firmwareServer(t)
	dir := s.config().FirmwareDir
	writePageFile(t, filepath.Join(dir, "sticky", "sticky-v0.11.0.bin"), "nested image")
	if offer := offerOf(displayPage(t, s, dev)); offer == nil || offer["version"] != "0.11.0" ||
		offer["path"] != "/firmware/sticky-v0.11.0.bin" {
		t.Fatalf("nested image must be offered: %v", offer)
	}
	if rec := fetchImage(t, s, "/firmware/sticky-v0.11.0.bin"); rec.Code != 200 || rec.Body.String() != "nested image" {
		t.Fatalf("nested download: %d %q", rec.Code, rec.Body)
	}
	// The same version flat wins.
	writePageFile(t, filepath.Join(dir, "sticky-v0.11.0.bin"), "flat image")
	if offer := offerOf(displayPage(t, s, dev)); offer == nil || offer["md5"] != md5Hex("flat image") {
		t.Fatalf("flat file must win the tie: %v", offer)
	}
	if body := htmlRequest(t, s, "/ui/downloads").Body.String(); !strings.Contains(body, "sticky/ (old layout)") {
		t.Fatal("the old directory must be reported")
	}
}

// TestFirmwarePin verifies that a pinned version is offered even when a higher
// image lies in the directory: the pin is the rollback switch (ADR 0060).
func TestFirmwarePin(t *testing.T) {
	s, dev := firmwareServer(t)
	s.config().Firmware.Version = "0.9.2"
	if offer := offerOf(displayPage(t, s, dev)); offer == nil || offer["version"] != "0.9.2" {
		t.Fatalf("pinned offer: %v", offer)
	}
	if info := deviceInfoOf(t, s); info["firmware_available"] != "0.9.2" {
		t.Fatalf("pinned availability: %v", info)
	}
	// A pinned image the device already runs offers nothing.
	dev.req.Firmware = "0.9.2"
	if offer := offerOf(displayPage(t, s, dev)); offer != nil {
		t.Fatalf("no offer for the pinned version: %v", offer)
	}
}

// TestFirmwareImagesOfUnknownHardwareAreValid verifies that an image of a
// hardware type without a display type of that name is not reported as ignored:
// the download installs every image of a bundle (ADR 0061), and the file name
// follows the hardware a device reports, not the display type's label.
func TestFirmwareImagesOfUnknownHardwareAreValid(t *testing.T) {
	s, _ := firmwareServer(t)
	writePageFile(t, filepath.Join(s.config().FirmwareDir, "lcd2004-v1.0.0.bin"), "image")
	body := htmlRequest(t, s, "/ui/downloads").Body.String()
	if !strings.Contains(body, "lcd2004-v1.0.0.bin") {
		t.Fatalf("a valid image is not listed: %s", body)
	}
	// Only the file that is no image is reported as ignored.
	_, issues, ok := strings.Cut(body, "Ignored firmware files")
	if !ok || !strings.Contains(issues, "notes.txt") || strings.Contains(issues, "lcd2004-v1.0.0.bin") {
		t.Fatalf("ignored files: %s", body)
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
	path := filepath.Join(s.config().FirmwareDir, "sticky-v0.10.0.bin")
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
	if err := os.Remove(filepath.Join(s.config().FirmwareDir, "sticky-v0.10.0.bin")); err != nil {
		t.Fatal(err)
	}
	dev.req.Firmware = "0.10.0"
	if offer := offerOf(displayPage(t, s, dev)); offer == nil || offer["version"] != "0.9.2" {
		t.Fatalf("rollback offer: %v", offer)
	}
	// Hardware without images offers nothing.
	if err := os.RemoveAll(filepath.Join(s.config().FirmwareDir, "sticky-v0.9.2.bin")); err != nil {
		t.Fatal(err)
	}
	if offerOf(displayPage(t, s, dev)) != nil {
		t.Fatal("no offer without images")
	}
	if info := deviceInfoOf(t, s); info["firmware_status"] != fwNone {
		t.Fatalf("no images: %v", info)
	}
}
