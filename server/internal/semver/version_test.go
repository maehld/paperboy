package semver

import "testing"

func parse(t *testing.T, s string) Version {
	t.Helper()
	v, ok := Parse(s)
	if !ok {
		t.Fatalf("%q does not parse", s)
	}
	return v
}

// TestParseKeepsTheShapesOfTheNames covers the versions the names carry
// (ADR 0089).
func TestParseKeepsTheShapesOfTheNames(t *testing.T) {
	for _, s := range []string{"1", "2", "1.4", "1.4.0", "0.9.0", "1.4.0-rc.1", "1.4.0-beta.3", "2.4.0-rc-1", "1.4.0-20261112"} {
		if got := parse(t, s).String(); got != s {
			t.Fatalf("%q: %q", s, got)
		}
	}
	for _, s := range []string{"", "v1.4.0", "next", "1.4.0-", "1.4.0-rc..1", ".1", "1.", "1..2", "1.4.x", "1.4.0-rc 1", "1.4.0-rc/1"} {
		if _, ok := Parse(s); ok {
			t.Fatalf("%q must not parse", s)
		}
	}
}

// TestCompareFollowsSemver orders prereleases below their final and numbers
// above text (ADR 0089).
func TestCompareFollowsSemver(t *testing.T) {
	ordered := []string{
		"1.3.9",
		"1.4.0-alpha",
		"1.4.0-alpha.1",
		"1.4.0-beta.2",
		"1.4.0-beta.11",
		"1.4.0-rc.1",
		"1.4.0-rc.2",
		"1.4.0",
		"1.4.1-rc.1",
		"1.4.1",
		"1.5",
		"1.10.0",
	}
	for i := 0; i+1 < len(ordered); i++ {
		a, b := parse(t, ordered[i]), parse(t, ordered[i+1])
		if c := Compare(a, b); c >= 0 {
			t.Fatalf("%s < %s: %d", ordered[i], ordered[i+1], c)
		}
		if c := Compare(b, a); c <= 0 {
			t.Fatalf("%s > %s: %d", ordered[i+1], ordered[i], c)
		}
	}
}

// TestComparePadsMissingParts keeps 1.4 and 1.4.0 the same version, as the
// firmware comparison did before (ADR 0043).
func TestComparePadsMissingParts(t *testing.T) {
	for _, pair := range [][2]string{
		{"1.2", "1.2.0"},
		{"1", "1.0.0"},
		{"1.4.0-rc.1", "1.4.0-rc.1.0"}, // an extra identifier is not no identifier
	} {
		a, b := parse(t, pair[0]), parse(t, pair[1])
		got := Compare(a, b)
		want := 0
		if pair[0] == "1.4.0-rc.1" {
			want = -1 // fewer identifiers is lower
		}
		if got != want {
			t.Fatalf("%s against %s: %d, want %d", pair[0], pair[1], got, want)
		}
	}
}

// TestPrerelease tells a suffix from a final version, which is what a channel
// asks (ADR 0089).
func TestPrerelease(t *testing.T) {
	for s, want := range map[string]bool{
		"1.4.0":        false,
		"1.4":          false,
		"1.4.0-rc.1":   true,
		"1.4.0-beta.3": true,
	} {
		if got := parse(t, s).Prerelease(); got != want {
			t.Fatalf("%s: %v", s, got)
		}
	}
}
