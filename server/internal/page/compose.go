package page

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
	"github.com/maehld/paperboy/server/internal/format"
)

// Display describes the panel a page is composed for. For a text display
// Width and Height are columns and rows.
type Display struct {
	Width    int
	Height   int
	Levels   int     // gray tones of an image display: 2, 4 or 16 (0 = 2)
	Text     bool    // character display
	Charset  string  // text displays only
	PPI      float64 // pixels per inch of an image display (ADR 0050)
	Language string  // "de" or "en"; empty means "de"
	Timezone *time.Location
	// Header is the display type's header band; nil means no band. The page
	// may hide it with header: false (ADR 0168).
	Header *Header
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
	if d.Levels > 2 {
		return d.Levels
	}
	return 2
}

// Context carries the position in the plan, extra banners such as the
// battery warning and the sources of the widgets and values.
type Context struct {
	Now     time.Time
	Page    int // 1-based page index
	Pages   int // number of pages in the plan rule
	Banners []string
	Widgets WidgetSource
	// Values is the source of the values of a text page (ADR 0111).
	Values ValueSource
	// Device is the display a page is composed for; nil for previews.
	Device *plugin.DeviceInfo
	// ServerStatus is Paperboy's own device status for the built-in plugin;
	// nil in previews that belong to no device (ADR 0115).
	ServerStatus *plugin.ServerStatus
}

// Batch asks for all widgets of one page.
type Batch struct {
	Levels       int // gray levels of an image display
	Charset      string
	Page         plugin.PageInfo
	Widgets      []WidgetRequest
	ServerStatus *plugin.ServerStatus // built-in plugin only (ADR 0115)
}

// WidgetRequest is one cell of the page.
type WidgetRequest struct {
	Plugin string
	Widget string
	Args   map[string]any
	Width  int // pixels
	Height int // pixels
	Style  plugin.Style
}

// Result is the answer for one WidgetRequest.
type Result struct {
	Image image.Image // image displays
	Value *plugin.Value
	Spec  plugin.ValueSpec // the declaration of a value
	Err   error
	Stale bool // an older result, because the plugin failed
}

// WidgetSource fetches the widgets of a page; the result has one entry per
// request in the same order. It runs before the page is drawn and outside the
// render lock, because plugins are external processes.
type WidgetSource func(b Batch) []Result

// ValueRequest is one declared value a text page needs, with the arguments of
// its box (ADR 0120).
type ValueRequest struct {
	Plugin string
	Name   string
	Args   map[string]any
}

// ValueBatch asks for all values of one page, one entry per value. Device is
// the display a device render is for; nil for a preview.
type ValueBatch struct {
	Page   plugin.PageInfo
	Device *plugin.DeviceInfo
	Values []ValueRequest
}

// ValueSource fetches values for a text page; the result has one entry per
// request in the same order. Like WidgetSource it runs outside the render lock.
type ValueSource func(b ValueBatch) []Result

// renderMu serializes drawing: the cached font faces are not safe for
// concurrent use.
var renderMu sync.Mutex

func text(language, de, en string) string {
	if language == "en" {
		return en
	}
	return de
}

// Rects returns the pixel rectangle of every cell on a display, in cell order.
// It is the geometry the action mode taps against (ADR 0162); the header band
// moves every cell down (ADR 0166).
func (t *Template) Rects(d Display) []image.Rectangle { return t.cellRects(d) }

// Actionable reports how many cells name an action. A text page has none.
func (t *Template) Actionable() int {
	if t.Kind != KindImage {
		return 0
	}
	n := 0
	for _, c := range t.Cells {
		if c.Action != nil && c.Action.Name != "" {
			n++
		}
	}
	return n
}

// headerHeight returns the height of the header band in pixels. A display
// without ppi, as in a test, uses 72.
func (t *Template) headerHeight(d Display) int {
	if !t.ShowHeader() || d.Header == nil {
		return 0
	}
	ppi := d.PPI
	if ppi <= 0 {
		ppi = 72
	}
	return max(int(math.Round(d.Header.Height*ppi/72)), 1)
}

// headerParts returns the header parts the page draws, or nil when the page
// hides the band.
func (t *Template) headerParts(d Display) []HeaderPart {
	if !t.ShowHeader() || d.Header == nil {
		return nil
	}
	return d.Header.Parts
}

// rulePx is the page's rule in pixels. The header band uses it for the line
// under its parts.
func (t *Template) rulePx(d Display) int {
	rule := t.Style.Rule
	if rule == 0 {
		rule = DefaultRulePt
	}
	return d.pxAtLeast1(rule)
}

// headerRects gives every fixed part its width and shares the remaining width
// among the fill parts, in drawing order (ADR 0168).
func (t *Template) headerRects(d Display) []image.Rectangle {
	parts := t.headerParts(d)
	if len(parts) == 0 {
		return nil
	}
	ppi := d.PPI
	if ppi <= 0 {
		ppi = 72
	}
	fixed := 0
	fills := 0
	for _, p := range parts {
		if p.Fill {
			fills++
			continue
		}
		fixed += max(int(math.Round(p.Width*ppi/72)), 1)
	}
	remaining := max(d.Width-fixed, 0)
	fillWidth, extra := 0, 0
	if fills > 0 {
		fillWidth, extra = remaining/fills, remaining%fills
	}
	h := t.headerHeight(d) - t.rulePx(d)
	if h < 1 {
		h = t.headerHeight(d)
	}
	out := make([]image.Rectangle, len(parts))
	x, fillIndex := 0, 0
	for i, p := range parts {
		w := 0
		if p.Fill {
			w = fillWidth
			if fillIndex < extra {
				w++
			}
			fillIndex++
		} else {
			w = max(int(math.Round(p.Width*ppi/72)), 1)
		}
		out[i] = image.Rect(x, 0, x+w, h)
		x += w
	}
	return out
}

// cellRects divides the area below the header band into the pixel rectangles
// of the cells.
func (t *Template) cellRects(d Display) []image.Rectangle {
	top := t.headerHeight(d)
	h := d.Height - top
	if h < 1 {
		top, h = 0, d.Height
	}
	cols, rows := t.Grid[0], t.Grid[1]
	out := make([]image.Rectangle, len(t.Cells))
	for i, c := range t.Cells {
		out[i] = image.Rect(d.Width*c.At[0]/cols, top+h*c.At[1]/rows,
			d.Width*(c.At[0]+c.Span[0])/cols, top+h*(c.At[1]+c.Span[1])/rows)
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
	rects := t.cellRects(d)
	headerParts := t.headerParts(d)
	headerRects := t.headerRects(d)
	batch := Batch{Levels: d.levels(), Page: t.pageInfo(d, c), ServerStatus: c.ServerStatus}
	for i, part := range headerParts {
		batch.Widgets = append(batch.Widgets, WidgetRequest{Plugin: part.Plugin(), Widget: part.Name(), Args: part.Args,
			Width: headerRects[i].Dx(), Height: headerRects[i].Dy(),
			Style: t.pluginStyle(Cell{Widget: part.Widget, Args: part.Args}, d)})
	}
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
	drawResult := func(r Result, area image.Rectangle, label string) {
		switch {
		case r.Image == nil:
			msg := text(language, "kein Ergebnis", "no result")
			if r.Err != nil {
				msg = r.Err.Error()
			}
			cellNotice(img, area, label, msg)
			return
		case r.Image.Bounds().Dx() != area.Dx() || r.Image.Bounds().Dy() != area.Dy():
			b := r.Image.Bounds()
			cellNotice(img, area, label, fmt.Sprintf(text(language, "falsche Größe %dx%d statt %dx%d",
				"wrong size %dx%d instead of %dx%d"), b.Dx(), b.Dy(), area.Dx(), area.Dy()))
			return
		}
		draw.Draw(img, area, render.Quantize(r.Image, d.levels()), image.Point{}, draw.Src)
		if r.Stale {
			stale = append(stale, label)
		}
	}
	for i, part := range headerParts {
		drawResult(results[i], headerRects[i], part.Widget)
	}
	if h, rule := t.headerHeight(d), t.rulePx(d); rule > 0 && h > rule {
		render.HLine(img, 0, h-rule, d.Width, rule)
	}
	offset := len(headerParts)
	for i, cell := range t.Cells {
		drawResult(results[offset+i], rects[i], cell.Widget)
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

// fetchValues fills the answers a source did not give, so a missing plugin is
// an error for its cells instead of a silent gap.
func fetchValues(src ValueSource, b ValueBatch) []Result {
	var results []Result
	if src != nil {
		results = src(b)
	}
	if len(results) != len(b.Values) {
		out := make([]Result, len(b.Values))
		copy(out, results)
		for i := len(results); i < len(out); i++ {
			out[i] = Result{Err: fmt.Errorf("plugin %q is not available", b.Values[i].Plugin)}
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
	render.TextAt(img, render.Ellipsize(format.FirstLine(msg), small, false, inner.Dx()-8), cx, cy+int(size*0.95), small, false, color.Black, render.Center)
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

// Text is a composed text page. Scrolls carry the full content and the first
// window of every scrolling box, so the firmware and the virtual display can
// animate without composing the page again (ADR 0118).
type Text struct {
	Lines   []string
	Scrolls []ScrollBox
}

// ScrollBox is one scrolling text box of a composed page.
type ScrollBox struct {
	At        [2]int
	Span      [2]int
	Lines     []string // full content, not cut to the box
	Window    []string // the first visible lines
	Direction string   // horizontal, vertical or both
	Align     string   // left, right or decimal; empty means left
}

// valueKey is the identity of an answer a text cell reads: the value and the
// arguments of its box (ADR 0120).
type valueKey struct{ plugin, name, args string }

// keyOf derives the identity from a text cell.
func keyOf(cell Cell) valueKey {
	args, _ := json.Marshal(cell.Args) // map keys are sorted
	return valueKey{cell.ValuePlugin(), cell.ValueKey(), string(args)}
}

// RenderText composes a text page from boxes of values and literal text. A
// cell's overflow decides whether its content is cut, wrapped or scrolled.
func (t *Template) RenderText(d Display, c Context) (Text, error) {
	if t.Kind != KindText {
		return Text{}, fmt.Errorf("page is for image displays (kind: %s)", t.Kind)
	}
	if err := t.Fits(d); err != nil {
		return Text{}, err
	}
	var requests []ValueRequest
	index := map[valueKey]int{}
	for _, cell := range t.Cells {
		if cell.Value == "" {
			continue
		}
		k := keyOf(cell)
		if _, ok := index[k]; !ok {
			index[k] = len(requests)
			requests = append(requests, ValueRequest{Plugin: cell.ValuePlugin(), Name: cell.ValueKey(), Args: cell.Args})
		}
	}
	results := fetchValues(c.Values, ValueBatch{Page: t.pageInfo(d, c), Device: c.Device, Values: requests})
	now := c.Now
	if now.IsZero() {
		now = time.Now()
	}
	loc := d.Timezone
	if loc == nil {
		loc = time.Local
	}
	lang := d.language()
	stale := false
	lines := make([]rune, d.Width*d.Height)
	for i := range lines {
		lines[i] = ' '
	}
	var scrolls []ScrollBox
	for _, cell := range t.Cells {
		var r Result
		if cell.Value != "" {
			r = results[index[keyOf(cell)]]
			stale = stale || r.Stale
		}
		window, full, direction, scrolling := textCellLines(r, cell, d.Charset, lang, now, loc, t.Overflow)
		for y, line := range window {
			row := cell.At[1] + y
			if row >= d.Height {
				break
			}
			text := []rune(fit(line, cell.Span[0]))
			copy(lines[row*d.Width+cell.At[0]:], text)
		}
		if scrolling {
			scrolls = append(scrolls, ScrollBox{At: cell.At, Span: cell.Span, Lines: full, Window: window, Direction: direction, Align: cell.Align})
		}
	}
	out := make([]string, d.Height)
	for y := 0; y < d.Height; y++ {
		out[y] = string(lines[y*d.Width : (y+1)*d.Width])
	}
	if stale && t.Notices == "show" && d.Width > 0 {
		line := []rune(out[0])
		line[len(line)-1] = '!'
		out[0] = string(line)
	}
	return Text{Lines: out, Scrolls: scrolls}, nil
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
