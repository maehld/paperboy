package main

import (
	"image"
	"image/color"
	"math"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

// balanceRow is one line of the balance. num is the value in W; a row without
// power shows a dash and stays in place, so the balance never jumps.
type balanceRow struct {
	icon, label, value string
	num                float64
	idle               bool
}

// balanceRows builds the fixed rows of the balance: sources on the left,
// consumers on the right. The set of rows depends on the hardware (battery,
// loadpoints), not on the current values.
func (e *evcc) balanceRows() (left, right []balanceRow) {
	s := e.cur
	g := s.gridPower()
	left = append(left,
		balanceRow{"sun", "PV", e.power(s.PVPower), s.PVPower, false},
		balanceRow{"grid", e.t("Netzbezug", "Grid import"), e.power(g), max(g, 0), false},
	)
	if s.hasBattery() {
		p := max(s.Battery.Power, 0)
		left = append(left, balanceRow{"battery", e.t("Akku-Entladung", "Battery discharge"), e.power(p), p, false})
	}
	right = append(right, balanceRow{"home", e.t("Haus", "Home"), e.power(s.HomePower), s.HomePower, false})
	if len(s.Loadpoints) > 0 {
		sum, charging := 0.0, 0
		for i := range s.Loadpoints {
			lp := &s.Loadpoints[i]
			sum += lp.ChargePower
			if lp.ChargePower >= flowIdle {
				charging++
			}
		}
		label := e.t("Laden", "Charging")
		if charging > 1 {
			label += " (" + e.num(float64(charging), 0) + ")"
		}
		right = append(right, balanceRow{"car", label, e.power(sum), sum, false})
	}
	if s.hasBattery() {
		p := max(-s.Battery.Power, 0)
		right = append(right, balanceRow{"battery", e.t("Akku-Ladung", "Battery charge"), e.power(p), p, false})
	}
	right = append(right, balanceRow{"grid", e.t("Einspeisung", "Grid export"), e.power(g), max(-g, 0), false})

	for i := range left {
		left[i].idle = left[i].num < flowIdle
		if left[i].idle {
			left[i].value = "–"
		}
	}
	for i := range right {
		right[i].idle = right[i].num < flowIdle
		if right[i].idle {
			right[i].value = "–"
		}
	}
	return left, right
}

// balance draws the energy split as a T-account: sources on the left,
// consumers on the right, both value columns at the centre line and a total
// per side. A zero row shows a gray dash in its fixed place.
func (e *evcc) balance(w plugin.WidgetRequest, img *image.Paletted) error {
	if img.Rect.Dx() < 260 || img.Rect.Dy() < 120 {
		return plugin.TooSmall(w)
	}
	b := img.Rect.Inset(w.Style.Padding)
	if b.Dx() < 240 || b.Dy() < 100 {
		return plugin.TooSmall(w)
	}
	left, right := e.balanceRows()
	base := e.req.TextPx(w)
	gray := render.Levels(img.Palette) > 2
	H := float64(b.Dy())
	W := float64(b.Dx())

	headSz := math.Min(H*0.075, base*0.85)
	headH := headSz * 1.9
	rows := max(len(left), len(right))
	totalH := headSz * 1.9
	rowH := (H - headH - totalH - 2) / float64(rows)
	if rowH < 18 {
		return plugin.TooSmall(w)
	}
	labelSz := math.Min(rowH*0.30, base*0.90)
	valueSz := math.Min(rowH*0.50, base*1.50)
	iconW := math.Min(rowH*0.50, labelSz*1.7)
	divX := float64(b.Min.X) + W/2
	pad := math.Max(base*0.25, 4)
	cpad := math.Max(base*0.55, 8)

	render.Text(img, e.t("Erzeugung", "Sources"), b.Min.X, b.Min.Y+int(headSz), render.Style{Size: headSz, Bold: true}, int(W/2-pad), color.Black, render.Left)
	render.Text(img, e.t("Verbrauch", "Consumers"), b.Max.X, b.Min.Y+int(headSz), render.Style{Size: headSz, Bold: true}, int(W/2-pad), color.Black, render.Right)
	render.HLine(img, b.Min.X, int(math.Round(float64(b.Min.Y)+headH)), b.Max.X, 1)
	render.VLine(img, int(divX), int(math.Round(float64(b.Min.Y)+headH)), b.Max.Y, 1)

	draw := func(list []balanceRow, leftSide bool) {
		for i, rt := range list {
			y := float64(b.Min.Y) + headH + float64(i)*rowH
			cy := y + rowH/2
			c := color.Color(color.Black)
			if rt.idle && gray {
				c = render.DarkGray
			}
			valW := int(W/2 - 2*cpad - iconW)
			if leftSide {
				lx := float64(b.Min.X) + pad
				if iconW >= 9 {
					drawFlowIcon(img, rt.icon, rectAt(lx, cy-iconW/2, iconW, iconW), c)
					lx += iconW + pad*0.6
				}
				labelW := int(divX-cpad-lx) - render.Measure(rt.value, valueSz, true) - int(pad)
				render.Text(img, rt.label, int(lx), int(math.Round(cy-labelSz*0.05)), render.Style{Size: labelSz, Shrink: true}, labelW, c, render.Left)
				render.Text(img, rt.value, int(divX-cpad), int(math.Round(cy+valueSz*0.36)), render.Style{Size: valueSz, Bold: true, Shrink: true}, valW, c, render.Right)
			} else {
				rx := float64(b.Max.X) - pad
				if iconW >= 9 {
					drawFlowIcon(img, rt.icon, rectAt(rx-iconW, cy-iconW/2, iconW, iconW), c)
					rx -= iconW + pad*0.6
				}
				labelW := int(rx-divX-cpad) - render.Measure(rt.value, valueSz, true) - int(pad)
				render.Text(img, rt.label, int(rx), int(math.Round(cy-labelSz*0.05)), render.Style{Size: labelSz, Shrink: true}, labelW, c, render.Right)
				render.Text(img, rt.value, int(divX+cpad), int(math.Round(cy+valueSz*0.36)), render.Style{Size: valueSz, Bold: true, Shrink: true}, valW, c, render.Left)
			}
			if i > 0 {
				render.HLine(img, int(b.Min.X), int(math.Round(y)), int(b.Max.X), 1)
			}
		}
	}
	draw(left, true)
	draw(right, false)

	totalTop := float64(b.Max.Y) - totalH
	render.HLine(img, b.Min.X, int(math.Round(totalTop)), b.Max.X, 1)
	leftSum, rightSum := 0.0, 0.0
	for _, r := range left {
		leftSum += r.num
	}
	for _, r := range right {
		rightSum += r.num
	}
	sumSz := math.Min(totalH*0.55, base*1.05)
	sumY := int(math.Round(totalTop + totalH/2 + sumSz*0.36))
	totalInk := color.Color(render.DarkGray)
	if !gray {
		totalInk = color.Black
	}
	render.Text(img, e.t("Summe", "Total"), b.Min.X+int(pad), int(math.Round(totalTop+totalH/2+labelSz*0.35)), render.Style{Size: labelSz, Shrink: true}, int(W/2-pad), totalInk, render.Left)
	render.Text(img, e.t("Summe", "Total"), b.Max.X-int(pad), int(math.Round(totalTop+totalH/2+labelSz*0.35)), render.Style{Size: labelSz, Shrink: true}, int(W/2-pad), totalInk, render.Right)
	render.Text(img, e.power(leftSum), int(divX-cpad), sumY, render.Style{Size: sumSz, Bold: true, Shrink: true}, int(W/2-2*cpad), color.Black, render.Right)
	render.Text(img, e.power(rightSum), int(divX+cpad), sumY, render.Style{Size: sumSz, Bold: true, Shrink: true}, int(W/2-2*cpad), color.Black, render.Left)
	return nil
}
