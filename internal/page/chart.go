package page

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"time"
)

const (
	chartDefaultPadding = 4
	chartGapLimit       = 8 // px between columns that still connect a line
	chartAxisThickness  = 2
)

// chart draws the series of a chart block (ADR 0034, section 5). With time the
// x axis runs from local midnight to midnight, otherwise the samples are spread
// over the region. Areas are filled first, then lines, each group in series
// order.
func (r *renderer) chart(img *image.Paletted, rc image.Rectangle, c *Chart, padding *int) {
	pad := chartDefaultPadding
	if padding != nil {
		pad = max(*padding, 0)
	}
	inner := image.Rect(rc.Min.X+pad, rc.Min.Y+pad, rc.Max.X-pad, rc.Max.Y-pad)
	if inner.Dx() < 8 || inner.Dy() < 8 {
		return
	}

	loc := r.f.location()
	var start, end time.Time
	if c.Time {
		now := r.now.In(loc)
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		end = start.AddDate(0, 0, 1)
	}

	points, _ := lookupList(r.root, c.Points)
	usable, maxV := chartStats(points, c, start, end, loc)
	if usable < 2 {
		r.chartNotice(img, rc)
		return
	}

	fontSize := math.Max(6, math.Min(float64(rc.Dy())*0.09, float64(rc.Dx())/30))
	ymax := chartYMax(c, maxV)

	title := r.expand(c.Title)
	legend := ""
	if c.Legend {
		labels := make([]string, 0, len(c.Series))
		for _, s := range c.Series {
			label := r.expand(s.Label)
			if label == "" {
				label = s.Key
			}
			labels = append(labels, label)
		}
		legend = joinLabels(labels)
	}

	contentTop := inner.Min.Y
	if title != "" || legend != "" {
		baseline := inner.Min.Y + int(fontSize)
		if title != "" {
			maxWidth := inner.Dx()
			if legend != "" {
				maxWidth = inner.Dx() * 2 / 3
			}
			drawStyled(img, title, inner.Min.X, baseline,
				textStyle{Size: fontSize, Shrink: true}, maxWidth, color.Black, alignLeft)
		}
		if legend != "" {
			drawStyled(img, legend, inner.Max.X, baseline,
				textStyle{Size: fontSize, Shrink: true}, inner.Dx()/2, color.Black, alignRight)
		}
		contentTop = inner.Min.Y + int(fontSize) + 2
	}

	left, top, right, bottom := inner.Min.X, contentTop, inner.Max.X, inner.Max.Y
	if c.Axis {
		labelWidth := measure(r.chartLabel(c, ymax), fontSize, false)
		if w := measure("0", fontSize, false); w > labelWidth {
			labelWidth = w
		}
		left += labelWidth + 4
		bottom -= int(fontSize) + 4
	}
	if right-left < 4 || bottom-top < 4 {
		return
	}

	// Bucket the samples into pixel columns before drawing. A missing value or
	// an invalid time leaves the column empty, so outages stay visible.
	samples := make([][]chartPoint, len(c.Series))
	for i, s := range c.Series {
		if c.Time {
			samples[i] = chartTimeSamples(points, s.Key, left, right-left, start, end)
		} else {
			samples[i] = chartIndexSamples(points, s.Key, left, right-left)
		}
		for j := range samples[i] {
			samples[i][j].y = chartY(samples[i][j].v, ymax, top, bottom)
		}
	}

	if c.Axis {
		r.chartAxis(img, c, inner, left, top, right, bottom, fontSize, ymax, start, end)
	}

	for pass := 0; pass < 2; pass++ {
		for i, s := range c.Series {
			if chartIsArea(s) != (pass == 0) {
				continue
			}
			if pass == 0 {
				drawChartArea(img, samples[i], c.Time, chartFill(s), bottom)
			} else {
				drawChartLine(img, samples[i], c.Time, chartWeight(s))
			}
		}
	}

	// The marker goes on top so that it stays visible over filled areas.
	if c.Now && c.Time {
		now := r.now.In(loc)
		if !now.Before(start) && now.Before(end) {
			frac := float64(now.Sub(start)) / float64(end.Sub(start))
			x := left + int(frac*float64(right-left))
			if x >= right {
				x = right - 1
			}
			segment(img, x, top, x, max(bottom-1, top), 1, true)
		}
	}
}

// chartNotice shows the language-specific notice when there are not enough
// usable samples.
func (r *renderer) chartNotice(img *image.Paletted, rc image.Rectangle) {
	drawStyled(img, text(r.language, "keine Verlaufsdaten", "no chart data"),
		rc.Min.X+rc.Dx()/2, rc.Min.Y+rc.Dy()/2,
		textStyle{Size: float64(rc.Dy()) / 8, Bold: false, Shrink: true},
		rc.Dx()-8, color.Black, alignCenter)
}

// chartAxis draws the left and bottom rules together with the y and time labels.
func (r *renderer) chartAxis(img *image.Paletted, c *Chart, inner image.Rectangle,
	left, top, right, bottom int, fontSize float64, ymax float64, start, end time.Time) {
	vline(img, left, top, bottom+chartAxisThickness, chartAxisThickness)
	hline(img, left, bottom, right, chartAxisThickness)

	labelStyle := textStyle{Size: fontSize, Shrink: true}
	width := left - 4 - inner.Min.X
	drawStyled(img, r.chartLabel(c, ymax), left-4, top+int(fontSize*0.8),
		labelStyle, width, color.Black, alignRight)
	drawStyled(img, "0", left-4, bottom,
		labelStyle, width, color.Black, alignRight)

	if !c.Time {
		return
	}
	ticks := time.Duration(c.Ticks)
	if ticks <= 0 {
		ticks = 6 * time.Hour
	}
	baseline := bottom + 2 + int(fontSize)
	lastRight := inner.Min.X - 1
	span := float64(end.Sub(start))
	for t := start; !t.After(end); t = t.Add(ticks) {
		minutes := int(t.Sub(start) / time.Minute)
		frac := float64(t.Sub(start)) / span
		x := left + int(frac*float64(right-left))
		if x > right {
			x = right
		}
		if x >= inner.Min.X && x <= inner.Max.X {
			vline(img, x, bottom, bottom+int(fontSize/2), chartAxisThickness)
		}
		label := chartTickLabel(minutes)
		w := measure(label, fontSize, false)
		ax, align := x, alignCenter
		if x-w/2 < inner.Min.X {
			ax, align = inner.Min.X, alignLeft
		}
		if x+w/2 > inner.Max.X {
			ax, align = inner.Max.X, alignRight
		}
		leftEdge := ax
		switch align {
		case alignCenter:
			leftEdge = ax - w/2
		case alignRight:
			leftEdge = ax - w
		}
		if leftEdge < lastRight+2 {
			continue
		}
		drawStyled(img, label, ax, baseline, textStyle{Size: fontSize, Shrink: false},
			inner.Dx(), color.Black, align)
		lastRight = leftEdge + w
	}
}

// chartTickLabel formats a tick of the time axis as hours since midnight.
func chartTickLabel(minutes int) string {
	if minutes%60 == 0 {
		return fmt.Sprintf("%d:00", minutes/60)
	}
	return fmt.Sprintf("%d:%02d", minutes/60, minutes%60)
}

// chartLabel formats a y value with the chart unit. Axis labels use whole
// numbers where possible, matching the reference pages ("5 kW", not
// "5,00 kW").
func (r *renderer) chartLabel(c *Chart, v float64) string {
	switch c.Unit {
	case "kW":
		kw := v / 1000
		decimals := 0
		if kw < 10 && kw != math.Trunc(kw) {
			decimals = 1
		}
		return r.f.num(kw, decimals) + " kW"
	case "W":
		return r.f.num(v, 0) + " W"
	case "kWh":
		return r.f.num(v, 1) + " kWh"
	case "Wh":
		return r.f.num(v, 0) + " Wh"
	case "%":
		return r.f.num(v, 0) + " %"
	case "°C":
		return r.f.num(v, 0) + " °C"
	}
	label := r.f.trim(v)
	if c.Unit != "" {
		label += " " + c.Unit
	}
	return label
}

// chartYMax returns the explicit maximum or rounds the data maximum up to a
// readable value. The result is always positive.
func chartYMax(c *Chart, maxV float64) float64 {
	if c.YMax > 0 {
		return float64(c.YMax)
	}
	if maxV < 0 {
		maxV = 0
	}
	switch unitFormat(c.Unit) {
	case "power":
		return math.Max(1000, math.Ceil(maxV/1000-1e-9)*1000)
	case "percent":
		return math.Max(10, math.Ceil(maxV/10-1e-9)*10)
	case "temp":
		return math.Max(5, math.Ceil(maxV/5-1e-9)*5)
	case "energy":
		step := 1.0
		if maxV < 1 {
			step = 0.1
		}
		return math.Max(step, math.Ceil(maxV/step-1e-9)*step)
	}
	if maxV <= 0 {
		return 1
	}
	exp := math.Ceil(math.Log10(maxV))
	if exp < 0 {
		exp = 0
	}
	return math.Pow(10, exp)
}

// chartY maps a value to a pixel row, clamped to the plot area.
func chartY(v, ymax float64, top, bottom int) int {
	if ymax <= 0 || bottom <= top {
		return bottom
	}
	y := bottom - int(math.Round(v/ymax*float64(bottom-top)))
	if y < top {
		return top
	}
	if y > bottom {
		return bottom
	}
	return y
}

// chartStats counts points that can be plotted and returns the largest value
// of all series. In time mode points outside the day or without a valid time
// do not count.
func chartStats(points []any, c *Chart, start, end time.Time, loc *time.Location) (int, float64) {
	usable := 0
	maxV := 0.0
	for _, p := range points {
		if c.Time {
			t, ok := pointTime(p)
			if !ok {
				continue
			}
			if t.In(loc).Before(start) || !t.In(loc).Before(end) {
				continue
			}
		}
		found := false
		for _, s := range c.Series {
			v, ok := seriesValue(p, s.Key)
			if !ok || math.IsNaN(v) {
				continue
			}
			found = true
			if v > maxV {
				maxV = v
			}
		}
		if found {
			usable++
		}
	}
	return usable, maxV
}

// pointTime reads the RFC 3339 timestamp of a point.
func pointTime(p any) (time.Time, bool) {
	m, ok := p.(map[string]any)
	if !ok {
		return time.Time{}, false
	}
	switch v := m["time"].(type) {
	case time.Time:
		return v, true
	case string:
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return time.Time{}, false
		}
		return t, true
	}
	return time.Time{}, false
}

// chartPoint is one plotted sample in pixel coordinates.
type chartPoint struct {
	x, y int
	v    float64 // value, mapped to y once the axis is laid out
	idx  int     // pixel column (time) or sample index, used to detect gaps
}

// chartTimeSamples buckets the samples into pixel columns from local midnight
// to midnight and averages the values per column.
func chartTimeSamples(points []any, key string, x0, width int, start, end time.Time) []chartPoint {
	if width < 1 {
		return nil
	}
	sums := make([]float64, width)
	counts := make([]int, width)
	span := float64(end.Sub(start))
	if span <= 0 {
		return nil
	}
	loc := start.Location()
	for _, p := range points {
		t, ok := pointTime(p)
		if !ok {
			continue
		}
		local := t.In(loc)
		if local.Before(start) || !local.Before(end) {
			continue
		}
		v, ok := seriesValue(p, key)
		if !ok || math.IsNaN(v) {
			continue
		}
		col := int(float64(local.Sub(start)) / span * float64(width))
		if col < 0 {
			col = 0
		}
		if col >= width {
			col = width - 1
		}
		sums[col] += v
		counts[col]++
	}
	var out []chartPoint
	for col := 0; col < width; col++ {
		if counts[col] == 0 {
			continue
		}
		out = append(out, chartPoint{x: x0 + col, idx: col, v: sums[col] / float64(counts[col])})
	}
	return out
}

// chartIndexSamples spreads the samples evenly over the chart width and keeps
// missing values as gaps.
func chartIndexSamples(points []any, key string, x0, width int) []chartPoint {
	var out []chartPoint
	for i, p := range points {
		v, ok := seriesValue(p, key)
		if !ok {
			continue
		}
		x := x0
		if len(points) > 1 {
			x = x0 + (width-1)*i/(len(points)-1)
		}
		out = append(out, chartPoint{x: x, v: v, idx: i})
	}
	return out
}

// chartConnected reports whether two samples may be joined. In time mode only
// neighbouring columns within chartGapLimit are connected; in index mode only
// directly adjacent samples.
func chartConnected(a, b chartPoint, timeAxis bool) bool {
	if timeAxis {
		return b.idx > a.idx && b.idx-a.idx <= chartGapLimit
	}
	return b.idx == a.idx+1
}

// drawChartArea fills each series from the sample down to the baseline.
func drawChartArea(img *image.Paletted, pts []chartPoint, timeAxis bool, fill string, baseline int) {
	for i, p := range pts {
		if i == 0 || !chartConnected(pts[i-1], p, timeAxis) {
			fillRect(img, image.Rect(p.x, p.y, p.x+1, baseline), fill)
			continue
		}
		prev := pts[i-1]
		for x := prev.x; x <= p.x; x++ {
			y := chartInterpolate(prev, p, x)
			fillRect(img, image.Rect(x, y, x+1, baseline), fill)
		}
	}
}

// drawChartLine draws a line of weight pixels. A sample without a neighbour is
// marked with a dot so that an outage remains visible.
func drawChartLine(img *image.Paletted, pts []chartPoint, timeAxis bool, weight int) {
	weight = max(weight, 1)
	for i := 1; i < len(pts); i++ {
		if chartConnected(pts[i-1], pts[i], timeAxis) {
			segment(img, pts[i-1].x, pts[i-1].y, pts[i].x, pts[i].y, weight, false)
		}
	}
	for i, p := range pts {
		if i > 0 && chartConnected(pts[i-1], p, timeAxis) {
			continue
		}
		fillSolid(img, image.Rect(p.x, p.y, p.x+weight, p.y+weight))
	}
}

// chartInterpolate returns the y value between two samples at column x.
func chartInterpolate(a, b chartPoint, x int) int {
	if b.x == a.x {
		return b.y
	}
	return a.y + (b.y-a.y)*(x-a.x)/(b.x-a.x)
}

func chartIsArea(s Series) bool { return s.Style == "area" }

func chartFill(s Series) string {
	if s.Fill == "" {
		return "shade"
	}
	return s.Fill
}

func chartWeight(s Series) int { return max(s.Weight, 1) }

func joinLabels(labels []string) string {
	out := ""
	for _, l := range labels {
		if l == "" {
			continue
		}
		if out != "" {
			out += " - "
		}
		out += l
	}
	return out
}
