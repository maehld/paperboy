package main

import (
	"image"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

// agendaPlugin returns a plugin at Tuesday 2026-11-03 08:00 Berlin with the
// events of data and a configured range of 14 days.
func agendaPlugin(t *testing.T, data string) *calendarPlugin {
	t.Helper()
	p := &calendarPlugin{loc: berlin, lang: "de", now: time.Date(2026, 11, 3, 8, 0, 0, 0, berlin)}
	p.opts.days = 14
	for _, e := range weekEvents(t, data) {
		p.events = append(p.events, event{Start: e.start.Format(time.RFC3339), End: e.end.Format(time.RFC3339), AllDay: e.allDay, Title: e.title})
	}
	return p
}

func agendaText(t *testing.T, p *calendarPlugin, args map[string]any, height int) []string {
	t.Helper()
	lines, err := p.Text(&plugin.Request{}, plugin.WidgetRequest{Widget: "agenda", Width: 30, Height: height, Args: args})
	if err != nil {
		t.Fatal(err)
	}
	return lines
}

// agendaCell lays the agenda out as the image path does for a cell of width ×
// height pixels and lines lines.
func agendaCell(t *testing.T, p *calendarPlugin, args map[string]any, lines, width, height int) ([]agendaLine, string) {
	t.Helper()
	area := image.Rect(0, 0, width, height)
	got, _, more := p.agendaLayout(lines, p.agendaArgs(args, lines > 1), area, 0.6*float64(height/lines))
	return got, more
}

func TestAgendaSmallCell(t *testing.T) {
	p := agendaPlugin(t, ics(
		vevent("UID:a", "DTSTART;TZID=Europe/Berlin:20261103T150000", "DTEND;TZID=Europe/Berlin:20261103T160000", "SUMMARY:Zahnarzt"),
		vevent("UID:b", "DTSTART;TZID=Europe/Berlin:20261103T170000", "DTEND;TZID=Europe/Berlin:20261103T180000", "SUMMARY:Abendessen"),
	))
	// One line on a Sticky's 6×2 cell: the event instead of a heading, and the
	// summary behind it rather than an empty cell (issue #160).
	lines, more := agendaCell(t, p, map[string]any{"days": 1}, 1, 772, 92)
	if len(lines) != 1 || !lines[0].event || lines[0].heading {
		t.Fatalf("one line = %+v", lines)
	}
	if lines[0].text != "Zahnarzt" || more != "· +1 weitere" {
		t.Errorf("one line = %+v, summary %q", lines, more)
	}
	// A text display keeps the summary on a line of its own, even with headings
	// asked for in a cell that has no room for one.
	if got, want := agendaText(t, p, map[string]any{"days": 1, "headings": true}, 2), []string{"15:00 Zahnarzt", "+1 weitere"}; !reflect.DeepEqual(got, want) {
		t.Errorf("text = %q, want %q", got, want)
	}
	// A free day still says so.
	lines, _ = agendaCell(t, agendaPlugin(t, ics()), map[string]any{"days": 1}, 1, 772, 92)
	if len(lines) != 1 || lines[0].event || lines[0].text != "Keine Termine" {
		t.Errorf("free day = %+v", lines)
	}
}

func TestAgendaTruncationKeepsEvents(t *testing.T) {
	p := agendaPlugin(t, ics(
		vevent("UID:a", "DTSTART;TZID=Europe/Berlin:20261103T150000", "DTEND;TZID=Europe/Berlin:20261103T160000", "SUMMARY:Zahnarzt"),
		vevent("UID:b", "DTSTART;TZID=Europe/Berlin:20261103T160000", "DTEND;TZID=Europe/Berlin:20261103T170000", "SUMMARY:Abholen"),
		vevent("UID:c", "DTSTART;TZID=Europe/Berlin:20261103T170000", "DTEND;TZID=Europe/Berlin:20261103T180000", "SUMMARY:Abendessen"),
		vevent("UID:d", "DTSTART;TZID=Europe/Berlin:20261103T190000", "DTEND;TZID=Europe/Berlin:20261103T200000", "SUMMARY:Kino"),
	))
	// Two lines, wide enough: heading and event, the summary behind the event.
	lines, more := agendaCell(t, p, map[string]any{"days": 1}, 2, 772, 152)
	if len(lines) != 2 || !lines[0].heading || !lines[1].event || more != "· +3 weitere" {
		t.Errorf("two lines = %+v, summary %q", lines, more)
	}
	// Too narrow for that: the summary takes the free line.
	lines, more = agendaCell(t, p, map[string]any{"days": 1}, 2, 240, 152)
	if more != "" || len(lines) != 2 || !lines[0].event || lines[1].text != "+3 weitere" {
		t.Errorf("narrow = %+v, summary %q", lines, more)
	}
	// A title that fills the line leaves no room for the summary either.
	long := agendaPlugin(t, ics(
		vevent("UID:a", "DTSTART;TZID=Europe/Berlin:20261103T150000", "DTEND;TZID=Europe/Berlin:20261103T160000", "SUMMARY:"+strings.Repeat("Zahnarztkontrolle ", 6)),
		vevent("UID:b", "DTSTART;TZID=Europe/Berlin:20261103T170000", "DTEND;TZID=Europe/Berlin:20261103T180000", "SUMMARY:Abendessen"),
	))
	lines, more = agendaCell(t, long, map[string]any{"days": 1}, 2, 772, 152)
	if more != "" || len(lines) != 2 || lines[1].text != "+1 weitere" {
		t.Errorf("long title = %+v, summary %q", lines, more)
	}
}

func TestAgendaDays(t *testing.T) {
	data := ics(
		vevent("UID:a", "DTSTART;TZID=Europe/Berlin:20261103T093000", "DTEND;TZID=Europe/Berlin:20261103T103000", "SUMMARY:Standup"),
		// crosses midnight into Wednesday: listed on its start day only
		vevent("UID:b", "DTSTART;TZID=Europe/Berlin:20261103T230000", "DTEND;TZID=Europe/Berlin:20261104T010000", "SUMMARY:Night"),
		// starts on day 3: outside a two-day range
		vevent("UID:c", "DTSTART;TZID=Europe/Berlin:20261105T000000", "DTEND;TZID=Europe/Berlin:20261105T010000", "SUMMARY:Thursday"),
		// multi-day, started yesterday: overlaps today
		vevent("UID:d", "DTSTART;VALUE=DATE:20261102", "DTEND;VALUE=DATE:20261105", "SUMMARY:Trip"),
		vevent("UID:e", "DTSTART;VALUE=DATE:20261104", "DTEND;VALUE=DATE:20261105", "SUMMARY:Holiday"),
	)
	p := agendaPlugin(t, data)
	got := agendaText(t, p, map[string]any{"days": 2}, 10)
	want := []string{"Heute:", "* Trip", "09:30 Standup", "23:00 Night", "Morgen:", "* Holiday"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("days 2 = %q, want %q", got, want)
	}
	got = agendaText(t, p, map[string]any{"days": 3}, 10)
	if got[len(got)-1] != "00:00 Thursday" {
		t.Errorf("days 3 = %q", got)
	}
	if got := agendaText(t, p, nil, 10); len(got) != 8 {
		t.Errorf("default range = %q", got)
	}
}

func TestAgendaEmptyDays(t *testing.T) {
	data := ics(
		vevent("UID:a", "DTSTART;TZID=Europe/Berlin:20261104T093000", "DTEND;TZID=Europe/Berlin:20261104T103000", "SUMMARY:Dentist"),
	)
	p := agendaPlugin(t, data)
	got := agendaText(t, p, map[string]any{"days": 3}, 10)
	want := []string{"Heute:", "Keine Termine", "Morgen:", "09:30 Dentist", "Do, 5.11.:", "Keine Termine"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("empty days = %q, want %q", got, want)
	}
	got = agendaText(t, p, map[string]any{"days": 2, "empty": false}, 10)
	if want := []string{"Morgen:", "09:30 Dentist"}; !reflect.DeepEqual(got, want) {
		t.Errorf("empty false = %q, want %q", got, want)
	}
	got = agendaText(t, p, map[string]any{"days": 2, "headings": false}, 10)
	if want := []string{"Keine Termine", "Mi 09:30 Dentist"}; !reflect.DeepEqual(got, want) {
		t.Errorf("no headings = %q, want %q", got, want)
	}
	// without the days argument free days stay hidden
	if got := agendaText(t, p, nil, 10); !reflect.DeepEqual(got, []string{"Morgen:", "09:30 Dentist"}) {
		t.Errorf("default = %q", got)
	}
}

func TestAgendaDaysClamped(t *testing.T) {
	p := agendaPlugin(t, ics())
	p.opts.days = 3
	if o := p.agendaArgs(plugin.Values{"days": 40}, true); o.days != 3 || !o.empty {
		t.Errorf("above range: %+v", o)
	}
	if o := p.agendaArgs(plugin.Values{"days": 0}, true); o.days != 1 {
		t.Errorf("below range: %+v", o)
	}
	if o := p.agendaArgs(nil, true); o.days != 3 || o.empty {
		t.Errorf("default: %+v", o)
	}
}

func TestAgendaImageDays(t *testing.T) {
	p := agendaPlugin(t, ics())
	p.lang = "en"
	img := render.NewCanvas(300, 200, 2)
	w := plugin.WidgetRequest{Widget: "agenda", Width: 300, Height: 200, Args: map[string]any{"days": 2, "lines": 6}}
	w.Style.Padding = 4
	if err := p.Render(&plugin.Request{}, w, img); err != nil {
		t.Fatal(err)
	}
	lines, _ := p.agendaLines(6, p.agendaArgs(w.Arg(), true))
	if len(lines) != 4 {
		t.Errorf("lines = %+v", lines)
	}
}
