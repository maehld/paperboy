package paperboy

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeProgram(t *testing.T, dir, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" && !strings.HasSuffix(name, ".exe") {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestPluginProgramLookup verifies the naming rules of ADR 0060: an unversioned
// file wins, then the pin, then the highest version, then PATH.
func TestPluginProgramLookup(t *testing.T) {
	dir := t.TempDir()
	old := writeProgram(t, dir, "weather-v2.0.0")
	newest := writeProgram(t, dir, "weather-v2.1.0")

	if program, version := pluginProgram(dir, "weather", ""); program != newest || version != "v2.1.0" {
		t.Fatalf("highest version: %q %q", program, version)
	}
	if program, version := pluginProgram(dir, "weather", "v2.0.0"); program != old || version != "v2.0.0" {
		t.Fatalf("pin: %q %q", program, version)
	}
	if program, version := pluginProgram(dir, "weather", "v9.9.9"); program != "" || version != "" {
		t.Fatalf("missing pin must not fall back: %q %q", program, version)
	}
	// A hand-built program without a version wins over every downloaded one.
	plain := writeProgram(t, dir, "weather")
	if program, version := pluginProgram(dir, "weather", ""); program != plain || version != "" {
		t.Fatalf("unversioned: %q %q", program, version)
	}
	if program, version := pluginProgram(dir, "weather", "v2.0.0"); program != plain || version != "" {
		t.Fatalf("unversioned beats the pin: %q %q", program, version)
	}
	if program, _ := pluginProgram(dir, "nopebin", ""); program != "" {
		t.Fatalf("unknown plugin: %q", program)
	}
}

func TestParsePluginProgram(t *testing.T) {
	for _, name := range []string{"weather-v2.1.0", "weather-v2.1.0.exe"} {
		version, parts, ok := parsePluginProgram("weather", name)
		if !ok || version != "v2.1.0" || compareVersions(parts, []int{2, 1, 0}) != 0 {
			t.Fatalf("%s: %q %v %v", name, version, parts, ok)
		}
	}
	for _, name := range []string{"weather", "weather-2.1.0", "calendar-v1.0.0", "weather-v1.0.0.txt"} {
		if _, _, ok := parsePluginProgram("weather", name); ok {
			t.Fatalf("%s must not parse", name)
		}
	}
	if v, ok := pluginPin("2.1.0"); !ok || v != "v2.1.0" {
		t.Fatalf("pin normalization: %q %v", v, ok)
	}
	if v, ok := pluginPin("v2.1.0"); !ok || v != "v2.1.0" {
		t.Fatalf("pin normalization: %q %v", v, ok)
	}
	for _, pin := range []string{"main", "2.1.0-rc1", ""} {
		if _, ok := pluginPin(pin); ok {
			t.Fatalf("pin %q must be rejected", pin)
		}
	}
	if v, ok := pluginPin("v2"); !ok || v != "v2" {
		t.Fatalf("a single number is a version: %q %v", v, ok)
	}
}
