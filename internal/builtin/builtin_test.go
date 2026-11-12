package builtin

import (
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
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
