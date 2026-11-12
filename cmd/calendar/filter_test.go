package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

// filterPlugin puts three events into three calendars on the agenda's two
// days: two today and one tomorrow.
func filterPlugin(t *testing.T) *calendarPlugin {
	t.Helper()
	p := agendaPlugin(t, ics(
		vevent("UID:a", "SUMMARY:Familienessen", "DTSTART;TZID=Europe/Berlin:20261103T180000", "DTEND;TZID=Europe/Berlin:20261103T190000"),
		vevent("UID:b", "SUMMARY:Arzt", "DTSTART;TZID=Europe/Berlin:20261103T193000", "DTEND;TZID=Europe/Berlin:20261103T200000"),
		vevent("UID:c", "SUMMARY:Standup", "DTSTART;TZID=Europe/Berlin:20261104T090000", "DTEND;TZID=Europe/Berlin:20261104T093000"),
	))
	calendar := map[string]string{"Familienessen": "Familie", "Arzt": "Privat", "Standup": "Arbeit"}
	for i := range p.events {
		p.events[i].Calendar = calendar[p.events[i].Title]
	}
	p.calendars = []string{"Familie", "Privat", "Arbeit"}
	return p
}

func TestAgendaCalendarFilter(t *testing.T) {
	p := filterPlugin(t)
	text := func(args map[string]any) []string {
		return agendaText(t, p, args, 10)
	}
	if got, want := text(map[string]any{"days": 1, "headings": false, "calendars": []any{"Familie"}}), []string{"18:00 Familienessen"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Familie = %q, want %q", got, want)
	}
	// The filter matches case-insensitively, like the calendars config.
	if got, want := text(map[string]any{"days": 1, "headings": false, "calendars": []any{"privat"}}), []string{"19:30 Arzt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("privat = %q, want %q", got, want)
	}
	// Several names keep their union.
	got := text(map[string]any{"days": 2, "headings": false, "calendars": []any{"Familie", "Arbeit"}})
	want := []string{"18:00 Familienessen", "Mi 09:00 Standup"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("union = %q, want %q", got, want)
	}
	// Without the argument every calendar stays visible.
	got = text(map[string]any{"days": 2, "headings": false})
	want = []string{"18:00 Familienessen", "19:30 Arzt", "Mi 09:00 Standup"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("all = %q, want %q", got, want)
	}
}

func TestCalendarFilterUnknown(t *testing.T) {
	p := filterPlugin(t)
	_, err := p.Text(&plugin.Request{}, plugin.WidgetRequest{Widget: "agenda", Width: 30, Height: 10,
		Args: map[string]any{"calendars": []any{"Gibt es nicht"}}})
	if err == nil || !strings.Contains(err.Error(), `calendar "Gibt es nicht" not found`) ||
		!strings.Contains(err.Error(), "available: Familie, Privat, Arbeit") {
		t.Fatalf("agenda error = %v", err)
	}

	img := render.NewCanvas(300, 200, 2)
	w := plugin.WidgetRequest{Widget: "month", Width: 300, Height: 200, Args: map[string]any{"calendars": []any{"Gibt es nicht"}}}
	w.Style.Padding = 4
	if err := p.Render(&plugin.Request{}, w, img); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("month error = %v", err)
	}
}

func TestBirthdaysCalendarFilter(t *testing.T) {
	p := filterPlugin(t)
	p.opts.birthdays = []string{"Geburtstage"}
	p.events = append(p.events, event{
		Start: "2026-11-04T23:00:00Z", End: "2026-11-05T23:00:00Z",
		First: "1985-11-04T23:00:00Z", AllDay: true, Title: "Anna", Calendar: "Geburtstage",
	})
	p.calendars = append(p.calendars, "Geburtstage")
	if got, want := birthdayText(t, p, map[string]any{"days": 4, "calendars": []any{"Geburtstage"}}, 4), []string{"Do, 5.11.:", "Anna (41)"}; !reflect.DeepEqual(got, want) {
		t.Errorf("birthdays = %q, want %q", got, want)
	}
	if got := birthdayText(t, p, map[string]any{"days": 4, "calendars": []any{"Privat"}}, 4); !reflect.DeepEqual(got, []string{"Keine Geburtstage"}) {
		t.Errorf("other calendar = %q", got)
	}
}

func TestMonthAndWeekCalendarFilter(t *testing.T) {
	p := filterPlugin(t)

	// The month text variant lists the titles of the next days.
	month := func(names ...string) ([]string, error) {
		args := map[string]any{}
		if len(names) > 0 {
			list := make([]any, len(names))
			for i, name := range names {
				list[i] = name
			}
			args["calendars"] = list
		}
		return p.Text(&plugin.Request{}, plugin.WidgetRequest{Widget: "month", Width: 30, Height: 2, Args: args})
	}
	got, err := month("Arbeit")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Di  3", "Mi  4 Standup"}; !reflect.DeepEqual(got, want) {
		t.Errorf("month = %q, want %q", got, want)
	}
	got, err = month("Familie")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Di  3 Familienessen", "Mi  4"}; !reflect.DeepEqual(got, want) {
		t.Errorf("month family = %q, want %q", got, want)
	}

	// The week text variant starts at today and lists one line per day.
	week := func(names ...string) ([]string, error) {
		args := map[string]any{"days": 2, "start": "today"}
		if len(names) > 0 {
			list := make([]any, len(names))
			for i, name := range names {
				list[i] = name
			}
			args["calendars"] = list
		}
		return p.Text(&plugin.Request{}, plugin.WidgetRequest{Widget: "week", Width: 30, Height: 2, Args: args})
	}
	got, err = week("Familie")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{">Di  3 18:00 Familienessen", " Mi  4"}; !reflect.DeepEqual(got, want) {
		t.Errorf("week = %q, want %q", got, want)
	}
	got, err = week("Arbeit")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{">Di  3", " Mi  4 09:00 Standup"}; !reflect.DeepEqual(got, want) {
		t.Errorf("week work = %q, want %q", got, want)
	}
}

func widgetArg(t *testing.T, d plugin.Description, widget, name string) plugin.Arg {
	t.Helper()
	for _, info := range d.Widgets {
		if info.Widget == widget {
			arg, ok := info.Args[name]
			if !ok {
				t.Fatalf("%s has no %s arg", widget, name)
			}
			return arg
		}
	}
	t.Fatalf("widget %q missing", widget)
	return plugin.Arg{}
}

func TestDescribeCalendarEnum(t *testing.T) {
	p := &calendarPlugin{}
	// Before the first load the configured names are the fallback, so the
	// editors can still offer check boxes.
	p.opts.calendars = []string{"Familie", "Privat"}
	for _, widget := range []string{"agenda", "birthdays", "month", "week"} {
		arg := widgetArg(t, p.Describe(), widget, "calendars")
		if arg.Type != "list" || !reflect.DeepEqual(arg.Enum, []any{"Familie", "Privat"}) {
			t.Errorf("%s calendars = %+v", widget, arg)
		}
	}
	// The loaded names win over the configured pool.
	p.ready = make(chan struct{})
	close(p.ready)
	p.loadedCalendars = []string{"Familie", "Arbeit", "Geburtstage"}
	arg := widgetArg(t, p.Describe(), "agenda", "calendars")
	if want := []any{"Familie", "Arbeit", "Geburtstage"}; !reflect.DeepEqual(arg.Enum, want) {
		t.Errorf("loaded enum = %v, want %v", arg.Enum, want)
	}
	// Without names the editors keep the text field.
	empty := &calendarPlugin{ready: make(chan struct{})}
	close(empty.ready)
	if arg := widgetArg(t, empty.Describe(), "agenda", "calendars"); arg.Enum != nil {
		t.Errorf("enum without calendars = %v", arg.Enum)
	}
}

func TestDescribeCalendarEnumWaitsForFirstLoad(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "BEGIN:VCALENDAR\r\nX-WR-CALNAME:Feiertage\r\n"+
			vevent("UID:f", "SUMMARY:Feiertag", "DTSTART;VALUE=DATE:20261105")+"END:VCALENDAR\r\n")
	}))
	defer srv.Close()

	p := &calendarPlugin{}
	p.opts = options{ics: []string{srv.URL}, days: 7, maxEvents: 10, refresh: time.Hour, language: "en", location: berlin}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx, func(...string) {})

	// No configured names: describe waits for the first load instead of
	// answering an empty enum.
	arg := widgetArg(t, p.Describe(), "agenda", "calendars")
	if want := []any{"Feiertage"}; !reflect.DeepEqual(arg.Enum, want) {
		t.Errorf("enum = %v, want %v", arg.Enum, want)
	}
}
