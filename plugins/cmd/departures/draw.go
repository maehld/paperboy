package main

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

func (p *departures) Render(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	if w.Widget != "departures" {
		return fmt.Errorf("%w %q", plugin.ErrUnknownWidget, w.Widget)
	}
	mode, err := timeMode(w)
	if err != nil {
		return err
	}
	locMode, err := localityMode(w)
	if err != nil {
		return err
	}
	order, err := localityOrder(w)
	if err != nil {
		return err
	}
	v, err := p.selected(w)
	if err != nil {
		return err
	}
	b := img.Bounds()
	pad := w.Style.Padding
	rule := max(w.Style.Rule, 1)
	base := max(req.TextPx(w), render.MinSize)
	// A row's text takes rowShare of the base text by default, the share the
	// calendar agenda uses. The count argument shows that many rows instead
	// and sizes their text to them, so fewer rows get a bigger text.
	headSize := max(base*rowShare, render.MinSize)
	top := b.Min.Y + pad
	left, right := b.Min.X+pad, b.Max.X-pad

	if w.Arg().Bool("header", false) && v.name != "" {
		render.Text(img, v.header(order), left, top+int(headSize), render.Style{Size: headSize, Bold: true, Shrink: true}, right-left, color.Black, render.Left)
		top += rowPitch(headSize, rule)
		render.HLine(img, left, top-rule-int(headSize*0.2), right, rule)
	}
	avail := b.Max.Y - pad - top
	if avail < rowPitch(render.MinSize, rule) {
		return plugin.TooSmall(w)
	}
	count := min(max(w.Arg().Int("count", 0), 0), p.max)
	rows, rowH, size := rowLayout(avail, base, rule, count)
	small := max(size*0.8, req.PtPx(5.8), render.MinSize)
	deps := v.list
	if len(deps) > rows {
		deps = deps[:rows]
	}
	if len(deps) == 0 {
		render.Text(img, p.t("Keine Abfahrten", "No departures"), left, top+int(size), render.Style{Size: size, Shrink: true}, right-left, color.Black, render.Left)
		return nil
	}

	// Column widths follow the content so that destinations get the rest.
	lineW, timeW, delayW := 0, 0, 0
	times := make([]string, len(deps))
	for i, d := range deps {
		times[i] = p.timeText(d, req.Now, mode)
		lineW = max(lineW, render.Measure(d.Line, size, true))
		timeW = max(timeW, render.Measure(times[i], size, true))
		if dl := delayText(d); dl != "" {
			delayW = max(delayW, render.Measure(dl, small, true))
		}
	}
	gap := int(size * 0.5)
	lineW = min(lineW, (right-left)/3)
	timeW = min(timeW, (right-left)/2)
	destX := left + lineW + gap
	timeX := right
	delayX := timeX - timeW
	destEnd := delayX
	if delayW > 0 {
		destEnd = delayX - gap/2 - delayW
	}
	destW := destEnd - gap/2 - destX
	names := make([]string, len(deps))
	for i, d := range deps {
		names[i] = v.destination(d, locMode, order)
	}
	destSize := destinationSize(names, size, destW)
	for i, d := range deps {
		y := top + i*rowH
		// The text sits in the middle of its row, so rows of different heights
		// read as one list.
		baseline := y + (rowH-rule)/2 + int(size*0.36)
		render.Text(img, d.Line, left, baseline, render.Style{Size: size, Bold: true, Shrink: true}, lineW, color.Black, render.Left)
		dest := render.Ellipsize(names[i], destSize, false, destW)
		render.TextAt(img, dest, destX, baseline, destSize, false, color.Black, render.Left)
		if d.Cancelled {
			tw := min(render.Measure(dest, destSize, false), destW)
			render.HLine(img, destX, baseline-int(size*0.3), destX+tw, rule)
		}
		if dl := delayText(d); dl != "" && !d.Cancelled {
			render.Text(img, dl, delayX-gap/2, baseline, render.Style{Size: small, Bold: true}, delayW, color.Black, render.Right)
		}
		render.Text(img, times[i], timeX, baseline, render.Style{Size: size, Bold: true, Shrink: true}, timeW, color.Black, render.Right)
		if i < len(deps)-1 {
			render.HLine(img, left, y+rowH-rule, right, rule)
		}
	}
	return nil
}

func timeMode(w plugin.WidgetRequest) (string, error) {
	m := w.Arg().String("time", "auto")
	switch m {
	case "auto", "minutes", "clock":
		return m, nil
	}
	return "", fmt.Errorf("time: %q is not auto, minutes or clock", m)
}

func (p *departures) t(de, en string) string { return p.req.T(de, en) }

// delayText is the real-time delay in minutes with a sign, empty when on time.
func delayText(d departure) string {
	if n := d.delay(); n != 0 {
		return fmt.Sprintf("%+d", n)
	}
	return ""
}

// timeText is the minutes until departure, or the clock time from one hour on.
// A cancelled departure shows that instead.
func (p *departures) timeText(d departure, now time.Time, mode string) string {
	return p.timeIn(d, now, mode, false)
}

func (p *departures) timeIn(d departure, now time.Time, mode string, compact bool) string {
	if d.Cancelled {
		if compact {
			return p.t("Ausf.", "canc.")
		}
		return p.t("Ausfall", "cancelled")
	}
	mins := int(d.when().Sub(now) / time.Minute)
	if mode == "clock" || (mode == "auto" && mins >= 60) {
		return d.when().In(p.location).Format("15:04")
	}
	if mins <= 0 {
		return p.t("jetzt", "now")
	}
	if compact {
		return fmt.Sprintf("%d", mins)
	}
	return fmt.Sprintf("%d min", mins)
}

func localityMode(w plugin.WidgetRequest) (string, error) {
	m := w.Arg().String("locality", "auto")
	switch m {
	case "auto", "always", "never":
		return m, nil
	}
	return "", fmt.Errorf("locality: %q is not auto, always or never", m)
}

func localityOrder(w plugin.WidgetRequest) (string, error) {
	o := w.Arg().String("locality_order", "after")
	switch o {
	case "after", "before", "none":
		return o, nil
	}
	return "", fmt.Errorf("locality_order: %q is not after, before or none", o)
}

// header is the selected stop as shown above the departures.
func (v *stopView) header(order string) string {
	if v.only == "" {
		return v.name
	}
	if order == "none" || v.locality == "" {
		return v.only
	}
	if order == "before" {
		return v.locality + ", " + v.only
	}
	return v.only + ", " + v.locality
}

// destination is the name shown for a departure. EFA names destinations
// "City, Stop"; locality decides whether the city stays, locality_order where
// it sits.
func (v *stopView) destination(d departure, mode, order string) string {
	city, stop, ok := splitLocality(d.Destination)
	if !ok {
		return d.Destination
	}
	keep := mode == "never"
	if mode == "auto" {
		keep = v.locality == "" || !strings.EqualFold(city, v.locality)
	}
	if !keep || order == "none" {
		return stop
	}
	if order == "before" {
		return city + ", " + stop
	}
	return stop + ", " + city
}

const (
	// minDestScale is how far a destination may shrink before it is
	// ellipsized.
	minDestScale = 0.8
	// rowShare is the text height of a list row relative to the base text,
	// the share the calendar agenda uses.
	rowShare = 0.6
	// rowLead is the row pitch in text heights: the text plus air for
	// descenders and the rule between rows.
	rowLead = 1.4
)

// rowPitch is the height of a row with this text size.
func rowPitch(size float64, rule int) int { return int(rowLead*size) + rule }

// rowLayout returns the rows, the row pitch and the text size of a departures
// list of avail pixels. Without count the list fills the cell with rows at
// rowShare of the base text. With count it shows that many rows: their text
// grows up to the base text, and rows that do not fit are dropped.
func rowLayout(avail int, base float64, rule, count int) (rows, rowH int, size float64) {
	if count <= 0 {
		size = max(min(rowShare*base, (float64(avail)-float64(rule))/rowLead), render.MinSize)
		rowH = rowPitch(size, rule)
		return max(avail/rowH, 1), rowH, size
	}
	rows = min(count, max(avail/rowPitch(render.MinSize, rule), 1))
	size = min(base, max((float64(avail)/float64(rows)-float64(rule))/rowLead, render.MinSize))
	return rows, rowPitch(size, rule), size
}

// destinationSize is the one font size for all destinations of a widget: the
// smallest any of them needs to fit width, but not below minDestScale of
// size. Names still too long are ellipsized at that size.
func destinationSize(names []string, size float64, width int) float64 {
	floor := max(size*minDestScale, render.MinSize)
	s := size
	for _, n := range names {
		for s > floor && render.Measure(n, s, false) > width {
			s = max(s-0.5, floor)
		}
	}
	return s
}

// splitLocality splits EFA's "City, Stop" into its two parts. Without a
// comma the name is the stop and there is no city.
func splitLocality(name string) (city, stop string, ok bool) {
	city, stop, ok = strings.Cut(name, ", ")
	if !ok || city == "" || stop == "" {
		return "", name, false
	}
	return city, stop, true
}
