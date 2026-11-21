package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	addressbookHomeQuery = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:carddav"><d:prop><c:addressbook-home-set/></d:prop></d:propfind>`
	addressbooksQuery = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:carddav"><d:prop><d:resourcetype/><d:displayname/></d:prop></d:propfind>`
	addressbookQuery = `<?xml version="1.0" encoding="utf-8"?>
<c:addressbook-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:carddav">
  <d:prop><c:address-data/></d:prop>
  <c:filter test="anyof"/>
</c:addressbook-query>`
)

// addressbookRef is one CardDAV addressbook.
type addressbookRef struct {
	Name string
	URL  *url.URL
}

// discoverAddressbooks follows the CardDAV chain (RFC 6352): well-known,
// current-user-principal, addressbook-home-set, then the addressbook
// collections below it. The fallback to the URL itself matches discover() for
// CalDAV, so a URL that already points at a principal or a home works too.
func (c *client) discoverAddressbooks(ctx context.Context, base *url.URL) ([]addressbookRef, error) {
	start := base
	if base.Path == "" || base.Path == "/" {
		wk, _ := base.Parse("/.well-known/carddav")
		if final, ms, err := c.propfind(ctx, wk, "0", principalQuery); err == nil && hrefOf(ms, func(p davProp) string { return p.Principal.Href }) != "" {
			start = final
		}
	}
	final, ms, err := c.propfind(ctx, start, "0", principalQuery)
	if err != nil {
		return nil, err
	}
	principal := final
	if href := hrefOf(ms, func(p davProp) string { return p.Principal.Href }); href != "" {
		if principal, err = final.Parse(href); err != nil {
			return nil, fmt.Errorf("invalid principal URL %q", href)
		}
	}

	final, ms, err = c.propfind(ctx, principal, "0", addressbookHomeQuery)
	if err != nil {
		return nil, err
	}
	home := final
	if href := hrefOf(ms, func(p davProp) string { return p.AddressHome.Href }); href != "" {
		if home, err = final.Parse(href); err != nil {
			return nil, fmt.Errorf("invalid addressbook home URL %q", href)
		}
	}

	final, ms, err = c.propfind(ctx, home, "1", addressbooksQuery)
	if err != nil {
		return nil, err
	}
	var out []addressbookRef
	seen := map[string]bool{}
	for _, r := range ms.Responses {
		var name string
		addressbook := false
		for _, p := range r.props() {
			if p.ResourceType.Addressbook != nil {
				addressbook = true
			}
			if p.DisplayName != "" {
				name = strings.TrimSpace(p.DisplayName)
			}
		}
		if !addressbook {
			continue
		}
		u, err := final.Parse(strings.TrimSpace(r.Href))
		if err != nil {
			continue
		}
		if seen[u.String()] {
			continue
		}
		seen[u.String()] = true
		if name == "" {
			name = lastSegment(u.Path)
		}
		out = append(out, addressbookRef{Name: name, URL: u})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no addressbooks found", redact(home))
	}
	return out, nil
}

// cards asks one addressbook for all vCards with an addressbook-query REPORT
// (RFC 6352 8.6). An empty filter matches every card.
func (c *client) cards(ctx context.Context, book addressbookRef) ([]byte, error) {
	final, data, status, err := c.do(ctx, "REPORT", book.URL, "1", []byte(addressbookQuery))
	if err != nil {
		return nil, err
	}
	if status != http.StatusMultiStatus {
		return nil, statusError(final, status)
	}
	var ms multistatus
	if err := xml.Unmarshal(data, &ms); err != nil {
		return nil, fmt.Errorf("%s: invalid WebDAV response: %w", redact(final), err)
	}
	var out bytes.Buffer
	for _, r := range ms.Responses {
		for _, p := range r.props() {
			if strings.TrimSpace(p.AddressData) != "" {
				out.WriteString(strings.TrimSpace(p.AddressData))
				out.WriteString("\r\n")
			}
		}
	}
	return out.Bytes(), nil
}

// contactBirthday is the birthday of one contact. A zero year means the
// contact does not carry one; the date still works and the age stays away.
type contactBirthday struct {
	uid   string
	name  string
	year  int
	month time.Month
	day   int
}

func (b contactBirthday) key() string {
	if b.uid != "" {
		return "uid:" + b.uid
	}
	return fmt.Sprintf("name:%s|%04d-%02d-%02d", strings.ToLower(b.name), b.year, b.month, b.day)
}

// readBirthdays reads the birthdays of all addressbooks. iCloud returns a
// contact in more than one addressbook ("All Contacts" and its groups), so the
// UID deduplicates them. A failing addressbook fails the load like a failing
// calendar; a single bad card only becomes a warning.
func readBirthdays(ctx context.Context, c *client, books []addressbookRef) ([]contactBirthday, []string, error) {
	var all []contactBirthday
	var warnings []string
	seen := map[string]bool{}
	for _, book := range books {
		raw, err := c.cards(ctx, book)
		if err != nil {
			return nil, nil, fmt.Errorf("addressbook %q: %w", book.Name, err)
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		cards, warn := parseVCards(raw)
		warnings = append(warnings, prefixAddressbook(book.Name, warn)...)
		list, warn := parseContactBirthdays(cards)
		warnings = append(warnings, prefixAddressbook(book.Name, warn)...)
		for _, b := range list {
			key := b.key()
			if seen[key] {
				continue
			}
			seen[key] = true
			all = append(all, b)
		}
	}
	return all, warnings, nil
}

func prefixAddressbook(name string, list []string) []string {
	out := make([]string, len(list))
	for i, w := range list {
		out[i] = fmt.Sprintf("addressbook %q: %s", name, w)
	}
	return out
}

// parseVCards reads the VCARD blocks of an address-data property. It is more
// tolerant than parseICS: a malformed line is a warning and only drops that
// line, so one odd contact does not hide the others.
func parseVCards(raw []byte) ([]*component, []string) {
	var out []*component
	var warnings []string
	var cur *component
	for n, line := range unfold(raw) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		p, err := parseLine(line)
		if err != nil {
			if cur != nil {
				warnings = append(warnings, fmt.Sprintf("line %d: %v", n+1, err))
			}
			continue
		}
		switch p.Name {
		case "BEGIN":
			if strings.EqualFold(strings.TrimSpace(p.Value), "VCARD") {
				cur = &component{Name: "VCARD"}
			} else {
				cur = nil
			}
		case "END":
			if strings.EqualFold(strings.TrimSpace(p.Value), "VCARD") {
				if cur != nil {
					out = append(out, cur)
				}
				cur = nil
			}
		default:
			if cur == nil {
				continue
			}
			// vCard property groups ("item1.BDAY") do not change the property.
			if i := strings.LastIndexByte(p.Name, '.'); i >= 0 {
				p.Name = p.Name[i+1:]
			}
			cur.Props = append(cur.Props, p)
		}
	}
	if cur != nil {
		warnings = append(warnings, "unterminated VCARD")
	}
	return out, warnings
}

func parseContactBirthdays(cards []*component) ([]contactBirthday, []string) {
	var out []contactBirthday
	var warnings []string
	for _, card := range cards {
		bday, ok := card.prop("BDAY")
		if !ok {
			continue // most contacts carry no birthday; that is not a warning
		}
		name := contactName(card)
		if name == "" {
			warnings = append(warnings, fmt.Sprintf("contact %q: birthday without a name", contactRef(card)))
			continue
		}
		year, month, day, err := parseBDAY(bday)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("contact %q: BDAY: %v", name, err))
			continue
		}
		uid := ""
		if p, ok := card.prop("UID"); ok {
			uid = strings.TrimSpace(p.Value)
		}
		out = append(out, contactBirthday{uid: uid, name: name, year: year, month: month, day: day})
	}
	return out, warnings
}

func contactName(card *component) string {
	if p, ok := card.prop("FN"); ok {
		if name := strings.TrimSpace(oneLine(unescapeText(p.Value))); name != "" {
			return name
		}
	}
	p, ok := card.prop("N")
	if !ok {
		return ""
	}
	// N is family;given;additional;prefix;suffix.
	parts := strings.Split(p.Value, ";")
	for i := range parts {
		parts[i] = strings.TrimSpace(unescapeText(parts[i]))
	}
	var name []string
	for _, i := range []int{3, 1, 2, 0, 4} {
		if i < len(parts) && parts[i] != "" {
			name = append(name, parts[i])
		}
	}
	return strings.Join(name, " ")
}

func contactRef(card *component) string {
	if p, ok := card.prop("UID"); ok && strings.TrimSpace(p.Value) != "" {
		return strings.TrimSpace(p.Value)
	}
	if p, ok := card.prop("FN"); ok && strings.TrimSpace(p.Value) != "" {
		return strings.TrimSpace(unescapeText(p.Value))
	}
	return "unknown"
}

// parseBDAY reads the date of birth from a BDAY property. vCard 3.0 and 4.0
// both allow a year or a date without one; Apple stores the latter as
// X-APPLE-OMIT-YEAR=1604 with a placeholder year.
func parseBDAY(p property) (year int, month time.Month, day int, err error) {
	raw := strings.TrimSpace(p.Value)
	if raw == "" {
		return 0, 0, 0, fmt.Errorf("empty date")
	}
	// A date-time carries a time and perhaps a zone after T; only the date
	// matters for a birthday.
	if i := strings.IndexAny(raw, "T "); i >= 0 {
		raw = raw[:i]
	}
	raw = strings.TrimSpace(raw)
	noYear := strings.HasPrefix(raw, "--")
	if noYear {
		raw = strings.TrimPrefix(raw, "--")
	}
	raw = strings.ReplaceAll(raw, "-", "")

	switch len(raw) {
	case 8:
		y, err1 := strconv.Atoi(raw[0:4])
		m, err2 := strconv.Atoi(raw[4:6])
		d, err3 := strconv.Atoi(raw[6:8])
		if err1 != nil || err2 != nil || err3 != nil {
			return 0, 0, 0, fmt.Errorf("invalid date %q", p.Value)
		}
		year, month, day = y, time.Month(m), d
	case 4:
		if !noYear {
			return 0, 0, 0, fmt.Errorf("invalid date %q", p.Value)
		}
		m, err1 := strconv.Atoi(raw[0:2])
		d, err2 := strconv.Atoi(raw[2:4])
		if err1 != nil || err2 != nil {
			return 0, 0, 0, fmt.Errorf("invalid date %q", p.Value)
		}
		month, day = time.Month(m), d
	default:
		return 0, 0, 0, fmt.Errorf("invalid date %q", p.Value)
	}

	// Apple's placeholder year and some exports without the parameter.
	if noYear || p.Params["X-APPLE-OMIT-YEAR"] != "" || year == 1604 {
		year = 0
	}
	check := year
	if check == 0 {
		check = 2000 // a leap year, so 29 February stays valid
	}
	t := time.Date(check, month, day, 0, 0, 0, 0, time.UTC)
	if int(t.Month()) != int(month) || t.Day() != day {
		return 0, 0, 0, fmt.Errorf("invalid date %q", p.Value)
	}
	return year, month, day, nil
}

// birthdayOccurrences turns the contact birthdays into all-day occurrences in
// [from, to). A birthday on 29 February falls on 28 February in a common year,
// like the RRULE expansion of a birthday calendar.
func birthdayOccurrences(list []contactBirthday, calendar string, from, to time.Time, loc *time.Location) []occurrence {
	var out []occurrence
	for _, b := range list {
		for year := from.Year(); year <= to.Year(); year++ {
			day := b.day
			if b.month == time.February && day == 29 && !leapYear(year) {
				day = 28
			}
			start := time.Date(year, b.month, day, 0, 0, 0, 0, loc)
			if start.Before(from) || !start.Before(to) {
				continue
			}
			var first time.Time
			if b.year > 0 {
				first = time.Date(b.year, b.month, b.day, 0, 0, 0, 0, loc)
			}
			out = append(out, occurrence{
				Start: start, End: start.AddDate(0, 0, 1), First: first,
				AllDay: true, Title: b.name, Calendar: calendar, UID: b.uid,
				Birthday: true,
			})
		}
	}
	return out
}

func leapYear(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

// contactsCalendar is the source name the CardDAV birthdays appear under in
// the widget filter and the calendar list.
func contactsCalendar(language string) string {
	if language == "de" {
		return "Kontakte"
	}
	return "Contacts"
}
