package paperboy

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

var monoPalette = color.Palette{color.Gray{0}, color.Gray{255}}

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

// placeholder is a notice page with a title and a line of text, used for
// configuration problems and the setup page.
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
