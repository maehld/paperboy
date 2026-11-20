// Package ident validates the names that pages, plans, display types, plugins,
// widgets, actions and devices share: one lowercase word of a-z, 0-9 and '-',
// at most 40 characters. A name becomes a file name, so the rule is strict.
package ident

// Valid reports whether s is a valid name.
func Valid(s string) bool {
	if s == "" || len(s) > 40 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}
