package page

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

// Display describes the panel a page is composed for. For a text display
// Width and Height are columns and rows.
type Display struct {
	Width    int
	Height   int
	Gray     bool    // four gray levels instead of black and white
	Text     bool    // character display
	Charset  string  // text displays only
	PPI      float64 // pixels per inch of an image display (ADR 0050)
	Language string  // "de" or "en"; empty means "de"
	Timezone *time.Location
}

// Default page style in points (ADR 0050).
const (
	DefaultTextPt    = 16.0
	DefaultPaddingPt = 4.5
	DefaultRulePt    = 0.6
	// maxTextShare caps the base text on physically tiny displays.
	maxTextShare = 0.12
)

// px converts points to pixels for the display.
func (d Display) px(pt float64) float64 { return pt * d.PPI / 72 }

// pxAtLeast1 converts a positive width in points to whole pixels, at least
// one; zero stays zero.
func (d Display) pxAtLeast1(pt float64) int {
	if pt <= 0 {
		return 0
	}
	return max(int(math.Round(d.px(pt))), 1)
}

func (d Display) language() string {
	if d.Language == "en" {
		return "en"
	}
	return "de"
}

func (d Display) levels() int {
	if d.Gray {
		return 4
	}
	return 2
}

// Context carries the position in the plan, extra banners such as the
// battery warning and the source of the widgets.
type Context struct {
	Now     time.Time
	Page    int // 1-based page index
	Pages   int // number of pages in the plan rule
	Banners []string
	Widgets WidgetSource
}

// Batch asks for all widgets of one page.
type Batch struct {
	Text    bool // text display: lines instead of images
	Levels  int  // gray levels of an image display
	Charset string
	Page    plugin.PageInfo
	Widgets []WidgetRequest
}

// WidgetRequest is one cell of the page.
type WidgetRequest struct {
	Plugin string
	Widget string
	Args   map[string]any
	Width  int // pixels or columns
	Height int // pixels or rows
	Style  plugin.Style
}

// Result is the answer for one WidgetRequest.
type Result struct {
	Image image.Image // image displays
	Lines []string    // text displays
	Err   error
	Stale bool // an older result, because the plugin failed
}

// WidgetSource fetches the widgets of a page; the result has one entry per
// request in the same order. It runs before the page is drawn and outside the
// render lock, because plugins are external processes.
type WidgetSource func(b Batch) []Result

// renderMu serializes drawing: the cached font faces are not safe for
// concurrent use.
var renderMu sync.Mutex

func text(language, de, en string) string {
	if language == "en" {
		return en
	}
	return de
}

// cellRects divides the display into the pixel rectangles of the cells.
func (t *Template) cellRects(w, h int) []image.Rectangle {
	cols, rows := t.Grid[0], t.Grid[1]
	out := make([]image.Rectangle, len(t.Cells))
	for i, c := range t.Cells {
		out[i] = image.Rect(w*c.At[0]/cols, h*c.At[1]/rows,
			w*(c.At[0]+c.Span[0])/cols, h*(c.At[1]+c.Span[1])/rows)
	}
	return out
}

func (t *Template) pageInfo(d Display, c Context) plugin.PageInfo {
	p := plugin.PageInfo{Title: t.Title, Number: max(c.Page, 1), Count: max(c.Pages, 1), Width: d.Width, Height: d.Height}
	if !d.Text {
		p.PPI = d.PPI
	}
	return p
}

// pluginStyle resolves the style a widget gets: points converted to pixels
// with the display's PPI (ADR 0050).
func (t *Template) pluginStyle(c Cell, d Display) plugin.Style {
	s := t.Style.merged(c.Style)
	pad := DefaultPaddingPt
	if s.Padding != nil {
		pad = *s.Padding
	}
	rule := s.Rule
	if rule == 0 {
		rule = DefaultRulePt
	}
	size := s.TextSize
	if size == 0 {
		size = DefaultTextPt
	}
	return plugin.Style{
		Padding: int(math.Round(d.px(pad))),
		Rule:    d.pxAtLeast1(rule),
		TextPx:  min(d.px(size), maxTextShare*float64(d.Height)),
	}
}

// RenderImage composes the page for an image display. A widget that fails
// becomes a notice inside its cell; stale widgets add a notice bar.
func (t *Template) RenderImage(d Display, c Context) (*image.Paletted, error) {
	if err := t.Fits(d); err != nil {
		return nil, err
	}
	rects := t.cellRects(d.Width, d.Height)
	batch := Batch{Levels: d.levels(), Page: t.pageInfo(d, c)}
	for i, cell := range t.Cells {
		batch.Widgets = append(batch.Widgets, WidgetRequest{Plugin: cell.Plugin(), Widget: cell.Name(), Args: cell.Args,
			Width: rects[i].Dx(), Height: rects[i].Dy(), Style: t.pluginStyle(cell, d)})
	}
	results := fetch(c.Widgets, batch)

	renderMu.Lock()
	defer renderMu.Unlock()
	language := d.language()
	img := render.NewCanvas(d.Width, d.Height, d.levels())
	var stale []string
	for i, cell := range t.Cells {
		r, area := results[i], rects[i]
		switch {
		case r.Image == nil:
			msg := text(language, "kein Ergebnis", "no result")
			if r.Err != nil {
				msg = r.Err.Error()
			}
			cellNotice(img, area, cell.Widget, msg)
			continue
		case r.Image.Bounds().Dx() != area.Dx() || r.Image.Bounds().Dy() != area.Dy():
			b := r.Image.Bounds()
			cellNotice(img, area, cell.Widget, fmt.Sprintf(text(language, "falsche Größe %dx%d statt %dx%d",
				"wrong size %dx%d instead of %dx%d"), b.Dx(), b.Dy(), area.Dx(), area.Dy()))
			continue
		}
		draw.Draw(img, area, render.Quantize(r.Image, d.levels()), image.Point{}, draw.Src)
		if r.Stale {
			stale = append(stale, cell.Widget)
		}
	}
	t.dividers(img, rects, d)
	var banners []string
	if len(stale) > 0 && t.Notices == "show" {
		banners = append(banners, text(language, "Veraltet: ", "Outdated: ")+strings.Join(unique(stale), ", "))
	}
	banners = append(banners, c.Banners...)
	if len(banners) > 0 {
		img = WithBanner(img, banners...)
	}
	return img, nil
}

func fetch(src WidgetSource, b Batch) []Result {
	var results []Result
	if src != nil {
		results = src(b)
	}
	if len(results) != len(b.Widgets) {
		out := make([]Result, len(b.Widgets))
		copy(out, results)
		for i := len(results); i < len(out); i++ {
			out[i] = Result{Err: fmt.Errorf("plugin %q is not available", b.Widgets[i].Plugin)}
		}
		results = out
	}
	return results
}

// dividers draws the configured lines along the inner edges of the cells.
func (t *Template) dividers(img *image.Paletted, rects []image.Rectangle, d Display) {
	w := d.pxAtLeast1(t.Style.Divider)
	if w <= 0 {
		return
	}
	for _, r := range rects {
		if r.Max.X < img.Rect.Max.X {
			render.VLine(img, r.Max.X-w/2-w%2, r.Min.Y, r.Max.Y, w)
		}
		if r.Max.Y < img.Rect.Max.Y {
			render.HLine(img, r.Min.X, r.Max.Y-w/2-w%2, r.Max.X, w)
		}
	}
}

func unique(list []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range list {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// cellNotice marks a cell whose widget is missing: a hatched frame with the
// widget name and the reason.
func cellNotice(img *image.Paletted, area image.Rectangle, label, msg string) {
	if area.Dx() < 4 || area.Dy() < 4 {
		return
	}
	render.Fill(img, area, "hatch")
	inner := area.Inset(max(min(area.Dx(), area.Dy())/10, 2))
	render.FillColor(img, inner, color.White)
	size := float64(max(min(inner.Dy()/4, 22), render.MinSize))
	cx := inner.Min.X + inner.Dx()/2
	cy := inner.Min.Y + inner.Dy()/2
	render.Text(img, label, cx, cy-int(size*0.2), render.Style{Size: size, Bold: true, Shrink: true}, inner.Dx()-8, color.Black, render.Center)
	small := size * 0.75
	render.TextAt(img, render.Ellipsize(firstLine(msg), small, false, inner.Dx()-8), cx, cy+int(size*0.95), small, false, color.Black, render.Center)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// WithBanner returns a copy of img with inverted notice bars stacked at the
// bottom. A bar too low for legible text stays a plain black bar: white text
// at the minimum font size would cover it and turn it gray.
func WithBanner(img *image.Paletted, texts ...string) *image.Paletted {
	out := image.NewPaletted(img.Rect, img.Palette)
	copy(out.Pix, img.Pix)
	w, h := img.Rect.Dx(), img.Rect.Dy()
	bar := max(h/8, 1)
	for i, s := range texts {
		bottom := h - i*bar
		if bottom-bar < 0 {
			break
		}
		render.FillSolid(out, image.Rect(0, bottom-bar, w, bottom))
		if i > 0 {
			render.FillColor(out, image.Rect(0, bottom-max(1, bar/30), w, bottom), color.White)
		}
		if float64(bar)*0.55 < render.MinSize {
			continue
		}
		render.Text(out, s, w/2, bottom-bar*3/10, render.Style{Size: float64(bar) * 0.55, Bold: true, Shrink: true},
			w-w/25, color.White, render.Center)
	}
	return out
}

// RenderText composes the page for a text display. The grid of a text page
// is the character grid of the display; every widget fills its cell with
// lines, cut and padded by Paperboy. A stale widget marks the last column of
// the first line with "!".
func (t *Template) RenderText(d Display, c Context) ([]string, error) {
	if err := t.Fits(d); err != nil {
		return nil, err
	}
	batch := Batch{Text: true, Charset: d.Charset, Page: t.pageInfo(d, c)}
	for _, cell := range t.Cells {
		batch.Widgets = append(batch.Widgets, WidgetRequest{Plugin: cell.Plugin(), Widget: cell.Name(), Args: cell.Args,
			Width: cell.Span[0], Height: cell.Span[1]})
	}
	results := fetch(c.Widgets, batch)
	grid := make([][]rune, d.Height)
	for y := range grid {
		grid[y] = []rune(strings.Repeat(" ", d.Width))
	}
	stale := false
	for i, cell := range t.Cells {
		r := results[i]
		lines := r.Lines
		if r.Err != nil && lines == nil {
			lines = wrap("? "+r.Err.Error(), cell.Span[0])
		}
		stale = stale || r.Stale
		for row := 0; row < cell.Span[1] && row < len(lines); row++ {
			line := []rune(fit(Convert(d.Charset, lines[row]), cell.Span[0]))
			copy(grid[cell.At[1]+row][cell.At[0]:], line)
		}
	}
	out := make([]string, d.Height)
	for y := range grid {
		out[y] = string(grid[y])
	}
	if stale && t.Notices == "show" && d.Width > 0 {
		line := []rune(out[0])
		line[len(line)-1] = '!'
		out[0] = string(line)
	}
	return out, nil
}

// DefaultStyle is the style a widget gets on a page of display d without a
// style section.
func DefaultStyle(d Display) plugin.Style {
	return (&Template{}).pluginStyle(Cell{}, d)
}

// NoticeImage is the hatched notice Paperboy shows for a widget without a
// result, as a standalone image of the widget's size.
func NoticeImage(width, height, levels int, label, msg string) *image.Paletted {
	renderMu.Lock()
	defer renderMu.Unlock()
	img := render.NewCanvas(width, height, levels)
	cellNotice(img, img.Rect, label, msg)
	return img
}

// WidgetImage maps a widget result onto the panel's palette like a page
// does; a missing or wrongly sized result becomes a notice.
func WidgetImage(r Result, width, height, levels int, label string) *image.Paletted {
	switch {
	case r.Image == nil:
		msg := "no result"
		if r.Err != nil {
			msg = r.Err.Error()
		}
		return NoticeImage(width, height, levels, label, msg)
	case r.Image.Bounds().Dx() != width || r.Image.Bounds().Dy() != height:
		b := r.Image.Bounds()
		return NoticeImage(width, height, levels, label, fmt.Sprintf("wrong size %dx%d instead of %dx%d", b.Dx(), b.Dy(), width, height))
	}
	return render.Quantize(r.Image, levels)
}

// TextCell fits widget lines to a cell of a text display like a page does:
// converted to the charset, cut and padded; an error becomes wrapped lines.
func TextCell(r Result, charset string, width, height int) []string {
	lines := r.Lines
	if r.Err != nil && lines == nil {
		lines = wrap("? "+r.Err.Error(), width)
	}
	out := make([]string, height)
	for i := range out {
		if i < len(lines) {
			out[i] = fit(Convert(charset, lines[i]), width)
		} else {
			out[i] = fit("", width)
		}
	}
	return out
}
