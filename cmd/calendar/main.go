// Command calendar writes the calendar branch of Paperboy's data document
// (ADR 0036, ADR 0048). It runs as an exec provider: it reads the calendars of
// a CalDAV account (iCloud, Nextcloud, Fastmail, ...) and optional iCalendar
// links, expands recurring events into the coming days, prints the branch as
// JSON on standard output and exits. Any failing source makes it exit with
// status 1 and a message on standard error, so Paperboy keeps the previous data
// and shows its age instead of a calendar with missing appointments.
//
//	PAPERBOY_CALDAV_PASSWORD=xxxx-xxxx-xxxx-xxxx \
//	go run ./cmd/calendar -caldav https://caldav.icloud.com -user me@icloud.com
//
// The password comes from an environment variable, never from an argument.
// iCloud needs an app-specific password from account.apple.com.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

const (
	// requestTimeout bounds the whole run; the provider timeout of the
	// configuration should be at least as long.
	requestTimeout = 50 * time.Second
	// defaultPasswordEnv names the variable with the CalDAV password.
	defaultPasswordEnv = "PAPERBOY_CALDAV_PASSWORD"
)

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

type options struct {
	caldav      string
	user        string
	password    string
	calendars   []string
	ics         []string
	days        int
	maxEvents   int
	language    string
	location    *time.Location
	passwordEnv string
	icsFiles    []string
	plugin      bool
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "calendar:", err)
		os.Exit(1)
	}
}

func run() error {
	opts, err := parseFlags(os.Args[1:])
	if err != nil {
		return err
	}
	if opts.plugin {
		plugin.Serve(&calendarPlugin{opts: opts})
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	now := time.Now()
	b, warnings, err := collect(ctx, opts, newClient(opts.user, opts.password), now)
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "calendar: warning:", w)
	}
	if err != nil {
		return err
	}
	out, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	os.Stdout.Write(append(out, '\n'))
	return nil
}

func parseFlags(args []string) (options, error) {
	fs := flag.NewFlagSet("calendar", flag.ContinueOnError)
	var o options
	var calendars, ics, icsEnv, icsFiles listFlag
	fs.StringVar(&o.caldav, "caldav", "", "CalDAV server or calendar URL, e.g. https://caldav.icloud.com")
	fs.StringVar(&o.user, "user", "", "CalDAV user name, for iCloud the Apple ID")
	fs.StringVar(&o.passwordEnv, "password-env", defaultPasswordEnv, "environment variable that holds the CalDAV password")
	fs.Var(&calendars, "calendar", "name of a CalDAV calendar to include; repeatable; default all")
	fs.Var(&ics, "ics", "iCalendar URL (https:// or webcal://); repeatable")
	fs.Var(&icsEnv, "ics-env", "environment variable that holds an iCalendar URL; repeatable")
	fs.Var(&icsFiles, "ics-file", "local iCalendar file; repeatable")
	fs.BoolVar(&o.plugin, "plugin", false, "run as a fragment plugin: read a request on standard input and answer with images")
	fs.IntVar(&o.days, "days", 14, "number of days from today to include")
	fs.IntVar(&o.maxEvents, "max-events", 200, "maximum number of events in the output")
	language := fs.String("language", envOr("PAPERBOY_LANGUAGE", "en"), `language of the day labels: "de" or "en"`)
	timezone := fs.String("timezone", envOr("PAPERBOY_TIMEZONE", ""), "IANA timezone of the local day; default the system timezone")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if fs.NArg() != 0 {
		return o, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	o.calendars = calendars
	o.ics = ics
	o.icsFiles = icsFiles
	for _, name := range icsEnv {
		v := strings.TrimSpace(os.Getenv(name))
		if v == "" {
			return o, fmt.Errorf("-ics-env: %s is empty", name)
		}
		o.ics = append(o.ics, v)
	}
	if o.caldav == "" && len(o.ics) == 0 && len(o.icsFiles) == 0 {
		return o, errors.New("-caldav, -ics or -ics-file is required")
	}
	if o.caldav != "" {
		if o.user == "" {
			return o, errors.New("-caldav needs -user")
		}
		o.password = os.Getenv(o.passwordEnv)
		if o.password == "" {
			return o, fmt.Errorf("-caldav needs the password in %s", o.passwordEnv)
		}
	} else if len(o.calendars) > 0 {
		return o, errors.New("-calendar needs -caldav")
	}
	if o.days < 1 || o.days > 366 {
		return o, errors.New("-days must be within 1..366")
	}
	if o.maxEvents < 1 {
		return o, errors.New("-max-events must be positive")
	}
	if *language != "de" && *language != "en" {
		return o, fmt.Errorf("language %q: expected de or en", *language)
	}
	o.language = *language
	o.location = time.Local
	if *timezone != "" {
		loc, err := time.LoadLocation(*timezone)
		if err != nil {
			return o, fmt.Errorf("timezone %q: %w", *timezone, err)
		}
		o.location = loc
	}
	return o, nil
}

func envOr(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

// collect reads all sources and builds the branch.
func collect(ctx context.Context, o options, c *client, now time.Time) (branch, []string, error) {
	local := now.In(o.location)
	from := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, o.location)
	to := from.AddDate(0, 0, o.days)
	z := newZones(o.location)

	var all []occurrence
	var warnings, names []string
	if o.caldav != "" {
		base, err := url.Parse(o.caldav)
		if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
			return branch{}, nil, fmt.Errorf("-caldav: invalid URL")
		}
		cals, err := c.discover(ctx, base)
		if err != nil {
			return branch{}, nil, err
		}
		cals, err = selectCalendars(cals, o.calendars)
		if err != nil {
			return branch{}, nil, err
		}
		for _, cal := range cals {
			raw, err := c.events(ctx, cal, from, to)
			if err != nil {
				return branch{}, nil, fmt.Errorf("calendar %q: %w", cal.Name, err)
			}
			names = append(names, cal.Name)
			if len(raw) == 0 {
				continue
			}
			roots, err := parseICS(raw)
			if err != nil {
				return branch{}, nil, fmt.Errorf("calendar %q: %w", cal.Name, err)
			}
			occ, warn := occurrences(roots, cal.Name, z, from, to)
			all = append(all, occ...)
			warnings = append(warnings, prefix(cal.Name, warn)...)
		}
	}
	for _, link := range o.ics {
		raw, err := c.fetchICS(ctx, link)
		if err != nil {
			return branch{}, nil, fmt.Errorf("iCalendar link: %w", err)
		}
		roots, err := parseICS(raw)
		if err != nil {
			return branch{}, nil, fmt.Errorf("iCalendar link: %w", err)
		}
		name := calendarName(roots, link)
		names = append(names, name)
		occ, warn := occurrences(roots, name, z, from, to)
		all = append(all, occ...)
		warnings = append(warnings, prefix(name, warn)...)
	}
	for _, file := range o.icsFiles {
		raw, err := os.ReadFile(file)
		if err != nil {
			return branch{}, nil, err
		}
		roots, err := parseICS(raw)
		if err != nil {
			return branch{}, nil, fmt.Errorf("%s: %w", file, err)
		}
		name := calendarName(roots, file)
		names = append(names, name)
		occ, warn := occurrences(roots, name, z, from, to)
		all = append(all, occ...)
		warnings = append(warnings, prefix(name, warn)...)
	}
	for tzid := range z.unknown {
		warnings = append(warnings, fmt.Sprintf("unknown timezone %q, using %s", tzid, o.location))
	}
	sort.Strings(warnings)
	sortOccurrences(all)
	if len(all) > o.maxEvents {
		all = all[:o.maxEvents]
	}
	return buildBranch(all, names, from, o.days, now, o.location, o.language), warnings, nil
}

func prefix(name string, list []string) []string {
	out := make([]string, len(list))
	for i, w := range list {
		out[i] = fmt.Sprintf("calendar %q: %s", name, w)
	}
	return out
}

// selectCalendars keeps the requested calendars by display name (case
// insensitive); a name that does not exist is an error, so a renamed
// calendar does not silently disappear from the display.
func selectCalendars(all []calendarRef, wanted []string) ([]calendarRef, error) {
	if len(wanted) == 0 {
		return all, nil
	}
	var out []calendarRef
	for _, w := range wanted {
		found := false
		for _, c := range all {
			if strings.EqualFold(c.Name, strings.TrimSpace(w)) {
				out = append(out, c)
				found = true
			}
		}
		if !found {
			var names []string
			for _, c := range all {
				names = append(names, c.Name)
			}
			return nil, fmt.Errorf("calendar %q not found; available: %s", w, strings.Join(names, ", "))
		}
	}
	return out, nil
}

func calendarName(roots []*component, link string) string {
	for _, r := range roots {
		if p, ok := r.prop("X-WR-CALNAME"); ok && strings.TrimSpace(p.Value) != "" {
			return strings.TrimSpace(unescapeText(p.Value))
		}
	}
	if u, err := url.Parse(link); err == nil && u.Host != "" {
		return u.Host
	}
	return "calendar"
}

// ---- document branch ----

// branch is the value of the calendar branch. "updated" is the run time.
type branch struct {
	Updated   string   `json:"updated"`
	From      string   `json:"from"`
	To        string   `json:"to"`
	Calendars []string `json:"calendars"`
	Events    []event  `json:"events"`
	Days      []day    `json:"days"`
}

// event is one appointment. start and end are RFC 3339 in UTC like the other
// providers; date and end_date are local calendar days (end_date inclusive),
// so templates can group without timezone arithmetic.
type event struct {
	Start    string `json:"start"`
	End      string `json:"end"`
	AllDay   bool   `json:"all_day"`
	Date     string `json:"date"`
	EndDate  string `json:"end_date"`
	Title    string `json:"title"`
	Location string `json:"location,omitempty"`
	Calendar string `json:"calendar"`
}

type day struct {
	Date   string  `json:"date"`
	Label  string  `json:"label"`
	Events []event `json:"events"`
}

func buildBranch(list []occurrence, names []string, from time.Time, days int, now time.Time, loc *time.Location, language string) branch {
	if names == nil {
		names = []string{}
	}
	b := branch{
		Updated:   now.UTC().Format(time.RFC3339),
		From:      from.UTC().Format(time.RFC3339),
		To:        from.AddDate(0, 0, days).UTC().Format(time.RFC3339),
		Calendars: names,
		Events:    []event{},
	}
	for _, o := range list {
		b.Events = append(b.Events, toEvent(o, loc, language))
	}
	for i := range days {
		dayStart := from.AddDate(0, 0, i)
		dayEnd := from.AddDate(0, 0, i+1)
		d := day{Date: dayStart.Format("2006-01-02"), Label: weekday(dayStart, language), Events: []event{}}
		for j, o := range list {
			if o.Start.Before(dayEnd) && (o.End.After(dayStart) || (o.End.Equal(o.Start) && !o.Start.Before(dayStart))) {
				d.Events = append(d.Events, b.Events[j])
			}
		}
		b.Days = append(b.Days, d)
	}
	return b
}

func toEvent(o occurrence, loc *time.Location, language string) event {
	title := o.Title
	if title == "" {
		title = "(no title)"
		if language == "de" {
			title = "(ohne Titel)"
		}
	}
	e := event{
		Start:    o.Start.UTC().Format(time.RFC3339),
		End:      o.End.UTC().Format(time.RFC3339),
		AllDay:   o.AllDay,
		Title:    title,
		Location: o.Location,
		Calendar: o.Calendar,
	}
	if o.AllDay {
		// All-day events are dates, not instants: they start at local
		// midnight of the configured timezone.
		s := time.Date(o.Start.Year(), o.Start.Month(), o.Start.Day(), 0, 0, 0, 0, loc)
		end := time.Date(o.End.Year(), o.End.Month(), o.End.Day(), 0, 0, 0, 0, loc)
		e.Start = s.UTC().Format(time.RFC3339)
		e.End = end.UTC().Format(time.RFC3339)
		e.Date = s.Format("2006-01-02")
		e.EndDate = end.AddDate(0, 0, -1).Format("2006-01-02")
		return e
	}
	start, end := o.Start.In(loc), o.End.In(loc)
	e.Date = start.Format("2006-01-02")
	last := end
	if end.After(start) {
		last = end.Add(-time.Nanosecond)
	}
	e.EndDate = last.Format("2006-01-02")
	return e
}

var weekdayNames = map[string][7]string{
	"de": {"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"},
	"en": {"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
}

func weekday(t time.Time, language string) string {
	names, ok := weekdayNames[language]
	if !ok {
		names = weekdayNames["en"]
	}
	return names[int(t.Weekday())]
}
