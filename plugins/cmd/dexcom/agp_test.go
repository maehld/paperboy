package main

import (
	"image"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

func TestPercentile(t *testing.T) {
	sorted := []float64{10, 20, 30, 40, 50}
	cases := []struct{ p, want float64 }{
		{0, 10},
		{0.25, 20},
		{0.5, 30},
		{1, 50},
		{0.1, 14},
	}
	for _, c := range cases {
		if got := percentile(sorted, c.p); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("percentile(%g) = %g, want %g", c.p, got, c.want)
		}
	}
	if got := percentile(nil, 0.5); got != 0 {
		t.Errorf("percentile of nothing = %g", got)
	}
	if got := percentile([]float64{7}, 0.9); got != 7 {
		t.Errorf("percentile of one value = %g", got)
	}
}

// foldSeries builds one reading every five minutes for the given days, with a
// value that rises by ten per day.
func foldSeries(now time.Time, days int) []reading {
	total := days * 288
	out := make([]reading, 0, total)
	for i := 0; i < total; i++ {
		out = append(out, reading{
			at:    now.Add(-time.Duration(total-1-i) * 5 * time.Minute),
			value: float64(i/288) * 10,
		})
	}
	return out
}

func TestBuildAGP(t *testing.T) {
	now := time.Date(2026, 11, 27, 20, 0, 0, 0, time.UTC)
	l := defaults(unitMGDL)
	p := buildAGP(foldSeries(now, 14), now, 14, time.UTC, l)
	// A window of 14 days touches 15 calendar dates.
	if p.days < 14 || p.readings != 14*288 {
		t.Fatalf("profile has %d days and %d readings", p.days, p.readings)
	}
	for i, s := range p.slots {
		if !s.hasBand() || s.days < 14 {
			t.Fatalf("slot %d: %+v", i, s)
		}
		if s.p5 > s.q1 || s.q1 > s.median || s.median > s.q3 || s.q3 > s.p95 {
			t.Fatalf("slot %d: the percentiles are out of order: %+v", i, s)
		}
		// The days rise by ten, so the median sits in the middle of 0..140.
		if s.median < 60 || s.median > 70 {
			t.Fatalf("slot %d: median = %g", i, s.median)
		}
	}
	// Two days give a median but no percentiles.
	p = buildAGP(foldSeries(now, 2), now, 14, time.UTC, l)
	if p.days != 2 {
		t.Errorf("two days: %d", p.days)
	}
	for i, s := range p.slots {
		if !s.hasReadings() {
			t.Fatalf("two days drew no median in slot %d: %+v", i, s)
		}
		if s.hasBand() {
			t.Fatalf("two days drew a band in slot %d: %+v", i, s)
		}
	}
}

// coveredDays counts the days with a reading, not the span of the window.
func TestCoveredDays(t *testing.T) {
	now := time.Date(2026, 11, 27, 20, 0, 0, 0, time.UTC)
	var rs []reading
	for day := 0; day < 14; day++ {
		if day >= 3 && day <= 6 {
			continue // four days without readings
		}
		at := now.Add(-time.Duration(day)*24*time.Hour - 12*time.Hour)
		rs = append(rs, reading{at: at, value: 120})
	}
	if got := coveredDays(rs, now, 14); got != 10 {
		t.Errorf("covered = %d, want 10", got)
	}
	req := &plugin.Request{Language: "de"}
	if got := daysLabel(req, 10, 14); got != "10/14 Tage" {
		t.Errorf("label with a gap = %q", got)
	}
	if got := daysLabel(req, 14, 14); got != "14 Tage" {
		t.Errorf("label of a full window = %q", got)
	}
	if got := daysLabel(req, 1, 1); got != "1 Tag" {
		t.Errorf("label of one day = %q", got)
	}
}

func TestAGPWidget(t *testing.T) {
	now := time.Date(2026, 11, 27, 20, 0, 0, 0, time.UTC)
	d := &dexcom{unit: unitMGDL, set: map[string]float64{}}
	d.readings = foldSeries(now, 14)
	req := &plugin.Request{Language: "de", Timezone: "UTC", Now: now,
		Page: plugin.PageInfo{Width: 800, Height: 480, PPI: 125}}
	for _, size := range []image.Rectangle{
		image.Rect(0, 0, 800, 240),
		image.Rect(0, 0, 400, 240),
		image.Rect(0, 0, 800, 180),
	} {
		for _, levels := range []int{2, 4, 16} {
			img := render.NewCanvas(size.Dx(), size.Dy(), levels)
			w := plugin.WidgetRequest{Widget: "agp", Width: size.Dx(), Height: size.Dy(), Style: style()}
			if err := d.Render(req, w, img); err != nil {
				t.Fatalf("agp at %v with %d levels: %v", size, levels, err)
			}
			if inkCount(img) == 0 {
				t.Fatalf("agp at %v with %d levels drew nothing", size, levels)
			}
		}
	}

	// The same chart in mmol/L, with the thresholds of that unit.
	d.unit = unitMMOL
	for i := range d.readings {
		d.readings[i].value = 5 + 3*math.Sin(float64(i)/40)
	}
	img := render.NewCanvas(800, 240, 16)
	w := plugin.WidgetRequest{Widget: "agp", Width: 800, Height: 240, Style: style()}
	if err := d.Render(req, w, img); err != nil {
		t.Fatalf("mmol/L: %v", err)
	}
	d.unit = unitMGDL
	d.readings = foldSeries(now, 14)

	// One day draws the curve of that day without bands.
	for _, days := range []int{1, 2} {
		d.readings = foldSeries(now, days)
		img = render.NewCanvas(800, 240, 4)
		w = plugin.WidgetRequest{Widget: "agp", Width: 800, Height: 240, Style: style()}
		if err := d.Render(req, w, img); err != nil {
			t.Fatalf("%d days: %v", days, err)
		}
		if inkCount(img) == 0 {
			t.Fatalf("%d days drew nothing", days)
		}
	}
	d.readings = nil
	img = render.NewCanvas(800, 240, 4)
	if err := d.Render(req, w, img); err != nil {
		t.Fatalf("no data: %v", err)
	}

	for _, days := range []int{0, 31} {
		w = plugin.WidgetRequest{Widget: "agp", Width: 800, Height: 240, Style: style(),
			Args: map[string]any{"days": days}}
		if err := d.Render(req, w, render.NewCanvas(800, 240, 4)); err == nil {
			t.Errorf("days %d passed the check", days)
		}
	}
}

// ink counts the non-white pixels of a canvas.
func inkCount(img *image.Paletted) int {
	count := 0
	for _, px := range img.Pix {
		if int(px) != len(img.Palette)-1 {
			count++
		}
	}
	return count
}

func TestAGPValues(t *testing.T) {
	now := time.Date(2026, 11, 27, 20, 0, 0, 0, time.UTC)
	d := &dexcom{unit: unitMGDL, set: map[string]float64{}}
	total := 14 * 288
	for i := 0; i < total; i++ {
		v := 100.0
		if i%2 == 0 {
			v = 140
		}
		d.readings = append(d.readings, reading{
			at:    now.Add(-time.Duration(total-1-i) * 5 * time.Minute),
			value: v,
		})
	}
	req := &plugin.Request{Now: now, Language: "de", Timezone: "UTC",
		Values: []string{"tight", "cv", "gmi", "in-band"}}
	values, err := d.Values(req)
	if err != nil {
		t.Fatal(err)
	}
	if v := values["tight"]; v.Number == nil || *v.Number != 100 {
		t.Errorf("tight = %+v", v)
	}
	if v := values["in-band"]; v.Number == nil || *v.Number != 100 {
		t.Errorf("in-band = %+v", v)
	}
	// Mean 120, deviation 20: CV 16.7 %, GMI 6.2 %.
	if v := values["cv"]; v.Number == nil || math.Abs(*v.Number-16.67) > 0.1 {
		t.Errorf("cv = %+v", v)
	}
	if v := values["gmi"]; v.Number == nil || math.Abs(*v.Number-6.18) > 0.01 {
		t.Errorf("gmi = %+v", v)
	}

	// Without readings the numbers are dashes, not zeros.
	d.readings = nil
	values, err = d.Values(req)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tight", "cv", "gmi"} {
		if v := values[name]; v.Text == nil || *v.Text != "–" {
			t.Errorf("%s without readings = %+v", name, v)
		}
	}
}

func TestAGPTightRangeInAValue(t *testing.T) {
	d := &dexcom{unit: unitMGDL, set: map[string]float64{}}
	values, err := d.Values(&plugin.Request{Now: time.Now(), Language: "de",
		Values: []string{"band"}})
	if err != nil {
		t.Fatal(err)
	}
	if v := values["band"]; v.Text == nil || !strings.Contains(*v.Text, "70") ||
		!strings.Contains(*v.Text, "180") {
		t.Errorf("band = %+v", v)
	}
}
