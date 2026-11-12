package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

// historyDays is the PV answer of the test server: one year around 12
// November 2026 that covers every period of the widget.
var historyDays = map[string][2]float64{ // date -> energy, returnEnergy
	"2025-12-31": {11, 0},
	"2026-01-01": {9, 0},
	"2026-10-15": {7, 0},
	"2026-11-01": {2, 0},
	"2026-11-02": {3, 0},
	"2026-11-08": {6, 0},
	"2026-11-09": {1, 0},
	"2026-11-11": {4, 0},
	"2026-11-12": {5, 0},
}

var batteryDays = map[string][2]float64{
	"2026-11-11": {2, 3},
	"2026-11-12": {1, 4},
}

var gridDays = map[string][2]float64{
	"2026-11-11": {8, 1},
	"2026-11-12": {10, 2},
}

var homeDays = map[string][2]float64{
	"2026-11-12": {20, 0},
}

var garageDays = map[string][2]float64{
	"2026-11-12": {1.5, 0},
}

// oldLoadpoint has a series in the history but no loadpoint in /api/state.
var oldLoadpointDays = map[string][2]float64{
	"2026-11-12": {99, 0},
}

// writeHistoryFixture answers like /api/history/energy, keyed by group.
func writeHistoryFixture(w http.ResponseWriter, r *http.Request) {
	type bucket struct {
		Start        string  `json:"start"`
		Energy       float64 `json:"energy"`
		ReturnEnergy float64 `json:"returnEnergy"`
	}
	type series struct {
		Title string   `json:"title"`
		Group string   `json:"group"`
		Data  []bucket `json:"data"`
	}
	mk := func(title, group string, days map[string][2]float64) series {
		dates := make([]string, 0, len(days))
		for d := range days {
			dates = append(dates, d)
		}
		sort.Strings(dates)
		s := series{Title: title, Group: group}
		for _, d := range dates {
			s.Data = append(s.Data, bucket{
				Start:        d + "T00:00:00Z",
				Energy:       days[d][0],
				ReturnEnergy: days[d][1],
			})
		}
		return s
	}
	var out []series
	switch r.URL.Query().Get("group") {
	case "pv":
		out = []series{mk("pv", "pv", historyDays)}
	case "battery":
		out = []series{mk("Akku", "battery", batteryDays)}
	case "grid":
		out = []series{mk("grid", "grid", gridDays)}
	case "home":
		out = []series{mk("home", "home", homeDays)}
	case "loadpoint":
		out = []series{
			mk("Garage", "loadpoint", garageDays),
			mk("Old", "loadpoint", oldLoadpointDays),
		}
	}
	if err := json.NewEncoder(w).Encode(out); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// newHistoryPlugin starts a plugin against a server with state and history.
func newHistoryPlugin(t *testing.T) (*evcc, *plugin.Request) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/state":
			w.Write([]byte(stateJSON))
		case "/api/history/energy":
			writeHistoryFixture(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	e := &evcc{client: srv.Client()}
	req := &plugin.Request{Language: "de", Timezone: "UTC", Now: time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC),
		Page: plugin.PageInfo{Height: 480}}
	if err := e.Configure(plugin.Values{"url": srv.URL}, req); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e.Start(ctx, func(...string) {})
	if err := e.Prepare(req); err != nil {
		t.Fatal(err)
	}
	return e, req
}

func historyText(t *testing.T, e *evcc, req *plugin.Request, height int, args map[string]any) []string {
	t.Helper()
	lines, err := e.Text(req, plugin.WidgetRequest{Widget: "history", Width: 20, Height: height, Args: args})
	if err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return lines
}

func line(label, value string) string { return plugin.Columns(label, value, 20) }

func TestHistoryText(t *testing.T) {
	e, req := newHistoryPlugin(t)
	// Four rows fit without a heading: today, yesterday, this week and last
	// week (12 November 2026 is a Thursday, the week starts on Monday).
	want := []string{
		line("Heute", "5,0 kWh"),
		line("Gestern", "4,0 kWh"),
		line("Woche", "10,0 kWh"),
		line("Vorwoche", "9,0 kWh"),
	}
	if got := historyText(t, e, req, 4, nil); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("4 rows:\n%q\nwant\n%q", got, want)
	}

	// A taller display gets the heading and all seven periods.
	got := historyText(t, e, req, 8, nil)
	want = []string{
		"PV-Ertrag",
		line("Heute", "5,0 kWh"),
		line("Gestern", "4,0 kWh"),
		line("Woche", "10,0 kWh"),
		line("Vorwoche", "9,0 kWh"),
		line("Monat", "21,0 kWh"),
		line("Vormonat", "7,0 kWh"),
		line("Jahr", "37,0 kWh"),
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("8 rows:\n%q\nwant\n%q", got, want)
	}
}

func TestHistorySources(t *testing.T) {
	e, req := newHistoryPlugin(t)
	cases := []struct {
		args  map[string]any
		head  string
		today string
	}{
		{map[string]any{"source": "battery"}, "Akku laden", "1,0 kWh"},
		{map[string]any{"source": "battery", "metric": "return"}, "Akku entladen", "4,0 kWh"},
		{map[string]any{"source": "grid"}, "Netzbezug", "10,0 kWh"},
		{map[string]any{"source": "grid", "metric": "return"}, "Einspeisung", "2,0 kWh"},
		{map[string]any{"source": "home"}, "Hausverbrauch", "20,0 kWh"},
		{map[string]any{"source": "loadpoint"}, "Garage", "1,5 kWh"},
	}
	for _, c := range cases {
		got := historyText(t, e, req, 5, c.args)
		if len(got) != 5 || got[0] != c.head || got[1] != line("Heute", c.today) {
			t.Errorf("%v: %q", c.args, got)
		}
	}
}

func TestHistoryErrors(t *testing.T) {
	e, req := newHistoryPlugin(t)
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"metric": "return"}, "only available for battery and grid"},
		{map[string]any{"source": "nonsense"}, "unknown widget"},
		{map[string]any{"source": "loadpoint", "index": 3}, "loadpoint 3"},
	} {
		_, err := e.Text(req, plugin.WidgetRequest{Widget: "history", Width: 20, Height: 4, Args: c.args})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want %q", c.args, err, c.want)
		}
	}
}

func TestHistoryWithoutEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/state" {
			w.Write([]byte(stateJSON))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	e := &evcc{client: srv.Client()}
	req := &plugin.Request{Language: "de", Timezone: "UTC", Now: time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC),
		Page: plugin.PageInfo{Height: 480}}
	if err := e.Configure(plugin.Values{"url": srv.URL}, req); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e.Start(ctx, func(...string) {})
	if err := e.Prepare(req); err != nil {
		t.Fatal(err)
	}
	// An evcc without the history database leaves the other widgets alone.
	img := render.NewCanvas(400, 200, 4)
	if err := e.Render(req, plugin.WidgetRequest{Widget: "site", Width: 400, Height: 200,
		Style: plugin.Style{Padding: 8, Rule: 1, TextPx: 28.8}}, img); err != nil || !hasInk(img) {
		t.Fatalf("site: %v", err)
	}
	_, err := e.Text(req, plugin.WidgetRequest{Widget: "history", Width: 20, Height: 4})
	if err == nil || !strings.Contains(err.Error(), "evcc history: HTTP 404") {
		t.Fatalf("history: %v", err)
	}
}

func TestHistoryImage(t *testing.T) {
	e, req := newHistoryPlugin(t)
	img := render.NewCanvas(400, 240, 4)
	style := plugin.Style{Padding: 8, Rule: 1, TextPx: 28.8}
	if err := e.Render(req, plugin.WidgetRequest{Widget: "history", Width: 400, Height: 240, Style: style}, img); err != nil {
		t.Fatal(err)
	}
	if !hasInk(img) {
		t.Error("empty widget")
	}
	// A cell that cannot hold a row stays empty but does not fail.
	img = render.NewCanvas(10, 10, 2)
	if err := e.Render(req, plugin.WidgetRequest{Widget: "history", Width: 10, Height: 10}, img); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryRequests(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/state":
			w.Write([]byte(stateJSON))
		case "/api/history/energy":
			queries = append(queries, r.URL.RawQuery)
			writeHistoryFixture(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	e := &evcc{client: srv.Client()}
	req := &plugin.Request{Language: "de", Timezone: "UTC", Now: time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)}
	if err := e.Configure(plugin.Values{"url": srv.URL}, req); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e.Start(ctx, func(...string) {})
	if err := e.Prepare(req); err != nil {
		t.Fatal(err)
	}
	if len(queries) != len(historySources) {
		t.Fatalf("%d requests: %q", len(queries), queries)
	}
	joined := strings.Join(queries, "\n")
	for _, want := range []string{
		"aggregate=day", "group=pv", "grouped=true",
		"group=loadpoint", "group=grid", "group=home", "group=battery",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("%q missing in\n%s", want, joined)
		}
	}
	for _, q := range queries {
		if strings.Contains(q, "group=loadpoint") && strings.Contains(q, "grouped=true") {
			t.Errorf("loadpoints must not be grouped: %s", q)
		}
	}
}

func TestHistoryRows(t *testing.T) {
	// A Monday starts the week on the same day, a Sunday belongs to the week
	// before.
	for _, c := range []struct {
		now            time.Time
		week, lastWeek string
	}{
		{time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC), "2026-11-09", "2026-11-02"},
		{time.Date(2026, 11, 9, 0, 1, 0, 0, time.UTC), "2026-11-09", "2026-11-02"},
		{time.Date(2026, 11, 15, 23, 0, 0, 0, time.UTC), "2026-11-09", "2026-11-02"},
		{time.Date(2026, 1, 3, 12, 0, 0, 0, time.UTC), "2025-12-29", "2025-12-22"},
	} {
		e := &evcc{req: &plugin.Request{Language: "de", Timezone: "UTC", Now: c.now}}
		rows := e.historyRows()
		if rows[2].from != c.week || rows[3].from != c.lastWeek || rows[3].to != c.week {
			t.Errorf("%s: week %v last %v", c.now, rows[2], rows[3])
		}
		if rows[4].from != c.now.Format("2006-01")+"-01" {
			t.Errorf("%s: month from %s", c.now, rows[4].from)
		}
		if rows[6].from != c.now.Format("2006")+"-01-01" {
			t.Errorf("%s: year from %s", c.now, rows[6].from)
		}
	}
}

func TestParseHistory(t *testing.T) {
	body := `[{"title":"pv","group":"pv","data":[
		{"start":"2026-11-11T00:00:00+01:00","energy":4,"returnEnergy":0},
		{"start":"2026-11-12T00:00:00+01:00","energy":5,"returnEnergy":0}]}]`
	for _, in := range []string{body, `{"result":` + body + `}`} {
		series, err := parseHistory([]byte(in))
		if err != nil {
			t.Fatal(err)
		}
		if len(series) != 1 || len(series["pv"]) != 2 || series["pv"][1].energy != 5 || series["pv"][0].date != "2026-11-11" {
			t.Fatalf("%+v", series)
		}
	}
	if _, err := parseHistory([]byte(`[{"data":[{"start":"yesterday"}]}]`)); err == nil {
		t.Error("invalid start accepted")
	}
}
