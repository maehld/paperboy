package paperboy

import (
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

// buildVersion is the identity of the running binary. Go stamps the module
// version and, with buildvcs, the commit, its time and whether the worktree
// was dirty.
type buildVersion struct {
	Version  string // module version; "(devel)" or empty for local builds
	Revision string // full VCS commit hash, empty without VCS information
	At       string // commit time as RFC 3339, empty without VCS information
	Modified bool   // uncommitted changes were part of the build
	Go       string // Go toolchain that built the binary
}

var build = readBuildInfo()

// Set with -X at build time. Go stamps VCS information by itself, but only when
// the Git repository is part of the build (not in the container image, where
// .dockerignore excludes .git) or when buildvcs is not disabled.
var (
	buildVersionTag string // -X ...buildVersionTag=v1.2.3
	buildCommit     string // -X ...buildCommit=<hash>
	buildTime       string // -X ...buildTime=<RFC 3339>
)

func readBuildInfo() buildVersion {
	v := buildVersion{Go: runtime.Version()}
	if info, ok := debug.ReadBuildInfo(); ok {
		v.Version = info.Main.Version
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				v.Revision = s.Value
			case "vcs.time":
				v.At = s.Value
			case "vcs.modified":
				v.Modified = s.Value == "true"
			}
		}
	}
	return applyBuildOverrides(v)
}

// applyBuildOverrides lets an explicit -X value win over the embedded VCS
// information, so container builds can report the same version.
func applyBuildOverrides(v buildVersion) buildVersion {
	if buildVersionTag != "" {
		v.Version = buildVersionTag
	}
	if buildCommit != "" {
		rev := strings.TrimSuffix(buildCommit, "-dirty")
		v.Revision = rev
		v.Modified = rev != buildCommit || v.Modified
	}
	if buildTime != "" {
		v.At = buildTime
	}
	return v
}

// versionText is the one-line build label shown next to the dashboard title:
// version, short commit and commit date in the configured timezone. A local
// build is called "dev" instead of staging Go's "(devel)".
func versionText(v buildVersion, loc *time.Location) string {
	name := v.Version
	if name == "" || name == "(devel)" {
		name = "dev"
	}
	parts := []string{name}
	if v.Revision != "" {
		// A local build is stamped with a pseudo-version that repeats the
		// commit and date; "dev" plus the short revision reads better.
		if isPseudoVersion(name) {
			parts[0] = "dev"
		}
		rev := v.Revision
		if len(rev) > 9 {
			rev = rev[:9]
		}
		if v.Modified {
			rev += "+dirty"
		}
		parts = append(parts, rev)
	} else if v.Modified {
		parts = append(parts, "dirty")
	}
	if v.At != "" {
		if t, err := time.Parse(time.RFC3339, v.At); err == nil {
			if loc == nil {
				loc = time.UTC
			}
			parts = append(parts, t.In(loc).Format("2006-01-02 15:04"))
		}
	}
	return strings.Join(parts, " · ")
}

// isPseudoVersion reports Go's version for an untagged revision, e.g.
// v0.0.0-20261030223002-0bb9c234b652 (optionally with a +dirty suffix).
func isPseudoVersion(v string) bool {
	base, _, _ := strings.Cut(v, "+")
	prefix, rest, ok := strings.Cut(base, "-")
	if !ok || !strings.HasPrefix(prefix, "v") {
		return false
	}
	stamp, rev, ok := strings.Cut(rest, "-")
	if !ok || len(stamp) != 14 || len(rev) < 12 {
		return false
	}
	for _, r := range stamp {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
