package page

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maehld/paperboy/pkg/plugin"
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

// clean replaces control characters with spaces.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}

// fit replaces control characters and cuts or pads a line to n columns.
func fit(s string, n int) string {
	return align(n, clean(s), 'l')
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

// Scroll directions of a text box (ADR 0118).
const (
	ScrollHorizontal = "horizontal"
	ScrollVertical   = "vertical"
	ScrollBoth       = "both"
)

// textCellLines turns one text box into the lines it shows and, for a
// scrolling box, its full content and direction. The cell's overflow overrides
// the page default.
func textCellLines(r Result, cell Cell, charset, lang string, now time.Time, loc *time.Location, pageOverflow string) (window, full []string, direction string, scrolling bool) {
	width, height := cell.Span[0], cell.Span[1]
	overflow := cell.Overflow
	if overflow == "" {
		overflow = pageOverflow
	}
	var lines []string
	value, unit := "", ""
	if cell.Text != "" {
		for _, line := range strings.Split(cell.Text, "\n") {
			lines = append(lines, clean(Convert(charset, line)))
		}
	} else {
		value, unit = textValueParts(r, cell, charset, lang, now, loc)
		if strings.Contains(value, "\n") {
			// A text value may carry one line per record, such as an agenda
			// (ADR 0111). The box lays the lines out like literal text; the
			// label opens the first line and the unit closes the last.
			lines = strings.Split(value, "\n")
			lines[0] = textLine(charset, cell.Label, lines[0], "")
			if unit != "" {
				lines[len(lines)-1] += " " + unit
			}
		} else if overflow != OverflowScroll {
			// A one-line value is composed with the cell's alignment, which
			// keeps decimal separators lined up before the unit (ADR 0111).
			window := make([]string, height)
			full := strings.TrimRight(textLine(charset, cell.Label, value, unit), " ")
			if overflow == OverflowWrap && len([]rune(full)) > width {
				for i, line := range wrap(full, width) {
					if i >= height {
						break
					}
					window[i] = clean(align(width, line, alignMode(cell.Align)))
				}
			} else {
				window[0] = composeTextCell(charset, cell.Label, value, unit, width, cell.Align)
			}
			return window, nil, "", false
		} else {
			lines = []string{textLine(charset, cell.Label, value, unit)}
		}
	}
	if overflow == OverflowWrap {
		var wrapped []string
		for _, line := range lines {
			if line == "" {
				wrapped = append(wrapped, "")
				continue
			}
			wrapped = append(wrapped, wrap(line, width)...)
		}
		lines = wrapped
	}
	alignLine := func(line string) string {
		return align(width, line, alignMode(cell.Align))
	}
	if overflow == OverflowScroll {
		wide, tall := false, len(lines) > height
		for _, line := range lines {
			if len([]rune(line)) > width {
				wide = true
			}
		}
		switch {
		case wide && tall:
			direction = ScrollBoth
		case wide:
			direction = ScrollHorizontal
		case tall:
			direction = ScrollVertical
		}
		for i := 0; i < height; i++ {
			line := ""
			if i < len(lines) {
				line = lines[i]
			}
			if wide && direction != ScrollVertical {
				if r := []rune(line); len(r) > width {
					line = string(r[:width])
				}
			}
			window = append(window, alignLine(line))
		}
		if direction != "" {
			return window, lines, direction, true
		}
		return window, nil, "", false
	}
	for i := 0; i < height; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		window = append(window, alignLine(line))
	}
	return window, nil, "", false
}

// textValueParts formats one value and resolves its unit.
func textValueParts(r Result, cell Cell, charset, lang string, now time.Time, loc *time.Location) (value, unit string) {
	value, unit = "", cell.Unit
	if r.Err != nil || r.Value == nil {
		msg := "no value"
		if r.Err != nil {
			msg = firstLine(r.Err.Error())
		}
		value = "? " + msg
	} else {
		value = formatValue(r.Spec, *r.Value, lang, now, loc)
		if unit == "" && r.Spec.Type == plugin.ValueNumber {
			unit = r.Spec.Unit
		}
	}
	return convertLines(charset, value), clean(Convert(charset, unit))
}

// convertLines maps a text value to the charset and cleans each of its lines,
// so a value that carries records keeps its line breaks (ADR 0111).
func convertLines(charset, s string) string {
	parts := strings.Split(Convert(charset, s), "\n")
	for i := range parts {
		parts[i] = clean(parts[i])
	}
	return strings.Join(parts, "\n")
}

// textLine writes label, value and unit without cutting them to a box.
func textLine(charset, label, value, unit string) string {
	label = clean(Convert(charset, label))
	var b strings.Builder
	if label != "" {
		b.WriteString(label)
		b.WriteByte(' ')
	}
	b.WriteString(value)
	if unit != "" {
		b.WriteByte(' ')
		b.WriteString(unit)
	}
	return b.String()
}

// alignMode maps a cell alignment to the byte align expects.
func alignMode(align string) byte {
	if align == AlignRight || align == AlignDecimal {
		return 'r'
	}
	return 'l'
}

// formatValue turns a plugin value into the text a cell shows. Paperboy owns
// the formatting, so the same reading looks the same on every page: numbers
// follow the global language and the declared precision, timestamps the page's
// day, durations and directions their compact form.
func formatValue(spec plugin.ValueSpec, v plugin.Value, lang string, now time.Time, loc *time.Location) string {
	switch v.Type() {
	case plugin.ValueNumber:
		precision := 0
		if spec.Type == plugin.ValueNumber {
			precision = spec.Precision
		}
		s := strconv.FormatFloat(*v.Number, 'f', precision, 64)
		if lang == "de" {
			s = strings.Replace(s, ".", ",", 1)
		}
		return s
	case plugin.ValueText:
		return *v.Text
	case plugin.ValueDuration:
		return formatDuration(*v.Duration)
	case plugin.ValueTimestamp:
		t, err := v.Time(loc)
		if err != nil {
			return "? " + err.Error()
		}
		return formatTimestamp(t, lang, now, loc)
	case plugin.ValueDirection:
		return formatDirection(*v.Direction)
	}
	return "?"
}

// formatDuration writes a duration of seconds compactly: 45s, 12min, 3h 20min,
// 2d 4h.
func formatDuration(seconds int64) string {
	sign := ""
	if seconds < 0 {
		sign, seconds = "-", -seconds
	}
	days := seconds / 86400
	hours := seconds % 86400 / 3600
	minutes := seconds % 3600 / 60
	switch {
	case days > 0:
		return fmt.Sprintf("%s%dd %dh", sign, days, hours)
	case hours > 0 && minutes > 0:
		return fmt.Sprintf("%s%dh %dmin", sign, hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%s%dh", sign, hours)
	case minutes > 0:
		return fmt.Sprintf("%s%dmin", sign, minutes)
	}
	return fmt.Sprintf("%s%ds", sign, seconds)
}

// formatDirection turns a trend direction into the ASCII arrows text displays
// use: ^ up, > flat, v down, doubled for a strong move.
func formatDirection(d int) string {
	switch d {
	case 2:
		return "^^"
	case 1:
		return "^"
	case 0:
		return ">"
	case -1:
		return "v"
	case -2:
		return "vv"
	}
	return "?"
}

// formatTimestamp writes a timestamp relative to the page's day: its time
// today, its date at midnight, and both on another day.
func formatTimestamp(t time.Time, lang string, now time.Time, loc *time.Location) string {
	if loc != nil {
		t, now = t.In(loc), now.In(loc)
	}
	switch {
	case t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0:
		if lang == "de" {
			return t.Format("02.01.2006")
		}
		return t.Format("Jan 2, 2006")
	case t.Year() == now.Year() && t.YearDay() == now.YearDay():
		return t.Format("15:04")
	}
	if lang == "de" {
		return t.Format("02.01. 15:04")
	}
	return t.Format("Jan 2, 15:04")
}

// composeTextCell lays label, value and unit out in exactly width characters.
// The charset conversion happens first, so the display's columns are counted
// after transliteration (ä becomes ae on ascii).
func composeTextCell(charset, label, value, unit string, width int, mode string) string {
	if width < 0 {
		width = 0
	}
	label = clean(Convert(charset, label))
	value = clean(Convert(charset, value))
	unit = clean(Convert(charset, unit))
	prefix, suffix := "", ""
	if label != "" {
		prefix = label + " "
	}
	if unit != "" {
		suffix = " " + unit
	}
	switch mode {
	case AlignRight:
		return align(width, prefix+value+suffix, 'r')
	case AlignDecimal:
		room := width - len([]rune(prefix)) - len([]rune(suffix))
		if room < 0 { // the label yields before the value and the unit
			prefix = ""
			room = width - len([]rune(suffix))
		}
		if room < 0 {
			suffix = ""
			room = width
		}
		v := []rune(value)
		if len(v) > room {
			v = v[len(v)-room:]
		}
		return fit(prefix+strings.Repeat(" ", room-len(v))+string(v)+suffix, width)
	}
	return fit(prefix+value+suffix, width)
}
