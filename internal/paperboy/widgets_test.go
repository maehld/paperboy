package paperboy

import (
	"bytes"
	"image/png"
	"net/http"
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
