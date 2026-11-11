package paperboy

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maehld/paperboy/internal/page"
	"github.com/maehld/paperboy/pkg/render"
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

// noisyPage is an 800x480 page of random four-level noise, the worst case the
// encoder can meet.
func noisyPage(w, h int) *image.Paletted {
	img := render.NewCanvas(w, h, 4)
	rnd := rand.New(rand.NewSource(1))
	for i := range img.Pix {
		img.Pix[i] = uint8(rnd.Intn(4))
	}
	return img
}

// alternatingPage is the worst case a page of Paperboy's palette can reach:
// every pixel changes between black and white, so QOI spends one index byte on
// each of them and nothing can be a run.
func alternatingPage(w, h int) *image.Paletted {
	img := render.NewCanvas(w, h, 4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Pix[y*img.Stride+x] = uint8((x + y) % 2 * 3)
		}
	}
	return img
}

// ditheredPage is an 800x480 page with a photo-like field dithered to four
// levels, the largest kind of page Paperboy draws (measured 233 KB as QOI).
func ditheredPage(w, h int) *image.Paletted {
	img := render.NewCanvas(w, h, 4)
	vals := make([]float64, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := 128 + 60*math.Sin(float64(x)/37)*math.Cos(float64(y)/29)
			v += 40 * math.Sin(math.Hypot(float64(x-w/2), float64(y-h/2))/23)
			vals[y*w+x] = max(0, min(255, v))
		}
	}
	const step = 255.0 / 3
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			old := vals[y*w+x]
			idx := max(0, min(int(math.Round(old/step)), 3))
			img.Pix[y*img.Stride+x] = uint8(idx)
			err := old - float64(idx)*step
			if x+1 < w {
				vals[y*w+x+1] += err * 7 / 16
			}
			if y+1 < h {
				if x > 0 {
					vals[(y+1)*w+x-1] += err * 3 / 16
				}
				vals[(y+1)*w+x] += err * 5 / 16
				if x+1 < w {
					vals[(y+1)*w+x+1] += err * 1 / 16
				}
			}
		}
	}
	return img
}

// TestFillPatternsStaySmall holds the notice and shade patterns under the old
// buffer size (ADR 0088): as a diagonal dot grid or a checkerboard they took
// almost a byte per pixel.
func TestFillPatternsStaySmall(t *testing.T) {
	const small = 20 * 1024
	for _, tc := range []struct {
		name   string
		levels int
		style  string
	}{
		{"hatch", 4, "hatch"},
		{"shade on two levels", 2, "shade"},
	} {
		img := render.NewCanvas(800, 480, tc.levels)
		render.Fill(img, img.Rect, tc.style)
		if size := len(encodeQOI(img)); size > small {
			t.Errorf("a full page of %s encodes to %d bytes, want at most %d", tc.name, size, small)
		}
	}
}

// TestWorstPageFitsBuffer is the reason the firmware buffer is 512 KiB: with a
// page palette every pixel costs at most one QOI byte, so 800x480 stays below
// 384 KB (ADR 0088).
func TestWorstPageFitsBuffer(t *testing.T) {
	for _, img := range []struct {
		name string
		img  *image.Paletted
	}{
		{"alternating black and white", alternatingPage(800, 480)},
		{"four-level noise", noisyPage(800, 480)},
		{"dithered photo", ditheredPage(800, 480)},
	} {
		size := len(encodeQOI(img.img))
		t.Logf("%s: %d bytes", img.name, size)
		if size > qoiLimit {
			t.Errorf("%s does not fit the %d byte buffer with %d bytes", img.name, qoiLimit, size)
		}
	}
}

func TestQOIWithinKeepsPagesThatFit(t *testing.T) {
	img := render.NewCanvas(800, 480, 4)
	render.Text(img, "Keine Termine", 400, 240, render.Style{Size: 32}, 700, color.Black, render.Center)
	got := qoiWithin(img, qoiLimit)
	if !got.fits || got.note != "" {
		t.Fatalf("a small page was changed: fits=%v note=%q", got.fits, got.note)
	}
	if !bytes.Equal(got.data, encodeQOI(img)) {
		t.Fatalf("a small page was not encoded as before")
	}
}

// TestQOIWithinRemovesDither uses a limit far below the firmware's to reach the
// fallback at all: no page of the shipped panels is large enough for it.
func TestQOIWithinRemovesDither(t *testing.T) {
	const limit = 40 * 1024
	img := ditheredPage(800, 480)
	if size := len(encodeQOI(img)); size <= limit {
		t.Fatalf("the test page is only %d bytes, it has to exceed %d", size, limit)
	}
	got := qoiWithin(img, limit)
	if !got.fits || got.note != "without dither" {
		t.Fatalf("the dithered page was not simplified: fits=%v note=%q size=%d", got.fits, got.note, len(got.data))
	}
	if len(got.data) > limit {
		t.Fatalf("simplified page is %d bytes", len(got.data))
	}
	if b := decodeQOI(t, got.data).Bounds(); b != img.Bounds() {
		t.Fatalf("decoded bounds %v, want the display's %v", b, img.Bounds())
	}
}

// TestQOIWithinReportsPagesThatDoNotFit covers the last resort: the page goes
// out as it is and the device keeps its old picture.
func TestQOIWithinReportsPagesThatDoNotFit(t *testing.T) {
	got := qoiWithin(noisyPage(800, 480), 1024)
	if got.fits {
		t.Fatalf("a page of %d bytes reported as fitting 1024", len(got.data))
	}
}

// TestNoticePageStaysSmall is the reported case: every widget of a page fails
// and the page turns into nine hatched notice cells (ADR 0088).
func TestNoticePageStaysSmall(t *testing.T) {
	tmpl := &page.Template{Grid: [2]int{3, 3}}
	for row := 0; row < 3; row++ {
		for col := 0; col < 3; col++ {
			tmpl.Cells = append(tmpl.Cells, page.Cell{At: [2]int{col, row}, Widget: "weather/current"})
		}
	}
	img, err := tmpl.RenderImage(page.Display{Width: 800, Height: 480, Gray: true, PPI: 125}, page.Context{
		Now: time.Now(), Page: 1, Pages: 1,
		Widgets: func(b page.Batch) []page.Result {
			out := make([]page.Result, len(b.Widgets))
			for i := range out {
				out[i] = page.Result{Err: errors.New("plugin exited")}
			}
			return out
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	size := len(encodeQOI(img))
	t.Logf("a page of nine notice cells is %d bytes as QOI", size)
	if size > qoiLimit {
		t.Fatalf("the notice page encodes to %d bytes, above the %d byte buffer", size, qoiLimit)
	}
}

// TestWorstPageOverHTTP covers the server path: the largest page Paperboy can
// draw is served as the QOI it is, not simplified (ADR 0088).
func TestWorstPageOverHTTP(t *testing.T) {
	s, _ := testServer(t)
	img := alternatingPage(800, 480)
	data := encodePNG(img)
	s.rendered["small/gross"] = &renderedPage{data: data, hash: hashBytes(data)}

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pages/small/gross/"+hashBytes(data)+".png?format=qoi", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("qoi response: %d", rec.Code)
	}
	if got := decodeQOI(t, rec.Body.Bytes()); got.Bounds() != img.Bounds() {
		t.Fatalf("page went out at %v, want the display's %v", got.Bounds(), img.Bounds())
	}
	if rec.Body.Len() > qoiLimit {
		t.Fatalf("page answered %d bytes", rec.Body.Len())
	}
}
