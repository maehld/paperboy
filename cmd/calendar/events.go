package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// occurrence is one concrete appointment in the window.
type occurrence struct {
	Start    time.Time
	End      time.Time
	AllDay   bool
	Title    string
	Location string
	Calendar string
	UID      string
}

// master is a VEVENT as read from the data, before recurrence expansion.
type master struct {
	uid      string
	start    dateValue
	days     int           // length of an all-day event in days
	length   time.Duration // length of a timed event
	title    string
	location string
	rule     *rrule
	exdates  map[string]bool
	rdates   []dateValue
	cancel   bool
	recur    *dateValue // RECURRENCE-ID of an override
}

// occurrences expands the events of one calendar into the window [from, to).
// Problems with single events are returned as warnings and skip only that
// event, so one odd entry does not hide the whole calendar.
func occurrences(roots []*component, calendar string, z *zones, from, to time.Time) ([]occurrence, []string) {
	var masters, overrides []*master
	var warnings []string
	for _, root := range roots {
		for _, c := range root.Children {
			if c.Name != "VEVENT" {
				continue
			}
			m, err := readEvent(c, z)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %s", eventName(c), err))
				continue
			}
			if m.recur != nil {
				overrides = append(overrides, m)
			} else {
				masters = append(masters, m)
			}
		}
	}
	replaced := map[string]bool{}
	for _, o := range overrides {
		replaced[o.uid+"|"+key(*o.recur)] = true
	}

	var out []occurrence
	add := func(m *master, start time.Time) {
		occ := occurrence{
			Start: start, AllDay: m.start.IsDate,
			Title: m.title, Location: m.location, Calendar: calendar, UID: m.uid,
		}
		if m.start.IsDate {
			occ.End = start.AddDate(0, 0, m.days)
		} else {
			occ.End = start.Add(m.length)
		}
		if occ.Start.Before(to) && (occ.End.After(from) || (occ.End.Equal(occ.Start) && !occ.Start.Before(from))) {
			out = append(out, occ)
		}
	}
	for _, m := range masters {
		if m.cancel {
			continue
		}
		starts := []dateValue{m.start}
		if m.rule != nil {
			starts = nil
			m.rule.expand(m.start.Time, to, func(t time.Time) bool {
				starts = append(starts, dateValue{Time: t, IsDate: m.start.IsDate})
				return true
			})
		}
		for _, rd := range m.rdates {
			rd.IsDate = m.start.IsDate
			starts = append(starts, rd)
		}
		seen := map[string]bool{}
		for _, s := range starts {
			k := key(s)
			if seen[k] || m.exdates[k] || replaced[m.uid+"|"+k] {
				continue
			}
			seen[k] = true
			add(m, s.Time)
		}
	}
	for _, o := range overrides {
		if !o.cancel {
			add(o, o.start.Time)
		}
	}
	sortOccurrences(out)
	return out, warnings
}

func sortOccurrences(list []occurrence) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if !a.Start.Equal(b.Start) {
			return a.Start.Before(b.Start)
		}
		if a.AllDay != b.AllDay {
			return a.AllDay
		}
		return a.Title < b.Title
	})
}

// key identifies an instance for EXDATE and RECURRENCE-ID matching.
func key(d dateValue) string {
	if d.IsDate {
		return "D" + dateKey(d.Time)
	}
	return fmt.Sprintf("T%d", d.Time.Unix())
}

func eventName(c *component) string {
	if p, ok := c.prop("SUMMARY"); ok && p.Value != "" {
		return fmt.Sprintf("event %q", unescapeText(p.Value))
	}
	if p, ok := c.prop("UID"); ok {
		return "event " + p.Value
	}
	return "event"
}

func readEvent(c *component, z *zones) (*master, error) {
	m := &master{exdates: map[string]bool{}}
	if p, ok := c.prop("UID"); ok {
		m.uid = strings.TrimSpace(p.Value)
	}
	if p, ok := c.prop("SUMMARY"); ok {
		m.title = strings.TrimSpace(unescapeText(p.Value))
	}
	if p, ok := c.prop("LOCATION"); ok {
		m.location = strings.TrimSpace(unescapeText(p.Value))
	}
	if p, ok := c.prop("STATUS"); ok && strings.EqualFold(strings.TrimSpace(p.Value), "CANCELLED") {
		m.cancel = true
	}
	p, ok := c.prop("DTSTART")
	if !ok {
		return nil, fmt.Errorf("DTSTART missing")
	}
	start, err := z.parseDate(p.Value, p.Params)
	if err != nil {
		return nil, fmt.Errorf("DTSTART: %w", err)
	}
	m.start = start

	switch {
	case hasProp(c, "DTEND"):
		p, _ := c.prop("DTEND")
		end, err := z.parseDate(p.Value, p.Params)
		if err != nil {
			return nil, fmt.Errorf("DTEND: %w", err)
		}
		if start.IsDate {
			m.days = daysBetween(start.Time, end.Time)
		} else {
			m.length = end.Time.Sub(start.Time)
		}
	case hasProp(c, "DURATION"):
		p, _ := c.prop("DURATION")
		days, d, err := parseDuration(strings.TrimSpace(p.Value))
		if err != nil {
			return nil, err
		}
		if start.IsDate {
			m.days = days
		} else {
			m.length = time.Duration(days)*24*time.Hour + d
		}
	default:
		if start.IsDate {
			m.days = 1
		}
	}
	if start.IsDate && m.days < 1 {
		m.days = 1
	}
	if m.length < 0 {
		m.length = 0
	}

	if p, ok := c.prop("RECURRENCE-ID"); ok {
		rid, err := z.parseDate(p.Value, p.Params)
		if err != nil {
			return nil, fmt.Errorf("RECURRENCE-ID: %w", err)
		}
		m.recur = &rid
		return m, nil
	}
	if p, ok := c.prop("RRULE"); ok {
		r, err := parseRRule(strings.TrimSpace(p.Value), z, start.Time.Location())
		if err != nil {
			return nil, fmt.Errorf("RRULE: %w", err)
		}
		m.rule = &r
	}
	for _, p := range c.props("EXDATE") {
		list, err := z.parseDates(p)
		if err != nil {
			return nil, fmt.Errorf("EXDATE: %w", err)
		}
		for _, d := range list {
			d.IsDate = start.IsDate
			m.exdates[key(d)] = true
		}
	}
	for _, p := range c.props("RDATE") {
		list, err := z.parseDates(p)
		if err != nil {
			return nil, fmt.Errorf("RDATE: %w", err)
		}
		m.rdates = append(m.rdates, list...)
	}
	return m, nil
}

func hasProp(c *component, name string) bool {
	_, ok := c.prop(name)
	return ok
}

func daysBetween(a, b time.Time) int {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return int(civil(by, bm, bd).Sub(civil(ay, am, ad)).Hours() / 24)
}
