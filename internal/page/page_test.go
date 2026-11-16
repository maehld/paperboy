package page

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

const gridPage = `
title: Test
kind: image
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

func TestParseNeedsKindAndValidVariants(t *testing.T) {
	if _, err := Parse(strings.NewReader("grid: [1, 1]\ncells: [{widget: a/b}]\n")); err == nil || !strings.Contains(err.Error(), "kind is required") {
		t.Fatalf("missing kind: %v", err)
	}
	for name, bad := range map[string]string{
		"display":   "kind: image\ngrid: [1, 1]\nvariants: {Sticky: s}\ncells: [{widget: a/b}]\n",
		"label":     "kind: image\ngrid: [1, 1]\nvariants: {sticky: S}\ncells: [{widget: a/b}]\n",
		"duplicate": "kind: image\ngrid: [1, 1]\nvariants: {sticky: a, sticky: b}\ncells: [{widget: a/b}]\n",
	} {
		if _, err := Parse(strings.NewReader(bad)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestParseVariantRejectsTitleAndVariants(t *testing.T) {
	for name, bad := range map[string]string{
		"title":    "title: x\nkind: image\ngrid: [1, 1]\ncells: [{widget: a/b}]\n",
		"variants": "kind: image\ngrid: [1, 1]\nvariants: {small: s}\ncells: [{widget: a/b}]\n",
	} {
		if _, err := ParseVariant(strings.NewReader(bad)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	variant := "kind: image\ngrid: [1, 1]\ncells: [{widget: a/b}]\n"
	if _, err := ParseVariant(strings.NewReader(variant)); err != nil {
		t.Errorf("a variant without a title: %v", err)
	}
	if _, err := Parse(strings.NewReader(variant)); err != nil {
		t.Errorf("a page may omit its title: %v", err)
	}
}

// TestLoadForNamesVariants explains the resolution of ADR 0110: the page file
// serves its kind, and the variants map names the files that serve the rest.
func TestLoadForNamesVariants(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.yaml", "title: shared\nkind: image\ngrid: [1, 1]\nvariants: {small: hoch}\ncells: [{widget: a/b}]\n")
	write("a.hoch.yaml", "kind: image\ngrid: [1, 2]\ncells: [{widget: a/b}]\n")
	if p, err := LoadFor(dir, "a", "small"); err != nil || p.Title != "shared" || p.Grid != [2]int{1, 2} {
		t.Fatalf("variant: %+v %v", p, err)
	}
	if p, err := LoadFor(dir, "a", "big"); err != nil || p.Grid != [2]int{1, 1} {
		t.Fatalf("page file: %+v %v", p, err)
	}
	if _, err := LoadFor(dir, "b", "big"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing page: %v", err)
	}
	write("c.yaml", "kind: image\ngrid: [1, 1]\nvariants: {small: gone}\ncells: [{widget: a/b}]\n")
	if _, err := LoadFor(dir, "c", "small"); !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "gone") {
		t.Fatalf("missing variant: %v", err)
	}
}

func gray(img image.Image, x, y int) uint8 {
	return color.GrayModel.Convert(img.At(x, y)).(color.Gray).Y
}

func TestRenderImageSixteenLevels(t *testing.T) {
	tmpl, err := Parse(strings.NewReader(gridPage))
	if err != nil {
		t.Fatal(err)
	}
	src := func(b Batch) []Result {
		out := make([]Result, len(b.Widgets))
		for i, w := range b.Widgets {
			img := image.NewGray(image.Rect(0, 0, w.Width, w.Height))
			if w.Widget == "chart" {
				for p := range img.Pix {
					img.Pix[p] = 100 // rounds to 102 on 16 levels
				}
			}
			out[i] = Result{Image: img}
		}
		return out
	}
	img, err := tmpl.RenderImage(Display{Width: 400, Height: 200, Levels: 16, PPI: 72},
		Context{Now: time.Now(), Page: 1, Pages: 1, Widgets: src})
	if err != nil {
		t.Fatal(err)
	}
	if len(img.Palette) != 16 {
		t.Fatalf("page has %d tones, want 16", len(img.Palette))
	}
	if got := gray(img, 250, 130); got != 102 {
		t.Errorf("chart quantized to %d, want 102", got)
	}
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
	img, err := tmpl.RenderImage(Display{Width: 400, Height: 200, Levels: 4, PPI: 72}, Context{Now: time.Now(), Page: 2, Pages: 3, Widgets: src})
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

func valueSource(values map[string]ValueResult) ValueSource {
	return func(b ValueBatch) []Result {
		out := make([]Result, len(b.Values))
		for i, v := range b.Values {
			r, ok := values[v.Plugin+"/"+v.Name]
			if !ok {
				out[i] = Result{Err: fmt.Errorf("value %s/%s is not declared", v.Plugin, v.Name)}
				continue
			}
			out[i] = r.Result()
		}
		return out
	}
}

// ValueResult is one value with its declaration for a test page.
type ValueResult struct {
	Value plugin.Value
	Spec  plugin.ValueSpec
	Err   error
	Stale bool
}

func (v ValueResult) Result() Result {
	return Result{Value: &v.Value, Spec: v.Spec, Err: v.Err, Stale: v.Stale}
}

func TestRenderTextBoxes(t *testing.T) {
	tmpl, err := Parse(strings.NewReader(`
kind: text
cells:
  - { at: [0, 0], span: [10, 1], text: Titel }
  - { at: [0, 1], span: [10, 1], value: a/temp, label: T, align: decimal }
  - { at: [0, 2], span: [10, 1], text: Ende, align: right }
`))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.November, 15, 12, 0, 0, 0, time.UTC)
	src := valueSource(map[string]ValueResult{
		"a/temp": {Value: plugin.NumberValue(3.5), Spec: plugin.ValueSpec{Name: "temp", Type: plugin.ValueNumber, Unit: "kW", Precision: 1}},
	})
	text, err := tmpl.RenderText(Display{Width: 10, Height: 3, Text: true, Language: "de", Timezone: time.UTC}, Context{Now: now, Values: src})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Titel     ", "T   3,5 kW", "      Ende"}
	if strings.Join(text.Lines, "|") != strings.Join(want, "|") {
		t.Fatalf("lines %q, want %q", text.Lines, want)
	}
	if len(text.Scrolls) != 0 {
		t.Fatalf("static boxes scroll: %+v", text.Scrolls)
	}
}

// TestTextUnitProgression covers the step choice and the unit text of a value
// (ADR 0122).
func TestTextUnitProgression(t *testing.T) {
	pv := plugin.ValueSpec{Name: "pv", Type: plugin.ValueNumber, Unit: "W", Precision: 0,
		Progression: []plugin.UnitStep{
			{Unit: "kW", Factor: 1000, Precision: 1},
			{Unit: "MW", Factor: 1000000, Precision: 2},
		}}
	plain := plugin.ValueSpec{Name: "pv", Type: plugin.ValueNumber, Unit: "W", Precision: 0}
	cases := []struct {
		name  string
		cell  string
		spec  plugin.ValueSpec
		value float64
		want  string
	}{
		{"rounds 999.6 W up into kW", "value: a/pv", pv, 999.6, "1,0 kW"},
		{"zero stays in the base unit", "value: a/pv", pv, 0, "0 W"},
		{"a negative chooses by magnitude", "value: a/pv", pv, -4200, "-4,2 kW"},
		{"a small value stays in the base unit", "value: a/pv", pv, 420, "420 W"},
		{"a forced base step", "value: a/pv, scale: W", pv, 4200, "4200 W"},
		{"a forced larger step", "value: a/pv, scale: MW", pv, 2500000, "2,50 MW"},
		{"a hidden unit", "value: a/pv, unit: none", pv, 4200, "4,2"},
		{"a label over the chosen step", "value: a/pv, unit: kWp", pv, 4200, "4,2 kWp"},
		{"no progression stays in the base unit", "value: a/pv", plain, 4200, "4200 W"},
	}
	for _, tc := range cases {
		tmpl, err := Parse(strings.NewReader("kind: text\ncells:\n  - { at: [0, 0], span: [14, 1], " + tc.cell + " }\n"))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		src := valueSource(map[string]ValueResult{
			"a/pv": {Value: plugin.NumberValue(tc.value), Spec: tc.spec},
		})
		text, err := tmpl.RenderText(Display{Width: 14, Height: 1, Text: true, Language: "de", Timezone: time.UTC}, Context{Values: src})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := strings.TrimRight(text.Lines[0], " "); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// An unknown scale marks its cell with an error, like an unknown argument.
func TestTextUnknownScale(t *testing.T) {
	tmpl, err := Parse(strings.NewReader(`
kind: text
cells:
  - { at: [0, 0], span: [30, 1], value: a/pv, scale: hp }
`))
	if err != nil {
		t.Fatal(err)
	}
	spec := plugin.ValueSpec{Name: "pv", Type: plugin.ValueNumber, Unit: "W", Precision: 0,
		Progression: []plugin.UnitStep{{Unit: "kW", Factor: 1000, Precision: 1}}}
	src := valueSource(map[string]ValueResult{
		"a/pv": {Value: plugin.NumberValue(4200), Spec: spec},
	})
	text, err := tmpl.RenderText(Display{Width: 30, Height: 1, Text: true, Language: "de"}, Context{Values: src})
	if err != nil {
		t.Fatal(err)
	}
	want := `? unknown scale "hp"`
	if got := strings.TrimRight(text.Lines[0], " "); got != want {
		t.Fatalf("line %q, want %q", got, want)
	}
}

func TestTextValueLines(t *testing.T) {
	tmpl, err := Parse(strings.NewReader(`
kind: text
cells:
  - { at: [0, 0], span: [10, 2], value: a/list, label: L }
`))
	if err != nil {
		t.Fatal(err)
	}
	src := valueSource(map[string]ValueResult{
		"a/list": {Value: plugin.TextValue("Bus 1\nBus 2\nBus 3"), Spec: plugin.ValueSpec{Name: "list", Type: plugin.ValueText}},
	})
	text, err := tmpl.RenderText(Display{Width: 10, Height: 2, Text: true}, Context{Values: src})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"L Bus 1   ", "Bus 2     "}
	if strings.Join(text.Lines, "|") != strings.Join(want, "|") {
		t.Fatalf("lines %q, want %q", text.Lines, want)
	}
}

func TestTextValueArguments(t *testing.T) {
	tmpl, err := Parse(strings.NewReader(`
kind: text
cells:
  - { at: [0, 0], span: [10, 1], value: a/text, args: { n: 1 } }
  - { at: [0, 1], span: [10, 1], value: a/text, args: { n: 2 } }
`))
	if err != nil {
		t.Fatal(err)
	}
	var batches []ValueBatch
	src := func(b ValueBatch) []Result {
		batches = append(batches, b)
		out := make([]Result, len(b.Values))
		for i, v := range b.Values {
			n := fmt.Sprint(v.Args["n"])
			val := plugin.TextValue("n=" + n)
			out[i] = Result{Value: &val, Spec: plugin.ValueSpec{Name: v.Name, Type: plugin.ValueText}}
		}
		return out
	}
	text, err := tmpl.RenderText(Display{Width: 10, Height: 2, Text: true}, Context{Values: src})
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 1 || len(batches[0].Values) != 2 {
		t.Fatalf("batches %+v, want one request with two values", batches)
	}
	if got := fmt.Sprint(batches[0].Values[0].Args["n"]); got != "1" {
		t.Errorf("first value args n = %s, want 1", got)
	}
	want := []string{"n=1       ", "n=2       "}
	if strings.Join(text.Lines, "|") != strings.Join(want, "|") {
		t.Fatalf("lines %q, want %q", text.Lines, want)
	}
}

func TestTextArgumentsAreForValueCellsOnly(t *testing.T) {
	_, err := Parse(strings.NewReader(`
kind: text
cells:
  - { at: [0, 0], span: [10, 1], text: hello, args: { n: 1 } }
`))
	if err == nil || !strings.Contains(err.Error(), "literal text cell has no label, unit, scale or args") {
		t.Fatalf("literal text with args: %v", err)
	}
}

func TestTextOverflow(t *testing.T) {
	cases := []struct {
		name   string
		cell   string
		width  int
		height int
		want   []string
		scroll bool
		dir    string
		full   []string
		align  string
	}{
		{"strip", "text: abcdefgh, at: [0, 0], span: [4, 2]", 4, 2, []string{"abcd", "    "}, false, "", nil, ""},
		{"wrap", "text: \"one two three four\", at: [0, 0], span: [6, 3], overflow: wrap", 6, 3, []string{"one   ", "two   ", "three "}, false, "", nil, ""},
		{"right", "text: x, at: [0, 0], span: [4, 1], align: right", 4, 1, []string{"   x"}, false, "", nil, ""},
		{"horizontal", "text: abcdef, at: [0, 0], span: [4, 1], overflow: scroll", 4, 1, []string{"abcd"}, true, ScrollHorizontal, []string{"abcdef"}, "left"},
		{"vertical", "text: \"a\\nb\\nc\", at: [0, 0], span: [4, 2], overflow: scroll, align: right", 4, 2, []string{"   a", "   b"}, true, ScrollVertical, []string{"a", "b", "c"}, "right"},
		{"both", "text: \"abcdef\\nghijkl\", at: [0, 0], span: [4, 1], overflow: scroll", 4, 1, []string{"abcd"}, true, ScrollBoth, []string{"abcdef", "ghijkl"}, "left"},
	}
	for _, tc := range cases {
		tmpl, err := Parse(strings.NewReader("kind: text\ncells:\n  - { " + tc.cell + " }\n"))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		text, err := tmpl.RenderText(Display{Width: tc.width, Height: tc.height, Text: true}, Context{})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if strings.Join(text.Lines, "|") != strings.Join(tc.want, "|") {
			t.Errorf("%s: lines %q, want %q", tc.name, text.Lines, tc.want)
		}
		if tc.scroll {
			if len(text.Scrolls) != 1 || text.Scrolls[0].Direction != tc.dir {
				t.Errorf("%s: scrolls %+v", tc.name, text.Scrolls)
			} else if strings.Join(text.Scrolls[0].Lines, "|") != strings.Join(tc.full, "|") {
				t.Errorf("%s: full %q, want %q", tc.name, text.Scrolls[0].Lines, tc.full)
			} else if text.Scrolls[0].Align != tc.align {
				t.Errorf("%s: align %q, want %q", tc.name, text.Scrolls[0].Align, tc.align)
			}
		} else if len(text.Scrolls) != 0 {
			t.Errorf("%s: unexpected scrolls %+v", tc.name, text.Scrolls)
		}
	}
}

func TestTextPageCellsParse(t *testing.T) {
	valid := `
kind: text
overflow: wrap
cells:
  - { at: [0, 0], span: [4, 1], text: "Hallo" }
  - { at: [4, 0], span: [6, 2], value: a/b, label: L, unit: kW, scale: auto, align: right, overflow: scroll }
`
	tmpl, err := Parse(strings.NewReader(valid))
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Overflow != OverflowWrap || len(tmpl.Cells) != 2 || tmpl.Cells[0].At != [2]int{0, 0} ||
		tmpl.Cells[1].Span != [2]int{6, 2} || tmpl.Cells[1].Overflow != OverflowScroll {
		t.Fatalf("parsed: %+v", tmpl)
	}
	for name, bad := range map[string]string{
		"grid":       "kind: text\ngrid: [2, 2]\ncells:\n  - { at: [0, 0], span: [2, 1], text: x }\n",
		"rows":       "kind: text\nrows:\n  - [{ value: a/b, width: 2 }]\n",
		"no cells":   "kind: text\n",
		"neither":    "kind: text\ncells:\n  - { at: [0, 0], span: [2, 1] }\n",
		"both":       "kind: text\ncells:\n  - { at: [0, 0], span: [2, 1], text: x, value: a/b }\n",
		"widget":     "kind: text\ncells:\n  - { at: [0, 0], span: [2, 1], text: x, widget: a/b }\n",
		"label text": "kind: text\ncells:\n  - { at: [0, 0], span: [2, 1], text: x, label: L }\n",
		"scale text": "kind: text\ncells:\n  - { at: [0, 0], span: [2, 1], text: x, scale: auto }\n",
		"outside":    "kind: text\ncells:\n  - { at: [79, 0], span: [2, 1], text: x }\n",
		"overlap":    "kind: text\ncells:\n  - { at: [0, 0], span: [2, 2], text: x }\n  - { at: [1, 1], span: [2, 1], text: y }\n",
		"align":      "kind: text\ncells:\n  - { at: [0, 0], span: [2, 1], text: x, align: center }\n",
		"overflow":   "kind: text\ncells:\n  - { at: [0, 0], span: [2, 1], text: x, overflow: zoom }\n",
		"scroll key": "kind: text\ncells:\n  - { at: [0, 0], span: [2, 1], text: x, scroll: true }\n",
	} {
		if _, err := Parse(strings.NewReader(bad)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := Parse(strings.NewReader("kind: image\ngrid: [2, 2]\ncells:\n  - { at: [0, 0], span: [2, 1], text: x }\n")); err == nil {
		t.Error("an image page with literal text was accepted")
	}
	if _, err := Parse(strings.NewReader("kind: image\ngrid: [2, 2]\ncells:\n  - { at: [0, 0], span: [2, 1], widget: a/b, scroll: true }\n")); err == nil {
		t.Error("an image page with scroll was accepted")
	}
	if _, err := Parse(strings.NewReader("kind: image\ngrid: [2, 2]\ncells:\n  - { at: [0, 0], span: [2, 1], widget: a/b, scale: auto }\n")); err == nil {
		t.Error("an image page with scale was accepted")
	}
}

func TestFits(t *testing.T) {
	img, err := Parse(strings.NewReader("kind: image\ngrid: [20, 4]\ncells: [{widget: a/b}]\n"))
	if err != nil {
		t.Fatal(err)
	}
	txt, err := Parse(strings.NewReader("kind: text\ncells:\n  - { at: [0, 0], span: [20, 4], text: x }\n"))
	if err != nil {
		t.Fatal(err)
	}
	lcd := Display{Width: 20, Height: 4, Text: true}
	if img.Fits(lcd) == nil {
		t.Error("an image page must not fit a text display")
	}
	if txt.Fits(lcd) != nil || txt.Fits(Display{Width: 16, Height: 2, Text: true}) == nil {
		t.Error("text page fit")
	}
	wide, _ := Parse(strings.NewReader("kind: text\ncells:\n  - { at: [18, 0], span: [3, 1], text: x }\n"))
	if wide.Fits(Display{Width: 20, Height: 4, Text: true}) == nil {
		t.Error("a box outside the display was accepted")
	}
	if txt.Fits(Display{Width: 800, Height: 480}) == nil || img.Fits(Display{Width: 800, Height: 480}) != nil {
		t.Error("image display fit")
	}
	if _, err := Parse(strings.NewReader("kind: led\ngrid: [1, 1]\ncells: [{widget: a/b}]\n")); err == nil {
		t.Error("unknown kind accepted")
	}
}

func TestTextStaleAndMissingSource(t *testing.T) {
	tmpl, err := Parse(strings.NewReader("kind: text\ncells:\n  - { at: [0, 0], span: [6, 1], value: a/x }\n"))
	if err != nil {
		t.Fatal(err)
	}
	stale := func(ValueBatch) []Result {
		v := plugin.TextValue("hi")
		return []Result{{Value: &v, Spec: plugin.ValueSpec{Type: plugin.ValueText}, Stale: true}}
	}
	text, err := tmpl.RenderText(Display{Width: 6, Height: 1, Text: true}, Context{Values: stale})
	if err != nil {
		t.Fatal(err)
	}
	if text.Lines[0] != "hi   !" {
		t.Fatalf("stale marker: %q", text.Lines[0])
	}
	text, err = tmpl.RenderText(Display{Width: 6, Height: 1, Text: true}, Context{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(text.Lines[0], "? ") {
		t.Fatalf("missing source: %q", text.Lines[0])
	}
}

func TestEncodeTextCells(t *testing.T) {
	tmpl, err := Parse(strings.NewReader(`
kind: text
overflow: wrap
cells:
  - { at: [0, 0], span: [6, 1], text: "a\nb", align: right }
  - { at: [6, 1], span: [4, 2], value: a/one, label: L, unit: kW, overflow: scroll }
`))
	if err != nil {
		t.Fatal(err)
	}
	data, err := Encode(tmpl)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "overflow: wrap") || !strings.Contains(string(data), "text: |-") {
		// Multi-line literal text is written as a block scalar or a quoted line;
		// both must read back.
	}
	back, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tmpl, back) {
		t.Fatalf("round trip changed the template\nwant %+v\ngot  %+v", tmpl, back)
	}
	if strings.Contains(string(data), "rows:") {
		t.Fatalf("encoded text page still has rows:\n%s", data)
	}
}
