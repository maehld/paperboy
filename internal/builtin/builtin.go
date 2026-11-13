// Package builtin is Paperboy's own plugin "paperboy" (ADR 0049). It runs in
// the Paperboy process and offers the widgets that need no data source: a
// page header, free text, a clock and the Paperboy logo.
package builtin

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

// Name is the plugin name pages use, e.g. paperboy/header.
const Name = "paperboy"

// Plugin implements plugin.Handler and plugin.Texter.
type Plugin struct{}

var (
	_ plugin.Handler = Plugin{}
	_ plugin.Texter  = Plugin{}
)

func (Plugin) Describe() plugin.Description {
	both := []string{plugin.KindImage, plugin.KindText}
	return plugin.Description{
		Title: "Paperboy",
		Widgets: []plugin.WidgetInfo{
			{Widget: "header", Title: "Page header", Kinds: both, Sizes: []plugin.Span{{Columns: 6, Rows: 1}, {Columns: 4, Rows: 1}},
				Args: map[string]plugin.Arg{
					"title":          {Type: "string", Description: "title; default the page title"},
					"subtitle":       {Type: "string"},
					"clock":          {Type: "bool", Default: true, Description: "current time on the right"},
					"date":           {Type: "bool", Default: false, Description: "weekday and date on the right, long or short depending on the space"},
					"page_indicator": {Type: "bool", Default: true, Description: "page number when the plan rule has several pages"},
					"align":          {Type: "string", Default: "left", Description: "left, center or right", Enum: []any{"left", "center", "right"}},
				}},
			{Widget: "text", Title: "Text", Kinds: both, Sizes: []plugin.Span{{Columns: 2, Rows: 1}, {Columns: 6, Rows: 1}},
				Args: map[string]plugin.Arg{
					"text":  {Type: "string", Required: true, Description: "lines separated by \\n"},
					"align": {Type: "string", Default: "center", Enum: []any{"left", "center", "right"}},
					"size":  {Type: "float", Default: 0.5, Description: "text height as a fraction of the line height"},
					"bold":  {Type: "bool", Default: false},
				}},
			{Widget: "clock", Title: "Clock", Kinds: both, Sizes: []plugin.Span{{Columns: 2, Rows: 1}, {Columns: 3, Rows: 2}, {Columns: 6, Rows: 2}},
				Args: map[string]plugin.Arg{
					"format": {Type: "string", Default: "time", Description: "time, date or datetime", Enum: []any{"time", "date", "datetime"}},
				}},
			{Widget: "logo", Title: "Paperboy logo", Kinds: []string{plugin.KindImage}, Sizes: []plugin.Span{{Columns: 2, Rows: 1}, {Columns: 3, Rows: 2}, {Columns: 4, Rows: 3}, {Columns: 6, Rows: 3}},
				Args: map[string]plugin.Arg{
					"wordmark": {Type: "bool", Default: false, Description: "with the wordmark, as in the lockup"},
				}},
		},
	}
}

func (Plugin) Configure(plugin.Values, *plugin.Request) error { return nil }

func align(s string, def render.Align) render.Align {
	switch s {
	case "left":
		return render.Left
	case "center":
		return render.Center
	case "right":
		return render.Right
	}
	return def
}

func (p Plugin) Render(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	switch w.Widget {
	case "header":
		p.header(req, w, img)
	case "text":
		p.text(w, img)
	case "clock":
		p.clock(req, w, img)
	case "logo":
		p.logo(w, img)
	default:
		return fmt.Errorf("%w %q", plugin.ErrUnknownWidget, w.Widget)
	}
	return nil
}

func (Plugin) header(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) {
	a := w.Arg()
	pad := w.Style.Padding
	rule := max(w.Style.Rule, 1)
	b := img.Rect
	h := b.Dy()
	title := a.String("title", req.Page.Title)
	if a.Bool("page_indicator", true) && req.Page.Count > 1 {
		title += fmt.Sprintf(" – %d/%d", req.Page.Number, req.Page.Count)
	}
	base := req.TextPx(w)
	titleSize := min(float64(h)*0.5, base*1.4)
	statusSize := min(float64(h)*0.45, base*1.1)
	status := headerStatus(req, a, func(s string) bool {
		return render.Measure(s, statusSize, false) <= b.Dx()/2-2*pad
	})
	right := b.Max.X - pad
	if status != "" {
		right -= render.Measure(status, statusSize, false) + 2*pad
	}
	left := b.Min.X + pad
	al := align(a.String("align", "left"), render.Left)
	x := left
	switch al {
	case render.Center:
		x = left + (right-left)/2
	case render.Right:
		x = right
	}
	bottom := b.Max.Y - rule - pad
	baseline := bottom
	sub := a.String("subtitle", "")
	if sub != "" {
		baseline = b.Min.Y + pad + (bottom-b.Min.Y-pad)*45/100
	}
	render.Text(img, title, x, baseline, render.Style{Size: titleSize, Bold: true, Shrink: true}, right-left, color.Black, al)
	if sub != "" {
		render.Text(img, sub, x, bottom, render.Style{Size: min(float64(h)*0.22, base*0.7), Shrink: true}, right-left, color.Black, al)
	}
	if status != "" {
		render.Text(img, status, b.Max.X-pad, baseline, render.Style{Size: statusSize, Shrink: true}, b.Dx()/2, color.Black, render.Right)
	}
	render.HLine(img, b.Min.X, b.Max.Y-rule, b.Max.X, rule)
}

func (Plugin) text(w plugin.WidgetRequest, img *image.Paletted) {
	a := w.Arg()
	lines := strings.Split(a.String("text", ""), "\n")
	pad := w.Style.Padding
	b := img.Rect.Inset(pad)
	if b.Empty() {
		return
	}
	lineH := b.Dy() / len(lines)
	al := align(a.String("align", "center"), render.Center)
	x := b.Min.X + b.Dx()/2
	switch al {
	case render.Left:
		x = b.Min.X
	case render.Right:
		x = b.Max.X
	}
	for i, l := range lines {
		base := b.Min.Y + i*lineH + lineH*3/5
		render.Text(img, l, x, base, render.Style{Size: float64(lineH) * a.Float("size", 0.5), Bold: a.Bool("bold", false), Shrink: true},
			b.Dx(), color.Black, al)
	}
}

var (
	daysDE   = [7]string{"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"}
	monthsDE = [12]string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"}
)

// dateForms returns the date of now from the longest to the shortest form.
func dateForms(req *plugin.Request) []string {
	now := req.Now.In(req.Location())
	if req.Lang() == "en" {
		return []string{
			now.Format("Monday, January 2, 2006"),
			now.Format("Mon, Jan 2, 2006"),
			now.Format("Mon, Jan 2"),
			now.Format("Jan 2"),
		}
	}
	day := daysDE[now.Weekday()]
	return []string{
		fmt.Sprintf("%s, %d. %s %d", day, now.Day(), monthsDE[now.Month()-1], now.Year()),
		fmt.Sprintf("%s, %d.%d.%d", day[:2], now.Day(), int(now.Month()), now.Year()),
		fmt.Sprintf("%s, %d.%d.", day[:2], now.Day(), int(now.Month())),
		fmt.Sprintf("%d.%d.", now.Day(), int(now.Month())),
	}
}

// headerStatus builds the right side of the header: the date (arg date,
// in the longest form that fits) followed by the time (arg clock). When even
// the shortest date does not fit, the date is left out.
func headerStatus(req *plugin.Request, a plugin.Values, fits func(string) bool) string {
	clock := ""
	if a.Bool("clock", true) {
		clock = req.Now.In(req.Location()).Format("15:04")
	}
	if !a.Bool("date", false) {
		return clock
	}
	for _, d := range dateForms(req) {
		s := d
		if clock != "" {
			s += "  " + clock
		}
		if fits(s) {
			return s
		}
	}
	return clock
}

func clockText(req *plugin.Request, format string) string {
	now := req.Now.In(req.Location())
	date := now.Format("2.1.2006")
	day := [7]string{"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"}[now.Weekday()]
	if req.Lang() == "en" {
		date = now.Format("Jan 2, 2006")
		day = now.Weekday().String()[:3]
	}
	switch format {
	case "date":
		return day + ", " + date
	case "datetime":
		return day + ", " + date + " " + now.Format("15:04")
	}
	return now.Format("15:04")
}

func (Plugin) clock(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) {
	s := clockText(req, w.Arg().String("format", "time"))
	b := img.Rect.Inset(w.Style.Padding)
	if b.Empty() {
		return
	}
	size := float64(b.Dy()) * 0.7
	render.Text(img, s, b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2+int(size*0.36), render.Style{Size: size, Bold: true, Shrink: true},
		b.Dx(), color.Black, render.Center)
}

// Text fills the widgets of a text display.
func (Plugin) Text(req *plugin.Request, w plugin.WidgetRequest) ([]string, error) {
	a := w.Arg()
	switch w.Widget {
	case "header":
		title := a.String("title", req.Page.Title)
		if a.Bool("page_indicator", true) && req.Page.Count > 1 {
			title += fmt.Sprintf(" %d/%d", req.Page.Number, req.Page.Count)
		}
		room := w.Width - len([]rune(title)) - 1
		status := headerStatus(req, a, func(s string) bool { return len([]rune(s)) <= room })
		if status == "" {
			return []string{title}, nil
		}
		return []string{plugin.Columns(title, status, w.Width)}, nil
	case "text":
		align := a.String("align", "left")
		var out []string
		for _, l := range strings.Split(a.String("text", ""), "\n") {
			// A scrolling cell gets the whole line: Paperboy cuts the first
			// window and the display scrolls the rest (ADR 0092).
			if w.Scroll && len([]rune(l)) > w.Width {
				out = append(out, l)
				continue
			}
			out = append(out, plugin.Align(l, w.Width, align))
		}
		return out, nil
	case "clock":
		return []string{plugin.Align(clockText(req, a.String("format", "time")), w.Width, "center")}, nil
	}
	return nil, fmt.Errorf("%w %q", plugin.ErrUnknownWidget, w.Widget)
}
