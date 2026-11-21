package main

import (
	"image"
	"image/color"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

// prices lists the nearest stations: name and open state left, distance and
// price right. The cheapest row is bold.
func (f *fuel) prices(w plugin.WidgetRequest, img *image.Paletted) error {
	b := img.Rect.Inset(w.Style.Padding)
	if b.Empty() {
		return plugin.TooSmall(w)
	}
	name, err := fuelFrom(w.Arg())
	if err != nil {
		return err
	}
	list := stationsWithFuel(f.cur, name)
	if len(list) == 0 {
		return f.message(w, img, f.t("Keine Preise", "No prices"))
	}
	n := w.Arg().Int("stations", 5)
	n = min(max(n, 1), min(len(list), maxStations))

	base := f.req.TextPx(w)
	head := min(base*0.9, float64(b.Dy())*0.16)
	top := b.Min.Y + int(head*1.5)
	if b.Max.Y-top < render.MinSize {
		return plugin.TooSmall(w)
	}
	rowH := float64(b.Max.Y-top) / float64(n)
	if rowH < base {
		n = max(int(float64(b.Max.Y-top)/base), 1)
		n = min(n, len(list))
		rowH = float64(b.Max.Y-top) / float64(n)
	}
	if n < 1 || rowH < render.MinSize*1.3 {
		return plugin.TooSmall(w)
	}

	gap := max(w.Style.Padding, 4)
	rule := max(w.Style.Rule, 1)
	nameSize := min(rowH*0.55, base*0.95)
	priceSize := min(rowH*0.62, base*1.05)
	priceW := render.Measure("1,699", priceSize, true)
	distW := render.Measure("99,9 km", nameSize, false)
	nameW := b.Dx() - priceW - distW - 2*gap
	showDist := true
	if nameW < 40 {
		showDist = false
		nameW = b.Dx() - priceW - gap
	}
	if nameW < 20 {
		return plugin.TooSmall(w)
	}

	render.Text(img, fuelLabel(name), b.Min.X, b.Min.Y+int(head),
		render.Style{Size: head, Bold: true, Shrink: true}, b.Dx()-priceW-gap, color.Black, render.Left)
	render.Text(img, "€/l", b.Max.X, b.Min.Y+int(head*0.95),
		render.Style{Size: head * 0.85}, priceW, color.Black, render.Right)

	mark := -1
	if cheap, ok := cheapestStation(list, name); ok {
		for i, s := range list {
			if s.ID == cheap.ID {
				mark = i
				break
			}
		}
	}
	for i, s := range list[:n] {
		rowY := top + int(float64(i)*rowH)
		baseline := rowY + int(rowH*0.72)
		cheap := i == mark
		text := s.label()
		if !s.Open {
			text += " · " + f.t("zu", "closed")
		}
		render.Text(img, text, b.Min.X, baseline,
			render.Style{Size: nameSize, Bold: cheap, Shrink: true}, nameW, color.Black, render.Left)
		v, _ := s.fuel(name)
		price := f.money(v)
		priceW := render.Measure(price, priceSize, cheap)
		if showDist {
			render.Text(img, f.km(s.Dist), b.Max.X-priceW-gap, baseline,
				render.Style{Size: nameSize, Shrink: true}, distW, color.Black, render.Right)
		}
		render.Text(img, price, b.Max.X, baseline,
			render.Style{Size: priceSize, Bold: cheap, Shrink: true}, priceW+2, color.Black, render.Right)
		if i > 0 && rowH > base*1.15 {
			render.HLine(img, b.Min.X, rowY, b.Max.X, rule)
		}
	}
	return nil
}

// cheapest shows one large price: the fuel, the station, the distance, the
// open state and since when the price holds.
func (f *fuel) cheapest(w plugin.WidgetRequest, img *image.Paletted) error {
	b := img.Rect.Inset(w.Style.Padding)
	if b.Empty() {
		return plugin.TooSmall(w)
	}
	name, err := fuelFrom(w.Arg())
	if err != nil {
		return err
	}
	s, ok := cheapestStation(f.cur, name)
	if !ok {
		return f.message(w, img, f.t("Keine Preise", "No prices"))
	}
	v, _ := s.fuel(name)

	h := b.Dy()
	base := f.req.TextPx(w)
	big := min(float64(h)*0.36, base*2.8)
	small := min(float64(h)*0.12, base*0.9)
	// The baselines: label, price, station, distance and state, since. The
	// price needs 0.78 of its size above its baseline, so the label clears it.
	layout := func() (float64, float64, float64, float64, float64) {
		y0 := small
		y1 := y0 + small*0.55 + big*0.78
		y2 := y1 + small*1.35
		y3 := y2 + small*1.2
		y4 := y3 + small*1.15
		return y0, y1, y2, y3, y4
	}
	y0, y1, y2, y3, total := layout()
	if total > float64(h) {
		k := float64(h) / total
		big, small = big*k, small*k
		y0, y1, y2, y3, total = layout()
	}
	if big < render.MinSize || small < render.MinSize {
		return plugin.TooSmall(w)
	}
	top := b.Min.Y + (h-int(total))/2

	render.Text(img, fuelLabel(name)+" · €/l", b.Min.X, top+int(y0),
		render.Style{Size: small, Bold: true, Shrink: true}, b.Dx(), color.Black, render.Left)
	render.Text(img, f.money(v), b.Min.X, top+int(y1),
		render.Style{Size: big, Bold: true, Shrink: true}, b.Dx(), color.Black, render.Left)
	render.Text(img, s.label(), b.Min.X, top+int(y2),
		render.Style{Size: small, Shrink: true}, b.Dx(), color.Black, render.Left)
	render.Text(img, f.km(s.Dist)+" · "+f.openText(s.Open), b.Min.X, top+int(y3),
		render.Style{Size: small * 0.9, Shrink: true}, b.Dx(), color.Black, render.Left)
	if since := f.lastChange(name); !since.IsZero() {
		render.Text(img, f.t("unverändert seit ", "unchanged since ")+since.In(f.loc).Format("15:04"),
			b.Min.X, top+int(y3+small*1.15),
			render.Style{Size: small * 0.85, Shrink: true}, b.Dx(), color.Black, render.Left)
	}
	return nil
}
