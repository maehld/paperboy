package main

import (
	"image"
	"math"

	"github.com/maehld/paperboy/sdk/design"
	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

// top is the inner layer of the clothing advice.
type top int

const (
	topTShirt top = iota
	topLongSleeve
	topPullover
)

// legs is the lower layer.
type legs int

const (
	legsLong legs = iota
	legsShorts
)

// outer is the outermost layer; outerNone means no jacket.
type outer int

const (
	outerNone outer = iota
	outerLightJacket
	outerJacket
	outerCoat
	outerRainJacket
)

// clothingAdvice is the outfit for the day: the three layers, the accessories
// and the extras that need no pictogram.
type clothingAdvice struct {
	Top                top
	Legs               legs
	Outer              outer
	Thermals           bool
	Rain, Snow         bool
	Wind, Gale         bool
	Heat               bool
	Scarf, Hat, Gloves bool
}

// clothingOf applies the clothing rules to the day. It uses the apparent
// temperature, because it already includes wind and humidity. The top and the
// trousers follow the daily maximum, the outer layer and the accessories the
// daily minimum, so a day with a wide range becomes a layered advice.
func clothingOf(t today) clothingAdvice {
	var a clothingAdvice
	switch {
	case t.ApparentMax >= 25:
		a.Top = topTShirt
	case t.ApparentMax >= 18:
		a.Top = topLongSleeve
	case t.ApparentMax >= 10:
		a.Top = topPullover
	default:
		a.Top, a.Thermals = topPullover, true
	}
	a.Legs = legsLong
	if t.ApparentMax >= 24 {
		a.Legs = legsShorts
	}
	switch {
	case t.ApparentMin >= 16:
		a.Outer = outerNone
	case t.ApparentMin >= 9:
		a.Outer = outerLightJacket
	case t.ApparentMin >= 1:
		a.Outer = outerJacket
	default:
		a.Outer = outerCoat
	}
	a.Rain = (t.PrecipProb != nil && *t.PrecipProb >= 50) || t.Precip >= 1
	a.Snow = t.Snow > 0
	a.Wind = t.Gusts >= 6
	a.Gale = t.Gusts >= 8
	a.Heat = t.ApparentMax >= 28 && (t.Code == 0 || t.Code == 1)
	a.Scarf = t.ApparentMin < 8
	a.Hat = t.ApparentMin < 5
	a.Gloves = t.ApparentMin < 0
	// A rain jacket replaces a lighter outer layer. A winter coat stays, and
	// the advice names the rain jacket as an extra layer.
	if a.Rain && a.Outer != outerCoat {
		a.Outer = outerRainJacket
	}
	return a
}

// garmentTop names the inner layer.
func (w *weather) garmentTop(t top) string {
	switch t {
	case topTShirt:
		return w.t("T-Shirt", "T-shirt")
	case topLongSleeve:
		return w.t("Langarmshirt", "long-sleeve shirt")
	}
	return w.t("Pullover", "pullover")
}

// garmentOuter names the outer layer.
func (w *weather) garmentOuter(o outer) string {
	switch o {
	case outerLightJacket:
		return w.t("leichte Jacke", "light jacket")
	case outerJacket:
		return w.t("Jacke", "jacket")
	case outerCoat:
		return w.t("Wintermantel", "winter coat")
	case outerRainJacket:
		return w.t("Regenjacke", "rain jacket")
	}
	return ""
}

// clothingDetails names the extras that the outfit strip cannot show.
func (w *weather) clothingDetails(a clothingAdvice) []string {
	var details []string
	if a.Rain && a.Outer == outerCoat {
		details = append(details, w.t("Regenjacke", "rain jacket"))
	}
	if a.Snow {
		details = append(details, w.t("wasserdichte Schuhe", "waterproof shoes"))
	}
	switch {
	case a.Gale:
		details = append(details, w.t("kein Schirm", "no umbrella"))
	case a.Wind:
		details = append(details, w.t("winddicht", "windproof"))
	}
	if a.Thermals {
		details = append(details, w.t("Thermowäsche", "thermal layer"))
	}
	if a.Heat {
		details = append(details, w.t("Sonnenschutz", "sun protection"))
	}
	return details
}

// outfitIcons lists the pictograms of the outfit in the order you dress: the
// top, the trousers, the outer layer and the accessories that the day needs.
func outfitIcons(a clothingAdvice) []string {
	icons := []string{"tshirt"}
	if a.Top != topTShirt {
		icons[0] = "pullover"
	}
	if a.Legs == legsShorts {
		icons = append(icons, "shorts")
	} else {
		icons = append(icons, "trousers")
	}
	if a.Outer != outerNone {
		icons = append(icons, "jacket")
	}
	if a.Scarf {
		icons = append(icons, "scarf")
	}
	if a.Hat {
		icons = append(icons, "hat")
	}
	if a.Gloves {
		icons = append(icons, "gloves")
	}
	return icons
}

// clothing draws the advice for today: the outermost garment as the value, the
// felt range as the caption and the outfit as a row of pictograms. A wide cell
// puts the row beside the text, a tall one below it.
func (w *weather) clothing(wr plugin.WidgetRequest, img *image.Paletted) error {
	b := img.Rect.Inset(wr.Style.Padding)
	if b.Empty() {
		return plugin.TooSmall(wr)
	}
	t := w.cur.Today
	a := clothingOf(t)
	value := w.garmentOuter(a.Outer)
	if a.Outer == outerNone {
		value = w.garmentTop(a.Top)
	}
	hero := design.Hero{
		Label:   w.t("Kleidung heute", "Clothing today"),
		Value:   value,
		Caption: w.t("gefühlt ", "feels ") + w.temp(t.ApparentMin) + "–" + w.temp(t.ApparentMax),
		Details: w.clothingDetails(a),
	}
	icons := outfitIcons(a)
	textPx := w.req.TextPx(wr)
	gap := max(int(math.Round(textPx*0.35)), 4)
	iconH := int(math.Round(textPx * 2))
	if hr, sr, h, ok := splitOutfit(b, len(icons), iconH, gap); ok {
		if design.DrawHero(img, hr, hero, design.NewScale(textPx, hr.Dy())) {
			w.drawOutfit(img, sr, icons, h, gap)
			return nil
		}
	}
	// A cell that has room for the text but not for the strip keeps the text.
	if !design.DrawHero(img, b, hero, design.NewScale(textPx, b.Dy())) {
		return plugin.TooSmall(wr)
	}
	return nil
}

// splitOutfit divides the cell between the text and the outfit strip and
// returns the height of one pictogram. A cell at least three times as wide as
// high puts the strip on the right, every other cell below the text.
func splitOutfit(b image.Rectangle, n, iconH, gap int) (hero, strip image.Rectangle, h int, ok bool) {
	if n < 1 || iconH < render.MinSize {
		return hero, strip, 0, false
	}
	if b.Dx() >= 3*b.Dy() {
		h = min(iconH, b.Dy()*3/4, (b.Dx()/2-(n-1)*gap)/n)
		if h < render.MinSize {
			return hero, strip, 0, false
		}
		width := n*h + (n-1)*gap
		strip = image.Rect(b.Max.X-width, b.Min.Y, b.Max.X, b.Max.Y)
		hero = image.Rect(b.Min.X, b.Min.Y, strip.Min.X-gap, b.Max.Y)
		return hero, strip, h, hero.Dx() > 0
	}
	h = min(iconH, b.Dy()/3, (b.Dx()-(n-1)*gap)/n)
	if h < render.MinSize {
		return hero, strip, 0, false
	}
	strip = image.Rect(b.Min.X, b.Max.Y-h, b.Max.X, b.Max.Y)
	hero = image.Rect(b.Min.X, b.Min.Y, b.Max.X, strip.Min.Y-gap)
	return hero, strip, h, hero.Dy() > 0
}

// drawOutfit draws the pictograms side by side and centered in r.
func (w *weather) drawOutfit(dst *image.Paletted, r image.Rectangle, icons []string, iconH, gap int) {
	if r.Empty() || iconH < render.MinSize || len(icons) == 0 {
		return
	}
	x := r.Min.X + (r.Dx()-(len(icons)*iconH+(len(icons)-1)*gap))/2
	for _, name := range icons {
		if m := icon(name); m != nil {
			m.Draw(dst, image.Rect(x, r.Min.Y, x+iconH, r.Max.Y), iconH*85/100, render.Center)
		}
		x += iconH + gap
	}
}
