package paperboy

import (
	"bytes"
	"image/png"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

func TestWidgetEndpoint(t *testing.T) {
	s, _ := testServer(t)
	s.config().Profiles["lcd"] = Profile{Kind: kindText, Charset: "ascii", Width: 10, Height: 2}
	rec := preview(t, s, "/api/widget/paperboy/text?display=small&width=30&height=10&arg.text=Hallo&arg.bold=true")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("image widget: %d %s", rec.Code, rec.Body)
	}
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil || img.Bounds().Dx() != 30 || img.Bounds().Dy() != 10 {
		t.Fatalf("image size: %v %v", img.Bounds(), err)
	}
	// span on the reference grid: 3x2 of 6x8 on 40x20 is 20x5
	rec = preview(t, s, "/api/widget/paperboy/clock?display=small&span=3x2")
	if img, _ := png.Decode(bytes.NewReader(rec.Body.Bytes())); img == nil || img.Bounds().Dx() != 20 || img.Bounds().Dy() != 5 {
		t.Fatalf("span size: %d", rec.Code)
	}
	rec = preview(t, s, "/api/widget/paperboy/text?display=lcd&arg.text=Grüße")
	if rec.Body.String() != "Gruesse   \n          \n" {
		t.Fatalf("text widget: %q", rec.Body)
	}
	rec = preview(t, s, "/api/widget/nope/x?display=small")
	if rec.Code != http.StatusOK || rec.Header().Get("X-Widget-Error") == "" {
		t.Fatalf("unknown plugin must become a notice image: %d %q", rec.Code, rec.Header())
	}
	for _, bad := range []string{"?display=big", "?display=small&span=7x2", "?display=small&width=999&height=1", "?display=small&span=x"} {
		if rec := preview(t, s, "/api/widget/paperboy/text"+bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, rec.Code)
		}
	}
}

func TestWidgetGallery(t *testing.T) {
	s, _ := testServer(t)
	s.config().Profiles["lcd"] = Profile{Kind: kindText, Charset: "ascii", Width: 20, Height: 4}
	s.config().Timezone = time.UTC
	body := preview(t, s, "/ui/plugins").Body.String()
	for _, want := range []string{"Display type", "header", "/api/widget/paperboy/header?display=small", "6x1"} {
		if !strings.Contains(body, want) {
			t.Fatalf("gallery misses %q: %s", want, body)
		}
	}
	text := preview(t, s, "/ui/plugins?display=lcd").Body.String()
	if !strings.Contains(text, `<pre class="lcd">`) {
		t.Fatalf("text gallery: %s", text)
	}
	if !strings.Contains(preview(t, s, "/ui").Body.String(), `href="/ui/plugins"`) {
		t.Fatal("dashboard does not link the gallery")
	}
}

// TestPlayground is the widgets page without a page: the palette and the
// inspector of the page editor around one widget, with the cell to copy
// (ADR 0055).
func TestPlayground(t *testing.T) {
	s, _ := testServer(t)
	s.config().Profiles["lcd"] = Profile{Kind: kindText, Charset: "ascii", Width: 20, Height: 4}
	s.config().Timezone = time.UTC

	body := preview(t, s, "/ui/plugins?tab=playground").Body.String()
	for _, want := range []string{
		`"playground":true`, `"display":"small"`, `id="palette"`, `id="inspector"`, `id="snippet"`,
		"Copy cell", `href="/ui/plugins?display=small&amp;tab=gallery"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("playground misses %q: %s", want, body)
		}
	}
	if strings.Contains(body, `id="board"`) {
		t.Fatalf("the playground draws a board: %s", body)
	}

	// The link carries the widget, its size and its args.
	body = preview(t, s, "/ui/plugins?tab=playground&display=small&widget=paperboy/clock&span=3x2&arg.format=date").Body.String()
	for _, want := range []string{`"widget":"paperboy/clock"`, `"span":[3,2]`, `"format":"date"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("playground link misses %q: %s", want, body)
		}
	}

	// A text display starts from its character grid.
	body = preview(t, s, "/ui/plugins?tab=playground&display=lcd&widget=paperboy/text").Body.String()
	for _, want := range []string{`"kind":"text"`, `"width":20`, `"height":4`, `"grid":[20,4]`} {
		if !strings.Contains(body, want) {
			t.Fatalf("text playground misses %q: %s", want, body)
		}
	}
	// The gallery keeps its tabs and is still the default.
	if body := preview(t, s, "/ui/plugins?tab=gallery").Body.String(); !strings.Contains(body, `href="/ui/plugins?display=small&amp;tab=playground"`) {
		t.Fatalf("gallery misses the playground tab: %s", body)
	}
}

// TestWidgetGalleryArgs covers the gallery's per-widget form.
func TestWidgetGalleryArgs(t *testing.T) {
	s, _ := testServer(t)
	s.config().Timezone = time.UTC
	body := preview(t, s, "/ui/plugins?display=small").Body.String()
	for _, want := range []string{`id="paperboy-clock"`, `<select name="arg.format" id="paperboy-clock-arg.format">`, `<option value="datetime">`,
		`<span class="desc">time, date or datetime</span>`, `<span class="req" title="required">*</span>`,
		`name="arg.text" value="" placeholder=""`, `<option value="" selected>default (left)</option>`, `name="widget" value="paperboy/clock"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("form misses %q: %s", want, body)
		}
	}
	if strings.Contains(body, "arg.format=") || strings.Contains(body, `class="snippet"`) || strings.Contains(body, `<ul class="args">`) {
		t.Fatal("args or snippet without a chosen widget")
	}
	body = preview(t, s, "/ui/plugins?display=small&widget=paperboy/clock&span=6x1&arg.format=date&arg.align=").Body.String()
	for _, want := range []string{`<option value="date" selected>`, "/api/widget/paperboy/clock?arg.format=%22date%22&amp;display=small&amp;grid=6x8&amp;span=6x1",
		"{ at: [0, 0], span: [6, 1], widget: paperboy/clock, args: { format: date } }", `<option value="6x1" selected>`} {
		if !strings.Contains(body, want) {
			t.Fatalf("gallery with args misses %q: %s", want, body)
		}
	}
	if strings.Contains(body, "/api/widget/paperboy/header?arg.") {
		t.Fatal("args leak into other widgets")
	}
	// defaults are left out of the snippet, strings are quoted when needed
	if got := cellSnippet("p", "w", [2]int{3, 2}, map[string]string{"a": "left", "b": "2", "c": "x, z", "d": "true"},
		map[string]plugin.Arg{"a": {Type: "string", Default: "left"}, "b": {Type: "string"}, "c": {Type: "list"}, "d": {Type: "bool"}}); got != `{ at: [0, 0], span: [3, 2], widget: p/w, args: { b: "2", c: [x, z], d: true } }` {
		t.Fatalf("snippet: %s", got)
	}
	// an invalid enum value comes back as the widget error
	body = preview(t, s, "/ui/plugins?display=small&widget=paperboy/clock&arg.format=year").Body.String()
	if !strings.Contains(body, `<option value="year" selected>year (not allowed)</option>`) {
		t.Fatalf("invalid value not kept in the form: %s", body)
	}
	rec := preview(t, s, "/api/widget/paperboy/clock?display=small&arg.format=%22year%22")
	if !strings.Contains(rec.Header().Get("X-Widget-Error"), "format: year is not one of time, date, datetime") {
		t.Fatalf("enum error: %q", rec.Header())
	}
}

func TestWidgetGalleryBoolCheckbox(t *testing.T) {
	s, _ := testServer(t)
	s.config().Timezone = time.UTC
	// defaults: clock true → checked, date false → unchecked
	body := preview(t, s, "/ui/plugins?display=small").Body.String()
	for _, want := range []string{
		`<input type="hidden" name="arg.clock" value="false"><input type="checkbox" name="arg.clock" value="true" checked id="paperboy-header-arg.clock">`,
		`<input type="hidden" name="arg.date" value="false"><input type="checkbox" name="arg.date" value="true" id="paperboy-header-arg.date">`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("checkbox missing %q", want)
		}
	}
	// a submitted form: clock unchecked (only false), date checked (false then true)
	body = preview(t, s, "/ui/plugins?display=small&widget=paperboy/header&arg.clock=false&arg.date=false&arg.date=true&arg.page_indicator=false&arg.page_indicator=true").Body.String()
	for _, want := range []string{
		`<input type="checkbox" name="arg.clock" value="true" id=`,
		`<input type="checkbox" name="arg.date" value="true" checked id=`,
		"args: { clock: false, date: true } }",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("submitted form misses %q: %s", want, body)
		}
	}
	// the API takes the last value of a repeated key
	if got := widgetArgs(url.Values{"arg.x": {"false", "true"}})["x"]; got != true {
		t.Fatalf("last value: %v", got)
	}
	// bools equal to their default, or false without a default, stay out of the snippet
	if got := cellSnippet("p", "w", [2]int{1, 1}, map[string]string{"a": "true", "b": "false"},
		map[string]plugin.Arg{"a": {Type: "bool", Default: true}, "b": {Type: "bool"}}); got != "{ at: [0, 0], span: [1, 1], widget: p/w }" {
		t.Fatalf("snippet: %s", got)
	}
}
