// Package format holds small text helpers that Paperboy and its built-in
// plugin share, so the same content never gets two spellings.
package format

import (
	"fmt"
	"strings"
	"time"
)

// FirstLine returns s up to its first line break.
func FirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// Runtime compacts a duration into "~6 h" or "~3 d"; zero and negative answer
// nothing.
func Runtime(d time.Duration) string {
	switch {
	case d <= 0:
		return ""
	case d < 48*time.Hour:
		hours := int(d.Round(time.Hour) / time.Hour)
		if hours < 1 {
			hours = 1
		}
		return fmt.Sprintf("~%d h", hours)
	}
	return fmt.Sprintf("~%d d", int(d.Round(24*time.Hour)/(24*time.Hour)))
}
