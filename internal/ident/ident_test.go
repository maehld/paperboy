package ident

import "testing"

func TestValid(t *testing.T) {
	for _, name := range []string{"solar", "a", "e1001", "wetter-2", "0"} {
		if !Valid(name) {
			t.Errorf("%q is a valid name", name)
		}
	}
	for _, name := range []string{"", "Solar", "a_b", "a/b", "a.b", "ä", "0123456789012345678901234567890123456789x"} {
		if Valid(name) {
			t.Errorf("%q must be refused", name)
		}
	}
}
