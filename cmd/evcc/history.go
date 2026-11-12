package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

// historySources are the groups of /api/history/energy the history widget
// shows. All but the loadpoints are fetched grouped, so evcc sums the devices
// of a group (its energy page does the same); a page picks one loadpoint's
// series by index.
var historySources = map[string]struct {
	group   string
	grouped bool
}{
	"pv":        {"pv", true},
	"battery":   {"battery", true},
	"grid":      {"grid", true},
	"home":      {"home", true},
	"loadpoint": {"loadpoint", false},
}

// historyOrder keeps the fetch order (and the first error) deterministic.
var historyOrder = []string{"pv", "battery", "grid", "home", "loadpoint"}

// dayValue is one day of the energy history, in kWh. evcc buckets days in its
// own timezone; the date is what the range comparisons use.
type dayValue struct {
	date         string // YYYY-MM-DD
	energy       float64
	returnEnergy float64 // battery discharge, grid export, otherwise 0
}

// historyDays is the key of one loadpoint series.
func loadpointKey(title string) string { return "loadpoint:" + title }

// fetchHistory reads the daily energy of every source: a year of day buckets
// answers all periods the widget shows (this year, last month, last week,
// today and yesterday) with a single request per group.
func (e *evcc) fetchHistory(ctx context.Context) (map[string][]dayValue, error) {
	now := time.Now()
	// 13 months and a week reach back past 1 January and the first of last
	// month in every calendar situation.
	from := now.AddDate(0, -13, -7)

	days := map[string][]dayValue{}
	var first error
	for _, source := range historyOrder {
		spec := historySources[source]
		series, err := e.fetchHistoryGroup(ctx, spec.group, spec.grouped, from, now)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if spec.grouped {
			days[source] = mergeDays(series)
			continue
		}
		for title, list := range series {
			days[loadpointKey(title)] = list
		}
	}
	if first != nil {
		return days, first
	}
	return days, nil
}

// fetchHistoryGroup reads one group; the answer is keyed by the series title.
func (e *evcc) fetchHistoryGroup(ctx context.Context, group string, grouped bool, from, to time.Time) (map[string][]dayValue, error) {
	q := url.Values{
		"from":      {from.Format(time.RFC3339)},
		"to":        {to.Format(time.RFC3339)},
		"aggregate": {"day"},
		"group":     {group},
	}
	if grouped {
		q.Set("grouped", "true")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.base+"/api/history/energy?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from /api/history/energy?group=%s", resp.StatusCode, group)
	}
	return parseHistory(body)
}

// historyAnswer is one series of the answer; evcc wraps the array in
// "result" in older versions.
type historyAnswer []struct {
	Title string `json:"title"`
	Group string `json:"group"`
	Data  []struct {
		Start        string  `json:"start"`
		Energy       float64 `json:"energy"`
		ReturnEnergy float64 `json:"returnEnergy"`
	} `json:"data"`
}

func parseHistory(body []byte) (map[string][]dayValue, error) {
	var wrapped struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil && len(wrapped.Result) > 0 && wrapped.Result[0] == '[' {
		body = wrapped.Result
	}
	var answer historyAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		return nil, fmt.Errorf("invalid /api/history/energy: %w", err)
	}
	out := map[string][]dayValue{}
	for _, s := range answer {
		title := s.Title
		if title == "" {
			title = s.Group
		}
		list := make([]dayValue, 0, len(s.Data))
		for _, d := range s.Data {
			t, err := time.Parse(time.RFC3339, d.Start)
			if err != nil {
				return nil, fmt.Errorf("invalid /api/history/energy: %w", err)
			}
			list = append(list, dayValue{date: t.Format("2006-01-02"), energy: d.Energy, returnEnergy: d.ReturnEnergy})
		}
		out[title] = list
	}
	return out, nil
}

// mergeDays adds the series of a group day by day, as evcc's grouped answer
// would.
func mergeDays(series map[string][]dayValue) []dayValue {
	sums := map[string]dayValue{}
	for _, list := range series {
		for _, d := range list {
			s := sums[d.date]
			s.date = d.date
			s.energy += d.energy
			s.returnEnergy += d.returnEnergy
			sums[d.date] = s
		}
	}
	out := make([]dayValue, 0, len(sums))
	for _, d := range sums {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].date < out[j].date })
	return out
}

// historyFingerprint covers the newest day of every series, which is the only
// bucket that moves between two refreshes.
func historyFingerprint(days map[string][]dayValue) string {
	keys := make([]string, 0, len(days))
	for k := range days {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		list := days[k]
		if len(list) == 0 {
			continue
		}
		last := list[len(list)-1]
		fmt.Fprintf(&b, "%s=%s:%.1f/%.1f;", k, last.date, last.energy, last.returnEnergy)
	}
	return b.String()
}

// watchHistory refreshes the energy history every history_refresh; state and
// history have their own loops because a year of buckets must not delay the
// state poll.
func (e *evcc) watchHistory(ctx context.Context, notify func(widgets ...string)) {
	var last string
	for {
		days, err := e.fetchHistory(ctx)
		e.mu.Lock()
		if err != nil {
			if e.histErr == nil || err.Error() != e.histErr.Error() {
				fmt.Fprintln(os.Stderr, "evcc history:", err)
			}
			e.histErr = err
			// A group that failed this time keeps the values of the last
			// successful read, like the other widgets serve stale data.
			for key, list := range e.hist {
				if _, ok := days[key]; !ok {
					days[key] = list
				}
			}
			e.hist = days
		} else {
			e.hist, e.histErr = days, nil
		}
		e.mu.Unlock()
		if err == nil {
			sum := historyFingerprint(days)
			if last != "" && sum != last {
				notify("history")
			}
			last = sum
		}
		select {
		case <-e.histReady:
		default:
			close(e.histReady)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(e.historyRefresh):
		}
	}
}

// historyData returns the last history and the error of the last refresh.
func (e *evcc) historyData() (map[string][]dayValue, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.hist, e.histErr
}

// historyRow is one period of the widget; from is inclusive, to exclusive.
type historyRow struct {
	label, short string
	from, to     string
}

func (e *evcc) historyRows() []historyRow {
	now := e.req.Now.In(e.req.Location())
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	// The week starts on Monday, like the calendar plugin does for German.
	monday := day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	year := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location())
	today, tomorrow := date(day), date(day.AddDate(0, 0, 1))
	return []historyRow{
		{e.t("Heute", "Today"), e.t("Heute", "Today"), today, tomorrow},
		{e.t("Gestern", "Yesterday"), e.t("Gestern", "Yesterday"), date(day.AddDate(0, 0, -1)), today},
		{e.t("Diese Woche", "This week"), e.t("Woche", "Week"), date(monday), tomorrow},
		{e.t("Letzte Woche", "Last week"), e.t("Vorwoche", "Last week"), date(monday.AddDate(0, 0, -7)), date(monday)},
		{e.t("Dieser Monat", "This month"), e.t("Monat", "Month"), date(month), tomorrow},
		{e.t("Letzter Monat", "Last month"), e.t("Vormonat", "Last month"), date(month.AddDate(0, -1, 0)), date(month)},
		{e.t("Dieses Jahr", "This year"), e.t("Jahr", "Year"), date(year), tomorrow},
	}
}

func date(t time.Time) string { return t.Format("2006-01-02") }

// historyHeading names the source and, for a battery or the grid, which of
// the two values the rows show.
func (e *evcc) historyHeading(source, metric string) string {
	switch source {
	case "pv":
		return e.t("PV-Ertrag", "PV yield")
	case "home":
		return e.t("Hausverbrauch", "Home consumption")
	case "battery":
		if metric == "return" {
			return e.t("Akku entladen", "Battery discharged")
		}
		return e.t("Akku laden", "Battery charged")
	case "grid":
		if metric == "return" {
			return e.t("Einspeisung", "Export")
		}
		return e.t("Netzbezug", "Grid import")
	}
	return source
}

// historyValues resolves the widget's source, its heading and the daily values
// of the last refresh.
func (e *evcc) historyValues(w plugin.WidgetRequest) (string, string, []dayValue, error) {
	source := w.Arg().String("source", "pv")
	metric := w.Arg().String("metric", "energy")
	if _, ok := historySources[source]; !ok {
		return "", "", nil, fmt.Errorf("%w %q for the history widget", plugin.ErrUnknownWidget, source)
	}
	if metric == "return" && source != "battery" && source != "grid" {
		return "", "", nil, errors.New("metric: return is only available for battery and grid")
	}
	heading := e.historyHeading(source, metric)
	key := source
	if source == "loadpoint" {
		lp, err := e.loadpoint(w)
		if err != nil {
			return "", "", nil, err
		}
		key, heading = loadpointKey(lp.Title), lp.Title
	}
	days, histErr := e.historyData()
	if days == nil {
		if histErr != nil {
			return "", "", nil, fmt.Errorf("evcc history: %w", histErr)
		}
		return "", "", nil, errors.New("evcc history is still loading")
	}
	list, ok := days[key]
	if !ok {
		if histErr != nil {
			return "", "", nil, fmt.Errorf("evcc history: %w", histErr)
		}
		return "", "", nil, fmt.Errorf("evcc has no history for %s", heading)
	}
	return metric, heading, list, nil
}

// sum adds the rows of a period.
func (e *evcc) historySum(list []dayValue, row historyRow, metric string) float64 {
	var sum float64
	for _, d := range list {
		if d.date < row.from || d.date >= row.to {
			continue
		}
		if metric == "return" {
			sum += d.returnEnergy
		} else {
			sum += d.energy
		}
	}
	return sum
}

// kwh formats a value of the history: one decimal below 100 kWh, none above.
func (e *evcc) kwh(v float64) string {
	decimals := 1
	if v >= 100 {
		decimals = 0
	}
	return e.num(v, decimals) + " kWh"
}

// historyWidget draws the heading and the periods: label left, value right,
// thin rules between the rows. Rows of the long periods drop first when the
// cell is too small.
func (e *evcc) historyWidget(w plugin.WidgetRequest, img *image.Paletted) error {
	metric, heading, list, err := e.historyValues(w)
	if err != nil {
		return err
	}
	b := img.Rect.Inset(w.Style.Padding)
	if b.Empty() {
		return nil
	}
	base := e.req.TextPx(w)
	rule := max(w.Style.Rule, 1)

	head := min(base*0.9, float64(b.Dy())*0.16)
	y := b.Min.Y + int(head)
	render.Text(img, heading, b.Min.X, y, render.Style{Size: head, Bold: true, Shrink: true}, b.Dx(), color.Black, render.Left)
	top := y + int(head*0.5)

	rows := e.historyRows()
	n := len(rows)
	for n > 1 && float64(b.Max.Y-top)/float64(n) < base*1.35 {
		n--
	}
	rowH := float64(b.Max.Y-top) / float64(n)
	label := render.Style{Size: min(rowH*0.55, base*0.95), Shrink: true}
	value := render.Style{Size: min(rowH*0.62, base*1.05), Bold: true, Shrink: true}
	gap := max(w.Style.Padding, 4)
	for i := 0; i < n; i++ {
		row := rows[i]
		rowY := top + int(float64(i)*rowH)
		baseline := rowY + int(rowH*0.72)
		text := e.kwh(e.historySum(list, row, metric))
		valueW := render.Measure(text, value.Size, true)
		render.Text(img, row.label, b.Min.X, baseline, label, b.Dx()-valueW-gap, color.Black, render.Left)
		render.Text(img, text, b.Max.X, baseline, value, valueW, color.Black, render.Right)
		if i > 0 {
			render.HLine(img, b.Min.X, rowY, b.Max.X, rule)
		}
	}
	return nil
}

// historyText puts one period per line, with shorter labels so that a 20
// column display keeps both label and value.
func (e *evcc) historyText(w plugin.WidgetRequest) ([]string, error) {
	metric, heading, list, err := e.historyValues(w)
	if err != nil {
		return nil, err
	}
	var lines []string
	if w.Height >= 5 {
		lines = append(lines, heading)
	}
	rows := e.historyRows()
	for i := 0; i < len(rows) && len(lines) < w.Height; i++ {
		lines = append(lines, plugin.Columns(rows[i].short, e.kwh(e.historySum(list, rows[i], metric)), w.Width))
	}
	return lines, nil
}
