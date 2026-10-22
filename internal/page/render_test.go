package page

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/internal/data"
)

func testDocument(t *testing.T, now *time.Time) *data.Document {
	t.Helper()
	d, err := data.New(filepath.Join(t.TempDir(), "data.json"), func() time.Time { return *now })
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func fillSolar(t *testing.T, doc *data.Document, at time.Time) {
	t.Helper()
	put(t, doc, "solar/current/latest", map[string]any{
		"time": "2026-10-21T11:12:03Z",
		"values": map[string]any{
			"pv_power": 4210.0, "load_power": 820.0, "grid_power": -1330.0, "battery_soc": 81.5,
		},
	}, at)
	put(t, doc, "solar/history/points", []any{
		map[string]any{"values": map[string]any{"pv_power": 1000.0, "load_power": 500.0}},
		map[string]any{"values": map[string]any{"pv_power": 3000.0, "load_power": 800.0}},
		map[string]any{"values": map[string]any{"pv_power": 4210.0, "load_power": 820.0}},
	}, at)
}

func put(t *testing.T, doc *data.Document, path string, v any, at time.Time) {
	t.Helper()
	if err := doc.Put(path, v, at); err != nil {
		t.Fatal(err)
	}
}

func render(t *testing.T, tmpl *Template, doc *data.Document, d Display, now time.Time) *image.Paletted {
	t.Helper()
	img, err := tmpl.Render(doc, d, now)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func encode(img image.Image) []byte {
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func blackIn(img *image.Paletted, r image.Rectangle) int {
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if img.ColorIndexAt(x, y) == 0 {
				n++
			}
		}
	}
	return n
}

func TestRenderPage(t *testing.T) {
	now := time.Date(2026, 10, 21, 13, 12, 0, 0, time.Local)
	doc := testDocument(t, &now)
	fillSolar(t, doc, now)
	tmpl, err := Parse(strings.NewReader(validTemplate))
	if err != nil {
		t.Fatal(err)
	}
	d := Display{Width: 200, Height: 120, Gray: true}
	img := render(t, tmpl, doc, d, now)

	if img.Rect.Dx() != 200 || img.Rect.Dy() != 120 || len(img.Palette) != 4 {
		t.Fatalf("image %v with %d palette entries", img.Rect, len(img.Palette))
	}
	if blackIn(img, img.Rect) < 100 {
		t.Fatal("page looks blank")
	}
	// Rendering is deterministic: the device filename hash relies on it.
	if again := render(t, tmpl, doc, d, now); !bytes.Equal(encode(img), encode(again)) {
		t.Fatal("two renders of the same data differ")
	}

	// Changed data must change the image.
	put(t, doc, "solar/current/latest/values/pv_power", 100.0, now)
	if changed := render(t, tmpl, doc, d, now); bytes.Equal(encode(img), encode(changed)) {
		t.Fatal("changed value did not change the image")
	}
}

func TestMissingDataRendersDashes(t *testing.T) {
	now := time.Date(2026, 10, 21, 13, 12, 0, 0, time.Local)
	doc := testDocument(t, &now) // empty document
	tmpl, err := Parse(strings.NewReader(validTemplate))
	if err != nil {
		t.Fatal(err)
	}
	img := render(t, tmpl, doc, Display{Width: 200, Height: 120, Gray: true}, now)
	if blackIn(img, img.Rect) < 20 {
		t.Fatal("page without data looks blank")
	}
}

func TestFormats(t *testing.T) {
	de := formatter{comma: true}
	en := formatter{comma: false}
	clock := func(s string) string {
		ts, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return ts.Local().Format("15:04")
	}
	cases := []struct {
		f    formatter
		v    any
		kind string
		want string
	}{
		{de, 4210.0, "power", "4,21 kW"},
		{de, 820.0, "power", "820 W"},
		{en, 4210.0, "power", "4.21 kW"},
		{de, 81.5, "percent", "82 %"},
		{de, 0.87, "ratio", "87 %"},
		{de, 12.4, "energy", "12,4 kWh"},
		{de, 18.4, "temp", "18,4 °C"},
		{de, -5.0, "abs", "5"},
		{de, 18.4, "", "18,4"},
		{de, "Sonnig", "", "Sonnig"},
		{de, nil, "power", "—"},
		{de, "2026-10-21T11:12:03Z", "time", clock("2026-10-21T11:12:03Z")},
		{de, "keine Zeit", "time", "—"},
	}
	for _, c := range cases {
		if got := c.f.format(c.v, c.kind); got != c.want {
			t.Fatalf("%s of %v: got %q, want %q", c.kind, c.v, got, c.want)
		}
	}
}

func TestSignWords(t *testing.T) {
	r := &renderer{t: &Template{}, root: map[string]any{}, f: formatter{comma: true}, language: "de"}
	r.root["grid"] = -1330.0
	tile := Tile{Value: "grid", Format: "power", Sign: []string{"Einspeisung", "Bezug"}}
	if got := r.value(tile); got != "1,33 kW Einspeisung" {
		t.Fatalf("negative: %q", got)
	}
	r.root["grid"] = 500.0
	if got := r.value(tile); got != "500 W Bezug" {
		t.Fatalf("positive: %q", got)
	}
}

func TestStaleBanner(t *testing.T) {
	now := time.Date(2026, 10, 21, 13, 12, 0, 0, time.Local)
	tmpl, err := Parse(strings.NewReader(`
watch:
  - { path: solar/current, stale_after: 5m }
rows: [1, 1]
blocks:
  - header: { title: Solar }
  - text: "ok"
`))
	if err != nil {
		t.Fatal(err)
	}
	d := Display{Width: 200, Height: 120, Gray: false}
	bottom := image.Rect(0, 120-120/8, 200, 120)

	fresh := testDocument(t, &now)
	put(t, fresh, "solar/current", map[string]any{}, now)
	if n := blackIn(render(t, tmpl, fresh, d, now), bottom); n > 50 {
		t.Fatalf("fresh data must not show a banner (%d black pixels)", n)
	}

	stale := testDocument(t, &now)
	put(t, stale, "solar/current", map[string]any{}, now.Add(-10*time.Minute))
	if n := blackIn(render(t, tmpl, stale, d, now), bottom); n < 1500 {
		t.Fatalf("stale data must show a banner (%d black pixels)", n)
	}
}

func TestBuiltInTextsFollowLanguage(t *testing.T) {
	now := time.Date(2026, 10, 21, 13, 12, 0, 0, time.Local)
	tmpl, err := Parse(strings.NewReader(`
watch:
  - { path: solar/current, stale_after: 5m }
rows: [1]
blocks:
  - text: "{{ power .solar.current.latest.values.pv_power }}"
`))
	if err != nil {
		t.Fatal(err)
	}
	doc := testDocument(t, &now)
	put(t, doc, "solar/current", map[string]any{"latest": map[string]any{"values": map[string]any{"pv_power": 1330}}}, now.Add(-10*time.Minute))
	if got := tmpl.staleBanners(doc, now, "de"); len(got) != 1 || !strings.Contains(got[0], "Keine neuen Daten seit") {
		t.Fatalf("german banner: %q", got)
	}
	if got := tmpl.staleBanners(doc, now, "en"); len(got) != 1 || !strings.Contains(got[0], "No new data since") {
		t.Fatalf("english banner: %q", got)
	}
	de := render(t, tmpl, doc, Display{Width: 200, Height: 120}, now)
	en := render(t, tmpl, doc, Display{Width: 200, Height: 120, Language: "en"}, now)
	if bytes.Equal(de.Pix, en.Pix) {
		t.Fatal("language must affect formatting and the banner")
	}
}

func TestTemplateHelpers(t *testing.T) {
	now := time.Date(2026, 10, 21, 13, 12, 0, 0, time.Local)
	tmpl, err := Parse(strings.NewReader(`
rows: [1]
blocks:
  - text: "Haus {{ power .solar.current.latest.values.load_power }} / {{ percent .fehlt }}"
`))
	if err != nil {
		t.Fatal(err)
	}
	doc := testDocument(t, &now)
	fillSolar(t, doc, now)
	render(t, tmpl, doc, Display{Width: 200, Height: 60}, now)
}

func TestTemplateParseErrorIsReported(t *testing.T) {
	now := time.Date(2026, 10, 21, 13, 12, 0, 0, time.Local)
	tmpl, err := Parse(strings.NewReader(`
rows: [1]
blocks:
  - text: "{{ power .a"
`))
	if err != nil {
		t.Fatal(err)
	}
	doc := testDocument(t, &now)
	if _, err := tmpl.Render(doc, Display{Width: 100, Height: 40}, now); err == nil {
		t.Fatal("a broken text template must fail the render")
	}
}

func TestMissingTemplateBranchShowsDash(t *testing.T) {
	r := &renderer{t: &Template{}, root: map[string]any{}, f: formatter{comma: true}, language: "de"}
	// A missing supplier must not fail the page: branches are created empty.
	if got := r.expand("{{ temp .wetter.aktuell.temp }}"); got != "—" {
		t.Fatalf("helper: %q", got)
	}
	if got := r.expand("{{ .wetter.aktuell.text }}"); got != "—" {
		t.Fatalf("bare field: %q", got)
	}
	if _, ok := r.root["wetter"].(map[string]any); !ok {
		t.Fatalf("branch not created: %v", r.root)
	}
}

func TestChartWithoutPoints(t *testing.T) {
	now := time.Date(2026, 10, 21, 13, 12, 0, 0, time.Local)
	tmpl, err := Parse(strings.NewReader(`
rows: [1]
blocks:
  - chart: { points: solar.history.points, series: [pv_power], unit: kW }
`))
	if err != nil {
		t.Fatal(err)
	}
	img := render(t, tmpl, testDocument(t, &now), Display{Width: 200, Height: 100}, now)
	if blackIn(img, img.Rect) < 20 {
		t.Fatal("expected a notice instead of an empty chart")
	}
}

func TestBandsCoverHeight(t *testing.T) {
	tmpl, err := Parse(strings.NewReader(validTemplate))
	if err != nil {
		t.Fatal(err)
	}
	bands := tmpl.bands(Display{Width: 200, Height: 120})
	total, y := 0, 0
	for _, b := range bands {
		if b.Min.Y != y || b.Dx() != 200 || b.Dy() < 1 {
			t.Fatalf("band %v after y=%d", b, y)
		}
		total += b.Dy()
		y = b.Max.Y
	}
	if total != 120 {
		t.Fatalf("bands cover %d px, want 120", total)
	}
}

func TestEdgePalette(t *testing.T) {
	// A monochrome page must produce a 1-bit palette like the image API.
	now := time.Date(2026, 10, 21, 13, 12, 0, 0, time.Local)
	tmpl, err := Parse(strings.NewReader(validTemplate))
	if err != nil {
		t.Fatal(err)
	}
	doc := testDocument(t, &now)
	fillSolar(t, doc, now)
	img := render(t, tmpl, doc, Display{Width: 200, Height: 120}, now)
	if len(img.Palette) != 2 {
		t.Fatalf("mono page has %d palette entries", len(img.Palette))
	}
	if c := img.Palette[0].(color.Gray); c.Y != 0 {
		t.Fatalf("first palette entry must be black: %v", c)
	}
}
