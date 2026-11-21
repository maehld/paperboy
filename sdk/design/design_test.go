package design

import (
	"image"
	"testing"
	"time"

	"github.com/maehld/paperboy/sdk/render"
)

// ink counts the pixels that are not white.
func ink(img *image.Paletted) int {
	n := 0
	for _, p := range img.Pix {
		if int(p) != len(img.Palette)-1 {
			n++
		}
	}
	return n
}

func TestNewScale(t *testing.T) {
	s := NewScale(20, 300)
	if s.Micro != 14 || s.Detail != 17 || s.Body != 20 || s.Title != 25 || s.Hero != 52 {
		t.Fatalf("unexpected scale: %+v", s)
	}
	short := NewScale(20, 80)
	if short.Hero != 33.6 {
		t.Fatalf("hero not capped by the cell: %v", short.Hero)
	}
	if short.Micro != s.Micro || short.Body != s.Body {
		t.Fatalf("the cell height changed a role other than hero: %+v", short)
	}
	tiny := NewScale(1, 1)
	if tiny.Micro < render.MinSize || tiny.Detail < render.MinSize ||
		tiny.Body < render.MinSize || tiny.Title < render.MinSize || tiny.Hero < render.MinSize {
		t.Fatalf("a role fell below the minimum: %+v", tiny)
	}
	if half := s.Scaled(0.5); half.Hero < half.Title || half.Hero > s.Hero || half.Body != 10 {
		t.Fatalf("unexpected scaled scale: %+v", half)
	}
}

func TestTileDropsDetailFirst(t *testing.T) {
	tile := Tile{Label: "PV", Value: "9,7", Unit: "kW", Detail: "detail"}
	s := NewScale(20, 200)
	if p, ok := planTile(tile, image.Rect(0, 0, 200, 200), s); !ok || !p.showDetail || !p.showValue {
		t.Fatalf("tall tile: %+v ok=%v", p, ok)
	}
	p, ok := planTile(tile, image.Rect(0, 0, 200, 42), s)
	if !ok || p.showDetail || !p.showValue {
		t.Fatalf("short tile: %+v ok=%v", p, ok)
	}
	p, ok = planTile(tile, image.Rect(0, 0, 200, 24), s)
	if !ok || p.showDetail || p.showLabel || !p.showValue {
		t.Fatalf("very short tile: %+v ok=%v", p, ok)
	}
	if _, ok := planTile(tile, image.Rect(0, 0, 200, 2), s); ok {
		t.Fatal("a two-pixel tile has room for a value")
	}
}

func TestTilesDraw(t *testing.T) {
	img := render.NewCanvas(400, 200, 4)
	tiles := []Tile{
		{Label: "PV", Value: "9,7", Unit: "kW", Detail: "solar"},
		{Label: "Haus", Value: "1,1", Unit: "kW"},
		{Label: "Netz", Value: "3,1", Unit: "kW", Trend: 1},
		{Label: "Akku", Value: "81", Unit: "%", Detail: "lädt"},
	}
	if !Tiles(img, img.Rect, tiles, NewScale(20, 100), TileOptions{Columns: 2, Divider: true, Gap: 8, Rule: 1}) {
		t.Fatal("tiles did not draw")
	}
	if ink(img) == 0 {
		t.Fatal("tiles left the canvas empty")
	}
	if Tiles(img, image.Rect(0, 0, 400, 200), nil, NewScale(20, 200), TileOptions{}) {
		t.Fatal("an empty list drew")
	}
}

func TestHeroDropsDetails(t *testing.T) {
	h := Hero{
		Label:   "Diesel · €/l",
		Value:   "1,769",
		Caption: "Freie Tankstelle",
		Details: []string{"1,6 km · offen", "unverändert seit 23:54", "third"},
		Note:    "Kein Regen heute",
		Meter:   &Meter{Fraction: 0.6},
		Rule:    1,
	}
	s := NewScale(20, 300)
	if p, ok := planHero(h, image.Rect(0, 0, 400, 300), s, 3, true, true); !ok || p.details != 3 {
		t.Fatalf("tall hero: %+v ok=%v", p, ok)
	}
	p, ok := planHero(h, image.Rect(0, 0, 400, 80), s, 3, true, true)
	if ok && p.details == 3 {
		t.Fatalf("short hero kept every detail: %+v", p)
	}
	img := render.NewCanvas(400, 80, 4)
	if !DrawHero(img, img.Rect, h, s) {
		t.Fatal("hero did not draw")
	}
	if ink(img) == 0 {
		t.Fatal("hero left the canvas empty")
	}
}

func points(n int, base float64) []Point {
	out := make([]Point, n)
	start := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	for i := range out {
		out[i] = Point{At: start.Add(time.Duration(i) * time.Hour), Value: base + float64(i%5)}
	}
	return out
}

func TestTrendDraws(t *testing.T) {
	from := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	pts := points(24, 5)
	for _, style := range []SeriesStyle{Line, Area, Step, Bars} {
		for _, levels := range []int{2, 4} {
			img := render.NewCanvas(400, 200, levels)
			ok := DrawTrend(img, img.Rect, Trend{
				From: from, To: to,
				Series: []Series{{Points: pts, Style: style, Halo: style == Line}},
				Axis:   true, Grid: style == Step,
				Label: "Temperatur", Current: "12,4 °C",
				Format: func(v float64) string { return defaultFormat(v) },
			}, NewScale(20, 200))
			if !ok {
				t.Fatalf("style %d levels %d did not draw", style, levels)
			}
			if ink(img) == 0 {
				t.Fatalf("style %d levels %d left the canvas empty", style, levels)
			}
		}
	}
	img := render.NewCanvas(400, 200, 4)
	if DrawTrend(img, img.Rect, Trend{}, NewScale(20, 200)) {
		t.Fatal("an empty trend drew")
	}
	if !DrawTrend(img, img.Rect, Trend{
		From: from, To: to,
		Series: []Series{{Points: []Point{{At: from.Add(time.Hour), Value: 3}}, Style: Line}},
	}, NewScale(20, 200)) {
		t.Fatal("a single point did not draw")
	}
}

func TestSparkline(t *testing.T) {
	img := render.NewCanvas(100, 30, 2)
	if !Sparkline(img, img.Rect, []float64{1, 2, 4, 3, 5}, Line, "") {
		t.Fatal("sparkline did not draw")
	}
	if ink(img) == 0 {
		t.Fatal("sparkline left the canvas empty")
	}
	if Sparkline(img, img.Rect, nil, Line, "") {
		t.Fatal("an empty sparkline drew")
	}
}

func TestMeter(t *testing.T) {
	img := render.NewCanvas(100, 20, 4)
	if !DrawMeter(img, img.Rect, Meter{Fraction: 0.5}, 1) {
		t.Fatal("meter did not draw")
	}
	if ink(img) == 0 {
		t.Fatal("meter left the canvas empty")
	}
	if !DrawMeter(img, img.Rect, Meter{Missing: true}, 1) {
		t.Fatal("missing meter did not draw")
	}
	if DrawMeter(img, image.Rect(0, 0, 2, 2), Meter{Fraction: 1}, 1) {
		t.Fatal("a tiny meter drew")
	}
}

func TestMuted(t *testing.T) {
	if got := Muted(render.NewCanvas(1, 1, 2)); got != render.Black {
		t.Fatalf("one-bit muted is %v", got)
	}
	if got := Muted(render.NewCanvas(1, 1, 4)); got != render.DarkGray {
		t.Fatalf("four-level muted is %v", got)
	}
}

func TestNotice(t *testing.T) {
	img := render.NewCanvas(200, 60, 2)
	if !Notice(img, img.Rect, "keine Daten", NewScale(20, 60)) {
		t.Fatal("notice did not draw")
	}
	if ink(img) == 0 {
		t.Fatal("notice left the canvas empty")
	}
	if Notice(img, img.Rect, "", NewScale(20, 60)) {
		t.Fatal("an empty notice drew")
	}
}

func TestArrow(t *testing.T) {
	for _, dir := range []int{-1, 0, 1} {
		for _, strength := range []int{1, 2} {
			img := render.NewCanvas(40, 40, 2)
			Arrow(img, 20, 20, 20, dir, strength)
			if ink(img) == 0 {
				t.Fatalf("arrow dir %d strength %d left the canvas empty", dir, strength)
			}
		}
	}
}
