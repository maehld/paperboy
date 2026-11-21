package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"slices"
	"strconv"
	"strings"

	xdraw "golang.org/x/image/draw"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
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

	stats := w.Arg().Bool("stats", true)
	flavor := w.Arg().Bool("flavor", true)
	if !stats {
		// Without the stat bars the header is two short rows: a wide cell puts
		// the artwork beside them at full height, else it stacks.
		if besideFits(inner, m, lang, base, gap) {
			return drawBeside(m, lang, img, inner, base, gap, rule, flavor)
		}
		return drawStacked(m, lang, img, inner, base, gap, rule, flavor)
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
	bottom, s := drawText(img, col, m, lang, base, gap, rule, stats, 1.2)
	if !flavor || s < base*0.99 {
		return nil // switched off or squeezed: no flavor text
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

// drawStacked draws the card without the stats on a tall cell: the header
// spans the full width, the artwork follows underneath, centered, and the
// flavor text closes the card. The header never grows beyond the base text
// size, so it does not eat the room the artwork gains.
func drawStacked(m *mon, lang string, img *image.Paletted, inner image.Rectangle, base float64, gap, rule int, flavor bool) error {
	bottom, s := drawText(img, inner, m, lang, base, gap, rule, false, 1)
	free := image.Rect(inner.Min.X, bottom+gap, inner.Max.X, inner.Max.Y)
	if flavor {
		// The flavor text keeps room at the bottom, but the artwork stays the
		// main thing: on a short cell it gives back a line.
		fs := max(s*0.62, render.MinSize)
		lineH := int(fs * 1.2)
		room := min(2, (free.Dy()-gap)/lineH)
		if room > 1 && free.Dy()-room*lineH-gap < inner.Dy()*2/5 {
			room = 1
		}
		if room > 0 && free.Dy()-room*lineH-gap >= 24 {
			free.Max.Y -= room*lineH + gap
		}
	}
	side := min(free.Dx(), free.Dy())
	if side >= 24 && m.art != nil {
		x := free.Min.X + (free.Dx()-side)/2
		drawArt(img, image.Rect(x, free.Min.Y, x+side, free.Min.Y+side), m.art)
	}
	if flavor {
		fs := max(s*0.62, render.MinSize)
		drawFlavor(img, image.Rect(inner.Min.X, free.Max.Y+gap, inner.Max.X, inner.Max.Y), m.flavorText(lang), fs)
	}
	return nil
}

// besideFits tells whether the artwork can fill the cell's height beside a
// column that still shows the types and the size at a readable size. The
// column gets what the square artwork leaves, so a cell that is only a little
// wider than tall stacks instead.
func besideFits(inner image.Rectangle, m *mon, lang string, base float64, gap int) bool {
	if inner.Dx() < inner.Dy()*11/10 {
		return false
	}
	col := inner.Dx() - inner.Dy() - gap*2
	if col < 1 {
		return false
	}
	s := textSize(inner.Dy(), base, gap, 0, 1)
	_, rows := infoRows(col, m, lang, s, gap)
	return rows > 0
}

// drawBeside draws the card without the stats on a wide cell: the artwork
// fills the cell's height on the left, the header and the flavor text take the
// column on the right. The text block sits in the middle of the column when it
// does not fill it, so a card without the flavor text does not look like an
// empty half.
func drawBeside(m *mon, lang string, img *image.Paletted, inner image.Rectangle, base float64, gap, rule int, flavor bool) error {
	side := inner.Dy()
	art := image.Rect(inner.Min.X, inner.Min.Y, inner.Min.X+side, inner.Min.Y+side)
	col := image.Rect(art.Max.X+gap*2, inner.Min.Y, inner.Max.X, inner.Max.Y)

	s := textSize(col.Dy(), base, gap, 0, 1)
	_, rows := infoRows(col.Dx(), m, lang, s, gap)
	head := headerHeight(s, gap, rows == 2)
	block, lines := head, 0
	fs := max(s*0.62, render.MinSize)
	if flavor {
		lineH := int(fs * 1.2)
		lines = min(len(wrap(m.flavorText(lang), fs, col.Dx())), max((col.Dy()-head-gap)/lineH, 0))
		if lines > 0 {
			block += gap + lines*lineH
		}
	}
	top := col.Min.Y + max((col.Dy()-block)/2, 0)
	drawHeader(img, image.Rect(col.Min.X, top, col.Max.X, top+block), m, lang, s, gap, rule, true)
	if lines > 0 {
		drawFlavor(img, image.Rect(col.Min.X, top+head+gap, col.Max.X, top+block), m.flavorText(lang), fs)
	}
	drawArt(img, art, m.art)
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

// Header rows in units of the text size: the name row and the info row.
const (
	nameRow = 1.45
	infoRow = 1.0
)

// textSize is the text size of a column of the given height with room for rows
// more rows: a roomy column grows a little, a short one shrinks, and grow caps
// the size relative to the base text.
func textSize(height int, base float64, gap int, rows, grow float64) float64 {
	return max(min((float64(height)-3*float64(gap))/(nameRow+infoRow+rows), base*grow), base*0.5)
}

// headerHeight is the height drawHeader needs for the text size s, with one or
// two rows for the types and the size.
func headerHeight(s float64, gap int, twoRows bool) int {
	h := int(s*nameRow) + gap + int(s*infoRow)
	if twoRows {
		h += int(s * infoRow)
	}
	return h
}

// infoRows is the size of the types and the size in a column of the given
// width and how many rows they need: one row when both fit next to each other,
// two rows when the column only holds one of them, and 0 when it holds
// neither. The size shrinks down to 0.55 of the text size.
func infoRows(width int, m *mon, lang string, s float64, gap int) (info float64, rows int) {
	info = s * 0.75
	types, dims := m.typesText(lang, " · "), m.sizeText(lang)
	measure := func() (int, int) { return render.Measure(dims, info, false), render.Measure(types, info, true) }
	sizeW, typesW := measure()
	for sizeW+typesW+gap*2 > width && info > s*0.55 {
		info--
		sizeW, typesW = measure()
	}
	switch {
	case sizeW+typesW+gap*2 <= width:
		return info, 1
	case max(sizeW, typesW) <= width:
		return info, 2
	}
	return info, 0
}

// drawHeader draws name, number, types and size into col and returns the
// bottom edge. twoRows lets a narrow column put the size below the types.
func drawHeader(img *image.Paletted, col image.Rectangle, m *mon, lang string, s float64, gap, rule int, twoRows bool) int {
	// Name and number.
	nameSize := s * 1.2
	numSize := s * 0.8
	numW := render.Measure(m.number(), numSize, false)
	render.TextAt(img, m.number(), col.Max.X, col.Min.Y+int(nameSize*1.05), numSize, false, color.Black, render.Right)
	render.Text(img, m.displayName(lang), col.Min.X, col.Min.Y+int(nameSize*1.05), render.Style{Size: nameSize, Bold: true, Shrink: true},
		col.Dx()-numW-gap*2, color.Black, render.Left)
	y := col.Min.Y + int(s*nameRow)
	render.HLine(img, col.Min.X, y-gap/2-rule/2, col.Max.X, rule)
	y += gap

	// Types and size.
	info, rows := infoRows(col.Dx(), m, lang, s, gap)
	if rows == 2 && !twoRows {
		rows = 1 // one row that ellipsizes, as before without the room for two
	}
	types, size := m.typesText(lang, " · "), m.sizeText(lang)
	switch rows {
	case 2:
		render.TextAt(img, render.Ellipsize(types, info, true, col.Dx()), col.Min.X, y+int(info), info, true, color.Black, render.Left)
		y += int(s * infoRow)
		render.TextAt(img, size, col.Max.X, y+int(info), info, false, color.Black, render.Right)
	default:
		sizeW := render.Measure(size, info, false)
		render.TextAt(img, size, col.Max.X, y+int(info), info, false, color.Black, render.Right)
		render.TextAt(img, render.Ellipsize(types, info, true, col.Dx()-sizeW-gap*2), col.Min.X, y+int(info), info, true, color.Black, render.Left)
	}
	return y + int(s*infoRow)
}

// drawText draws the header and, when stats is set, the six base stats into
// col. It returns the bottom edge and the text size used. When the column is
// too short everything shrinks down to half the base size.
func drawText(img *image.Paletted, col image.Rectangle, m *mon, lang string, base float64, gap, rule int, stats bool, grow float64) (bottom int, used float64) {
	// Heights in units of the text size s: name 1.45, info 1.0, six stat rows
	// 0.9. Roomy columns grow the text a little; short ones shrink it; without
	// the stats their rows are free.
	statsRows := 6 * 0.9
	if !stats {
		statsRows = 0
	}
	s := textSize(col.Dy(), base, gap, statsRows, grow)
	y := drawHeader(img, col, m, lang, s, gap, rule, false)

	if !stats {
		return y, s
	}
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

// Values answers the declared values of a text page for the Pokémon of the
// seed argument (ADR 0111, 0120).
func (p *pokedex) Values(req *plugin.Request) (map[string]plugin.Value, error) {
	if err := req.ValueArgs.Check(valueArgs); err != nil {
		return nil, err
	}
	m, err := p.current(plugin.WidgetRequest{Widget: "card", Args: req.ValueArgs})
	if err != nil {
		return nil, err
	}
	lang := req.Lang()
	stats := [6]string{"hp", "attack", "defense", "special-attack", "special-defense", "speed"}
	out := make(map[string]plugin.Value, len(req.Values))
	for _, name := range req.Values {
		switch name {
		case "name":
			out[name] = plugin.TextValue(m.displayName(lang))
		case "number":
			out[name] = plugin.NumberValue(float64(m.id))
		case "types":
			out[name] = plugin.TextValue(m.typesText(lang, "/"))
		case "height":
			out[name] = plugin.NumberValue(float64(m.height) / 10)
		case "weight":
			out[name] = plugin.NumberValue(float64(m.weight) / 10)
		case "flavor":
			out[name] = plugin.TextValue(m.flavorText(lang))
		default:
			i := slices.Index(stats[:], name)
			if i < 0 {
				return nil, fmt.Errorf("%w %q", plugin.ErrUnknownValue, name)
			}
			out[name] = plugin.NumberValue(float64(m.stats[i]))
		}
	}
	return out, nil
}
