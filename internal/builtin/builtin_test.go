package builtin

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

func TestHeaderDateFitsWidth(t *testing.T) {
	now := time.Date(2026, 11, 7, 9, 5, 0, 0, time.UTC)
	cases := []struct {
		lang  string
		width int
		want  string
	}{
		{"de", 60, "Seite" + strings.Repeat(" ", 23) + "Samstag, 7. November 2026  09:05"},
		{"de", 30, "Seite     Sa, 7.11.2026  09:05"},
		{"de", 20, "Seite   7.11.  09:05"},
		{"en", 30, "Seite  Sat, Nov 7, 2026  09:05"},
		{"de", 12, "Seite  09:05"},
	}
	for _, c := range cases {
		req := &plugin.Request{Language: c.lang, Timezone: "UTC", Now: now, Page: plugin.PageInfo{Title: "Seite"}}
		w := plugin.WidgetRequest{Widget: "header", Width: c.width, Args: map[string]any{"date": true}}
		lines, err := Plugin{}.Text(req, w)
		if err != nil {
			t.Fatal(err)
		}
		if lines[0] != c.want {
			t.Errorf("%s/%d: got %q, want %q", c.lang, c.width, lines[0], c.want)
		}
	}
}

func TestTextScrollKeepsLongLine(t *testing.T) {
	req := &plugin.Request{Language: "de", Timezone: "UTC"}
	long := "Eine Zeile, die nicht in die Zelle passt"
	for _, c := range []struct {
		scroll bool
		want   string
	}{
		{false, long[:10]},
		{true, long},
	} {
		w := plugin.WidgetRequest{Widget: "text", Width: 10, Scroll: c.scroll, Args: map[string]any{"text": long}}
		lines, err := Plugin{}.Text(req, w)
		if err != nil {
			t.Fatal(err)
		}
		if lines[0] != c.want {
			t.Errorf("scroll %v: got %q, want %q", c.scroll, lines[0], c.want)
		}
	}
	// A line that fits is aligned as before; Paperboy pads it to the cell.
	w := plugin.WidgetRequest{Widget: "text", Width: 20, Scroll: true, Args: map[string]any{"text": "kurz"}}
	if lines, _ := (Plugin{}).Text(req, w); lines[0] != "kurz" {
		t.Errorf("short line: %q", lines[0])
	}
}

func TestValues(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 34, 0, 0, time.UTC)
	req := &plugin.Request{Language: "de", Timezone: "UTC", Now: now, Page: plugin.PageInfo{Title: "Werte"}}
	out, err := Plugin{}.Values(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("a request without names answered %v", out)
	}
	req.Values = []string{"title", "time", "date", "now"}
	out, err = Plugin{}.Values(req)
	if err != nil {
		t.Fatal(err)
	}
	if out["title"].Text == nil || *out["title"].Text != "Werte" {
		t.Errorf("title: %+v", out["title"])
	}
	if out["time"].Timestamp == nil || *out["time"].Timestamp != "2026-11-15T12:34:00Z" {
		t.Errorf("time: %+v", out["time"])
	}
	if out["now"].Timestamp == nil || *out["now"].Timestamp != "2026-11-15T12:34:00Z" {
		t.Errorf("now: %+v", out["now"])
	}
	// date is midnight, so Paperboy shows the date and not the time.
	if out["date"].Timestamp == nil || *out["date"].Timestamp != "2026-11-15T00:00:00Z" {
		t.Errorf("date: %+v", out["date"])
	}
	req.Values = []string{"nope"}
	if _, err := (Plugin{}).Values(req); !errors.Is(err, plugin.ErrUnknownValue) {
		t.Errorf("unknown value: %v", err)
	}
}

func TestValuesDeclared(t *testing.T) {
	d := Plugin{}.Describe()
	seen := map[string]bool{}
	for _, v := range d.Values {
		seen[v.Name] = true
		if v.Type == "" || v.Title == "" {
			t.Errorf("value %q is incomplete: %+v", v.Name, v)
		}
	}
	for _, name := range []string{"title", "time", "date", "now"} {
		if !seen[name] {
			t.Errorf("value %q is not declared", name)
		}
	}
}

// A cell the padding eats up is reported for every widget, so Paperboy draws
// its notice instead of an empty cell.
func TestTooSmallCell(t *testing.T) {
	req := &plugin.Request{Language: "de"}
	w := plugin.WidgetRequest{Width: 10, Height: 10, Style: plugin.Style{Padding: 8, Rule: 1, TextPx: 16},
		Args: map[string]any{"text": "x"}}
	for _, name := range []string{"text", "clock", "logo"} {
		w.Widget = name
		err := (Plugin{}).Render(req, w, render.NewCanvas(10, 10, 2))
		if !errors.Is(err, plugin.ErrTooSmall) {
			t.Errorf("%s in a 10x10 cell: error = %v, want ErrTooSmall", name, err)
		}
	}
}
