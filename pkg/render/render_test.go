package render

import (
	"image"
	"image/color"
	"testing"
)

func TestGray16Palette(t *testing.T) {
	if len(Gray16Palette) != 16 {
		t.Fatalf("palette has %d tones, want 16", len(Gray16Palette))
	}
	for i, c := range Gray16Palette {
		g, ok := c.(color.Gray)
		if !ok {
			t.Fatalf("tone %d is %T, want color.Gray", i, c)
		}
		want := uint8(i * 255 / 15)
		if g.Y != want {
			t.Errorf("tone %d = %d, want %d", i, g.Y, want)
		}
	}
	// The four-level palette stays part of the 16-level one, so a 4-level
	// page keeps its exact grays on a 16-level panel.
	for _, want := range []color.Gray{DarkGray, LightGray} {
		found := false
		for _, c := range Gray16Palette {
			if c == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("gray %d is missing from the 16-level palette", want.Y)
		}
	}
}

func TestNewCanvasLevels(t *testing.T) {
	for _, tc := range []struct {
		levels int
		want   int
	}{
		{1, 2},
		{2, 2},
		{4, 4},
		{16, 16},
		{8, 16},
	} {
		img := NewCanvas(3, 2, tc.levels)
		if got := Levels(img.Palette); got != tc.want {
			t.Errorf("NewCanvas(%d): %d tones, want %d", tc.levels, got, tc.want)
		}
		if r, _, _, a := img.At(1, 1).RGBA(); r != 0xffff || a != 0xffff {
			t.Errorf("NewCanvas(%d) starts as %d/%d, want white", tc.levels, r, a)
		}
	}
}

func TestQuantizeSixteenLevels(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 256, 1))
	for x := 0; x < 256; x++ {
		src.SetGray(x, 0, color.Gray{uint8(x)})
	}
	out := Quantize(src, 16)
	for x := 0; x < 256; x++ {
		want := (x*15 + 127) / 255
		if got := int(out.Pix[x]); got != want {
			t.Fatalf("pixel %d: index %d, want %d", x, got, want)
		}
	}
	if _, ok := out.At(255, 0).(color.Gray); !ok {
		t.Fatal("16-level quantization did not keep gray colors")
	}
}

func TestQuantizeSixteenLevelsTransparentIsWhite(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1, 1))
	out := Quantize(src, 16)
	if got := int(out.Pix[0]); got != 15 {
		t.Fatalf("transparent pixel became index %d, want white", got)
	}
}
