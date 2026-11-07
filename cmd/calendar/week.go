package main

import (
	"fmt"
	"image"
	"image/color"
	"sort"
	"strings"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

const maxWeekDays = 14

// weekStart returns the first day of the week that contains t: Monday for
// German, Sunday for every other language.
func weekStart(t time.Time, lang string) time.Time {
	day := midnight(t)
	first := time.Sunday
	if lang == "de" {
		first = time.Monday
	}
	back := (int(day.Weekday()) - int(first) + 7) % 7
	return day.AddDate(0, 0, -back)
}

// weekFirstDay is the first column of the week widget: today or the start of
// the week.
func weekFirstDay(now time.Time, lang, start string) time.Time {
	if start == "today" {
		return midnight(now)
	}
	return weekStart(now, lang)
}

// dayIndex counts calendar days from start to t (both in the same zone),
// independent of daylight saving changes.
func dayIndex(start, t time.Time) int {
	a := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	b := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return int(b.Sub(a).Hours() / 24)
}

// weekBar is an event drawn as a bar over the columns first..last (already
// clipped to the visible days). Every all-day event and every timed event
// that covers more than one calendar day is a bar.
type weekBar struct {
	event       parsedEvent
	first, last int
}

// assignDays distributes events over n days that start at start (midnight in
// loc): bars for all-day and multi-day events, timed single-day events per
// column. End times are exclusive, so an event that ends at midnight does not
// reach into the next day.
func assignDays(events []parsedEvent, start time.Time, n int, loc *time.Location) ([]weekBar, [][]parsedEvent) {
	var bars []weekBar
	timed := make([][]parsedEvent, n)
	for _, e := range events {
		s := e.start.In(loc)
		end := e.end.In(loc)
		if !end.After(s) {
			end = s
		} else {
			end = end.Add(-time.Nanosecond)
		}
		first, last := dayIndex(start, s), dayIndex(start, end)
		if last < 0 || first >= n {
			continue
		}
		clipped := weekBar{event: e, first: max(first, 0), last: min(last, n-1)}
		if e.allDay || last > first {
			bars = append(bars, clipped)
		} else {
			timed[first] = append(timed[first], e)
		}
	}
	sort.SliceStable(bars, func(i, j int) bool {
		a, b := bars[i], bars[j]
		if a.first != b.first {
			return a.first < b.first
		}
		if a.last != b.last {
			return a.last > b.last
		}
		return a.event.start.Before(b.event.start)
	})
	for _, list := range timed {
		sort.SliceStable(list, func(i, j int) bool { return list[i].start.Before(list[j].start) })
	}
	return bars, timed
}

// packBars puts the bars into rows so that bars in one row do not overlap.
func packBars(bars []weekBar) [][]weekBar {
	var rows [][]weekBar
	for _, b := range bars {
		placed := false
		for i, row := range rows {
			if row[len(row)-1].last < b.first {
				rows[i] = append(row, b)
				placed = true
				break
			}
		}
		if !placed {
			rows = append(rows, []weekBar{b})
		}
	}
	return rows
}

// weekDays reads the "days" argument: 1 to 14, default 7.
func weekDays(a plugin.Values, def int) int {
	return min(max(a.Int("days", def), 1), maxWeekDays)
}

func (p *calendarPlugin) weekFirst(a plugin.Values, def string) time.Time {
	return weekFirstDay(p.now.In(p.loc), p.lang, a.String("start", def))
}

// shortDate is the date line of a column heading.
func (p *calendarPlugin) shortDate(day time.Time) string {
	if p.lang == "de" {
		return fmt.Sprintf("%d.%d.", day.Day(), int(day.Month()))
	}
	return fmt.Sprintf("%s %d", day.Month().String()[:3], day.Day())
}

// week draws one column per day: weekday and date as heading (today
// inverted), bars for all-day and multi-day events, then the timed events
// with start time and ellipsized title and "+N weitere" for the rest.
func (p *calendarPlugin) week(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) {
	area := img.Rect.Inset(w.Style.Padding)
	n := weekDays(w.Arg(), 7)
	if area.Dx() < n*24 || area.Dy() < 60 {
		return
	}
	size := max(req.TextPx(w)*0.6, req.PtPx(6.3), render.MinSize)
	lineH := int(size*1.25 + 0.5)
	colW := area.Dx() / n
	x0 := area.Min.X + (area.Dx()-colW*n)/2
	pad := max(int(req.PtPx(1.5)), 2)
	today := midnight(p.now.In(p.loc))
	first := p.weekFirst(w.Arg(), "week")

	bars, timed := assignDays(p.parsed(), first, n, p.loc)

	// Heading: weekday and date, today inverted.
	headH := lineH*2 + pad
	for i := range n {
		day := first.AddDate(0, 0, i)
		cell := image.Rect(x0+i*colW, area.Min.Y, x0+(i+1)*colW, area.Min.Y+headH)
		ink := color.Color(color.Black)
		if day.Equal(today) {
			render.FillSolid(img, cell.Inset(1))
			ink = color.White
		}
		cx := cell.Min.X + colW/2
		render.Text(img, weekday(day, p.lang), cx, cell.Min.Y+lineH-int(size*0.22), render.Style{Size: size, Bold: true, Shrink: true}, colW-2, ink, render.Center)
		render.Text(img, p.shortDate(day), cx, cell.Min.Y+2*lineH-int(size*0.22), render.Style{Size: size, Shrink: true}, colW-2, ink, render.Center)
	}
	top := area.Min.Y + headH
	render.HLine(img, x0, top, x0+colW*n, 1)
	for i := 1; i < n; i++ {
		render.VLine(img, x0+i*colW, top, area.Max.Y, 1)
	}
	top += pad

	// Bars on top, at most a third of the body; the rest counts as hidden.
	hidden := make([]int, n)
	rows := packBars(bars)
	maxRows := max((area.Max.Y-top)/3/(lineH+1), 1)
	for r, row := range rows {
		if r >= maxRows {
			for _, b := range row {
				hidden[b.first]++
			}
			continue
		}
		y := top + r*(lineH+1)
		for _, b := range row {
			box := image.Rect(x0+b.first*colW+1, y, x0+(b.last+1)*colW-1, y+lineH)
			render.FillSolid(img, box)
			title := render.Ellipsize(strings.TrimSpace(b.event.title), size, false, box.Dx()-2*pad)
			render.TextAt(img, title, box.Min.X+pad, box.Min.Y+lineH-int(size*0.25), size, false, color.White, render.Left)
		}
	}
	if len(rows) > 0 {
		top += min(len(rows), maxRows)*(lineH+1) + pad
	}
	bottom := area.Max.Y

	// Timed events: a start time and the title, on two lines in narrow columns.
	for i := range n {
		colX := x0 + i*colW
		textW := colW - 2*pad
		oneLine := textW >= render.Measure("00:00 Wwwwwwww", size, false)
		avail := (bottom - top) / lineH
		used := 0
		shown := 0
		for _, e := range timed[i] {
			cost := 2
			if oneLine {
				cost = 1
			}
			// Keep a line for "+N weitere" when events remain.
			left := len(timed[i]) - shown - 1
			if used+cost > avail || (left+hidden[i] > 0 && used+cost+1 > avail) {
				break
			}
			base := top + (used+1)*lineH - int(size*0.25)
			clock := e.start.In(p.loc).Format("15:04")
			title := strings.TrimSpace(e.title)
			if oneLine {
				cw := render.Measure(clock+" ", size, true)
				render.TextAt(img, clock, colX+pad, base, size, true, color.Black, render.Left)
				render.TextAt(img, render.Ellipsize(title, size, false, textW-cw), colX+pad+cw, base, size, false, color.Black, render.Left)
			} else {
				render.TextAt(img, clock, colX+pad, base, size, true, color.Black, render.Left)
				render.TextAt(img, render.Ellipsize(title, size, false, textW), colX+pad, base+lineH, size, false, color.Black, render.Left)
			}
			used += cost
			shown++
		}
		if more := len(timed[i]) - shown + hidden[i]; more > 0 {
			base := top + (used+1)*lineH - int(size*0.25)
			render.Text(img, fmt.Sprintf(p.text("+%d weitere", "+%d more"), more), colX+pad, base, render.Style{Size: size, Bold: true, Shrink: true}, textW, color.Black, render.Left)
		}
	}
}

// weekLines is the text variant: one line per day, "Mo  7 title, 09:30 title".
// With fewer lines than days only the first days are shown.
func (p *calendarPlugin) weekLines(w plugin.WidgetRequest) []string {
	n := min(weekDays(w.Arg(), 7), max(w.Height, 1))
	first := p.weekFirst(w.Arg(), "today") // few lines: start with today
	bars, timed := assignDays(p.parsed(), first, n, p.loc)
	today := midnight(p.now.In(p.loc))
	var out []string
	for i := range n {
		day := first.AddDate(0, 0, i)
		mark := " "
		if day.Equal(today) {
			mark = ">"
		}
		line := fmt.Sprintf("%s%s %2d", mark, weekday(day, p.lang), day.Day())
		var items []string
		for _, b := range bars {
			if b.first <= i && i <= b.last {
				items = append(items, strings.TrimSpace(b.event.title))
			}
		}
		for _, e := range timed[i] {
			items = append(items, e.start.In(p.loc).Format("15:04")+" "+strings.TrimSpace(e.title))
		}
		if len(items) > 0 {
			line += " " + strings.Join(items, ", ")
		}
		out = append(out, line)
	}
	return out
}
