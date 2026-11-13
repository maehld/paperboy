// The widgets of the dexcom plugin.

package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"sort"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

var (
	ink   = color.Gray{0}
	mid   = color.Gray{85}
	light = color.Gray{170}
)

func (d *dexcom) Render(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	switch w.Widget {
	case "glucose":
		return d.glucoseWidget(req, w, img)
	case "graph":
		return d.graphWidget(req, w, img)
	case "values":
		return d.valuesWidget(req, w, img)
	}
	return fmt.Errorf("%w %q", plugin.ErrUnknownWidget, w.Widget)
}

func (d *dexcom) Text(req *plugin.Request, w plugin.WidgetRequest) ([]string, error) {
	switch w.Widget {
	case "glucose":
		return d.glucoseText(req, w)
	case "values":
		return d.valuesText(req, w)
	}
	return nil, fmt.Errorf("%w %q on text displays", plugin.ErrUnknownWidget, w.Widget)
}

// ---- glucose ----

// glucoseWidget draws the reading, the trend, how it moved, its age and a bar
// with the target band.
func (d *dexcom) glucoseWidget(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	l, err := d.limits()
	if err != nil {
		return err
	}
	rs, loadErr, _ := d.snapshot()
	if loadErr != nil {
		return loadErr
	}
	b := img.Rect.Inset(w.Style.Padding)
	if b.Dx() < 40 || b.Dy() < 30 {
		return nil
	}
	last, ok := current(rs, req.Now)
	if !ok {
		textFit(img, req.T("keine Daten", "no data"), b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2,
			req.TextPx(w), false, mid, render.Center, b.Dx())
		return nil
	}

	age := req.Now.Sub(last.at)
	stale := age > staleAfter
	lvl := level(last.value, l)

	val := d.value(req, l, last.value)
	size := float64(b.Dy()) * 0.46
	arrowSize := int(size * 0.6)
	maxW := float64(b.Dx() - arrowSize - b.Dy()/8)
	for size > render.MinSize && float64(render.Measure(val, size, true)) > maxW {
		size *= 0.94
	}
	arrowSize = int(size * 0.6)
	tw := render.Measure(val, size, true)
	total := tw + arrowSize + b.Dy()/16
	x := b.Min.X + (b.Dx()-total)/2
	baseline := b.Min.Y + int(float64(b.Dy())*0.56)
	if lvl == "urgent" {
		baseline = b.Min.Y + int(float64(b.Dy())*0.50)
	}

	fg := ink
	if stale {
		fg = mid
	}
	if lvl != "" {
		fg = color.Gray{255}
		box := image.Rect(x-b.Dy()/28, baseline-int(size*0.80), x+total+b.Dy()/28, baseline+int(size*0.18))
		if lvl == "urgent" {
			// The ring of the app's urgent style.
			fill(img, box.Inset(-b.Dy()/28), ink)
			fill(img, box.Inset(-3), color.Gray{255})
		}
		fill(img, box, ink)
	}
	render.TextAt(img, val, x, baseline, size, true, fg, render.Left)
	if angle, count, ok := trendAngle(last.trend); ok && !stale {
		drawTrend(img, x+tw+b.Dy()/16+arrowSize/2, baseline-int(size*0.34), arrowSize, angle, count, fg)
	}

	small := float64(b.Dy()) * 0.085
	render.TextAt(img, ageLabel(req, age), b.Max.X, b.Min.Y+int(small), small, false, mid, render.Right)

	sub := baseline + int(float64(b.Dy())*0.22)
	switch lvl {
	case "urgent":
		textFit(img, req.T("Dringend!", "Urgent!"), b.Min.X+b.Dx()/2, sub, small*1.2, true, ink, render.Center, b.Dx())
	case "":
		if prev, ok := earlier(rs, 15); ok {
			delta := last.value - prev.value
			text := fmt.Sprintf("%s %s", d.signed(req, l, delta), l.unit)
			textFit(img, text, x, sub, small, false, mid, render.Left, b.Dx()-(x-b.Min.X))
		}
	default:
		textFit(img, l.unit, x, sub, small, false, mid, render.Left, b.Dx()-(x-b.Min.X))
	}

	// The band bar: the target range, the urgent threshold and the value.
	barH := max(3, b.Dy()/55)
	barY := b.Max.Y - barH/2 - 1
	span := l.max - l.min
	at := func(v float64) int { return b.Min.X + int((v-l.min)/span*float64(b.Dx()-1)) }
	hline(img, b.Min.X, b.Max.X, barY, 1, light)
	fill(img, image.Rect(at(l.low), barY-barH/2, at(l.high), barY+barH/2+1), mid)
	if l.urgent != 0 {
		fill(img, image.Rect(at(l.urgent), barY-barH/2-1, at(l.urgent)+1, barY+barH/2+2), mid)
	}
	fill(img, image.Rect(at(last.value)-1, barY-barH/2-3, at(last.value)+2, barY+barH/2+4), ink)
	if b.Dy() >= 100 {
		labels := float64(b.Dy()) * 0.06
		render.TextAt(img, d.value(req, l, l.low), b.Min.X, barY-int(labels)-2, labels, false, mid, render.Left)
		render.TextAt(img, d.value(req, l, l.high), b.Max.X, barY-int(labels)-2, labels, false, mid, render.Right)
	}
	return nil
}

// signed formats a change with its sign.
func (d *dexcom) signed(req *plugin.Request, l limits, v float64) string {
	s := d.num(req, v, l.decimals)
	if v > 0 {
		return "+" + s
	}
	return s
}

func (d *dexcom) glucoseText(req *plugin.Request, w plugin.WidgetRequest) ([]string, error) {
	l, err := d.limits()
	if err != nil {
		return nil, err
	}
	rs, loadErr, _ := d.snapshot()
	if loadErr != nil {
		return nil, loadErr
	}
	last, ok := current(rs, req.Now)
	if !ok {
		return []string{req.T("keine Daten", "no data")}, nil
	}
	age := req.Now.Sub(last.at)
	value := d.value(req, l, last.value) + " " + l.unit
	detail := ""
	switch lvl := level(last.value, l); {
	case lvl == "urgent":
		detail = req.T("Dringend!", "Urgent!")
	case lvl == "low":
		detail = req.T("niedrig", "low")
	case lvl == "high":
		detail = req.T("hoch", "high")
	case age > staleAfter:
		detail = ageLabel(req, age)
	default:
		if prev, ok := earlier(rs, 15); ok {
			detail = d.signed(req, l, last.value-prev.value)
		}
	}
	if w.Width < len([]rune(value))+len([]rune(detail))+2 {
		value = d.value(req, l, last.value)
	}
	return []string{plugin.Columns(value, detail, w.Width)}, nil
}

// ---- graph ----

// graphWidget draws the readings of the day or of the last hour. The area the
// curve spends outside the band is filled, so the chart shows how far and how
// long a reading was off, not just where the limits are.
func (d *dexcom) graphWidget(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	l, err := d.limits()
	if err != nil {
		return err
	}
	rs, loadErr, _ := d.snapshot()
	if loadErr != nil {
		return loadErr
	}
	b := img.Rect.Inset(w.Style.Padding)
	if b.Dx() < 60 || b.Dy() < 40 {
		return nil
	}
	period := w.Arg().String("period", "day")
	now := req.Now
	end := now
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, req.Location())
	title := req.T("Heute", "Today")
	step := 4
	if period == "hour" {
		if last, ok := current(rs, now); ok {
			end = last.at
		}
		start = end.Add(-time.Hour)
		title = req.T("Letzte Stunde", "Last hour")
		step = 0
	}
	span := end.Sub(start)
	if span <= 0 {
		return nil
	}

	// plot area: room for the title, the scale labels and the hour marks
	base := req.TextPx(w)
	titleSize := max(base*0.9, float64(b.Dy())*0.05)
	labelSize := max(base*0.6, 8.0)
	left := render.Measure(fmt.Sprintf("%.0f", l.max), labelSize, false) + 6
	plot := image.Rect(b.Min.X+left, b.Min.Y+int(titleSize*1.6), b.Max.X, b.Max.Y-int(labelSize*1.9))
	if plot.Dx() < 20 || plot.Dy() < 20 {
		return nil
	}
	xAt := func(t time.Time) int {
		f := float64(t.Sub(start)) / float64(span)
		return plot.Min.X + int(f*float64(plot.Dx()-1))
	}
	yAt := func(v float64) int {
		f := (v - l.min) / (l.max - l.min)
		if f < 0 {
			f = 0
		}
		if f > 1 {
			f = 1
		}
		return plot.Max.Y - 1 - int(f*float64(plot.Dy()-1))
	}

	// reading per pixel, for the fill
	var window []reading
	for _, r := range rs {
		if !r.at.Before(start) && !r.at.After(end) {
			window = append(window, r)
		}
	}
	if len(window) >= 2 {
		for x := plot.Min.X; x < plot.Max.X; x++ {
			v := valueAt(window, start, span, x, plot.Min.X, plot.Max.X-1)
			var top, bottom int
			switch {
			case v > l.high:
				top, bottom = yAt(v), yAt(l.high)
			case v < l.low:
				top, bottom = yAt(l.low), yAt(v)
			default:
				continue
			}
			if bottom > top {
				fill(img, image.Rect(x, top, x+1, bottom), mid)
			}
		}
	}

	// the band as two lines with their values
	hline(img, plot.Min.X, plot.Max.X, yAt(l.low), 1, mid)
	hline(img, plot.Min.X, plot.Max.X, yAt(l.high), 1, mid)
	render.TextAt(img, d.value(req, l, l.low), plot.Min.X-6, yAt(l.low)+int(labelSize*0.4), labelSize, false, mid, render.Right)
	render.TextAt(img, d.value(req, l, l.high), plot.Min.X-6, yAt(l.high)+int(labelSize*0.4), labelSize, false, mid, render.Right)
	hline(img, plot.Min.X, plot.Max.X, plot.Max.Y, 1, ink)

	// the curve
	for i := 1; i < len(window); i++ {
		line(img, xAt(window[i-1].at), yAt(window[i-1].value), xAt(window[i].at), yAt(window[i].value), max(2, plot.Dy()/160), ink)
	}
	if len(window) > 0 {
		last := window[len(window)-1]
		dot(img, xAt(last.at), yAt(last.value), max(4, plot.Dy()/45), ink)
	}

	// hour marks
	if period == "day" {
		for hour := 0; hour <= 24; hour += step {
			t := start.Add(time.Duration(hour) * time.Hour)
			if t.After(end) {
				break
			}
			x := xAt(t)
			fill(img, image.Rect(x, plot.Max.Y+2, x+1, plot.Max.Y+6), mid)
			render.TextAt(img, fmt.Sprintf("%02d", hour), x, plot.Max.Y+int(labelSize)+4, labelSize, false, mid, render.Center)
		}
	} else {
		// the five-minute points, so the resolution of the sensor shows
		for _, r := range window {
			dot(img, xAt(r.at), yAt(r.value), max(3, plot.Dy()/60), ink)
		}
		for i := 0; i <= 4; i++ {
			t := start.Add(time.Duration(i) * 15 * time.Minute)
			label := t.Format("15:04")
			half := render.Measure(label, labelSize, false) / 2
			x := min(max(xAt(t), plot.Min.X+half), plot.Max.X-half)
			render.TextAt(img, label, x, plot.Max.Y+int(labelSize)+4, labelSize, false, mid, render.Center)
		}
	}

	render.TextAt(img, title, b.Min.X, b.Min.Y+int(titleSize), titleSize, true, ink, render.Left)
	render.TextAt(img, l.unit, b.Max.X, b.Min.Y+int(titleSize), titleSize*0.8, false, mid, render.Right)
	if len(window) == 0 {
		textFit(img, req.T("keine Daten", "no data"), plot.Min.X+plot.Dx()/2, plot.Min.Y+plot.Dy()/2,
			base, false, mid, render.Center, plot.Dx())
	}
	return nil
}

// valueAt interpolates the series at the time of pixel x.
func valueAt(rs []reading, start time.Time, span time.Duration, x, x0, x1 int) float64 {
	f := float64(x-x0) / float64(max(x1-x0, 1))
	want := start.Add(time.Duration(f * float64(span)))
	i := sort.Search(len(rs), func(i int) bool { return !rs[i].at.Before(want) })
	switch {
	case i == 0:
		return rs[0].value
	case i >= len(rs):
		return rs[len(rs)-1].value
	}
	if rs[i].at.Equal(want) {
		return rs[i].value
	}
	prev := rs[i-1]
	f = float64(want.Sub(prev.at)) / float64(rs[i].at.Sub(prev.at))
	return prev.value + f*(rs[i].value-prev.value)
}

// ---- values ----

type valueEntry struct{ key, de, en string }

var valueEntries = []valueEntry{
	{"now", "Aktuell", "Current"},
	{"avg", "Ø 24h", "Avg 24h"},
	{"in_band", "Im Band", "In band"},
	{"min", "Tief 24h", "Low 24h"},
	{"max", "Hoch 24h", "High 24h"},
	{"delta", "Änderung", "Change"},
	{"trend", "Trend", "Trend"},
	{"age", "Alter", "Age"},
	{"count", "Messungen", "Readings"},
	{"band", "Zielband", "Target"},
}

func valueKeys() []any {
	out := make([]any, len(valueEntries))
	for i, v := range valueEntries {
		out[i] = v.key
	}
	return out
}

var valueByKey = func() map[string]valueEntry {
	m := make(map[string]valueEntry, len(valueEntries))
	for _, v := range valueEntries {
		m[v.key] = v
	}
	return m
}()

// entryLabels names the keys for the editor in the request's language.
func entryLabels(lang string) map[string]string {
	out := make(map[string]string, len(valueEntries))
	for _, v := range valueEntries {
		if lang == "de" {
			out[v.key] = v.de
		} else {
			out[v.key] = v.en
		}
	}
	return out
}

func (d *dexcom) entryLabel(req *plugin.Request, v valueEntry) string {
	if req.Lang() == "de" {
		return v.de
	}
	return v.en
}

// entryText formats one entry.
func (d *dexcom) entryText(req *plugin.Request, l limits, rs []reading, key string) string {
	s := summarize(rs, l)
	last, ok := current(rs, req.Now)
	switch key {
	case "now":
		if ok {
			return d.value(req, l, last.value) + " " + l.unit
		}
	case "avg":
		if s.count > 0 {
			return d.value(req, l, s.avg) + " " + l.unit
		}
	case "in_band":
		if s.count > 0 {
			return fmt.Sprintf("%.0f %%", s.inBand)
		}
	case "min":
		if s.count > 0 {
			return d.value(req, l, s.min) + " " + l.unit
		}
	case "max":
		if s.count > 0 {
			return d.value(req, l, s.max) + " " + l.unit
		}
	case "delta":
		if prev, ok := earlier(rs, 15); ok && len(rs) > 0 {
			return d.signed(req, l, rs[len(rs)-1].value-prev.value)
		}
	case "trend":
		if ok {
			return trendArrow(last.trend)
		}
	case "age":
		if ok {
			return ageLabel(req, req.Now.Sub(last.at))
		}
	case "count":
		return fmt.Sprintf("%d", s.count)
	case "band":
		return fmt.Sprintf("%s–%s %s", d.value(req, l, l.low), d.value(req, l, l.high), l.unit)
	}
	return "–"
}

func (d *dexcom) valueKeysFor(w plugin.WidgetRequest) []string {
	keys := w.Arg().Strings("values")
	if len(keys) == 0 {
		for _, v := range valueEntries {
			keys = append(keys, v.key)
		}
	}
	return keys
}

func (d *dexcom) valuesWidget(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	l, err := d.limits()
	if err != nil {
		return err
	}
	rs, loadErr, _ := d.snapshot()
	if loadErr != nil {
		return loadErr
	}
	keys := d.valueKeysFor(w)
	b := img.Rect.Inset(w.Style.Padding)
	if b.Empty() || len(keys) == 0 {
		return nil
	}
	cols := w.Arg().Int("columns", 1)
	if cols < 1 || cols > 2 {
		cols = 1
	}
	base := req.TextPx(w)
	rows := (len(keys) + cols - 1) / cols
	for rows > 1 && float64(b.Dy())/float64(rows) < render.MinSize*1.8 {
		rows--
	}
	n := min(len(keys), rows*cols)
	rowH := float64(b.Dy()) / float64(rows)
	cellW := float64(b.Dx()) / float64(cols)
	label := render.Style{Size: min(rowH*0.5, base*0.9), Shrink: true}
	value := render.Style{Size: min(rowH*0.58, base), Bold: true, Shrink: true}
	gap := max(w.Style.Padding, 4)
	rule := max(w.Style.Rule, 1)
	for i := 0; i < n; i++ {
		v, ok := valueByKey[keys[i]]
		if !ok {
			return fmt.Errorf("unknown value %q", keys[i])
		}
		row, col := i/cols, i%cols
		x := b.Min.X + int(float64(col)*cellW)
		rowY := b.Min.Y + int(float64(row)*rowH)
		baseline := rowY + int(rowH*0.72)
		text := d.entryText(req, l, rs, v.key)
		valueW := render.Measure(text, value.Size, true)
		width := int(cellW) - 2
		render.Text(img, d.entryLabel(req, v), x, baseline, label, width-valueW-gap, ink, render.Left)
		render.Text(img, text, x+int(cellW)-1, baseline, value, valueW, ink, render.Right)
		if row > 0 && col == 0 {
			hline(img, b.Min.X, b.Max.X, rowY, rule, light)
		}
	}
	return nil
}

func (d *dexcom) valuesText(req *plugin.Request, w plugin.WidgetRequest) ([]string, error) {
	l, err := d.limits()
	if err != nil {
		return nil, err
	}
	rs, loadErr, _ := d.snapshot()
	if loadErr != nil {
		return nil, loadErr
	}
	var lines []string
	for _, key := range d.valueKeysFor(w) {
		v, ok := valueByKey[key]
		if !ok {
			return nil, fmt.Errorf("unknown value %q", key)
		}
		if len(lines) == w.Height {
			break
		}
		lines = append(lines, plugin.Columns(d.entryLabel(req, v), d.entryText(req, l, rs, key), w.Width))
	}
	return lines, nil
}

// ---- small helpers ----

func fill(img *image.Paletted, r image.Rectangle, c color.Color) {
	render.FillColor(img, r, c)
}

func hline(img *image.Paletted, x0, x1, y, thickness int, c color.Color) {
	render.FillColor(img, image.Rect(x0, y, x1, y+thickness), c)
}

func line(img *image.Paletted, x0, y0, x1, y1, thickness int, c color.Color) {
	steps := max(absInt(x1-x0), absInt(y1-y0), 1)
	for i := 0; i <= steps; i++ {
		x := x0 + (x1-x0)*i/steps
		y := y0 + (y1-y0)*i/steps
		render.FillColor(img, image.Rect(x, y, x+thickness, y+thickness), c)
	}
}

func dot(img *image.Paletted, x, y, size int, c color.Color) {
	render.FillColor(img, image.Rect(x-size/2, y-size/2, x-size/2+size, y-size/2+size), c)
}

// drawTrend draws a trend arrow at the given angle, once or twice.
func drawTrend(img *image.Paletted, cx, cy, size int, angle float64, count int, c color.Color) {
	if count >= 2 {
		drawTrend(img, cx-size/3, cy, size*2/3, angle, 1, c)
		drawTrend(img, cx+size/3, cy, size*2/3, angle, 1, c)
		return
	}
	rad := angle * math.Pi / 180
	dx, dy := math.Cos(rad), math.Sin(rad)
	half := float64(size) / 2
	thickness := max(2, size/10)
	x0 := int(float64(cx) - dx*half)
	y0 := int(float64(cy) - dy*half)
	x1 := int(float64(cx) + dx*half)
	y1 := int(float64(cy) + dy*half)
	line(img, x0, y0, x1, y1, thickness, c)
	head := float64(size) * 0.38
	for _, a := range []float64{angle - 145, angle + 145} {
		r := a * math.Pi / 180
		line(img, x1, y1, x1+int(math.Cos(r)*head), y1+int(math.Sin(r)*head), thickness, c)
	}
}

// textFit draws s, shrinking it until it fits maxWidth, and returns the size.
func textFit(img *image.Paletted, s string, x, baseline int, size float64, bold bool, c color.Color, align render.Align, maxWidth int) float64 {
	if maxWidth <= 0 {
		return 0
	}
	for size > render.MinSize && render.Measure(s, size, bold) > maxWidth {
		size *= 0.94
	}
	render.TextAt(img, s, x, baseline, size, bold, c, align)
	return size
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
