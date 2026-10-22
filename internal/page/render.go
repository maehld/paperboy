package page

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"text/template/parse"
	"time"

	"github.com/maehld/paperboy/internal/data"
)

// Display describes the panel a page is rendered for.
type Display struct {
	Width    int
	Height   int
	Gray     bool           // four gray levels instead of black and white
	Language string         // "de" or "en"; empty means "de"
	Timezone *time.Location // plans, built-in dates and chart days; nil = local
}

// language returns the effective language of the display.
func (d Display) language() string {
	if d.Language == "en" {
		return "en"
	}
	return "de"
}

func (d Display) location() *time.Location {
	if d.Timezone == nil {
		return time.Local
	}
	return d.Timezone
}

// Context carries everything that does not come from the data document: the
// clock, the position in the plan and extra banners such as the battery warning.
type Context struct {
	Now     time.Time
	Page    int // 1-based page index for the page indicator and templates
	Pages   int // number of pages in the plan
	Banners []string
}

// text picks the German or English variant of a built-in text.
func text(language, de, en string) string {
	if language == "en" {
		return en
	}
	return de
}

// Render draws the page from the data document. Values that are missing or
// null are shown as an em dash; stale watched paths add a notice bar. extra
// banners (e.g. the battery warning) are stacked on top.
func (t *Template) Render(doc *data.Document, d Display, now time.Time, extra ...string) (*image.Paletted, error) {
	return t.RenderContext(doc, d, Context{Now: now, Banners: extra})
}

// renderMu serializes rendering: the cached font faces are not safe for
// concurrent use, and a page render is cheap.
var renderMu sync.Mutex

// RenderContext draws the page with the full render context (ADR 0034).
func (t *Template) RenderContext(doc *data.Document, d Display, c Context) (*image.Paletted, error) {
	renderMu.Lock()
	defer renderMu.Unlock()
	raw, _ := doc.Get("")
	root, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("data document is not an object")
	}
	language := d.language()
	loc := d.location()
	now := c.Now.In(loc)
	if c.Page <= 0 {
		c.Page = 1
	}
	if c.Pages <= 0 {
		c.Pages = 1
	}
	r := &renderer{t: t, doc: doc, root: root, f: formatter{comma: language != "en", loc: loc},
		now: now, language: language, page: c.Page, pages: c.Pages, badge: t.noticeMode() == "badge"}
	r.stale = t.isStale(doc, now)

	img := newCanvas(d.Width, d.Height, d.Gray)
	bands := t.bands(d)
	for i, b := range t.Blocks {
		if i >= len(bands) {
			break
		}
		r.block(img, bands[i], b)
	}
	if r.err != nil {
		return nil, r.err
	}
	var banners []string
	if t.noticeMode() == "bar" {
		banners = append(banners, t.staleBanners(doc, now, language)...)
	}
	banners = append(banners, c.Banners...)
	if len(banners) > 0 {
		img = withBanner(img, banners...)
	}
	return img, nil
}

// noticeMode returns the notice style; the default is the bottom bar.
func (t *Template) noticeMode() string {
	if t.Notices == "" {
		return "bar"
	}
	return t.Notices
}

// bands splits the page height according to the row weights.
func (t *Template) bands(d Display) []image.Rectangle {
	total := 0
	for _, w := range t.Rows {
		total += w
	}
	out := make([]image.Rectangle, len(t.Rows))
	y := 0
	for i, w := range t.Rows {
		h := d.Height * w / total
		if h < 1 {
			h = 1
		}
		if i == len(t.Rows)-1 {
			h = d.Height - y
		}
		if h < 0 {
			h = 0
		}
		out[i] = image.Rect(0, y, d.Width, y+h)
		y += h
	}
	return out
}

func (t *Template) staleBanners(doc *data.Document, now time.Time, language string) []string {
	var banners []string
	for _, w := range t.Watch {
		age, ok := doc.Age(w.Path)
		if !ok {
			continue // never written: values show an em dash, no banner
		}
		if age <= time.Duration(w.StaleAfter) {
			continue
		}
		label := w.Label
		if label == "" {
			label = w.Path
		}
		banners = append(banners, text(language, "Keine neuen Daten seit ", "No new data since ")+now.Add(-age).Format("02.01. 15:04")+" ("+label+")")
	}
	return banners
}

// isStale reports whether any watched path is older than its stale_after.
func (t *Template) isStale(doc *data.Document, now time.Time) bool {
	for _, w := range t.Watch {
		age, ok := doc.Age(w.Path)
		if ok && age > time.Duration(w.StaleAfter) {
			return true
		}
	}
	return false
}

type renderer struct {
	t        *Template
	doc      *data.Document
	root     map[string]any
	f        formatter
	now      time.Time
	language string
	page     int
	pages    int
	stale    bool
	badge    bool
	err      error
}

func (r *renderer) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

// expand runs the text/template helpers on a label, title or text. Missing
// branches of the document are created as empty objects so that an unset
// supplier still renders dashes instead of failing the whole page.
func (r *renderer) expand(s string) string {
	if s == "" || !strings.Contains(s, "{{") {
		return s
	}
	tmpl, err := template.New("text").Option("missingkey=zero").Funcs(r.funcs()).Parse(s)
	if err != nil {
		r.fail(err)
		return ""
	}
	ensureBranches(r.root, tmpl.Tree)
	var b strings.Builder
	if err := tmpl.Execute(&b, r.root); err != nil {
		r.fail(err)
		return ""
	}
	return strings.TrimSpace(strings.ReplaceAll(b.String(), "<no value>", "—"))
}

// ensureBranches creates the intermediate objects of every field chain used in
// the template, so a missing branch yields an em dash instead of an error.
func ensureBranches(root map[string]any, tree *parse.Tree) {
	if tree == nil || tree.Root == nil {
		return
	}
	walkFields(root, tree.Root)
}

func walkFields(root map[string]any, n parse.Node) {
	switch n := n.(type) {
	case *parse.ListNode:
		for _, c := range n.Nodes {
			walkFields(root, c)
		}
	case *parse.ActionNode:
		walkFields(root, n.Pipe)
	case *parse.PipeNode:
		for _, c := range n.Cmds {
			walkFields(root, c)
		}
	case *parse.CommandNode:
		for _, a := range n.Args {
			walkFields(root, a)
		}
	case *parse.FieldNode:
		ensurePath(root, n.Ident)
	case *parse.ChainNode:
		if f, ok := n.Node.(*parse.FieldNode); ok {
			ensurePath(root, append(append([]string{}, f.Ident...), n.Field...))
		}
	case *parse.IfNode:
		walkFields(root, n.Pipe)
		walkFields(root, n.List)
		if n.ElseList != nil {
			walkFields(root, n.ElseList)
		}
	case *parse.WithNode:
		walkFields(root, n.Pipe)
		walkFields(root, n.List)
		if n.ElseList != nil {
			walkFields(root, n.ElseList)
		}
	case *parse.RangeNode:
		walkFields(root, n.Pipe)
		walkFields(root, n.List)
		if n.ElseList != nil {
			walkFields(root, n.ElseList)
		}
	case *parse.TemplateNode:
		walkFields(root, n.Pipe)
	}
}

func ensurePath(root map[string]any, path []string) {
	cur := root
	for i := 0; i < len(path)-1; i++ {
		next, ok := cur[path[i]].(map[string]any)
		if !ok {
			if v, exists := cur[path[i]]; exists && v != nil {
				return // a scalar cannot be descended into; leave the error visible
			}
			next = map[string]any{}
			cur[path[i]] = next
		}
		cur = next
	}
}

func (r *renderer) funcs() template.FuncMap {
	return template.FuncMap{
		"power":   func(v any) string { return r.f.format(v, "power") },
		"energy":  func(v any) string { return r.f.format(v, "energy") },
		"percent": func(v any) string { return r.f.format(v, "percent") },
		"temp":    func(v any) string { return r.f.format(v, "temp") },
		"time": func(v any) string {
			if ts, ok := v.(time.Time); ok {
				return ts.In(r.f.location()).Format("15:04")
			}
			return r.f.format(v, "time")
		},
		"abs":   func(v any) string { return r.f.format(v, "abs") },
		"ratio": func(v any) string { return r.f.format(v, "ratio") },
		"now":   func() time.Time { return r.now },
		"page":  func() int { return r.page },
		"pages": func() int { return r.pages },
		"stale": func(p string) (bool, error) { return r.stalePath(p) },
		"below": func(v, limit any) bool { return compare(v, limit, func(a, b float64) bool { return a < b }) },
		"above": func(v, limit any) bool { return compare(v, limit, func(a, b float64) bool { return a > b }) },
	}
}

// stalePath reports whether a watched path is older than its stale_after. The
// path has to appear in watch; an unknown path is a template error.
func (r *renderer) stalePath(p string) (bool, error) {
	path, err := normalizePath(p)
	if err != nil {
		return false, err
	}
	for _, w := range r.t.Watch {
		if w.Path != path {
			continue
		}
		if r.doc == nil {
			return false, nil
		}
		age, ok := r.doc.Age(path)
		return ok && age > time.Duration(w.StaleAfter), nil
	}
	return false, fmt.Errorf("stale %q: path is not watched", path)
}

func compare(v, limit any, fn func(a, b float64) bool) bool {
	a, ok := toFloat(v)
	if !ok {
		return false
	}
	b, ok := toFloat(limit)
	return ok && fn(a, b)
}

// value formats a tile value and applies the optional sign words.
func (r *renderer) value(t Tile) string {
	v, ok := lookupValue(r.root, t.Value)
	if !ok {
		return "—"
	}
	n, isNumber := toFloat(v)
	if len(t.Sign) == 2 && isNumber {
		word := t.Sign[1]
		if n < 0 {
			word = t.Sign[0]
		}
		return r.f.magnitude(n, t.Format) + " " + word
	}
	return r.f.format(v, t.Format)
}

// ---- blocks ----

func (r *renderer) block(img *image.Paletted, rc image.Rectangle, b Block) {
	if rc.Dx() <= 0 || rc.Dy() <= 0 {
		return
	}
	switch {
	case b.Header != nil:
		r.header(img, rc, b)
	case b.Tiles != nil:
		r.tiles(img, rc, b)
	case b.Rows != nil:
		r.rows(img, rc, b)
	case b.Chart != nil:
		r.chart(img, rc, b.Chart, b.Padding)
	case b.Text != nil:
		r.text(img, rc, b)
	case b.Columns != nil:
		r.columns(img, rc, b)
	case b.Bar != nil:
		r.bar(img, rc, b.Bar, b.Padding)
	}
	if b.Rule > 0 {
		hline(img, rc.Min.X, rc.Max.Y-b.Rule, rc.Max.X, b.Rule)
	}
}

// padding resolves the inner distance of a block; the object form overrides the
// block-level value.
func (r *renderer) padding(b *Block, override *int) int {
	p := 4
	if b.Padding != nil {
		p = *b.Padding
	}
	if override != nil {
		p = *override
	}
	return max(p, 0)
}

// style resolves size, weight and shrink with the given defaults. An explicit
// size is a fraction of refHeight, as in ADR 0034.
func style(size, refHeight, defFraction float64, weight string, defBold bool, shrink *bool) textStyle {
	bold := defBold
	switch weight {
	case "bold":
		bold = true
	case "regular":
		bold = false
	}
	sh := true
	if shrink != nil {
		sh = *shrink
	}
	return textStyle{Size: fractionSize(size, refHeight, defFraction), Bold: bold, Shrink: sh}
}

// fractionSize turns a fraction of refHeight into pixels; zero means the default
// fraction.
func fractionSize(size, refHeight, defFraction float64) float64 {
	if size <= 0 {
		size = defFraction
	}
	return size * refHeight
}

func pickShrink(a, b *bool) *bool {
	if a != nil {
		return a
	}
	return b
}

func parseAlign(s string, def int) int {
	switch s {
	case "left":
		return alignLeft
	case "center":
		return alignCenter
	case "right":
		return alignRight
	}
	return def
}

func (r *renderer) header(img *image.Paletted, rc image.Rectangle, b Block) {
	h := b.Header
	pad := r.padding(&b, h.Padding)
	rule := h.Rule
	if rule == 0 {
		rule = 1
	}
	hgt := rc.Dy()

	title := r.expand(h.Title)
	if title == "" {
		title = r.expand(r.t.Title)
	}
	subtitle := r.expand(h.Subtitle)

	statusText := ""
	if h.Status != nil {
		clock := ""
		if h.Status.Time != "" {
			if v, ok := lookupValue(r.root, h.Status.Time); ok {
				clock = r.f.format(v, "time")
			} else {
				clock = "—"
			}
		}
		statusText = strings.TrimSpace(strings.Join([]string{r.expand(h.Status.Label), clock}, " "))
	} else if h.Time != "" {
		if v, ok := lookupValue(r.root, h.Time); ok {
			statusText = r.f.format(v, "time")
		}
	}

	stale := r.stale && r.badge
	if stale {
		statusText = strings.TrimSpace(statusText + " · " + text(r.language, "veraltet", "outdated"))
	}
	if h.PageIndicator && r.pages > 1 && r.page > 0 {
		title += fmt.Sprintf(" - %d/%d", r.page, r.pages)
	}

	statusShrink := h.Shrink
	statusSize := fractionSize(h.StatusSize, float64(hgt), 0.45)
	statusBold := h.StatusWeight == "bold" || (stale && h.StatusWeight == "")
	statusWidth := 0
	if statusText != "" {
		statusWidth = measure(statusText, statusSize, statusBold) + pad
	}

	left, right := rc.Min.X+pad, rc.Max.X-pad
	if statusWidth > 0 {
		right -= statusWidth + pad
	}
	if right < left+1 {
		right = left + 1
	}
	align := parseAlign(h.Align, alignCenter)
	x := left
	switch align {
	case alignCenter:
		x = left + (right-left)/2
	case alignRight:
		x = right
	}

	bodyTop := rc.Min.Y + pad
	bodyBottom := rc.Max.Y - rule - pad
	baseline := bodyBottom
	if subtitle != "" {
		baseline = bodyTop + (bodyBottom-bodyTop)*45/100
	}
	titleStyle := style(h.TitleSize, float64(hgt), 0.5, h.TitleWeight, true, h.Shrink)
	drawStyled(img, title, x, baseline, titleStyle, right-left, color.Black, align)

	if subtitle != "" {
		subStyle := style(h.SubtitleSize, float64(hgt), 0.22, h.SubtitleWeight, false, h.Shrink)
		drawStyled(img, subtitle, x, bodyBottom, subStyle, right-left, color.Black, align)
	}

	if statusText != "" {
		if stale {
			bw := statusWidth + pad/2
			x0 := rc.Max.X - pad - bw
			badge := image.Rect(x0, rc.Min.Y+pad/2, rc.Max.X-pad, rc.Max.Y-rule-pad/2)
			if badge.Empty() {
				badge = image.Rect(x0, rc.Min.Y, rc.Max.X-pad, rc.Max.Y-rule)
			}
			fillSolid(img, badge)
			drawStyled(img, statusText, rc.Max.X-pad-pad/4, baseline,
				textStyle{Size: statusSize, Bold: statusBold, Shrink: true}, rc.Dx(), color.White, alignRight)
		} else {
			drawStyled(img, statusText, rc.Max.X-pad, baseline,
				textStyle{Size: statusSize, Bold: statusBold, Shrink: statusShrinkOrDefault(statusShrink)},
				rc.Dx()/2, color.Black, alignRight)
		}
	}

	if rule > 0 {
		hline(img, rc.Min.X, rc.Max.Y-rule, rc.Max.X, rule)
	}
}

func statusShrinkOrDefault(s *bool) bool {
	if s == nil {
		return true
	}
	return *s
}

func (r *renderer) tiles(img *image.Paletted, rc image.Rectangle, b Block) {
	t := b.Tiles
	cols := t.Columns
	if cols <= 0 {
		cols = 2
	}
	rows := (len(t.Items) + cols - 1) / cols
	cw, ch := rc.Dx()/cols, rc.Dy()/rows
	if cw < 1 || ch < 1 {
		return
	}
	for i := range t.Items {
		x := rc.Min.X + (i%cols)*cw
		y := rc.Min.Y + (i/cols)*ch
		r.tile(img, image.Rect(x, y, x+cw, y+ch), t.Items[i], t, b.Padding)
	}
	if t.Divider > 0 {
		for c := 1; c < cols; c++ {
			vline(img, rc.Min.X+c*cw-t.Divider/2, rc.Min.Y, rc.Max.Y, t.Divider)
		}
		for row := 1; row < rows; row++ {
			hline(img, rc.Min.X, rc.Min.Y+row*ch-t.Divider/2, rc.Max.X, t.Divider)
		}
	}
}

func (r *renderer) tile(img *image.Paletted, cell image.Rectangle, tile Tile, list *Tiles, blockPad *int) {
	b := Block{Padding: blockPad}
	pad := r.padding(&b, list.Padding)
	cw, ch := cell.Dx(), cell.Dy()
	align := parseAlign(tile.Align, parseAlign(list.Align, alignLeft))
	shrink := pickShrink(tile.Shrink, list.Shrink)

	labelStyle := style(tile.LabelSize, float64(ch), 0.20, tile.LabelWeight, false, shrink)
	valueStyle := style(tile.ValueSize, float64(ch), 0.34, tile.ValueWeight, true, shrink)
	detail := r.expand(tile.Detail)
	detailStyle := style(tile.DetailSize, float64(ch), 0.17, tile.DetailWeight, false, shrink)

	x := cell.Min.X + pad
	switch align {
	case alignCenter:
		x = cell.Min.X + cw/2
	case alignRight:
		x = cell.Max.X - pad
	}
	maxWidth := cw - 2*pad
	labelBase := cell.Min.Y + pad + int(labelStyle.Size)
	valueBase := cell.Min.Y + int(float64(ch)*0.72)
	if detail != "" {
		valueBase = cell.Min.Y + int(float64(ch)*0.66)
	}
	drawStyled(img, r.expand(tile.Label), x, labelBase, labelStyle, maxWidth, color.Black, align)
	drawStyled(img, r.value(tile), x, valueBase, valueStyle, maxWidth, color.Black, align)
	if detail != "" {
		drawStyled(img, detail, x, cell.Max.Y-pad, detailStyle, maxWidth, color.Black, align)
	}
}

func (r *renderer) rows(img *image.Paletted, rc image.Rectangle, b Block) {
	rs := b.Rows
	items := rs.Items
	step := rc.Dy() / len(items)
	if step < 1 {
		return
	}
	pad := r.padding(&b, rs.Padding)
	align := parseAlign(rs.Align, alignRight)
	for i := range items {
		item := &items[i]
		y := rc.Min.Y + i*step
		if i > 0 && rs.Rule > 0 {
			hline(img, rc.Min.X+pad, y-rs.Rule/2, rc.Max.X-pad, rs.Rule)
		}
		shrink := pickShrink(item.Shrink, rs.Shrink)
		labelStyle := style(item.LabelSize, float64(step), 0.40, item.LabelWeight, false, shrink)
		valueStyle := style(item.ValueSize, float64(step), 0.40, item.ValueWeight, true, shrink)
		detail := r.expand(item.Detail)
		detailStyle := style(item.DetailSize, float64(step), 0.30, item.DetailWeight, false, shrink)

		labelBase := y + step*7/10
		detailBase := y + step*85/100
		if detail != "" {
			labelBase = y + step*45/100
		}
		drawStyled(img, r.expand(item.Label), rc.Min.X+pad, labelBase, labelStyle,
			rc.Dx()/2-pad, color.Black, alignLeft)
		drawStyled(img, r.value(*item), rc.Max.X-pad, labelBase, valueStyle,
			rc.Dx()/2-pad, color.Black, align)
		if detail != "" {
			drawStyled(img, detail, rc.Min.X+pad, detailBase, detailStyle,
				rc.Dx()/2-pad, color.Black, alignLeft)
		}
	}
}

func (r *renderer) text(img *image.Paletted, rc image.Rectangle, b Block) {
	t := b.Text
	pad := r.padding(&b, t.Padding)
	lines := strings.Split(r.expand(t.Value), "\n")
	if len(lines) == 0 {
		return
	}
	lineHeight := (rc.Dy() - 2*pad) / len(lines)
	if lineHeight < 1 {
		return
	}
	align := parseAlign(t.Align, alignCenter)
	x := rc.Min.X + rc.Dx()/2
	switch align {
	case alignLeft:
		x = rc.Min.X + pad
	case alignRight:
		x = rc.Max.X - pad
	}
	for i, line := range lines {
		st := style(t.Size, float64(lineHeight), 0.5, t.Weight, false, t.Shrink)
		baseline := rc.Min.Y + pad + i*lineHeight + lineHeight*3/5
		drawStyled(img, line, x, baseline, st, rc.Dx()-2*pad, color.Black, align)
	}
}

func (r *renderer) columns(img *image.Paletted, rc image.Rectangle, b Block) {
	c := b.Columns
	total := 0
	for _, col := range c.Items {
		total += col.Weight
	}
	if total < 1 {
		return
	}
	x := rc.Min.X
	for i, col := range c.Items {
		w := rc.Dx() * col.Weight / total
		if i == len(c.Items)-1 {
			w = rc.Max.X - x
		}
		if c.Divider > 0 && i > 0 {
			vline(img, x-c.Divider/2, rc.Min.Y, rc.Max.Y, c.Divider)
		}
		r.block(img, image.Rect(x, rc.Min.Y, x+w, rc.Max.Y), col.Block)
		x += w
	}
}

// ---- formatting ----

type formatter struct {
	comma bool
	loc   *time.Location
}

func (f formatter) location() *time.Location {
	if f.loc == nil {
		return time.Local
	}
	return f.loc
}

func (f formatter) format(v any, kind string) string {
	if v == nil {
		return "—"
	}
	if kind == "time" {
		switch ts := v.(type) {
		case time.Time:
			return ts.In(f.location()).Format("15:04")
		case string:
			parsed, err := time.Parse(time.RFC3339, ts)
			if err != nil {
				return "—"
			}
			return parsed.In(f.location()).Format("15:04")
		}
		return "—"
	}
	if kind == "" {
		if s, ok := v.(string); ok {
			return s
		}
		if n, ok := toFloat(v); ok {
			return f.trim(n)
		}
		return "—"
	}
	n, ok := toFloat(v)
	if !ok {
		return "—"
	}
	return f.magnitude(n, kind)
}

func (f formatter) magnitude(n float64, kind string) string {
	a := math.Abs(n)
	switch kind {
	case "power":
		if a >= 1000 {
			return f.num(a/1000, 2) + " kW"
		}
		return f.num(a, 0) + " W"
	case "energy":
		return f.num(a, 1) + " kWh"
	case "percent":
		return f.num(a, 0) + " %"
	case "ratio":
		return f.num(a*100, 0) + " %"
	case "temp":
		return f.num(a, 1) + " °C"
	case "abs":
		return f.trim(a)
	case "duration":
		return f.durationText(a)
	}
	return f.trim(n)
}

// durationText renders seconds as whole minutes, e.g. "42 min", "2 h" or
// "3 h 20 min"; longer than a day stays in hours ("31 h 5 min").
func (f formatter) durationText(seconds float64) string {
	minutes := int(math.Round(seconds / 60))
	if minutes < 60 {
		return f.num(float64(minutes), 0) + " min"
	}
	hours := minutes / 60
	if minutes%60 == 0 {
		return f.num(float64(hours), 0) + " h"
	}
	return f.num(float64(hours), 0) + " h " + f.num(float64(minutes%60), 0) + " min"
}

func (f formatter) num(v float64, decimals int) string {
	return f.separator(strconv.FormatFloat(v, 'f', decimals, 64))
}

func (f formatter) trim(v float64) string {
	return f.separator(strconv.FormatFloat(v, 'f', -1, 64))
}

func (f formatter) separator(s string) string {
	if f.comma {
		return strings.Replace(s, ".", ",", 1)
	}
	return s
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// ---- document access ----

func lookupValue(root map[string]any, path string) (any, bool) {
	p, err := normalizePath(path)
	if err != nil {
		return nil, false
	}
	var cur any = root
	for _, seg := range strings.Split(p, "/") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[seg]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func lookupList(root map[string]any, path string) ([]any, bool) {
	v, ok := lookupValue(root, path)
	if !ok {
		return nil, false
	}
	list, ok := v.([]any)
	return list, ok
}

func seriesValue(p any, key string) (float64, bool) {
	m, ok := p.(map[string]any)
	if !ok {
		return 0, false
	}
	values, ok := m["values"].(map[string]any)
	if !ok {
		return 0, false
	}
	return toFloat(values[key])
}

// unitFormat maps a chart unit to a formatting helper.
func unitFormat(unit string) string {
	switch unit {
	case "W", "kW":
		return "power"
	case "kWh", "Wh":
		return "energy"
	case "%":
		return "percent"
	case "°C":
		return "temp"
	}
	return ""
}

// withBanner returns a copy of img with inverted notice bars stacked at the
// bottom, matching the bars of the image API.
func withBanner(img *image.Paletted, texts ...string) *image.Paletted {
	out := image.NewPaletted(img.Rect, img.Palette)
	copy(out.Pix, img.Pix)
	w, h := img.Rect.Dx(), img.Rect.Dy()
	bar := max(h/8, 1)
	for i, text := range texts {
		bottom := h - i*bar
		if bottom-bar < 0 {
			break
		}
		draw.Draw(out, image.Rect(0, bottom-bar, w, bottom), image.NewUniform(color.Black), image.Point{}, draw.Src)
		if i > 0 {
			draw.Draw(out, image.Rect(0, bottom-max(1, bar/30), w, bottom), image.NewUniform(color.White), image.Point{}, draw.Src)
		}
		drawStyled(out, text, w/2, bottom-bar*3/10, textStyle{Size: float64(bar) * 0.55, Bold: true, Shrink: true},
			w-w/25, color.White, alignCenter)
	}
	return out
}
