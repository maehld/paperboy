package page

import (
	"image"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/internal/data"
)

func renderCtx(t *testing.T, tmpl *Template, doc *data.Document, d Display, c Context) *image.Paletted {
	t.Helper()
	img, err := tmpl.RenderContext(doc, d, c)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestColumnsSplitAndDivider(t *testing.T) {
	now := time.Date(2026, 10, 21, 13, 12, 0, 0, time.UTC)
	tmpl, err := Parse(strings.NewReader(`
rows: [1]
blocks:
  - columns:
      divider: 2
      items:
        - weight: 30
          block: { text: "L" }
        - weight: 70
          block: { text: "R" }
`))
	if err != nil {
		t.Fatal(err)
	}
	img := renderCtx(t, tmpl, testDocument(t, &now), Display{Width: 200, Height: 60}, Context{Now: now})
	divider := image.Rect(200*30/100-2, 0, 200*30/100+2, 60)
	if n := blackIn(img, divider); n < 50 {
		t.Fatalf("column divider missing (%d black pixels)", n)
	}
	// The gap between the two centered texts belongs to no column text.
	if n := blackIn(img, image.Rect(70, 0, 110, 60)); n > 0 {
		t.Fatalf("column texts overlap the wrong column (%d black pixels)", n)
	}
	if n := blackIn(img, image.Rect(115, 0, 150, 60)); n < 10 {
		t.Fatal("right column text missing")
	}
}

func TestHeaderStatusAndPageIndicator(t *testing.T) {
	now := time.Date(2026, 10, 21, 13, 12, 0, 0, time.UTC)
	doc := testDocument(t, &now)
	fillSolar(t, doc, now)
	tmpl, err := Parse(strings.NewReader(`
rows: [1, 2]
blocks:
  - header:
      title: Solar
      align: left
      status: { time: solar.current.latest.time, label: Stand }
      page_indicator: true
  - text: "{{ page }}/{{ pages }}"
`))
	if err != nil {
		t.Fatal(err)
	}
	d := Display{Width: 300, Height: 120}
	one := renderCtx(t, tmpl, doc, d, Context{Now: now, Page: 1, Pages: 1})
	two := renderCtx(t, tmpl, doc, d, Context{Now: now, Page: 2, Pages: 4})
	if blackIn(two, image.Rect(0, 0, 300, 40)) < 20 {
		t.Fatal("header looks blank")
	}
	if encode(one) == nil || string(encode(one)) == string(encode(two)) {
		t.Fatal("page indicator and page/pages must change the image")
	}
	// The status area on the right must contain the time.
	if n := blackIn(two, image.Rect(200, 0, 300, 40)); n < 20 {
		t.Fatalf("status area looks blank (%d black pixels)", n)
	}
}

func TestStaleBadgeReplacesBottomBar(t *testing.T) {
	now := time.Date(2026, 10, 21, 13, 12, 0, 0, time.UTC)
	const page = `
watch:
  - { path: solar/current, stale_after: 5m, label: Solar }
notices: %s
rows: [1, 2]
blocks:
  - header:
      title: Solar
      status: { time: solar.current.latest.time, label: Stand }
  - text: "ok"
`
	render := func(notices string, stale bool) *image.Paletted {
		tmpl, err := Parse(strings.NewReader(strings.Replace(page, "%s", notices, 1)))
		if err != nil {
			t.Fatal(err)
		}
		doc := testDocument(t, &now)
		at := now
		if stale {
			at = now.Add(-10 * time.Minute)
		}
		put(t, doc, "solar/current", map[string]any{"latest": map[string]any{"time": at.Format(time.RFC3339)}}, at)
		return renderCtx(t, tmpl, doc, Display{Width: 300, Height: 120}, Context{Now: now})
	}
	bar := render("bar", true)
	badge := render("badge", true)
	none := render("none", true)
	bottom := image.Rect(0, 120-120/8, 300, 120)
	if blackIn(bar, bottom) < 500 {
		t.Fatal("notices bar must fill the bottom bar")
	}
	if blackIn(badge, bottom) > 20 {
		t.Fatal("notices badge must not fill the bottom bar")
	}
	if blackIn(none, bottom) > 20 {
		t.Fatal("notices none must not fill the bottom bar")
	}
	if blackIn(badge, image.Rect(150, 0, 300, 40)) < 100 {
		t.Fatal("stale badge missing in the header")
	}
	if blackIn(none, image.Rect(150, 0, 300, 40)) > blackIn(badge, image.Rect(150, 0, 300, 40)) {
		t.Fatal("inverted badge must add black pixels")
	}
}

func TestTileDetailAddsThirdLine(t *testing.T) {
	now := time.Date(2026, 10, 21, 13, 12, 0, 0, time.UTC)
	doc := testDocument(t, &now)
	fillSolar(t, doc, now)
	const page = `
rows: [1]
blocks:
  - tiles:
      columns: 1
      items:
        - label: Akku
          value: solar.current.latest.values.battery_soc
          format: percent
          %s
`
	plain, err := Parse(strings.NewReader(strings.Replace(page, "%s", "", 1)))
	if err != nil {
		t.Fatal(err)
	}
	detailed, err := Parse(strings.NewReader(strings.Replace(page, "%s",
		"detail: '{{ if below .solar.current.latest.values.battery_soc 90 }}lädt{{ else }}voll{{ end }}'", 1)))
	if err != nil {
		t.Fatal(err)
	}
	d := Display{Width: 200, Height: 100}
	a := renderCtx(t, plain, doc, d, Context{Now: now})
	b := renderCtx(t, detailed, doc, d, Context{Now: now})
	if blackIn(b, image.Rect(0, 70, 200, 100)) <= blackIn(a, image.Rect(0, 70, 200, 100)) {
		t.Fatal("detail line is missing")
	}
}

func TestTemplateContextFunctions(t *testing.T) {
	now := time.Date(2026, 10, 21, 13, 12, 0, 0, time.UTC)
	doc := testDocument(t, &now)
	put(t, doc, "solar/current/latest", map[string]any{
		"values": map[string]any{"battery_power": -500.0, "battery_soc": 81.0},
	}, now)
	tmpl, err := Parse(strings.NewReader(`
watch:
  - { path: solar/current, stale_after: 5m }
rows: [1]
blocks:
  - text: '{{ page }}/{{ pages }} {{ time now }} {{ if below .solar.current.latest.values.battery_power -10 }}entlädt{{ end }} {{ if above .solar.current.latest.values.battery_soc 50 }}voll{{ end }} {{ if stale "solar/current" }}alt{{ else }}neu{{ end }}'
`))
	if err != nil {
		t.Fatal(err)
	}
	img := renderCtx(t, tmpl, doc, Display{Width: 400, Height: 60}, Context{Now: now, Page: 2, Pages: 3})
	if blackIn(img, img.Rect) < 20 {
		t.Fatal("context text looks blank")
	}
	if _, err := tmpl.RenderContext(doc, Display{Width: 400, Height: 60},
		Context{Now: now, Page: 2, Pages: 3}); err != nil {
		t.Fatalf("render with fresh data: %v", err)
	}
}

func TestUnknownStalePathFails(t *testing.T) {
	now := time.Date(2026, 10, 21, 13, 12, 0, 0, time.UTC)
	tmpl, err := Parse(strings.NewReader(`
rows: [1]
blocks:
  - text: '{{ if stale "solar/current" }}x{{ end }}'
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tmpl.Render(testDocument(t, &now), Display{Width: 100, Height: 40}, now); err == nil {
		t.Fatal("stale of an unwatched path must be a template error")
	}
}

func TestRuleAndPadding(t *testing.T) {
	now := time.Date(2026, 10, 21, 13, 12, 0, 0, time.UTC)
	tmpl, err := Parse(strings.NewReader(`
rows: [1]
blocks:
  - text: "x"
    rule: 3
    padding: 20
`))
	if err != nil {
		t.Fatal(err)
	}
	img := renderCtx(t, tmpl, testDocument(t, &now), Display{Width: 200, Height: 100}, Context{Now: now})
	if n := blackIn(img, image.Rect(0, 97, 200, 100)); n < 550 {
		t.Fatalf("bottom rule missing (%d black pixels)", n)
	}
	// With 20 px padding, nothing may be drawn in the top-left corner.
	if n := blackIn(img, image.Rect(0, 0, 20, 20)); n > 0 {
		t.Fatalf("padding ignored (%d black pixels)", n)
	}
}

func TestFormatUsesDisplayTimezone(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC)
	doc := testDocument(t, &now)
	put(t, doc, "solar/current/latest", map[string]any{"time": "2026-10-21T11:00:00Z"}, now)
	tmpl, err := Parse(strings.NewReader(`
rows: [1]
blocks:
  - header: { title: Solar, status: { time: solar.current.latest.time } }
`))
	if err != nil {
		t.Fatal(err)
	}
	berlin := renderCtx(t, tmpl, doc, Display{Width: 300, Height: 100, Timezone: loc}, Context{Now: now})
	utc := renderCtx(t, tmpl, doc, Display{Width: 300, Height: 100, Timezone: time.UTC}, Context{Now: now})
	if string(encode(berlin)) == string(encode(utc)) {
		t.Fatal("display timezone must affect the header clock")
	}
}
