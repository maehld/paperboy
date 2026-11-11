// Command countdown is a Paperboy plugin (ADR 0049) that shows how many days
// are left until a date and, when the widget names a reference date, a bar
// for the part of the span that has passed. A target with a time of day
// counts hours and minutes once less than a day is left.
//
// Date, label and reference live in the widget's args, so one page may show
// several countdowns:
//
//	plugins:
//	  countdown: {}
//
//	pages/weihnachten.yaml:
//	  cells:
//	    - { at: [0, 0], span: [3, 2], widget: countdown/countdown,
//	        args: { label: Weihnachten, date: "12-24", since: "01-01" } }
package main

import (
	"fmt"
	"image"
	"math"
	"strings"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

func main() { plugin.Serve(countdown{}) }

// countdown implements plugin.Handler and plugin.Texter.
type countdown struct{}

var (
	_ plugin.Handler = countdown{}
	_ plugin.Texter  = countdown{}
)

var widgetArgs = map[string]plugin.Arg{
	"date": {
		Type: "string", Required: true,
		Description: "target date: YYYY-MM-DD, YYYY-MM-DDTHH:MM, or MM-DD for a date that repeats every year",
	},
	"label": {
		Type:        "string",
		Description: "text above the number; optional",
	},
	"since": {
		Type:        "string",
		Description: "reference date for the progress bar: YYYY-MM-DD, YYYY-MM-DDTHH:MM, or MM-DD for the occurrence before date; without it there is no bar",
	},
}

func (countdown) Describe() plugin.Description {
	return plugin.Description{
		Title:   "Countdown",
		Version: version(),
		Widgets: []plugin.WidgetInfo{{
			Widget:    "countdown",
			Title:     "Days to a date",
			Kinds:     []string{plugin.KindImage, plugin.KindText},
			Sizes:     []plugin.Span{{Columns: 3, Rows: 1}, {Columns: 3, Rows: 2}, {Columns: 3, Rows: 4}, {Columns: 6, Rows: 2}},
			MinWidth:  120,
			MinHeight: 40,
			Args:      widgetArgs,
		}},
	}
}

// Configure accepts an empty config section: everything a countdown needs is
// a widget argument.
func (countdown) Configure(plugin.Values, *plugin.Request) error { return nil }

// args is one widget's arguments with the dates resolved for the request.
type args struct {
	label  string
	target time.Time
	yearly bool // the target repeats every year
	timed  bool // the target names a time of day
	since  time.Time
	bar    bool // a reference date is set, so the widget draws a bar
	now    time.Time
	loc    *time.Location
}

// state is what one widget shows at the request time.
type state struct {
	passed   bool
	today    bool
	days     int           // calendar days to a date-only target, negative after it
	timed    bool          // less than a day is left to a target with a time
	remain   time.Duration // to a target with a time
	fraction float64       // passed part of the span, 0..1; only with a reference
	bar      bool
}

func parseArgs(req *plugin.Request, w plugin.WidgetRequest) (*args, error) {
	a := w.Arg()
	if err := a.Check(widgetArgs); err != nil {
		return nil, err
	}
	loc := req.Location()
	out := &args{label: strings.TrimSpace(a.String("label", "")), now: req.Now.In(loc), loc: loc}
	target, yearly, timed, err := parseDate(strings.TrimSpace(a.String("date", "")), out.now, loc)
	if err != nil {
		return nil, err
	}
	out.target, out.yearly, out.timed = target, yearly, timed
	if s := strings.TrimSpace(a.String("since", "")); s != "" {
		since, err := parseSince(s, target, loc)
		if err != nil {
			return nil, err
		}
		if !since.Before(target) {
			return nil, fmt.Errorf("since must be before date")
		}
		out.since, out.bar = since, true
	}
	return out, nil
}

// parseDate resolves the date argument in loc. A date without a year repeats
// every year: the result is its next occurrence, today included.
func parseDate(s string, now time.Time, loc *time.Location) (t time.Time, yearly, timed bool, err error) {
	if s != "" {
		if v, err := time.ParseInLocation("2006-01-02T15:04", s, loc); err == nil {
			return v, false, true, nil
		}
		if v, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
			return v, false, false, nil
		}
		if md, err := time.Parse("01-02", s); err == nil {
			y, _, _ := now.Date()
			v := time.Date(y, md.Month(), md.Day(), 0, 0, 0, 0, loc)
			if daysBetween(now, v) < 0 {
				v = v.AddDate(1, 0, 0)
			}
			return v, true, false, nil
		}
	}
	return time.Time{}, false, false, fmt.Errorf("date: expected YYYY-MM-DD, YYYY-MM-DDTHH:MM or MM-DD, got %q", s)
}

// parseSince resolves the since argument. A date without a year is the
// occurrence before the target, so a yearly "01-01" measures the current
// cycle of a yearly target.
func parseSince(s string, target time.Time, loc *time.Location) (time.Time, error) {
	if v, err := time.ParseInLocation("2006-01-02T15:04", s, loc); err == nil {
		return v, nil
	}
	if v, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
		return v, nil
	}
	if md, err := time.Parse("01-02", s); err == nil {
		v := time.Date(target.Year(), md.Month(), md.Day(), 0, 0, 0, 0, loc)
		if !v.Before(target) {
			v = v.AddDate(-1, 0, 0)
		}
		return v, nil
	}
	return time.Time{}, fmt.Errorf("since: expected YYYY-MM-DD, YYYY-MM-DDTHH:MM or MM-DD, got %q", s)
}

// daysBetween counts calendar days from a to b, so a DST change does not
// shift a count.
func daysBetween(a, b time.Time) int {
	return int((dayStart(b) - dayStart(a)) / 86400)
}

// dayStart is the midnight of t's calendar day as a Unix time in UTC; the
// zone is irrelevant because only the difference of two of them is used.
func dayStart(t time.Time) int64 {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix()
}

func (a *args) evaluate() state {
	st := state{bar: a.bar}
	if a.timed {
		st.remain = a.target.Sub(a.now)
		st.passed = st.remain <= 0
		st.timed = !st.passed && st.remain < 24*time.Hour
		if !st.passed && !st.timed {
			st.days = daysBetween(a.now, a.target)
		}
	} else {
		st.days = daysBetween(a.now, a.target)
		st.today = st.days == 0
		st.passed = st.days < 0
	}
	if a.bar {
		span := a.target.Sub(a.since)
		st.fraction = math.Min(math.Max(float64(a.now.Sub(a.since))/float64(span), 0), 1)
	}
	return st
}

// MaxAge ends at the next local midnight, when the day count changes; a
// countdown in its last day counts minutes and refreshes more often.
func (countdown) MaxAge(req *plugin.Request) time.Duration {
	loc := req.Location()
	now := req.Now.In(loc)
	d := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, loc).Sub(now)
	for _, w := range req.Widgets {
		if w.Widget != "countdown" {
			continue
		}
		a, err := parseArgs(req, w)
		if err != nil {
			continue
		}
		if a.evaluate().timed {
			d = min(d, 30*time.Second)
		}
	}
	return max(d, time.Second)
}

func (countdown) Render(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	if w.Widget != "countdown" {
		return fmt.Errorf("%w %q", plugin.ErrUnknownWidget, w.Widget)
	}
	a, err := parseArgs(req, w)
	if err != nil {
		return err
	}
	draw(a, req, w, img)
	return nil
}

// Text fills the widget on a text display: the value, the bar and the target
// date, as many lines as the cell has rows.
func (countdown) Text(req *plugin.Request, w plugin.WidgetRequest) ([]string, error) {
	if w.Widget != "countdown" {
		return nil, fmt.Errorf("%w %q", plugin.ErrUnknownWidget, w.Widget)
	}
	a, err := parseArgs(req, w)
	if err != nil {
		return nil, err
	}
	st := a.evaluate()
	value := a.textValue(req, st, w.Width)
	lines := make([]string, 0, 3)
	if a.label != "" {
		lines = append(lines, plugin.Columns(a.label, value, w.Width))
	} else {
		lines = append(lines, plugin.Align(value, w.Width, "center"))
	}
	if st.bar && len(lines) < w.Height {
		lines = append(lines, barLine(st.fraction, w.Width))
	}
	if date := a.dateText(req); date != "" && len(lines) < w.Height {
		lines = append(lines, plugin.Align(date, w.Width, "left"))
	}
	return lines, nil
}

// textValue is the value on a text display, short enough for the cell.
func (a *args) textValue(req *plugin.Request, st state, width int) string {
	switch {
	case st.passed:
		return req.T("vorbei", "passed")
	case st.today:
		return req.T("heute", "today")
	case st.timed:
		h, m := int(st.remain.Hours()), int(st.remain.Minutes())%60
		switch {
		case h > 0 && m > 0:
			if long := fmt.Sprintf("%d %s %d %s", h, req.T("Std", "h"), m, req.T("Min", "min")); len([]rune(long)) <= width {
				return long
			}
			return fmt.Sprintf("%dh%02dm", h, m)
		case h > 0:
			return fmt.Sprintf("%d %s", h, req.T("Std", "h"))
		default:
			return fmt.Sprintf("%d %s", max(m, 1), req.T("Min", "min"))
		}
	}
	unit := req.T("Tage", "days")
	if st.days == 1 {
		unit = req.T("Tag", "day")
	}
	if long := fmt.Sprintf("%d %s", st.days, unit); len([]rune(long)) <= width {
		return long
	}
	return fmt.Sprintf("%d d", st.days)
}

// barLine draws the passed part of the span with the characters a 20x4
// display has.
func barLine(fraction float64, width int) string {
	if width < 4 {
		return ""
	}
	n := width - 2
	filled := int(math.Round(float64(n) * fraction))
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", n-filled) + "]"
}

// dateText is the target date below the bar; a date without a year repeats,
// so it shows no year.
func (a *args) dateText(req *plugin.Request) string {
	t := a.target.In(a.loc)
	if req.Lang() == "en" {
		switch {
		case a.yearly && a.timed:
			return t.Format("Jan 2, 15:04")
		case a.yearly:
			return t.Format("Jan 2")
		case a.timed:
			return t.Format("Jan 2, 2006 15:04")
		}
		return t.Format("Jan 2, 2006")
	}
	switch {
	case a.yearly && a.timed:
		return t.Format("2.1. 15:04")
	case a.yearly:
		return t.Format("2.1.")
	case a.timed:
		return t.Format("2.1.2006 15:04")
	}
	return t.Format("2.1.2006")
}
