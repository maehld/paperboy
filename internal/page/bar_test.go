package page

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

// renderBar draws a bar into a fresh canvas. The value is stored under "soc";
// present=false leaves the path missing.
func renderBar(t *testing.T, value any, present bool, b *Bar, w, h int, gray bool, padding *int) *image.Paletted {
	t.Helper()
	root := map[string]any{}
	if present {
		root["soc"] = value
	}
	r := &renderer{t: &Template{}, root: root, f: formatter{comma: true}, language: "de"}
	img := newCanvas(w, h, gray)
	r.bar(img, img.Rect, b, padding)
	if r.err != nil {
		t.Fatalf("bar failed: %v", r.err)
	}
	return img
}

// whiteIn counts white pixels in r.
func whiteIn(img *image.Paletted, r image.Rectangle) int {
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if c, ok := img.Palette[img.ColorIndexAt(x, y)].(color.Gray); ok && c.Y == 255 {
				n++
			}
		}
	}
	return n
}

func TestBarFullValueFillsBody(t *testing.T) {
	b := &Bar{Value: "soc", Max: 100, Border: 4, Fill: "solid"}
	img := renderBar(t, 100.0, true, b, 40, 100, true, nil)
	// Padding 4 and a 4 px frame leave (8,8)-(32,92) as the inner body.
	body := image.Rect(8, 8, 32, 92)
	if got, want := blackIn(img, body), body.Dx()*body.Dy(); got != want {
		t.Fatalf("100%% fill: %d of %d pixels are black", got, want)
	}
	if whiteIn(img, body) != 0 {
		t.Fatalf("100%% fill left %d white pixels in the body", whiteIn(img, body))
	}
}

func TestBarZeroStaysWhite(t *testing.T) {
	b := &Bar{Value: "soc", Max: 100, Border: 4, Fill: "solid"}
	img := renderBar(t, 0.0, true, b, 40, 100, true, nil)
	body := image.Rect(8, 8, 32, 92)
	if got := whiteIn(img, body); got != body.Dx()*body.Dy() {
		t.Fatalf("0%% fill: %d of %d pixels are white", got, body.Dx()*body.Dy())
	}
}

func TestBarClampsValues(t *testing.T) {
	b := &Bar{Value: "soc", Max: 100, Fill: "shade"}
	high := renderBar(t, 150.0, true, b, 40, 100, true, nil)
	full := renderBar(t, 100.0, true, b, 40, 100, true, nil)
	if !bytes.Equal(high.Pix, full.Pix) {
		t.Fatal("a value above max must render like max")
	}
	low := renderBar(t, -20.0, true, b, 40, 100, true, nil)
	empty := renderBar(t, 0.0, true, b, 40, 100, true, nil)
	if !bytes.Equal(low.Pix, empty.Pix) {
		t.Fatal("a value below min must render like min")
	}
}

func TestBarOrientationsDiffer(t *testing.T) {
	vertical := &Bar{Value: "soc", Max: 100, Orientation: "vertical", Fill: "shade"}
	horizontal := &Bar{Value: "soc", Max: 100, Orientation: "horizontal", Fill: "shade"}
	v := renderBar(t, 50.0, true, vertical, 60, 60, true, nil)
	h := renderBar(t, 50.0, true, horizontal, 60, 60, true, nil)
	if bytes.Equal(v.Pix, h.Pix) {
		t.Fatal("vertical and horizontal bars must differ")
	}
	if whiteIn(h, image.Rect(4, 4, 30, 56)) != 0 {
		t.Fatal("horizontal bar must fill from the left")
	}
	if whiteIn(h, image.Rect(34, 4, 56, 56)) == 0 {
		t.Fatal("horizontal bar must leave the right side empty")
	}
}

func TestBarCapAndBorderDrawPixels(t *testing.T) {
	b := &Bar{Value: "soc", Max: 100, Border: 4, Cap: true, Fill: "none"}
	img := renderBar(t, 0.0, true, b, 40, 100, true, nil)
	// The cap sits above the body between the padding and the frame.
	if blackIn(img, image.Rect(4, 4, 36, 8)) == 0 {
		t.Fatal("cap did not draw pixels")
	}
	// The body starts below the cap, so its frame runs from y=8 to y=96.
	if img.ColorIndexAt(4, 8) != 0 || img.ColorIndexAt(35, 95) != 0 {
		t.Fatal("border did not draw the frame corners")
	}
	inner := image.Rect(8, 12, 32, 92)
	if got := whiteIn(img, inner); got != inner.Dx()*inner.Dy() {
		t.Fatalf("empty fill must leave the inner body white (%d white pixels)", got)
	}
}

func TestBarMonoShadeIsCheckerboard(t *testing.T) {
	b := &Bar{Value: "soc", Max: 100} // empty fill means shade
	img := renderBar(t, 80.0, true, b, 40, 100, false, nil)
	filled := image.Rect(4, 22, 36, 96) // 80 % of the inner body, from the bottom
	if blackIn(img, filled) == 0 {
		t.Fatal("mono shade must contain black pixels")
	}
	if whiteIn(img, filled) == 0 {
		t.Fatal("mono shade must contain white pixels (checkerboard)")
	}
}

func TestBarMissingValueDoesNotFail(t *testing.T) {
	b := &Bar{Value: "soc", Max: 100, Border: 2, Fill: "solid", Label: "Battery"}
	img := renderBar(t, nil, false, b, 40, 100, true, nil)
	if got := whiteIn(img, image.Rect(6, 6, 34, 77)); got != image.Rect(6, 6, 34, 77).Dx()*image.Rect(6, 6, 34, 77).Dy() {
		t.Fatalf("missing value must leave the body white (%d white pixels)", got)
	}
	// The label still renders instead of failing the page.
	if blackIn(img, image.Rect(4, 79, 36, 96)) == 0 {
		t.Fatal("label must render when the value is missing")
	}
}

func TestBarDeterministic(t *testing.T) {
	b := &Bar{Value: "soc", Max: 100, Border: 3, Cap: true, Fill: "hatch", Label: "{{ percent .soc }}"}
	first := renderBar(t, 42.0, true, b, 40, 120, true, nil)
	second := renderBar(t, 42.0, true, b, 40, 120, true, nil)
	if !bytes.Equal(first.Pix, second.Pix) {
		t.Fatal("two renders of the same bar differ")
	}
}
