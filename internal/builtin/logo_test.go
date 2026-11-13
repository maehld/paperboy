package builtin

import (
	"image"
	"testing"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

func TestMarkInk(t *testing.T) {
	cases := []struct {
		name string
		x, y float64
		want bool
	}{
		{"frame top left", 1, 1, true},
		{"hole", 28, 24, false},
		{"frame left", 1, 40, true},
		{"drawn cell", 8, 40, true},
		{"vertical divider", 16, 10, true},
		{"horizontal divider", 40, 16, true},
		{"corner fold", 52, 8, true},
		{"cut corner", 62, 2, false},
		{"outside", 64, 24, false},
	}
	for _, c := range cases {
		if got := markInk(c.x, c.y); got != c.want {
			t.Errorf("%s (%v, %v): got %v, want %v", c.name, c.x, c.y, got, c.want)
		}
	}
}

func TestLogoFitsTheCell(t *testing.T) {
	// 320x240 is exactly the mark's aspect ratio, so one unit is five pixels
	// and the corners of the shapes are predictable.
	img := render.NewCanvas(320, 240, 2)
	if err := (Plugin{}).Render(nil, plugin.WidgetRequest{Widget: "logo"}, img); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		x, y int
		ink  bool
	}{
		{"frame", 5, 5, true},
		{"hole", 150, 120, false},
		{"cut corner", 310, 10, false},
		{"drawn cell", 40, 200, true},
		{"bottom frame", 160, 235, true},
	} {
		got := img.ColorIndexAt(c.x, c.y) == 0
		if got != c.ink {
			t.Errorf("%s at (%d, %d): ink %v, want %v", c.name, c.x, c.y, got, c.ink)
		}
	}
}

func TestLogoAntialiasesOnFourLevels(t *testing.T) {
	img := render.NewCanvas(133, 100, 4)
	if err := (Plugin{}).Render(nil, plugin.WidgetRequest{Widget: "logo"}, img); err != nil {
		t.Fatal(err)
	}
	// The diagonal cut of the corner lands between pixels: the four-level
	// palette keeps its edges as gray, a 1-bit panel rounds them.
	gray := false
	for y := 0; y < img.Rect.Dy() && !gray; y++ {
		for x := 0; x < img.Rect.Dx(); x++ {
			if idx := img.ColorIndexAt(x, y); idx == 1 || idx == 2 {
				gray = true
				break
			}
		}
	}
	if !gray {
		t.Error("no gray level drawn")
	}
}

func TestLogoWordmarkExtendsTheMark(t *testing.T) {
	mark := render.NewCanvas(400, 120, 2)
	if err := (Plugin{}).Render(nil, plugin.WidgetRequest{Widget: "logo"}, mark); err != nil {
		t.Fatal(err)
	}
	lockup := render.NewCanvas(400, 120, 2)
	w := plugin.WidgetRequest{Widget: "logo", Args: map[string]any{"wordmark": true}}
	if err := (Plugin{}).Render(nil, w, lockup); err != nil {
		t.Fatal(err)
	}
	if inkRight(mark, 300) {
		t.Error("the mark alone has ink right of its right half")
	}
	if !inkRight(lockup, 300) {
		t.Error("the lockup has no wordmark")
	}
	// Nothing may leave the cell, and the lockup keeps its own aspect ratio.
	if inkRight(lockup, 400) {
		t.Error("the lockup draws outside the cell")
	}
}

// inkRight reports whether the right half of img, starting at x, has any ink.
func inkRight(img *image.Paletted, x int) bool {
	for y := 0; y < img.Rect.Dy(); y++ {
		for i := x; i < img.Rect.Dx(); i++ {
			if img.ColorIndexAt(i, y) != uint8(len(img.Palette)-1) {
				return true
			}
		}
	}
	return false
}
