package paperboy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// pluginProgram finds the program of a plugin (ADR 0060):
//
//  1. an unversioned file in the plugins directory always wins — a program
//     built or unpacked by hand;
//  2. a pin selects exactly <name>-<pin>;
//  3. otherwise the highest downloaded <name>-v<version>;
//  4. otherwise the name on PATH.
//
// The returned version is empty for an unversioned program and for PATH.
func pluginProgram(dir, name, pin string) (program, version string) {
	if dir != "" {
		if p, err := exec.LookPath(filepath.Join(dir, name)); err == nil {
			return p, ""
		}
		if pin != "" {
			if p, err := exec.LookPath(filepath.Join(dir, name+"-"+pin)); err == nil {
				return p, pin
			}
		} else if best := highestPluginVersion(dir, name); best != "" {
			if p, err := exec.LookPath(filepath.Join(dir, name+"-"+best)); err == nil {
				return p, best
			}
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, ""
	}
	return "", ""
}

// highestPluginVersion returns the highest <name>-v<version> file in dir.
func highestPluginVersion(dir, name string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	best := ""
	var bestParts []int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		version, parts, ok := parsePluginProgram(name, e.Name())
		if !ok {
			continue
		}
		if best == "" || compareVersions(parts, bestParts) > 0 {
			best, bestParts = version, parts
		}
	}
	return best
}

// parsePluginProgram returns the version of a downloaded plugin program
// <name>-v<version> (with the Windows .exe suffix when there is one).
func parsePluginProgram(name, file string) (string, []int, bool) {
	file = strings.TrimSuffix(file, ".exe")
	rest, ok := strings.CutPrefix(file, name+"-")
	if !ok {
		return "", nil, false
	}
	version, ok := strings.CutPrefix(rest, "v")
	if !ok || !firmwareVersionPattern.MatchString(version) {
		return "", nil, false
	}
	parts, ok := versionParts(version)
	if !ok {
		return "", nil, false
	}
	return "v" + version, parts, true
}
