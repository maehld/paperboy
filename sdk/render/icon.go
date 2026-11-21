package render

import (
	"image"
	"image/color"
	"math"
)

// Mask is a pictogram reduced to ink: opaque dark pixels are ink, everything
// else is background, so one asset renders on 1-bit and 4-gray panels.
type Mask struct {
	w, h int
	ink  []bool
}

// NewMask converts a picture to a mask cropped to its ink. It returns nil for
// a picture without ink.
func NewMask(img image.Image) *Mask {
	b := img.Bounds()
	m := &Mask{w: b.Dx(), h: b.Dy(), ink: make([]bool, b.Dx()*b.Dy())}
	for y := 0; y < m.h; y++ {
		for x := 0; x < m.w; x++ {
			n := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			if n.A < 128 {
				continue
			}
			lum := (299*int(n.R) + 587*int(n.G) + 114*int(n.B)) / 1000
			m.ink[y*m.w+x] = lum < 128
		}
	}
	return m.trim()
}

func (m *Mask) at(x, y int) bool {
	if x < 0 || y < 0 || x >= m.w || y >= m.h {
		return false
	}
	return m.ink[y*m.w+x]
}

func (m *Mask) trim() *Mask {
	minX, minY, maxX, maxY := m.w, m.h, -1, -1
	for y := 0; y < m.h; y++ {
		for x := 0; x < m.w; x++ {
			if !m.ink[y*m.w+x] {
				continue
			}
			minX, minY = min(minX, x), min(minY, y)
			maxX, maxY = max(maxX, x), max(maxY, y)
		}
	}
	if maxX < 0 {
		return nil
	}
	w, h := maxX-minX+1, maxY-minY+1
	out := &Mask{w: w, h: h, ink: make([]bool, w*h)}
	for y := 0; y < h; y++ {
		copy(out.ink[y*w:(y+1)*w], m.ink[(minY+y)*m.w+minX:])
	}
	return out
}

// Draw paints the mask in black, scaled to height and limited by the width of
// r, aligned horizontally and centered vertically. Downscaling uses the
// majority of the covered source pixels, so thin strokes survive.
func (m *Mask) Draw(dst *image.Paletted, r image.Rectangle, height int, align Align) {
	if m == nil || height < 1 || m.h < 1 {
		return
	}
	w := max(int(math.Round(float64(m.w)*float64(height)/float64(m.h))), 1)
	h := height
	if w > r.Dx() {
		w = r.Dx()
		h = max(int(math.Round(float64(m.h)*float64(w)/float64(m.w))), 1)
	}
	if w < 1 || h < 1 {
		return
	}
	x0 := r.Min.X
	switch align {
	case Center:
		x0 = r.Min.X + (r.Dx()-w)/2
	case Right:
		x0 = r.Max.X - w
	}
	y0 := r.Min.Y + (r.Dy()-h)/2
	for y := 0; y < h; y++ {
		sy0, sy1 := y*m.h/h, (y+1)*m.h/h
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		for x := 0; x < w; x++ {
			sx0, sx1 := x*m.w/w, (x+1)*m.w/w
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			if !m.majority(sx0, sy0, sx1, sy1) {
				continue
			}
			if p := image.Pt(x0+x, y0+y); p.In(dst.Rect) {
				dst.SetColorIndex(p.X, p.Y, 0)
			}
		}
	}
}

func (m *Mask) majority(x0, y0, x1, y1 int) bool {
	ink, total := 0, 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			total++
			if m.at(x, y) {
				ink++
			}
		}
	}
	return total > 0 && ink*2 >= total
}
