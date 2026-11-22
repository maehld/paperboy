package design

import (
	"image"
	"image/color"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/maehld/paperboy/sdk/render"
)

// SeriesStyle is how a trend series is drawn.
type SeriesStyle int

const (
	// Line connects the points with a line.
	Line SeriesStyle = iota
	// Area fills the columns below the line.
	Area
	// Step holds a value until the next point.
	Step
	// Bars draws one bar per point.
	Bars
)

// Point is one sample of a series. Fill overrides the fill of its bar.
type Point struct {
	At    time.Time
	Value float64
	Fill  string
}

// Range is a value range, for the trend axis or for a series with its own
// scale.
type Range struct {
	Min, Max float64
}

// Series is one data series of a trend.
type Series struct {
	Points []Point
	Style  SeriesStyle
	// Fill is shade (default), hatch, solid or none.
	Fill string
	// Scale maps the series onto its own range. Without it the series uses
	// the trend's axis range.
	Scale *Range
	// Halo draws a white edge around a line, so bars below it do not hide it.
	Halo  bool
	Width int
}

// Mark is a labelled point in a trend, such as the highest reading of a day.
// The plugin chooses the point and formats the label.
type Mark struct {
	At    time.Time
	Value float64
	// Scale maps the mark onto a series' own range. Without it the mark uses
	// the trend's axis range.
	Scale *Range
	Label string
}

// LegendItem is one entry of a trend legend.
type LegendItem struct {
	Label string
	Fill  string
}

// Trend is a chart of one or more series over time. Label, Current and Legend
// occupy the title row; the axis margins follow the type scale.
type Trend struct {
	From, To time.Time
	Series   []Series
	// Range fixes the value axis. Without it the axis follows the series
	// that has no Scale of its own.
	Range *Range
	// Axis draws the value labels and the bottom line.
	Axis bool
	// Grid draws a line at the minimum, the middle and the maximum.
	Grid bool
	// Ticks are the time marks of the bottom axis. Without them the chart
	// uses the start, the middle and the end.
	Ticks      []time.Time
	TickFormat func(time.Time) string
	Now        time.Time
	// Marks label single points, such as the extrema of a series. A mark
	// outside the trend span is not drawn.
	Marks   []Mark
	Label   string
	Current string
	Legend  []LegendItem
	Format  func(float64) string
	// BarGap is the space between two bars in pixels; default 1.
	BarGap int
}

// DrawTrend draws a trend into r and returns false when r is too small.
func DrawTrend(dst *image.Paletted, r image.Rectangle, t Trend, s Scale) bool {
	from, to := trendSpan(t)
	if from.IsZero() || to.IsZero() || !to.After(from) {
		return false
	}
	lo, hi, ok := trendRange(t)
	if !ok {
		return false
	}
	if t.Range != nil {
		lo, hi = t.Range.Min, t.Range.Max
	}
	if hi < lo {
		lo, hi = hi, lo
	}
	if hi == lo {
		pad := math.Abs(lo) * 0.05
		if pad == 0 {
			pad = 1
		}
		lo, hi = lo-pad, hi+pad
	}

	format := t.Format
	if format == nil {
		format = defaultFormat
	}
	axisSize := s.Micro
	titleSize := s.Detail
	titleH := 0
	if t.Label != "" || t.Current != "" || len(t.Legend) > 0 {
		titleH = lineHeight(titleSize)
	}
	labelLeft := 0
	rightW := 0
	if t.Current != "" {
		rightW = render.Measure(t.Current, titleSize, true)
	}
	legendRight := r.Max.X
	if t.Current != "" {
		legendRight -= rightW + gap(titleSize)
	}
	if len(t.Legend) > 0 {
		w := measureLegend(t.Legend, titleSize, r.Dx())
		if w > 0 && legendRight-w >= r.Min.X {
			labelLeft = legendRight - w
		}
	}
	labelRight := r.Max.X - rightW
	if labelLeft > 0 {
		labelRight = min(labelRight, labelLeft-gap(titleSize))
	}
	if t.Current != "" || len(t.Legend) > 0 {
		// The title needs room for the value or the legend.
		if labelRight < r.Min.X+40 {
			labelRight = r.Min.X + 40
		}
	}

	leftW := 0
	yValues := []float64{lo, (lo + hi) / 2, hi}
	if t.Axis {
		for _, v := range yValues {
			leftW = max(leftW, render.Measure(format(v), axisSize, false))
		}
		leftW += 6
	}
	bottom := r.Max.Y
	if t.Axis || len(t.Ticks) > 0 {
		bottom -= lineHeight(axisSize) + 3
	}
	top := r.Min.Y + titleH
	if titleH > 0 {
		top += 2
	}
	plot := image.Rect(r.Min.X+leftW, top, r.Max.X, bottom)
	if plot.Dx() < 20 || plot.Dy() < 20 {
		return false
	}

	if titleH > 0 {
		if t.Label != "" {
			drawLine(dst, t.Label, image.Rect(r.Min.X, r.Min.Y, labelRight, r.Max.Y),
				r.Min.Y, titleSize, true, render.Black, render.Left)
		}
		if t.Current != "" {
			drawLine(dst, t.Current, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Max.Y),
				r.Min.Y, titleSize, true, render.Black, render.Right)
		}
		if len(t.Legend) > 0 {
			drawLegend(dst, r, t.Legend, titleSize, legendRight)
		}
	}

	xAt := func(at time.Time) int {
		f := float64(at.Sub(from)) / float64(to.Sub(from))
		return plot.Min.X + int(math.Round(f*float64(plot.Dx()-1)))
	}
	yAt := func(v float64, lo, hi float64) int {
		f := (v - lo) / (hi - lo)
		f = min(max(f, 0), 1)
		return plot.Max.Y - int(math.Round(f*float64(plot.Dy()-1)))
	}

	if t.Grid {
		for _, v := range yValues {
			render.HLine(dst, plot.Min.X, yAt(v, lo, hi), plot.Max.X, 1)
		}
	}
	if t.Axis {
		render.HLine(dst, plot.Min.X, plot.Max.Y, plot.Max.X, 1)
		for _, v := range yValues {
			y := yAt(v, lo, hi)
			render.TextAt(dst, format(v), plot.Min.X-4, y+int(math.Round(axisSize*0.35)),
				axisSize, false, render.Black, render.Right)
		}
	}
	ticks := t.Ticks
	if len(ticks) == 0 && t.Axis {
		ticks = []time.Time{from, from.Add(to.Sub(from) / 2), to}
	}
	tickFormat := t.TickFormat
	if tickFormat == nil {
		tickFormat = func(at time.Time) string { return at.Format("15:04") }
	}
	for i, at := range ticks {
		if at.Before(from) || at.After(to) {
			continue
		}
		x := xAt(at)
		render.VLine(dst, x, plot.Max.Y, plot.Max.Y+3, 1)
		align := render.Center
		switch i {
		case 0:
			align = render.Left
		case len(ticks) - 1:
			align = render.Right
		}
		render.TextAt(dst, tickFormat(at), x, r.Max.Y-2, axisSize, false, render.Black, align)
	}
	if !t.Now.IsZero() && !t.Now.Before(from) && !t.Now.After(to) {
		render.Segment(dst, xAt(t.Now), plot.Min.Y, xAt(t.Now), plot.Max.Y, 1, true)
	}
	for _, se := range t.Series {
		slo, shi := lo, hi
		if se.Scale != nil {
			slo, shi = se.Scale.Min, se.Scale.Max
		}
		if shi <= slo {
			continue
		}
		drawSeries(dst, plot, se, slo, shi, xAt, yAt, t.BarGap)
	}
	var labels []image.Rectangle
	for _, m := range t.Marks {
		if m.At.IsZero() || m.At.Before(from) || m.At.After(to) {
			continue
		}
		mlo, mhi := lo, hi
		if m.Scale != nil {
			mlo, mhi = m.Scale.Min, m.Scale.Max
		}
		if mhi <= mlo {
			continue
		}
		at := image.Pt(xAt(m.At), yAt(m.Value, mlo, mhi))
		at, inPlot := clampMark(plot, at)
		if !inPlot {
			continue
		}
		label := m.Label
		if box, ok := markLabelRect(plot, at, label, axisSize); ok {
			// Two labels close to each other would overwrite each other. The
			// dot stays, the second label drops.
			for _, used := range labels {
				if box.Overlaps(used) {
					label = ""
					break
				}
			}
			if label != "" {
				labels = append(labels, box)
			}
		}
		DrawMark(dst, plot, at, label, axisSize)
	}
	return true
}

// Extrema returns the lowest and the highest point of a series. A series
// without points answers ok false.
func Extrema(points []Point) (low, high Point, ok bool) {
	if len(points) == 0 {
		return Point{}, Point{}, false
	}
	low, high = points[0], points[0]
	for _, p := range points[1:] {
		if p.Value < low.Value {
			low = p
		}
		if p.Value > high.Value {
			high = p
		}
	}
	return low, high, true
}

// ExtremaMarks returns the marks of the extrema of a series. Mode is "min",
// "max", "both" or anything else for no marks. Format labels one value.
func ExtremaMarks(mode string, points []Point, format func(float64) string) []Mark {
	low, high, ok := Extrema(points)
	if !ok {
		return nil
	}
	var out []Mark
	if mode == "min" || mode == "both" {
		out = append(out, Mark{At: low.At, Value: low.Value, Label: format(low.Value)})
	}
	if mode == "max" || mode == "both" {
		out = append(out, Mark{At: high.At, Value: high.Value, Label: format(high.Value)})
	}
	return out
}

// DrawMark draws a dot at at and, when it fits, the label beside it. The label
// goes to the side with more room and drops when the plot has no room for it;
// the dot always stays. DrawMark reports whether the dot was inside the plot.
func DrawMark(dst *image.Paletted, plot image.Rectangle, at image.Point, label string, size float64) bool {
	if plot.Empty() || at.X < plot.Min.X || at.X > plot.Max.X ||
		at.Y < plot.Min.Y || at.Y > plot.Max.Y {
		return false
	}
	at, ok := clampMark(plot, at)
	if !ok {
		return false
	}
	dot := markDot(plot)
	// A white edge keeps the dot readable on a line or a bar.
	render.FillColor(dst, image.Rect(at.X-dot-1, at.Y-dot-1, at.X+dot+2, at.Y+dot+2), render.White)
	render.FillColor(dst, image.Rect(at.X-dot, at.Y-dot, at.X+dot+1, at.Y+dot+1), render.Black)
	box, ok := markLabelRect(plot, at, label, size)
	if !ok {
		return true
	}
	size = max(size, render.MinSize)
	// The white box keeps the label readable over a bar or a filled area.
	render.FillColor(dst, box, render.White)
	render.TextAt(dst, label, box.Min.X+box.Dx()/2, box.Min.Y+int(math.Round(size*0.8)),
		size, false, render.Black, render.Center)
	return true
}

// markLabelRect returns the rectangle of a mark label and whether the plot has
// room for it. The label goes below a point in the upper half and above a
// point in the lower half.
func markLabelRect(plot image.Rectangle, at image.Point, label string, size float64) (image.Rectangle, bool) {
	if label == "" || plot.Empty() {
		return image.Rectangle{}, false
	}
	size = max(size, render.MinSize)
	w := render.Measure(label, size, false)
	pad := max(int(math.Round(size*0.15)), 1)
	if w+2*pad > plot.Dx() {
		return image.Rectangle{}, false
	}
	height := int(math.Round(size*0.8)) + 2*pad
	gap := max(markDot(plot), int(math.Round(size*0.2)))
	x := min(max(at.X, plot.Min.X+w/2+pad), plot.Max.X-w/2-pad)
	above := image.Rect(x-w/2-pad, at.Y-gap-height, x+w/2+pad, at.Y-gap)
	below := image.Rect(above.Min.X, at.Y+gap, above.Max.X, at.Y+gap+height)
	first, second := below, above
	if at.Y > plot.Min.Y+plot.Dy()/2 {
		first, second = above, below
	}
	for _, box := range []image.Rectangle{first, second} {
		if box.Min.X >= plot.Min.X && box.Max.X <= plot.Max.X &&
			box.Min.Y >= plot.Min.Y && box.Max.Y <= plot.Max.Y {
			return box, true
		}
	}
	return image.Rectangle{}, false
}

// markDot is the radius of a mark dot.
func markDot(plot image.Rectangle) int {
	return max(plot.Dy()/80, 2)
}

// clampMark moves a point inside the plot so the dot does not cover the axis
// or the frame. It answers false when the plot is smaller than the dot.
func clampMark(plot image.Rectangle, at image.Point) (image.Point, bool) {
	dot := markDot(plot)
	if plot.Dx() <= 2*dot || plot.Dy() <= 2*dot {
		return at, false
	}
	at.X = min(max(at.X, plot.Min.X+dot), plot.Max.X-dot)
	at.Y = min(max(at.Y, plot.Min.Y+dot), plot.Max.Y-dot)
	return at, true
}

// trendSpan returns the time range of the trend.
func trendSpan(t Trend) (time.Time, time.Time) {
	from, to := t.From, t.To
	if !from.IsZero() && !to.IsZero() {
		return from, to
	}
	var lo, hi time.Time
	for _, se := range t.Series {
		for _, p := range se.Points {
			if p.At.IsZero() {
				continue
			}
			if lo.IsZero() || p.At.Before(lo) {
				lo = p.At
			}
			if hi.IsZero() || p.At.After(hi) {
				hi = p.At
			}
		}
	}
	if from.IsZero() {
		from = lo
	}
	if to.IsZero() {
		to = hi
	}
	return from, to
}

// trendRange returns the value range of the series that share the axis.
func trendRange(t Trend) (float64, float64, bool) {
	lo, hi := math.Inf(1), math.Inf(-1)
	found := false
	for _, se := range t.Series {
		if se.Scale != nil {
			continue
		}
		for _, p := range se.Points {
			lo, hi = min(lo, p.Value), max(hi, p.Value)
			found = true
		}
	}
	if !found {
		for _, se := range t.Series {
			if se.Scale != nil {
				return se.Scale.Min, se.Scale.Max, true
			}
		}
	}
	return lo, hi, found
}

// drawSeries draws one series.
func drawSeries(dst *image.Paletted, plot image.Rectangle, se Series, lo, hi float64,
	xAt func(time.Time) int, yAt func(float64, float64, float64) int, barGap int) {
	points := append([]Point(nil), se.Points...)
	sort.SliceStable(points, func(i, j int) bool { return points[i].At.Before(points[j].At) })
	if len(points) == 0 {
		return
	}
	width := se.Width
	if width < 1 {
		width = 2
	}
	switch se.Style {
	case Bars:
		if barGap < 0 {
			barGap = 0
		}
		if barGap == 0 {
			barGap = 1
		}
		for i, p := range points {
			x0 := xAt(p.At)
			x1 := x0 + max(plot.Dx()/max(len(points), 1), 2)
			if i+1 < len(points) {
				x1 = xAt(points[i+1].At)
			}
			w := max(x1-x0-barGap, 2)
			top := yAt(p.Value, lo, hi)
			fill := p.Fill
			if fill == "" {
				fill = se.Fill
			}
			if fill == "" {
				fill = "shade"
			}
			fillRect(dst, image.Rect(x0, top, x0+w, plot.Max.Y), fill)
		}
	case Area:
		for i := 0; i+1 < len(points); i++ {
			a, b := points[i], points[i+1]
			x0, x1 := xAt(a.At), xAt(b.At)
			if x1 <= x0 {
				continue
			}
			for x := max(x0, plot.Min.X); x < min(x1, plot.Max.X); x++ {
				f := float64(x-x0) / float64(x1-x0)
				v := a.Value + (b.Value-a.Value)*f
				top := yAt(v, lo, hi)
				fill := se.Fill
				if fill == "" {
					fill = "shade"
				}
				fillRect(dst, image.Rect(x, top, x+1, plot.Max.Y), fill)
			}
		}
	case Step:
		for i := 1; i < len(points); i++ {
			a, b := points[i-1], points[i]
			x0, x1 := xAt(a.At), xAt(b.At)
			y0, y1 := yAt(a.Value, lo, hi), yAt(b.Value, lo, hi)
			if se.Halo {
				colorSegment(dst, x0, y0, x1, y0, width+2, render.White)
				colorSegment(dst, x1, y0, x1, y1, width+2, render.White)
			}
			render.Segment(dst, x0, y0, x1, y0, width, false)
			render.Segment(dst, x1, y0, x1, y1, width, false)
		}
	default: // Line
		if len(points) == 1 {
			p := points[0]
			x, y := xAt(p.At), yAt(p.Value, lo, hi)
			fillRect(dst, image.Rect(x-width/2, y-width/2, x-width/2+width+1, y-width/2+width+1), "solid")
			return
		}
		if se.Halo {
			for i := 1; i < len(points); i++ {
				a, b := points[i-1], points[i]
				colorSegment(dst, xAt(a.At), yAt(a.Value, lo, hi), xAt(b.At), yAt(b.Value, lo, hi),
					width+2, render.White)
			}
		}
		for i := 1; i < len(points); i++ {
			a, b := points[i-1], points[i]
			render.Segment(dst, xAt(a.At), yAt(a.Value, lo, hi), xAt(b.At), yAt(b.Value, lo, hi),
				width, false)
		}
	}
}

// Sparkline draws a compact trend without axes or labels, for a tile or a
// hero.
func Sparkline(dst *image.Paletted, r image.Rectangle, values []float64, style SeriesStyle, fill string) bool {
	if len(values) == 0 || r.Empty() {
		return false
	}
	lo, hi := values[0], values[0]
	for _, v := range values {
		lo, hi = min(lo, v), max(hi, v)
	}
	if hi == lo {
		hi = lo + 1
	}
	yAt := func(v float64) int {
		f := (v - lo) / (hi - lo)
		return r.Max.Y - 1 - int(math.Round(f*float64(r.Dy()-1)))
	}
	if style == Bars {
		w := max(r.Dx()/len(values)-1, 1)
		for i, v := range values {
			x := r.Min.X + i*r.Dx()/len(values)
			if fill == "" {
				fill = "solid"
			}
			fillRect(dst, image.Rect(x, yAt(v), x+w, r.Max.Y), fill)
		}
		return true
	}
	xAt := func(i int) int {
		if len(values) == 1 {
			return r.Min.X + r.Dx()/2
		}
		return r.Min.X + i*(r.Dx()-1)/(len(values)-1)
	}
	for i := 1; i < len(values); i++ {
		render.Segment(dst, xAt(i-1), yAt(values[i-1]), xAt(i), yAt(values[i]), 2, false)
	}
	return true
}

// measureLegend returns the width of a legend or 0 when it does not fit.
func measureLegend(items []LegendItem, size float64, maxWidth int) int {
	square := int(math.Round(size * 1.1))
	gap := int(math.Round(size * 0.5))
	total := 0
	for _, it := range items {
		total += square + gap + render.Measure(it.Label, size, false) + gap
	}
	if total > maxWidth*3/5 {
		return 0
	}
	return total
}

// drawLegend draws a legend right-aligned at right and returns its left edge.
func drawLegend(dst *image.Paletted, r image.Rectangle, items []LegendItem, size float64, right int) int {
	width := measureLegend(items, size, r.Dx())
	if width == 0 {
		return 0
	}
	square := int(math.Round(size * 1.1))
	gap := int(math.Round(size * 0.5))
	x := right - width
	if x < r.Min.X {
		return 0
	}
	left := x
	base := r.Min.Y + int(math.Round(size*0.95))
	for _, it := range items {
		fill := it.Fill
		if fill == "" {
			fill = "shade"
		}
		fillRect(dst, image.Rect(x, base-square, x+square, base), fill)
		x += square + gap
		w := render.Measure(it.Label, size, false)
		render.TextAt(dst, it.Label, x, base, size, false, render.Black, render.Left)
		x += w + gap
	}
	return left
}

// defaultFormat formats a value for an axis label.
func defaultFormat(v float64) string {
	if v == math.Trunc(v) {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}

// colorSegment draws a line in one colour.
func colorSegment(dst *image.Paletted, x0, y0, x1, y1, thickness int, c color.Color) {
	if thickness < 1 {
		return
	}
	steps := max(absInt(x1-x0), absInt(y1-y0), 1)
	for i := 0; i <= steps; i++ {
		x := x0 + (x1-x0)*i/steps
		y := y0 + (y1-y0)*i/steps
		render.FillColor(dst, image.Rect(x, y, x+thickness, y+thickness), c)
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
