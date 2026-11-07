package plugin

import (
	"math"
	"testing"
)

func TestPhysicalSizes(t *testing.T) {
	r := &Request{Page: PageInfo{Width: 800, Height: 480, PPI: 144}}
	if got := r.PtPx(36); got != 72 {
		t.Errorf("PtPx(36) at 144 PPI = %v", got)
	}
	if got := r.MMPx(25.4); math.Abs(got-144) > 1e-9 {
		t.Errorf("MMPx(25.4) at 144 PPI = %v", got)
	}
	if got := r.TextPx(WidgetRequest{Style: Style{TextPx: 31}}); got != 31 {
		t.Errorf("TextPx = %v, want the style's text_px", got)
	}
	if got := r.TextPx(WidgetRequest{}); got != 16 {
		t.Errorf("TextPx without style = %v", got)
	}
	if got := (&Request{}).PtPx(10); got != 0 {
		t.Errorf("PtPx without PPI = %v", got)
	}
}
