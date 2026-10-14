package opendisplay

import (
	"fmt"
	"image"
	"image/color"
)

// Panels whose 4-gray controller swaps the two middle gray codes (bb_epaper "v2").
var gray4V2Panels = map[uint16]bool{0x0028: true, 0x0048: true}

// Encode converts an image of exactly the panel size into the wire format.
// Supported: monochrome (1 bpp, MSB = leftmost pixel, 1 = white) and 4 gray
// levels (two 1-bit controller planes, plane 0 = code bit 0, then plane 1).
func Encode(img image.Image, d Display) ([]byte, error) {
	b := img.Bounds()
	if b.Dx() != d.Width || b.Dy() != d.Height {
		return nil, fmt.Errorf("Bildgröße %dx%d passt nicht zum Panel %dx%d", b.Dx(), b.Dy(), d.Width, d.Height)
	}
	stride := (d.Width + 7) / 8
	switch d.ColorScheme {
	case SchemeMono:
		out := make([]byte, stride*d.Height)
		for y := 0; y < d.Height; y++ {
			for x := 0; x < d.Width; x++ {
				if luma(img.At(b.Min.X+x, b.Min.Y+y)) >= 128 {
					out[y*stride+x/8] |= 0x80 >> (x % 8)
				}
			}
		}
		return out, nil
	case SchemeGray4:
		// Level 0 = black … 3 = white, mapped to the controller's stored code.
		codes := [4]byte{3, 1, 2, 0}
		if gray4V2Panels[d.PanelIC] {
			codes = [4]byte{3, 2, 1, 0}
		}
		plane := stride * d.Height
		out := make([]byte, 2*plane)
		for y := 0; y < d.Height; y++ {
			for x := 0; x < d.Width; x++ {
				code := codes[(int(luma(img.At(b.Min.X+x, b.Min.Y+y)))*4)/256]
				bit := byte(0x80 >> (x % 8))
				if code&1 != 0 {
					out[y*stride+x/8] |= bit
				}
				if code&2 != 0 {
					out[plane+y*stride+x/8] |= bit
				}
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("Farbschema %s wird noch nicht unterstützt", d.ColorScheme)
}

func luma(c color.Color) uint8 {
	return color.GrayModel.Convert(c).(color.Gray).Y
}
