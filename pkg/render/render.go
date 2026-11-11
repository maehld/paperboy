// Package render holds the drawing primitives of Paperboy's page renderer:
// palettes, fonts, text, lines and fills. Paperboy's own blocks and external
// plugins that render image fragments use the same code, so a fragment has
// the same typeface, hinting and gray levels as the page around it.
//
// The font faces are cached and not safe for concurrent use; callers that
// render from several goroutines must serialize their drawing.
package render

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Palettes of the supported panels. Index 0 is always black and the last
// index white.
var (
	MonoPalette = color.Palette{color.Gray{0}, color.Gray{255}}
	GrayPalette = color.Palette{color.Gray{0}, color.Gray{85}, color.Gray{170}, color.Gray{255}}
)

// Gray tones of the four-level palette.
var (
	Black     = color.Gray{0}
	DarkGray  = color.Gray{85}
	LightGray = color.Gray{170}
	White     = color.Gray{255}
)

// Align is the horizontal anchor of a text.
type Align int

const (
	Left Align = iota
	Center
	Right
)

// Style is a resolved typography style. Size is the font size in pixels.
type Style struct {
	Size   float64
	Bold   bool
	Shrink bool // reduce the size until the text fits maxWidth
}

// MinSize is the smallest font size that is drawn.
const MinSize = 6

var (
	fontOnce sync.Once
	fonts    struct {
		regular *opentype.Font
		bold    *opentype.Font
	}

	faceMu    sync.Mutex
	faceCache = map[faceKey]font.Face{}
)

type faceKey struct {
	bold bool
	size int
}

// Face returns the cached Go font face for a pixel size, rounded to whole
// pixels with full hinting.
func Face(size float64, bold bool) font.Face {
	fontOnce.Do(func() {
		var err error
		if fonts.regular, err = opentype.Parse(goregular.TTF); err != nil {
			panic(err)
		}
		if fonts.bold, err = opentype.Parse(gobold.TTF); err != nil {
			panic(err)
		}
	})
	n := max(int(math.Round(size)), MinSize)
	key := faceKey{bold: bold, size: n}
	faceMu.Lock()
	defer faceMu.Unlock()
	if f, ok := faceCache[key]; ok {
		return f
	}
	src := fonts.regular
	if bold {
		src = fonts.bold
	}
	f, err := opentype.NewFace(src, &opentype.FaceOptions{Size: float64(n), DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		panic(err)
	}
	faceCache[key] = f
	return f
}

// Measure returns the advance width of s in pixels.
func Measure(s string, size float64, bold bool) int {
	return font.MeasureString(Face(size, bold), s).Ceil()
}

// Text draws s with the given style. Shrinking reduces the font size until
// the text fits maxWidth; without shrinking it may be clipped.
func Text(dst draw.Image, s string, x, baseline int, st Style, maxWidth int, c color.Color, align Align) {
	if s == "" || maxWidth <= 0 {
		return
	}
	size := max(st.Size, MinSize)
	if st.Shrink {
		for size > MinSize && Measure(s, size, st.Bold) > maxWidth {
			size--
		}
	}
	TextAt(dst, s, x, baseline, size, st.Bold, c, align)
}

// TextAt draws s at a fixed size.
func TextAt(dst draw.Image, s string, x, baseline int, size float64, bold bool, c color.Color, align Align) {
	if s == "" {
		return
	}
	f := Face(size, bold)
	w := font.MeasureString(f, s).Ceil()
	switch align {
	case Center:
		x -= w / 2
	case Right:
		x -= w
	}
	d := font.Drawer{Dst: dst, Src: image.NewUniform(c), Face: f, Dot: fixed.P(x, baseline)}
	d.DrawString(s)
}

// Ellipsize shortens s with "…" until it fits maxWidth.
func Ellipsize(s string, size float64, bold bool, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if Measure(s, size, bold) <= maxWidth {
		return s
	}
	runes := []rune(s)
	for n := len(runes) - 1; n > 0; n-- {
		cut := strings.TrimRight(string(runes[:n]), " ,;·-") + "…"
		if Measure(cut, size, bold) <= maxWidth {
			return cut
		}
	}
	return "…"
}

// Levels returns the number of gray levels of a palette: 2 or 4.
func Levels(p color.Palette) int {
	if len(p) >= 4 {
		return 4
	}
	return 2
}

// NewCanvas returns a white paletted image with 2 (black and white) or 4 gray
// levels.
func NewCanvas(w, h, levels int) *image.Paletted {
	p := MonoPalette
	if levels == 4 {
		p = GrayPalette
	}
	img := image.NewPaletted(image.Rect(0, 0, w, h), p)
	draw.Draw(img, img.Rect, image.NewUniform(color.White), image.Point{}, draw.Src)
	return img
}

// HLine draws a horizontal black line.
func HLine(img *image.Paletted, x0, y, x1, thickness int) {
	if thickness < 1 {
		return
	}
	FillSolid(img, image.Rect(x0, y, x1, y+thickness))
}

// VLine draws a vertical black line.
func VLine(img *image.Paletted, x, y0, y1, thickness int) {
	if thickness < 1 {
		return
	}
	FillSolid(img, image.Rect(x, y0, x+thickness, y1))
}

// FillSolid fills r with black.
func FillSolid(img *image.Paletted, r image.Rectangle) {
	FillColor(img, r, color.Black)
}

// FillColor fills r with one color of the palette.
func FillColor(img *image.Paletted, r image.Rectangle, c color.Color) {
	r = r.Intersect(img.Rect)
	if r.Empty() {
		return
	}
	draw.Draw(img, r, image.NewUniform(c), image.Point{}, draw.Src)
}

// Segment draws a black segment with the given thickness, optionally dashed.
func Segment(img *image.Paletted, x0, y0, x1, y1, thickness int, dashed bool) {
	if thickness < 1 {
		return
	}
	steps := max(abs(x1-x0), abs(y1-y0), 1)
	for i := 0; i <= steps; i++ {
		if dashed && (i/4)%2 == 1 {
			continue
		}
		x := x0 + (x1-x0)*i/steps
		y := y0 + (y1-y0)*i/steps
		FillSolid(img, image.Rect(x, y, x+thickness, y+thickness))
	}
}

// Fill fills a rectangle with one of the shared fill styles: "solid",
// "hatch", "shade" (the default; light gray, or every other row on 1-bit
// panels) or "none".
//
// Both patterns are rows on purpose: a page travels to the device as QOI
// (ADR 0072), and a diagonal dot grid or a checkerboard costs almost a byte
// per pixel there, while a whole row of one colour becomes a run (ADR 0088).
// The ink coverage is the same as before: a quarter of the pixels for "hatch",
// half of them for "shade".
func Fill(img *image.Paletted, r image.Rectangle, style string) {
	r = r.Intersect(img.Rect)
	if r.Empty() {
		return
	}
	switch style {
	case "none":
		return
	case "solid":
		FillSolid(img, r)
	case "hatch":
		for y := r.Min.Y; y < r.Max.Y; y += 4 {
			FillSolid(img, image.Rect(r.Min.X, y, r.Max.X, y+1))
		}
	case "", "shade":
		if len(img.Palette) >= 4 {
			draw.Draw(img, r, image.NewUniform(LightGray), image.Point{}, draw.Src)
			return
		}
		for y := r.Min.Y; y < r.Max.Y; y += 2 {
			FillSolid(img, image.Rect(r.Min.X, y, r.Max.X, y+1))
		}
	}
}

// Quantize maps any image onto the palette with 2 or 4 gray levels: every
// pixel takes the nearest gray by luminance, without dithering, so text and
// lines stay crisp. Transparent pixels count as white.
func Quantize(src image.Image, levels int) *image.Paletted {
	b := src.Bounds()
	out := NewCanvas(b.Dx(), b.Dy(), levels)
	n := len(out.Palette)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := src.At(x, y).RGBA()
			// Composite onto white, then take the luminance (0..65535).
			r = r + (0xffff - a)
			g = g + (0xffff - a)
			bl = bl + (0xffff - a)
			lum := (299*r + 587*g + 114*bl) / 1000
			idx := int((lum*uint32(n-1) + 0x7fff) / 0xffff)
			out.SetColorIndex(x-b.Min.X, y-b.Min.Y, uint8(min(idx, n-1)))
		}
	}
	return out
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
