package page

import (
	"errors"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"
)

const pluginPage = `
rows: [1, 3]
blocks:
  - text: Kopf
  - columns:
      items:
        - weight: 1
          block: { plugin: cal/agenda, padding: 5 }
        - weight: 3
          block:
            plugin: { name: cal, image: month, args: { lines: 4 } }
`

func TestPluginBlockRequestsExactCells(t *testing.T) {
	tmpl, err := Parse(strings.NewReader(pluginPage))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	var got []FragmentRequest
	src := func(reqs []FragmentRequest, gray bool) []Fragment {
		got = reqs
		out := make([]Fragment, len(reqs))
		for i, r := range reqs {
			if r.Image == "month" {
				img := image.NewGray(image.Rect(0, 0, r.Width, r.Height))
				for p := range img.Pix {
					img.Pix[p] = 100 // between dark gray and light gray
				}
				out[i] = Fragment{Image: img}
			} else {
				out[i] = Fragment{Err: errors.New("boom")}
			}
		}
		return out
	}
	img, err := tmpl.RenderContext(testDocument(t, &now), Display{Width: 400, Height: 200, Gray: true}, Context{Now: now, Fragments: src})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("requests: %+v", got)
	}
	if got[0].Width != 90 || got[0].Height != 140 || got[0].Plugin != "cal" || got[0].Image != "agenda" {
		t.Errorf("agenda request %+v, want 90x140", got[0])
	}
	if got[1].Width != 300 || got[1].Height != 150 || got[1].Args["lines"] != 4 {
		t.Errorf("month request %+v, want 300x150 with args", got[1])
	}
	// The month fragment is quantized to dark gray (85) without dithering.
	for _, p := range []image.Point{{100, 50}, {399, 199}, {250, 120}} {
		if c := color.GrayModel.Convert(img.At(p.X, p.Y)).(color.Gray); c.Y != 85 {
			t.Errorf("pixel %v = %d, want 85", p, c.Y)
		}
	}
	// The failing agenda becomes a hatched notice inside its cell.
	if c := color.GrayModel.Convert(img.At(5, 50)).(color.Gray); c.Y != 0 && c.Y != 255 {
		t.Errorf("notice pixel = %d", c.Y)
	}
}

func TestPluginBlockWithoutSourceRenders(t *testing.T) {
	tmpl, err := Parse(strings.NewReader(pluginPage))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := tmpl.RenderContext(testDocument(t, &now), Display{Width: 400, Height: 200}, Context{Now: now}); err != nil {
		t.Fatal(err)
	}
}

func TestPluginBlockValidation(t *testing.T) {
	for _, bad := range []string{"plugin: cal", "plugin: Cal/x", "plugin: {name: cal, image: m, padding: -1}"} {
		if _, err := Parse(strings.NewReader("rows: [1]\nblocks:\n  - " + bad + "\n")); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
