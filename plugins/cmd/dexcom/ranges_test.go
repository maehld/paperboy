package main

import (
	"image"
	"math"
	"testing"
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

// rangeSeries builds readings with a known number of values per range.
func rangeSeries(now time.Time, counts map[float64]int) []reading {
	var out []reading
	for value, count := range counts {
		for i := 0; i < count; i++ {
			out = append(out, reading{
				at:    now.Add(-time.Duration(len(out)) * 5 * time.Minute),
				value: value,
			})
		}
	}
	return out
}

func TestRangeShares(t *testing.T) {
	now := time.Date(2026, 11, 27, 20, 0, 0, 0, time.UTC)
	l := defaults(unitMGDL)
	rs := rangeSeries(now, map[float64]int{50: 1, 65: 4, 120: 80, 200: 10, 280: 5})
	shares, covered := rangeShares(rs, now, 14, l, "de")
	if covered != 1 {
		t.Errorf("covered = %d", covered)
	}
	want := []struct {
		label string
		share float64
		kind  rangeKind
	}{
		{"<55", 1, rangeVeryLow},
		{"55–69", 4, rangeLow},
		{"70–180", 80, rangeTarget},
		{"181–250", 10, rangeHigh},
		{">250", 5, rangeVeryHigh},
	}
	if len(shares) != len(want) {
		t.Fatalf("%d ranges, want %d: %+v", len(shares), len(want), shares)
	}
	for i, w := range want {
		if shares[i].label != w.label || math.Abs(shares[i].share-w.share) > 0.01 || shares[i].kind != w.kind {
			t.Errorf("range %d = %+v, want %+v", i, shares[i], w)
		}
	}

	// Around the edges: a reading at a threshold counts into the higher range,
	// so 55 is low, 70 and 180 are in the target range and 250 is high.
	l = apply(defaults(unitMGDL), map[string]float64{})
	rs = rangeSeries(now, map[float64]int{54: 1, 55: 1, 70: 1, 180: 1, 250: 1, 251: 1})
	shares, _ = rangeShares(rs, now, 14, l, "de")
	for i, s := range shares {
		want := 100.0 / 6
		if i == 2 {
			want = 200.0 / 6
		}
		if math.Abs(s.share-want) > 0.01 {
			t.Errorf("edge reading in range %d (%s) = %g %%, want %g", i, s.label, s.share, want)
		}
	}
}

// A disabled urgent low or very high merges its range into the next one.
func TestRangeSharesMerged(t *testing.T) {
	now := time.Date(2026, 11, 27, 20, 0, 0, 0, time.UTC)
	rs := rangeSeries(now, map[float64]int{50: 1, 65: 4, 120: 80, 200: 10, 280: 5})

	l := apply(defaults(unitMGDL), map[string]float64{"very_high": 0})
	shares, _ := rangeShares(rs, now, 14, l, "de")
	if len(shares) != 4 || shares[3].label != ">180" || math.Abs(shares[3].share-15) > 0.01 {
		t.Errorf("without very_high: %+v", shares)
	}

	l = apply(defaults(unitMGDL), map[string]float64{"urgent": 0})
	shares, _ = rangeShares(rs, now, 14, l, "de")
	if len(shares) != 4 || shares[0].label != "<70" || math.Abs(shares[0].share-5) > 0.01 {
		t.Errorf("without urgent: %+v", shares)
	}
}

func TestRangesWidget(t *testing.T) {
	now := time.Date(2026, 11, 27, 20, 0, 0, 0, time.UTC)
	d := &dexcom{unit: unitMGDL, set: map[string]float64{}}
	d.readings = foldSeries(now, 14)
	for i := range d.readings {
		d.readings[i].value = 80 + math.Mod(float64(i), 180)
	}
	req := &plugin.Request{Language: "de", Timezone: "UTC", Now: now,
		Page: plugin.PageInfo{Width: 800, Height: 480, PPI: 125}}
	for _, size := range []image.Rectangle{
		image.Rect(0, 0, 800, 120),
		image.Rect(0, 0, 800, 60),
		image.Rect(0, 0, 266, 300),
		image.Rect(0, 0, 400, 240),
	} {
		for _, levels := range []int{2, 4, 16} {
			img := render.NewCanvas(size.Dx(), size.Dy(), levels)
			w := plugin.WidgetRequest{Widget: "ranges", Width: size.Dx(), Height: size.Dy(), Style: style()}
			if err := d.Render(req, w, img); err != nil {
				t.Fatalf("ranges at %v with %d levels: %v", size, levels, err)
			}
			if inkCount(img) == 0 {
				t.Fatalf("ranges at %v with %d levels drew nothing", size, levels)
			}
		}
	}

	d.readings = nil
	if err := d.Render(req, plugin.WidgetRequest{Widget: "ranges", Width: 800, Height: 120, Style: style()},
		render.NewCanvas(800, 120, 4)); err != nil {
		t.Fatalf("no data: %v", err)
	}
	for _, days := range []int{0, 31} {
		w := plugin.WidgetRequest{Widget: "ranges", Width: 800, Height: 120, Style: style(),
			Args: map[string]any{"days": days}}
		if err := d.Render(req, w, render.NewCanvas(800, 120, 4)); err == nil {
			t.Errorf("days %d passed the check", days)
		}
	}
}

// The mmol/L account labels its ranges with one decimal.
func TestRangeLabelsMMOL(t *testing.T) {
	now := time.Date(2026, 11, 27, 20, 0, 0, 0, time.UTC)
	l := defaults(unitMMOL)
	rs := rangeSeries(now, map[float64]int{3.0: 1, 4.0: 1, 6.0: 1})
	shares, _ := rangeShares(rs, now, 1, l, "de")
	want := []string{"<3,1", "3,1–3,8", "3,9–10,0", "10,1–13,9", ">13,9"}
	if len(shares) != len(want) {
		t.Fatalf("%d ranges: %+v", len(shares), shares)
	}
	for i, w := range want {
		if shares[i].label != w {
			t.Errorf("range %d = %q, want %q", i, shares[i].label, w)
		}
	}
}
