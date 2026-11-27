// The child-friendly smileys of the dexcom widgets. A happy face means the
// reading is in the range, a sad face means it is above or below it, and the
// urgent low adds an exclamation mark. Old data keeps the happy face in gray.

package main

import (
	"image"
	"image/color"
	"math"
)

const (
	// signStale is the state of a missing or old reading. The other states are
	// the level strings of level(): "", "low", "high" and "urgent".
	signStale = "stale"

	// minSign is the smallest smiley that stays readable. The glucose widget
	// drops the sign below it and keeps the value.
	minSign = 24
)

// signState maps the reading to the sign. A missing or stale reading wins over
// the level, so the sign never shows a current value that the page no longer
// knows (ADR 0127).
func signState(lvl string, has, stale bool) string {
	if !has || stale {
		return signStale
	}
	return lvl
}

// ---- shapes ----

// A sign is a solid pictogram. The plugin draws it from geometry instead of a
// PNG mask, so it stays sharp at every ppi and needs no asset per size. The
// rasterizer samples each pixel ss × ss times and sets it at half coverage.
const signSamples = 6

type signShape interface {
	contains(x, y float64) bool
	// bounds returns the unit box that can hold ink. The rasterizer visits
	// only this part of the target rectangle.
	bounds() (x0, y0, x1, y1 float64)
}

type disc struct{ x, y, r float64 }

func (d disc) contains(x, y float64) bool {
	dx, dy := x-d.x, y-d.y
	return dx*dx+dy*dy <= d.r*d.r
}

func (d disc) bounds() (float64, float64, float64, float64) {
	return d.x - d.r, d.y - d.r, d.x + d.r, d.y + d.r
}

type ring struct{ x, y, r, w float64 }

func (r ring) contains(x, y float64) bool {
	return disc{r.x, r.y, r.r}.contains(x, y) && !disc{r.x, r.y, r.r - r.w}.contains(x, y)
}

func (r ring) bounds() (float64, float64, float64, float64) {
	return r.x - r.r, r.y - r.r, r.x + r.r, r.y + r.r
}

type stroke struct{ x0, y0, x1, y1, w float64 }

func (s stroke) contains(x, y float64) bool {
	dx, dy := s.x1-s.x0, s.y1-s.y0
	l := dx*dx + dy*dy
	t := 0.0
	if l > 0 {
		t = math.Max(0, math.Min(1, ((x-s.x0)*dx+(y-s.y0)*dy)/l))
	}
	return math.Hypot(x-(s.x0+t*dx), y-(s.y0+t*dy)) <= s.w/2
}

func (s stroke) bounds() (float64, float64, float64, float64) {
	return math.Min(s.x0, s.x1) - s.w/2, math.Min(s.y0, s.y1) - s.w/2,
		math.Max(s.x0, s.x1) + s.w/2, math.Max(s.y0, s.y1) + s.w/2
}

// shapes is the union of several shapes.
type shapes []signShape

func (s shapes) contains(x, y float64) bool {
	for _, part := range s {
		if part.contains(x, y) {
			return true
		}
	}
	return false
}

func (s shapes) bounds() (float64, float64, float64, float64) {
	x0, y0, x1, y1 := s[0].bounds()
	for _, part := range s[1:] {
		a, b, c, d := part.bounds()
		x0, y0, x1, y1 = math.Min(x0, a), math.Min(y0, b), math.Max(x1, c), math.Max(y1, d)
	}
	return x0, y0, x1, y1
}

// rasterSign draws s into r. It sets a pixel when at least half of its samples
// fall inside the shape, so the edges stay smooth at small sizes.
func rasterSign(img *image.Paletted, r image.Rectangle, s signShape, c color.Color) {
	if r.Empty() {
		return
	}
	bx0, by0, bx1, by1 := s.bounds()
	px0 := max(0, min(r.Dx()-1, int(math.Floor(bx0*float64(r.Dx())))))
	py0 := max(0, min(r.Dy()-1, int(math.Floor(by0*float64(r.Dy())))))
	px1 := max(0, min(r.Dx()-1, int(math.Ceil(bx1*float64(r.Dx())))))
	py1 := max(0, min(r.Dy()-1, int(math.Ceil(by1*float64(r.Dy())))))
	for py := py0; py <= py1; py++ {
		for px := px0; px <= px1; px++ {
			n := 0
			for sy := 0; sy < signSamples; sy++ {
				for sx := 0; sx < signSamples; sx++ {
					x := (float64(px) + (float64(sx)+0.5)/signSamples) / float64(r.Dx())
					y := (float64(py) + (float64(sy)+0.5)/signSamples) / float64(r.Dy())
					if s.contains(x, y) {
						n++
					}
				}
			}
			if n*2 >= signSamples*signSamples {
				img.Set(r.Min.X+px, r.Min.Y+py, c)
			}
		}
	}
}

// ---- the smileys ----

// drawSign draws one state. A happy face means the reading is in the range, a
// sad face means it is above or below it, and the urgent low has an O-mouth.
// The smiley is an outline or filled, as the page selected.
func drawSign(img *image.Paletted, r image.Rectangle, state string, solid bool, c color.Color) {
	if r.Dx() < minSign || r.Dy() < minSign {
		return
	}
	switch state {
	case "high", "low":
		drawFace(img, r, "sad", solid, c)
	case "urgent":
		drawFace(img, r, "surprised", solid, c)
	default:
		drawFace(img, r, "happy", solid, c)
	}
}

// drawFace draws the face as an outline or filled. The expression is happy,
// sad or surprised; a stale reading keeps the happy face and the caller's gray.
func drawFace(img *image.Paletted, r image.Rectangle, expression string, solid bool, c color.Color) {
	fg := color.Color(c)
	if solid {
		rasterSign(img, r, disc{0.5, 0.5, 0.47}, c)
		fg = color.White
	} else {
		rasterSign(img, r, ring{0.5, 0.5, 0.44, 0.10}, c)
	}
	rasterSign(img, r, shapes{
		disc{0.36, 0.38, 0.06},
		disc{0.64, 0.38, 0.06},
	}, fg)
	switch expression {
	case "sad":
		rasterSign(img, r, mouth(0.5, 0.62, 0.22, 0.06, false), fg)
	case "surprised":
		rasterSign(img, r, ring{0.5, 0.67, 0.10, 0.055}, fg)
	default:
		rasterSign(img, r, mouth(0.5, 0.62, 0.22, 0.06, true), fg)
	}
}

// mouth draws an arc inside one box from top to top+drop. A smile fills the box
// from the top edge down, a frown from the bottom edge up, so both mouths keep
// the same box and centre line and the face only turns the mouth around.
func mouth(cx, top, radius, width float64, smile bool) signShape {
	drop := radius * (1 - math.Sin(math.Pi*0.18))
	bend := 1.0
	baseline := top
	if !smile {
		bend = -1
		baseline = top + drop
	}
	cy := baseline - bend*radius*math.Sin(math.Pi*0.18)
	parts := shapes{}
	const segments = 8
	for i := 0; i < segments; i++ {
		a0 := math.Pi * (0.18 + 0.64*float64(i)/segments)
		a1 := math.Pi * (0.18 + 0.64*float64(i+1)/segments)
		parts = append(parts, stroke{
			cx + radius*math.Cos(a0), cy + bend*radius*math.Sin(a0),
			cx + radius*math.Cos(a1), cy + bend*radius*math.Sin(a1), width,
		})
	}
	return parts
}
