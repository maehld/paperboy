package displayd

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strconv"
	"strings"
)

// Battery warning stages, stored in deviceState.BatteryLevel.
const (
	batteryOK       = ""
	batteryWarn     = "warn"
	batteryCritical = "critical"
)

func batteryRank(level string) int {
	switch level {
	case batteryWarn:
		return 1
	case batteryCritical:
		return 2
	}
	return 0
}

// parseVoltage reads the Battery-Voltage header. Missing, invalid or
// non-positive values (the firmware sends -1 without a measurement) are not ok.
func parseVoltage(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || v <= 0 || v > 10 {
		return 0, false
	}
	return v, true
}

// rankAt returns the stage for voltage v when every threshold is raised by offset.
func (b *Battery) rankAt(v, offset float64) int {
	switch {
	case b.CriticalVolts > 0 && v < b.CriticalVolts+offset:
		return 2
	case b.WarnVolts > 0 && v < b.WarnVolts+offset:
		return 1
	}
	return 0
}

// nextBatteryLevel returns the new stage. A stage starts below its threshold
// and only ends once the voltage is the hysteresis above it, so readings
// around a threshold do not make the warning flicker.
func nextBatteryLevel(b *Battery, prev, header string) string {
	if b == nil {
		return batteryOK
	}
	v, ok := parseVoltage(header)
	if !ok {
		return prev
	}
	rank := b.rankAt(v, 0)
	if p := batteryRank(prev); rank < p {
		rank = min(p, b.rankAt(v, b.HysteresisVolts))
	}
	return [...]string{batteryOK, batteryWarn, batteryCritical}[rank]
}

// voltageKey rounds the voltage to 0.1 V for image keys so that small
// fluctuations do not force a redraw on every wake-up.
func voltageKey(header string) string {
	v, ok := parseVoltage(header)
	if !ok {
		return "0"
	}
	return strconv.Itoa(int(v*10 + 0.5))
}

func voltageText(key string) string {
	n, err := strconv.Atoi(key)
	if err != nil || n <= 0 {
		return ""
	}
	return fmt.Sprintf(" (ca. %d,%d V)", n/10, n%10)
}

func lowBatteryText(key string) string { return "Akku schwach – bitte laden" + voltageText(key) }

// criticalBatteryImage is a full-screen notice shown instead of the pages.
func criticalBatteryImage(p Profile, key string) *image.Paletted {
	img := image.NewPaletted(image.Rect(0, 0, p.Width, p.Height), monoPalette)
	draw.Draw(img, img.Rect, image.NewUniform(color.White), image.Point{}, draw.Src)
	w, h := p.Width, p.Height
	// Battery outline with a small remaining charge.
	bw, bh := w*2/5, h/4
	x0, y0 := (w-bw)/2, h/5
	line := max(4, bh/12)
	fill := func(r image.Rectangle) { draw.Draw(img, r, image.NewUniform(color.Black), image.Point{}, draw.Src) }
	fill(image.Rect(x0, y0, x0+bw, y0+line))
	fill(image.Rect(x0, y0+bh-line, x0+bw, y0+bh))
	fill(image.Rect(x0, y0, x0+line, y0+bh))
	fill(image.Rect(x0+bw-line, y0, x0+bw, y0+bh))
	fill(image.Rect(x0+bw, y0+bh/3, x0+bw+line*2, y0+bh*2/3))
	fill(image.Rect(x0+line*2, y0+line*2, x0+line*2+bw/10, y0+bh-line*2))
	size := float64(h) / 10
	drawText(img, "Akku fast leer", w/2, y0+bh+int(size*1.6), size, color.Black, true)
	drawText(img, "Bitte jetzt laden"+voltageText(key), w/2, y0+bh+int(size*2.9), size*0.6, color.Black, true)
	return img
}
