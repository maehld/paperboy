package main

import (
	"testing"

	"github.com/maehld/paperboy/sdk/render"
)

func TestFlowBatteryDraws(t *testing.T) {
	for _, soc := range []float64{0, 25, 85, 100} {
		img := render.NewCanvas(64, 64, 2)
		drawFlowBattery(img, img.Rect, soc)
		if !hasInk(img) {
			t.Errorf("%.0f %%: nothing drawn", soc)
		}
	}
}
