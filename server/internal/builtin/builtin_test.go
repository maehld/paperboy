package builtin

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

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
	req.Values = []string{"title", "time", "date", "now", "long", "block"}
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
	if out["long"].Text == nil || len(*out["long"].Text) <= 20 {
		t.Errorf("long: %+v", out["long"])
	}
	if out["block"].Text == nil || !strings.Contains(*out["block"].Text, "\n") {
		t.Errorf("block: %+v", out["block"])
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
	for _, name := range []string{"title", "time", "date", "now", "long", "block"} {
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
