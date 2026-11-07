package page

import (
	"errors"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/render"
)

const gridPage = `
title: Test
grid: [4, 2]
style: { padding: 4, divider: 2 }
cells:
  - { at: [0, 0], span: [4, 1], widget: paperboy/header }
  - { at: [0, 1], widget: cal/agenda, args: { lines: 4 }, style: { padding: 0 } }
  - { at: [1, 1], span: [3, 1], widget: solar/chart }
`

func TestParseValidatesGrid(t *testing.T) {
	if _, err := Parse(strings.NewReader(gridPage)); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]string{
		"no grid":   "cells: [{widget: a/b}]\n",
		"no cells":  "grid: [1, 1]\n",
		"outside":   "grid: [2, 1]\ncells: [{at: [1, 0], span: [2, 1], widget: a/b}]\n",
		"overlap":   "grid: [2, 1]\ncells: [{span: [2, 1], widget: a/b}, {at: [1, 0], widget: a/c}]\n",
		"no plugin": "grid: [1, 1]\ncells: [{widget: agenda}]\n",
		"bad name":  "grid: [1, 1]\ncells: [{widget: Cal/agenda}]\n",
		"unknown":   "grid: [1, 1]\ncells: [{widget: a/b, colour: red}]\n",
		"notices":   "grid: [1, 1]\nnotices: bar\ncells: [{widget: a/b}]\n",
		"old rows":  "rows: [1]\nblocks: [{text: x}]\n",
		"actions":   "grid: [1, 1]\nactions: [{action: a/b}]\ncells: [{widget: a/b}]\n",
	} {
		if _, err := Parse(strings.NewReader(bad)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestLoadFallsBackToSharedPage(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.yaml"), []byte("title: shared\ngrid: [1, 1]\ncells: [{widget: a/b}]\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "a.small.yaml"), []byte("title: own\ngrid: [1, 1]\ncells: [{widget: a/b}]\n"), 0o644)
	if p, err := Load(dir, "a", "small"); err != nil || p.Title != "own" {
		t.Fatalf("own variant: %v %v", p, err)
	}
	if p, err := Load(dir, "a", "big"); err != nil || p.Title != "shared" {
		t.Fatalf("shared page: %v %v", p, err)
	}
	if _, err := Load(dir, "b", "big"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing page: %v", err)
	}
}

func gray(img image.Image, x, y int) uint8 {
	return color.GrayModel.Convert(img.At(x, y)).(color.Gray).Y
}

func TestRenderImageComposesCells(t *testing.T) {
	tmpl, err := Parse(strings.NewReader(gridPage))
	if err != nil {
		t.Fatal(err)
	}
	var got Batch
	src := func(b Batch) []Result {
		got = b
		out := make([]Result, len(b.Widgets))
		for i, w := range b.Widgets {
			switch w.Widget {
			case "chart":
				img := image.NewGray(image.Rect(0, 0, w.Width, w.Height))
				for p := range img.Pix {
					img.Pix[p] = 100 // between dark and light gray
				}
				out[i] = Result{Image: img, Stale: true}
			case "header":
				out[i] = Result{Image: image.NewGray(image.Rect(0, 0, w.Width, w.Height))} // black
			default:
				out[i] = Result{Err: errors.New("boom")}
			}
		}
		return out
	}
	img, err := tmpl.RenderImage(Display{Width: 400, Height: 200, Gray: true, PPI: 72}, Context{Now: time.Now(), Page: 2, Pages: 3, Widgets: src})
	if err != nil {
		t.Fatal(err)
	}
	if got.Levels != 4 || got.Text || got.Page.Number != 2 || got.Page.Count != 3 || got.Page.Title != "Test" || got.Page.PPI != 72 {
		t.Fatalf("batch header: %+v", got)
	}
	w := got.Widgets
	if len(w) != 3 || w[0].Width != 400 || w[0].Height != 100 || w[1].Width != 100 || w[2].Width != 300 ||
		w[1].Plugin != "cal" || w[1].Widget != "agenda" || w[1].Args["lines"] != 4 {
		t.Fatalf("widgets: %+v", w)
	}
	if w[0].Style.Padding != 4 || w[1].Style.Padding != 0 || w[0].Style.Rule != 1 {
		t.Fatalf("styles: %+v %+v", w[0].Style, w[1].Style)
	}
	if gray(img, 200, 50) != 0 {
		t.Error("header not pasted")
	}
	if gray(img, 250, 130) != 85 {
		t.Errorf("chart quantized to %d, want 85", gray(img, 250, 130))
	}
	if gray(img, 99, 130) != 0 {
		t.Error("divider between cells missing")
	}
	// A stale widget adds the notice bar at the bottom.
	if gray(img, 2, 198) != 0 {
		t.Error("stale bar missing")
	}
}

func TestRenderImageWithoutSource(t *testing.T) {
	tmpl, _ := Parse(strings.NewReader(gridPage))
	if _, err := tmpl.RenderImage(Display{Width: 400, Height: 200}, Context{}); err != nil {
		t.Fatal(err)
	}
}

func TestRenderText(t *testing.T) {
	tmpl, err := Parse(strings.NewReader(`
kind: text
grid: [10, 3]
cells:
  - { at: [0, 0], span: [10, 1], widget: paperboy/header }
  - { at: [0, 1], span: [6, 2], widget: cal/agenda }
  - { at: [6, 1], span: [4, 2], widget: solar/tiles }
`))
	if err != nil {
		t.Fatal(err)
	}
	src := func(b Batch) []Result {
		if !b.Text || b.Charset != "ascii" {
			t.Fatalf("batch: %+v", b)
		}
		return []Result{
			{Lines: []string{"Übersicht 12:00"}},
			{Lines: []string{"10:00 Schwimmen", "Mo", "ignored"}, Stale: true},
			{Err: errors.New("down")},
		}
	}
	lines, err := tmpl.RenderText(Display{Width: 10, Height: 3, Text: true, Charset: "ascii"}, Context{Widgets: src})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Uebersich!", "10:00 ?   ", "Mo    down"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("lines %q, want %q", lines, want)
	}
	if _, err := tmpl.RenderText(Display{Width: 20, Height: 4, Text: true}, Context{Widgets: src}); err == nil {
		t.Fatal("a grid that does not match the display must fail")
	}
}

func TestBannerOnTinyDisplayStaysBlack(t *testing.T) {
	img := WithBanner(render.NewCanvas(40, 20, 4), "Akku schwach – bitte laden")
	for x := 0; x < 40; x++ {
		if gray(img, x, 19) != 0 {
			t.Fatalf("pixel %d of the bar is %d, want black", x, gray(img, x, 19))
		}
	}
	big := WithBanner(render.NewCanvas(400, 200, 4), "Akku schwach")
	white := 0
	for x := 0; x < 400; x++ {
		if gray(big, x, 190) == 255 {
			white++
		}
	}
	if white == 0 {
		t.Fatal("a large bar must carry the text")
	}
}

func TestFits(t *testing.T) {
	img, _ := Parse(strings.NewReader("grid: [20, 4]\ncells: [{widget: a/b}]\n"))
	txt, _ := Parse(strings.NewReader("kind: text\ngrid: [20, 4]\ncells: [{widget: a/b}]\n"))
	lcd := Display{Width: 20, Height: 4, Text: true}
	if img.Fits(lcd) == nil {
		t.Error("an image page with a matching grid must not fit a text display")
	}
	if txt.Fits(lcd) != nil || txt.Fits(Display{Width: 16, Height: 2, Text: true}) == nil {
		t.Error("text page fit")
	}
	if txt.Fits(Display{Width: 800, Height: 480}) == nil || img.Fits(Display{Width: 800, Height: 480}) != nil {
		t.Error("image display fit")
	}
	if _, err := Parse(strings.NewReader("kind: led\ngrid: [1, 1]\ncells: [{widget: a/b}]\n")); err == nil {
		t.Error("unknown kind accepted")
	}
}
