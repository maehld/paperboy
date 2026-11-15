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

func TestRenderText(t *testing.T) {
	tmpl, err := Parse(strings.NewReader(`
kind: text
rows:
  - [{ value: a/temp, label: T, width: 10, align: decimal }]
  - [{ value: a/name, width: 5, align: left }, { value: a/now, width: 5, align: right }]
  - [{ value: a/down, width: 10, align: left }]
`))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.November, 15, 12, 0, 0, 0, time.UTC)
	src := valueSource(map[string]ValueResult{
		"a/temp": {Value: plugin.NumberValue(3.5), Spec: plugin.ValueSpec{Name: "temp", Type: plugin.ValueNumber, Unit: "kW", Precision: 1}},
		"a/name": {Value: plugin.TextValue("Übersicht"), Spec: plugin.ValueSpec{Name: "name", Type: plugin.ValueText}},
		"a/now":  {Value: plugin.TimestampValue(now), Spec: plugin.ValueSpec{Name: "now", Type: plugin.ValueTimestamp}},
		"a/down": {Err: errors.New("down")},
	})
	text, err := tmpl.RenderText(Display{Width: 10, Height: 3, Text: true, Charset: "ascii", Language: "de", Timezone: time.UTC},
		Context{Now: now, Values: src})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"T   3,5 kW", "Ueber12:00", "? down    "}
	if strings.Join(text.Lines, "|") != strings.Join(want, "|") {
		t.Fatalf("lines %q, want %q", text.Lines, want)
	}
	if text.Scroll != nil {
		t.Fatalf("a page of values must not scroll: %q", text.Scroll)
	}
}

func TestRenderTextEmptyRow(t *testing.T) {
	tmpl, err := Parse(strings.NewReader("kind: text\nrows:\n  - []\n  - [{ value: a/x, width: 10 }]\n"))
	if err != nil {
		t.Fatal(err)
	}
	v := plugin.TextValue("hi")
	text, err := tmpl.RenderText(Display{Width: 10, Height: 2, Text: true},
		Context{Values: func(ValueBatch) []Result {
			return []Result{{Value: &v, Spec: plugin.ValueSpec{Type: plugin.ValueText}}}
		}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"          ", "hi        "}; strings.Join(text.Lines, "|") != strings.Join(want, "|") {
		t.Fatalf("lines %q, want %q", text.Lines, want)
	}
}

func TestRenderTextGap(t *testing.T) {
	tmpl, err := Parse(strings.NewReader("kind: text\nrows:\n  - [{ value: a/x, width: 3 }, { value: a/y, at: 6, width: 3 }]\n"))
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"a/x": "ab", "a/y": "cd"}
	text, err := tmpl.RenderText(Display{Width: 10, Height: 1, Text: true},
		Context{Values: func(b ValueBatch) []Result {
			out := make([]Result, len(b.Values))
			for i, v := range b.Values {
				s := values[v.Plugin+"/"+v.Name]
				out[i] = Result{Value: &plugin.Value{Text: &s}, Spec: plugin.ValueSpec{Type: plugin.ValueText}}
			}
			return out
		}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "ab    cd  "; text.Lines[0] != want {
		t.Fatalf("line %q, want %q", text.Lines[0], want)
	}
	if err := tmpl.Fits(Display{Width: 8, Height: 1, Text: true}); err == nil {
		t.Error("a cell past the display was accepted")
	}
}

func TestRenderTextStaleAndMissingSource(t *testing.T) {
	tmpl, err := Parse(strings.NewReader("kind: text\nrows:\n  - [{ value: a/x, width: 6 }]\n"))
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

func TestTextPageRowsParse(t *testing.T) {
	valid := `
kind: text
rows:
  - [{ value: a/one, width: 4 }]
  - [{ value: a/two, width: 2, align: right }, { value: a/three, width: 2, align: decimal }]
`
	tmpl, err := Parse(strings.NewReader(valid))
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Rows[0][0].Align != AlignLeft || tmpl.Rows[1][1].Align != AlignDecimal {
		t.Fatalf("alignments: %+v", tmpl.Rows)
	}
	for name, bad := range map[string]string{
		"grid":    "kind: text\ngrid: [2, 1]\nrows:\n  - [{ value: a/b, width: 2 }]\n",
		"cells":   "kind: text\ncells: [{ widget: a/b }]\nrows:\n  - [{ value: a/b, width: 2 }]\n",
		"no rows": "kind: text\n",
		"width":   "kind: text\nrows:\n  - [{ value: a/b, width: 0 }]\n",
		"wide":    "kind: text\nrows:\n  - [{ value: a/b, width: 81 }]\n",
		"align":   "kind: text\nrows:\n  - [{ value: a/b, width: 2, align: center }]\n",
		"value":   "kind: text\nrows:\n  - [{ value: agenda, width: 2 }]\n",
		"plugin":  "kind: text\nrows:\n  - [{ value: A/b, width: 2 }]\n",
		"sum":     "kind: text\nrows:\n  - [{ value: a/b, width: 50 }, { value: a/c, width: 31 }]\n",
	} {
		if _, err := Parse(strings.NewReader(bad)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := Parse(strings.NewReader("kind: image\ngrid: [1, 1]\nrows:\n  - [{ value: a/b, width: 1 }]\ncells: [{ widget: a/b }]\n")); err == nil {
		t.Error("an image page with rows was accepted")
	}
	// A row without cells stays a blank line; the editor can delete the last
	// cell without making the page unreadable.
	empty, err := Parse(strings.NewReader("kind: text\nrows:\n  - []\n  - [{ value: a/b, width: 2 }]\n"))
	if err != nil {
		t.Fatalf("empty row: %v", err)
	}
	if len(empty.Rows) != 2 || len(empty.Rows[0]) != 0 {
		t.Fatalf("empty row parsed as %+v", empty.Rows)
	}
	// A missing at follows the previous cell; an explicit at leaves a gap.
	gap, err := Parse(strings.NewReader("kind: text\nrows:\n  - [{ value: a/b, width: 2 }, { value: a/c, width: 2 }, { value: a/d, at: 6, width: 2 }, { value: a/e, width: 2 }]\n"))
	if err != nil {
		t.Fatalf("gap: %v", err)
	}
	positions, err := textRowPositions(gap.Rows[0], maxGrid)
	if err != nil {
		t.Fatalf("positions: %v", err)
	}
	if want := []int{0, 2, 6, 8}; !reflect.DeepEqual(positions, want) {
		t.Fatalf("positions %v, want %v", positions, want)
	}
	for name, bad := range map[string]string{
		"overlap":  "kind: text\nrows:\n  - [{ value: a/b, width: 4 }, { value: a/c, at: 2, width: 2 }]\n",
		"order":    "kind: text\nrows:\n  - [{ value: a/b, at: 5, width: 2 }, { value: a/c, at: 3, width: 2 }]\n",
		"range":    "kind: text\nrows:\n  - [{ value: a/b, at: 79, width: 2 }]\n",
		"negative": "kind: text\nrows:\n  - [{ value: a/b, at: -1, width: 2 }]\n",
	} {
		if _, err := Parse(strings.NewReader(bad)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestFits(t *testing.T) {
	img, err := Parse(strings.NewReader("kind: image\ngrid: [20, 4]\ncells: [{widget: a/b}]\n"))
	if err != nil {
		t.Fatal(err)
	}
	txt, err := Parse(strings.NewReader("kind: text\nrows:\n  - [{ value: a/b, width: 20 }]\n  - [{ value: a/b, width: 20 }]\n  - [{ value: a/b, width: 20 }]\n  - [{ value: a/b, width: 20 }]\n"))
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
	if txt.Fits(Display{Width: 20, Height: 3, Text: true}) == nil {
		t.Error("more rows than the display were accepted")
	}
	wide, _ := Parse(strings.NewReader("kind: text\nrows:\n  - [{ value: a/b, width: 21 }]\n"))
	if wide.Fits(Display{Width: 20, Height: 4, Text: true}) == nil {
		t.Error("a row wider than the display was accepted")
	}
	if txt.Fits(Display{Width: 800, Height: 480}) == nil || img.Fits(Display{Width: 800, Height: 480}) != nil {
		t.Error("image display fit")
	}
	if _, err := Parse(strings.NewReader("kind: led\ngrid: [1, 1]\ncells: [{widget: a/b}]\n")); err == nil {
		t.Error("unknown kind accepted")
	}
}

func TestFormatValue(t *testing.T) {
	now := time.Date(2026, time.November, 15, 12, 0, 0, 0, time.UTC)
	loc := time.UTC
	number := plugin.ValueSpec{Type: plugin.ValueNumber, Precision: 1}
	if got := formatValue(number, plugin.NumberValue(12.5), "de", now, loc); got != "12,5" {
		t.Errorf("German decimal: %q", got)
	}
	if got := formatValue(number, plugin.NumberValue(12.5), "en", now, loc); got != "12.5" {
		t.Errorf("English decimal: %q", got)
	}
	if got := formatValue(plugin.ValueSpec{Type: plugin.ValueDuration}, plugin.DurationValue(3*time.Hour+20*time.Minute), "de", now, loc); got != "3h 20min" {
		t.Errorf("duration: %q", got)
	}
	if got := formatValue(plugin.ValueSpec{Type: plugin.ValueDirection}, plugin.DirectionValue(-1), "de", now, loc); got != "v" {
		t.Errorf("direction: %q", got)
	}
	cases := []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, time.November, 15, 9, 30, 0, 0, loc), "09:30"},
		{time.Date(2026, time.November, 15, 0, 0, 0, 0, loc), "15.11.2026"},
		{time.Date(2026, time.November, 14, 18, 5, 0, 0, loc), "14.11. 18:05"},
	}
	for _, c := range cases {
		if got := formatValue(plugin.ValueSpec{Type: plugin.ValueTimestamp}, plugin.TimestampValue(c.at), "de", now, loc); got != c.want {
			t.Errorf("timestamp %v: %q, want %q", c.at, got, c.want)
		}
	}
	if got := formatValue(plugin.ValueSpec{Type: plugin.ValueTimestamp}, plugin.TimestampValue(cases[0].at), "en", now, loc); got != "09:30" {
		t.Errorf("English time: %q", got)
	}
}

func TestTextPageCellsAlign(t *testing.T) {
	if got := composeTextCell("utf8", "", "12,5", "", 8, AlignRight); got != "    12,5" {
		t.Errorf("right: %q", got)
	}
	if got := composeTextCell("utf8", "PV", "12,5", "kW", 10, AlignDecimal); got != "PV 12,5 kW" {
		t.Errorf("decimal: %q", got)
	}
	if got := composeTextCell("utf8", "", "Übersicht", "", 5, AlignLeft); got != "Übers" {
		t.Errorf("cut: %q", got)
	}
	if got := composeTextCell("ascii", "", "Übersicht", "", 10, AlignLeft); got != "Uebersicht" {
		t.Errorf("ascii: %q", got)
	}
	if got := composeTextCell("utf8", "", "123456789", "", 4, AlignRight); got != "6789" {
		t.Errorf("right cuts the head: %q", got)
	}
	if got := composeTextCell("utf8", "", "123456789", "", 4, AlignLeft); got != "1234" {
		t.Errorf("left cuts the tail: %q", got)
	}
}

func TestEncodeTextAt(t *testing.T) {
	tmpl, err := Parse(strings.NewReader("kind: text\nrows:\n  - [{ value: a/one, width: 2 }, { value: a/two, at: 6, width: 3 }]\n"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := Encode(tmpl)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "at: 6") {
		t.Fatalf("encoded page lacks at:\n%s", data)
	}
	back, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tmpl, back) {
		t.Fatalf("round trip changed the template\nwant %+v\ngot  %+v", tmpl, back)
	}
}

func TestEncodeTextRows(t *testing.T) {
	tmpl, err := Parse(strings.NewReader(`
kind: text
rows:
  - [{ value: a/one, label: L, unit: kW, width: 6, align: decimal }]
  - []
  - [{ value: a/two, width: 4, align: right }]
`))
	if err != nil {
		t.Fatal(err)
	}
	data, err := Encode(tmpl)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "rows:") || strings.Contains(string(data), "grid:") {
		t.Fatalf("encoded text page:\n%s", data)
	}
	back, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tmpl, back) {
		t.Fatalf("round trip changed the template\nwant %+v\ngot  %+v", tmpl, back)
	}
}
