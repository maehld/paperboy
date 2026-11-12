package main

import (
	"reflect"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

// weekEvents expands ICS data (recorded fixture, no network) into parsed
// events for the window around the test week.
func weekEvents(t *testing.T, data string) []parsedEvent {
	t.Helper()
	from := time.Date(2026, 10, 15, 0, 0, 0, 0, berlin)
	occ, warn := expandICS(t, data, from, from.AddDate(0, 0, 40))
	if len(warn) > 0 {
		t.Fatal(warn)
	}
	var out []parsedEvent
	for _, o := range occ {
		out = append(out, parsedEvent{start: o.Start, end: o.End, allDay: o.AllDay, title: o.Title})
	}
	return out
}

func barSpans(bars []weekBar) []string {
	var out []string
	for _, b := range bars {
		out = append(out, b.event.title+":"+string(rune('0'+b.first))+"-"+string(rune('0'+b.last)))
	}
	return out
}

func timedTitles(timed [][]parsedEvent) [][]string {
	out := make([][]string, len(timed))
	for i, list := range timed {
		for _, e := range list {
			out[i] = append(out[i], e.title)
		}
	}
	return out
}

func newWeekRequest(args map[string]any, height int) plugin.WidgetRequest {
	return plugin.WidgetRequest{Widget: "week", Width: 20, Height: height, Args: args}
}

func TestWeekStart(t *testing.T) {
	sat := time.Date(2026, 11, 7, 15, 0, 0, 0, berlin) // Saturday
	if got := weekStart(sat, "de").Format("2006-01-02 Mon"); got != "2026-11-02 Mon" {
		t.Errorf("de: %s", got)
	}
	if got := weekStart(sat, "en").Format("2006-01-02 Mon"); got != "2026-11-01 Sun" {
		t.Errorf("en: %s", got)
	}
	mon := time.Date(2026, 11, 2, 9, 0, 0, 0, berlin)
	if got := weekStart(mon, "de").Format("2006-01-02"); got != "2026-11-02" {
		t.Errorf("monday stays: %s", got)
	}
	if got := weekFirstDay(sat, "de", "today").Format("2006-01-02"); got != "2026-11-07" {
		t.Errorf("today: %s", got)
	}
	if got := weekFirstDay(sat, "de", "week").Format("2006-01-02"); got != "2026-11-02" {
		t.Errorf("week: %s", got)
	}
}

func TestAssignDays(t *testing.T) {
	data := ics(
		vevent("UID:a", "DTSTART;VALUE=DATE:20261103", "DTEND;VALUE=DATE:20261104", "SUMMARY:Holiday"),
		vevent("UID:b", "DTSTART;VALUE=DATE:20261104", "DTEND;VALUE=DATE:20261107", "SUMMARY:Trip"),
		vevent("UID:c", "DTSTART;TZID=Europe/Berlin:20261103T093000", "DTEND;TZID=Europe/Berlin:20261103T103000", "SUMMARY:Standup"),
		vevent("UID:d", "DTSTART;TZID=Europe/Berlin:20261105T220000", "DTEND;TZID=Europe/Berlin:20261106T020000", "SUMMARY:Night"),
		vevent("UID:e", "DTSTART;TZID=Europe/Berlin:20261106T100000", "DTEND;TZID=Europe/Berlin:20261107T000000", "SUMMARY:Ends at midnight"),
		vevent("UID:f", "DTSTART;TZID=Europe/Berlin:20261020T100000", "DTEND;TZID=Europe/Berlin:20261020T110000", "SUMMARY:Outside"),
		vevent("UID:g", "DTSTART;TZID=Europe/Berlin:20261102T080000", "DTEND;TZID=Europe/Berlin:20261102T081500", "SUMMARY:Monday early"),
	)
	start := time.Date(2026, 11, 2, 0, 0, 0, 0, berlin) // Monday
	bars, timed := assignDays(weekEvents(t, data), start, 7, berlin)

	wantBars := []string{"Holiday:1-1", "Trip:2-4", "Night:3-4"}
	if got := barSpans(bars); !reflect.DeepEqual(got, wantBars) {
		t.Errorf("bars = %v, want %v", got, wantBars)
	}
	wantTimed := [][]string{{"Monday early"}, {"Standup"}, nil, nil, {"Ends at midnight"}, nil, nil}
	if got := timedTitles(timed); !reflect.DeepEqual(got, wantTimed) {
		t.Errorf("timed = %v, want %v", got, wantTimed)
	}
}

func TestAssignDaysClipsAtVisibleRange(t *testing.T) {
	data := ics(vevent("UID:a", "DTSTART;VALUE=DATE:20261029", "DTEND;VALUE=DATE:20261110", "SUMMARY:Long"))
	start := time.Date(2026, 11, 2, 0, 0, 0, 0, berlin)
	bars, _ := assignDays(weekEvents(t, data), start, 5, berlin)
	if got := barSpans(bars); !reflect.DeepEqual(got, []string{"Long:0-4"}) {
		t.Errorf("bars = %v", got)
	}
}

func TestAssignDaysTimezone(t *testing.T) {
	// 23:30 UTC on Nov 3 is 00:30 in Berlin on Nov 4.
	data := ics(vevent("UID:a", "DTSTART:20261103T233000Z", "DTEND:20261104T003000Z", "SUMMARY:Late"))
	start := time.Date(2026, 11, 2, 0, 0, 0, 0, berlin)
	_, timed := assignDays(weekEvents(t, data), start, 7, berlin)
	if got := timedTitles(timed); len(got[2]) != 1 || len(got[1]) != 0 {
		t.Errorf("timed = %v", got)
	}
	// The same instant seen from New York is on Nov 3.
	ny := mustLoad("America/New_York")
	nyStart := time.Date(2026, 11, 2, 0, 0, 0, 0, ny)
	_, timed = assignDays(weekEvents(t, data), nyStart, 7, ny)
	if got := timedTitles(timed); len(got[1]) != 1 {
		t.Errorf("ny timed = %v", got)
	}
}

func TestAssignDaysDST(t *testing.T) {
	// Clocks go back on Sunday 2026-10-25; an all-day event on Monday stays
	// in column 1 of a week that starts on Sunday.
	data := ics(vevent("UID:a", "DTSTART;VALUE=DATE:20261026", "DTEND;VALUE=DATE:20261027", "SUMMARY:After"))
	bars, _ := assignDays(weekEvents(t, data), time.Date(2026, 10, 25, 0, 0, 0, 0, berlin), 7, berlin)
	if got := barSpans(bars); !reflect.DeepEqual(got, []string{"After:1-1"}) {
		t.Errorf("bars = %v", got)
	}
}

func TestAssignDaysRecurring(t *testing.T) {
	data := ics(
		vevent("UID:a", "DTSTART;TZID=Europe/Berlin:20261026T090000", "DTEND;TZID=Europe/Berlin:20261026T093000", "RRULE:FREQ=WEEKLY;BYDAY=MO,WE", "SUMMARY:Sync"),
		vevent("UID:b", "DTSTART;VALUE=DATE:20261106", "DTEND;VALUE=DATE:20261107", "RRULE:FREQ=DAILY;COUNT=2", "SUMMARY:Daily"),
	)
	start := time.Date(2026, 11, 2, 0, 0, 0, 0, berlin)
	bars, timed := assignDays(weekEvents(t, data), start, 7, berlin)
	wantTimed := [][]string{{"Sync"}, nil, {"Sync"}, nil, nil, nil, nil}
	if got := timedTitles(timed); !reflect.DeepEqual(got, wantTimed) {
		t.Errorf("timed = %v, want %v", got, wantTimed)
	}
	if got := barSpans(bars); !reflect.DeepEqual(got, []string{"Daily:4-4", "Daily:5-5"}) {
		t.Errorf("bars = %v", got)
	}
}

func TestPackBars(t *testing.T) {
	bars := []weekBar{{first: 0, last: 2}, {first: 1, last: 1}, {first: 2, last: 2}, {first: 3, last: 4}}
	rows := packBars(bars)
	if len(rows) != 2 || len(rows[0]) != 2 || len(rows[1]) != 2 {
		t.Errorf("rows = %v", rows)
	}
}

func TestWeekLines(t *testing.T) {
	data := ics(
		vevent("UID:a", "DTSTART;VALUE=DATE:20261104", "DTEND;VALUE=DATE:20261106", "SUMMARY:Trip"),
		vevent("UID:b", "DTSTART;TZID=Europe/Berlin:20261103T093000", "DTEND;TZID=Europe/Berlin:20261103T103000", "SUMMARY:Standup"),
	)
	p := &calendarPlugin{loc: berlin, lang: "de", now: time.Date(2026, 11, 3, 8, 0, 0, 0, berlin)}
	for _, e := range weekEvents(t, data) {
		p.events = append(p.events, event{Start: e.start.Format(time.RFC3339), End: e.end.Format(time.RFC3339), AllDay: e.allDay, Title: e.title})
	}
	lines := p.weekLines(newWeekRequest(map[string]any{"days": 4, "start": "today"}, 7), p.events)
	want := []string{">Di  3 09:30 Standup", " Mi  4 Trip", " Do  5 Trip", " Fr  6"}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("lines = %q, want %q", lines, want)
	}
	if got := len(p.weekLines(newWeekRequest(map[string]any{"days": 7}, 3), p.events)); got != 3 {
		t.Errorf("limited to the height: %d", got)
	}
}
