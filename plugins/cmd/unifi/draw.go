// The status widget of the unifi plugin.

package main

import (
	"fmt"
	"image"

	"github.com/maehld/paperboy/sdk/design"
	"github.com/maehld/paperboy/sdk/format"
	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

// minNarrowHeight is the smallest stacked cell that can still hold a label and
// a value. A cell at the declared minimum of 120x60 has less room after the
// page padding, so this stays below that; Tiles drops lines and shrinks the
// value on its own.
const minNarrowHeight = 32

// Render draws the network status. The cell is either a stacked tile (2x2 and
// 3x2) or one row with the value on the left and the status line on the right
// (6x1). The value is the count the focus names; the status line names the most
// important fact.
func (u *unifi) Render(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	if w.Widget != "status" {
		return fmt.Errorf("%w %q", plugin.ErrUnknownWidget, w.Widget)
	}

	u.mu.Lock()
	snap, wan := u.snap, u.wanEnabled
	u.mu.Unlock()

	b := img.Rect.Inset(w.Style.Padding)
	if b.Empty() {
		return plugin.TooSmall(w)
	}
	t := func(de, en string) string { return req.T(de, en) }

	label, count := t("Clients", "Clients"), snap.clients
	if w.Arg().String("focus", "clients") == "devices" {
		label, count = t("Geräte", "Devices"), len(snap.devices)
	}
	value := "–"
	if snap.loaded {
		value = format.Number(float64(count), 0, req.Lang())
	}
	status := ""
	if w.Arg().Bool("detail", true) {
		status = snap.statusLine(wan, t)
	}

	scale := design.NewScale(req.TextPx(w), b.Dy())
	if b.Dx() > b.Dy()*6 {
		return drawRow(img, b, label, value, status, scale, w)
	}
	return drawStacked(img, b, label, value, status, scale, w)
}

// drawStacked draws the label, the value and the status line as one tile. The
// tile label does not shrink with the cell, so a short cell reduces the whole
// scale: the label keeps at most a quarter of the height, and the value stays
// above the label while the status line still fits.
func drawStacked(img *image.Paletted, b image.Rectangle, label, value, status string, s design.Scale, w plugin.WidgetRequest) error {
	if b.Dx() < 80 || b.Dy() < minNarrowHeight {
		return plugin.TooSmall(w)
	}
	if maxLabel := float64(b.Dy()) * 0.24; s.Detail > maxLabel {
		s = s.Scaled(maxLabel / s.Detail)
	}
	tile := design.Tile{Label: label, Value: value, Detail: status}
	if !design.Tiles(img, b, []design.Tile{tile}, s, design.TileOptions{
		Columns: 1,
		Align:   render.Center,
		Gap:     0,
		Rule:    max(w.Style.Rule, 1),
	}) {
		return plugin.TooSmall(w)
	}
	return nil
}

// drawRow draws one row: the label and the large value on the left, the status
// line on the right. A 6x1 cell has no room for the stacked layout.
func drawRow(img *image.Paletted, b image.Rectangle, label, value, status string, s design.Scale, w plugin.WidgetRequest) error {
	if b.Dx() < 80 || b.Dy() < 24 {
		return plugin.TooSmall(w)
	}
	gap := max(w.Style.Padding, 4)
	labelSize := min(s.Detail, float64(b.Dy())*0.45)
	statusSize := min(s.Detail, float64(b.Dy())*0.5)

	labelW := render.Measure(label, labelSize, false)
	valueX := b.Min.X + labelW + gap
	statusW := 0
	if status != "" {
		statusW = min(render.Measure(status, statusSize, false), b.Dx()*3/5)
	}
	valueMaxX := b.Max.X
	if statusW > 0 {
		valueMaxX = b.Max.X - statusW - gap
	}
	if valueMaxX-valueX < render.MinSize*2 {
		return plugin.TooSmall(w)
	}

	render.TextAt(img, label, b.Min.X, capBaseline(b.Min.Y, b.Dy(), labelSize),
		labelSize, false, render.Black, render.Left)
	if !design.DrawHero(img, image.Rect(valueX, b.Min.Y, valueMaxX, b.Max.Y),
		design.Hero{Value: value}, s) {
		return plugin.TooSmall(w)
	}
	if status != "" {
		text := render.Ellipsize(status, statusSize, false, statusW)
		render.TextAt(img, text, b.Max.X, capBaseline(b.Min.Y, b.Dy(), statusSize),
			statusSize, false, design.Muted(img), render.Right)
	}
	return nil
}

// capBaseline centers the capitals of a line in a box of height h. The design
// package keeps its own helper private; the row layout needs the same anchor.
func capBaseline(top, h int, size float64) int {
	const capShare = 0.72
	return top + int((float64(h)+size*capShare)/2)
}
