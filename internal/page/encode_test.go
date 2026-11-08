package page

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestEncodeExamplesRoundTrip reads every example page, writes it back and
// verifies that the result parses to the same template (ADR 0066).
func TestEncodeExamplesRoundTrip(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "examples", "pages", "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("example pages: %v (%d files)", err, len(files))
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			want, err := Parse(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			data, err := Encode(want)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			got, err := Parse(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("reparse: %v\n%s", err, data)
			}
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("round trip changed the template\nwant %+v\ngot  %+v\nyaml:\n%s", want, got, data)
			}
		})
	}
}

// TestEncodeKeepsCellLayout verifies the readable layout: one flow cell per
// line and the defaults left out.
func TestEncodeKeepsCellLayout(t *testing.T) {
	tmpl, err := Parse(strings.NewReader(previewYAML))
	if err != nil {
		t.Fatal(err)
	}
	data, err := Encode(tmpl)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	for _, want := range []string{
		"title: Preview\n",
		"grid: [6, 8]\n",
		"- {at: [0, 0], span: [6, 1], widget: paperboy/header}\n",
		"args: {days: 3, start: today}",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"kind: image", "notices: show", "actions:"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("unexpected %q in:\n%s", unwanted, out)
		}
	}
}

const previewYAML = `
title: Preview
grid: [6, 8]
cells:
  - { at: [0, 0], span: [6, 1], widget: paperboy/header }
  - { at: [0, 1], span: [6, 7], widget: calendar/week, args: { days: 3, start: today } }
`

// TestEncodeRejectsInvalidTemplate verifies that nothing is written for a
// template Parse would reject as well.
func TestEncodeRejectsInvalidTemplate(t *testing.T) {
	for name, tmpl := range map[string]*Template{
		"overlap": {Grid: [2]int{2, 2}, Cells: []Cell{
			{At: [2]int{0, 0}, Span: [2]int{2, 2}, Widget: "a/b"},
			{At: [2]int{1, 1}, Span: [2]int{1, 1}, Widget: "a/c"},
		}},
		"leaves grid": {Grid: [2]int{1, 1}, Cells: []Cell{{At: [2]int{1, 0}, Span: [2]int{1, 1}, Widget: "a/b"}}},
		"bad widget":  {Grid: [2]int{1, 1}, Cells: []Cell{{At: [2]int{0, 0}, Span: [2]int{1, 1}, Widget: "nope"}}},
	} {
		if _, err := Encode(tmpl); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

// TestEncodeArgsKeepTheirTypes verifies that numbers, booleans and lists read
// back as the types an operator wrote.
func TestEncodeArgsKeepTheirTypes(t *testing.T) {
	tmpl := &Template{Grid: [2]int{1, 1}, Cells: []Cell{{
		At: [2]int{0, 0}, Span: [2]int{1, 1}, Widget: "calendar/agenda",
		Args: map[string]any{"days": 3.0, "empty": true, "lines": []any{"a", 2.0}, "city": "Münster: Mitte"},
	}}}
	data, err := Encode(tmpl)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("reparse: %v\n%s", err, data)
	}
	args := got.Cells[0].Args
	if args["city"] != "Münster: Mitte" {
		t.Fatalf("string arg: %#v\n%s", args["city"], data)
	}
	if args["empty"] != true {
		t.Fatalf("bool arg: %#v", args["empty"])
	}
	list, ok := args["lines"].([]any)
	if !ok || len(list) != 2 || list[0] != "a" || list[1] != 2 {
		t.Fatalf("list arg: %#v", args["lines"])
	}
}
