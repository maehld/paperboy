package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"time"

	"github.com/maehld/paperboy/sdk/design"
	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

// The synodic month and the new moon of 2000-01-06 18:14 UTC, which anchor the
// phase computation.
const (
	synodicMonth = 29.530588853 // days
	newMoonEpoch = 947182440    // unix seconds
	// moonGlyphs is the number of phase glyphs of the font; the first is the
	// new moon.
	moonGlyphs    = 28
	moonGlyphBase = 0xf095
)

// moonPhase returns the position in the synodic month, 0 new to 1 just before
// the next new moon, and the illuminated share of the disc.
func moonPhase(now time.Time) (phase, illumination float64) {
	phase = math.Mod((float64(now.Unix())-newMoonEpoch)/(synodicMonth*86400), 1)
	if phase < 0 {
		phase++
	}
	return phase, (1 - math.Cos(2*math.Pi*phase)) / 2
}

// moonGlyphName names the phase glyph for the icon cache.
func moonGlyphName(i int) string { return fmt.Sprintf("moon-%d", i) }

// moonGlyphIndex chooses one of the 28 glyphs the font carries.
func moonGlyphIndex(phase float64) int {
	i := int(math.Round(phase*moonGlyphs)) % moonGlyphs
	if i < 0 {
		i += moonGlyphs
	}
	return i
}

// moonNames are the eight named phases, in order from the new moon.
var moonNames = [8][2]string{
	{"Neumond", "New moon"},
	{"Zunehmende Sichel", "Waxing crescent"},
	{"Erstes Viertel", "First quarter"},
	{"Zunehmender Mond", "Waxing gibbous"},
	{"Vollmond", "Full moon"},
	{"Abnehmender Mond", "Waning gibbous"},
	{"Letztes Viertel", "Last quarter"},
	{"Abnehmende Sichel", "Waning crescent"},
}

// moonName names the phase in the request language. The eight names are wider
// than the 28 glyphs: the picture follows the nearest 28th of the cycle, the
// name the nearest eighth. A thin crescent just before the new moon therefore
// still reads "New moon", which is what the eighth stands for.
func (w *weather) moonName(phase float64) string {
	i := int(math.Floor(phase*8+0.5)) % 8
	if w.req.Lang() == "de" {
		return moonNames[i][0]
	}
	return moonNames[i][1]
}

// moon draws the phase glyph beside the localised name and the illuminated
// share.
func (w *weather) moon(wr plugin.WidgetRequest, img *image.Paletted) error {
	b := img.Rect.Inset(wr.Style.Padding)
	if b.Empty() {
		return plugin.TooSmall(wr)
	}
	scale := design.NewScale(w.req.TextPx(wr), b.Dy())
	phase, illumination := moonPhase(w.req.Now)

	size := min(b.Dy(), b.Dx()/2)
	gap := max(int(scale.Body*0.18), 2)
	right := image.Rect(b.Min.X+size+gap, b.Min.Y, b.Max.X, b.Max.Y)
	if size < 8 || right.Dx() < 8 {
		return plugin.TooSmall(wr)
	}
	glyph := image.Rect(b.Min.X, b.Min.Y+(b.Dy()-size)/2, b.Min.X+size, b.Min.Y+(b.Dy()-size)/2+size)
	icon(moonGlyphName(moonGlyphIndex(phase))).Draw(img, glyph, size*85/100, render.Center)

	nameSize, shareSize := max(scale.Title, render.MinSize), max(scale.Body, render.MinSize)
	lineGap := max(int(shareSize*0.18), 2)
	top := b.Min.Y + (b.Dy()-(int(nameSize+shareSize)+lineGap))/2
	render.Text(img, w.moonName(phase), right.Min.X+right.Dx()/2, top+int(nameSize*0.8), render.Style{
		Size: nameSize, Bold: true, Shrink: true,
	}, right.Dx(), color.Black, render.Center)
	render.Text(img, w.num(illumination*100, 0)+" %", right.Min.X+right.Dx()/2,
		top+int(nameSize)+lineGap+int(shareSize*0.8), render.Style{Size: shareSize},
		right.Dx(), color.Black, render.Center)
	return nil
}
