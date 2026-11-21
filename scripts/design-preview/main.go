// Command design-preview renders the gallery of the shared widget design
// language: the type scale, tiles, a hero, meters, arrows and trend charts, in
// four gray levels and in one bit.
//
// Run it from the repository root. The tool belongs to no module, so Go starts
// it from its files:
//
//	go run ./scripts/design-preview/main.go
//	go run ./scripts/design-preview/main.go -out /tmp/preview
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/maehld/paperboy/sdk/design"
	"github.com/maehld/paperboy/sdk/render"
)

func main() {
	out := flag.String("out", "docs/images/design", "output directory for the PNGs")
	flag.Parse()
	if err := run(*out); err != nil {
		fmt.Fprintln(os.Stderr, "design-preview:", err)
		os.Exit(1)
	}
}

func run(out string) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	for _, levels := range []int{4, 2} {
		img := render.NewCanvas(1200, 1020, levels)
		drawGallery(img)
		name := "components.png"
		if levels == 2 {
			name = "components-1bit.png"
		}
		if err := writePNG(filepath.Join(out, name), img); err != nil {
			return err
		}
	}
	return nil
}

func writePNG(name string, img image.Image) error {
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func drawGallery(img *image.Paletted) {
	s := design.NewScale(20, 300)
	m := 32
	heading := func(x, y int, text string) {
		render.TextAt(img, text, x, y+int(s.Body*0.95), s.Body, true, render.Black, render.Left)
	}
	render.TextAt(img, "Paperboy design language", m, m+34, 30, true, render.Black, render.Left)
	render.TextAt(img, "One page-wide type scale and the shapes that many plugins repeat.",
		m, m+62, s.Detail, false, design.Muted(img), render.Left)
	render.TextAt(img, "Regenerate with ./scripts/design-preview.sh",
		1200-m, m+62, s.Detail, false, design.Muted(img), render.Right)

	// Type scale.
	heading(m, 120, "Type scale")
	rows := []struct {
		name string
		size float64
		text string
	}{
		{"Micro", s.Micro, "axis tick · since 14:20"},
		{"Detail", s.Detail, "detail and tile label"},
		{"Body", s.Body, "body and list row"},
		{"Title", s.Title, "title and tile value"},
		{"Hero", s.Hero, "12,4"},
	}
	y := 170
	for _, row := range rows {
		render.TextAt(img, row.name, m, y+int(row.size*0.8), s.Micro, false, design.Muted(img), render.Left)
		render.TextAt(img, row.text, m+110, y+int(row.size*0.8), row.size, row.name == "Hero" || row.name == "Title", render.Black, render.Left)
		y += int(row.size*1.25) + 8
	}

	drawTiles(img, image.Rect(620, 150, 1168, 390), s)
	drawHero(img, image.Rect(m, 440, 588, 720), s)
	drawMisc(img, image.Rect(620, 440, 1168, 720), s)
	drawPriceTrend(img, image.Rect(m, 770, 588, 990), s)
	drawWeatherTrend(img, image.Rect(620, 770, 1168, 990), s)
}

func drawTiles(img *image.Paletted, r image.Rectangle, s design.Scale) {
	render.TextAt(img, "Tiles", r.Min.X, r.Min.Y+int(s.Body*0.95), s.Body, true, render.Black, render.Left)
	tiles := []design.Tile{
		{Label: "PV", Value: "9,7", Unit: "kW", Detail: "solar"},
		{Label: "Haus", Value: "1,1", Unit: "kW"},
		{Label: "Netz", Value: "3,1", Unit: "kW", Detail: "Einspeisung"},
		{Label: "Akku", Value: "81", Unit: "%", Detail: "lädt 1,2 kW", Trend: 1},
	}
	design.Tiles(img, image.Rect(r.Min.X, r.Min.Y+40, r.Max.X, r.Max.Y), tiles, s,
		design.TileOptions{Columns: 2, Divider: true, Gap: 10, Rule: 1})
}

func drawHero(img *image.Paletted, r image.Rectangle, s design.Scale) {
	render.TextAt(img, "Hero with details", r.Min.X, r.Min.Y+int(s.Body*0.95), s.Body, true, render.Black, render.Left)
	design.DrawHero(img, image.Rect(r.Min.X, r.Min.Y+40, r.Max.X, r.Max.Y), design.Hero{
		Label:   "Diesel · €/l",
		Value:   "1,769",
		Caption: "Freie Tankstelle",
		Details: []string{"1,6 km · offen", "unverändert seit 23:54"},
		Note:    "Kein Regen heute",
		Meter:   &design.Meter{Fraction: 0.64},
		Rule:    1,
	}, s)
}

func drawMisc(img *image.Paletted, r image.Rectangle, s design.Scale) {
	render.TextAt(img, "Meter, arrows, notice", r.Min.X, r.Min.Y+int(s.Body*0.95), s.Body, true, render.Black, render.Left)
	design.DrawMeter(img, image.Rect(r.Min.X, r.Min.Y+50, r.Min.X+340, r.Min.Y+82), design.Meter{Fraction: 0.64}, 2)
	design.DrawMeter(img, image.Rect(r.Min.X, r.Min.Y+92, r.Min.X+340, r.Min.Y+124), design.Meter{Missing: true}, 2)
	for i, dir := range []int{-1, 0, 1} {
		design.Arrow(img, r.Min.X+380+i*36, r.Min.Y+66, 26, dir, 1)
		design.Arrow(img, r.Min.X+380+i*36, r.Min.Y+108, 26, dir, 2)
	}
	render.TextAt(img, "Secondary ink stays readable on one bit.", r.Min.X, r.Min.Y+158, s.Detail, false, design.Muted(img), render.Left)
	design.Notice(img, image.Rect(r.Min.X, r.Min.Y+180, r.Max.X, r.Max.Y), "No data yet", s)
}

func drawPriceTrend(img *image.Paletted, r image.Rectangle, s design.Scale) {
	start := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	points := []design.Point{
		{At: start, Value: 1.790},
		{At: start.Add(5 * time.Hour), Value: 1.775},
		{At: start.Add(8 * time.Hour), Value: 1.769},
		{At: start.Add(13 * time.Hour), Value: 1.776},
		{At: start.Add(18 * time.Hour), Value: 1.782},
		{At: start.Add(22 * time.Hour), Value: 1.769},
		{At: start.Add(24 * time.Hour), Value: 1.769},
	}
	design.DrawTrend(img, r, design.Trend{
		From: start, To: start.Add(24 * time.Hour),
		Series:  []design.Series{{Points: points, Style: design.Step, Width: 2}},
		Range:   &design.Range{Min: 1.76, Max: 1.80},
		Axis:    true,
		Grid:    true,
		Label:   "Diesel · €/l",
		Current: "1,769",
		Format:  func(v float64) string { return fmt.Sprintf("%.3f", v) },
	}, s)
}

func drawWeatherTrend(img *image.Paletted, r image.Rectangle, s design.Scale) {
	start := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	var temp []design.Point
	var rain []design.Point
	for h := 0; h <= 24; h++ {
		t := start.Add(time.Duration(h) * time.Hour)
		v := 6 + 9*math.Sin((float64(h)-6)/24*2*math.Pi)
		temp = append(temp, design.Point{At: t, Value: v})
		prob := math.Max(0, 55*math.Sin((float64(h)-2)/24*2*math.Pi))
		if h > 6 && h < 12 {
			prob = 60
		}
		fill := "shade"
		if h >= 6 && h < 9 {
			fill = "hatch"
		}
		rain = append(rain, design.Point{At: t, Value: prob, Fill: fill})
	}
	ticks := make([]time.Time, 0, 5)
	for h := 0; h <= 24; h += 6 {
		ticks = append(ticks, start.Add(time.Duration(h)*time.Hour))
	}
	design.DrawTrend(img, r, design.Trend{
		From: start, To: start.Add(24 * time.Hour),
		Series: []design.Series{
			{Points: rain, Style: design.Bars, Scale: &design.Range{Min: 0, Max: 100}, Fill: "shade"},
			{Points: temp, Style: design.Line, Width: 2, Halo: true},
		},
		Range: &design.Range{Min: 3, Max: 17},
		Axis:  true,
		Ticks: ticks,
		TickFormat: func(t time.Time) string {
			return fmt.Sprintf("%02d", t.Hour())
		},
		Now:   start.Add(15 * time.Hour),
		Label: "Temperatur (°C)",
		Legend: []design.LegendItem{
			{Label: "Regen %", Fill: "shade"},
			{Label: "Schnee %", Fill: "hatch"},
		},
		Format: func(v float64) string { return fmt.Sprintf("%.0f°", v) },
	}, s)
}
