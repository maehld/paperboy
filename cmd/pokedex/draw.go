package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strconv"
	"strings"

	xdraw "golang.org/x/image/draw"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

// current returns the Pokémon that Prepare loaded for the widget's seed.
func (p *pokedex) current(w plugin.WidgetRequest) (*mon, error) {
	seed, err := seedOf(w)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	l, ok := p.curs[seed]
	p.mu.Unlock()
	switch {
	case !ok:
		return nil, fmt.Errorf("no Pokémon loaded for seed %q", seed)
	case l.err != nil:
		return nil, l.err
	}
	return l.mon, nil
}

func (p *pokedex) Render(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	if w.Widget != "card" {
		return fmt.Errorf("%w %q", plugin.ErrUnknownWidget, w.Widget)
	}
	m, err := p.current(w)
	if err != nil {
		return err
	}
	return drawCard(m, req, req.Lang(), w, img)
}

func num(v float64, decimals int, lang string) string {
	s := strconv.FormatFloat(v, 'f', decimals, 64)
	if lang == "de" {
		s = strings.Replace(s, ".", ",", 1)
	}
	return s
}

// statLabels are the short names of the six base stats.
func statLabels(lang string) [6]string {
	if lang == "de" {
		return [6]string{"KP", "Ang", "Vert", "SpA", "SpV", "Init"}
	}
	return [6]string{"HP", "Atk", "Def", "SpA", "SpD", "Spe"}
}

func (m *mon) typesText(lang string, sep string) string {
	var names []string
	for _, t := range m.types {
		names = append(names, m.typeName(t, lang))
	}
	return strings.Join(names, sep)
}

func (m *mon) sizeText(lang string) string {
	return num(float64(m.height)/10, 1, lang) + " m · " + num(float64(m.weight)/10, 1, lang) + " kg"
}

func (m *mon) number() string { return fmt.Sprintf("#%04d", m.id) }

// drawCard draws the card. All sizes derive from the request's base text
// height, stroke width and padding (physical since ADR 0050), the gaps from
// millimetres.
func drawCard(m *mon, req *plugin.Request, lang string, w plugin.WidgetRequest, img *image.Paletted) error {
	pad := w.Style.Padding
	rule := max(w.Style.Rule, 1)
	base := req.TextPx(w)
	gap := max(int(req.MMPx(0.8)+0.5), 2)
	b := img.Rect
	inner := image.Rect(b.Min.X+pad, b.Min.Y+pad, b.Max.X-pad, b.Max.Y-pad)
	if inner.Dx() < 20 || inner.Dy() < 20 {
		return plugin.TooSmall(w)
	}

	// Artwork left of the text on wide cells, above it on tall ones.
	art, col := inner, inner
	if inner.Dx() >= inner.Dy()*11/10 {
		side := min(inner.Dy(), inner.Dx()*42/100)
		art = image.Rect(inner.Min.X, inner.Min.Y, inner.Min.X+side, inner.Min.Y+side)
		col.Min.X = art.Max.X + gap*2
	} else {
		side := min(inner.Dx(), inner.Dy()*32/100)
		art = image.Rect(inner.Min.X, inner.Min.Y, inner.Min.X+side, inner.Min.Y+side)
		col.Min.Y = art.Max.Y + gap
	}
	if m.art != nil {
		drawArt(img, art, m.art)
	}
	bottom, s := drawText(img, col, m, lang, base, gap, rule)
	if s < base*0.99 {
		return nil // squeezed: no flavor text
	}
	// The flavor text goes below the artwork when there is room for two
	// lines, else below the stats.
	fs := max(s*0.62, render.MinSize)
	rect := image.Rect(col.Min.X, bottom+gap, col.Max.X, inner.Max.Y)
	if below := image.Rect(inner.Min.X, art.Max.Y+gap, art.Max.X, inner.Max.Y); col.Min.X > art.Min.X && below.Dy() >= int(fs*1.2)*2 {
		rect = below
	}
	drawFlavor(img, rect, m.flavorText(lang), fs)
	return nil
}

// drawFlavor wraps text into r and cuts it with an ellipsis where the height
// ends.
func drawFlavor(img *image.Paletted, r image.Rectangle, text string, fs float64) {
	lineH := int(fs * 1.2)
	room := r.Dy() / lineH
	if text == "" || room < 1 {
		return
	}
	lines := wrap(text, fs, r.Dx())
	if len(lines) > room {
		lines = lines[:room]
		lines[room-1] = render.Ellipsize(lines[room-1]+" …", fs, false, r.Dx())
	}
	for i, l := range lines {
		render.TextAt(img, l, r.Min.X, r.Min.Y+i*lineH+int(fs), fs, false, color.Black, render.Left)
	}
}

// drawText draws name, number, types, size, stats and the flavor text into
// col and returns the bottom edge and the text size used. When the column is
// too short everything shrinks down to half the base size.
func drawText(img *image.Paletted, col image.Rectangle, m *mon, lang string, base float64, gap, rule int) (bottom int, used float64) {
	// Heights in units of the text size s: name 1.45, info 1.0, six stat rows 0.9.
	// Roomy columns grow the text a little; short ones shrink it.
	s := max(min((float64(col.Dy())-3*float64(gap))/(1.45+1.0+6*0.9), base*1.2), base*0.5)
	y := col.Min.Y

	// Name and number.
	nameSize := s * 1.2
	numSize := s * 0.8
	numW := render.Measure(m.number(), numSize, false)
	render.TextAt(img, m.number(), col.Max.X, y+int(nameSize*1.05), numSize, false, color.Black, render.Right)
	render.Text(img, m.displayName(lang), col.Min.X, y+int(nameSize*1.05), render.Style{Size: nameSize, Bold: true, Shrink: true},
		col.Dx()-numW-gap*2, color.Black, render.Left)
	y += int(s * 1.45)
	render.HLine(img, col.Min.X, y-gap/2-rule/2, col.Max.X, rule)
	y += gap

	// Types and size.
	info := s * 0.75
	types := m.typesText(lang, " · ")
	size := m.sizeText(lang)
	sizeW := render.Measure(size, info, false)
	for sizeW+render.Measure(types, info, true)+gap*2 > col.Dx() && info > s*0.55 {
		info--
		sizeW = render.Measure(size, info, false)
	}
	render.TextAt(img, size, col.Max.X, y+int(info), info, false, color.Black, render.Right)
	render.TextAt(img, render.Ellipsize(types, info, true, col.Dx()-sizeW-gap*2), col.Min.X, y+int(info), info, true, color.Black, render.Left)
	y += int(s * 1.0)

	// Base stats as bars.
	labels := statLabels(lang)
	row := int(s * 0.9)
	ls := s * 0.62
	labelW, valueW := 0, render.Measure("255", ls, false)
	for _, l := range labels {
		labelW = max(labelW, render.Measure(l, ls, false))
	}
	barX0 := col.Min.X + labelW + gap
	barX1 := col.Max.X - valueW - gap
	barH := max(row*55/100, 2*rule+2)
	for i, l := range labels {
		base := y + row*(i+1) - (row-int(ls))/2 - 1
		render.TextAt(img, l, col.Min.X, base, ls, false, color.Black, render.Left)
		render.TextAt(img, strconv.Itoa(m.stats[i]), col.Max.X, base, ls, false, color.Black, render.Right)
		top := y + row*i + (row-barH)/2
		bar := image.Rect(barX0, top, barX1, top+barH)
		drawBar(img, bar, float64(m.stats[i])/255, rule)
	}
	y += row * 6

	return y, s
}

// drawBar draws an outlined bar filled to frac.
func drawBar(img *image.Paletted, r image.Rectangle, frac float64, rule int) {
	if r.Dx() < 4 {
		return
	}
	t := min(rule, r.Dy()/3)
	render.HLine(img, r.Min.X, r.Min.Y, r.Max.X, t)
	render.HLine(img, r.Min.X, r.Max.Y-t, r.Max.X, t)
	render.VLine(img, r.Min.X, r.Min.Y, r.Max.Y, t)
	render.VLine(img, r.Max.X-t, r.Min.Y, r.Max.Y, t)
	frac = min(max(frac, 0), 1)
	inner := r.Inset(t + 1)
	inner.Max.X = inner.Min.X + int(float64(inner.Dx())*frac+0.5)
	if !inner.Empty() {
		render.FillSolid(img, inner)
	}
}

// wrap breaks text into lines of at most maxWidth pixels.
func wrap(text string, size float64, maxWidth int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		try := word
		if line != "" {
			try = line + " " + word
		}
		if line != "" && render.Measure(try, size, false) > maxWidth {
			lines = append(lines, line)
			line = word
			continue
		}
		line = try
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// drawArt scales the artwork into r (keeping its proportions) and reduces it
// to the gray levels of img with error diffusion; a smooth picture would
// otherwise collapse into flat areas at 2 or 4 levels.
func drawArt(img *image.Paletted, r image.Rectangle, art image.Image) {
	sb := art.Bounds()
	if sb.Empty() || r.Empty() {
		return
	}
	w, h := r.Dx(), r.Dy()
	if sb.Dx()*h > sb.Dy()*w {
		h = max(sb.Dy()*w/sb.Dx(), 1)
	} else {
		w = max(sb.Dx()*h/sb.Dy(), 1)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Rect, image.NewUniform(color.White), image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Rect, art, sb, xdraw.Over, nil)
	r = image.Rect(0, 0, w, h).Add(image.Pt(r.Min.X+(r.Dx()-w)/2, r.Min.Y+(r.Dy()-h)/2))
	dither(img, r, dst)
}

// dither writes src into img at r (same size) with Floyd–Steinberg error
// diffusion onto the palette's gray levels.
func dither(img *image.Paletted, r image.Rectangle, src *image.RGBA) {
	levels := render.Levels(img.Palette)
	w, h := r.Dx(), r.Dy()
	lum := make([]float64, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := src.RGBAAt(x, y)
			lum[y*w+x] = 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)
		}
	}
	step := 255.0 / float64(levels-1)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			old := lum[y*w+x]
			idx := int(min(max(old/step+0.5, 0), float64(levels-1)))
			img.SetColorIndex(r.Min.X+x, r.Min.Y+y, uint8(idx))
			e := old - float64(idx)*step
			spread := func(dx, dy int, f float64) {
				if x+dx >= 0 && x+dx < w && y+dy < h {
					lum[(y+dy)*w+x+dx] += e * f
				}
			}
			spread(1, 0, 7.0/16)
			spread(-1, 1, 3.0/16)
			spread(0, 1, 5.0/16)
			spread(1, 1, 1.0/16)
		}
	}
}

// Text is the text variant: name and number, types and size, the stats packed
// into as few lines as fit, then the flavor text.
func (p *pokedex) Text(req *plugin.Request, w plugin.WidgetRequest) ([]string, error) {
	m, err := p.current(w)
	if err != nil {
		return nil, err
	}
	return textLines(m, req.Lang(), w.Width), nil
}

func textLines(m *mon, lang string, width int) []string {
	lines := []string{plugin.Columns(m.displayName(lang), m.number(), width)}
	types, size := m.typesText(lang, "/"), strings.ReplaceAll(m.sizeText(lang), " ", "")
	if len([]rune(types))+len([]rune(size))+1 <= width {
		lines = append(lines, plugin.Columns(types, size, width))
	} else {
		lines = append(lines, types, size)
	}
	labels := statLabels(lang)
	line := ""
	for i, l := range labels {
		item := l + strconv.Itoa(m.stats[i])
		if line != "" && len([]rune(line+" "+item)) > width {
			lines = append(lines, line)
			line = item
			continue
		}
		if line != "" {
			line += " "
		}
		line += item
	}
	lines = append(lines, line)
	if text := m.flavorText(lang); text != "" {
		line = ""
		for _, word := range strings.Fields(text) {
			if line != "" && len([]rune(line+" "+word)) > width {
				lines = append(lines, line)
				line = word
				continue
			}
			if line != "" {
				line += " "
			}
			line += word
		}
		lines = append(lines, line)
	}
	return lines
}
