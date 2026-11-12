// Package version parses and compares the versions that name plugin programs
// and firmware images (ADR 0089): 1.4.0, 1.4.0-rc.1, 1.4.0-beta.3.
//
// A version is a dotted number followed by an optional prerelease suffix and
// orders like SemVer: 1.4.0-rc.1 < 1.4.0-rc.2 < 1.4.0, and missing numeric
// parts count as zero, so 1.4 and 1.4.0 are the same version.
package semver

import (
	"strconv"
	"strings"
)

// Version is one parsed version.
type Version struct {
	parts []int    // numeric parts, at least one
	pre   []string // prerelease identifiers; empty for a final version
	text  string   // as parsed, for String
}

// Parse reads a version like 1.4, 1.4.0 or 1.4.0-rc.1. A leading "v" is not
// part of a version: the callers strip it where pins may carry one. ok is false
// for names that are no version at all, e.g. a plugin's file name or "next".
func Parse(s string) (Version, bool) {
	main, suffix, hasPre := strings.Cut(s, "-")
	if main == "" {
		return Version{}, false
	}
	v := Version{text: s}
	for _, p := range strings.Split(main, ".") {
		n, ok := number(p)
		if !ok {
			return Version{}, false
		}
		v.parts = append(v.parts, n)
	}
	if hasPre {
		for _, id := range strings.Split(suffix, ".") {
			if !identifier(id) {
				return Version{}, false
			}
			v.pre = append(v.pre, id)
		}
	}
	return v, true
}

// String returns the version as it was parsed.
func (v Version) String() string { return v.text }

// Prerelease reports whether the version carries a prerelease suffix.
func (v Version) Prerelease() bool { return len(v.pre) > 0 }

// Compare orders two versions like SemVer: -1 when a is lower, 0 when they are
// equal, 1 when a is higher.
func Compare(a, b Version) int {
	for i := 0; i < max(len(a.parts), len(b.parts)); i++ {
		x, y := part(a.parts, i), part(b.parts, i)
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1 // a final release is higher than its prereleases
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < max(len(a.pre), len(b.pre)); i++ {
		if i >= len(a.pre) {
			return -1 // fewer identifiers is lower: rc < rc.1
		}
		if i >= len(b.pre) {
			return 1
		}
		if c := compareIdentifier(a.pre[i], b.pre[i]); c != 0 {
			return c
		}
	}
	return 0
}

// part returns the numeric part at i, or zero when the version is shorter.
func part(parts []int, i int) int {
	if i < len(parts) {
		return parts[i]
	}
	return 0
}

// compareIdentifier orders two prerelease identifiers: numeric ones by their
// number, a number below an alphanumeric one, the rest lexically (SemVer §11).
func compareIdentifier(a, b string) int {
	an, aok := number(a)
	bn, bok := number(b)
	switch {
	case aok && bok:
		switch {
		case an < bn:
			return -1
		case an > bn:
			return 1
		}
		return 0
	case aok:
		return -1
	case bok:
		return 1
	}
	return strings.Compare(a, b)
}

// number parses a numeric part: digits only, no sign, no space.
func number(s string) (int, bool) {
	if s == "" || len(s) > 9 {
		return 0, false // nine digits is far more than any version needs
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

// identifier reports whether s may be a prerelease identifier: letters, digits
// and inner hyphens, as in rc.1, beta-2 or 20261112.
func identifier(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '-':
		default:
			return false
		}
	}
	return true
}
