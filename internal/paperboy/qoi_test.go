package paperboy

import (
	"encoding/binary"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"testing"
)

// decodeQOI is a reference decoder for the tests: the firmware decodes QOI
// with ESPHome's runtime_image decoder, so the encoder must follow the
// specification exactly.
func decodeQOI(t *testing.T, data []byte) *image.NRGBA {
	t.Helper()
	if len(data) < 14 || string(data[:4]) != "qoif" {
		t.Fatalf("not a QOI image: %q", data[:min(len(data), 4)])
	}
	w := int(binary.BigEndian.Uint32(data[4:8]))
	h := int(binary.BigEndian.Uint32(data[8:12]))
	if data[12] != 4 {
		t.Fatalf("channels = %d, want 4", data[12])
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	var index [64]color.NRGBA
	px := color.NRGBA{0, 0, 0, 255}
	i := 14
	n := 0
	for n < w*h {
		if i >= len(data) {
			t.Fatalf("QOI data ended after %d of %d pixels", n, w*h)
		}
		b := data[i]
		switch {
		case b == qoiOpRGB:
			px.R, px.G, px.B = data[i+1], data[i+2], data[i+3]
			i += 4
			n++
		case b == qoiOpRGBA:
			px.R, px.G, px.B, px.A = data[i+1], data[i+2], data[i+3], data[i+4]
			i += 5
			n++
		case b&0xc0 == qoiOpIndex:
			px = index[b&0x3f]
			i++
			n++
		case b&0xc0 == qoiOpDiff:
			px.R += (b>>4)&0x03 - 2
			px.G += (b>>2)&0x03 - 2
			px.B += b&0x03 - 2
			i++
			n++
		case b&0xc0 == qoiOpLuma:
			dg := int(b&0x3f) - 32
			px.R += uint8(dg - 8 + int(data[i+1]>>4&0x0f))
			px.G += uint8(dg)
			px.B += uint8(dg - 8 + int(data[i+1]&0x0f))
			i += 2
			n++
		default: // run
			run := int(b&0x3f) + 1
			for k := 0; k < run && n < w*h; k++ {
				img.SetNRGBA(n%w, n/w, px)
				n++
			}
			i++
			continue
		}
		img.SetNRGBA((n-1)%w, (n-1)/w, px)
		index[qoiSlot(color.RGBA(px))] = px
	}
	return img
}

func TestEncodeQOIRoundTrip(t *testing.T) {
	img := image.NewPaletted(image.Rect(0, 0, 40, 12), color.Palette{
		color.Gray{0}, color.Gray{85}, color.Gray{170}, color.Gray{255}})
	for y := 0; y < 12; y++ {
		for x := 0; x < 40; x++ {
			switch {
			case x < 10:
				img.SetColorIndex(x, y, 3)
			case x < 20:
				img.SetColorIndex(x, y, 2)
			case x == 25:
				img.SetColorIndex(x, y, 1)
			default:
				img.SetColorIndex(x, y, 0)
			}
		}
	}
	got := decodeQOI(t, encodeQOI(img))
	for y := 0; y < 12; y++ {
		for x := 0; x < 40; x++ {
			want := img.At(x, y)
			if r, g, b, a := got.At(x, y).RGBA(); r>>8 != uint32Gray(want) || g>>8 != uint32Gray(want) ||
				b>>8 != uint32Gray(want) || a>>8 != 255 {
				t.Fatalf("pixel %d,%d = %d/%d/%d/%d, want %d", x, y, r>>8, g>>8, b>>8, a>>8, uint32Gray(want))
			}
		}
	}
}

func uint32Gray(c color.Color) uint32 {
	r, _, _, _ := c.RGBA()
	return r >> 8
}

func TestEncodeQOISmallerThanRaw(t *testing.T) {
	img := image.NewPaletted(image.Rect(0, 0, 800, 480), color.Palette{
		color.Gray{0}, color.Gray{85}, color.Gray{170}, color.Gray{255}})
	for i := range img.Pix {
		img.Pix[i] = 3 // white page
	}
	data := encodeQOI(img)
	if len(data) > 800*480/8 {
		t.Fatalf("an empty page encoded to %d bytes", len(data))
	}
}

func TestRenderedImageAsQOI(t *testing.T) {
	s, _ := testServer(t)
	img := image.NewPaletted(image.Rect(0, 0, 20, 10), color.Palette{
		color.Gray{0}, color.Gray{85}, color.Gray{170}, color.Gray{255}})
	for i := range img.Pix {
		img.Pix[i] = 3
	}
	data := encodePNG(img)
	s.rendered["small/eins"] = &renderedPage{data: data, hash: hashBytes(data)}

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pages/small/eins/"+hashBytes(data)+".png?format=qoi", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/qoi" {
		t.Fatalf("qoi response: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if got := decodeQOI(t, rec.Body.Bytes()); got.Bounds() != img.Bounds() {
		t.Fatalf("decoded bounds: %v", got.Bounds())
	}

	// Without the parameter the same URL stays a PNG for browsers.
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pages/small/eins/"+hashBytes(data)+".png", nil))
	if rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("plain response: %q", rec.Header().Get("Content-Type"))
	}
}
