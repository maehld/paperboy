package main

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

// testPayload builds an Open-Meteo response for 2026-10-22 09:00 UTC at UTC+2,
// with 48 hourly entries from local midnight and five daily entries.
func testPayload(t *testing.T) map[string]any {
	t.Helper()
	localMidnight := time.Date(2026, 10, 21, 22, 0, 0, 0, time.UTC)
	hours := make([]int64, 48)
	temps := make([]float64, 48)
	precip := make([]float64, 48)
	rain := make([]float64, 48)
	showers := make([]float64, 48)
	snow := make([]float64, 48)
	codes := make([]int, 48)
	probs := make([]*float64, 48)
	for i := range hours {
		hours[i] = localMidnight.Add(time.Duration(i) * time.Hour).Unix()
		temps[i] = 8 + float64(i)/3
		p := float64(i * 2 % 50)
		probs[i] = &p
		codes[i] = 3
	}
	// Rain from 14:00 to 16:00, snow from 18:00 to 19:00 and a mixed hour at
	// 20:00, all local time.
	for i := 14; i < 16; i++ {
		precip[i], rain[i], codes[i] = 0.4, 0.4, 61
	}
	precip[18], snow[18], codes[18] = 0.5, 0.5, 71
	precip[20], rain[20], snow[20] = 0.5, 0.2, 0.3
	days := make([]int64, 5)
	for i := range days {
		days[i] = localMidnight.AddDate(0, 0, i).Unix()
	}
	sunrise := make([]int64, 5)
	sunset := make([]int64, 5)
	for i := range sunrise {
		sunrise[i] = localMidnight.AddDate(0, 0, i).Add(6 * time.Hour).Unix()
		sunset[i] = localMidnight.AddDate(0, 0, i).Add(16 * time.Hour).Unix()
	}
	current := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	return map[string]any{
		"utc_offset_seconds": 7200,
		"current": map[string]any{
			"time": current.Unix(), "temperature_2m": 12.44, "apparent_temperature": 10.9,
			"relative_humidity_2m": 78, "precipitation": 0.123, "weather_code": 3,
			"wind_speed_10m": 14.2, "is_day": 1,
		},
		"hourly": map[string]any{
			"time": hours, "temperature_2m": temps, "precipitation": precip,
			"precipitation_probability": probs, "rain": rain, "showers": showers,
			"snowfall": snow, "weather_code": codes,
		},
		"daily": map[string]any{
			"time":                          days,
			"weather_code":                  []int{3, 61, 0, 2, 80},
			"temperature_2m_max":            []float64{15.24, 14, 18, 19.3, 16},
			"temperature_2m_min":            []float64{8.06, 7, 9, 10, 8},
			"precipitation_probability_max": []*float64{ptr(40.0), ptr(80.0), ptr(0.0), ptr(10.0), ptr(60.0)},
			"precipitation_sum":             []float64{3.42, 8, 0, 1.2, 2.1},
			"sunrise":                       sunrise,
			"sunset":                        sunset,
		},
	}
}

func ptr(v float64) *float64 { return &v }

// serve starts a fake Open-Meteo endpoint and returns its base URL.
func serve(t *testing.T, payload any, status int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/forecast" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("timezone") != "auto" || q.Get("timeformat") != "unixtime" {
			t.Errorf("timezone/timeformat: %q", r.URL.RawQuery)
		}
		if q.Get("latitude") != "52.52" || q.Get("longitude") != "13.4" {
			t.Errorf("location: %q %q", q.Get("latitude"), q.Get("longitude"))
		}
		w.WriteHeader(status)
		if status != http.StatusOK {
			w.Write([]byte("upstream is down"))
			return
		}
		json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestFetchAndBranch(t *testing.T) {
	base := serve(t, testPayload(t), http.StatusOK)
	f, err := fetch(context.Background(), http.DefaultClient, base, 52.52, 13.4)
	if err != nil {
		t.Fatal(err)
	}
	b, err := branchOf(f, "de")
	if err != nil {
		t.Fatal(err)
	}
	if b.Updated != "2026-10-22T09:00:00Z" {
		t.Fatalf("updated = %q", b.Updated)
	}
	c := b.Current
	if c.Time != "2026-10-22T09:00:00Z" || c.Temp != 12.4 || c.Apparent != 10.9 || c.Humidity != 78 ||
		c.Precip != 0.12 || c.Wind != 14.2 || c.Beaufort != 3 || c.Code != 3 || !c.IsDay ||
		c.Symbol != "cloudy" || c.Text != "Bedeckt" {
		t.Fatalf("current = %+v", c)
	}
	if c.PrecipProb == nil || *c.PrecipProb != 22 {
		t.Fatalf("current precipitation probability = %v", c.PrecipProb)
	}
	if b.Today.Min != 8.1 || b.Today.Max != 15.2 || b.Today.Sunrise != "2026-10-22T04:00:00Z" ||
		b.Today.Sunset != "2026-10-22T14:00:00Z" || b.Today.Text != "Bedeckt" {
		t.Fatalf("today = %+v", b.Today)
	}
	if b.Today.PrecipProb == nil || *b.Today.PrecipProb != 40 {
		t.Fatalf("today precipitation probability = %v", b.Today.PrecipProb)
	}
	if b.Today.Precip != 3.42 {
		t.Fatalf("today precipitation = %v", b.Today.Precip)
	}
	if len(b.Days) != 5 || b.Days[0].Date != "2026-10-22" || b.Days[0].Label != "Do" ||
		b.Days[1].Label != "Fr" || b.Days[0].Max != 15.2 || b.Days[1].Text != "Leichter Regen" {
		t.Fatalf("days = %+v", b.Days)
	}
	if len(b.Hours) != hoursPerDay {
		t.Fatalf("%d hours, want %d", len(b.Hours), hoursPerDay)
	}
	if b.Hours[0].Time != "2026-10-21T22:00:00Z" || b.Hours[len(b.Hours)-1].Time != "2026-10-22T21:00:00Z" {
		t.Fatalf("hour range: %s .. %s", b.Hours[0].Time, b.Hours[len(b.Hours)-1].Time)
	}
	if b.Hours[0].Values["precip_prob"] != 0 || b.Hours[11].Values["temp"] != round1(8+11.0/3) {
		t.Fatalf("hour values = %+v", b.Hours)
	}
	if b.Hours[14].Values["precip"] != 0.4 || b.Hours[14].Type != precipRain ||
		b.Hours[18].Type != precipSnow || b.Hours[20].Type != precipMixed || b.Hours[0].Type != "" {
		t.Fatalf("hour types = %+v", b.Hours[0].Type)
	}
	want := []precipEvent{
		{Type: precipRain, Start: "2026-10-22T12:00:00Z", End: "2026-10-22T14:00:00Z"},
		{Type: precipSnow, Start: "2026-10-22T16:00:00Z", End: "2026-10-22T17:00:00Z"},
		{Type: precipMixed, Start: "2026-10-22T18:00:00Z", End: "2026-10-22T19:00:00Z"},
	}
	if len(b.Events) != len(want) {
		t.Fatalf("events = %+v", b.Events)
	}
	for i := range want {
		if b.Events[i] != want[i] {
			t.Fatalf("event %d = %+v, want %+v", i, b.Events[i], want[i])
		}
	}
}

func TestFetchReportsUpstreamErrors(t *testing.T) {
	base := serve(t, nil, http.StatusInternalServerError)
	_, err := fetch(context.Background(), http.DefaultClient, base, 52.52, 13.4)
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") || !strings.Contains(err.Error(), "upstream is down") {
		t.Fatalf("error = %v", err)
	}
}

func TestBranchRejectsIncompleteResponse(t *testing.T) {
	if _, err := branchOf(&openMeteo{}, "de"); err == nil {
		t.Fatal("empty response accepted")
	}
	base := serve(t, map[string]any{"current": map[string]any{"time": 1}}, http.StatusOK)
	if _, err := fetch(context.Background(), http.DefaultClient, base, 52.52, 13.4); err == nil ||
		!strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("error = %v", err)
	}
}

func TestHourIndex(t *testing.T) {
	times := []int64{100, 200, 300}
	tests := []struct {
		now  int64
		want int
	}{{50, -1}, {100, 0}, {250, 1}, {300, 2}, {400, 2}}
	for _, tt := range tests {
		if got := hourIndex(times, tt.now); got != tt.want {
			t.Fatalf("hourIndex(%d) = %d, want %d", tt.now, got, tt.want)
		}
	}
}

func TestConditionText(t *testing.T) {
	tests := []struct {
		code     int
		language string
		want     string
	}{
		{0, "de", "Klar"},
		{0, "en", "Clear"},
		{95, "de", "Gewitter"},
		{42, "de", "Unbekannt"},
		{42, "en", "Unknown"},
	}
	for _, tt := range tests {
		if got := conditionText(tt.code, tt.language); got != tt.want {
			t.Fatalf("conditionText(%d, %s) = %q, want %q", tt.code, tt.language, got, tt.want)
		}
	}
}

func TestWeekday(t *testing.T) {
	d := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC) // Thursday
	if got := weekday(d, "de"); got != "Do" {
		t.Fatalf("de = %q", got)
	}
	if got := weekday(d, "en"); got != "Thu" {
		t.Fatalf("en = %q", got)
	}
}

func TestPrecipType(t *testing.T) {
	tests := []struct {
		code                int
		rain, showers, snow float64
		prob                float64
		want                string
	}{
		{3, 0, 0, 0, 0, ""},
		{3, 0, 0, 0, 40, ""}, // a probability without a precipitation code
		{61, 0, 0, 0, 40, precipRain},
		{71, 0, 0, 0, 60, precipSnow},
		{61, 0.2, 0, 0, 0, precipRain},
		{71, 0, 0, 0.3, 0, precipSnow},
		{61, 0.2, 0, 0.3, 0, precipMixed},
		{3, 0.2, 0.1, 0, 0, precipRain},
		{95, 0, 0, 0, 90, precipRain},
		{86, 0, 0, 0, 10, precipSnow},
	}
	for _, tt := range tests {
		if got := precipType(tt.code, tt.rain, tt.showers, tt.snow, tt.prob); got != tt.want {
			t.Fatalf("precipType(%d, %g, %g, %g, %g) = %q, want %q", tt.code, tt.rain, tt.showers, tt.snow, tt.prob, got, tt.want)
		}
	}
}

// Events are the contiguous hours above the threshold, snow and rain in one
// run make a mixed event.
func TestEventsOf(t *testing.T) {
	at := func(hour int) string {
		return time.Date(2026, 10, 22, hour, 0, 0, 0, time.UTC).Format(time.RFC3339)
	}
	hour := func(h int, amount float64, typ string) point {
		return point{Time: at(h), Values: map[string]float64{"precip": amount}, Type: typ}
	}
	points := []point{
		hour(10, 0.05, precipRain), // below the threshold
		hour(11, 0.4, precipRain),
		hour(12, 0.3, precipRain),
		hour(13, 0, ""),
		hour(14, 0.5, precipSnow),
		hour(15, 0.2, precipRain),
	}
	got := eventsOf(points)
	want := []precipEvent{
		{Type: precipRain, Start: at(11), End: at(13)},
		{Type: precipMixed, Start: at(14), End: at(16)},
	}
	if len(got) != len(want) {
		t.Fatalf("events = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if len(eventsOf(nil)) != 0 {
		t.Fatal("empty series has events")
	}
}

func TestEventText(t *testing.T) {
	rain := precipEvent{Type: precipRain, Start: "2026-10-22T12:00:00Z", End: "2026-10-22T14:00:00Z"}
	snow := precipEvent{Type: precipSnow, Start: "2026-10-22T16:00:00Z", End: "2026-10-22T17:00:00Z"}
	mixed := precipEvent{Type: precipMixed, Start: "2026-10-22T18:00:00Z", End: "2026-10-22T19:00:00Z"}
	tests := []struct {
		name   string
		now    time.Time
		lang   string
		events []precipEvent
		want   string
	}{
		{"before", time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC), "de", []precipEvent{rain}, "Regen ab 14:00, ca. 2 h"},
		{"running", time.Date(2026, 10, 22, 12, 30, 0, 0, time.UTC), "de", []precipEvent{rain}, "Regen bis 16:00"},
		{"over", time.Date(2026, 10, 22, 15, 0, 0, 0, time.UTC), "de", []precipEvent{rain}, "Kein Regen heute"},
		{"skips finished", time.Date(2026, 10, 22, 15, 0, 0, 0, time.UTC), "de", []precipEvent{rain, snow}, "Schnee ab 18:00, ca. 1 h"},
		{"snow", time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC), "de", []precipEvent{snow}, "Schnee ab 18:00, ca. 1 h"},
		{"mixed", time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC), "de", []precipEvent{mixed}, "Regen+Schnee ab 20:00, ca. 1 h"},
		{"english", time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC), "en", []precipEvent{rain}, "Rain from 14:00, about 2 h"},
		{"none", time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC), "en", nil, "No rain today"},
	}
	for _, tt := range tests {
		req := &plugin.Request{Language: tt.lang, Timezone: "Europe/Berlin", Now: tt.now}
		p := &weather{cur: &branch{Events: tt.events}, req: req, loc: req.Location(), lang: tt.lang}
		if got := p.eventText(); got != tt.want {
			t.Fatalf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestBeaufort(t *testing.T) {
	tests := []struct {
		kmh  float64
		want int
	}{
		{0, 0}, {0.9, 0}, {1, 1}, {5.9, 1}, {12, 3}, {19.9, 3}, {20, 4},
		{29, 5}, {50, 7}, {103, 11}, {117.9, 11}, {118, 12}, {200, 12},
	}
	for _, tt := range tests {
		if got := beaufort(tt.kmh); got != tt.want {
			t.Fatalf("beaufort(%g) = %d, want %d", tt.kmh, got, tt.want)
		}
	}
}

func TestSymbol(t *testing.T) {
	tests := []struct {
		code  int
		isDay bool
		want  string
	}{
		{0, true, "day-sunny"}, {1, true, "day-sunny"}, {0, false, "night-clear"},
		{1, false, "night-clear"}, {2, true, "day-cloudy"},
		{2, false, "night-alt-partly-cloudy"}, {3, true, "cloudy"},
		{3, false, "cloudy"}, {45, true, "fog"}, {48, false, "fog"},
		{51, true, "sprinkle"}, {53, false, "sprinkle"}, {55, true, "sprinkle"},
		{56, true, "sleet"}, {57, false, "sleet"}, {66, true, "sleet"},
		{67, false, "sleet"}, {61, true, "rain"}, {63, false, "rain"},
		{65, true, "rain"}, {80, true, "rain"}, {81, false, "rain"},
		{82, true, "rain"}, {71, true, "snow"}, {73, false, "snow"},
		{75, true, "snow"}, {77, false, "snow"}, {85, true, "snow"},
		{86, false, "snow"}, {95, true, "thunderstorm"}, {96, false, "thunderstorm"},
		{99, true, "thunderstorm"}, {42, true, "cloudy"}, {42, false, "cloudy"},
	}
	for _, tt := range tests {
		if got := symbol(tt.code, tt.isDay); got != tt.want {
			t.Fatalf("symbol(%d, %t) = %q, want %q", tt.code, tt.isDay, got, tt.want)
		}
	}
}

// Every pictogram the widgets can name must exist in the embedded font, so a
// wrong code point fails here instead of leaving a cell empty.
func TestIcons(t *testing.T) {
	for code := range conditions {
		for _, isDay := range []bool{true, false} {
			name := symbol(code, isDay)
			m := icon(name)
			if m == nil {
				t.Fatalf("icon(%q) for code %d is missing", name, code)
			}
		}
	}
	for name := range iconRunes {
		if icon(name) == nil {
			t.Fatalf("icon(%q) is missing", name)
		}
	}
	for i := 0; i < moonGlyphs; i++ {
		if icon(moonGlyphName(i)) == nil {
			t.Fatalf("icon(%q) is missing", moonGlyphName(i))
		}
	}
}

// A widget draws on every palette the protocol knows; a 16-level display
// (ADR 0105) must not need anything but the levels the plugin already gets.
func TestRenderWidgets(t *testing.T) {
	base := serve(t, testPayload(t), http.StatusOK)
	f, err := fetch(context.Background(), http.DefaultClient, base, 52.52, 13.4)
	if err != nil {
		t.Fatal(err)
	}
	b, err := branchOf(f, "de")
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range [][3]int{{800, 480, 125}, {936, 351, 227}, {400, 200, 125}} {
		w, h, ppi := size[0], size[1], size[2]
		req := &plugin.Request{Language: "de", Timezone: "Europe/Berlin",
			Now:  time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC),
			Page: plugin.PageInfo{Width: w, Height: h, PPI: float64(ppi)}}
		p := &weather{cur: &b, req: req, loc: req.Location(), lang: "de"}
		style := plugin.Style{Padding: 8, Rule: 1, TextPx: 16 * float64(ppi) / 72}
		for _, levels := range []int{2, 4, 16} {
			for _, name := range []string{"current", "days", "hours", "sun", "moon"} {
				img := render.NewCanvas(w, h, levels)
				wr := plugin.WidgetRequest{Widget: name, Width: w, Height: h, Style: style}
				if err := p.Render(req, wr, img); err != nil {
					t.Fatalf("%s at %dx%d with %d levels: %v", name, w, h, levels, err)
				}
			}
		}
	}
}

// The legend of the hours widget names the fill patterns; `legend: false`
// drops it on a cell that needs the room.
func TestHoursLegendCanBeSwitchedOff(t *testing.T) {
	base := serve(t, testPayload(t), http.StatusOK)
	f, err := fetch(context.Background(), http.DefaultClient, base, 52.52, 13.4)
	if err != nil {
		t.Fatal(err)
	}
	b, err := branchOf(f, "de")
	if err != nil {
		t.Fatal(err)
	}
	req := &plugin.Request{Language: "de", Timezone: "Europe/Berlin",
		Now:  time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC),
		Page: plugin.PageInfo{Width: 800, Height: 120, PPI: 236}}
	p := &weather{cur: &b, req: req, loc: req.Location(), lang: "de"}
	draw := func(args map[string]any) *image.Paletted {
		img := render.NewCanvas(800, 120, 4)
		wr := plugin.WidgetRequest{Widget: "hours", Width: 800, Height: 120,
			Style: plugin.Style{Padding: 8, Rule: 1, TextPx: 52}, Args: args}
		if err := p.Render(req, wr, img); err != nil {
			t.Fatal(err)
		}
		return img
	}
	count := func(img *image.Paletted, r image.Rectangle) int {
		n := 0
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				if int(img.ColorIndexAt(x, y)) != len(img.Palette)-1 {
					n++
				}
			}
		}
		return n
	}
	area := image.Rect(400, 0, 800, 29)
	if count(draw(nil), area) == 0 {
		t.Fatal("the legend drew no ink")
	}
	if n := count(draw(map[string]any{"legend": false}), area); n != 0 {
		t.Fatalf("legend: false left %d pixels in the title row", n)
	}
}

// TestValues checks the declared values of a text page: the readings stay
// numbers with their unit, and the forecast carries one line per day.
func TestValues(t *testing.T) {
	base := serve(t, testPayload(t), http.StatusOK)
	f, err := fetch(context.Background(), http.DefaultClient, base, 52.52, 13.4)
	if err != nil {
		t.Fatal(err)
	}
	b, err := branchOf(f, "de")
	if err != nil {
		t.Fatal(err)
	}
	req := &plugin.Request{Language: "de", Timezone: "Europe/Berlin",
		Now:  time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC),
		Page: plugin.PageInfo{Width: 20, Height: 4}}
	p := &weather{cur: &b, req: req, loc: req.Location(), lang: "de"}
	names := []string{"temperature", "apparent", "humidity", "precipitation", "precipitation-probability",
		"precipitation-today", "precipitation-next", "wind", "beaufort", "condition", "min-today",
		"max-today", "sunrise", "sunset", "daylight", "moon-phase", "moon-illumination",
		"updated", "forecast"}
	values, err := p.Values(&plugin.Request{Values: names})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if v, ok := values[name]; !ok || v.Type() == "" {
			t.Fatalf("%s: %+v", name, v)
		}
	}
	if v := values["temperature"]; v.Number == nil || *v.Number != 12.4 {
		t.Fatalf("temperature = %+v", v)
	}
	if v := values["condition"]; v.Text == nil || *v.Text != "Bedeckt" {
		t.Fatalf("condition = %+v", v)
	}
	if v := values["precipitation-today"]; v.Number == nil || *v.Number != 3.42 {
		t.Fatalf("precipitation-today = %+v", v)
	}
	if v := values["precipitation-next"]; v.Text == nil || *v.Text != "Regen ab 14:00, ca. 2 h" {
		t.Fatalf("precipitation-next = %+v", v)
	}
	if v := values["sunrise"]; v.Timestamp == nil || *v.Timestamp != "2026-10-22T04:00:00Z" {
		t.Fatalf("sunrise = %+v", v)
	}
	if v := values["daylight"]; v.Text == nil || *v.Text != "10 Std." {
		t.Fatalf("daylight = %+v", v)
	}
	phase, illumination := moonPhase(req.Now)
	if v := values["moon-phase"]; v.Text == nil || *v.Text != p.moonName(phase) {
		t.Fatalf("moon-phase = %+v", v)
	}
	if v := values["moon-illumination"]; v.Number == nil || math.Abs(*v.Number-illumination*100) > 1e-9 {
		t.Fatalf("moon-illumination = %+v", v)
	}
	if v := values["forecast"]; v.Text == nil {
		t.Fatalf("forecast = %+v", v)
	} else if lines := strings.Split(*v.Text, "\n"); len(lines) != 5 ||
		lines[0] != "Heute Bedeckt 15°/8°" || !strings.HasPrefix(lines[1], "Fr ") {
		t.Fatalf("forecast lines = %q", lines)
	}
	if _, err := p.Values(&plugin.Request{Values: []string{"nope"}}); !errors.Is(err, plugin.ErrUnknownValue) {
		t.Fatalf("unknown value: %v", err)
	}
}

// The daylight text follows the request language and keeps the minutes.
func TestDaylightText(t *testing.T) {
	tests := []struct {
		lang string
		d    time.Duration
		want string
	}{
		{"de", 10*time.Hour + 30*time.Minute, "10 Std. 30 Min."},
		{"en", 10*time.Hour + 30*time.Minute, "10 h 30 min"},
		{"de", 10*time.Hour + 5*time.Minute, "10 Std. 5 Min."},
		{"de", 45 * time.Minute, "45 Min."},
		{"en", 10 * time.Hour, "10 h"},
	}
	for _, tt := range tests {
		p := &weather{req: &plugin.Request{Language: tt.lang}}
		if got := p.daylightText(tt.d); got != tt.want {
			t.Fatalf("daylightText(%s, %v) = %q, want %q", tt.lang, tt.d, got, tt.want)
		}
	}
}

// The moon phase counts from the new moon of 2000-01-06 18:14 UTC, with the
// eight named phases at their eighths and 28 glyphs around the cycle.
func TestMoonPhase(t *testing.T) {
	epoch := time.Unix(newMoonEpoch, 0)
	half := time.Duration(synodicMonth * 24 * float64(time.Hour) / 2)
	quarter := half / 2
	tests := []struct {
		at                       time.Time
		phase, illumination, tol float64
	}{
		{epoch, 0, 0, 1e-9},
		{epoch.Add(quarter), 0.25, 0.5, 1e-6},
		{epoch.Add(half), 0.5, 1, 1e-6},
		{epoch.Add(-quarter), 0.75, 0.5, 1e-6},
	}
	for _, tt := range tests {
		phase, illumination := moonPhase(tt.at)
		if math.Abs(phase-tt.phase) > tt.tol || math.Abs(illumination-tt.illumination) > tt.tol {
			t.Fatalf("moonPhase(%v) = %g, %g; want %g, %g", tt.at, phase, illumination, tt.phase, tt.illumination)
		}
	}
	if got := moonGlyphIndex(0); got != 0 {
		t.Fatalf("moonGlyphIndex(0) = %d", got)
	}
	if got := moonGlyphIndex(0.5); got != 14 {
		t.Fatalf("moonGlyphIndex(0.5) = %d", got)
	}
	if got := moonGlyphIndex(0.99); got != 0 {
		t.Fatalf("moonGlyphIndex(0.99) = %d", got)
	}
}

// The eight phase names follow the request language.
func TestMoonName(t *testing.T) {
	tests := []struct {
		phase  float64
		de, en string
	}{
		{0, "Neumond", "New moon"},
		{0.125, "Zunehmende Sichel", "Waxing crescent"},
		{0.25, "Erstes Viertel", "First quarter"},
		{0.375, "Zunehmender Mond", "Waxing gibbous"},
		{0.5, "Vollmond", "Full moon"},
		{0.625, "Abnehmender Mond", "Waning gibbous"},
		{0.75, "Letztes Viertel", "Last quarter"},
		{0.875, "Abnehmende Sichel", "Waning crescent"},
	}
	for _, tt := range tests {
		de := &weather{req: &plugin.Request{Language: "de"}}
		en := &weather{req: &plugin.Request{Language: "en"}}
		if got := de.moonName(tt.phase); got != tt.de {
			t.Fatalf("moonName(%g, de) = %q, want %q", tt.phase, got, tt.de)
		}
		if got := en.moonName(tt.phase); got != tt.en {
			t.Fatalf("moonName(%g, en) = %q, want %q", tt.phase, got, tt.en)
		}
	}
}

// A cell the padding eats up is reported, so Paperboy draws its notice instead
// of an empty cell.
func TestTooSmallCell(t *testing.T) {
	req := &plugin.Request{}
	w := plugin.WidgetRequest{Width: 10, Height: 10, Style: plugin.Style{Padding: 8, Rule: 1, TextPx: 16}}
	p := &weather{cur: &branch{}, req: req}
	for _, name := range []string{"current", "days", "hours", "sun", "moon"} {
		w.Widget = name
		err := p.Render(req, w, render.NewCanvas(10, 10, 2))
		if !errors.Is(err, plugin.ErrTooSmall) {
			t.Errorf("%s in a 10x10 cell: error = %v, want ErrTooSmall", name, err)
		}
	}
}
