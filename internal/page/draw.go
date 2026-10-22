package page

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

var (
	monoPalette = color.Palette{color.Gray{0}, color.Gray{255}}
	grayPalette = color.Palette{color.Gray{0}, color.Gray{85}, color.Gray{170}, color.Gray{255}}
)

const (
	alignLeft = iota
	alignCenter
	alignRight
)

// textStyle is a resolved typography style.
type textStyle struct {
	Size   float64
	Bold   bool
	Shrink bool
}

var (
	fontOnce sync.Once
	fonts    struct {
		regular *opentype.Font
		bold    *opentype.Font
	}

	faceOnce  sync.Mutex
	faceCache = map[faceKey]font.Face{}
)

type faceKey struct {
	bold bool
	size int
}

func faceFor(size float64, bold bool) font.Face {
	fontOnce.Do(func() {
		var err error
		if fonts.regular, err = opentype.Parse(goregular.TTF); err != nil {
			panic(err)
		}
		if fonts.bold, err = opentype.Parse(gobold.TTF); err != nil {
			panic(err)
		}
	})
	n := max(int(math.Round(size)), 6)
	key := faceKey{bold: bold, size: n}
	faceOnce.Lock()
	defer faceOnce.Unlock()
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

func measure(s string, size float64, bold bool) int {
	return font.MeasureString(faceFor(size, bold), s).Ceil()
}

// drawStyled draws text with the given style. Shrinking reduces the font size
// until the text fits maxWidth; without shrinking it may be clipped.
func drawStyled(dst draw.Image, s string, x, baseline int, st textStyle, maxWidth int, c color.Color, align int) {
	if s == "" || maxWidth <= 0 {
		return
	}
	size := max(st.Size, 6)
	if st.Shrink {
		for size > 6 && measure(s, size, st.Bold) > maxWidth {
			size--
		}
	}
	drawTextAt(dst, s, x, baseline, size, st.Bold, c, align)
}

func drawTextAt(dst draw.Image, s string, x, baseline int, size float64, bold bool, c color.Color, align int) {
	if s == "" {
		return
	}
	f := faceFor(size, bold)
	w := font.MeasureString(f, s).Ceil()
	switch align {
	case alignCenter:
		x -= w / 2
	case alignRight:
		x -= w
	}
	d := font.Drawer{Dst: dst, Src: image.NewUniform(c), Face: f, Dot: fixed.P(x, baseline)}
	d.DrawString(s)
}

func newCanvas(w, h int, gray bool) *image.Paletted {
	p := monoPalette
	if gray {
		p = grayPalette
	}
	img := image.NewPaletted(image.Rect(0, 0, w, h), p)
	draw.Draw(img, img.Rect, image.NewUniform(color.White), image.Point{}, draw.Src)
	return img
}

func hline(img *image.Paletted, x0, y, x1, thickness int) {
	if thickness < 1 {
		return
	}
	fillSolid(img, image.Rect(x0, y, x1, y+thickness))
}

func vline(img *image.Paletted, x, y0, y1, thickness int) {
	if thickness < 1 {
		return
	}
	fillSolid(img, image.Rect(x, y0, x+thickness, y1))
}

func fillSolid(img *image.Paletted, r image.Rectangle) {
	r = r.Intersect(img.Rect)
	if r.Empty() {
		return
	}
	draw.Draw(img, r, image.NewUniform(color.Black), image.Point{}, draw.Src)
}

// segment draws a black segment with the given thickness. Dashed segments are
// used for the "now" marker of the chart.
func segment(img *image.Paletted, x0, y0, x1, y1, thickness int, dashed bool) {
	if thickness < 1 {
		return
	}
	steps := max(absInt(x1-x0), absInt(y1-y0), 1)
	for i := 0; i <= steps; i++ {
		if dashed && (i/4)%2 == 1 {
			continue
		}
		x := x0 + (x1-x0)*i/steps
		y := y0 + (y1-y0)*i/steps
		fillSolid(img, image.Rect(x, y, x+thickness, y+thickness))
	}
}

// fillRect fills a rectangle with one of the shared fill styles (ADR 0034).
func fillRect(img *image.Paletted, r image.Rectangle, style string) {
	r = r.Intersect(img.Rect)
	if r.Empty() {
		return
	}
	switch style {
	case "none":
		return
	case "solid":
		fillSolid(img, r)
	case "hatch":
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				if (x+y)%4 == 0 {
					img.SetColorIndex(x, y, 0)
				}
			}
		}
	case "", "shade":
		if len(img.Palette) >= 4 {
			draw.Draw(img, r, image.NewUniform(color.Gray{170}), image.Point{}, draw.Src)
			return
		}
		// A checkerboard is the 1-bit equivalent of a mid tone.
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				if (x+y)%2 == 0 {
					img.SetColorIndex(x, y, 0)
				}
			}
		}
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
