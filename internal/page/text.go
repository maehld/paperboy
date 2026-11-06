package page

import (
	"strings"
	"unicode/utf8"
)

// Character sets of text displays (ADR 0044).
const (
	CharsetUTF8    = "utf8"
	CharsetASCII   = "ascii"
	CharsetHD44780 = "hd44780"
)

// ValidCharset reports whether s names a supported character set; empty means utf8.
func ValidCharset(s string) bool {
	switch s {
	case "", CharsetUTF8, CharsetASCII, CharsetHD44780:
		return true
	}
	return false
}

// align cuts or fills s to exactly n characters.
func align(n int, s string, mode byte) string {
	if n < 0 {
		n = 0
	}
	r := []rune(s)
	if len(r) >= n {
		if mode == 'r' {
			return string(r[len(r)-n:])
		}
		return string(r[:n])
	}
	gap := n - len(r)
	switch mode {
	case 'r':
		return strings.Repeat(" ", gap) + s
	case 'c':
		return strings.Repeat(" ", gap/2) + s + strings.Repeat(" ", gap-gap/2)
	}
	return s + strings.Repeat(" ", gap)
}

// fit replaces control characters and cuts or pads a line to n columns.
func fit(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	return align(n, s, 'l')
}

// hd44780Kept are the characters of the A00 ROM the firmware maps itself.
var hd44780Kept = map[rune]bool{'ä': true, 'ö': true, 'ü': true, 'ß': true, '°': true, 'µ': true}

var transliteration = map[rune]string{
	'ä': "ae", 'ö': "oe", 'ü': "ue", 'Ä': "Ae", 'Ö': "Oe", 'Ü': "Ue", 'ß': "ss",
	'°': "o", 'µ': "u", '€': "EUR", '—': "-", '–': "-", '−': "-", '…': "...",
	'„': "\"", '“': "\"", '”': "\"", '‚': "'", '‘': "'", '’': "'",
	'\u00a0': " ", '\u202f': " ", '\u2009': " ", '·': ".", '×': "x",
	'²': "2", '³': "3", '½': "1/2", 'é': "e", 'è': "e", 'á': "a", 'à': "a",
}

// hd44780Upper maps capitals without a ROM glyph to their base letter, so a
// column keeps its width.
var hd44780Upper = map[rune]string{'Ä': "A", 'Ö': "O", 'Ü': "U"}

// Convert maps s to the character set of a text display (ADR 0044).
func Convert(charset, s string) string {
	if charset == "" || charset == CharsetUTF8 {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < utf8.RuneSelf:
			b.WriteRune(r)
		case charset == CharsetHD44780 && hd44780Kept[r]:
			b.WriteRune(r)
		case charset == CharsetHD44780 && hd44780Upper[r] != "":
			b.WriteString(hd44780Upper[r])
		case transliteration[r] != "":
			b.WriteString(transliteration[r])
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}

// NoticeLines word-wraps a notice title and text onto the grid.
func NoticeLines(d Display, title, text string) []string {
	var lines []string
	for _, part := range []string{title, text} {
		lines = append(lines, wrap(Convert(d.Charset, part), d.Width)...)
	}
	if len(lines) > d.Height {
		lines = lines[:d.Height]
	}
	out := make([]string, d.Height)
	for i := range out {
		if i < len(lines) {
			out[i] = lines[i]
		}
		out[i] = fit(out[i], d.Width)
	}
	return out
}

func wrap(s string, n int) []string {
	var lines []string
	cur := ""
	for _, w := range strings.Fields(s) {
		for len([]rune(w)) > n {
			if cur != "" {
				lines, cur = append(lines, cur), ""
			}
			r := []rune(w)
			lines, w = append(lines, string(r[:n])), string(r[n:])
		}
		switch {
		case cur == "":
			cur = w
		case len([]rune(cur))+1+len([]rune(w)) <= n:
			cur += " " + w
		default:
			lines, cur = append(lines, cur), w
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}
