package main

import (
	"errors"
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
	return agendaPluginAt(t, time.Date(2026, 11, 3, 8, 0, 0, 0, berlin), data)
}

// agendaPluginAt is agendaPlugin at another point in time, with the events
// expanded around it.
func agendaPluginAt(t *testing.T, now time.Time, data string) *calendarPlugin {
	t.Helper()
	p := &calendarPlugin{loc: berlin, lang: "de", now: now}
	p.opts.days = 14
	from := midnight(now.In(berlin)).AddDate(0, 0, -20)
	occ, warn := expandICS(t, data, from, from.AddDate(0, 0, 60))
	if len(warn) > 0 {
		t.Fatal(warn)
	}
	for _, o := range occ {
		p.events = append(p.events, toEvent(o, berlin, "de"))
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

// eventLine returns the first event line of the image layout, so a test can
// check what a cell shows without a whole canvas.
func eventLine(t *testing.T, p *calendarPlugin, args map[string]any, lines, width, height int) (agendaLine, bool) {
	t.Helper()
	got, _ := agendaCell(t, p, args, lines, width, height)
	for _, l := range got {
		if l.event {
			return l, true
		}
	}
	return agendaLine{}, false
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

// birthdayText asks the birthdays widget for its text lines.
func birthdayText(t *testing.T, p *calendarPlugin, args map[string]any, height int) []string {
	t.Helper()
	lines, err := p.Text(&plugin.Request{}, plugin.WidgetRequest{Widget: "birthdays", Width: 30, Height: height, Args: args})
	if err != nil {
		t.Fatal(err)
	}
	return lines
}

func TestBirthdayAge(t *testing.T) {
	p := agendaPlugin(t, ics(
		vevent("UID:a", "SUMMARY:Anna", "DTSTART;VALUE=DATE:19851105", "RRULE:FREQ=YEARLY"),
		vevent("UID:b", "SUMMARY:Jahrestag", "DTSTART;VALUE=DATE:19901106", "RRULE:FREQ=YEARLY"),
	))
	p.opts.birthdays = []string{"Geburtstage"}
	p.events[0].Calendar = "Geburtstage"

	// Without the argument the title stays as it is.
	got := agendaText(t, p, map[string]any{"days": 4, "empty": false}, 10)
	want := []string{"Do, 5.11.:", "* Anna", "Fr, 6.11.:", "* Jahrestag"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("titles = %q, want %q", got, want)
	}
	// The age belongs to the birthday calendar only, and a text display keeps
	// it short.
	got = agendaText(t, p, map[string]any{"days": 4, "empty": false, "age": true}, 10)
	want = []string{"Do, 5.11.:", "* Anna (41)", "Fr, 6.11.:", "* Jahrestag"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ages = %q, want %q", got, want)
	}
	// The image spells the age out.
	if line, ok := eventLine(t, p, map[string]any{"days": 4, "empty": false, "age": true}, 4, 772, 304); !ok || p.lineText(line, true) != "Anna wird 41" {
		t.Errorf("image line = %+v", line)
	}
	// English follows the request language.
	p.lang = "en"
	if got := agendaText(t, p, map[string]any{"days": 4, "empty": false, "age": true}, 10); got[1] != "* Anna (41)" {
		t.Errorf("english text = %q", got)
	}
	if line, ok := eventLine(t, p, map[string]any{"days": 4, "empty": false, "age": true}, 4, 772, 304); !ok || p.lineText(line, true) != "Anna turns 41" {
		t.Errorf("english image line = %+v", line)
	}
}

func TestBirthdaysWidget(t *testing.T) {
	p := agendaPlugin(t, ics(
		vevent("UID:a", "SUMMARY:Anna", "DTSTART;VALUE=DATE:19851105", "RRULE:FREQ=YEARLY"),
		vevent("UID:b", "SUMMARY:Zahnarzt", "DTSTART;TZID=Europe/Berlin:20261105T150000", "DTEND;TZID=Europe/Berlin:20261105T160000"),
	))
	p.opts.birthdays = []string{"Geburtstage"}
	for i := range p.events {
		if p.events[i].Title == "Anna" {
			p.events[i].Calendar = "Geburtstage"
		}
	}
	// Only the birthday, always with its age.
	got := birthdayText(t, p, map[string]any{"days": 4}, 4)
	want := []string{"Do, 5.11.:", "Anna (41)"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("birthdays = %q, want %q", got, want)
	}
	// A day without one is not a line of its own.
	if len(got) > 3 {
		t.Errorf("free days shown: %q", got)
	}
	// Without birthdays in range the cell says so.
	if got := birthdayText(t, p, map[string]any{"days": 1}, 4); !reflect.DeepEqual(got, []string{"Keine Geburtstage"}) {
		t.Errorf("empty = %q", got)
	}
	// The image path draws the same lines.
	img := render.NewCanvas(300, 200, 2)
	w := plugin.WidgetRequest{Widget: "birthdays", Width: 300, Height: 200, Args: map[string]any{"days": 4, "lines": 4}}
	w.Style.Padding = 4
	if err := p.Render(&plugin.Request{}, w, img); err != nil {
		t.Fatal(err)
	}
}

func TestBirthdayAgePlausible(t *testing.T) {
	p := agendaPlugin(t, ics(
		// Born 2026: the day of birth itself is no birthday to announce.
		vevent("UID:a", "SUMMARY:Baby", "DTSTART;VALUE=DATE:20261105"),
		// A year that no living person reaches is ignored instead of shown.
		vevent("UID:b", "SUMMARY:Urahn", "DTSTART;VALUE=DATE:18501106", "RRULE:FREQ=YEARLY"),
	))
	p.opts.birthdays = []string{"Test"}
	got := agendaText(t, p, map[string]any{"age": true, "days": 4, "empty": false}, 10)
	want := []string{"Do, 5.11.:", "* Baby", "Fr, 6.11.:", "* Urahn"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ages = %q, want %q", got, want)
	}
}

func TestBirthdayLeapDay(t *testing.T) {
	data := ics(vevent("UID:a", "SUMMARY:Lisa", "DTSTART;VALUE=DATE:19880229", "RRULE:FREQ=YEARLY"))
	// A leap year shows the birthday on 29 February.
	p := agendaPluginAt(t, time.Date(2028, 2, 25, 8, 0, 0, 0, berlin), data)
	p.opts.birthdays = []string{"Test"}
	if got := agendaText(t, p, map[string]any{"age": true, "days": 6, "empty": false}, 4); !reflect.DeepEqual(got, []string{"Di, 29.2.:", "* Lisa (40)"}) {
		t.Errorf("leap year = %q", got)
	}
	// In a common year the yearly rule falls on 28 February, so the birthday
	// does not vanish three years out of four.
	p = agendaPluginAt(t, time.Date(2027, 2, 25, 8, 0, 0, 0, berlin), data)
	p.opts.birthdays = []string{"Test"}
	if got := agendaText(t, p, map[string]any{"age": true, "days": 6, "empty": false}, 4); !reflect.DeepEqual(got, []string{"So, 28.2.:", "* Lisa (39)"}) {
		t.Errorf("common year = %q", got)
	}
}

func TestBirthdaysDefaultWindow(t *testing.T) {
	p := agendaPluginAt(t, time.Date(2026, 11, 3, 8, 0, 0, 0, berlin), ics(
		vevent("UID:a", "SUMMARY:Anna", "DTSTART;VALUE=DATE:19851203", "RRULE:FREQ=YEARLY"),
	))
	p.opts.birthdays = []string{"Test"}
	// The agenda range stays at 7 days, but birthdays reach the loaded window:
	// the month grid needs 42 days, so a birthday 30 days ahead still appears.
	p.opts.days = 7
	if got := birthdayText(t, p, nil, 4); !reflect.DeepEqual(got, []string{"Do, 3.12.:", "Anna (41)"}) {
		t.Errorf("default window = %q", got)
	}
	// An explicit window below the default still applies.
	if got := birthdayText(t, p, map[string]any{"days": 20}, 4); !reflect.DeepEqual(got, []string{"Keine Geburtstage"}) {
		t.Errorf("days 20 = %q", got)
	}
}

// A cell without room is reported for every widget, before any data is read.
func TestTooSmallCell(t *testing.T) {
	p := &calendarPlugin{}
	req := &plugin.Request{Language: "de", Timezone: "Europe/Berlin"}
	w := plugin.WidgetRequest{Width: 10, Height: 10, Style: plugin.Style{Padding: 8, Rule: 1, TextPx: 16}}
	for _, name := range []string{"agenda", "birthdays", "month", "week"} {
		w.Widget = name
		err := p.Render(req, w, render.NewCanvas(10, 10, 2))
		if !errors.Is(err, plugin.ErrTooSmall) {
			t.Errorf("%s in a 10x10 cell: error = %v, want ErrTooSmall", name, err)
		}
	}
}
