package paperboy

import (
	"path/filepath"
	"testing"
	"time"
)

// TestRotationInheritance: a device overrides the rotation of its display
// type, and an explicit 0 is an override too (ADR 0102).
func TestRotationInheritance(t *testing.T) {
	cfg := &Config{Profiles: map[string]Profile{"portrait": {Width: 480, Height: 800, PPI: 236, Rotation: 90}}}
	d := &Device{Profile: "portrait"}
	if got := cfg.rotation(d); got != 90 {
		t.Fatalf("inherited rotation: %d, want 90", got)
	}
	override := 270
	d.Rotation = &override
	if got := cfg.rotation(d); got != 270 {
		t.Fatalf("device rotation: %d, want 270", got)
	}
	zero := 0
	d.Rotation = &zero
	if got := cfg.rotation(d); got != 0 {
		t.Fatalf("explicit 0: %d, want 0", got)
	}
}

// TestSyncAnswersRotation verifies that the sync carries the effective
// rotation of the device file for the firmware to apply (ADR 0102).
func TestSyncAnswersRotation(t *testing.T) {
	dir := t.TempDir()
	writePageFile(t, filepath.Join(dir, pagesDirName, "eins.small.yaml"), textPage("eins"))
	writePageFile(t, filepath.Join(dir, plansDirName, "zimmer.yaml"), "fallback: eins\n")
	override := 270
	cfg := &Config{Dir: dir, Timezone: time.UTC,
		Profiles: map[string]Profile{"small": {Width: 480, Height: 800, PPI: 236, Rotation: 90}},
		Devices: []Device{{ID: "test", MAC: planDeviceMAC, Profile: "small", Plan: "zimmer",
			Refresh: Duration(10 * time.Minute), Rotation: &override}}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	out := displayPage(t, s, planDevice(t, s))
	if got := out["rotation"]; got != float64(270) {
		t.Fatalf("sync rotation: %v, want 270", got)
	}
}
