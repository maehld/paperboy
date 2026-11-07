package page

import (
	"math"
	"strings"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestStyleScalesWithPPI(t *testing.T) {
	e1001 := Display{Width: 800, Height: 480, PPI: 125}
	sticky := Display{Width: 800, Height: 480, PPI: 236}
	a, b := DefaultStyle(e1001), DefaultStyle(sticky)
	// 16 pt, 4.5 pt and 0.6 pt at 125 PPI: about the look before ADR 0050.
	if !near(a.TextPx, 16*125.0/72) || a.Padding != 8 || a.Rule != 1 {
		t.Fatalf("e1001: %+v", a)
	}
	if !near(b.TextPx, 16*236.0/72) || b.Padding != 15 || b.Rule != 2 {
		t.Fatalf("sticky: %+v", b)
	}
	// Physically equal: the denser panel gets proportionally more pixels.
	if !near(b.TextPx/a.TextPx, 236.0/125) {
		t.Fatalf("text does not scale with the PPI: %v %v", a.TextPx, b.TextPx)
	}
}

func TestStyleCapsTextOnTinyDisplays(t *testing.T) {
	// 16 pt at 300 PPI would be 66.7 px, more than 0.12 of 200 px.
	s := DefaultStyle(Display{Width: 300, Height: 200, PPI: 300})
	if !near(s.TextPx, 24) {
		t.Fatalf("text_px %v, want the cap 24", s.TextPx)
	}
}

func TestStyleRuleAtLeastOnePixel(t *testing.T) {
	if s := DefaultStyle(Display{Width: 100, Height: 100, PPI: 40}); s.Rule != 1 {
		t.Fatalf("rule %d at 40 PPI", s.Rule)
	}
}

func TestStyleInPointsFromPage(t *testing.T) {
	tmpl, err := Parse(strings.NewReader(`
grid: [1, 2]
style: { padding: 9, rule: 1.5, text_size: 12 }
cells:
  - { widget: a/b }
  - { at: [0, 1], widget: a/c, style: { text_size: 24, padding: 0 } }
`))
	if err != nil {
		t.Fatal(err)
	}
	d := Display{Width: 800, Height: 960, PPI: 144} // 2 px per pt
	s0, s1 := tmpl.pluginStyle(tmpl.Cells[0], d), tmpl.pluginStyle(tmpl.Cells[1], d)
	if s0.Padding != 18 || s0.Rule != 3 || !near(s0.TextPx, 24) {
		t.Fatalf("page style: %+v", s0)
	}
	if s1.Padding != 0 || s1.Rule != 3 || !near(s1.TextPx, 48) {
		t.Fatalf("cell override: %+v", s1)
	}
}

func TestParseRejectsFractionalTextSize(t *testing.T) {
	for _, style := range []string{"{ text_size: 0.06 }", "{ text_size: 200 }", "{ padding: -1 }"} {
		_, err := Parse(strings.NewReader("grid: [1, 1]\nstyle: " + style + "\ncells: [{widget: a/b}]\n"))
		if err == nil {
			t.Errorf("%s accepted", style)
		}
	}
	_, err := Parse(strings.NewReader("grid: [1, 1]\ncells: [{widget: a/b, style: { text_size: 0.06 }}]\n"))
	if err == nil || !strings.Contains(err.Error(), "points") {
		t.Fatalf("fraction in a cell: %v", err)
	}
}
