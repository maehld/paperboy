package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

var berlin = mustLoad("Europe/Berlin")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func ics(events ...string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:test\r\n" + strings.Join(events, "") + "END:VCALENDAR\r\n"
}

func vevent(lines ...string) string {
	return "BEGIN:VEVENT\r\n" + strings.Join(lines, "\r\n") + "\r\nEND:VEVENT\r\n"
}

func expandICS(t *testing.T, data string, from, to time.Time) ([]occurrence, []string) {
	t.Helper()
	roots, err := parseICS([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	return occurrences(roots, "Test", newZones(berlin), from, to)
}

func starts(list []occurrence) []string {
	var out []string
	for _, o := range list {
		if o.AllDay {
			out = append(out, o.Start.Format("2006-01-02"))
		} else {
			out = append(out, o.Start.In(berlin).Format("2006-01-02 15:04"))
		}
	}
	return out
}

func TestParseLineUnfoldAndEscapes(t *testing.T) {
	data := ics(vevent(
		"UID:1",
		"SUMMARY:Arzt\\, Zahn\\; Kontrolle",
		"LOCATION;LANGUAGE=de:Haupt",
		" straße 1",
		`DTSTART;TZID="Europe/Berlin":20261105T090000`,
		"DTEND;TZID=Europe/Berlin:20261105T093000",
	))
	list, warn := expandICS(t, data, time.Date(2026, 11, 5, 0, 0, 0, 0, berlin), time.Date(2026, 11, 6, 0, 0, 0, 0, berlin))
	if len(warn) != 0 || len(list) != 1 {
		t.Fatalf("got %v %v", list, warn)
	}
	o := list[0]
	if o.Title != "Arzt, Zahn; Kontrolle" || o.Location != "Hauptstraße 1" {
		t.Fatalf("title %q location %q", o.Title, o.Location)
	}
	if got := o.Start.UTC().Format(time.RFC3339); got != "2026-11-05T08:00:00Z" {
		t.Fatalf("start %s", got)
	}
	if o.End.Sub(o.Start) != 30*time.Minute {
		t.Fatalf("length %s", o.End.Sub(o.Start))
	}
}

func TestParseDuration(t *testing.T) {
	cases := map[string]struct {
		days int
		d    time.Duration
	}{
		"P1D":      {1, 0},
		"PT1H30M":  {0, 90 * time.Minute},
		"P2W":      {14, 0},
		"P1DT12H":  {1, 12 * time.Hour},
		"-PT15M":   {0, -15 * time.Minute},
		"+PT0S":    {0, 0},
		"PT45S":    {0, 45 * time.Second},
		"P0DT2H0M": {0, 2 * time.Hour},
	}
	for in, want := range cases {
		days, d, err := parseDuration(in)
		if err != nil || days != want.days || d != want.d {
			t.Errorf("%s: got %d %s %v", in, days, d, err)
		}
	}
	for _, bad := range []string{"1D", "PT1D", "P1H", "PX", "P1"} {
		if _, _, err := parseDuration(bad); err == nil {
			t.Errorf("%s: expected an error", bad)
		}
	}
}

func TestAllDayAndDuration(t *testing.T) {
	data := ics(
		vevent("UID:a", "SUMMARY:Urlaub", "DTSTART;VALUE=DATE:20261104", "DTEND;VALUE=DATE:20261107"),
		vevent("UID:b", "SUMMARY:Call", "DTSTART:20261105T150000Z", "DURATION:PT1H"),
		vevent("UID:c", "SUMMARY:Feiertag", "DTSTART;VALUE=DATE:20261106"),
	)
	from := time.Date(2026, 11, 5, 0, 0, 0, 0, berlin)
	list, _ := expandICS(t, data, from, from.AddDate(0, 0, 3))
	if got := starts(list); !reflect.DeepEqual(got, []string{"2026-11-04", "2026-11-05 16:00", "2026-11-06"}) {
		t.Fatalf("starts %v", got)
	}
	if !list[0].AllDay || list[0].End.Format("2006-01-02") != "2026-11-07" {
		t.Fatalf("all-day end %v", list[0].End)
	}
	if !list[2].End.Equal(list[2].Start.AddDate(0, 0, 1)) {
		t.Fatalf("default all-day length")
	}
}

func TestRecurrenceRules(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, berlin)
	to := time.Date(2027, 1, 1, 0, 0, 0, 0, berlin)
	cases := []struct {
		name  string
		start string
		rule  string
		want  []string
		limit int
	}{
		{"weekly days", "20261102T180000", "FREQ=WEEKLY;BYDAY=MO,WE;COUNT=4", []string{"2026-11-02 18:00", "2026-11-04 18:00", "2026-11-09 18:00", "2026-11-11 18:00"}, 0},
		{"biweekly", "20261103T070000", "FREQ=WEEKLY;INTERVAL=2;UNTIL=20261201T060000Z", []string{"2026-11-03 07:00", "2026-11-17 07:00", "2026-12-01 07:00"}, 0},
		{"daily count", "20261230T080000", "FREQ=DAILY;COUNT=5", []string{"2026-12-30 08:00", "2026-12-31 08:00"}, 0},
		{"monthly last friday", "20260130T100000", "FREQ=MONTHLY;BYDAY=-1FR", []string{"2026-01-30 10:00", "2026-02-27 10:00", "2026-03-27 10:00"}, 3},
		{"monthly 31st skips short months", "20260131T100000", "FREQ=MONTHLY", []string{"2026-01-31 10:00", "2026-03-31 10:00", "2026-05-31 10:00"}, 3},
		{"monthly second tuesday", "20260113T190000", "FREQ=MONTHLY;BYDAY=2TU;COUNT=3", []string{"2026-01-13 19:00", "2026-02-10 19:00", "2026-03-10 19:00"}, 0},
		{"last workday", "20260130T120000", "FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-1;COUNT=3", []string{"2026-01-30 12:00", "2026-02-27 12:00", "2026-03-31 12:00"}, 0},
		{"monthly negative day", "20260131T090000", "FREQ=MONTHLY;BYMONTHDAY=-1;COUNT=3", []string{"2026-01-31 09:00", "2026-02-28 09:00", "2026-03-31 09:00"}, 0},
		{"yearly birthday", "20100315T000000", "FREQ=YEARLY", []string{"2026-03-15 00:00"}, 0},
		{"yearly bymonth byday", "20260510T100000", "FREQ=YEARLY;BYMONTH=5;BYDAY=2SU", []string{"2026-05-10 10:00"}, 0},
		{"dst keeps wall clock", "20261020T090000", "FREQ=WEEKLY;COUNT=3", []string{"2026-10-20 09:00", "2026-10-27 09:00", "2026-11-03 09:00"}, 0},
		{"daily every weekday filter", "20261106T060000", "FREQ=DAILY;BYDAY=MO,FR;COUNT=3", []string{"2026-11-06 06:00", "2026-11-09 06:00", "2026-11-13 06:00"}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := ics(vevent("UID:x", "SUMMARY:x", "DTSTART;TZID=Europe/Berlin:"+c.start, "DURATION:PT1H", "RRULE:"+c.rule))
			list, warn := expandICS(t, data, from, to)
			if len(warn) != 0 {
				t.Fatal(warn)
			}
			got := starts(list)
			if c.limit > 0 && len(got) > c.limit {
				got = got[:c.limit]
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestExdateAndOverrides(t *testing.T) {
	data := ics(
		vevent("UID:w", "SUMMARY:Training", "DTSTART;TZID=Europe/Berlin:20261102T180000",
			"DTEND;TZID=Europe/Berlin:20261102T190000", "RRULE:FREQ=WEEKLY;COUNT=5",
			"EXDATE;TZID=Europe/Berlin:20261109T180000"),
		vevent("UID:w", "SUMMARY:Training (verlegt)", "RECURRENCE-ID;TZID=Europe/Berlin:20261116T180000",
			"DTSTART;TZID=Europe/Berlin:20261117T200000", "DTEND;TZID=Europe/Berlin:20261117T210000"),
		vevent("UID:w", "SUMMARY:Training", "RECURRENCE-ID;TZID=Europe/Berlin:20261123T180000",
			"DTSTART;TZID=Europe/Berlin:20261123T180000", "STATUS:CANCELLED"),
	)
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, berlin)
	list, warn := expandICS(t, data, from, from.AddDate(0, 1, 0))
	if len(warn) != 0 {
		t.Fatal(warn)
	}
	want := []string{"2026-11-02 18:00", "2026-11-17 20:00", "2026-11-30 18:00"}
	if got := starts(list); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if list[1].Title != "Training (verlegt)" {
		t.Fatalf("override title %q", list[1].Title)
	}
}

func TestUnsupportedRuleIsSkippedWithWarning(t *testing.T) {
	data := ics(
		vevent("UID:h", "SUMMARY:Stündlich", "DTSTART:20261105T080000Z", "RRULE:FREQ=HOURLY"),
		vevent("UID:ok", "SUMMARY:Okay", "DTSTART:20261105T080000Z"),
	)
	from := time.Date(2026, 11, 5, 0, 0, 0, 0, berlin)
	list, warn := expandICS(t, data, from, from.AddDate(0, 0, 1))
	if len(list) != 1 || list[0].Title != "Okay" || len(warn) != 1 || !strings.Contains(warn[0], "Stündlich") {
		t.Fatalf("list %v warnings %v", list, warn)
	}
}

func TestWindowsAndUnknownZones(t *testing.T) {
	z := newZones(berlin)
	d, err := z.parseDate("20260701T090000", map[string]string{"TZID": "Pacific Standard Time"})
	if err != nil || d.Time.UTC().Hour() != 16 {
		t.Fatalf("got %v %v", d.Time.UTC(), err)
	}
	d, _ = z.parseDate("20260701T090000", map[string]string{"TZID": "Custom Zone"})
	if d.Time.Location() != berlin || !z.unknown["Custom Zone"] {
		t.Fatalf("fallback not applied")
	}
}

// ---- CalDAV ----

// fakeICloud mimics iCloud: the public host redirects to the account's
// partition host, which answers the discovery chain and the calendar query.
// Credentials must arrive on every request, also after the host change.
type fakeICloud struct {
	public, partition *httptest.Server
	requests          []string
	calendarData      map[string]string
}

func newFakeICloud(t *testing.T) *fakeICloud {
	f := &fakeICloud{calendarData: map[string]string{}}
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte("me@icloud.com:app-pass"))
	check := func(w http.ResponseWriter, r *http.Request) bool {
		f.requests = append(f.requests, r.Method+" "+r.Host+r.URL.Path)
		if r.Header.Get("Authorization") != auth {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}
	f.partition = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/xml")
		switch {
		case r.Method == "PROPFIND" && r.URL.Path == "/":
			w.WriteHeader(http.StatusMultiStatus)
			fmt.Fprint(w, `<?xml version="1.0"?><multistatus xmlns="DAV:"><response><href>/</href><propstat><prop><current-user-principal><href>/1234/principal/</href></current-user-principal></prop><status>HTTP/1.1 200 OK</status></propstat></response></multistatus>`)
		case r.Method == "PROPFIND" && r.URL.Path == "/1234/principal/":
			w.WriteHeader(http.StatusMultiStatus)
			fmt.Fprintf(w, `<?xml version="1.0"?><multistatus xmlns="DAV:"><response><href>/1234/principal/</href><propstat><prop><calendar-home-set xmlns="urn:ietf:params:xml:ns:caldav"><href xmlns="DAV:">%s/1234/calendars/</href></calendar-home-set></prop><status>HTTP/1.1 200 OK</status></propstat></response></multistatus>`, f.partition.URL)
		case r.Method == "PROPFIND" && r.URL.Path == "/1234/calendars/":
			if r.Header.Get("Depth") != "1" {
				t.Errorf("collections depth %q", r.Header.Get("Depth"))
			}
			w.WriteHeader(http.StatusMultiStatus)
			fmt.Fprint(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
<d:response><d:href>/1234/calendars/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
<d:response><d:href>/1234/calendars/home/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/><c:calendar/></d:resourcetype><d:displayname>Privat</d:displayname><c:supported-calendar-component-set><c:comp name="VEVENT"/></c:supported-calendar-component-set></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
<d:response><d:href>/1234/calendars/work/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/><c:calendar/></d:resourcetype><d:displayname>Arbeit</d:displayname></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
<d:response><d:href>/1234/calendars/tasks/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/><c:calendar/></d:resourcetype><d:displayname>Erinnerungen</d:displayname><c:supported-calendar-component-set><c:comp name="VTODO"/></c:supported-calendar-component-set></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
</d:multistatus>`)
		case r.Method == "REPORT":
			if !strings.Contains(string(body), `<c:time-range start="20261104T230000Z" end="20261111T230000Z"/>`) {
				t.Errorf("time range missing in %s", body)
			}
			data, ok := f.calendarData[r.URL.Path]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusMultiStatus)
			fmt.Fprintf(w, `<?xml version="1.0"?><multistatus xmlns="DAV:"><response><href>%sevent.ics</href><propstat><prop><calendar-data xmlns="urn:ietf:params:xml:ns:caldav">%s</calendar-data></prop><status>HTTP/1.1 200 OK</status></propstat></response></multistatus>`,
				r.URL.Path, strings.ReplaceAll(strings.ReplaceAll(data, "&", "&amp;"), "<", "&lt;"))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	f.public = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		if r.URL.Path == "/.well-known/caldav" {
			http.Redirect(w, r, "/", http.StatusMovedPermanently)
			return
		}
		http.Redirect(w, r, f.partition.URL+r.URL.Path, http.StatusMovedPermanently)
	}))
	t.Cleanup(f.public.Close)
	t.Cleanup(f.partition.Close)
	return f
}

func TestCollectFromCalDAV(t *testing.T) {
	f := newFakeICloud(t)
	f.calendarData["/1234/calendars/home/"] = ics(
		vevent("UID:1", "SUMMARY:Müll rausbringen", "DTSTART;VALUE=DATE:20261106"),
		vevent("UID:2", "SUMMARY:Zahnarzt", "LOCATION:Praxis", "DTSTART;TZID=Europe/Berlin:20261105T143000", "DTEND;TZID=Europe/Berlin:20261105T153000"),
	)
	f.calendarData["/1234/calendars/work/"] = ics(
		vevent("UID:3", "SUMMARY:Standup", "DTSTART;TZID=Europe/Berlin:20261102T091500", "DURATION:PT15M", "RRULE:FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"),
	)
	o := options{caldav: f.public.URL, user: "me@icloud.com", password: "app-pass", days: 7, maxEvents: 100, language: "de", location: berlin}
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, berlin)
	b, warn, err := collect(context.Background(), o, newClient(o.user, o.password), now)
	if err != nil {
		t.Fatalf("%v\nrequests: %v", err, f.requests)
	}
	if len(warn) != 0 {
		t.Fatal(warn)
	}
	if !reflect.DeepEqual(b.Calendars, []string{"Privat", "Arbeit"}) {
		t.Fatalf("calendars %v", b.Calendars)
	}
	var titles []string
	for _, e := range b.Events {
		titles = append(titles, e.Date+" "+e.Title)
	}
	want := []string{"2026-11-05 Standup", "2026-11-05 Zahnarzt", "2026-11-06 Müll rausbringen", "2026-11-06 Standup",
		"2026-11-09 Standup", "2026-11-10 Standup", "2026-11-11 Standup"}
	if !reflect.DeepEqual(titles, want) {
		t.Fatalf("events\n got %v\nwant %v", titles, want)
	}
	if len(b.Days) != 7 || b.Days[0].Label != "Do" || len(b.Days[0].Events) != 2 || len(b.Days[2].Events) != 0 {
		t.Fatalf("days %+v", b.Days[:3])
	}
	trash := b.Events[2]
	if !trash.AllDay || trash.Start != "2026-11-05T23:00:00Z" || trash.EndDate != "2026-11-06" || trash.Calendar != "Privat" {
		t.Fatalf("all-day event %+v", trash)
	}
	if b.Events[1].Location != "Praxis" || b.Events[1].Start != "2026-11-05T13:30:00Z" {
		t.Fatalf("timed event %+v", b.Events[1])
	}

	o.calendars = []string{"arbeit"}
	b, _, err = collect(context.Background(), o, newClient(o.user, o.password), now)
	if err != nil || !reflect.DeepEqual(b.Calendars, []string{"Arbeit"}) {
		t.Fatalf("selection: %v %v", b.Calendars, err)
	}
	o.calendars = []string{"Gibt es nicht"}
	if _, _, err = collect(context.Background(), o, newClient(o.user, o.password), now); err == nil || !strings.Contains(err.Error(), "available: Privat, Arbeit") {
		t.Fatalf("unknown calendar: %v", err)
	}
}

func TestCalDAVWrongPassword(t *testing.T) {
	f := newFakeICloud(t)
	o := options{caldav: f.public.URL, user: "me@icloud.com", password: "wrong", days: 7, maxEvents: 10, language: "en", location: berlin}
	_, _, err := collect(context.Background(), o, newClient(o.user, o.password), time.Now())
	if err == nil || !strings.Contains(err.Error(), "app-specific password") || strings.Contains(err.Error(), "wrong") {
		t.Fatalf("got %v", err)
	}
}

func TestICSLink(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != "secret" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		fmt.Fprint(w, "BEGIN:VCALENDAR\r\nX-WR-CALNAME:Feiertage\r\n"+
			vevent("UID:f", "SUMMARY:Feiertag", "DTSTART;VALUE=DATE:20261105")+"END:VCALENDAR\r\n")
	}))
	defer srv.Close()
	o := options{ics: []string{srv.URL + "/cal.ics?token=secret"}, days: 2, maxEvents: 10, language: "en", location: berlin}
	b, _, err := collect(context.Background(), o, newClient("", ""), time.Date(2026, 11, 5, 8, 0, 0, 0, berlin))
	if err != nil || len(b.Events) != 1 || b.Events[0].Calendar != "Feiertage" {
		t.Fatalf("%+v %v", b, err)
	}
	o.ics = []string{srv.URL + "/cal.ics?token=nope"}
	_, _, err = collect(context.Background(), o, newClient("", ""), time.Now())
	if err == nil || strings.Contains(err.Error(), "nope") {
		t.Fatalf("error must not leak the link: %v", err)
	}
}

func TestParseFlags(t *testing.T) {
	t.Setenv("PAPERBOY_CALDAV_PASSWORD", "")
	if _, err := parseFlags([]string{"-caldav", "https://caldav.icloud.com", "-user", "me"}); err == nil || !strings.Contains(err.Error(), "PAPERBOY_CALDAV_PASSWORD") {
		t.Fatalf("missing password: %v", err)
	}
	t.Setenv("PAPERBOY_CALDAV_PASSWORD", "pw")
	t.Setenv("PAPERBOY_TIMEZONE", "Europe/Berlin")
	o, err := parseFlags([]string{"-caldav", "https://caldav.icloud.com", "-user", "me", "-calendar", "Privat", "-calendar", "Arbeit"})
	if err != nil || o.password != "pw" || len(o.calendars) != 2 || o.location.String() != "Europe/Berlin" {
		t.Fatalf("%+v %v", o, err)
	}
	if _, err := parseFlags(nil); err == nil {
		t.Fatal("expected an error without sources")
	}
}
