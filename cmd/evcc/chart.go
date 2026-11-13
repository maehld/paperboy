package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

var (
	monthShortDE = []string{"Jan", "Feb", "Mär", "Apr", "Mai", "Jun", "Jul", "Aug", "Sep", "Okt", "Nov", "Dez"}
	monthLongDE  = []string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"}
	monthShortEN = []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	monthLongEN  = []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	weekdayDE    = []string{"Mo", "Di", "Mi", "Do", "Fr", "Sa", "So"}
	weekdayEN    = []string{"Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"}
)

func (e *evcc) monthShort(m time.Month) string {
	if e.lang == "de" {
		return monthShortDE[m-1]
	}
	return monthShortEN[m-1]
}

func (e *evcc) monthLong(m time.Month) string {
	if e.lang == "de" {
		return monthLongDE[m-1]
	}
	return monthLongEN[m-1]
}

func (e *evcc) weekday(i int) string {
	if e.lang == "de" {
		return weekdayDE[i]
	}
	return weekdayEN[i]
}

// chartBar is one bar: its label under the axis and its value in kWh.
type chartBar struct {
	label string
	value float64
}

// chartBars puts the buckets of the history into the bars of one period: hours
// for a day, days for a week or a month, months for a year.
func (e *evcc) chartBars(period string, list []energyPoint, metric string) ([]chartBar, string, error) {
	loc := e.req.Location()
	now := e.req.Now.In(loc)
	hourly := period == "today" || period == "yesterday"
	sum := map[string]float64{}
	for _, d := range list {
		key := d.date()
		if hourly {
			key = d.at.In(loc).Format("2006-01-02T15")
		}
		if metric == "return" {
			sum[key] += d.returnEnergy
		} else {
			sum[key] += d.energy
		}
	}
	day := func(t time.Time) string { return t.Format("2006-01-02") }
	midnight := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, loc) }

	switch period {
	case "today", "yesterday":
		start := midnight(now.Year(), now.Month(), now.Day())
		suffix := e.t("heute", "today")
		if period == "yesterday" {
			start, suffix = start.AddDate(0, 0, -1), e.t("gestern", "yesterday")
		}
		bars := make([]chartBar, 24)
		for i := range bars {
			label := ""
			if i%3 == 0 {
				label = strconv.Itoa(i)
			}
			bars[i] = chartBar{label: label, value: sum[start.Add(time.Duration(i)*time.Hour).Format("2006-01-02T15")]}
		}
		return bars, suffix, nil

	case "week", "last_week":
		monday := midnight(now.Year(), now.Month(), now.Day()).AddDate(0, 0, -((int(now.Weekday()) + 6) % 7))
		if period == "last_week" {
			monday = monday.AddDate(0, 0, -7)
		}
		bars := make([]chartBar, 7)
		for i := range bars {
			bars[i] = chartBar{label: e.weekday(i), value: sum[day(monday.AddDate(0, 0, i))]}
		}
		_, week := monday.ISOWeek()
		suffix := fmt.Sprintf("KW %d", week)
		if e.lang != "de" {
			suffix = fmt.Sprintf("week %d", week)
		}
		return bars, suffix, nil

	case "month", "last_month":
		first := midnight(now.Year(), now.Month(), 1)
		if period == "last_month" {
			first = first.AddDate(0, -1, 0)
		}
		next := first.AddDate(0, 1, 0)
		days := int(next.Sub(first).Hours() / 24)
		bars := make([]chartBar, days)
		for i := range bars {
			label := ""
			d := i + 1
			if d == 1 || d%5 == 0 {
				label = strconv.Itoa(d)
			}
			bars[i] = chartBar{label: label, value: sum[day(first.AddDate(0, 0, i))]}
		}
		return bars, fmt.Sprintf("%s %d", e.monthLong(first.Month()), first.Year()), nil

	case "year", "last_year":
		year := now.Year()
		if period == "last_year" {
			year--
		}
		bars := make([]chartBar, 12)
		for i := range bars {
			bars[i] = chartBar{label: e.monthShort(time.Month(i + 1))}
		}
		for date, v := range sum {
			t, err := time.Parse("2006-01-02", date)
			if err != nil || t.Year() != year {
				continue
			}
			bars[int(t.Month())-1].value += v
		}
		return bars, strconv.Itoa(year), nil
	}
	return nil, "", fmt.Errorf("unknown period %q", period)
}

// chartWidget draws one series as bars: month names for a year, day numbers
// for a month and weekdays for a week. The heading names the series and the
// period, the value at the top right is the sum of the bars.
func (e *evcc) chartWidget(w plugin.WidgetRequest, img *image.Paletted) error {
	period := w.Arg().String("period", "year")
	hourly := period == "today" || period == "yesterday"
	metric, heading, list, err := e.historyValues(w, hourly)
	if err != nil {
		return err
	}
	bars, suffix, err := e.chartBars(period, list, metric)
	if err != nil {
		return err
	}
	b := img.Rect.Inset(w.Style.Padding)
	if b.Empty() {
		return plugin.TooSmall(w)
	}
	base := e.req.TextPx(w)
	head := min(base*0.9, float64(b.Dy())*0.17)
	title := heading + " · " + suffix
	total := 0.0
	for _, bar := range bars {
		total += bar.value
	}
	totalText := e.kwh(total)
	totalW := render.Measure(totalText, head, true)
	render.Text(img, title, b.Min.X, b.Min.Y+int(head), render.Style{Size: head, Bold: true, Shrink: true},
		b.Dx()-totalW-max(w.Style.Padding, 4), color.Black, render.Left)
	render.Text(img, totalText, b.Max.X, b.Min.Y+int(head), render.Style{Size: head, Bold: true, Shrink: true},
		totalW+int(base), color.Black, render.Right)

	labelSize := max(min(head*0.62, float64(b.Dy())*0.11), render.MinSize)
	xH := int(labelSize * 1.7)
	// The axis labels stand to the right of the plot, like evcc's.
	maxValue := 0.0
	for _, bar := range bars {
		maxValue = max(maxValue, bar.value)
	}
	step := niceStep(maxValue / 3)
	top := math.Ceil(maxValue/step) * step
	axisW := 0
	if top > 0 {
		axisW = render.Measure(e.kwh(top), labelSize, false) + max(w.Style.Padding, 4)
	}
	left, right := b.Min.X, b.Max.X-axisW
	plotTop, bottom := b.Min.Y+int(head*1.9), b.Max.Y-xH
	if right-left < 40 || bottom-plotTop < 30 {
		return nil
	}

	// Grid lines and their values.
	if top > 0 {
		for v := 0.0; v <= top+step/2; v += step {
			y := bottom - int(v/top*float64(bottom-plotTop))
			if v == 0 {
				render.HLine(img, left, y, right, max(w.Style.Rule, 1))
			} else {
				render.HLine(img, left, y, right, 1)
			}
			render.Text(img, e.tick(v, step), right+max(w.Style.Padding, 4), y+int(labelSize*0.35),
				render.Style{Size: labelSize}, axisW, color.Black, render.Left)
		}
	}
	render.HLine(img, left, bottom, right, max(w.Style.Rule, 1))

	// Bars and their labels: a label that would touch the one before it is
	// left out, so the month names of a narrow chart become every other one.
	slot := float64(right-left) / float64(len(bars))
	barW := max(int(slot*0.62), 2)
	lastLabel := math.Inf(-1)
	for i, bar := range bars {
		x := left + int(float64(i)*slot) + (int(slot)-barW)/2
		if top > 0 && bar.value > 0 {
			h := int(bar.value / top * float64(bottom-plotTop))
			render.Fill(img, image.Rect(x, bottom-h, x+barW, bottom), "solid")
		}
		if bar.label == "" {
			continue
		}
		labelW := float64(render.Measure(bar.label, labelSize, false))
		center := left + int(float64(i)*slot+slot/2)
		if float64(center)-labelW/2 < lastLabel+labelSize {
			continue
		}
		render.Text(img, bar.label, center, bottom+xH-int(labelSize*0.5),
			render.Style{Size: labelSize}, int(slot), color.Black, render.Center)
		lastLabel = float64(center) + labelW/2
	}
	return nil
}

// tick formats an axis value: whole numbers with a thousand separator, one
// decimal when the step is smaller than 1 kWh.
func (e *evcc) tick(v, step float64) string {
	switch {
	case v == 0:
		return "0"
	case step < 1:
		return e.num(v, 1)
	}
	return e.grouped(v)
}

// grouped formats a number with thousand separators: 5798 -> "5.798" (de),
// "5,798" (en).
func (e *evcc) grouped(v float64) string {
	s := strconv.FormatFloat(math.Round(v), 'f', 0, 64)
	sep := ","
	if e.lang == "de" {
		sep = "."
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + sep + s[i:]
	}
	return s
}

// niceStep rounds a step up to 1, 2 or 5 times a power of ten.
func niceStep(v float64) float64 {
	if v <= 0 {
		return 1
	}
	p := math.Pow(10, math.Floor(math.Log10(v)))
	for _, m := range []float64{1, 2, 5} {
		if v <= m*p {
			return m * p
		}
	}
	return 10 * p
}
