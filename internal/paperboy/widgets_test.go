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
// page — the gallery it once shared the page with is gone (ADR 0077).
func TestWidgetsPage(t *testing.T) {
	s, _ := testServer(t)
	s.config().Profiles["lcd"] = Profile{Kind: kindText, Charset: "ascii", Width: 20, Height: 4}
	s.config().Timezone = time.UTC

	body := preview(t, s, "/ui/plugins").Body.String()
	for _, want := range []string{
		`"widgets_page":true`, `"display":"small"`, `id="palette"`, `id="inspector"`, `id="snippet"`,
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
	// (issue #171).
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
		`data-display="lcd"`,
		`href="/ui/plugins?arg.date=dd&arg.format=date&display=small&span=3x2&widget=paperboy%2Fclock"`,
		`href="/ui/plugins?arg.date=dd&arg.format=date&display=lcd&span=3x2&widget=paperboy%2Fclock"`,
	} {
		if !strings.Contains(switchBox, want) {
			t.Fatalf("display type switch misses %q: %s", want, switchBox)
		}
	}

	// A text display starts from its character grid.
	body = preview(t, s, "/ui/plugins?display=lcd&widget=paperboy/text").Body.String()
	for _, want := range []string{`"kind":"text"`, `"width":20`, `"height":4`, `"grid":[20,4]`} {
		if !strings.Contains(body, want) {
			t.Fatalf("text widgets page misses %q: %s", want, body)
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
