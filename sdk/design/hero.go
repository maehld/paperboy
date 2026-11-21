package design

import (
	"image"
	"math"

	"github.com/maehld/paperboy/sdk/render"
)

// Hero is one large value with its context: a label above, a caption and
// details below, an optional icon and an optional meter. Details fall from the
// bottom when the cell is short; the value stays.
type Hero struct {
	Label   string
	Value   string
	Unit    string
	Caption string
	Details []string
	// Note is one emphasized line below the details, for a warning or the
	// next event.
	Note  string
	Icon  *render.Mask
	Meter *Meter
	Align render.Align
	Rule  int
}

// heroPlan is the resolved layout of a hero.
type heroPlan struct {
	s         Scale
	details   int
	showCap   bool
	showNote  bool
	labelH    int
	valueH    int
	capH      int
	detailH   int
	noteH     int
	meterH    int
	gapLabel  int
	gapValue  int
	gapDetail int
	gapMeter  int
	total     int
	top       int
	rule      int
}

// DrawHero draws a hero element into r. It returns false when r has no room
// for the value and its label.
func DrawHero(dst *image.Paletted, r image.Rectangle, h Hero, s Scale) bool {
	if r.Empty() {
		return false
	}
	textR := r
	if h.Icon != nil {
		iconW := min(r.Dy(), r.Dx()/3)
		if iconW >= render.MinSize {
			h.Icon.Draw(dst, image.Rect(r.Min.X, r.Min.Y, r.Min.X+iconW, r.Max.Y),
				int(math.Round(float64(iconW)*0.85)), render.Center)
			textR.Min.X += iconW + gap(s.Body)
		}
	}
	if textR.Empty() {
		return false
	}
	for n := len(h.Details); n >= 0; n-- {
		if p, ok := planHero(h, textR, s, n, h.Caption != "", h.Note != ""); ok {
			return drawHero(dst, textR, h, p)
		}
	}
	if h.Caption != "" {
		if p, ok := planHero(h, textR, s, 0, false, h.Note != ""); ok {
			return drawHero(dst, textR, h, p)
		}
	}
	if h.Note != "" {
		if p, ok := planHero(h, textR, s, 0, false, false); ok {
			return drawHero(dst, textR, h, p)
		}
	}
	return false
}

// planHero resolves a hero with the given number of details.
func planHero(h Hero, r image.Rectangle, s Scale, details int, caption, note bool) (heroPlan, bool) {
	details = max(min(details, len(h.Details)), 0)
	caption = caption && h.Caption != ""
	note = note && h.Note != ""
	rule := h.Rule
	if rule < 1 {
		rule = 1
	}
	layout := func(s Scale) (heroPlan, int) {
		p := heroPlan{s: s, details: details, showCap: caption, showNote: note, rule: rule}
		p.gapLabel = gap(s.Detail)
		p.gapValue = gap(s.Hero)
		p.gapDetail = max(int(math.Round(s.Detail*0.12)), 1)
		p.gapMeter = gap(s.Detail)
		if h.Label != "" {
			p.labelH = lineHeight(s.Detail)
		}
		if h.Value != "" {
			p.valueH = int(math.Round(s.Hero * 1.05))
		}
		if caption {
			p.capH = lineHeight(s.Detail)
		}
		if details > 0 {
			p.detailH = lineHeight(s.Detail)
		}
		if note {
			p.noteH = lineHeight(s.Detail)
		}
		if h.Meter != nil {
			p.meterH = max(int(math.Round(s.Body*0.35)), 4)
		}
		add := func(height, space int) {
			if height <= 0 {
				return
			}
			if p.total > 0 {
				p.total += space
			}
			p.total += height
		}
		add(p.labelH, 0)
		add(p.valueH, p.gapLabel)
		add(p.capH, p.gapValue)
		for i := 0; i < details; i++ {
			space := p.gapDetail
			if i == 0 {
				space = p.gapValue
			}
			add(p.detailH, space)
		}
		add(p.noteH, p.gapDetail*2)
		add(p.meterH, p.gapMeter)
		return p, p.total
	}
	p, total := layout(s)
	if total > r.Dy() {
		f := float64(r.Dy()) / float64(total)
		// Drop content before the type falls below 80% of its role. A
		// shorter hero reads better than a page of tiny details.
		if f < 0.8 && (details > 0 || caption || note) {
			return heroPlan{}, false
		}
		p, total = layout(s.Scaled(f))
	}
	if total <= 0 || total > r.Dy() || p.s.Hero < render.MinSize {
		return heroPlan{}, false
	}
	p.top = r.Min.Y + (r.Dy()-total)/2
	return p, true
}

// drawHero draws a resolved hero.
func drawHero(dst *image.Paletted, r image.Rectangle, h Hero, p heroPlan) bool {
	y := p.top
	if p.labelH > 0 {
		drawLine(dst, h.Label, r, y, p.s.Detail, true, render.Black, h.Align)
		y += p.labelH
	}
	if p.valueH > 0 {
		if p.labelH > 0 {
			y += p.gapLabel
		}
		drawValue(dst, r, y, p.valueH, h.Value, h.Unit, p.s.Hero, p.s.Detail,
			true, render.Black, h.Align, 0)
		y += p.valueH
	}
	if p.showCap {
		y += p.gapValue
		drawLine(dst, h.Caption, r, y, p.s.Detail, false, render.Black, h.Align)
		y += p.capH
	}
	for i := 0; i < p.details; i++ {
		if i == 0 {
			y += p.gapValue
		} else {
			y += p.gapDetail
		}
		drawLine(dst, h.Details[i], r, y, p.s.Detail, false, Muted(dst), h.Align)
		y += p.detailH
	}
	if p.showNote {
		y += p.gapDetail * 2
		drawLine(dst, h.Note, r, y, p.s.Detail, true, render.Black, h.Align)
		y += p.noteH
	}
	if p.meterH > 0 && h.Meter != nil {
		y += p.gapMeter
		DrawMeter(dst, image.Rect(r.Min.X, y, r.Max.X, y+p.meterH), *h.Meter, p.rule)
	}
	return true
}
