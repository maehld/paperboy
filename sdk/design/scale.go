// Package design is the drawing language of Paperboy's widgets: a page-wide
// type scale and the shapes that many plugins repeat, such as tiles, hero
// elements, trend charts and meters.
//
// The package draws into a rectangle and returns whether it drew. A plugin
// keeps the cell, the data and the decision what to show; it can use one
// component and paint the rest itself. The protocol does not change: this is
// a Go SDK package for plugins that draw in Go.
package design

import (
	"math"

	"github.com/maehld/paperboy/sdk/render"
)

// Factors of the type scale, relative to the page's base text size.
const (
	tinyFactor   = 0.55
	microFactor  = 0.70
	detailFactor = 0.85
	titleFactor  = 1.25
	heroFactor   = 2.6
	// heroShare caps the hero size on a short cell, so the value still leaves
	// room for a label and details.
	heroShare = 0.42
)

// Scale is the type scale of one page. Every role follows the page's base
// text size, so a tile label is the same size in every widget. Only Hero
// grows with the cell.
type Scale struct {
	Tiny   float64
	Micro  float64
	Detail float64
	Body   float64
	Title  float64
	Hero   float64
}

// NewScale builds the roles from the base text size of the page in pixels.
// cellHeight caps the hero; it does not change the other roles.
func NewScale(base float64, cellHeight int) Scale {
	base = max(base, render.MinSize)
	s := Scale{
		Tiny:   base * tinyFactor,
		Micro:  base * microFactor,
		Detail: base * detailFactor,
		Body:   base,
		Title:  base * titleFactor,
		Hero:   base * heroFactor,
	}
	if cellHeight > 0 {
		s.Hero = min(s.Hero, float64(cellHeight)*heroShare)
	}
	return s.floor()
}

// Scaled returns the roles multiplied by f. The result never falls below
// render.MinSize, the smallest drawn text.
func (s Scale) Scaled(f float64) Scale {
	if f < 0 {
		f = 0
	}
	return Scale{
		Tiny:   s.Tiny * f,
		Micro:  s.Micro * f,
		Detail: s.Detail * f,
		Body:   s.Body * f,
		Title:  s.Title * f,
		Hero:   s.Hero * f,
	}.floor()
}

// floor keeps the roles in order and above the smallest drawn text.
func (s Scale) floor() Scale {
	s.Tiny = max(s.Tiny, render.MinSize)
	s.Micro = max(s.Micro, s.Tiny, render.MinSize)
	s.Detail = max(s.Detail, s.Micro, render.MinSize)
	s.Body = max(s.Body, s.Detail, render.MinSize)
	s.Title = max(s.Title, s.Body, render.MinSize)
	s.Hero = max(s.Hero, s.Title, render.MinSize)
	return s
}

// lineHeight is the height of one line of text at size.
func lineHeight(size float64) int {
	return int(math.Round(size * 1.18))
}

// baseline places the baseline of a line whose top is top.
func baseline(top int, size float64) int {
	return top + int(math.Round(size*0.95))
}

// capBaseline centers the capitals of a value in a box of height h.
func capBaseline(top, h int, size float64) int {
	const capShare = 0.72
	capHeight := size * capShare
	return top + int(math.Round((float64(h)+capHeight)/2))
}

// gap returns a gap that follows a text size.
func gap(size float64) int {
	return max(int(math.Round(size*0.18)), 2)
}
