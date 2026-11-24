package main

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

// balanceRow is one line of the balance. num is the value in W or kWh; a row
// without a value shows a dash and stays in place, so the balance never jumps.
type balanceRow struct {
	icon, label, value string
	num                float64
	idle               bool
}

// balanceTotals are the numbers of one balance, in W or in kWh. The set of rows
// follows the hardware (battery, loadpoints), not the values.
type balanceTotals struct {
	pv, gridImport, gridExport      float64
	batteryCharge, batteryDischarge float64
	home, charging                  float64
	chargingCount                   int
	hasBattery, hasLoadpoints       bool
	energy                          bool // kWh of a period instead of the W of /api/state
}

// balanceRows builds the fixed rows of the balance: sources on the left,
// consumers on the right.
func (e *evcc) balanceRows(t balanceTotals) (left, right []balanceRow) {
	value := e.power
	idle := float64(flowIdle)
	if t.energy {
		value, idle = e.kwhShort, flowIdleEnergy
	}
	left = append(left,
		balanceRow{"sun", "PV", value(t.pv), t.pv, false},
		balanceRow{"grid", e.t("Netzbezug", "Grid import"), value(t.gridImport), t.gridImport, false},
	)
	if t.hasBattery {
		left = append(left, balanceRow{"battery", e.t("Akku-Entladung", "Battery discharge"), value(t.batteryDischarge), t.batteryDischarge, false})
	}
	right = append(right, balanceRow{"home", e.t("Haus", "Home"), value(t.home), t.home, false})
	if t.hasLoadpoints {
		label := e.t("Laden", "Charging")
		if t.chargingCount > 1 {
			label += " (" + e.num(float64(t.chargingCount), 0) + ")"
		}
		right = append(right, balanceRow{"car", label, value(t.charging), t.charging, false})
	}
	if t.hasBattery {
		right = append(right, balanceRow{"battery", e.t("Akku-Ladung", "Battery charge"), value(t.batteryCharge), t.batteryCharge, false})
	}
	right = append(right, balanceRow{"grid", e.t("Einspeisung", "Grid export"), value(t.gridExport), t.gridExport, false})

	for i := range left {
		left[i].idle = left[i].num < idle
		if left[i].idle {
			left[i].value = "–"
		}
	}
	for i := range right {
		right[i].idle = right[i].num < idle
		if right[i].idle {
			right[i].value = "–"
		}
	}
	return left, right
}

// balanceNow reads the current power of /api/state.
func (e *evcc) balanceNow() balanceTotals {
	s := e.cur
	g := s.gridPower()
	t := balanceTotals{
		pv:            s.PVPower,
		gridImport:    max(g, 0),
		gridExport:    max(-g, 0),
		home:          s.HomePower,
		hasBattery:    s.hasBattery(),
		hasLoadpoints: len(s.Loadpoints) > 0,
	}
	if t.hasBattery {
		t.batteryDischarge = max(s.Battery.Power, 0)
		t.batteryCharge = max(-s.Battery.Power, 0)
	}
	for i := range s.Loadpoints {
		lp := &s.Loadpoints[i]
		t.charging += lp.ChargePower
		if lp.ChargePower >= flowIdle {
			t.chargingCount++
		}
	}
	return t
}

// balancePeriod sums the energy history of a period, the same day buckets and
// categories as the flow and the history widget.
func (e *evcc) balancePeriod(period string) (balanceTotals, error) {
	from, to, ok := e.periodRange(period)
	if !ok {
		return balanceTotals{}, fmt.Errorf("unknown period %q", period)
	}
	days, _, histErr := e.historyData()
	if days == nil {
		if histErr != nil {
			return balanceTotals{}, fmt.Errorf("evcc history: %w", histErr)
		}
		return balanceTotals{}, errors.New("evcc history is still loading")
	}
	row := historyRow{from: from, to: to}
	sum := func(key, metric string) float64 {
		list, ok := days[key]
		if !ok {
			return 0
		}
		return e.historySum(list, row, metric)
	}
	s := e.cur
	t := balanceTotals{
		pv:               sum("pv", "energy"),
		gridImport:       sum("grid", "energy"),
		gridExport:       sum("grid", "return"),
		batteryCharge:    sum("battery", "energy"),
		batteryDischarge: sum("battery", "return"),
		home:             sum("home", "energy"),
		hasBattery:       s.hasBattery(),
		hasLoadpoints:    len(s.Loadpoints) > 0,
		energy:           true,
	}
	for i := range s.Loadpoints {
		energy := sum(loadpointKey(s.Loadpoints[i].Title), "energy")
		t.charging += energy
		if energy >= flowIdleEnergy {
			t.chargingCount++
		}
	}
	return t, nil
}

// balance draws the energy split as a T-account: sources on the left,
// consumers on the right, both value columns at the centre line and a total
// per side. A zero row shows a gray dash in its fixed place. "now" shows the
// power of /api/state; a period shows the energy of the history in kWh.
func (e *evcc) balance(w plugin.WidgetRequest, img *image.Paletted) error {
	period := w.Arg().String("period", "now")
	var totals balanceTotals
	if period == "now" {
		totals = e.balanceNow()
	} else {
		t, err := e.balancePeriod(period)
		if err != nil {
			return err
		}
		totals = t
	}
	if img.Rect.Dx() < 260 || img.Rect.Dy() < 120 {
		return plugin.TooSmall(w)
	}
	b := img.Rect.Inset(w.Style.Padding)
	if b.Dx() < 240 || b.Dy() < 100 {
		return plugin.TooSmall(w)
	}
	left, right := e.balanceRows(totals)
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
				render.Text(img, rt.label, int(lx), int(math.Round(cy+labelSz*0.36)), render.Style{Size: labelSz, Shrink: true}, labelW, c, render.Left)
				render.Text(img, rt.value, int(divX-cpad), int(math.Round(cy+valueSz*0.36)), render.Style{Size: valueSz, Bold: true, Shrink: true}, valW, c, render.Right)
			} else {
				rx := float64(b.Max.X) - pad
				if iconW >= 9 {
					drawFlowIcon(img, rt.icon, rectAt(rx-iconW, cy-iconW/2, iconW, iconW), c)
					rx -= iconW + pad*0.6
				}
				labelW := int(rx-divX-cpad) - render.Measure(rt.value, valueSz, true) - int(pad)
				render.Text(img, rt.label, int(rx), int(math.Round(cy+labelSz*0.36)), render.Style{Size: labelSz, Shrink: true}, labelW, c, render.Right)
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
	value := e.power
	if totals.energy {
		value = e.kwhShort
	}
	sumSz := math.Min(totalH*0.55, base*1.05)
	sumY := int(math.Round(totalTop + totalH/2 + sumSz*0.36))
	totalInk := color.Color(render.DarkGray)
	if !gray {
		totalInk = color.Black
	}
	render.Text(img, e.t("Summe", "Total"), b.Min.X+int(pad), int(math.Round(totalTop+totalH/2+labelSz*0.35)), render.Style{Size: labelSz, Shrink: true}, int(W/2-pad), totalInk, render.Left)
	render.Text(img, e.t("Summe", "Total"), b.Max.X-int(pad), int(math.Round(totalTop+totalH/2+labelSz*0.35)), render.Style{Size: labelSz, Shrink: true}, int(W/2-pad), totalInk, render.Right)
	render.Text(img, value(leftSum), int(divX-cpad), sumY, render.Style{Size: sumSz, Bold: true, Shrink: true}, int(W/2-2*cpad), color.Black, render.Right)
	render.Text(img, value(rightSum), int(divX+cpad), sumY, render.Style{Size: sumSz, Bold: true, Shrink: true}, int(W/2-2*cpad), color.Black, render.Left)
	return nil
}
