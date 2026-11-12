// Command calendar is a Paperboy plugin (ADR 0048, ADR 0049). It reads the
// calendars of a CalDAV account (iCloud, Nextcloud, Fastmail, ...) and
// optional iCalendar links or files in the background, keeps the events in
// memory and draws them as widgets: "agenda" lists the upcoming events,
// "month" is a month grid. Both have a text variant for character displays.
//
// The configuration comes from its section in paperboy.yaml:
//
//	plugins:
//	  calendar:
//	    command: [calendar]
//	    config:
//	      caldav: https://caldav.icloud.com
//	      user: me@icloud.com
//	      password: ${PAPERBOY_CALDAV_PASSWORD}
//	      calendars: [Privat]
//
// iCloud needs an app-specific password from account.apple.com.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

// requestTimeout bounds one reading of all calendars.
const requestTimeout = 50 * time.Second

type options struct {
	caldav    string
	user      string
	password  string
	calendars []string
	birthdays []string
	ics       []string
	days      int
	maxEvents int
	language  string
	location  *time.Location
	icsFiles  []string
	refresh   time.Duration
}

func main() {
	plugin.Serve(&calendarPlugin{})
}

// configKeys are the keys of the plugin's config section.
var configKeys = map[string]plugin.Arg{
	"caldav":             {Type: "string", Description: "CalDAV server or calendar URL, e.g. https://caldav.icloud.com"},
	"user":               {Type: "string", Description: "CalDAV user name, for iCloud the Apple ID"},
	"password":           {Type: "string", Description: "CalDAV password; use ${VAR} to read it from the environment"},
	"calendars":          {Type: "list", Description: "CalDAV calendars to include by name; default all"},
	"birthday_calendars": {Type: "list", Description: "calendars whose yearly events are birthdays; their titles carry the age"},
	"ics":                {Type: "list", Description: "iCalendar URLs (https:// or webcal://)"},
	"ics_files":          {Type: "list", Description: "local iCalendar files"},
	"days":               {Type: "int", Default: 14, Description: "days from today the agenda covers"},
	"max_events":         {Type: "int", Default: 200},
	"refresh":            {Type: "duration", Default: "15m", Description: "how often the calendars are read again"},
}

// parseConfig turns the config section into options.
func parseConfig(cfg plugin.Values, language string, loc *time.Location) (options, error) {
	var o options
	if err := cfg.Check(configKeys); err != nil {
		return o, err
	}
	o.caldav = cfg.String("caldav", "")
	o.user = cfg.String("user", "")
	o.password = cfg.String("password", "")
	o.calendars = cfg.Strings("calendars")
	o.birthdays = cfg.Strings("birthday_calendars")
	o.ics = cfg.Strings("ics")
	o.icsFiles = cfg.Strings("ics_files")
	o.days = cfg.Int("days", 14)
	o.maxEvents = cfg.Int("max_events", 200)
	refresh, err := cfg.Duration("refresh", 15*time.Minute)
	if err != nil {
		return o, err
	}
	o.refresh = refresh
	o.language, o.location = language, loc
	if o.caldav == "" && len(o.ics) == 0 && len(o.icsFiles) == 0 {
		return o, errors.New("caldav, ics or ics_files is required")
	}
	if o.caldav != "" {
		if o.user == "" || o.password == "" {
			return o, errors.New("caldav needs user and password")
		}
	} else if len(o.calendars) > 0 {
		return o, errors.New("calendars needs caldav")
	}
	if o.days < 1 || o.days > 366 {
		return o, errors.New("days must be within 1..366")
	}
	if o.maxEvents < 1 {
		return o, errors.New("max_events must be positive")
	}
	if o.refresh < time.Minute {
		return o, errors.New("refresh must be at least 1m")
	}
	return o, nil
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
	if err := checkBirthdayCalendars(o.birthdays, names); err != nil {
		return branch{}, nil, err
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

// checkBirthdayCalendars verifies that every configured birthday calendar is
// among the loaded ones, so a renamed calendar does not silently drop the ages.
func checkBirthdayCalendars(wanted, names []string) error {
	for _, w := range wanted {
		found := false
		for _, n := range names {
			if strings.EqualFold(strings.TrimSpace(w), n) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("birthday calendar %q not found; available: %s", w, strings.Join(names, ", "))
		}
	}
	return nil
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
	First    string `json:"first,omitempty"`
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
	first := o.First
	if first.IsZero() {
		first = o.Start
	}
	e := event{
		Start:    o.Start.UTC().Format(time.RFC3339),
		End:      o.End.UTC().Format(time.RFC3339),
		First:    first.UTC().Format(time.RFC3339),
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
		e.First = time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, loc).UTC().Format(time.RFC3339)
		e.Date = s.Format("2006-01-02")
		e.EndDate = end.AddDate(0, 0, -1).Format("2006-01-02")
		return e
	}
	start, end := o.Start.In(loc), o.End.In(loc)
	e.First = first.In(loc).Format(time.RFC3339)
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
