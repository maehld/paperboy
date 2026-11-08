package paperboy

import (
	"encoding/binary"
	"image"
	"image/color"
)

// QOI chunk tags (https://qoiformat.org/qoi-specification.pdf).
const (
	qoiOpIndex = 0x00 // 00xxxxxx: repeat a colour from the index
	qoiOpDiff  = 0x40 // 01xxxxxx: small differences per channel
	qoiOpLuma  = 0x80 // 10xxxxxx: green difference plus red/blue deltas
	qoiOpRun   = 0xc0 // 11xxxxxx: run of the previous colour
	qoiOpRGB   = 0xfe
	qoiOpRGBA  = 0xff
)

// encodeQOI writes img as a QOI image (ADR 0072). A Sticky fetches a page over
// the LAN in 34-61 ms but needs 2.6 s to decode its PNG, and that time is the
// same for every page because the frame is always 800x480. QOI is three to
// four times larger on the wire, which costs about 150 ms here, and decodes in
// a plain byte loop without zlib.
//
// Always four channels: the pages are opaque, but an image with transparency
// then still round-trips.
func encodeQOI(img image.Image) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	out := make([]byte, 0, 14+4*w*h)
	out = append(out, 'q', 'o', 'i', 'f')
	out = binary.BigEndian.AppendUint32(out, uint32(w))
	out = binary.BigEndian.AppendUint32(out, uint32(h))
	out = append(out, 4, 0) // four channels, sRGB with linear alpha

	var index [64]color.RGBA
	prev := color.RGBA{0, 0, 0, 255}
	run := 0
	flushRun := func() {
		for run > 0 {
			n := min(run, 62)
			out = append(out, byte(qoiOpRun|(n-1)))
			run -= n
		}
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := qoiColor(img.At(x, y))
			if c == prev {
				run++
				if run == 62 {
					flushRun()
				}
				continue
			}
			flushRun()
			slot := qoiSlot(c)
			if index[slot] == c {
				out = append(out, byte(qoiOpIndex|slot))
			} else {
				index[slot] = c
				dr, dg, db := int(c.R)-int(prev.R), int(c.G)-int(prev.G), int(c.B)-int(prev.B)
				switch {
				case c.A != prev.A:
					out = append(out, qoiOpRGBA, c.R, c.G, c.B, c.A)
				case dr >= -2 && dr <= 1 && dg >= -2 && dg <= 1 && db >= -2 && db <= 1:
					out = append(out, byte(qoiOpDiff|(dr+2)<<4|(dg+2)<<2|(db+2)))
				case dg >= -32 && dg <= 31 && dr-dg >= -8 && dr-dg <= 7 && db-dg >= -8 && db-dg <= 7:
					out = append(out, byte(qoiOpLuma|(dg+32)), byte((dr-dg+8)<<4|(db-dg+8)))
				default:
					out = append(out, qoiOpRGB, c.R, c.G, c.B)
				}
			}
			prev = c
		}
	}
	flushRun()
	return append(out, 0, 0, 0, 0, 0, 0, 0, 1) // end marker
}

// qoiSlot is the hash of the specification: (r*3 + g*5 + b*7 + a*11) % 64.
func qoiSlot(c color.RGBA) int {
	return (int(c.R)*3 + int(c.G)*5 + int(c.B)*7 + int(c.A)*11) % 64
}

// qoiColor converts any colour of the palettes used for pages (color.Gray and
// friends) to the non-premultiplied RGBA a QOI chunk carries.
func qoiColor(c color.Color) color.RGBA {
	switch v := c.(type) {
	case color.RGBA:
		return v
	case color.Gray:
		return color.RGBA{v.Y, v.Y, v.Y, 255}
	case color.NRGBA:
		return color.RGBA{v.R, v.G, v.B, v.A}
	}
	r, g, b, a := c.RGBA()
	if a == 0 {
		return color.RGBA{}
	}
	unpremultiply := func(v uint32) uint8 { return uint8((v * 0xffff / a) >> 8) }
	return color.RGBA{unpremultiply(r), unpremultiply(g), unpremultiply(b), uint8(a >> 8)}
}
