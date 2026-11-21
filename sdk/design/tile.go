package design

import (
	"image"
	"math"

	"github.com/maehld/paperboy/sdk/render"
)

// Tile is one value in a grid: a label above, the value in the title role, an
// optional detail below and an optional trend arrow beside the value.
type Tile struct {
	Label  string
	Value  string
	Unit   string
	Detail string
	// Trend is -1 down, 0 none, 1 up.
	Trend int
}

// TileOptions places a tile grid. Gap insets the text inside a cell; Rule is
// the thickness of the dividers.
type TileOptions struct {
	Columns int
	Divider bool
	Align   render.Align
	Gap     int
	Rule    int
}

// Tiles draws tiles in a grid with thin dividers. It returns false when no
// tile had room for even a value.
func Tiles(dst *image.Paletted, r image.Rectangle, tiles []Tile, s Scale, o TileOptions) bool {
	if len(tiles) == 0 || r.Empty() {
		return false
	}
	cols := o.Columns
	if cols < 1 {
		cols = 1
	}
	cols = min(cols, len(tiles))
	rows := (len(tiles) + cols - 1) / cols
	cw, ch := r.Dx()/cols, r.Dy()/rows
	if cw < 1 || ch < 1 {
		return false
	}
	pad := o.Gap
	if pad < 0 {
		pad = 0
	}
	rule := o.Rule
	if rule < 1 {
		rule = 1
	}
	drew := false
	for i, t := range tiles {
		cell := image.Rect(
			r.Min.X+(i%cols)*cw, r.Min.Y+(i/cols)*ch,
			r.Min.X+(i%cols+1)*cw, r.Min.Y+(i/cols+1)*ch,
		)
		if drawTile(dst, cell.Inset(pad), t, s, o.Align) {
			drew = true
		}
	}
	if o.Divider {
		for c := 1; c < cols; c++ {
			render.VLine(dst, r.Min.X+c*cw, r.Min.Y, r.Max.Y, rule)
		}
		for row := 1; row < rows; row++ {
			render.HLine(dst, r.Min.X, r.Min.Y+row*ch, r.Max.X, rule)
		}
	}
	return drew
}

// tilePlan is the resolved layout of one tile.
type tilePlan struct {
	labelSize, valueSize, detailSize float64
	showLabel, showValue, showDetail bool
	labelTop, valueTop, detailTop    int
	valueHeight                      int
}

// planTile resolves the sizes and the vertical placement of a tile. The
// detail falls before the value does.
func planTile(t Tile, inner image.Rectangle, s Scale) (tilePlan, bool) {
	if inner.Empty() {
		return tilePlan{}, false
	}
	p := tilePlan{labelSize: s.Detail, valueSize: max(min(s.Title, float64(inner.Dy())*0.40), render.MinSize), detailSize: s.Micro}
	p.showLabel = t.Label != ""
	p.showValue = t.Value != ""
	p.showDetail = t.Detail != ""

	if !p.showLabel && !p.showValue && !p.showDetail {
		return tilePlan{}, false
	}
	height := func(p tilePlan) int {
		h := 0
		add := func(line, space int) {
			if h > 0 {
				h += space
			}
			h += line
		}
		if p.showLabel {
			add(lineHeight(p.labelSize), 0)
		}
		if p.showValue {
			add(int(math.Round(p.valueSize*1.05)), gap(p.labelSize))
		}
		if p.showDetail {
			add(lineHeight(p.detailSize), gap(p.valueSize))
		}
		return h
	}
	for p.showDetail && height(p) > inner.Dy() {
		p.showDetail = false
	}
	for p.showValue && p.valueSize > render.MinSize && height(p) > inner.Dy() {
		p.valueSize *= 0.94
	}
	if height(p) > inner.Dy() {
		p.showLabel = false
	}
	if height(p) > inner.Dy() {
		return tilePlan{}, false
	}
	p.valueHeight = int(math.Round(p.valueSize * 1.05))
	top := inner.Min.Y
	if !p.showLabel && !p.showDetail {
		top += (inner.Dy() - height(p)) / 2
	}
	p.labelTop = top
	p.valueTop = top
	if p.showLabel {
		p.valueTop += lineHeight(p.labelSize) + gap(p.labelSize)
	}
	if p.showDetail {
		p.detailTop = inner.Max.Y - lineHeight(p.detailSize)
	}
	return p, true
}

// drawTile draws one tile into inner.
func drawTile(dst *image.Paletted, inner image.Rectangle, t Tile, s Scale, align render.Align) bool {
	p, ok := planTile(t, inner, s)
	if !ok {
		return false
	}
	if p.showLabel {
		drawLine(dst, t.Label, inner, p.labelTop, p.labelSize, false, render.Black, align)
	}
	if p.showValue {
		drawValue(dst, inner, p.valueTop, p.valueHeight, t.Value, t.Unit,
			p.valueSize, p.detailSize, true, render.Black, align, t.Trend)
	}
	if p.showDetail {
		drawLine(dst, t.Detail, inner, p.detailTop, p.detailSize, false, Muted(dst), align)
	}
	return true
}
