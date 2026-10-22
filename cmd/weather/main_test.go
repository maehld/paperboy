package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testPayload builds an Open-Meteo response for 2026-10-22 09:00 UTC at UTC+2,
// with 48 hourly entries from local midnight and five daily entries.
func testPayload(t *testing.T) map[string]any {
	t.Helper()
	localMidnight := time.Date(2026, 10, 21, 22, 0, 0, 0, time.UTC)
	hours := make([]int64, 48)
	temps := make([]float64, 48)
	probs := make([]*float64, 48)
	for i := range hours {
		hours[i] = localMidnight.Add(time.Duration(i) * time.Hour).Unix()
		temps[i] = 8 + float64(i)/3
		p := float64(i * 2 % 50)
		probs[i] = &p
	}
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
			"time": hours, "temperature_2m": temps, "precipitation_probability": probs,
		},
		"daily": map[string]any{
			"time":                          days,
			"weather_code":                  []int{3, 61, 0, 2, 80},
			"temperature_2m_max":            []float64{15.24, 14, 18, 19.3, 16},
			"temperature_2m_min":            []float64{8.06, 7, 9, 10, 8},
			"precipitation_probability_max": []*float64{ptr(40.0), ptr(80.0), ptr(0.0), ptr(10.0), ptr(60.0)},
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
		c.Precip != 0.12 || c.Wind != 14.2 || c.Code != 3 || !c.IsDay || c.Text != "Bedeckt" {
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
