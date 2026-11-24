package page

import (
	"bytes"
	"encoding/json"
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

	"github.com/maehld/paperboy/sdk/plugin"
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
		"no grid":    "cells: [{widget: a/b}]\n",
		"no cells":   "grid: [1, 1]\n",
		"outside":    "grid: [2, 1]\ncells: [{at: [1, 0], span: [2, 1], widget: a/b}]\n",
		"overlap":    "grid: [2, 1]\ncells: [{span: [2, 1], widget: a/b}, {at: [1, 0], widget: a/c}]\n",
		"no plugin":  "grid: [1, 1]\ncells: [{widget: agenda}]\n",
		"bad name":   "grid: [1, 1]\ncells: [{widget: Cal/agenda}]\n",
		"unknown":    "grid: [1, 1]\ncells: [{widget: a/b, colour: red}]\n",
		"notices":    "grid: [1, 1]\nnotices: bar\ncells: [{widget: a/b}]\n",
		"old rows":   "rows: [1]\nblocks: [{text: x}]\n",
		"actions":    "grid: [1, 1]\nactions: [{action: a/b}]\ncells: [{widget: a/b}]\n",
		"bad action": "grid: [1, 1]\ncells: [{widget: a/b, action: Mode}]\n",
		"second":     "kind: image\ngrid: [1, 1]\ncells: [{widget: a/b}]\n---\nkind: text\n",
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
	if got.Levels != 4 || got.Page.Number != 2 || got.Page.Count != 3 || got.Page.Title != "Test" || got.Page.PPI != 72 {
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

// TestTextEnumLabels shows the label of the answered enum key, with the key as
// the fallback (ADR 0127).
func TestTextEnumLabels(t *testing.T) {
	tmpl, err := Parse(strings.NewReader(`
kind: text
cells:
  - { at: [0, 0], span: [20, 1], value: dexcom/state, labels: { low: "The reading is low", urgent: "Urgent" } }
  - { at: [0, 1], span: [20, 1], value: dexcom/state }
`))
	if err != nil {
		t.Fatal(err)
	}
	spec := plugin.ValueSpec{Name: "state", Type: plugin.ValueEnum, Enum: []string{"normal", "low", "urgent"}}
	src := valueSource(map[string]ValueResult{
		"dexcom/state": {Value: plugin.EnumValue("low"), Spec: spec},
	})
	text, err := tmpl.RenderText(Display{Width: 20, Height: 2, Text: true, Language: "en"}, Context{Values: src})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimRight(text.Lines[0], " "); got != "The reading is low" {
		t.Errorf("labelled key = %q", got)
	}
	if got := strings.TrimRight(text.Lines[1], " "); got != "low" {
		t.Errorf("key without a label = %q", got)
	}
	// An empty answer leaves the cell as a question mark.
	src = valueSource(map[string]ValueResult{"dexcom/state": {Value: plugin.Value{}, Spec: spec}})
	text, err = tmpl.RenderText(Display{Width: 20, Height: 2, Text: true}, Context{Values: src})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimRight(text.Lines[0], " "); got != "?" {
		t.Errorf("empty enum = %q", got)
	}
}

// TestTextFlow marks a signed number as a direction (issue #399): the arrow
// form, the words and a value that rounds to zero.
func TestTextFlow(t *testing.T) {
	tmpl, err := Parse(strings.NewReader(`
kind: text
cells:
  - { at: [0, 0], span: [20, 1], value: e/grid, label: Netz, flow: arrow }
  - { at: [0, 1], span: [20, 1], value: e/battery, label: Akku, flow: { out: entlädt, in: lädt } }
  - { at: [0, 2], span: [20, 1], value: e/out, label: Akku, flow: { out: entlädt, in: lädt } }
  - { at: [0, 3], span: [20, 1], value: e/zero, label: "Null", flow: arrow }
`))
	if err != nil {
		t.Fatal(err)
	}
	spec := plugin.ValueSpec{Name: "power", Type: plugin.ValueNumber, Unit: "W", Precision: 0}
	src := valueSource(map[string]ValueResult{
		"e/grid":    {Value: plugin.NumberValue(-400), Spec: spec},
		"e/battery": {Value: plugin.NumberValue(-400), Spec: spec},
		"e/out":     {Value: plugin.NumberValue(400), Spec: spec},
		"e/zero":    {Value: plugin.NumberValue(0.4), Spec: spec},
	})
	text, err := tmpl.RenderText(Display{Width: 20, Height: 4, Text: true, Language: "de"}, Context{Values: src})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Netz → 400 W", "Akku lädt 400 W", "Akku entlädt 400 W", "Null 0 W"}
	for i, w := range want {
		if got := strings.TrimRight(text.Lines[i], " "); got != w {
			t.Errorf("line %d = %q, want %q", i+1, got, w)
		}
	}
}

// TestTextGlyphsByCharset covers the CGRAM glyphs and their ASCII fallbacks
// (ADR 0155): arrows, trend and a checkmark.
func TestTextGlyphsByCharset(t *testing.T) {
	tmpl, err := Parse(strings.NewReader(`
kind: text
cells:
  - { at: [0, 0], span: [20, 1], value: e/out, flow: arrow }
  - { at: [0, 1], span: [20, 1], value: e/grid, flow: arrow }
  - { at: [0, 2], span: [20, 1], value: e/trend }
  - { at: [0, 3], span: [20, 1], text: "✓" }
`))
	if err != nil {
		t.Fatal(err)
	}
	power := plugin.ValueSpec{Name: "power", Type: plugin.ValueNumber, Unit: "W", Precision: 0}
	trend := plugin.ValueSpec{Name: "trend", Type: plugin.ValueDirection}
	src := valueSource(map[string]ValueResult{
		"e/out":   {Value: plugin.NumberValue(400), Spec: power},
		"e/grid":  {Value: plugin.NumberValue(-400), Spec: power},
		"e/trend": {Value: plugin.DirectionValue(1), Spec: trend},
	})
	want := map[string][]string{
		CharsetUTF8:    {"← 400 W", "→ 400 W", "↑", "✓"},
		CharsetASCII:   {"< 400 W", "> 400 W", "^", "OK"},
		CharsetHD44780: {"← 400 W", "→ 400 W", "↑", "✓"},
	}
	for charset, lines := range want {
		text, err := tmpl.RenderText(Display{Width: 20, Height: 4, Text: true, Charset: charset}, Context{Values: src})
		if err != nil {
			t.Fatalf("%s: %v", charset, err)
		}
		for i, w := range lines {
			if got := strings.TrimRight(text.Lines[i], " "); got != w {
				t.Errorf("%s line %d = %q, want %q", charset, i+1, got, w)
			}
		}
	}
}

// TestTextDirectionGlyphs covers every trend step and its ASCII fallback.
func TestTextDirectionGlyphs(t *testing.T) {
	tmpl, err := Parse(strings.NewReader(`
kind: text
cells:
  - { at: [0, 0], span: [10, 1], value: e/two }
  - { at: [0, 1], span: [10, 1], value: e/one }
  - { at: [0, 2], span: [10, 1], value: e/flat }
  - { at: [0, 3], span: [10, 1], value: e/minus }
  - { at: [0, 4], span: [10, 1], value: e/minus2 }
`))
	if err != nil {
		t.Fatal(err)
	}
	spec := plugin.ValueSpec{Name: "trend", Type: plugin.ValueDirection}
	src := valueSource(map[string]ValueResult{
		"e/two":    {Value: plugin.DirectionValue(2), Spec: spec},
		"e/one":    {Value: plugin.DirectionValue(1), Spec: spec},
		"e/flat":   {Value: plugin.DirectionValue(0), Spec: spec},
		"e/minus":  {Value: plugin.DirectionValue(-1), Spec: spec},
		"e/minus2": {Value: plugin.DirectionValue(-2), Spec: spec},
	})
	want := map[string][]string{
		CharsetUTF8:    {"↑↑", "↑", "→", "↓", "↓↓"},
		CharsetASCII:   {"^^", "^", ">", "v", "vv"},
		CharsetHD44780: {"↑↑", "↑", "→", "↓", "↓↓"},
	}
	for charset, lines := range want {
		text, err := tmpl.RenderText(Display{Width: 10, Height: 5, Text: true, Charset: charset}, Context{Values: src})
		if err != nil {
			t.Fatalf("%s: %v", charset, err)
		}
		for i, w := range lines {
			if got := strings.TrimRight(text.Lines[i], " "); got != w {
				t.Errorf("%s line %d = %q, want %q", charset, i+1, got, w)
			}
		}
	}
}

func TestConvertGlyphs(t *testing.T) {
	for charset, want := range map[string]string{
		CharsetUTF8:    "←→↑↓✓",
		CharsetASCII:   "<>^vOK",
		CharsetHD44780: "←→↑↓✓",
	} {
		if got := Convert(charset, "←→↑↓✓"); got != want {
			t.Errorf("%s: %q, want %q", charset, got, want)
		}
	}
	if got := Convert(CharsetHD44780, "äöüß°µ"); got != "äöüß°µ" {
		t.Errorf("the A00 characters changed: %q", got)
	}
}

func TestTextFlowValidation(t *testing.T) {
	for _, body := range []string{
		"kind: text\ncells: [{ at: [0, 0], span: [1, 1], text: x, flow: arrow }]",
		"kind: text\ncells: [{ at: [0, 0], span: [1, 1], value: e/x, flow: { out: nur } }]",
		"kind: text\ncells: [{ at: [0, 0], span: [1, 1], value: e/x, flow: { out: a, in: b, extra: c } }]",
		"kind: text\ncells: [{ at: [0, 0], span: [1, 1], value: e/x, flow: sidewards }]",
	} {
		if _, err := Parse(strings.NewReader(body)); err == nil {
			t.Errorf("accepted %q", body)
		}
	}
}

func TestFlowJSON(t *testing.T) {
	for _, c := range []struct {
		in   string
		want Flow
	}{
		{`"arrow"`, Flow{Arrow: true}},
		{`{"out":"bezieht","in":"speist ein"}`, Flow{Out: "bezieht", In: "speist ein"}},
	} {
		var f Flow
		if err := json.Unmarshal([]byte(c.in), &f); err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if f != c.want {
			t.Errorf("%s = %+v, want %+v", c.in, f, c.want)
		}
		data, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		var back Flow
		if err := json.Unmarshal(data, &back); err != nil || back != f {
			t.Errorf("round trip %s: %+v, %v", data, back, err)
		}
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
	if err == nil || !strings.Contains(err.Error(), "literal text cell has no label, labels, unit, scale, flow or args") {
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
		"grid":        "kind: text\ngrid: [2, 2]\ncells:\n  - { at: [0, 0], span: [2, 1], text: x }\n",
		"rows":        "kind: text\nrows:\n  - [{ value: a/b, width: 2 }]\n",
		"no cells":    "kind: text\n",
		"neither":     "kind: text\ncells:\n  - { at: [0, 0], span: [2, 1] }\n",
		"both":        "kind: text\ncells:\n  - { at: [0, 0], span: [2, 1], text: x, value: a/b }\n",
		"widget":      "kind: text\ncells:\n  - { at: [0, 0], span: [2, 1], text: x, widget: a/b }\n",
		"label text":  "kind: text\ncells:\n  - { at: [0, 0], span: [2, 1], text: x, label: L }\n",
		"labels text": "kind: text\ncells:\n  - { at: [0, 0], span: [2, 1], text: x, labels: { low: Low } }\n",
		"scale text":  "kind: text\ncells:\n  - { at: [0, 0], span: [2, 1], text: x, scale: auto }\n",
		"outside":     "kind: text\ncells:\n  - { at: [79, 0], span: [2, 1], text: x }\n",
		"overlap":     "kind: text\ncells:\n  - { at: [0, 0], span: [2, 2], text: x }\n  - { at: [1, 1], span: [2, 1], text: y }\n",
		"align":       "kind: text\ncells:\n  - { at: [0, 0], span: [2, 1], text: x, align: center }\n",
		"overflow":    "kind: text\ncells:\n  - { at: [0, 0], span: [2, 1], text: x, overflow: zoom }\n",
		"scroll key":  "kind: text\ncells:\n  - { at: [0, 0], span: [2, 1], text: x, scroll: true }\n",
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
	if _, err := Parse(strings.NewReader("kind: image\ngrid: [2, 2]\ncells:\n  - { at: [0, 0], span: [2, 1], widget: a/b, labels: { low: Low } }\n")); err == nil {
		t.Error("an image page with labels was accepted")
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
  - { at: [6, 1], span: [4, 2], value: a/one, label: L, labels: { low: "Low", urgent: "Urgent" }, unit: kW, overflow: scroll }
  - { at: [0, 3], span: [6, 1], value: a/power, flow: arrow }
  - { at: [6, 3], span: [4, 1], value: a/power, flow: { out: bezieht, in: speist ein } }
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
	if !strings.Contains(string(data), `labels: {low: Low, urgent: Urgent}`) {
		t.Fatalf("labels missing from the file:\n%s", data)
	}
	if !strings.Contains(string(data), `flow: arrow`) || !strings.Contains(string(data), `flow: {out: bezieht, in: speist ein}`) {
		t.Fatalf("flow missing from the file:\n%s", data)
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

// TestCellAction checks the scalar shorthand and the block with its own args
// (ADR 0164).
func TestCellAction(t *testing.T) {
	src := "kind: image\ngrid: [2, 1]\ncells: [{at: [0, 0], span: [2, 1], widget: evcc/loadpoint, args: {index: 0}, action: mode}]\n"
	tp, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	action := tp.Cells[0].Action
	if action == nil || action.Name != "mode" || len(action.Args) != 0 || tp.Actionable() != 1 {
		t.Fatalf("cell action: %+v", tp.Cells[0])
	}
	out, err := Encode(tp)
	if err != nil || !strings.Contains(string(out), "action: mode") {
		t.Fatalf("encoded page: %v\n%s", err, out)
	}
	again, err := Parse(bytes.NewReader(out))
	if err != nil || again.Cells[0].Action == nil || again.Cells[0].Action.Name != "mode" {
		t.Fatalf("round trip: %v %+v", err, again)
	}
	if rects := tp.Rects(200, 100); len(rects) != 1 || rects[0] != image.Rect(0, 0, 200, 100) {
		t.Fatalf("rects: %v", rects)
	}

	block := "kind: image\ngrid: [2, 1]\ncells: [{at: [0, 0], span: [2, 1], widget: evcc/loadpoint, action: {name: mode, args: {index: 0, mode: now}}}]\n"
	tb, err := Parse(strings.NewReader(block))
	if err != nil {
		t.Fatal(err)
	}
	action = tb.Cells[0].Action
	if action == nil || action.Name != "mode" || action.Args["index"] != 0 || action.Args["mode"] != "now" {
		t.Fatalf("action block: %+v", action)
	}
	out, err = Encode(tb)
	if err != nil || !strings.Contains(string(out), "action: {name: mode, args: {index: 0, mode: now}}") {
		t.Fatalf("encoded block: %v\n%s", err, out)
	}
	back, err := Parse(bytes.NewReader(out))
	if err != nil || !reflect.DeepEqual(back.Cells[0].Action, action) {
		t.Fatalf("block round trip: %v %+v", err, back.Cells[0].Action)
	}

	if _, err := Parse(strings.NewReader("kind: text\ncells: [{at: [0, 0], text: x, action: {name: mode}}]\n")); err == nil || !strings.Contains(err.Error(), "image") {
		t.Fatalf("a text cell accepted an action: %v", err)
	}
}

// TestCellActionBlock keeps the block strict: the keys are name and args, a
// name is required, and args keep their types (ADR 0164).
func TestCellActionBlock(t *testing.T) {
	page := func(action string) string {
		return "kind: image\ngrid: [1, 1]\ncells: [{widget: a/b, action: " + action + "}]\n"
	}
	tp, err := Parse(strings.NewReader(page(`{name: mode, args: {index: 0, ratio: 1.5, on: true, names: [a, b]}}`)))
	if err != nil {
		t.Fatal(err)
	}
	args := tp.Cells[0].Action.Args
	if args["index"] != 0 || args["ratio"] != 1.5 || args["on"] != true || !reflect.DeepEqual(args["names"], []any{"a", "b"}) {
		t.Fatalf("args types: %#v", args)
	}
	out, err := Encode(tp)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse(bytes.NewReader(out))
	if err != nil || !reflect.DeepEqual(back.Cells[0].Action, tp.Cells[0].Action) {
		t.Fatalf("args round trip: %v\n%s", err, out)
	}
	for name, action := range map[string]string{
		"unknown key":   `{name: mode, title: Laden}`,
		"no name":       `{args: {index: 0}}`,
		"bad name":      `{name: Mode}`,
		"duplicate key": `{name: mode, name: pv}`,
		"scalar name":   `Mode`,
		"sequence":      `[mode]`,
	} {
		if _, err := Parse(strings.NewReader(page(action))); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	tp, err = Parse(strings.NewReader(page(`{name: mode, args: {}}`)))
	if err != nil {
		t.Fatal(err)
	}
	if tp.Cells[0].Action.Args != nil {
		t.Fatalf("an empty args map must stay absent: %+v", tp.Cells[0].Action.Args)
	}
}

// TestActionJSON keeps the editor API on the same model: it reads the scalar
// and the block, writes the block, and rejects an unknown key (ADR 0164).
func TestActionJSON(t *testing.T) {
	var scalar Cell
	if err := json.Unmarshal([]byte(`{"action":"mode"}`), &scalar); err != nil {
		t.Fatal(err)
	}
	if scalar.Action == nil || scalar.Action.Name != "mode" || len(scalar.Action.Args) != 0 {
		t.Fatalf("scalar JSON: %+v", scalar.Action)
	}
	data, err := json.Marshal(scalar)
	if err != nil || !strings.Contains(string(data), `"action":{"name":"mode"}`) {
		t.Fatalf("marshaled scalar: %v %s", err, data)
	}
	var block Cell
	if err := json.Unmarshal([]byte(`{"action":{"name":"mode","args":{"index":0}}}`), &block); err != nil {
		t.Fatal(err)
	}
	if block.Action == nil || block.Action.Args["index"] != float64(0) {
		t.Fatalf("block JSON: %+v", block.Action)
	}
	if err := json.Unmarshal([]byte(`{"action":{"name":"mode","title":"Laden"}}`), &block); err == nil {
		t.Fatal("an unknown action key was accepted")
	}
}
