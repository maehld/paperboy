package page

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/internal/data"
)

// solidPNG encodes a picture whose ink is a centred square of size fraction.
func solidPNG(t *testing.T, w, h int, fraction float64, transparent bool) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	if !transparent {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				img.SetNRGBA(x, y, color.NRGBA{255, 255, 255, 255})
			}
		}
	}
	inner := int(float64(min(w, h)) * fraction)
	x0, y0 := (w-inner)/2, (h-inner)/2
	for y := y0; y < y0+inner; y++ {
		for x := x0; x < x0+inner; x++ {
			img.SetNRGBA(x, y, color.NRGBA{0, 0, 0, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// iconTemplate writes a page and its icons into a temporary instance directory
// and loads the page, so the icon directory is derived like in production.
func iconTemplate(t *testing.T, pageYAML string, icons map[string][]byte) *Template {
	t.Helper()
	dir := t.TempDir()
	pages := filepath.Join(dir, "pages")
	if err := os.MkdirAll(pages, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "icons"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range icons {
		if err := os.WriteFile(filepath.Join(dir, "icons", name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(pages, "page.small.yaml"), []byte(pageYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	tmpl, err := Load(pages, "page", "small")
	if err != nil {
		t.Fatal(err)
	}
	return tmpl
}

func iconDocument(t *testing.T) *data.Document {
	t.Helper()
	doc, err := data.New(filepath.Join(t.TempDir(), "data.json"), func() time.Time {
		return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func renderIcon(t *testing.T, tmpl *Template, doc *data.Document) *image.Paletted {
	t.Helper()
	img, err := tmpl.RenderContext(doc, Display{Width: 200, Height: 100, Language: "en", Timezone: time.UTC},
		Context{Now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC), Page: 1, Pages: 1})
	if err != nil {
		t.Fatal(err)
	}
	return img
}

const iconPage = `
rows: [100]
blocks:
  - name: bild
    icon:
      source: sun.png
`

func TestMaskOf(t *testing.T) {
	opaque := maskOf(decodePNG(t, solidPNG(t, 40, 40, 0.5, false)))
	alpha := maskOf(decodePNG(t, solidPNG(t, 40, 40, 0.5, true)))
	for name, m := range map[string]*mask{"opaque background": opaque, "transparent background": alpha} {
		if !m.at(20, 20) {
			t.Fatalf("%s: the square must be ink", name)
		}
		if m.at(2, 2) || m.at(37, 37) {
			t.Fatalf("%s: the background must not be ink", name)
		}
	}
	// trim removes the margins around the square.
	trimmed := opaque.trim()
	if trimmed == nil || trimmed.w != 20 || trimmed.h != 20 {
		t.Fatalf("trimmed mask: %+v", trimmed)
	}
	for y := 0; y < trimmed.h; y++ {
		for x := 0; x < trimmed.w; x++ {
			if !trimmed.at(x, y) {
				t.Fatalf("trimmed mask has a hole at %d,%d", x, y)
			}
		}
	}
}

func decodePNG(t *testing.T, raw []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestIconPath(t *testing.T) {
	dir := t.TempDir()
	if got, err := iconPath(dir, " sun.png "); err != nil || got != filepath.Join(dir, "sun.png") {
		t.Fatalf("relative: %q %v", got, err)
	}
	if got, err := iconPath(dir, "set/sun.png"); err != nil || got != filepath.Join(dir, "set", "sun.png") {
		t.Fatalf("subdirectory: %q %v", got, err)
	}
	for _, bad := range []string{"", "../secret.png", "/etc/passwd", `..\secret.png`} {
		if _, err := iconPath(dir, bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestIconDrawsShape(t *testing.T) {
	tmpl := iconTemplate(t, iconPage, map[string][]byte{"sun.png": solidPNG(t, 40, 20, 0.5, true)})
	img := renderIcon(t, tmpl, iconDocument(t))

	// The square is 20x10 source pixels scaled to the region: it covers the
	// middle, the corners stay white.
	if img.ColorIndexAt(100, 50) != 0 {
		t.Fatal("the icon must be drawn in the centre")
	}
	if img.ColorIndexAt(5, 5) != 1 || img.ColorIndexAt(195, 95) != 1 {
		t.Fatal("the icon must not fill the region")
	}
}

func TestIconAlign(t *testing.T) {
	const page = `
rows: [100]
blocks:
  - name: bild
    icon: { source: sun.png, align: %s }
`
	for _, tc := range []struct {
		align string
		x     int
	}{{"left", 40}, {"center", 100}, {"right", 160}} {
		tmpl := iconTemplate(t, strings.Replace(page, "%s", tc.align, 1),
			map[string][]byte{"sun.png": solidPNG(t, 40, 40, 0.6, true)})
		img := renderIcon(t, tmpl, iconDocument(t))
		if img.ColorIndexAt(tc.x, 50) != 0 {
			t.Fatalf("%s: no ink at x=%d", tc.align, tc.x)
		}
	}
}

func TestIconLabel(t *testing.T) {
	const page = `
rows: [100]
blocks:
  - name: bild
    icon:
      source: sun.png
      label: Pullover
      label_weight: bold
`
	tmpl := iconTemplate(t, page, map[string][]byte{"sun.png": solidPNG(t, 20, 20, 0.6, true)})
	img := renderIcon(t, tmpl, iconDocument(t))
	label := image.Rect(0, 78, 200, 100)
	if blackIn(img, label) < 20 {
		t.Fatal("the label must be drawn below the icon")
	}
}

func TestIconShorthand(t *testing.T) {
	const page = `
rows: [100]
blocks:
  - name: bild
    icon: sun.png
`
	tmpl := iconTemplate(t, page, map[string][]byte{"sun.png": solidPNG(t, 20, 20, 0.6, true)})
	if tmpl.Blocks[0].Icon == nil || tmpl.Blocks[0].Icon.Source != "sun.png" {
		t.Fatalf("shorthand not normalized: %+v", tmpl.Blocks[0].Icon)
	}
	renderIcon(t, tmpl, iconDocument(t))
}

func TestIconSourceIsTemplate(t *testing.T) {
	const page = `
rows: [100]
blocks:
  - name: bild
    icon:
      source: '{{ if below .weather.current.apparent 15 }}pullover{{ else }}tshirt{{ end }}.png'
`
	tmpl := iconTemplate(t, page, map[string][]byte{"pullover.png": solidPNG(t, 20, 20, 0.6, true)})

	doc := iconDocument(t)
	if err := doc.Put("weather/current/apparent", 9.5, time.Time{}); err != nil {
		t.Fatal(err)
	}
	renderIcon(t, tmpl, doc)

	// Above the threshold the template asks for tshirt.png, which does not
	// exist: the render must fail instead of showing the wrong picture.
	tmpl = iconTemplate(t, page, map[string][]byte{"pullover.png": solidPNG(t, 20, 20, 0.6, true)})
	doc = iconDocument(t)
	if err := doc.Put("weather/current/apparent", 20.0, time.Time{}); err != nil {
		t.Fatal(err)
	}
	_, err := tmpl.RenderContext(doc, Display{Width: 200, Height: 100, Gray: true}, Context{Now: time.Now()})
	if err == nil || !strings.Contains(err.Error(), "tshirt.png") {
		t.Fatalf("error = %v", err)
	}
}

func TestIconWithoutDataDrawsNothing(t *testing.T) {
	const page = `
rows: [100]
blocks:
  - name: bild
    icon:
      source: '{{ if .weather.current.symbol }}{{ .weather.current.symbol }}.png{{ end }}'
      label: '{{ if .weather.current.symbol }}Wetter{{ end }}'
`
	// No weather data and no icons: the page must render empty instead of
	// failing on a file called ".png" (a missing value is not an em dash here).
	tmpl := iconTemplate(t, page, nil)
	img := renderIcon(t, tmpl, iconDocument(t))
	if n := blackIn(img, img.Rect); n != 0 {
		t.Fatalf("expected an empty page, found %d ink pixels", n)
	}
}

func TestIconUsesExpandedSource(t *testing.T) {
	const page = `
rows: [100]
blocks:
  - name: bild
    icon: { source: '{{ .weather.current.symbol }}.png' }
`
	tmpl := iconTemplate(t, page, map[string][]byte{"sun.png": solidPNG(t, 20, 20, 0.6, true)})
	doc := iconDocument(t)
	if err := doc.Put("weather/current/symbol", "sun", time.Time{}); err != nil {
		t.Fatal(err)
	}
	img := renderIcon(t, tmpl, doc)
	if img.ColorIndexAt(100, 50) != 0 {
		t.Fatal("the icon chosen by the data must be drawn")
	}
}

func TestIconMissingFileFails(t *testing.T) {
	tmpl := iconTemplate(t, iconPage, nil)
	_, err := tmpl.RenderContext(iconDocument(t), Display{Width: 200, Height: 100, Gray: true}, Context{Now: time.Now()})
	if err == nil || !strings.Contains(err.Error(), "sun.png") {
		t.Fatalf("error = %v", err)
	}
}

func TestIconValidation(t *testing.T) {
	tests := []struct {
		name string
		page string
		want string
	}{
		{"source", "rows: [100]\nblocks:\n  - name: a\n    icon: {}\n", "source missing"},
		{"align", "rows: [100]\nblocks:\n  - name: a\n    icon: {source: sun.png, align: middle}\n", "align"},
		{"size", "rows: [100]\nblocks:\n  - name: a\n    icon: {source: sun.png, size: 5}\n", "size"},
		{"two kinds", "rows: [100]\nblocks:\n  - name: a\n    icon: {source: sun.png}\n    text: hello\n", "exactly one"},
	}
	for _, tt := range tests {
		_, err := Parse(strings.NewReader(tt.page))
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("%s: error = %v", tt.name, err)
		}
	}
}

func TestIconDeterministic(t *testing.T) {
	tmpl := iconTemplate(t, iconPage, map[string][]byte{"sun.png": solidPNG(t, 32, 32, 0.7, true)})
	doc := iconDocument(t)
	first := renderIcon(t, tmpl, doc)
	second := renderIcon(t, tmpl, doc)
	if !bytes.Equal(first.Pix, second.Pix) {
		t.Fatal("two renders of the same icon differ")
	}
}
