package main

import (
	"errors"
	"image"
	"testing"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

// cellWidth and cellHeight are one cell of the reference grid on the e1001
// display (800x480 at 125 ppi).
const (
	cellWidth  = 800 / 6
	cellHeight = 480 / 8
	textPx     = 16.0 * 125.0 / 72.0
)

// drawWidget draws one widget into a canvas of the cell size.
func drawWidget(t *testing.T, v *vicare, name string, w, h int, args map[string]any) (*image.Paletted, error) {
	t.Helper()
	img := render.NewCanvas(w, h, 4)
	req := plugin.WidgetRequest{Widget: name, Width: w, Height: h, Args: args,
		Style: plugin.Style{Padding: 8, Rule: 1, TextPx: textPx}}
	return img, v.Render(v.req, req, img)
}

// ink counts the pixels that are not white.
func ink(img *image.Paletted) int {
	white := uint8(len(img.Palette) - 1)
	n := 0
	for _, p := range img.Pix {
		if p != white {
			n++
		}
	}
	return n
}

func TestRenderEveryWidgetAtEverySize(t *testing.T) {
	for _, lang := range []string{"de", "en"} {
		v := testPlugin(t, lang)
		for _, wi := range v.Describe().Widgets {
			for _, span := range wi.Sizes {
				w, h := span.Columns*cellWidth, span.Rows*cellHeight
				img, err := drawWidget(t, v, wi.Widget, w, h, nil)
				if err != nil {
					t.Fatalf("%s %dx%d (%s): %v", wi.Widget, w, h, lang, err)
				}
				if n := ink(img); n < 30 {
					t.Errorf("%s %dx%d (%s): only %d ink pixels", wi.Widget, w, h, lang, n)
				}
			}
		}
	}
}

func TestRenderTextAndImageWidgetsOnOneBit(t *testing.T) {
	v := testPlugin(t, "de")
	img := render.NewCanvas(400, 120, 2)
	err := v.Render(v.req, plugin.WidgetRequest{Widget: "curve", Width: 400, Height: 120,
		Style: plugin.Style{Padding: 8, Rule: 1, TextPx: textPx}}, img)
	if err != nil {
		t.Fatal(err)
	}
	if ink(img) < 100 {
		t.Fatalf("only %d ink pixels", ink(img))
	}
}

func TestRenderTooSmall(t *testing.T) {
	v := testPlugin(t, "de")
	if _, err := drawWidget(t, v, "heating", 50, 30, nil); !errors.Is(err, plugin.ErrTooSmall) {
		t.Fatalf("err = %v", err)
	}
	if _, err := drawWidget(t, v, "curve", 80, 40, nil); !errors.Is(err, plugin.ErrTooSmall) {
		t.Fatalf("curve err = %v", err)
	}
}

func TestRenderUnknownWidget(t *testing.T) {
	v := testPlugin(t, "de")
	if _, err := drawWidget(t, v, "nope", 400, 120, nil); !errors.Is(err, plugin.ErrUnknownWidget) {
		t.Fatalf("err = %v", err)
	}
}

func TestRenderWithoutAHeatingCurve(t *testing.T) {
	v := testPlugin(t, "de")
	v.cur.Circuits[0].Curve = nil
	if _, err := drawWidget(t, v, "curve", 400, 120, nil); err != nil {
		t.Fatalf("a device without a curve must show a notice: %v", err)
	}
}

func TestRenderWithoutDWHOrHeatGenerator(t *testing.T) {
	v := testPlugin(t, "de")
	v.cur.DHW = nil
	v.cur.Compressor = nil
	for _, name := range []string{"dhw", "burner"} {
		if _, err := drawWidget(t, v, name, 400, 120, nil); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestCurveTargetMatchesTheFirmwareShapes(t *testing.T) {
	// Values from PyViCare's formula tests: 0.6 / 4 K at 21 °C inside.
	if got := curveTarget(0.6, 4, 21, 4.2, 1); got < 36.5 || got > 36.8 {
		t.Fatalf("target = %v", got)
	}
	if got, want := curveTarget(0.6, 4, 21, 4.2, 2), curveTarget(0.6, 4, 21, 4.2, 1)+5; got != want {
		t.Fatalf("variant 2 = %v, want %v", got, want)
	}
	if got := curveTarget(1.2, 4, 21, -20, 1); got < 73 || got > 75 {
		t.Fatalf("cold target = %v", got)
	}
}

func TestCurveVariantFollowsTheRoles(t *testing.T) {
	roles := []string{"type:heatpump"}
	cases := []struct {
		want     string
		roles    []string
		circuits int
		wantV    int
	}{
		{"auto", roles, 1, 2},
		{"auto", roles, 2, 1},
		{"auto", nil, 1, 1},
		{"boiler", roles, 1, 1},
		{"heatpump", nil, 2, 2},
	}
	for _, c := range cases {
		if got := curveVariant(c.want, c.roles, c.circuits); got != c.wantV {
			t.Errorf("curveVariant(%q, %v, %d) = %d, want %d", c.want, c.roles, c.circuits, got, c.wantV)
		}
	}
}
