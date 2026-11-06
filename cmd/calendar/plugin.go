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

// calendarPlugin draws the calendar as image fragments (spike: plugin
// fragments) instead of writing the calendar branch: "agenda" mirrors the
// agenda block of the page renderer, "month" is a month grid that page blocks
// cannot express.
type calendarPlugin struct {
	opts options
	loc  *time.Location
	lang string
	now  time.Time

	events []event // snapshot for the current request

	mu      sync.Mutex
	loaded  []event
	loadErr error
	ready   chan struct{} // closed after the first load
}

// Start loads the calendars in the background, right away and then every
// -refresh, and reports a change when the events differ.
func (p *calendarPlugin) Start(ctx context.Context, notify func(images ...string)) {
	p.ready = make(chan struct{})
	go func() {
		first := true
		var last string
		for {
			events, err := p.load(ctx)
			p.mu.Lock()
			if err != nil {
				p.loadErr = err
				fmt.Fprintln(os.Stderr, "calendar:", err)
			} else {
				p.loaded, p.loadErr = events, nil
			}
			p.mu.Unlock()
			if err == nil {
				sum, _ := json.Marshal(events)
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
func (p *calendarPlugin) load(ctx context.Context) ([]event, error) {
	o := p.opts
	local := time.Now().In(o.location)
	from := midnight(local).AddDate(0, 0, -(local.Day() - 1 + 7))
	o.days = max(o.days, 42) + local.Day() - 1 + 7
	o.maxEvents = max(o.maxEvents, 1000)
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	b, _, err := collect(ctx, o, newClient(o.user, o.password), from)
	if err != nil {
		return nil, err
	}
	return b.Events, nil
}

func (p *calendarPlugin) Describe() plugin.Description {
	return plugin.Description{
		Title: "Calendar",
		Images: []plugin.ImageInfo{
			{
				Image: "agenda", Title: "Upcoming events",
				Sizes:    []plugin.Span{{Columns: 1, Rows: 2}, {Columns: 2, Rows: 2}, {Columns: 3, Rows: 3}},
				MinWidth: 160, MinHeight: 80,
				Args: map[string]plugin.Arg{
					"lines":    {Type: "int", Default: 10, Description: "number of text lines"},
					"location": {Type: "bool", Default: false, Description: "append the location to the title"},
					"headings": {Type: "bool", Default: true, Description: "day headings"},
					"rule":     {Type: "int", Default: 1, Description: "separator above later day headings, px"},
					"padding":  {Type: "int", Default: 10, Description: "inner margin, px"},
				},
			},
			{
				Image: "month", Title: "Month grid",
				Sizes:    []plugin.Span{{Columns: 2, Rows: 2}, {Columns: 3, Rows: 3}},
				MinWidth: 210, MinHeight: 160,
				Args: map[string]plugin.Arg{
					"titles":  {Type: "bool", Default: true, Description: "event titles in the day cells when they fit"},
					"padding": {Type: "int", Default: 6, Description: "inner margin, px"},
				},
			},
		},
	}
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
	case <-time.After(requestTimeout):
		return fmt.Errorf("calendars not loaded yet")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.loaded == nil && p.loadErr != nil {
		return p.loadErr
	}
	p.events = p.loaded
	return nil
}

func (p *calendarPlugin) Render(req *plugin.Request, ir plugin.ImageRequest, img *image.Paletted) error {
	switch ir.Image {
	case "agenda":
		p.agenda(ir, img)
	case "month":
		p.month(ir, img)
	default:
		return fmt.Errorf("%w %q", plugin.ErrUnknownImage, ir.Image)
	}
	return nil
}

func (p *calendarPlugin) text(de, en string) string {
	if p.lang == "de" {
		return de
	}
	return en
}

// ---- agenda ----

type agendaLine struct {
	heading bool
	when    string
	text    string
}

type parsedEvent struct {
	start, end time.Time
	allDay     bool
	title      string
	location   string
}

func (p *calendarPlugin) parsed() []parsedEvent {
	var out []parsedEvent
	for _, e := range p.events {
		s, err1 := time.Parse(time.RFC3339, e.Start)
		en, err2 := time.Parse(time.RFC3339, e.End)
		if err1 != nil {
			continue
		}
		if err2 != nil || en.Before(s) {
			en = s
		}
		out = append(out, parsedEvent{start: s, end: en, allDay: e.AllDay, title: e.Title, location: e.Location})
	}
	return out
}

func (p *calendarPlugin) agenda(ir plugin.ImageRequest, img *image.Paletted) {
	lines := max(ir.IntArg("lines", 10), 1)
	pad := max(ir.IntArg("padding", 10), 0)
	rule := ir.IntArg("rule", 1)
	area := img.Rect.Inset(pad)
	lineH := area.Dy() / lines
	if area.Empty() || lineH < 1 {
		return
	}
	size := 0.6 * float64(lineH)
	baseline := func(i int) int { return area.Min.Y + i*lineH + lineH/2 + int(size*0.36) }

	all, hidden := p.agendaLines(lines, ir.BoolArg("headings", true), ir.BoolArg("location", false))
	if len(all) == 0 {
		render.Text(img, p.text("Keine Termine", "No events"), area.Min.X+area.Dx()/2, baseline(0),
			render.Style{Size: size, Shrink: true}, area.Dx(), color.Black, render.Center)
		return
	}
	if hidden > 0 {
		all = append(all, agendaLine{text: fmt.Sprintf(p.text("+%d weitere", "+%d more"), hidden)})
	}
	gap := int(size * 0.6)
	whenW := 0
	for _, l := range all {
		if !l.heading && l.when != "" {
			whenW = max(whenW, render.Measure(l.when, size, false))
		}
	}
	whenW = min(whenW, area.Dx()/2)
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
		render.TextAt(img, render.Ellipsize(l.text, size, false, area.Max.X-textX), textX, y, size, false, color.Black, render.Left)
	}
}

func (p *calendarPlugin) agendaLines(maxLines int, headings, location bool) ([]agendaLine, int) {
	now := p.now.In(p.loc)
	today := midnight(now)
	type dated struct {
		parsedEvent
		day time.Time
	}
	var upcoming []dated
	horizon := today.AddDate(0, 0, p.opts.days)
	for _, e := range p.parsed() {
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
		upcoming = append(upcoming, dated{e, day})
	}
	sort.SliceStable(upcoming, func(i, j int) bool {
		a, b := upcoming[i], upcoming[j]
		if !a.day.Equal(b.day) {
			return a.day.Before(b.day)
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
		line := agendaLine{when: p.when(d.parsedEvent, d.day, today, headings), text: strings.TrimSpace(d.title)}
		if location && strings.TrimSpace(d.location) != "" {
			line.text += " · " + strings.TrimSpace(d.location)
		}
		eventIdx = append(eventIdx, len(all))
		all = append(all, line)
	}
	if len(all) <= maxLines {
		return all, 0
	}
	keep := maxLines - 1
	for keep > 0 && all[keep-1].heading {
		keep--
	}
	shown := 0
	for _, idx := range eventIdx {
		if idx < keep {
			shown++
		}
	}
	return all[:keep], len(eventIdx) - shown
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
func (p *calendarPlugin) month(ir plugin.ImageRequest, img *image.Paletted) {
	pad := max(ir.IntArg("padding", 6), 0)
	area := img.Rect.Inset(pad)
	if area.Dx() < 70 || area.Dy() < 60 {
		return
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

	byDay := p.eventsByDay(gridStart, weeks*7)
	numSize := min(float64(cellH)*0.32, float64(cellW)*0.42)
	lineSize := max(float64(cellH)*0.22, 11)
	for i := range weeks * 7 {
		day := gridStart.AddDate(0, 0, i)
		c, r := i%7, i/7
		cell := image.Rect(x0+c*cellW, gridTop+r*cellH, x0+(c+1)*cellW, gridTop+(r+1)*cellH)
		if c >= 5 && levels == 4 {
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
		} else if !inMonth && levels == 4 {
			ink = render.DarkGray
		}
		if inMonth || levels == 4 || day.Equal(today) {
			render.TextAt(img, num, numX, numBase, numSize, day.Equal(today) || inMonth, ink, render.Left)
		}
		evs := byDay[i]
		if len(evs) == 0 || !inMonth {
			continue
		}
		top := numBase + 3
		room := (cell.Max.Y - 2 - top) / int(lineSize*1.15)
		if ir.BoolArg("titles", true) && room >= 1 && cellW >= 50 {
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
}

// eventsByDay lists the event titles of every day of the grid, all-day
// events first.
func (p *calendarPlugin) eventsByDay(start time.Time, days int) [][]string {
	out := make([][]string, days)
	evs := p.parsed()
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
