// Package format holds the text helpers that Paperboy and its plugins share,
// so the same content never gets two spellings.
package format

import (
	"strconv"
	"strings"
)

// Number formats v with the given decimals in the request's language. German
// uses a decimal comma, English a decimal point.
func Number(v float64, decimals int, lang string) string {
	if v == 0 {
		v = 0 // do not print a negative zero
	}
	s := strconv.FormatFloat(v, 'f', decimals, 64)
	if lang == "de" {
		s = strings.Replace(s, ".", ",", 1)
	}
	return s
}
