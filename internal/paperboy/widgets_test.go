package paperboy

import (
	"bytes"
	"image/png"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
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
	// The API takes the last value of a repeated key, as a checkbox after its
	// hidden "false" input sends it.
	if got := widgetArgs(url.Values{"arg.x": {"false", "true"}})["x"]; got != true {
		t.Fatalf("last value: %v", got)
	}
	// And an invalid enum value comes back as the widget error.
	rec = preview(t, s, "/api/widget/paperboy/clock?display=small&arg.format=%22year%22")
	if !strings.Contains(rec.Header().Get("X-Widget-Error"), "format: year is not one of time, date, datetime") {
		t.Fatalf("enum error: %q", rec.Header())
	}
}

// TestWidgetsPage is the widgets page: the palette and the inspector of the page
// editor around one widget, with the cell to copy (ADR 0055). It is the whole
// page — the gallery it once shared the page with is gone (ADR 0077). The page
// offers image display types only (ADR 0124).
func TestWidgetsPage(t *testing.T) {
	s, _ := testServer(t)
	s.config().Profiles["lcd"] = Profile{Kind: kindText, Charset: "ascii", Width: 20, Height: 4}
	s.config().Timezone = time.UTC

	body := preview(t, s, "/ui/plugins").Body.String()
	for _, want := range []string{
		`"widgets_page":true`, `"display":"small"`, `"kind":"image"`, `id="palette"`, `id="inspector"`, `id="snippet"`,
		`id="preview-img"`, "Copy cell",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("widgets page misses %q: %s", want, body)
		}
	}
	if strings.Contains(body, `class="tabs"`) {
		t.Fatalf("the widgets page still has sub-tabs: %s", body)
	}
	if strings.Contains(body, `id="board"`) {
		t.Fatalf("the widgets page draws a board: %s", body)
	}

	// The link carries the widget, its size and its args, and the display type
	// switch keeps them too, so a click opens the same cell on another type
	// (issue #171). Text display types are not offered (ADR 0124).
	body = preview(t, s, "/ui/plugins?display=small&widget=paperboy/clock&span=3x2&arg.format=date&arg.date=dd").Body.String()
	for _, want := range []string{`"widget":"paperboy/clock"`, `"span":[3,2]`, `"format":"date"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("widgets page link misses %q: %s", want, body)
		}
	}
	switchBox := body[strings.Index(body, `id="display-switch"`):]
	switchBox = switchBox[:strings.Index(switchBox, "</div>")]
	switchBox = strings.ReplaceAll(switchBox, "&amp;", "&")
	for _, want := range []string{
		`data-display="small"`,
		`href="/ui/plugins?arg.date=dd&arg.format=date&display=small&span=3x2&widget=paperboy%2Fclock"`,
	} {
		if !strings.Contains(switchBox, want) {
			t.Fatalf("display type switch misses %q: %s", want, switchBox)
		}
	}
	for _, unwanted := range []string{`data-display="lcd"`, `display=lcd`} {
		if strings.Contains(switchBox, unwanted) {
			t.Fatalf("display type switch still offers the text type (%q): %s", unwanted, switchBox)
		}
	}

	// A link with a text display type falls back to an image type and names the
	// page editor, because a text page is a character grid of boxes and no cell
	// the Playground makes is valid in it (ADR 0111, 0124).
	body = preview(t, s, "/ui/plugins?display=lcd&widget=paperboy/text&span=20x4").Body.String()
	for _, want := range []string{`"display":"small"`, `"kind":"image"`, `"grid":[6,8]`,
		`id="playground-notice"`, `href="/ui/editor?tab=pages"`, "Text displays are edited under"} {
		if !strings.Contains(body, want) {
			t.Fatalf("text display fallback misses %q: %s", want, body)
		}
	}
	for _, unwanted := range []string{`"kind":"text"`, `"grid":[20,4]`, `"span":`, `data-display="lcd"`} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("text display fallback still shows %q: %s", unwanted, body)
		}
	}

	// A link to the removed gallery still opens the widgets page.
	if body := preview(t, s, "/ui/plugins?tab=gallery").Body.String(); !strings.Contains(body, `"widgets_page":true`) {
		t.Fatalf("an old gallery link does not land on the widgets page: %s", body)
	}
	if !strings.Contains(preview(t, s, "/ui").Body.String(), `href="/ui/plugins"`) {
		t.Fatal("dashboard does not link the widgets page")
	}
}

// TestWidgetsPageTextOnly is an instance with text display types only: the
// Playground says where text pages are edited instead of failing (ADR 0124).
// GET /api/widget still answers text widgets, because the protocol keeps the
// text action for older plugins.
func TestWidgetsPageTextOnly(t *testing.T) {
	s, _ := testServer(t)
	delete(s.config().Profiles, "small")
	s.config().Profiles["lcd"] = Profile{Kind: kindText, Charset: "ascii", Width: 20, Height: 4}
	s.config().Timezone = time.UTC

	for _, path := range []string{"/ui/plugins", "/ui/plugins?display=lcd", "/ui/plugins?display=small"} {
		rec := preview(t, s, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
		body := rec.Body.String()
		for _, want := range []string{`id="playground-notice"`, "Every configured display type is a text display",
			`href="/ui/editor?tab=pages"`} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s misses %q: %s", path, want, body)
			}
		}
		if strings.Contains(body, `id="palette"`) {
			t.Fatalf("%s still renders the image palette: %s", path, body)
		}
	}

	rec := preview(t, s, "/api/widget/paperboy/text?display=lcd&arg.text=Hi")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("text widget with a text-only instance: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	lines := strings.Split(strings.TrimRight(rec.Body.String(), "\n"), "\n")
	if len(lines) != 4 || strings.TrimSpace(lines[0]) != "Hi" || len(lines[0]) != 20 {
		t.Fatalf("text widget lines: %q", rec.Body.String())
	}

	// No display type at all is a configuration error, not a text-only instance.
	delete(s.config().Profiles, "lcd")
	if rec := preview(t, s, "/ui/plugins"); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "no display type is configured") {
		t.Fatalf("no display type: %d %s", rec.Code, rec.Body)
	}
}
