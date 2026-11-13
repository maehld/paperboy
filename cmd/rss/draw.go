package main

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

// Render draws the feed list or the headline.
func (p *rss) Render(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	switch w.Widget {
	case "feed":
		return p.renderFeed(req, w, img)
	case "headline":
		return p.renderHeadline(req, w, img)
	}
	return fmt.Errorf("%w %q", plugin.ErrUnknownWidget, w.Widget)
}

func (p *rss) renderFeed(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	name := w.Arg().String("feed", "all")
	list, err := p.snap.entries(name)
	if err != nil {
		return err
	}
	mode, err := timeMode(w)
	if err != nil {
		return err
	}
	inner := img.Rect.Inset(w.Style.Padding)
	if inner.Empty() {
		return plugin.TooSmall(w)
	}
	base := max(req.TextPx(w), render.MinSize)
	rule := max(w.Style.Rule, 1)
	muted := mutedColor(img)
	now := p.now(req)
	source := p.sourcesOn(w, name)

	if w.Arg().Bool("header", false) {
		title := name
		if name == "" || name == "all" {
			title = req.T("Alle Quellen", "All feeds")
		}
		size := max(base*0.55, render.MinSize)
		render.Text(img, title, inner.Min.X, inner.Min.Y+int(size), render.Style{Size: size, Bold: true, Shrink: true}, inner.Dx(), render.Black, render.Left)
		inner.Min.Y += int(size * 1.6)
		render.HLine(img, inner.Min.X, inner.Min.Y, inner.Max.X, rule)
		inner.Min.Y += rule + max(int(base*0.2), 2)
	}
	if inner.Empty() {
		return plugin.TooSmall(w)
	}

	metaSize := max(base*0.45, 9)
	metaStep := int(metaSize * 1.45)
	sumSize := max(base*0.48, 10)
	sumStep := int(sumSize * 1.45)
	summaries := w.Arg().Bool("summary", false)
	metaOn := mode != "none" || source

	row := int(base * 0.95)
	if metaOn {
		row += metaStep
	}
	if summaries {
		row += sumStep
	}
	rows := inner.Dy() / row
	if limit := w.Arg().Int("items", 0); limit > 0 {
		rows = min(rows, limit)
	}
	rows = min(rows, len(list))
	if rows < 1 {
		if len(list) == 0 {
			render.Text(img, req.T("Keine Meldungen", "No entries"), inner.Min.X, inner.Min.Y+int(base), render.Style{Size: base, Shrink: true}, inner.Dx(), render.Black, render.Left)
			return nil
		}
		rows = 1
	}

	// All titles share one size: the largest that fits every visible title,
	// down to 0.7 of the base text; longer ones get an ellipsis.
	titleSize := base
	for titleSize > base*0.7 && !titlesFit(list[:rows], titleSize, inner.Dx()) {
		titleSize--
	}
	titleH := int(titleSize * 0.95)
	row = inner.Dy() / rows
	need := titleH + boolInt(metaOn)*metaStep
	if summaries {
		need += sumStep
	}
	withSummary := summaries && row >= need

	y := inner.Min.Y
	for i := 0; i < rows; i++ {
		e := list[i]
		contentH := titleH + boolInt(metaOn)*metaStep
		if withSummary {
			contentH += sumStep
		}
		if i > 0 {
			render.HLine(img, inner.Min.X, y, inner.Max.X, rule)
		}
		baseline := y + max((row-contentH)/2, 0) + titleH
		title := render.Ellipsize(e.Title, titleSize, true, inner.Dx())
		render.Text(img, title, inner.Min.X, baseline, render.Style{Size: titleSize, Bold: true}, inner.Dx(), render.Black, render.Left)
		if metaOn {
			meta := p.entryMeta(e, now, mode, req, source)
			if meta != "" {
				baseline += metaStep
				render.Text(img, meta, inner.Min.X, baseline, render.Style{Size: metaSize}, inner.Dx(), muted, render.Left)
			}
		}
		if withSummary && e.Summary != "" {
			baseline += sumStep
			render.Text(img, render.Ellipsize(e.Summary, sumSize, false, inner.Dx()), inner.Min.X, baseline, render.Style{Size: sumSize}, inner.Dx(), muted, render.Left)
		}
		y += row
	}
	return nil
}

func (p *rss) renderHeadline(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	name := w.Arg().String("feed", "all")
	list, err := p.snap.entries(name)
	if err != nil {
		return err
	}
	mode, err := timeMode(w)
	if err != nil {
		return err
	}
	inner := img.Rect.Inset(w.Style.Padding)
	if inner.Empty() {
		return plugin.TooSmall(w)
	}
	if len(list) == 0 {
		base := max(req.TextPx(w), render.MinSize)
		render.Text(img, req.T("Keine Meldungen", "No entries"), inner.Min.X, inner.Min.Y+int(base), render.Style{Size: base, Shrink: true}, inner.Dx(), render.Black, render.Left)
		return nil
	}
	e := list[0]
	base := max(req.TextPx(w), render.MinSize)
	muted := mutedColor(img)

	meta := strings.ToUpper(e.Source)
	if t := p.timeText(e.Time, p.now(req), mode, req); t != "" {
		meta += "  ·  " + t
	}
	metaSize := max(base*0.45, 9)
	render.Text(img, render.Ellipsize(meta, metaSize, false, inner.Dx()), inner.Min.X, inner.Min.Y+int(metaSize), render.Style{Size: metaSize}, inner.Dx(), muted, render.Left)

	y := inner.Min.Y + int(metaSize*1.7)
	if y >= inner.Max.Y {
		return nil
	}
	room := inner.Dy() - int(metaSize*1.7)

	// The title grows with the cell, up to four lines; with a summary it
	// keeps one line of room free for it.
	capLines := 4
	if w.Arg().Bool("summary", true) && e.Summary != "" {
		capLines = 3
	}
	size := min(base*2, float64(room)*0.32)
	size = max(size, base)
	var lines []string
	for {
		maxLines := min(max(room/(int(size*1.05)+1), 1), capLines)
		lines = wrapLines(e.Title, size, true, inner.Dx(), maxLines)
		if len(lines) <= maxLines || size <= base {
			break
		}
		size -= 2
	}
	for _, line := range lines {
		next := y + int(size*1.02)
		if next > inner.Max.Y {
			break
		}
		y = next
		render.Text(img, line, inner.Min.X, y, render.Style{Size: size, Bold: true}, inner.Dx(), render.Black, render.Left)
	}

	if w.Arg().Bool("summary", true) && e.Summary != "" {
		ssize := max(base*0.5, 10)
		step := int(ssize * 1.35)
		y += int(base * 0.35)
		room := inner.Max.Y - y - int(base*0.3)
		if room >= step {
			for _, line := range wrapLines(e.Summary, ssize, false, inner.Dx(), min(room/step, 3)) {
				if y+step > inner.Max.Y-int(ssize*0.3) {
					break
				}
				y += step
				render.Text(img, line, inner.Min.X, y, render.Style{Size: ssize}, inner.Dx(), muted, render.Left)
			}
		}
	}
	return nil
}

// entryMeta joins the age and, when the list mixes feeds, the feed's name.
func (p *rss) entryMeta(e entry, now time.Time, mode string, req *plugin.Request, source bool) string {
	var parts []string
	if t := p.timeText(e.Time, now, mode, req); t != "" {
		parts = append(parts, t)
	}
	if source && e.Source != "" {
		parts = append(parts, e.Source)
	}
	return strings.Join(parts, "  ·  ")
}

// timeText renders the age of an entry the way the widget's time arg asks.
func (p *rss) timeText(t, now time.Time, mode string, req *plugin.Request) string {
	if mode == "none" || t.IsZero() {
		return ""
	}
	if mode == "clock" {
		return t.In(req.Location()).Format("15:04")
	}
	return relativeTime(t, now, req)
}

// relativeTime says how old an entry is: minutes, hours, "yesterday", a
// weekday within the last week, otherwise the date.
func relativeTime(t, now time.Time, req *plugin.Request) string {
	if t.IsZero() {
		return ""
	}
	lang := req.Lang()
	loc := req.Location()
	now = now.In(loc)
	d := now.Sub(t.In(loc))
	if d < 0 {
		d = 0
	}
	days := calendarDays(t.In(loc), now)
	switch {
	case d < time.Minute:
		return req.T("gerade eben", "just now")
	case d < time.Hour:
		return fmt.Sprintf(req.T("vor %d Min", "%d min ago"), int(d.Minutes()))
	case days == 0:
		return fmt.Sprintf(req.T("vor %d Std", "%d h ago"), int(d.Hours()))
	case days == 1:
		return req.T("gestern", "yesterday")
	case days < 7:
		return weekday(t.In(loc).Weekday(), lang)
	}
	return dateText(t.In(loc), now, lang)
}

// compactAge is the short form for character displays: 12m, 5h, 2d, then a
// date.
func compactAge(t, now time.Time, req *plugin.Request) string {
	if t.IsZero() {
		return ""
	}
	loc := req.Location()
	now = now.In(loc)
	d := now.Sub(t.In(loc))
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "0m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	days := calendarDays(t.In(loc), now)
	if days < 7 {
		return fmt.Sprintf("%dd", days)
	}
	return dateText(t.In(loc), now, req.Lang())
}

func calendarDays(t, now time.Time) int {
	a := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	b := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return int(b.Sub(a).Hours() / 24)
}

func weekday(d time.Weekday, lang string) string {
	names := weekdays[lang]
	if names == nil {
		names = weekdays["en"]
	}
	return names[int(d)]
}

var weekdays = map[string][]string{
	"de": {"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"},
	"en": {"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
}

func dateText(t, now time.Time, lang string) string {
	if lang == "de" {
		if t.Year() == now.Year() {
			return t.Format("02.01.")
		}
		return t.Format("02.01.06")
	}
	if t.Year() == now.Year() {
		return t.Format("Jan 2")
	}
	return t.Format("Jan 2, 2006")
}

// titlesFit reports whether every title fits the width at this size.
func titlesFit(list []entry, size float64, width int) bool {
	for _, e := range list {
		if render.Measure(e.Title, size, true) > width {
			return false
		}
	}
	return true
}

// timeMode validates the widget's time arg.
func timeMode(w plugin.WidgetRequest) (string, error) {
	mode := w.Arg().String("time", "relative")
	for _, m := range timeModes {
		if mode == m {
			return mode, nil
		}
	}
	return "", fmt.Errorf("time: %q is not relative, clock or none", mode)
}

// mutedColor keeps secondary text readable on 2-level panels, where a light
// gray would turn white.
func mutedColor(img *image.Paletted) color.Color {
	if render.Levels(img.Palette) >= 4 {
		return render.DarkGray
	}
	return render.Black
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// wrapLines breaks s at spaces into at most maxLines lines of width pixels.
func wrapLines(s string, size float64, bold bool, width, maxLines int) []string {
	if maxLines < 1 {
		return nil
	}
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		try := word
		if line != "" {
			try = line + " " + word
		}
		if render.Measure(try, size, bold) <= width {
			line = try
			continue
		}
		if line != "" {
			lines = append(lines, line)
		}
		line = word
	}
	if line != "" {
		lines = append(lines, line)
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] = render.Ellipsize(lines[maxLines-1]+"…", size, bold, width)
	}
	return lines
}
