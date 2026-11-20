package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

// now is the fixed request time of the tests.
var now = time.Date(2026, 11, 20, 17, 30, 0, 0, time.UTC)

// testStations is a small answer: A is the cheapest diesel station and closed,
// B is the cheapest open diesel station, C has no diesel.
func testStations() []station {
	return []station{
		{ID: "a", Name: "ARAL MITTE", Dist: 0.8, Open: false,
			Diesel: price{1.659, true}, E5: price{1.759, true}, E10: price{1.739, true}},
		{ID: "b", Name: "ESSO NORD", Dist: 1.2, Open: true,
			Diesel: price{1.679, true}, E5: price{1.779, true}, E10: price{1.749, true}},
		{ID: "c", Name: "FREIE TANKE", Dist: 2.4, Open: true,
			Diesel: price{}, E5: price{1.769, true}, E10: price{}},
	}
}

func testFuel() *fuel {
	req := &plugin.Request{Version: 9, Language: "de", Timezone: "Europe/Berlin", Now: now}
	return &fuel{
		apikey: "key", lat: 52.52, lon: 13.40, radius: 5,
		refresh: 10 * time.Minute, span: 7 * 24 * time.Hour,
		clock: func() time.Time { return now },
		cur:   testStations(), curAt: now, curHist: history{},
		req: req, loc: req.Location(), lang: req.Lang(),
	}
}

func TestFetch(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = map[string]string{}
		for k := range r.URL.Query() {
			got[k] = r.URL.Query().Get(k)
		}
		if r.URL.Path != "/json/list.php" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"license":"CC BY 4.0","data":"MTS-K","status":"ok","stations":[
			{"id":"b","name":"ESSO NORD","brand":"ESSO","dist":2.5,"diesel":1.709,"e5":false,"e10":1.689,"isOpen":true},
			{"id":"a","name":"ARAL MITTE","brand":"ARAL","dist":0.8,"diesel":1.659,"e5":1.759,"e10":1.739,"isOpen":false}
		]}`))
	}))
	defer srv.Close()

	list, err := fetch(context.Background(), srv.Client(), srv.URL, "secret", 52.5, 13.4, 7.5)
	if err != nil {
		t.Fatal(err)
	}
	if got["lat"] != "52.5" || got["lng"] != "13.4" || got["rad"] != "7.5" {
		t.Errorf("query = %v", got)
	}
	if got["type"] != "all" || got["apikey"] != "secret" {
		t.Errorf("query = %v", got)
	}
	if len(list) != 2 || list[0].ID != "a" || list[1].ID != "b" {
		t.Fatalf("stations = %+v", list)
	}
	if v, ok := list[0].fuel(fuelE5); !ok || v != 1.759 {
		t.Errorf("e5 = %v, %v", v, ok)
	}
	if _, ok := list[1].fuel(fuelE5); ok {
		t.Error("false price counted as set")
	}
}

func TestFetchErrors(t *testing.T) {
	for name, body := range map[string]string{
		"refused": `{"ok":false,"message":"invalid api key"}`,
		"silent":  `{"ok":false}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(body))
			}))
			defer srv.Close()
			_, err := fetch(context.Background(), srv.Client(), srv.URL, "k", 52, 13, 5)
			if err == nil || !strings.Contains(err.Error(), "tankerkoenig") {
				t.Fatalf("err = %v", err)
			}
		})
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer srv.Close()
	if _, err := fetch(context.Background(), srv.Client(), srv.URL, "k", 52, 13, 5); err == nil {
		t.Fatal("HTTP error not reported")
	}
}

func TestConfigure(t *testing.T) {
	req := &plugin.Request{Version: 9}
	f := &fuel{}
	cfg := plugin.Values{"apikey": "key", "lat": 52.5, "lon": 13.4}
	if err := f.Configure(cfg, req); err != nil {
		t.Fatal(err)
	}
	if f.radius != 5 || f.refresh != 10*time.Minute || f.span != 7*24*time.Hour || f.base != defaultBase {
		t.Errorf("defaults = %+v", f)
	}

	bad := map[string]plugin.Values{
		"no key":      {"lat": 52.5, "lon": 13.4},
		"empty key":   {"apikey": " ", "lat": 52.5, "lon": 13.4},
		"lat":         {"apikey": "k", "lat": 91, "lon": 13.4},
		"lon":         {"apikey": "k", "lat": 52.5, "lon": -181},
		"lat type":    {"apikey": "k", "lat": "52.5", "lon": 13.4},
		"radius":      {"apikey": "k", "lat": 52.5, "lon": 13.4, "radius": 30},
		"radius type": {"apikey": "k", "lat": 52.5, "lon": 13.4, "radius": "5"},
		"refresh":     {"apikey": "k", "lat": 52.5, "lon": 13.4, "refresh": "m1"},
		"span":        {"apikey": "k", "lat": 52.5, "lon": 13.4, "span": "30m"},
		"unknown":     {"apikey": "k", "lat": 52.5, "lon": 13.4, "fuel": "diesel"},
	}
	for name, cfg := range bad {
		if err := (&fuel{}).Configure(cfg, req); err == nil {
			t.Errorf("%s: no error", name)
		}
	}

	if err := (&fuel{}).Configure(plugin.Values{"apikey": "k", "lat": 52.5, "lon": 13.4},
		&plugin.Request{Version: 8}); err == nil {
		t.Error("protocol 8 accepted")
	}

	manual := plugin.Values{"apikey": "k", "lat": 52.5, "lon": 13.4, "radius": 25.0,
		"refresh": "5m", "span": "1h", "base": "http://localhost:1/"}
	if err := (&fuel{}).Configure(manual, req); err != nil {
		t.Fatalf("manual: %v", err)
	}
}

func TestCheapest(t *testing.T) {
	list := testStations()
	s, ok := cheapestStation(list, fuelDiesel)
	if !ok || s.ID != "b" {
		t.Fatalf("diesel = %q, %v", s.ID, ok)
	}
	s, ok = cheapestStation(list, fuelE5)
	if !ok || s.ID != "c" {
		t.Fatalf("e5 = %q, %v", s.ID, ok)
	}
	if _, ok := cheapestStation([]station{{ID: "d", Diesel: price{1.7, true}}}, fuelE10); ok {
		t.Error("diesel-only station reported e10")
	}

	// A tie goes to the nearer station.
	tie := []station{
		{ID: "far", Dist: 3, Open: true, Diesel: price{1.7, true}},
		{ID: "near", Dist: 1, Open: true, Diesel: price{1.7, true}},
	}
	if s, _ := cheapestStation(tie, fuelDiesel); s.ID != "near" {
		t.Errorf("tie = %q", s.ID)
	}

	// Without an open station the cheapest closed one wins.
	closed := []station{{ID: "x", Dist: 1, Open: false, Diesel: price{1.8, true}},
		{ID: "y", Dist: 2, Open: false, Diesel: price{1.7, true}}}
	if s, _ := cheapestStation(closed, fuelDiesel); s.ID != "y" {
		t.Errorf("closed = %q", s.ID)
	}
}

func TestHistoryRecord(t *testing.T) {
	f := testFuel()
	f.series = history{}

	if !f.record(now, testStations()) {
		t.Fatal("first record did not change")
	}
	// Diesel is 1.679 (the open ESSO), e5 1.769 (the open FREIE TANKE). An
	// open station wins over the cheaper closed ARAL.
	if got := f.series[fuelDiesel]; len(got) != 1 || got[0].Price != 1.679 {
		t.Fatalf("diesel = %+v", got)
	}
	if got := f.series[fuelE5]; len(got) != 1 || got[0].Price != 1.769 {
		t.Fatalf("e5 = %+v", got)
	}
	if f.record(now.Add(10*time.Minute), testStations()) {
		t.Error("an unchanged price was recorded")
	}

	changed := testStations()
	changed[1].Diesel = price{1.689, true}
	if !f.record(now.Add(20*time.Minute), changed) {
		t.Fatal("a changed price was not recorded")
	}
	if got := f.series[fuelDiesel]; len(got) != 2 || got[1].Price != 1.689 {
		t.Fatalf("diesel = %+v", got)
	}

	// Trim keeps the newest point before the span as the start of the line.
	f.span = time.Hour
	f.series[fuelDiesel] = []pricePoint{
		{At: now.Add(-3 * time.Hour).Unix(), Price: 1.6},
		{At: now.Add(-2 * time.Hour).Unix(), Price: 1.62},
		{At: now.Add(-30 * time.Minute).Unix(), Price: 1.64},
	}
	f.trim(now)
	got := f.series[fuelDiesel]
	if len(got) != 2 || got[0].Price != 1.62 || got[1].Price != 1.64 {
		t.Fatalf("trim = %+v", got)
	}
}

func TestHistoryState(t *testing.T) {
	f := testFuel()
	f.series = history{}
	f.record(now, testStations())
	f.loaded, f.pending = true, true
	state := f.State()
	if len(state) == 0 {
		t.Fatal("no state")
	}
	if f.State() != nil {
		t.Error("state returned twice")
	}

	second := testFuel()
	if err := second.LoadState(state, nil); err != nil {
		t.Fatal(err)
	}
	if got := second.series[fuelDiesel]; len(got) != 1 || got[0].Price != 1.679 {
		t.Fatalf("restored = %+v", got)
	}

	other := testFuel()
	other.apikey = "another key"
	if err := other.LoadState(state, nil); err != nil {
		t.Fatal(err)
	}
	if len(other.series) != 0 {
		t.Error("history of another key accepted")
	}

	old := testFuel()
	var s storedState
	json.Unmarshal(state, &s)
	s.Version = 0
	data, _ := json.Marshal(s)
	if err := old.LoadState(data, nil); err != nil {
		t.Fatal(err)
	}
	if len(old.series) != 0 {
		t.Error("history of another version accepted")
	}

	if err := testFuel().LoadState(json.RawMessage("{"), nil); err != nil {
		t.Fatalf("broken state: %v", err)
	}
}

func TestSteps(t *testing.T) {
	f := testFuel()
	f.curHist = history{fuelDiesel: {
		{At: now.Add(-2 * time.Hour).Unix(), Price: 1.60},
		{At: now.Add(-time.Hour).Unix(), Price: 1.65},
	}}
	from, to := now.Add(-90*time.Minute), now
	got := f.steps(fuelDiesel, from, to)
	if len(got) != 3 {
		t.Fatalf("steps = %+v", got)
	}
	if !got[0].at.Equal(from) || got[0].price != 1.60 {
		t.Errorf("base = %+v", got[0])
	}
	if !got[1].at.Equal(now.Add(-time.Hour)) || got[1].price != 1.65 {
		t.Errorf("change = %+v", got[1])
	}
	if !got[2].at.Equal(to) || got[2].price != 1.65 {
		t.Errorf("end = %+v", got[2])
	}

	// Without a point inside the window the line holds the newest older
	// point from the window start to now.
	got = f.steps(fuelDiesel, now.Add(-30*time.Minute), now)
	if len(got) != 2 || got[0].price != 1.65 || got[1].price != 1.65 {
		t.Errorf("steps without a change = %+v", got)
	}
}

func TestValues(t *testing.T) {
	f := testFuel()
	f.curHist = history{fuelDiesel: {{At: now.Add(-time.Hour).Unix(), Price: 1.679}}}
	req := &plugin.Request{
		Version: 9, Language: "de", Timezone: "Europe/Berlin", Now: now,
		Values:    []string{"price", "station", "distance", "open", "list", "updated"},
		ValueArgs: plugin.Values{"fuel": "diesel", "stations": 2},
	}
	f.req, f.loc, f.lang = req, req.Location(), req.Lang()
	got, err := f.Values(req)
	if err != nil {
		t.Fatal(err)
	}
	if v := got["price"]; v.Number == nil || *v.Number != 1.679 {
		t.Errorf("price = %+v", v)
	}
	if v := got["station"]; v.Text == nil || *v.Text != "ESSO NORD" {
		t.Errorf("station = %+v", v)
	}
	if v := got["distance"]; v.Number == nil || *v.Number != 1.2 {
		t.Errorf("distance = %+v", v)
	}
	if v := got["open"]; v.Text == nil || *v.Text != "offen" {
		t.Errorf("open = %+v", v)
	}
	list := strings.Split(*got["list"].Text, "\n")
	if len(list) != 2 || !strings.HasPrefix(list[0], "ARAL MITTE 1,659") {
		t.Errorf("list = %q", *got["list"].Text)
	}
	if !strings.Contains(list[0], "zu") {
		t.Errorf("closed station not marked: %q", list[0])
	}
	if v := got["updated"]; v.Timestamp == nil || *v.Timestamp != now.Format(time.RFC3339) {
		t.Errorf("updated = %+v", v)
	}

	if _, err := f.Values(&plugin.Request{Values: []string{"price"}, ValueArgs: plugin.Values{"fuel": "gas"}}); err == nil {
		t.Error("unknown fuel accepted")
	}
	if _, err := f.Values(&plugin.Request{Values: []string{"missing"}}); !errors.Is(err, plugin.ErrUnknownValue) {
		t.Errorf("unknown value: %v", err)
	}
}

func TestWidgets(t *testing.T) {
	f := testFuel()
	f.curHist = history{fuelDiesel: {
		{At: now.Add(-6 * time.Hour).Unix(), Price: 1.62},
		{At: now.Add(-2 * time.Hour).Unix(), Price: 1.65},
		{At: now.Add(-10 * time.Minute).Unix(), Price: 1.679},
	}}
	for _, widget := range []string{"prices", "cheapest", "history"} {
		t.Run(widget, func(t *testing.T) {
			w := plugin.WidgetRequest{
				Widget: widget, Width: 400, Height: 200,
				Args:  map[string]any{"fuel": "diesel"},
				Style: plugin.Style{Padding: 4, Rule: 1, TextPx: 16},
			}
			img := render.NewCanvas(w.Width, w.Height, 2)
			if err := f.Render(f.req, w, img); err != nil {
				t.Fatal(err)
			}
			if img.Rect.Dx() != w.Width {
				t.Error("canvas changed")
			}

			small := plugin.WidgetRequest{Widget: widget, Width: 30, Height: 20,
				Args: map[string]any{"fuel": "diesel"}, Style: plugin.Style{Padding: 4}}
			if err := f.Render(f.req, small, render.NewCanvas(30, 20, 2)); !errors.Is(err, plugin.ErrTooSmall) {
				t.Errorf("small cell: %v", err)
			}
		})
	}

	empty := testFuel()
	w := plugin.WidgetRequest{Widget: "history", Width: 400, Height: 200,
		Args: map[string]any{"fuel": "diesel"}, Style: plugin.Style{Padding: 4, TextPx: 16}}
	if err := empty.Render(empty.req, w, render.NewCanvas(400, 200, 2)); err != nil {
		t.Errorf("empty history: %v", err)
	}

	if err := f.Render(f.req, plugin.WidgetRequest{Widget: "missing"}, render.NewCanvas(10, 10, 2)); err == nil {
		t.Error("unknown widget accepted")
	}
}
