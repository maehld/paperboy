package displayd

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

var (
	monoPalette = color.Palette{color.Gray{0}, color.Gray{255}}
	grayPalette = color.Palette{color.Gray{0}, color.Gray{85}, color.Gray{170}, color.Gray{255}}
)

// normalize converts any image to the profile's palette. Go's PNG encoder
// writes palettes of two or four colors with 1 or 2 bits per pixel, which the
// TRMNL firmware decodes directly.
func normalize(src image.Image, p Profile) *image.Paletted {
	palette := monoPalette
	if p.Gray {
		palette = grayPalette
	}
	b := src.Bounds()
	dst := image.NewPaletted(image.Rect(0, 0, b.Dx(), b.Dy()), palette)
	levels := len(palette) - 1
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			g := color.GrayModel.Convert(src.At(b.Min.X+x, b.Min.Y+y)).(color.Gray).Y
			dst.Pix[y*dst.Stride+x] = uint8(math.Round(float64(g) * float64(levels) / 255))
		}
	}
	return dst
}

func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, img); err != nil {
		panic(err) // encoding an in-memory paletted image cannot fail
	}
	return buf.Bytes()
}

var (
	boldOnce sync.Once
	boldFont *opentype.Font
)

func drawText(dst draw.Image, s string, x, baseline int, size float64, c color.Color, centered bool) {
	boldOnce.Do(func() {
		var err error
		if boldFont, err = opentype.Parse(gobold.TTF); err != nil {
			panic(err)
		}
	})
	face, err := opentype.NewFace(boldFont, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		panic(err)
	}
	defer face.Close()
	for size > 8 && font.MeasureString(face, s).Ceil() > dst.Bounds().Dx()*94/100 {
		face.Close()
		size--
		face, _ = opentype.NewFace(boldFont, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	}
	if centered {
		x -= font.MeasureString(face, s).Ceil() / 2
	}
	d := font.Drawer{Dst: dst, Src: image.NewUniform(c), Face: face, Dot: fixed.P(x, baseline)}
	d.DrawString(s)
}

// withBanner returns a copy of img with an inverted notice bar at the bottom.
func withBanner(img *image.Paletted, text string) *image.Paletted {
	out := image.NewPaletted(img.Rect, img.Palette)
	copy(out.Pix, img.Pix)
	w, h := img.Rect.Dx(), img.Rect.Dy()
	bar := h / 8
	draw.Draw(out, image.Rect(0, h-bar, w, h), image.NewUniform(color.Black), image.Point{}, draw.Src)
	drawText(out, text, w/2, h-bar*3/10, float64(bar)*0.55, color.White, true)
	return out
}

// placeholder is shown before a producer uploaded anything for a profile.
func placeholder(p Profile, lines ...string) *image.Paletted {
	img := image.NewPaletted(image.Rect(0, 0, p.Width, p.Height), monoPalette)
	draw.Draw(img, img.Rect, image.NewUniform(color.White), image.Point{}, draw.Src)
	size := float64(p.Height) / 12
	top := p.Height/2 - int(size*float64(len(lines)-1)*0.75)
	for i, line := range lines {
		drawText(img, line, p.Width/2, top+int(float64(i)*size*1.5), size, color.Black, true)
	}
	return img
}
