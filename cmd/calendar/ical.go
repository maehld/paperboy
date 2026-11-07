package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// property is one content line of an iCalendar object (RFC 5545 3.1).
type property struct {
	Name   string
	Params map[string]string
	Value  string
}

// component is a BEGIN/END block with its properties and children.
type component struct {
	Name     string
	Props    []property
	Children []*component
}

func (c *component) prop(name string) (property, bool) {
	for _, p := range c.Props {
		if p.Name == name {
			return p, true
		}
	}
	return property{}, false
}

func (c *component) props(name string) []property {
	var out []property
	for _, p := range c.Props {
		if p.Name == name {
			out = append(out, p)
		}
	}
	return out
}

// parseICS reads one or more VCALENDAR objects and returns them as components.
func parseICS(raw []byte) ([]*component, error) {
	lines := unfold(raw)
	var roots []*component
	var stack []*component
	for n, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		p, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n+1, err)
		}
		switch p.Name {
		case "BEGIN":
			c := &component{Name: strings.ToUpper(p.Value)}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, c)
			} else {
				roots = append(roots, c)
			}
			stack = append(stack, c)
		case "END":
			if len(stack) == 0 || stack[len(stack)-1].Name != strings.ToUpper(p.Value) {
				return nil, fmt.Errorf("line %d: unexpected END:%s", n+1, p.Value)
			}
			stack = stack[:len(stack)-1]
		default:
			if len(stack) == 0 {
				continue // content outside a component is ignored
			}
			cur := stack[len(stack)-1]
			cur.Props = append(cur.Props, p)
		}
	}
	if len(stack) != 0 {
		return nil, fmt.Errorf("unterminated component %s", stack[len(stack)-1].Name)
	}
	if len(roots) == 0 {
		return nil, errors.New("no calendar data")
	}
	return roots, nil
}

// unfold joins continuation lines (a line break followed by a space or tab).
func unfold(raw []byte) []string {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	var out []string
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) && len(out) > 0 {
			out[len(out)-1] += line[1:]
			continue
		}
		out = append(out, line)
	}
	return out
}

// parseLine splits NAME;PARAM=VALUE;...:VALUE, honouring quoted parameters.
func parseLine(line string) (property, error) {
	p := property{Params: map[string]string{}}
	i := strings.IndexAny(line, ";:")
	if i <= 0 {
		return p, fmt.Errorf("invalid content line %q", truncate(line, 40))
	}
	p.Name = strings.ToUpper(line[:i])
	rest := line[i:]
	for strings.HasPrefix(rest, ";") {
		rest = rest[1:]
		eq := strings.IndexByte(rest, '=')
		if eq < 0 {
			return p, fmt.Errorf("invalid parameter in %q", truncate(line, 40))
		}
		key := strings.ToUpper(rest[:eq])
		rest = rest[eq+1:]
		var val string
		if strings.HasPrefix(rest, `"`) {
			end := strings.IndexByte(rest[1:], '"')
			if end < 0 {
				return p, fmt.Errorf("unterminated quote in %q", truncate(line, 40))
			}
			val = rest[1 : end+1]
			rest = rest[end+2:]
		} else {
			end := strings.IndexAny(rest, ";:")
			if end < 0 {
				return p, fmt.Errorf("missing value in %q", truncate(line, 40))
			}
			val = rest[:end]
			rest = rest[end:]
		}
		p.Params[key] = val
	}
	if !strings.HasPrefix(rest, ":") {
		return p, fmt.Errorf("missing value in %q", truncate(line, 40))
	}
	p.Value = rest[1:]
	return p, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// unescapeText decodes a TEXT value (RFC 5545 3.3.11).
func unescapeText(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n', 'N':
			b.WriteByte('\n')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// oneLine joins the lines of a decoded TEXT value with ", " for single-line
// display; Apple puts name, street and city of a location on separate lines.
func oneLine(s string) string {
	var parts []string
	for _, l := range strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' }) {
		if l = strings.TrimSpace(l); l != "" {
			parts = append(parts, l)
		}
	}
	return strings.Join(parts, ", ")
}

// zones resolves TZID parameters. Most servers, iCloud included, use IANA
// names; Exchange and Outlook use Windows names, which are mapped for the
// common European and US zones. Unknown names fall back to the configured
// timezone, which is right for the usual single-zone household.
type zones struct {
	fallback *time.Location
	cache    map[string]*time.Location
	unknown  map[string]bool
}

func newZones(fallback *time.Location) *zones {
	return &zones{fallback: fallback, cache: map[string]*time.Location{}, unknown: map[string]bool{}}
}

var windowsZones = map[string]string{
	"W. Europe Standard Time":         "Europe/Berlin",
	"Central Europe Standard Time":    "Europe/Budapest",
	"Romance Standard Time":           "Europe/Paris",
	"Central European Standard Time":  "Europe/Warsaw",
	"GMT Standard Time":               "Europe/London",
	"E. Europe Standard Time":         "Europe/Chisinau",
	"FLE Standard Time":               "Europe/Kiev",
	"GTB Standard Time":               "Europe/Bucharest",
	"Russian Standard Time":           "Europe/Moscow",
	"Eastern Standard Time":           "America/New_York",
	"Central Standard Time":           "America/Chicago",
	"Mountain Standard Time":          "America/Denver",
	"Pacific Standard Time":           "America/Los_Angeles",
	"UTC":                             "UTC",
	"Coordinated Universal Time":      "UTC",
	"Greenwich Standard Time":         "Atlantic/Reykjavik",
	"Tokyo Standard Time":             "Asia/Tokyo",
	"China Standard Time":             "Asia/Shanghai",
	"India Standard Time":             "Asia/Kolkata",
	"AUS Eastern Standard Time":       "Australia/Sydney",
	"W. Central Africa Standard Time": "Africa/Lagos",
}

func (z *zones) get(tzid string) *time.Location {
	tzid = strings.TrimSpace(strings.TrimPrefix(tzid, "/"))
	if tzid == "" {
		return z.fallback
	}
	if loc, ok := z.cache[tzid]; ok {
		return loc
	}
	name := tzid
	if mapped, ok := windowsZones[tzid]; ok {
		name = mapped
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		loc = z.fallback
		z.unknown[tzid] = true
	}
	z.cache[tzid] = loc
	return loc
}

// dateValue is a DATE or DATE-TIME value. Dates and floating times use the
// fallback zone.
type dateValue struct {
	Time   time.Time
	IsDate bool
}

// parseDate reads one value of a DTSTART-like property.
func (z *zones) parseDate(value string, params map[string]string) (dateValue, error) {
	value = strings.TrimSpace(value)
	if params["VALUE"] == "DATE" || len(value) == 8 {
		t, err := time.ParseInLocation("20060102", value, z.fallback)
		if err != nil {
			return dateValue{}, fmt.Errorf("invalid date %q", value)
		}
		return dateValue{Time: t, IsDate: true}, nil
	}
	if strings.HasSuffix(value, "Z") {
		t, err := time.Parse("20060102T150405Z", value)
		if err != nil {
			return dateValue{}, fmt.Errorf("invalid date-time %q", value)
		}
		return dateValue{Time: t}, nil
	}
	loc := z.fallback
	if tzid, ok := params["TZID"]; ok {
		loc = z.get(tzid)
	}
	t, err := time.ParseInLocation("20060102T150405", value, loc)
	if err != nil {
		return dateValue{}, fmt.Errorf("invalid date-time %q", value)
	}
	return dateValue{Time: t}, nil
}

// parseDates reads a comma-separated list such as EXDATE or RDATE.
func (z *zones) parseDates(p property) ([]dateValue, error) {
	var out []dateValue
	for _, part := range strings.Split(p.Value, ",") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		if p.Params["VALUE"] == "PERIOD" || strings.Contains(part, "/") {
			part = part[:strings.IndexByte(part, '/')]
		}
		d, err := z.parseDate(part, p.Params)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// parseDuration reads an iCalendar duration such as P1D, PT1H30M or -P2W.
func parseDuration(s string) (days int, d time.Duration, err error) {
	orig := s
	sign := 1
	switch {
	case strings.HasPrefix(s, "-"):
		sign, s = -1, s[1:]
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	}
	if !strings.HasPrefix(s, "P") {
		return 0, 0, fmt.Errorf("invalid duration %q", orig)
	}
	s = s[1:]
	inTime := false
	num := ""
	for _, ch := range s {
		switch {
		case ch >= '0' && ch <= '9':
			num += string(ch)
		case ch == 'T':
			inTime = true
		default:
			n, convErr := strconv.Atoi(num)
			if convErr != nil {
				return 0, 0, fmt.Errorf("invalid duration %q", orig)
			}
			num = ""
			switch {
			case ch == 'W' && !inTime:
				days += 7 * n
			case ch == 'D' && !inTime:
				days += n
			case ch == 'H' && inTime:
				d += time.Duration(n) * time.Hour
			case ch == 'M' && inTime:
				d += time.Duration(n) * time.Minute
			case ch == 'S' && inTime:
				d += time.Duration(n) * time.Second
			default:
				return 0, 0, fmt.Errorf("invalid duration %q", orig)
			}
		}
	}
	if num != "" {
		return 0, 0, fmt.Errorf("invalid duration %q", orig)
	}
	return sign * days, time.Duration(sign) * d, nil
}
