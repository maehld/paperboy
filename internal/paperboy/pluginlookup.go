package paperboy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/maehld/paperboy/internal/semver"
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
	var bestVersion semver.Version
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		parsed, ver, ok := parsePluginProgram(name, e.Name())
		if !ok {
			continue
		}
		if best == "" || semver.Compare(ver, bestVersion) > 0 {
			best, bestVersion = parsed, ver
		}
	}
	return best
}

// parsePluginProgram returns the version of a downloaded plugin program
// <name>-v<version> (with the Windows .exe suffix when there is one); the
// version may carry a prerelease suffix (ADR 0089).
func parsePluginProgram(name, file string) (string, semver.Version, bool) {
	file = strings.TrimSuffix(file, ".exe")
	rest, ok := strings.CutPrefix(file, name+"-")
	if !ok {
		return "", semver.Version{}, false
	}
	raw, ok := strings.CutPrefix(rest, "v")
	if !ok {
		return "", semver.Version{}, false
	}
	ver, ok := semver.Parse(raw)
	if !ok {
		return "", semver.Version{}, false
	}
	return "v" + raw, ver, true
}
