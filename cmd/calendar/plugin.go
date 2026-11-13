package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

// calendarPlugin draws the calendar widgets "agenda", "month" and "week".
type calendarPlugin struct {
	opts options
	loc  *time.Location
	lang string
	now  time.Time

	events    []event  // snapshot for the current request
	calendars []string // the names of the loaded calendars, for the widget filter

	mu              sync.Mutex
	loaded          []event
	loadedCalendars []string
	loadErr         error
	ready           chan struct{} // closed after the first load
}

// Start loads the calendars in the background, right away and then every
// -refresh, and reports a change when the events differ.
func (p *calendarPlugin) Start(ctx context.Context, notify func(widgets ...string)) {
	if p.ready == nil {
		p.ready = make(chan struct{})
	}
	go func() {
		first := true
		var last string
		for {
			b, err := p.load(ctx)
			p.mu.Lock()
			if err != nil {
				p.loadErr = err
				fmt.Fprintln(os.Stderr, "calendar:", err)
			} else {
				p.loaded, p.loadedCalendars, p.loadErr = b.Events, b.Calendars, nil
			}
			p.mu.Unlock()
			if err == nil {
				sum, _ := json.Marshal(b.Events)
				if !first && string(sum) != last {
					notify()
				}
				last = string(sum)
			}
			if first {
				close(p.ready)
				first = false
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(p.opts.refresh):
			}
		}
	}()
}

// load reads all sources for a window that starts a week before the first of
// the month, so the month grid shows past days too.
func (p *calendarPlugin) load(ctx context.Context) (branch, error) {
	o := p.opts
	local := time.Now().In(o.location)
	from := midnight(local).AddDate(0, 0, -(local.Day() - 1 + 7))
	o.days = max(o.days, 42) + local.Day() - 1 + 7
	o.maxEvents = max(o.maxEvents, 1000)
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	b, _, err := collect(ctx, o, newClient(o.user, o.password), from)
	return b, err
}

func (p *calendarPlugin) Describe() plugin.Description {
	both := []string{plugin.KindImage, plugin.KindText}
	calendars := plugin.Arg{Type: "list", Description: "only these calendars; default all"}
	if choices := p.calendarChoices(); len(choices) > 0 {
		calendars.Enum = choices
	}
	return plugin.Description{
		Title:   "Calendar",
		Version: version(),
		Config:  configKeys,
		Widgets: []plugin.WidgetInfo{
			{
				Widget: "agenda", Title: "Upcoming events", Kinds: both,
				Sizes:    []plugin.Span{{Columns: 3, Rows: 4}, {Columns: 3, Rows: 7}, {Columns: 6, Rows: 4}},
				MinWidth: 160, MinHeight: 80,
				Args: map[string]plugin.Arg{
					"lines":     {Type: "int", Description: "number of text lines on image displays; default: as many as the page's text size allows (text displays use the cell height)"},
					"location":  {Type: "bool", Default: false, Description: "append the location to the title"},
					"headings":  {Type: "bool", Default: true, Description: "day headings"},
					"days":      {Type: "int", Description: "days to show starting today, 1 to the configured days (clamped); default: the configured days"},
					"empty":     {Type: "bool", Description: "show days without events with \"No events\"; default true when days is set"},
					"age":       {Type: "bool", Default: false, Description: "append the age of birthdays from the configured birthday calendars"},
					"more":      {Type: "bool", Default: true, Description: "show \"+N more\" for events that did not fit"},
					"calendars": calendars,
				},
			},
			{
				Widget: "birthdays", Title: "Next birthdays", Kinds: both,
				Sizes:    []plugin.Span{{Columns: 3, Rows: 4}, {Columns: 3, Rows: 7}, {Columns: 6, Rows: 4}},
				MinWidth: 160, MinHeight: 80,
				Args: map[string]plugin.Arg{
					"lines":     {Type: "int", Description: "number of text lines on image displays; default: as many as the page's text size allows (text displays use the cell height)"},
					"headings":  {Type: "bool", Default: true, Description: "day headings"},
					"days":      {Type: "int", Description: "days to show starting today, 1 to the loaded window (the configured days, but at least the month grid's 42 days); default: the loaded window"},
					"more":      {Type: "bool", Default: true, Description: "show \"+N more\" for birthdays that did not fit"},
					"calendars": calendars,
				},
			},
			{
				Widget: "month", Title: "Month grid", Kinds: both,
				Sizes:    []plugin.Span{{Columns: 3, Rows: 7}, {Columns: 6, Rows: 7}},
				MinWidth: 210, MinHeight: 160,
				Args: map[string]plugin.Arg{
					"titles":    {Type: "bool", Default: true, Description: "event titles in the day cells when they fit"},
					"calendars": calendars,
				},
			},
			{
				Widget: "week", Title: "Week view", Kinds: both,
				Sizes:    []plugin.Span{{Columns: 6, Rows: 4}, {Columns: 6, Rows: 7}},
				MinWidth: 320, MinHeight: 160,
				Args: map[string]plugin.Arg{
					"days":      {Type: "int", Default: 7, Description: "number of day columns, 1 to 14 (5 for a work week); text displays show as many days as lines"},
					"start":     {Type: "string", Default: "week", Description: "first column: week = start of the week (Monday for German, Sunday otherwise), today = today (default week; today on text displays)", Enum: []any{"week", "today"}},
					"more":      {Type: "bool", Default: true, Description: "show \"+N more\" for events that did not fit"},
					"calendars": calendars,
				},
			},
		},
	}
}

// calendarChoices lists the loaded calendar names for the widget arg's enum,
// so the editors offer one check box per calendar. Before the first load it
// falls back to the configured names and waits for the load when those are
// empty too.
func (p *calendarPlugin) calendarChoices() []any {
	p.mu.Lock()
	ready := p.ready
	names := append([]string(nil), p.loadedCalendars...)
	if len(names) == 0 {
		names = append([]string(nil), p.opts.calendars...)
	}
	p.mu.Unlock()
	if len(names) == 0 && ready != nil {
		select {
		case <-ready:
		case <-time.After(10 * time.Second):
		}
		p.mu.Lock()
		names = append([]string(nil), p.loadedCalendars...)
		p.mu.Unlock()
	}
	if len(names) == 0 {
		return nil
	}
	out := make([]any, len(names))
	for i, name := range names {
		out[i] = name
	}
	return out
}

// Configure reads the config section; the calendars load in Start.
func (p *calendarPlugin) Configure(cfg plugin.Values, req *plugin.Request) error {
	o, err := parseConfig(cfg, req.Lang(), req.Location())
	if err != nil {
		return err
	}
	p.opts = o
	if p.ready == nil {
		p.ready = make(chan struct{})
	}
	return nil
}

// MaxAge lets Paperboy reuse the images for 15 minutes: changed calendars
// are reported as events, and ended events leave the agenda within that time.
func (p *calendarPlugin) MaxAge(*plugin.Request) time.Duration { return 15 * time.Minute }

func (p *calendarPlugin) Prepare(req *plugin.Request) error {
	p.loc = req.Location()
	p.lang = req.Language
	if p.lang != "de" {
		p.lang = "en"
	}
	p.now = req.Now
	if p.now.IsZero() {
		p.now = time.Now()
	}
	select {
	case <-p.ready:
	case <-time.After(10 * time.Second):
		return fmt.Errorf("calendars are still loading")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.loaded == nil && p.loadErr != nil {
		return p.loadErr
	}
	p.events = p.loaded
	p.calendars = p.loadedCalendars
	return nil
}

func (p *calendarPlugin) Render(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	switch w.Widget {
	case "agenda", "birthdays":
		return p.agenda(req, w, img)
	case "month":
		return p.month(req, w, img)
	case "week":
		return p.week(req, w, img)
	default:
		return fmt.Errorf("%w %q", plugin.ErrUnknownWidget, w.Widget)
	}
}

// Text fills the widgets of a character display: the agenda as "time title"
// lines, the month as the remaining days of the week with their events.
func (p *calendarPlugin) Text(req *plugin.Request, w plugin.WidgetRequest) ([]string, error) {
	switch w.Widget {
	case "agenda", "birthdays":
		opts, err := p.widgetArgs(w, w.Height > 2)
		if err != nil {
			return nil, err
		}
		lines, hidden := p.agendaLines(w.Height, opts)
		// A text display puts the summary on a line of its own, so keep one free
		// while events remain.
		if opts.more && hidden > 0 && len(lines) >= w.Height && w.Height > 1 {
			lines, hidden = p.agendaLines(w.Height-1, opts)
		}
		if len(lines) == 0 {
			return []string{p.emptyText(opts)}, nil
		}
		if opts.more && hidden > 0 && len(lines) < w.Height {
			lines = append(lines, agendaLine{text: p.moreText(hidden)})
		}
		var out []string
		for _, l := range lines {
			switch {
			case l.heading:
				out = append(out, l.text+":")
			case l.when != "":
				out = append(out, shortWhen(l.when)+" "+p.lineText(l, false))
			default:
				out = append(out, p.lineText(l, false))
			}
		}
		return out, nil
	case "week":
		evs, err := p.eventsFor(w)
		if err != nil {
			return nil, err
		}
		return p.weekLines(w, evs), nil
	case "month":
		evs, err := p.eventsFor(w)
		if err != nil {
			return nil, err
		}
		now := p.now.In(p.loc)
		today := midnight(now)
		byDay := p.eventsByDay(evs, today, w.Height)
		var out []string
		for i := range w.Height {
			day := today.AddDate(0, 0, i)
			line := fmt.Sprintf("%s %2d", weekday(day, p.lang), day.Day())
			if len(byDay[i]) > 0 {
				line += " " + strings.Join(byDay[i], ", ")
			}
			out = append(out, line)
		}
		return out, nil
	}
	return nil, fmt.Errorf("%w %q", plugin.ErrUnknownWidget, w.Widget)
}

// shortWhen keeps the time column of a text agenda narrow.
func shortWhen(s string) string {
	switch s {
	case "ganztägig", "all day":
		return "*"
	}
	return s
}

func (p *calendarPlugin) text(de, en string) string {
	if p.lang == "de" {
		return de
	}
	return en
}

// ---- agenda ----

type agendaLine struct {
	heading  bool
	event    bool
	when     string
	text     string // title, or the heading or the "no events" text
	location string // appended to the title when the cell asks for it
	age      int    // birthday age in years, 0 when unknown
}

// lineText returns what a cell shows for an event line; text displays put the
// age in parentheses to keep the line short.
func (p *calendarPlugin) lineText(l agendaLine, wide bool) string {
	text := l.text
	if l.age > 0 {
		if wide {
			text += " " + fmt.Sprintf(p.text("wird %d", "turns %d"), l.age)
		} else {
			text += fmt.Sprintf(" (%d)", l.age)
		}
	}
	if l.location != "" {
		text += " · " + l.location
	}
	return text
}

type parsedEvent struct {
	start, end time.Time
	first      time.Time // DTSTART of the series; the birth date of a birthday
	allDay     bool
	title      string
	location   string
	calendar   string
	birthday   bool
}

func (p *calendarPlugin) parsed(events []event) []parsedEvent {
	var out []parsedEvent
	for _, e := range events {
		s, err1 := time.Parse(time.RFC3339, e.Start)
		en, err2 := time.Parse(time.RFC3339, e.End)
		if err1 != nil {
			continue
		}
		if err2 != nil || en.Before(s) {
			en = s
		}
		first, err := time.Parse(time.RFC3339, e.First)
		if err != nil {
			first = s
		}
		out = append(out, parsedEvent{start: s, end: en, first: first, allDay: e.AllDay, title: e.Title, location: e.Location, calendar: e.Calendar, birthday: e.Birthday})
	}
	return out
}

// birthday reports whether an event is a birthday: either the CardDAV contacts
// already marked it, or it belongs to one of the configured birthday
// calendars, matched by display name like the calendars section.
func (p *calendarPlugin) birthday(e parsedEvent) bool {
	if e.birthday {
		return true
	}
	for _, name := range p.opts.birthdays {
		if strings.EqualFold(strings.TrimSpace(name), e.calendar) {
			return true
		}
	}
	return false
}

// birthdayAge returns the age a person turns on the day of the occurrence. The
// year comes from DTSTART of the recurring series, which the Apple and
// Nextcloud birthday calendars carry. It reports false for other calendars,
// implausible years and the day of birth itself.
func (p *calendarPlugin) birthdayAge(e parsedEvent) (int, bool) {
	if !p.birthday(e) || e.first.IsZero() {
		return 0, false
	}
	year := e.first.In(p.loc).Year()
	age := e.start.In(p.loc).Year() - year
	if year < 1900 || age < 1 || age > 130 {
		return 0, false
	}
	return age, true
}

func (p *calendarPlugin) agenda(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	a := w.Arg()
	pad := w.Style.Padding
	rule := w.Style.Rule
	area := img.Rect.Inset(pad)
	// Without a lines argument as many lines as the page's base text allows
	// (ADR 0050), so the text has the same physical size on every display.
	lines := max(a.Int("lines", area.Dy()*10/max(int(req.TextPx(w)*10), 1)), 1)
	lineH := area.Dy() / lines
	if area.Empty() || lineH < 1 {
		return plugin.TooSmall(w)
	}
	size := 0.6 * float64(lineH)
	baseline := func(i int) int { return area.Min.Y + i*lineH + lineH/2 + int(size*0.36) }

	opts, err := p.widgetArgs(w, lines > 1)
	if err != nil {
		return err
	}
	all, whenW, inline := p.agendaLayout(lines, opts, area, size)
	if len(all) == 0 {
		render.Text(img, p.emptyText(opts), area.Min.X+area.Dx()/2, baseline(0),
			render.Style{Size: size, Shrink: true}, area.Dx(), color.Black, render.Center)
		return nil
	}
	gap := int(size * 0.6)
	textX := area.Min.X
	if whenW > 0 {
		textX += whenW + gap
	}
	for i, l := range all {
		y := baseline(i)
		if l.heading {
			if i > 0 && rule > 0 {
				render.HLine(img, area.Min.X, area.Min.Y+i*lineH, area.Max.X, rule)
			}
			render.TextAt(img, render.Ellipsize(l.text, size, true, area.Dx()), area.Min.X, y, size, true, color.Black, render.Left)
			continue
		}
		if l.when != "" {
			render.Text(img, l.when, area.Min.X, y, render.Style{Size: size, Shrink: true}, whenW, color.Black, render.Left)
		}
		width := area.Max.X - textX
		last := i == len(all)-1
		if last && inline != "" {
			width -= render.Measure(inline, size, false) + gap
		}
		text := render.Ellipsize(p.lineText(l, true), size, false, width)
		render.TextAt(img, text, textX, y, size, false, color.Black, render.Left)
		if last && inline != "" {
			render.TextAt(img, inline, textX+render.Measure(text, size, false)+gap, y, size, false, color.Black, render.Left)
		}
	}
	return nil
}

// agendaLayout fits the agenda into a cell of lines lines: it returns the lines
// to draw, the width of the time column and the "+N more" summary when that one
// goes behind the last line instead of taking a line of its own.
func (p *calendarPlugin) agendaLayout(lines int, o agendaOpts, area image.Rectangle, size float64) ([]agendaLine, int, string) {
	gap := int(size * 0.6)
	columns := func(all []agendaLine) (whenW, textX int) {
		for _, l := range all {
			if !l.heading && l.when != "" {
				whenW = max(whenW, render.Measure(l.when, size, false))
			}
		}
		whenW = min(whenW, area.Dx()/2)
		textX = area.Min.X
		if whenW > 0 {
			textX += whenW + gap
		}
		return whenW, textX
	}

	// A heading is only worth a line when another one is left for the events it
	// introduces, otherwise a small cell would show the day and claim that it
	// has no events.
	all, hidden := p.agendaLines(lines, o)
	if len(all) == 0 {
		return nil, 0, ""
	}
	whenW, textX := columns(all)
	// The summary follows the last event when it fits next to it without cutting
	// the title. Otherwise it needs a line of its own, and only a cell with more
	// than one line has one to spare.
	var inline string
	if hidden > 0 && o.more {
		more := p.moreText(hidden)
		// The separator matches the location that titles carry.
		behind := "· " + more
		switch {
		case p.agendaMoreFits(all[len(all)-1], behind, size, gap, area.Max.X-textX):
			inline = behind
		case lines > 1:
			all, hidden = p.agendaLines(lines-1, o)
			whenW, textX = columns(all)
			if hidden > 0 {
				all = append(all, agendaLine{text: p.moreText(hidden)})
			}
		}
	}
	return all, whenW, inline
}

// moreText counts the events that did not fit.
func (p *calendarPlugin) moreText(n int) string {
	return fmt.Sprintf(p.text("+%d weitere", "+%d more"), n)
}

// agendaMoreFits reports whether the summary fits behind the line without
// cutting its title; day headings and free days do not take one.
func (p *calendarPlugin) agendaMoreFits(l agendaLine, more string, size float64, gap, width int) bool {
	return l.event && render.Measure(p.lineText(l, true), size, false)+gap+render.Measure(more, size, false) <= width
}

// agendaOpts are the arguments of one agenda widget.
type agendaOpts struct {
	headings, location bool
	age                bool // append the age of birthdays
	more               bool // show "+N more" for events that did not fit
	birthdays          bool // only events of the configured birthday calendars
	days               int  // days from today, 1 to the configured days
	empty              bool // show days without events
	events             []event
}

// agendaArgs reads the agenda arguments; days is clamped to 1..configured days.
func (p *calendarPlugin) agendaArgs(a plugin.Values, headings bool) agendaOpts {
	limit := max(p.opts.days, 1)
	_, set := a["days"]
	return agendaOpts{
		headings: a.Bool("headings", headings),
		location: a.Bool("location", false),
		age:      a.Bool("age", false),
		more:     a.Bool("more", true),
		days:     min(max(a.Int("days", limit), 1), limit),
		empty:    a.Bool("empty", set),
	}
}

// widgetArgs reads the arguments of the agenda or the birthdays widget and
// narrows the events to the calendars it asks for.
func (p *calendarPlugin) widgetArgs(w plugin.WidgetRequest, headings bool) (agendaOpts, error) {
	evs, err := p.eventsFor(w)
	if err != nil {
		return agendaOpts{}, err
	}
	var o agendaOpts
	if w.Widget == "birthdays" {
		o = p.birthdayArgs(w.Arg(), headings)
	} else {
		o = p.agendaArgs(w.Arg(), headings)
	}
	o.events = evs
	return o, nil
}

// eventsFor narrows the loaded events to the widget's calendars argument. An
// unknown name is an error that lists the available calendars, so a renamed
// calendar does not silently leave an empty cell. CheckEnums already rejects
// it once the describe answer carries the enum; this also covers the phase
// before the first load, when the enum is still missing.
func (p *calendarPlugin) eventsFor(w plugin.WidgetRequest) ([]event, error) {
	wanted := w.Arg().Strings("calendars")
	if len(wanted) == 0 {
		return p.events, nil
	}
	known := make(map[string]bool, len(p.calendars))
	for _, name := range p.calendars {
		known[strings.ToLower(strings.TrimSpace(name))] = true
	}
	picked := make(map[string]bool, len(wanted))
	for _, name := range wanted {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" {
			continue
		}
		if !known[key] {
			return nil, fmt.Errorf("calendar %q not found; available: %s", strings.TrimSpace(name), strings.Join(p.calendars, ", "))
		}
		picked[key] = true
	}
	if len(picked) == 0 {
		return p.events, nil
	}
	out := make([]event, 0, len(p.events))
	for _, e := range p.events {
		if picked[strings.ToLower(strings.TrimSpace(e.Calendar))] {
			out = append(out, e)
		}
	}
	return out, nil
}

// birthdayArgs reads the arguments of the birthdays widget: only birthdays,
// always with the age and never a day without one. The window reaches as far as
// the loaded events do, which is at least the 42 days the month grid needs.
func (p *calendarPlugin) birthdayArgs(a plugin.Values, headings bool) agendaOpts {
	limit := max(p.opts.days, 42)
	return agendaOpts{
		birthdays: true,
		age:       true,
		headings:  a.Bool("headings", headings),
		more:      a.Bool("more", true),
		days:      min(max(a.Int("days", limit), 1), limit),
	}
}

// emptyText is the message of a cell without events to show.
func (p *calendarPlugin) emptyText(o agendaOpts) string {
	if o.birthdays {
		return p.text("Keine Geburtstage", "No birthdays")
	}
	return p.text("Keine Termine", "No events")
}

// agendaLines returns at most maxLines lines and the number of events that did
// not fit. Neither a day heading nor a day without events may use the last line
// of a small cell while events are left, otherwise the cell would report that
// there are none.
func (p *calendarPlugin) agendaLines(maxLines int, o agendaOpts) ([]agendaLine, int) {
	lines, hidden, events := p.agendaList(maxLines, o)
	if events == 0 && hidden > 0 {
		if o.headings {
			o.headings = false
			lines, hidden, events = p.agendaList(maxLines, o)
		}
		if events == 0 && o.empty {
			o.empty = false
			lines, hidden, events = p.agendaList(maxLines, o)
		}
	}
	return lines, hidden
}

// agendaList builds the agenda lines, cuts them to maxLines and counts the
// events that did not fit and those that did.
func (p *calendarPlugin) agendaList(maxLines int, o agendaOpts) ([]agendaLine, int, int) {
	headings, location := o.headings, o.location
	now := p.now.In(p.loc)
	today := midnight(now)
	type dated struct {
		parsedEvent
		day   time.Time
		empty bool
	}
	var upcoming []dated
	horizon := today.AddDate(0, 0, o.days)
	events := o.events
	if events == nil {
		events = p.events
	}
	for _, e := range p.parsed(events) {
		if o.birthdays && !p.birthday(e) {
			continue
		}
		if !e.start.Before(horizon) {
			continue
		}
		if !e.end.After(now) && !(e.end.Equal(e.start) && !e.start.Before(now)) {
			continue
		}
		day := midnight(e.start.In(p.loc))
		if day.Before(today) {
			day = today
		}
		upcoming = append(upcoming, dated{parsedEvent: e, day: day})
	}
	if o.empty {
		busy := map[time.Time]bool{}
		for _, d := range upcoming {
			busy[d.day] = true
		}
		for i := range o.days {
			if day := today.AddDate(0, 0, i); !busy[day] {
				upcoming = append(upcoming, dated{day: day, empty: true})
			}
		}
	}
	sort.SliceStable(upcoming, func(i, j int) bool {
		a, b := upcoming[i], upcoming[j]
		if !a.day.Equal(b.day) {
			return a.day.Before(b.day)
		}
		if a.empty != b.empty {
			return a.empty
		}
		if a.allDay != b.allDay {
			return a.allDay
		}
		return a.start.Before(b.start)
	})
	var all []agendaLine
	var eventIdx []int
	var lastDay time.Time
	for _, d := range upcoming {
		if headings && !d.day.Equal(lastDay) {
			all = append(all, agendaLine{heading: true, text: p.dayLabel(d.day, today)})
			lastDay = d.day
		}
		if d.empty {
			line := agendaLine{text: p.emptyText(o)}
			if !headings && !d.day.Equal(today) {
				line.when = weekday(d.day, p.lang)
			}
			all = append(all, line)
			continue
		}
		line := agendaLine{event: true, when: p.when(d.parsedEvent, d.day, today, headings), text: strings.TrimSpace(d.title)}
		if o.birthdays && headings {
			// The day heading already names the date; a birthday is all-day.
			line.when = ""
		}
		if location && strings.TrimSpace(d.location) != "" {
			line.location = strings.TrimSpace(d.location)
		}
		if o.age {
			if age, ok := p.birthdayAge(d.parsedEvent); ok {
				line.age = age
			}
		}
		eventIdx = append(eventIdx, len(all))
		all = append(all, line)
	}
	if len(all) <= maxLines {
		return all, 0, len(eventIdx)
	}
	keep := maxLines
	for keep > 0 && all[keep-1].heading {
		keep--
	}
	shown := 0
	for _, idx := range eventIdx {
		if idx < keep {
			shown++
		}
	}
	return all[:keep], len(eventIdx) - shown, shown
}

func (p *calendarPlugin) when(e parsedEvent, day, today time.Time, headings bool) string {
	allDay := p.text("ganztägig", "all day")
	var w string
	switch {
	case e.allDay:
		w = allDay
	case e.start.In(p.loc).Before(day):
		end := e.end.In(p.loc)
		if end.Before(day.AddDate(0, 0, 1)) {
			w = p.text("bis ", "until ") + end.Format("15:04")
		} else {
			w = allDay
		}
	default:
		w = e.start.In(p.loc).Format("15:04")
	}
	if !headings && !day.Equal(today) {
		w = weekday(day, p.lang) + " " + w
	}
	return w
}

func (p *calendarPlugin) dayLabel(day, today time.Time) string {
	switch {
	case day.Equal(today):
		return p.text("Heute", "Today")
	case day.Equal(today.AddDate(0, 0, 1)):
		return p.text("Morgen", "Tomorrow")
	}
	if p.lang == "de" {
		return fmt.Sprintf("%s, %d.%d.", weekday(day, p.lang), day.Day(), int(day.Month()))
	}
	return fmt.Sprintf("%s, %s %d", weekday(day, p.lang), day.Month().String()[:3], day.Day())
}

func midnight(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// ---- month ----

var monthNames = map[string][12]string{
	"de": {"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"},
	"en": {"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"},
}

// month draws the current month as a grid from Monday to Sunday. Today is
// inverted; days with events show their titles when the cell is tall enough,
// otherwise a bar under the day number.
func (p *calendarPlugin) month(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	pad := w.Style.Padding
	area := img.Rect.Inset(pad)
	if area.Dx() < 70 || area.Dy() < 60 {
		return plugin.TooSmall(w)
	}
	now := p.now.In(p.loc)
	today := midnight(now)
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, p.loc)
	offset := (int(first.Weekday()) + 6) % 7 // Monday first
	gridStart := first.AddDate(0, 0, -offset)
	daysInMonth := first.AddDate(0, 1, -1).Day()
	weeks := (offset + daysInMonth + 6) / 7

	titleH := area.Dy() * 12 / 100
	headH := area.Dy() * 7 / 100
	title := fmt.Sprintf("%s %d", monthNames[p.lang][now.Month()-1], now.Year())
	render.Text(img, title, area.Min.X, area.Min.Y+titleH*78/100, render.Style{Size: float64(titleH) * 0.75, Bold: true, Shrink: true},
		area.Dx(), color.Black, render.Left)

	cols := 7
	cellW := area.Dx() / cols
	gridW := cellW * cols
	x0 := area.Min.X + (area.Dx()-gridW)/2
	headTop := area.Min.Y + titleH
	gridTop := headTop + headH
	cellH := (area.Max.Y - gridTop) / weeks
	levels := render.Levels(img.Palette)

	names := weekdayNames[p.lang]
	for c := range cols {
		name := names[(c+1)%7]
		cx := x0 + c*cellW + cellW/2
		render.Text(img, name, cx, headTop+headH*75/100, render.Style{Size: float64(headH) * 0.8, Bold: true, Shrink: true},
			cellW-2, color.Black, render.Center)
	}
	render.HLine(img, x0, gridTop-1, x0+gridW, 2)

	evs, err := p.eventsFor(w)
	if err != nil {
		return err
	}
	byDay := p.eventsByDay(evs, gridStart, weeks*7)
	numSize := min(float64(cellH)*0.32, float64(cellW)*0.42)
	lineSize := max(min(float64(cellH)*0.22, req.TextPx(w)*0.8), req.PtPx(6.3), render.MinSize)
	for i := range weeks * 7 {
		day := gridStart.AddDate(0, 0, i)
		c, r := i%7, i/7
		cell := image.Rect(x0+c*cellW, gridTop+r*cellH, x0+(c+1)*cellW, gridTop+(r+1)*cellH)
		if c >= 5 && levels > 2 {
			render.Fill(img, cell.Inset(1), "shade")
		}
		inMonth := day.Month() == now.Month()
		num := fmt.Sprint(day.Day())
		numX := cell.Min.X + 4
		numBase := cell.Min.Y + int(numSize) + 3
		ink := color.Color(color.Black)
		if day.Equal(today) {
			box := image.Rect(cell.Min.X+1, cell.Min.Y+1, cell.Min.X+render.Measure(num, numSize, true)+8, cell.Min.Y+int(numSize)+7)
			render.FillSolid(img, box)
			ink = color.White
		} else if !inMonth && levels > 2 {
			ink = render.DarkGray
		}
		if inMonth || levels > 2 || day.Equal(today) {
			render.TextAt(img, num, numX, numBase, numSize, day.Equal(today) || inMonth, ink, render.Left)
		}
		evs := byDay[i]
		if len(evs) == 0 || !inMonth {
			continue
		}
		top := numBase + 3
		room := (cell.Max.Y - 2 - top) / int(lineSize*1.15)
		if w.Arg().Bool("titles", true) && room >= 1 && cellW >= 50 {
			shown := evs
			if len(evs) > room {
				shown = evs[:max(room-1, 0)]
			}
			for j, t := range shown {
				base := top + (j+1)*int(lineSize*1.15) - 2
				render.TextAt(img, render.Ellipsize(t, lineSize, false, cell.Dx()-6), cell.Min.X+3, base, lineSize, false, color.Black, render.Left)
			}
			if len(shown) < len(evs) {
				base := top + (len(shown)+1)*int(lineSize*1.15) - 2
				render.TextAt(img, fmt.Sprintf("+%d", len(evs)-len(shown)), cell.Min.X+3, base, lineSize, true, color.Black, render.Left)
			}
			continue
		}
		// Too small for titles: one bar per event, at most four.
		bw := max((cell.Dx()-8)/4, 3)
		for j := range min(len(evs), 4) {
			x := cell.Min.X + 4 + j*bw
			render.FillSolid(img, image.Rect(x, cell.Max.Y-7, x+bw-2, cell.Max.Y-3))
		}
	}
	for c := 0; c <= cols; c++ {
		render.VLine(img, x0+c*cellW, gridTop, gridTop+weeks*cellH, 1)
	}
	for r := 1; r <= weeks; r++ {
		render.HLine(img, x0, gridTop+r*cellH, x0+gridW, 1)
	}
	return nil
}

// eventsByDay lists the event titles of every day of the grid, all-day
// events first.
func (p *calendarPlugin) eventsByDay(events []event, start time.Time, days int) [][]string {
	out := make([][]string, days)
	evs := p.parsed(events)
	sort.SliceStable(evs, func(i, j int) bool {
		if evs[i].allDay != evs[j].allDay {
			return evs[i].allDay
		}
		return evs[i].start.Before(evs[j].start)
	})
	for i := range days {
		dayStart := start.AddDate(0, 0, i)
		dayEnd := dayStart.AddDate(0, 0, 1)
		for _, e := range evs {
			if e.start.Before(dayEnd) && (e.end.After(dayStart) || (e.end.Equal(e.start) && !e.start.Before(dayStart))) {
				out[i] = append(out[i], e.title)
			}
		}
	}
	return out
}
