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
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/maehld/paperboy/internal/data"
)

// Display describes the panel a page is rendered for.
type Display struct {
	Width  int
	Height int
	Gray   bool // four gray levels instead of black and white
}

var (
	monoPalette = color.Palette{color.Gray{0}, color.Gray{255}}
	grayPalette = color.Palette{color.Gray{0}, color.Gray{85}, color.Gray{170}, color.Gray{255}}
)

// Render draws the page from the data document. Values that are missing or
// null are shown as an em dash; stale watched paths add a notice bar.
func (t *Template) Render(doc *data.Document, d Display, now time.Time) (*image.Paletted, error) {
	raw, _ := doc.Get("")
	root, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("data document is not an object")
	}
	r := &renderer{t: t, root: root, f: formatter{comma: t.Locale != "en"}, now: now}
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
	if banners := t.staleBanners(doc, now); len(banners) > 0 {
		img = withBanner(img, banners...)
	}
	return img, nil
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

func (t *Template) staleBanners(doc *data.Document, now time.Time) []string {
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
		banners = append(banners, "Keine neuen Daten seit "+now.Add(-age).Format("02.01. 15:04")+" ("+label+")")
	}
	return banners
}

type renderer struct {
	t    *Template
	root map[string]any
	f    formatter
	now  time.Time
	err  error
}

func (r *renderer) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

// expand runs the text/template helpers on a label, title or text.
func (r *renderer) expand(s string) string {
	if s == "" || !strings.Contains(s, "{{") {
		return s
	}
	tmpl, err := template.New("text").Option("missingkey=zero").Funcs(r.funcs()).Parse(s)
	if err != nil {
		r.fail(err)
		return ""
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, r.root); err != nil {
		r.fail(err)
		return ""
	}
	return strings.TrimSpace(b.String())
}

func (r *renderer) funcs() template.FuncMap {
	return template.FuncMap{
		"power":   func(v any) string { return r.f.format(v, "power") },
		"energy":  func(v any) string { return r.f.format(v, "energy") },
		"percent": func(v any) string { return r.f.format(v, "percent") },
		"temp":    func(v any) string { return r.f.format(v, "temp") },
		"time":    func(v any) string { return r.f.format(v, "time") },
		"abs":     func(v any) string { return r.f.format(v, "abs") },
	}
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

func (r *renderer) block(img *image.Paletted, rc image.Rectangle, b Block) {
	switch {
	case b.Header != nil:
		r.header(img, rc, b.Header)
	case len(b.Tiles) > 0:
		r.grid(img, rc, b.Tiles, 2)
	case len(b.Rows) > 0:
		r.list(img, rc, b.Rows)
	case b.Chart != nil:
		r.chart(img, rc, b.Chart)
	case b.Text != nil:
		r.text(img, rc, *b.Text)
	}
}

func (r *renderer) header(img *image.Paletted, rc image.Rectangle, h *Header) {
	title := r.expand(h.Title)
	if title == "" {
		title = r.expand(r.t.Title)
	}
	subtitle := r.expand(h.Subtitle)
	clock := ""
	if h.Time != "" {
		if v, ok := lookupValue(r.root, h.Time); ok {
			clock = r.f.format(v, "time")
		}
	}
	w, hgt := rc.Dx(), rc.Dy()
	pad := max(w/40, 4)
	titleSize := float64(hgt) * 0.38
	base := rc.Min.Y + hgt*3/5
	if subtitle != "" {
		titleSize = float64(hgt) * 0.32
		base = rc.Min.Y + hgt*2/5
	}
	drawFitted(img, title, rc.Min.X+w/2, base, titleSize, w-2*pad, color.Black, alignCenter)
	if clock != "" {
		drawFitted(img, clock, rc.Max.X-pad, base, titleSize*0.7, w/4, color.Black, alignRight)
	}
	if subtitle != "" {
		drawFitted(img, subtitle, rc.Min.X+w/2, rc.Min.Y+hgt*4/5, float64(hgt)*0.22, w-2*pad, color.Black, alignCenter)
	}
	hline(img, rc.Min.X, rc.Max.Y-1, rc.Max.X)
}

func (r *renderer) grid(img *image.Paletted, rc image.Rectangle, tiles []Tile, cols int) {
	lines := (len(tiles) + cols - 1) / cols
	cw, ch := rc.Dx()/cols, rc.Dy()/lines
	for i, tile := range tiles {
		x := rc.Min.X + (i%cols)*cw
		y := rc.Min.Y + (i/cols)*ch
		pad := max(cw/16, 2)
		drawFitted(img, r.expand(tile.Label), x+pad, y+ch/3, float64(ch)*0.2, cw-2*pad, color.Black, alignLeft)
		drawFitted(img, r.value(tile), x+pad, y+ch*4/5, float64(ch)*0.34, cw-2*pad, color.Black, alignLeft)
	}
}

func (r *renderer) list(img *image.Paletted, rc image.Rectangle, items []Tile) {
	ch := rc.Dy() / len(items)
	pad := max(rc.Dx()/40, 2)
	for i, tile := range items {
		y := rc.Min.Y + i*ch
		drawFitted(img, r.expand(tile.Label), rc.Min.X+pad, y+ch*3/5, float64(ch)*0.4, rc.Dx()/2-2*pad, color.Black, alignLeft)
		drawFitted(img, r.value(tile), rc.Max.X-pad, y+ch*3/5, float64(ch)*0.4, rc.Dx()/2-2*pad, color.Black, alignRight)
	}
}

func (r *renderer) chart(img *image.Paletted, rc image.Rectangle, c *Chart) {
	points, _ := lookupList(r.root, c.Points)
	if len(points) < 2 {
		drawFitted(img, "keine Verlaufsdaten", rc.Min.X+rc.Dx()/2, rc.Min.Y+rc.Dy()/2,
			float64(rc.Dy())/8, rc.Dx()-8, color.Black, alignCenter)
		return
	}
	left := rc.Min.X + rc.Dx()/20
	right := rc.Max.X - rc.Dx()/20
	top := rc.Min.Y + rc.Dy()/4
	bottom := rc.Max.Y - rc.Dy()/8
	series := make([][]float64, len(c.Series))
	maxV := 0.0
	for si, key := range c.Series {
		series[si] = make([]float64, len(points))
		for i, p := range points {
			v, ok := seriesValue(p, key)
			if !ok {
				continue
			}
			series[si][i] = v
			if v > maxV {
				maxV = v
			}
		}
	}
	hline(img, left, bottom, right)
	if maxV > 0 {
		label := r.f.format(maxV, unitFormat(c.Unit))
		if unitFormat(c.Unit) == "" && c.Unit != "" {
			label = r.f.trim(maxV) + " " + c.Unit
		}
		drawFitted(img, label, left, top, float64(rc.Dy())/14, rc.Dx()/3, color.Black, alignLeft)
	}
	for si := range series {
		px, py, has := 0, 0, false
		for i, v := range series[si] {
			x := left + (right-left)*i/(len(points)-1)
			y := bottom - int(float64(bottom-top)*v/maxV)
			if has {
				line(img, px, py, x, y, si%2 == 1)
			}
			px, py, has = x, y, true
		}
	}
}

func (r *renderer) text(img *image.Paletted, rc image.Rectangle, s string) {
	s = r.expand(s)
	lines := strings.Split(s, "\n")
	ch := rc.Dy() / len(lines)
	for i, text := range lines {
		drawFitted(img, text, rc.Min.X+rc.Dx()/2, rc.Min.Y+i*ch+ch*3/5,
			float64(ch)*0.5, rc.Dx()-rc.Dx()/20, color.Black, alignCenter)
	}
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

// ---- formatting ----

type formatter struct {
	comma bool
}

func (f formatter) format(v any, kind string) string {
	if v == nil {
		return "—"
	}
	if kind == "time" {
		s, ok := v.(string)
		if !ok {
			return "—"
		}
		ts, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return "—"
		}
		return ts.Local().Format("15:04")
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
		return f.num(a, 2) + " kWh"
	case "percent":
		return f.num(a, 0) + " %"
	case "temp":
		return f.num(a, 1) + " °C"
	case "abs":
		return f.trim(a)
	}
	return f.trim(n)
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

// ---- drawing ----

const (
	alignLeft = iota
	alignCenter
	alignRight
)

var (
	fontOnce sync.Once
	fontBold *opentype.Font
)

func face(size float64) (font.Face, error) {
	fontOnce.Do(func() {
		f, err := opentype.Parse(gobold.TTF)
		if err != nil {
			panic(err)
		}
		fontBold = f
	})
	return opentype.NewFace(fontBold, &opentype.FaceOptions{Size: math.Max(size, 6), DPI: 72, Hinting: font.HintingFull})
}

func newCanvas(w, h int, gray bool) *image.Paletted {
	p := monoPalette
	if gray {
		p = grayPalette
	}
	img := image.NewPaletted(image.Rect(0, 0, w, h), p)
	draw.Draw(img, img.Rect, image.NewUniform(color.White), image.Point{}, draw.Src)
	return img
}

func drawText(dst draw.Image, s string, x, baseline int, size float64, c color.Color, align int) {
	if s == "" {
		return
	}
	fc, err := face(size)
	if err != nil {
		return
	}
	defer fc.Close()
	switch align {
	case alignCenter:
		x -= font.MeasureString(fc, s).Ceil() / 2
	case alignRight:
		x -= font.MeasureString(fc, s).Ceil()
	}
	d := font.Drawer{Dst: dst, Src: image.NewUniform(c), Face: fc, Dot: fixed.P(x, baseline)}
	d.DrawString(s)
}

// drawFitted shrinks the text until it fits maxWidth.
func drawFitted(dst draw.Image, s string, x, baseline int, size float64, maxWidth int, c color.Color, align int) {
	if s == "" || maxWidth <= 0 {
		return
	}
	for size > 6 {
		fc, err := face(size)
		if err != nil {
			break
		}
		w := font.MeasureString(fc, s).Ceil()
		fc.Close()
		if w <= maxWidth {
			break
		}
		size--
	}
	drawText(dst, s, x, baseline, size, c, align)
}

func hline(img *image.Paletted, x0, y, x1 int) {
	if y < 0 || y >= img.Rect.Dy() {
		return
	}
	draw.Draw(img, image.Rect(x0, y, x1, y+1), image.NewUniform(color.Black), image.Point{}, draw.Src)
}

func line(img *image.Paletted, x0, y0, x1, y1 int, dotted bool) {
	dx, dy := absInt(x1-x0), -absInt(y1-y0)
	sx, sy := -1, -1
	if x0 < x1 {
		sx = 1
	}
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	for n := 1; ; n++ {
		if (!dotted || n%3 != 0) && image.Pt(x0, y0).In(img.Rect) {
			img.SetColorIndex(x0, y0, 0)
		}
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
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
		drawFitted(out, text, w/2, bottom-bar*3/10, float64(bar)*0.55, w-w/25, color.White, alignCenter)
	}
	return out
}
