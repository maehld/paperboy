package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"
	"time"

	"github.com/maehld/paperboy/sdk/design"
	"github.com/maehld/paperboy/sdk/format"
	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
	xdraw "golang.org/x/image/draw"
)

// drawCharacter draws the character card: the render on the left, name, level,
// class and spec beside it, and the item level as the hero value.
func drawCharacter(img *image.Paletted, r image.Rectangle, req *plugin.Request, w plugin.WidgetRequest, c *character) bool {
	if r.Dx() < 160 || r.Dy() < 120 {
		return false
	}
	s := design.NewScale(req.TextPx(w), r.Dy())
	muted := design.Muted(img)

	col := r
	side := 0
	if c.portrait != nil {
		side = min(int(float64(r.Dy())*portraitShare), r.Dx()*45/100)
	}
	if side >= 24 {
		art := image.Rect(r.Min.X, r.Min.Y, r.Min.X+side, r.Max.Y)
		drawArt(img, art, c.portrait)
		col = image.Rect(art.Max.X+w.Style.Padding, r.Min.Y, r.Max.X, r.Max.Y)
	}
	if col.Dx() < 60 {
		// Only the render fits; the card stays a picture.
		return side > 0
	}

	baseline := col.Min.Y + int(math.Round(s.Title*0.9))
	drawLine(img, c.display(), col, baseline, s.Title, true, render.Black)
	y := baseline + int(math.Round(s.Tiny*0.55))

	var meta []string
	if c.level > 0 && c.class != "" {
		meta = append(meta, fmt.Sprintf("%s %d · %s", req.T("Stufe", "Level"), c.level, c.class))
	}
	var sub []string
	if c.spec != "" {
		sub = append(sub, c.spec)
	}
	if c.guild != "" {
		sub = append(sub, c.guild)
	}
	for _, line := range [][]string{meta, sub} {
		if len(line) == 0 {
			continue
		}
		y += int(math.Round(s.Tiny * 1.15))
		drawLine(img, strings.Join(line, " · "), col, y, s.Tiny, false, muted)
	}
	y += int(math.Round(s.Tiny * 0.4))

	// The hero value takes what the header left.
	if y+int(s.Hero*0.6) > col.Max.Y {
		return true
	}
	var details []string
	if c.achievements > 0 && col.Dx() >= 200 {
		details = append(details, fmt.Sprintf("%s %s", req.T("Erfolge", "Achievements"),
			format.Number(float64(c.achievements), 0, req.Lang())))
	}
	if d := dateText(c.lastLogin, req); d != "" && col.Dx() >= 240 {
		details = append(details, req.T("Zuletzt ", "Last seen ")+d)
	}

	label := req.T("Gegenstandsstufe", "Item level")
	value := "—"
	if c.itemLevel > 0 {
		value = format.Number(c.itemLevel, 0, req.Lang())
		label = req.T("Itemlevel", "Item level")
	}
	rule := w.Style.Rule
	if rule < 1 {
		rule = 1
	}
	render.HLine(img, col.Min.X, y, col.Max.X, rule)
	hero := image.Rect(col.Min.X, y+rule+w.Style.Padding/2, col.Max.X, col.Max.Y)
	design.DrawHero(img, hero, design.Hero{Label: label, Value: value, Details: details, Rule: rule}, s)
	return true
}

// portraitShare is the share of the cell height the render may take.
const portraitShare = 0.55

// drawMythic draws the Mythic+ score as the hero value with the best run.
func drawMythic(img *image.Paletted, r image.Rectangle, req *plugin.Request, w plugin.WidgetRequest, c *character) bool {
	if r.Dx() < 110 || r.Dy() < 80 {
		return false
	}
	s := design.NewScale(req.TextPx(w), r.Dy())
	hero := design.Hero{Label: "Mythic+", Value: "—", Rule: max(w.Style.Rule, 1)}
	if c.score > 0 {
		hero.Value = format.Number(c.score, 0, req.Lang())
		hero.Caption = c.bestRun
	} else {
		hero.Caption = req.T("Keine Wertung", "No score")
	}
	return design.DrawHero(img, r, hero, s)
}

// drawRaid draws the current tier and the progress per difficulty.
func drawRaid(img *image.Paletted, r image.Rectangle, req *plugin.Request, w plugin.WidgetRequest, c *character) bool {
	if r.Dx() < 110 || r.Dy() < 80 {
		return false
	}
	s := design.NewScale(req.TextPx(w), r.Dy())
	if c.raid == "" || len(c.tiers) == 0 {
		return design.Notice(img, r, req.T("Kein Raid-Fortschritt", "No raid progress"), s)
	}

	y := r.Min.Y + int(math.Round(s.Micro*0.9))
	render.Text(img, req.T("Raid", "Raid"), r.Min.X, y, render.Style{Size: s.Micro}, r.Dx(), design.Muted(img), render.Left)
	y += int(math.Round(s.Micro * 0.35))
	drawLine(img, c.raid, r, y+int(math.Round(s.Detail*0.9)), s.Detail, true, render.Black)
	top := y + int(math.Round(s.Detail*1.15))

	if r.Max.Y-top < len(c.tiers)*int(s.Tiny) {
		// A short cell keeps the tier name only.
		return true
	}
	rowH := (r.Max.Y - top) / len(c.tiers)
	rule := max(w.Style.Rule, 1)
	muted := design.Muted(img)
	for i, d := range c.tiers {
		row := image.Rect(r.Min.X, top+i*rowH, r.Max.X, top+(i+1)*rowH)
		if i > 0 {
			render.HLine(img, row.Min.X, row.Min.Y, row.Max.X, rule)
		}
		baseline := row.Min.Y + rowH/2 + int(math.Round(s.Body*0.35))
		render.Text(img, difficultyName(req, d.key), row.Min.X, baseline, render.Style{Size: s.Tiny}, row.Dx()/2, muted, render.Left)
		value := fmt.Sprintf("%d/%d", d.killed, d.total)
		render.Text(img, value, row.Max.X, baseline, render.Style{Size: s.Title, Bold: true}, row.Dx()/2, render.Black, render.Right)
	}
	return true
}

// difficultyName names a raid difficulty in the request language.
func difficultyName(req *plugin.Request, key string) string {
	switch key {
	case "normal":
		return req.T("Normal", "Normal")
	case "heroic":
		return req.T("Heroisch", "Heroic")
	case "mythic":
		return req.T("Mythisch", "Mythic")
	}
	return key
}

// drawGear lists the equipped items: the slot, rarity and origin above, the
// item's name and item level below.
func drawGear(img *image.Paletted, r image.Rectangle, req *plugin.Request, w plugin.WidgetRequest, c *character) bool {
	if r.Dx() < 160 || r.Dy() < 90 {
		return false
	}
	s := design.NewScale(req.TextPx(w), r.Dy())
	if len(c.gear) == 0 {
		return design.Notice(img, r, req.T("Ohne Blizzard-Zugang keine Ausrüstung", "No gear without Blizzard credentials"), s)
	}

	cols := 1
	if r.Dx() >= 400 {
		cols = 2
	}
	rowH := int(math.Round(s.Tiny*1.15)) + int(math.Round(s.Detail*1.15)) + w.Style.Padding
	rows := r.Dy() / max(rowH, 1)
	if rows < 1 {
		return false
	}
	n := min(w.Arg().Int("count", 10), len(c.gear), rows*cols)
	if n < 1 {
		return false
	}
	rows = (n + cols - 1) / cols
	cw := r.Dx() / cols
	rh := min(r.Dy()/rows, int(float64(rowH)*1.8))
	rule := max(w.Style.Rule, 1)
	muted := design.Muted(img)

	for i := 0; i < n; i++ {
		it := c.gear[i]
		x := r.Min.X + (i/rows)*cw
		y := r.Min.Y + (i%rows)*rh
		cell := image.Rect(x, y, x+cw-w.Style.Padding*2, y+rh)

		meta := it.name
		for _, part := range []string{it.quality, it.origin} {
			if part != "" {
				meta += " · " + part
			}
		}
		drawLine(img, meta, cell, cell.Min.Y+int(math.Round(s.Tiny*0.85)), s.Tiny, false, muted)

		level := ""
		if it.level > 0 {
			level = format.Number(float64(it.level), 0, req.Lang())
		}
		levelW := 0
		if level != "" {
			levelW = render.Measure(level, s.Title, true) + w.Style.Padding
		}
		top := cell.Min.Y + int(math.Round(s.Tiny*1.15))
		drawLine(img, it.item, image.Rect(cell.Min.X, cell.Min.Y, cell.Max.X-levelW, cell.Max.Y),
			top+int(math.Round(s.Detail*0.9)), s.Detail, false, render.Black)
		if level != "" {
			render.Text(img, level, cell.Max.X, cell.Min.Y+rh/2+int(math.Round(s.Title*0.35)),
				render.Style{Size: s.Title, Bold: true}, cell.Dx(), render.Black, render.Right)
		}
		if i%rows > 0 {
			render.HLine(img, cell.Min.X, y, cell.Max.X+w.Style.Padding, rule)
		}
	}
	return true
}

// drawLine draws one ellipsized line of text, top-aligned at baseline.
func drawLine(img *image.Paletted, text string, r image.Rectangle, baseline int, size float64, bold bool, c color.Color) {
	render.Text(img, render.Ellipsize(text, size, bold, r.Dx()), r.Min.X, baseline,
		render.Style{Size: size, Bold: bold}, r.Dx(), c, render.Left)
}

// dateText formats a day in the request language.
func dateText(t time.Time, req *plugin.Request) string {
	if t.IsZero() {
		return ""
	}
	t = t.In(req.Location())
	if req.Lang() == "de" {
		return t.Format("02.01.2006")
	}
	return t.Format("02 Jan 2006")
}

// drawArt scales the render into r and reduces it to the gray levels of img
// with error diffusion; flat areas would otherwise lose the character.
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
	dither(img, image.Rect(0, 0, w, h).Add(image.Pt(r.Min.X+(r.Dx()-w)/2, r.Min.Y+(r.Dy()-h)/2)), dst)
}

// dither writes src into img at r with Floyd–Steinberg error diffusion onto
// the palette's gray levels.
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
				if x+dx >= 0 && x+dx < w && y+dy >= 0 && y+dy < h {
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
