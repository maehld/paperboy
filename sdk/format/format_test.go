package format

import "testing"

func TestNumber(t *testing.T) {
	tests := []struct {
		v        float64
		decimals int
		lang     string
		want     string
	}{
		{12.4, 1, "de", "12,4"},
		{12.4, 1, "en", "12.4"},
		{1.769, 3, "de", "1,769"},
		{-3.25, 2, "de", "-3,25"},
		{0, 0, "de", "0"},
		{-0.0, 0, "de", "0"},
		{1000, 0, "en", "1000"},
		{7, 0, "de", "7"},
	}
	for _, tt := range tests {
		if got := Number(tt.v, tt.decimals, tt.lang); got != tt.want {
			t.Errorf("Number(%v, %d, %q) = %q, want %q", tt.v, tt.decimals, tt.lang, got, tt.want)
		}
	}
}
