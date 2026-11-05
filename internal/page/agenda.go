package page

import (
	"fmt"
	"image"
	"image/color"
	"sort"
	"strings"
	"time"
)

// agendaEvent is one entry of the event list as the calendar provider writes
// it (ADR 0048).
type agendaEvent struct {
	start, end time.Time
	allDay     bool
	title      string
	location   string
}

// agendaLine is one line of the agenda: a day heading or an event.
type agendaLine struct {
	heading bool
	when    string
	text    string
}

// agenda draws upcoming events as lines: a heading per day ("Heute", "Morgen",
// "Mo, 9.11.") followed by time and title. Ended events are left out, an event
// that started on an earlier day shows up under today, and when the lines run
// out the last one says how many events are missing.
func (r *renderer) agenda(img *image.Paletted, rc image.Rectangle, b Block) {
	a := b.Agenda
	pad := r.padding(&b, a.Padding)
	area := rc.Inset(pad)
	if area.Dx() <= 0 || area.Dy() <= 0 {
		return
	}
	lineH := area.Dy() / a.Lines
	if lineH < 1 {
		return
	}
	st := style(a.Size, float64(lineH), 0.6, "", false, nil)
	baseline := func(i int) int { return area.Min.Y + i*lineH + lineH/2 + int(st.Size*0.36) }

	list, ok := lookupList(r.root, a.Events)
	if !ok {
		drawStyled(img, "—", area.Min.X+area.Dx()/2, baseline(0), st, area.Dx(), color.Black, alignCenter)
		return
	}
	events := r.agendaEvents(list)
	lines, hidden := r.agendaLines(events, a)
	if len(lines) == 0 {
		empty := r.expand(a.Empty)
		if empty == "" {
			empty = text(r.language, "Keine Termine", "No events")
		}
		drawStyled(img, empty, area.Min.X+area.Dx()/2, baseline(0), st, area.Dx(), color.Black, alignCenter)
		return
	}
	if hidden > 0 {
		lines = append(lines, agendaLine{text: fmt.Sprintf(text(r.language, "+%d weitere", "+%d more"), hidden)})
	}

	gap := int(st.Size * 0.6)
	whenW := 0
	for _, l := range lines {
		if !l.heading && l.when != "" {
			whenW = max(whenW, measure(l.when, st.Size, false))
		}
	}
	whenW = min(whenW, area.Dx()/2)
	textX := area.Min.X
	if whenW > 0 {
		textX += whenW + gap
	}
	for i, l := range lines {
		y := baseline(i)
		if l.heading {
			if i > 0 && a.Rule > 0 {
				top := area.Min.Y + i*lineH
				hline(img, area.Min.X, top, area.Max.X, a.Rule)
			}
			drawTextAt(img, ellipsize(l.text, st.Size, true, area.Dx()), area.Min.X, y, st.Size, true, color.Black, alignLeft)
			continue
		}
		if l.when != "" {
			drawStyled(img, l.when, area.Min.X, y, textStyle{Size: st.Size, Shrink: true}, whenW, color.Black, alignLeft)
		}
		x := textX
		if l.when == "" && whenW == 0 {
			x = area.Min.X
		}
		drawTextAt(img, ellipsize(l.text, st.Size, false, area.Max.X-x), x, y, st.Size, false, color.Black, alignLeft)
	}
}

// agendaEvents reads the event list; entries without a valid start are skipped.
func (r *renderer) agendaEvents(list []any) []agendaEvent {
	var out []agendaEvent
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		start, ok := parseStamp(m["start"])
		if !ok {
			continue
		}
		end, ok := parseStamp(m["end"])
		if !ok || end.Before(start) {
			end = start
		}
		e := agendaEvent{start: start, end: end}
		e.allDay, _ = m["all_day"].(bool)
		e.title, _ = m["title"].(string)
		e.location, _ = m["location"].(string)
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].start.Equal(out[j].start) {
			return out[i].start.Before(out[j].start)
		}
		return out[i].allDay && !out[j].allDay
	})
	return out
}

func parseStamp(v any) (time.Time, bool) {
	s, ok := v.(string)
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	return t, err == nil
}

// agendaLines lays out the events that have not ended yet into at most
// a.Lines lines and returns how many events did not fit. When events are left
// over, one line stays free for the "+N more" note.
func (r *renderer) agendaLines(events []agendaEvent, a *Agenda) ([]agendaLine, int) {
	loc := r.f.location()
	now := r.now.In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	headings := a.Headings == nil || *a.Headings

	type dated struct {
		agendaEvent
		day time.Time
	}
	var upcoming []dated
	for _, e := range events {
		if !e.end.After(now) && !(e.end.Equal(e.start) && !e.start.Before(now)) {
			continue
		}
		start := e.start.In(loc)
		day := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
		if day.Before(today) {
			day = today
		}
		upcoming = append(upcoming, dated{e, day})
	}
	// Per day, all-day events come first, then the rest by start time.
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
	var eventIdx []int // index into all for every event line
	var lastDay time.Time
	for _, d := range upcoming {
		e, day := d.agendaEvent, d.day
		if headings && !day.Equal(lastDay) {
			all = append(all, agendaLine{heading: true, text: r.dayLabel(day, today)})
			lastDay = day
		}
		line := agendaLine{when: r.agendaWhen(e, day, today, headings), text: strings.TrimSpace(e.title)}
		if a.Location && strings.TrimSpace(e.location) != "" {
			line.text += " · " + strings.TrimSpace(e.location)
		}
		eventIdx = append(eventIdx, len(all))
		all = append(all, line)
	}
	if len(all) <= a.Lines {
		return all, 0
	}
	keep := a.Lines - 1
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

// agendaWhen is the time column of an event line.
func (r *renderer) agendaWhen(e agendaEvent, day, today time.Time, headings bool) string {
	loc := r.f.location()
	allDay := text(r.language, "ganztägig", "all day")
	var when string
	switch {
	case e.allDay:
		when = allDay
	case e.start.In(loc).Before(day):
		end := e.end.In(loc)
		if end.Before(day.AddDate(0, 0, 1)) {
			when = text(r.language, "bis ", "until ") + end.Format("15:04")
		} else {
			when = allDay
		}
	default:
		when = e.start.In(loc).Format("15:04")
	}
	if !headings && !day.Equal(today) {
		when = r.shortDay(day) + " " + when
	}
	return when
}

func (r *renderer) dayLabel(day, today time.Time) string {
	switch {
	case day.Equal(today):
		return text(r.language, "Heute", "Today")
	case day.Equal(today.AddDate(0, 0, 1)):
		return text(r.language, "Morgen", "Tomorrow")
	}
	if r.language == "de" {
		return fmt.Sprintf("%s, %d.%d.", r.shortDay(day), day.Day(), int(day.Month()))
	}
	return fmt.Sprintf("%s, %s %d", r.shortDay(day), day.Month().String()[:3], day.Day())
}

var agendaWeekdays = map[string][7]string{
	"de": {"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"},
	"en": {"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
}

func (r *renderer) shortDay(day time.Time) string {
	names, ok := agendaWeekdays[r.language]
	if !ok {
		names = agendaWeekdays["en"]
	}
	return names[day.Weekday()]
}

// ellipsize shortens s with "…" until it fits maxWidth. Titles keep their size,
// so a long title does not make one line smaller than its neighbours.
func ellipsize(s string, size float64, bold bool, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if measure(s, size, bold) <= maxWidth {
		return s
	}
	runes := []rune(s)
	for n := len(runes) - 1; n > 0; n-- {
		cut := strings.TrimRight(string(runes[:n]), " ,;·-") + "…"
		if measure(cut, size, bold) <= maxWidth {
			return cut
		}
	}
	return "…"
}
