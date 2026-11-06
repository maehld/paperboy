package page

import (
	"image"
	"image/color"
	"image/draw"

	pb "github.com/maehld/paperboy/pkg/render"
)

// The drawing primitives live in pkg/render so that plugins draw fragments
// with the same fonts and palettes (spike: plugin fragments).

var (
	monoPalette = pb.MonoPalette
	grayPalette = pb.GrayPalette
)

const (
	alignLeft   = int(pb.Left)
	alignCenter = int(pb.Center)
	alignRight  = int(pb.Right)
)

// textStyle is a resolved typography style.
type textStyle = pb.Style

func measure(s string, size float64, bold bool) int { return pb.Measure(s, size, bold) }

func drawStyled(dst draw.Image, s string, x, baseline int, st textStyle, maxWidth int, c color.Color, align int) {
	pb.Text(dst, s, x, baseline, st, maxWidth, c, pb.Align(align))
}

func drawTextAt(dst draw.Image, s string, x, baseline int, size float64, bold bool, c color.Color, align int) {
	pb.TextAt(dst, s, x, baseline, size, bold, c, pb.Align(align))
}

func newCanvas(w, h int, gray bool) *image.Paletted {
	if gray {
		return pb.NewCanvas(w, h, 4)
	}
	return pb.NewCanvas(w, h, 2)
}

func hline(img *image.Paletted, x0, y, x1, thickness int) { pb.HLine(img, x0, y, x1, thickness) }

func vline(img *image.Paletted, x, y0, y1, thickness int) { pb.VLine(img, x, y0, y1, thickness) }

func fillSolid(img *image.Paletted, r image.Rectangle) { pb.FillSolid(img, r) }

func segment(img *image.Paletted, x0, y0, x1, y1, thickness int, dashed bool) {
	pb.Segment(img, x0, y0, x1, y1, thickness, dashed)
}

func fillRect(img *image.Paletted, r image.Rectangle, style string) { pb.Fill(img, r, style) }

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
