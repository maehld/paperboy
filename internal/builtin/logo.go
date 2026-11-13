package builtin

import (
	"image"
	"math"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

// The mark of Paperboy's logo (docs/logo): a page of four by three cells of 16
// units, a frame of 3 units with the top-right corner turned down, dividers of
// 2 units and one cell drawn by a widget. Every shape is filled and the mark
// needs one colour, so it scales to any size. The lockup adds the wordmark in
// Go Mono Bold after a gap of 16.66 units.
const (
	markWidth   = 64.0
	markHeight  = 48.0
	lockupWidth = 226.0
)

// markInk reports whether the mark covers the point (x, y) in its own
// coordinates, which are 64 by 48 units with the origin at the top left.
func markInk(x, y float64) bool {
	if x < 0 || y < 0 || x >= markWidth || y >= markHeight {
		return false
	}
	// The page is cut at the top right: the outer path M0 0 H48 L64 16 V48 H0 Z
	// keeps every point below the diagonal x - y = 48.
	if x-y > 48 {
		return false
	}
	// Inside the hole of the frame (the inner path, 3 units wide) only the
	// dividers, the drawn cell and the turned-down corner are ink.
	if x >= 3 && x < 61 && y >= 3 && y < 45 && x-y <= 43.757 {
		switch {
		case x >= 15 && x < 17, x >= 31 && x < 33, x >= 47 && x < 49 && y < 16:
			return true // dividers between the columns
		case y >= 15 && y < 17, y >= 31 && y < 33:
			return true // dividers between the rows
		case x < 16 && y >= 32:
			return true // the cell a widget draws
		case x >= 48 && y < 16:
			return true // the turned-down corner
		}
		return false
	}
	return true
}

// drawMark paints the mark black into img, fitted to r with the aspect ratio of
// the design. Edges are antialiased by supersampling, and the palette of the
// page decides how many gray levels an edge takes.
func drawMark(img *image.Paletted, r image.Rectangle) {
	levels := len(img.Palette)
	if r.Dx() < 1 || r.Dy() < 1 || levels < 2 {
		return
	}
	const samples = 3
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			ink := 0
			for sy := 0; sy < samples; sy++ {
				for sx := 0; sx < samples; sx++ {
					u := (float64(x-r.Min.X) + (float64(sx)+0.5)/samples) * markWidth / float64(r.Dx())
					v := (float64(y-r.Min.Y) + (float64(sy)+0.5)/samples) * markHeight / float64(r.Dy())
					if markInk(u, v) {
						ink++
					}
				}
			}
			if ink == 0 || !image.Pt(x, y).In(img.Rect) {
				continue
			}
			coverage := float64(ink) / (samples * samples)
			idx := min(int((1-coverage)*float64(levels-1)+0.5), levels-1)
			img.SetColorIndex(x, y, uint8(idx))
		}
	}
}

// logo draws the mark into the cell, centered and as large as the cell allows;
// the arg wordmark adds the wordmark of the lockup.
func (Plugin) logo(w plugin.WidgetRequest, img *image.Paletted) error {
	b := img.Rect.Inset(w.Style.Padding)
	if b.Empty() {
		return plugin.TooSmall(w)
	}
	wordmark := w.Arg().Bool("wordmark", false)
	ratio := markWidth / markHeight
	if wordmark {
		ratio = lockupWidth / markHeight
	}
	h := b.Dy()
	width := int(math.Round(float64(h) * ratio))
	if width > b.Dx() {
		width = b.Dx()
		h = int(math.Round(float64(width) / ratio))
	}
	if h < 1 || width < 1 {
		return plugin.TooSmall(w)
	}
	x0 := b.Min.X + (b.Dx()-width)/2
	y0 := b.Min.Y + (b.Dy()-h)/2
	markW := min(int(math.Round(float64(h)*markWidth/markHeight)), width)
	drawMark(img, image.Rect(x0, y0, x0+markW, y0+h))
	if wordmark {
		// The wordmark starts after the gap of the design and sits on the
		// baseline of the lockup, 0.80 of its height.
		gap := int(math.Round(float64(h) * 16.66 / markHeight))
		room := width - markW - gap
		size := float64(h) * 0.63
		for size > render.MinSize && render.MeasureMono("paperboy", size) > room {
			size--
		}
		render.MonoText(img, "paperboy", x0+markW+gap, y0+int(math.Round(float64(h)*0.80)), size,
			render.Black, render.Left)
	}
	return nil
}
