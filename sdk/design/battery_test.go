package design

import (
	"image"
	"math"
	"testing"

	"github.com/maehld/paperboy/sdk/render"
)

func batteryInk(img *image.Paletted, r image.Rectangle) int {
	r = r.Intersect(img.Rect)
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

func TestDrawBatteryTooSmall(t *testing.T) {
	img := render.NewCanvas(10, 10, 2)
	if DrawBattery(img, image.Rect(0, 0, 3, 2), Battery{Fraction: 1}, 0) {
		t.Fatal("a three-pixel rect has room for a gauge")
	}
	if DrawBattery(img, image.Rect(0, 0, 4, 3), Battery{Fraction: 1}, 0) {
		t.Fatal("a four-by-three rect has room for a gauge")
	}
}

func TestDrawBatterySegments(t *testing.T) {
	const w, h = 240, 80
	r := image.Rect(4, 4, w-4, h-4)
	f, ok := frameBattery(r, Battery{}, 0)
	if !ok {
		t.Fatal("no frame")
	}
	draw := func(fraction float64) int {
		img := render.NewCanvas(w, h, 2)
		if !DrawBattery(img, r, Battery{Fraction: fraction}, 0) {
			t.Fatal("the gauge did not draw")
		}
		return batteryInk(img, f.inner)
	}
	zero := draw(0)
	low, mid, full := draw(0.04)-zero, draw(0.84)-zero, draw(1)-zero
	if zero > 40 {
		t.Errorf("0 %% draws %d pixels, more than the rounded corners", zero)
	}
	if low == 0 {
		t.Error("a low charge shows no segment")
	}
	if mid <= low || full <= mid {
		t.Errorf("the fill does not step up: %d, %d, %d", low, mid, full)
	}
	if want := f.inner.Dx() * f.inner.Dy(); full < want*4/5 {
		t.Errorf("100 %% fills %d of %d pixels", full, want)
	}
	if want := full * 4 / 5; mid < want*9/10 || mid > want*11/10 {
		t.Errorf("84 %% fills %d pixels, four segments are about %d", mid, want)
	}
}

func TestDrawBatteryNoGapOnSmallGauge(t *testing.T) {
	small, ok := frameBattery(image.Rect(0, 0, 60, 20), Battery{}, 4)
	if !ok {
		t.Fatal("no small frame")
	}
	if small.gap != 0 {
		t.Errorf("a small gauge keeps a gap of %d", small.gap)
	}
	large, ok := frameBattery(image.Rect(0, 0, 200, 64), Battery{}, 0)
	if !ok {
		t.Fatal("no large frame")
	}
	if large.gap == 0 {
		t.Error("a tall gauge has no gap between fill and wall")
	}
}

func TestDrawBatterySmallGaugeReachesTheWall(t *testing.T) {
	const w, h = 60, 20
	r := image.Rect(0, 0, w, h)
	f, ok := frameBattery(r, Battery{Fraction: 0.25}, 4)
	if !ok {
		t.Fatal("no frame")
	}
	if f.gap != 0 {
		t.Fatalf("a small gauge keeps a gap of %d", f.gap)
	}
	img := render.NewCanvas(w, h, 2)
	DrawBattery(img, r, Battery{Fraction: 0.25}, 4)
	mid := f.inner.Min.Y + f.inner.Dy()/2
	if img.ColorIndexAt(f.inner.Min.X, mid) != 0 {
		t.Error("the fill does not reach the wall")
	}
	if img.ColorIndexAt(f.inner.Min.X, f.inner.Min.Y) != 0 {
		t.Error("a white pixel sits in the corner")
	}
}

func TestDrawBatteryFirstFillFollowsTheWall(t *testing.T) {
	const w, h = 240, 80
	r := image.Rect(4, 4, w-4, h-4)
	f, _ := frameBattery(r, Battery{Fraction: 0.25}, 0)
	img := render.NewCanvas(w, h, 2)
	fillBattery(img, f.inner, f.line, 0.25)
	clipCorners(img, f.inner, math.Max(f.rad-float64(f.line+f.gap), 1))
	mid := f.inner.Min.Y + f.inner.Dy()/2
	if img.ColorIndexAt(f.inner.Min.X, mid) != 0 {
		t.Error("the first fill does not sit on the wall")
	}
	if img.ColorIndexAt(f.inner.Min.X, f.inner.Min.Y) != 1 {
		t.Error("the first fill squares off the rounded corner")
	}
}

func TestDrawBatteryCap(t *testing.T) {
	const w, h = 240, 80
	r := image.Rect(4, 4, w-4, h-4)
	f, _ := frameBattery(r, Battery{Fraction: 1}, 0)
	img := render.NewCanvas(w, h, 2)
	DrawBattery(img, r, Battery{Fraction: 1}, 0)
	mid := r.Min.Y + r.Dy()/2
	if img.ColorIndexAt(f.body.Max.X, mid) != 0 {
		t.Error("the cap does not sit on the body")
	}
	nubH := max(4, r.Dy()*2/5)
	top := r.Min.Y + (r.Dy()-nubH)/2
	if img.ColorIndexAt(r.Max.X-1, top) != 1 {
		t.Error("the cap is not rounded on the right")
	}
}

func TestDrawBatteryMissingReading(t *testing.T) {
	const w, h = 240, 80
	r := image.Rect(4, 4, w-4, h-4)
	empty := render.NewCanvas(w, h, 2)
	DrawBattery(empty, r, Battery{Fraction: 0}, 0)
	missing := render.NewCanvas(w, h, 2)
	DrawBattery(missing, r, Battery{Missing: true}, 0)
	f, _ := frameBattery(r, Battery{Missing: true}, 0)
	if missingInk, emptyInk := batteryInk(missing, f.inner), batteryInk(empty, f.inner); missingInk <= emptyInk {
		t.Errorf("a missing reading adds no mark: %d, %d", missingInk, emptyInk)
	}
}

func TestDrawBatteryChargingBolt(t *testing.T) {
	const w, h = 240, 80
	r := image.Rect(4, 4, w-4, h-4)
	f, _ := frameBattery(r, Battery{Fraction: 1}, 0)
	plain := render.NewCanvas(w, h, 2)
	DrawBattery(plain, r, Battery{Fraction: 1}, 0)
	charging := render.NewCanvas(w, h, 2)
	DrawBattery(charging, r, Battery{Fraction: 1, Charging: true}, 0)
	if batteryInk(charging, f.inner) >= batteryInk(plain, f.inner) {
		t.Error("the charging mark does not knock a bolt out of the fill")
	}
}

func TestDrawBatteryLevelThickensTheOutline(t *testing.T) {
	r := image.Rect(0, 0, 200, 64)
	ok, warn, critical := 0, 0, 0
	for _, c := range []struct {
		level string
		line  *int
	}{
		{"", &ok},
		{"warn", &warn},
		{"critical", &critical},
	} {
		f, good := frameBattery(r, Battery{Level: c.level}, 0)
		if !good {
			t.Fatal("no frame")
		}
		*c.line = f.line
	}
	if ok >= warn || warn >= critical {
		t.Errorf("outline thickness: ok %d, warn %d, critical %d", ok, warn, critical)
	}
}
