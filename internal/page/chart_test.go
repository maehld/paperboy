package page

import (
	"bytes"
	"image"
	"image/color"
	"testing"
	"time"
)

// chartRenderer builds a renderer whose data document holds one point list.
func chartRenderer(now time.Time, language string, points []any) *renderer {
	return &renderer{
		t: &Template{},
		root: map[string]any{
			"solar": map[string]any{
				"history": map[string]any{"points": points},
			},
		},
		f:        formatter{comma: language != "en", loc: time.UTC},
		now:      now.In(time.UTC),
		language: language,
	}
}

// timedPoints creates n points with RFC 3339 times starting at start.
func timedPoints(key string, start time.Time, step time.Duration, n int, value func(i int) float64) []any {
	points := make([]any, 0, n)
	for i := 0; i < n; i++ {
		points = append(points, map[string]any{
			"time":   start.Add(time.Duration(i) * step).Format(time.RFC3339),
			"values": map[string]any{key: value(i)},
		})
	}
	return points
}

func chartImage(r *renderer, c *Chart) *image.Paletted {
	img := newCanvas(200, 100, false) // 1-bit, like the mono display
	r.chart(img, img.Rect, c, nil)
	return img
}

func chartTestNow() time.Time {
	return time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
}

func chartTestStart() time.Time {
	return time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
}

func constant(v float64) func(int) float64 { return func(int) float64 { return v } }

func TestChartTimeAxisUsesPointTimes(t *testing.T) {
	now, start := chartTestNow(), chartTestStart()
	full := timedPoints("pv", start, 5*time.Minute, 144, constant(50)) // 00:00-11:55
	morning := timedPoints("pv", start, 5*time.Minute, 72, constant(50))
	chart := &Chart{Points: "solar/history/points", Time: true, Series: []Series{{Key: "pv"}}}

	fullImg := chartImage(chartRenderer(now, "en", full), chart)
	morningImg := chartImage(chartRenderer(now, "en", morning), chart)

	// Row 50 holds the constant series; x=80 is around 09:30.
	if fullImg.ColorIndexAt(80, 50) != 0 {
		t.Fatal("12 hours of data must cover 09:30")
	}
	if morningImg.ColorIndexAt(80, 50) != 1 {
		t.Fatal("6 hours of data must stop before 09:30")
	}
	if fullImg.ColorIndexAt(20, 50) != 0 {
		t.Fatal("both charts must cover the early morning")
	}
	if bytes.Equal(fullImg.Pix, morningImg.Pix) {
		t.Fatal("different point times must produce different images")
	}
}

func TestChartTimeAxisShiftChangesImage(t *testing.T) {
	now, start := chartTestNow(), chartTestStart()
	morning := timedPoints("pv", start, 5*time.Minute, 72, constant(50))
	shifted := timedPoints("pv", start.Add(6*time.Hour), 5*time.Minute, 72, constant(50))
	chart := &Chart{Points: "solar/history/points", Time: true, Series: []Series{{Key: "pv"}}}

	morningImg := chartImage(chartRenderer(now, "en", morning), chart)
	shiftedImg := chartImage(chartRenderer(now, "en", shifted), chart)

	if morningImg.ColorIndexAt(20, 50) != 0 {
		t.Fatal("the 00:00-06:00 series must cover x=20")
	}
	if shiftedImg.ColorIndexAt(20, 50) != 1 {
		t.Fatal("the 06:00-12:00 series must not cover x=20")
	}
	if bytes.Equal(morningImg.Pix, shiftedImg.Pix) {
		t.Fatal("shifting the timestamps must change the image")
	}
}

func TestChartMissingTimeLeavesGap(t *testing.T) {
	now, start := chartTestNow(), chartTestStart()
	all := timedPoints("pv", start, 5*time.Minute, 288, constant(50))
	gapStart, gapEnd := start.Add(10*time.Hour), start.Add(14*time.Hour)
	var points []any
	for _, p := range all {
		m := p.(map[string]any)
		ts, err := time.Parse(time.RFC3339, m["time"].(string))
		if err != nil {
			t.Fatal(err)
		}
		if !ts.Before(gapStart) && ts.Before(gapEnd) {
			continue
		}
		points = append(points, p)
	}
	// A point with an unusable timestamp must not fill the outage.
	points = append(points, map[string]any{
		"time":   "noon-ish",
		"values": map[string]any{"pv": 50.0},
	})
	chart := &Chart{Points: "solar/history/points", Time: true, Series: []Series{{Key: "pv"}}}
	img := chartImage(chartRenderer(now, "en", points), chart)

	if img.ColorIndexAt(100, 50) != 1 { // 12:00, inside the outage
		t.Fatal("a missing timestamp must leave a gap")
	}
	if img.ColorIndexAt(20, 50) != 0 {
		t.Fatal("data outside the gap must still be drawn")
	}
}

func TestChartIndexModeLeavesGaps(t *testing.T) {
	now := chartTestNow()
	points := []any{
		map[string]any{"values": map[string]any{"pv": 50.0}},
		map[string]any{"values": map[string]any{"load": 50.0}}, // no value for pv
		map[string]any{"values": map[string]any{"pv": 50.0}},
	}
	chart := &Chart{Points: "solar/history/points", Series: []Series{{Key: "pv"}}}
	img := chartImage(chartRenderer(now, "en", points), chart)

	if img.ColorIndexAt(4, 50) != 0 {
		t.Fatal("the first sample must be drawn")
	}
	if img.ColorIndexAt(99, 50) != 1 {
		t.Fatal("a missing value in the middle must leave a gap")
	}
	if img.ColorIndexAt(195, 50) != 0 {
		t.Fatal("the last sample must be drawn")
	}
}

func TestChartYMax(t *testing.T) {
	cases := []struct {
		name string
		c    Chart
		maxV float64
		want float64
	}{
		{"explicit", Chart{YMax: 8000}, 4210, 8000},
		{"power step", Chart{Unit: "W"}, 4210, 5000},
		{"power minimum", Chart{Unit: "W"}, 200, 1000},
		{"percent", Chart{Unit: "%"}, 81.5, 90},
		{"temp", Chart{Unit: "°C"}, 18.4, 20},
		{"energy", Chart{Unit: "kWh"}, 3.2, 4},
		{"small energy", Chart{Unit: "kWh"}, 0.42, 0.5},
		{"default decade", Chart{}, 3, 10},
		{"zero data", Chart{}, 0, 1},
	}
	for _, tc := range cases {
		if got := chartYMax(&tc.c, tc.maxV); got != tc.want {
			t.Errorf("%s: y max %g, want %g", tc.name, got, tc.want)
		}
	}
}

func TestChartAreaFillOnMono(t *testing.T) {
	now, start := chartTestNow(), chartTestStart()
	points := timedPoints("pv", start, 5*time.Minute, 288, constant(50))
	chart := &Chart{
		Points: "solar/history/points",
		Time:   true,
		Series: []Series{{Key: "pv", Style: "area", Fill: "shade"}},
	}
	img := chartImage(chartRenderer(now, "en", points), chart)
	if img.Palette[0] != (color.Gray{0}) {
		t.Fatal("expected a black and white palette")
	}
	// shade is a deterministic checkerboard on 1-bit panels.
	if img.ColorIndexAt(100, 70) != 0 {
		t.Fatal("missing checkerboard pixel inside the area fill")
	}
	if img.ColorIndexAt(101, 70) != 1 {
		t.Fatal("the checkerboard must leave every second pixel white")
	}
}

func TestChartAreasBeforeLines(t *testing.T) {
	now, start := chartTestNow(), chartTestStart()
	points := timedPoints("pv", start, 5*time.Minute, 288, constant(80))
	chart := &Chart{
		Points: "solar/history/points",
		Time:   true,
		Series: []Series{
			{Key: "pv", Style: "area", Fill: "shade"},
			{Key: "load", Style: "line"},
		},
	}
	// The line sits inside the area: the area leaves (101,78) white, the line
	// must paint it black because lines are drawn last.
	for i := range points {
		points[i].(map[string]any)["values"].(map[string]any)["load"] = 20.0
	}
	img := chartImage(chartRenderer(now, "en", points), chart)

	if img.ColorIndexAt(101, 78) != 0 {
		t.Fatal("the line must be drawn over the area fill")
	}
	if img.ColorIndexAt(100, 70) != 0 {
		t.Fatal("the area must still be filled")
	}
}

func TestChartAxisAndLabels(t *testing.T) {
	now, start := chartTestNow(), chartTestStart()
	points := timedPoints("pv", start, 5*time.Minute, 288, constant(1000))
	for i := range points {
		points[i].(map[string]any)["values"].(map[string]any)["load"] = 500.0
	}
	chart := &Chart{
		Points: "solar/history/points",
		Title:  "Solar",
		Unit:   "W",
		Time:   true,
		Axis:   true,
		Legend: true,
		Series: []Series{
			{Key: "pv", Label: "Solar", Style: "area"},
			{Key: "load", Label: "House"},
		},
	}
	img := chartImage(chartRenderer(now, "en", points), chart)

	// The bottom axis is two pixels high and starts right of the y labels.
	if n := blackIn(img, image.Rect(60, 84, 150, 86)); n < 100 {
		t.Fatalf("bottom axis has %d black pixels", n)
	}
	if blackIn(img, image.Rect(0, 0, 40, 90)) < 10 {
		t.Fatal("y labels missing")
	}
	if blackIn(img, image.Rect(0, 86, 200, 100)) < 10 {
		t.Fatal("time labels missing")
	}
	if blackIn(img, image.Rect(0, 0, 200, 13)) < 10 {
		t.Fatal("title or legend missing")
	}
}

func TestChartNoDataNotice(t *testing.T) {
	now := chartTestNow()
	single := timedPoints("pv", chartTestStart(), time.Hour, 1, constant(50))
	chart := &Chart{Points: "solar/history/points", Time: true, Series: []Series{{Key: "pv"}}}

	en := chartImage(chartRenderer(now, "en", single), chart)
	de := chartImage(chartRenderer(now, "de", single), chart)
	if blackIn(en, en.Rect) < 20 || blackIn(de, de.Rect) < 20 {
		t.Fatal("a chart with fewer than two points must show a notice")
	}
	if bytes.Equal(en.Pix, de.Pix) {
		t.Fatal("the notice must follow the display language")
	}
}

func TestChartDeterministic(t *testing.T) {
	now, start := chartTestNow(), chartTestStart()
	points := timedPoints("pv", start, 5*time.Minute, 288, func(i int) float64 { return float64(i % 2000) })
	for i := range points {
		points[i].(map[string]any)["values"].(map[string]any)["load"] = 500.0
	}
	chart := &Chart{
		Points: "solar/history/points",
		Title:  "Today",
		Unit:   "W",
		Time:   true,
		Axis:   true,
		Now:    true,
		Legend: true,
		Series: []Series{
			{Key: "pv", Label: "Solar", Style: "area"},
			{Key: "load", Label: "House", Style: "line", Weight: 3},
		},
	}
	first := chartImage(chartRenderer(now, "en", points), chart)
	second := chartImage(chartRenderer(now, "en", points), chart)
	if !bytes.Equal(first.Pix, second.Pix) {
		t.Fatal("two renders of the same chart differ")
	}
}
