package main

import "testing"

func TestBatteryStep(t *testing.T) {
	cases := []struct {
		soc  float64
		want int
	}{
		{-5, 0},
		{0, 0},
		{1, 1},
		{12, 1},
		{13, 1},
		{37, 1},
		{38, 2},
		{62, 2},
		{63, 3},
		{87, 3},
		{88, 4},
		{100, 4},
		{150, 4},
	}
	for _, c := range cases {
		if got := batteryStep(c.soc); got != c.want {
			t.Errorf("batteryStep(%v) = %d, want %d", c.soc, got, c.want)
		}
	}
}
