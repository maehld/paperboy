// Command weather is a Paperboy plugin (ADR 0049) with Open-Meteo data. It
// fetches the forecast in the background, keeps it in memory and draws the
// widgets "current" (pictogram, temperature, condition, optional clothing
// advice), "days" (daily forecast) and "hours" (today's temperature curve).
// "current" and "days" have text variants for character displays.
//
//	plugins:
//	  weather:
//	    command: [weather]
//	    config: { lat: 52.52, lon: 13.40 }
//
// Open-Meteo needs no API key. The pictograms come from OpenMoji (icons/).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

const (
	// defaultBase is the public Open-Meteo endpoint; -base points tests and
	// self-hosted installations at another server.
	defaultBase = "https://api.open-meteo.com"
	// hoursPerDay is the number of hourly entries of the local day.
	hoursPerDay = 24
	// forecastDays is the length of the daily list.
	forecastDays = 5
	// requestTimeout bounds the HTTP call.
	requestTimeout = 20 * time.Second
)

func main() {
	plugin.Serve(&weather{})
}

// ---- Open-Meteo response ----

// openMeteo is the part of the Open-Meteo forecast response that the weather
// branch uses. Times are unix seconds (timeformat=unixtime); the daily
// aggregation follows the local day of the location (timezone=auto).
type openMeteo struct {
	Current struct {
		Time     int64   `json:"time"`
		Temp     float64 `json:"temperature_2m"`
		Apparent float64 `json:"apparent_temperature"`
		Humidity float64 `json:"relative_humidity_2m"`
		Precip   float64 `json:"precipitation"`
		Wind     float64 `json:"wind_speed_10m"`
		Code     int     `json:"weather_code"`
		IsDay    int     `json:"is_day"`
	} `json:"current"`
	Hourly struct {
		Time       []int64    `json:"time"`
		Temp       []float64  `json:"temperature_2m"`
		PrecipProb []*float64 `json:"precipitation_probability"`
	} `json:"hourly"`
	Daily struct {
		Time       []int64    `json:"time"`
		Code       []int      `json:"weather_code"`
		Max        []float64  `json:"temperature_2m_max"`
		Min        []float64  `json:"temperature_2m_min"`
		PrecipProb []*float64 `json:"precipitation_probability_max"`
		Sunrise    []int64    `json:"sunrise"`
		Sunset     []int64    `json:"sunset"`
	} `json:"daily"`
	UTCOffsetSeconds int `json:"utc_offset_seconds"`
}

// fetch requests the current values, the next hours and the daily forecast.
func fetch(ctx context.Context, client *http.Client, base string, lat, lon float64) (*openMeteo, error) {
	u, err := url.Parse(strings.TrimRight(base, "/") + "/v1/forecast")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("latitude", strconv.FormatFloat(lat, 'f', -1, 64))
	q.Set("longitude", strconv.FormatFloat(lon, 'f', -1, 64))
	q.Set("current", "temperature_2m,apparent_temperature,relative_humidity_2m,precipitation,weather_code,wind_speed_10m,is_day")
	q.Set("hourly", "temperature_2m,precipitation_probability")
	q.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_max,sunrise,sunset")
	q.Set("timezone", "auto")
	q.Set("timeformat", "unixtime")
	q.Set("forecast_days", strconv.Itoa(forecastDays))
	q.Set("wind_speed_unit", "kmh")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("open-meteo: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var f openMeteo
	if err := json.Unmarshal(body, &f); err != nil {
		return nil, fmt.Errorf("open-meteo: invalid response: %w", err)
	}
	if f.Current.Time == 0 || len(f.Hourly.Time) == 0 || len(f.Daily.Time) == 0 {
		return nil, errors.New("open-meteo: incomplete response")
	}
	return &f, nil
}

// ---- document branch ----

// branch is the forecast the widgets draw from.
type branch struct {
	Updated string  `json:"updated"`
	Current current `json:"current"`
	Today   today   `json:"today"`
	Days    []day   `json:"days"`
	Hours   []point `json:"hours"`
}

type current struct {
	Time       string   `json:"time"`
	Temp       float64  `json:"temp"`
	Apparent   float64  `json:"apparent"`
	Humidity   float64  `json:"humidity"`
	Precip     float64  `json:"precip"`
	PrecipProb *float64 `json:"precip_prob,omitempty"`
	Wind       float64  `json:"wind"`
	Beaufort   int      `json:"beaufort"`
	Code       int      `json:"code"`
	IsDay      bool     `json:"is_day"`
	Symbol     string   `json:"symbol"`
	Text       string   `json:"text"`
}

type today struct {
	Min        float64  `json:"min"`
	Max        float64  `json:"max"`
	PrecipProb *float64 `json:"precip_prob,omitempty"`
	Sunrise    string   `json:"sunrise"`
	Sunset     string   `json:"sunset"`
	Code       int      `json:"code"`
	Text       string   `json:"text"`
}

type day struct {
	Date       string   `json:"date"`
	Label      string   `json:"label"`
	Min        float64  `json:"min"`
	Max        float64  `json:"max"`
	PrecipProb *float64 `json:"precip_prob,omitempty"`
	Code       int      `json:"code"`
	Text       string   `json:"text"`
}

// point is one hour of today.
type point struct {
	Time   string             `json:"time"`
	Values map[string]float64 `json:"values"`
}

// branchOf maps the response to the weather branch. The times are RFC 3339 in
// UTC; templates and the chart convert them to the configured timezone.
func branchOf(f *openMeteo, language string) (branch, error) {
	if len(f.Daily.Time) == 0 || len(f.Daily.Max) == 0 || len(f.Daily.Min) == 0 {
		return branch{}, errors.New("open-meteo: no daily data")
	}
	loc := time.FixedZone("location", f.UTCOffsetSeconds)
	hour := hourIndex(f.Hourly.Time, f.Current.Time)

	b := branch{Updated: stamp(f.Current.Time)}
	b.Current = current{
		Time:     stamp(f.Current.Time),
		Temp:     round1(f.Current.Temp),
		Apparent: round1(f.Current.Apparent),
		Humidity: round1(f.Current.Humidity),
		Precip:   round2(f.Current.Precip),
		Wind:     round1(f.Current.Wind),
		Beaufort: beaufort(f.Current.Wind),
		Code:     f.Current.Code,
		IsDay:    f.Current.IsDay != 0,
		Symbol:   symbol(f.Current.Code),
		Text:     conditionText(f.Current.Code, language),
	}
	b.Current.PrecipProb = at(f.Hourly.PrecipProb, hour)

	b.Today = today{
		Min:        round1(f.Daily.Min[0]),
		Max:        round1(f.Daily.Max[0]),
		PrecipProb: at(f.Daily.PrecipProb, 0),
		Sunrise:    stamp(atInt(f.Daily.Sunrise, 0)),
		Sunset:     stamp(atInt(f.Daily.Sunset, 0)),
		Code:       atInt(f.Daily.Code, 0),
		Text:       conditionText(atInt(f.Daily.Code, 0), language),
	}

	for i, t := range f.Daily.Time {
		if i >= len(f.Daily.Max) || i >= len(f.Daily.Min) {
			break
		}
		local := time.Unix(t, 0).In(loc)
		code := atInt(f.Daily.Code, i)
		b.Days = append(b.Days, day{
			Date:       local.Format("2006-01-02"),
			Label:      weekday(local, language),
			Min:        round1(f.Daily.Min[i]),
			Max:        round1(f.Daily.Max[i]),
			PrecipProb: at(f.Daily.PrecipProb, i),
			Code:       code,
			Text:       conditionText(code, language),
		})
	}

	// The hours cover the local day of the location, so the time chart runs from
	// midnight to midnight like the solar history page.
	local := time.Unix(f.Current.Time, 0).In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	first := -1
	for i, t := range f.Hourly.Time {
		if t >= start.Unix() {
			first = i
			break
		}
	}
	for i := first; i >= 0 && i < len(f.Hourly.Time) && len(b.Hours) < hoursPerDay; i++ {
		if f.Hourly.Time[i] >= start.Add(24*time.Hour).Unix() {
			break
		}
		if i >= len(f.Hourly.Temp) {
			break
		}
		p := point{Time: stamp(f.Hourly.Time[i]), Values: map[string]float64{"temp": round1(f.Hourly.Temp[i])}}
		if prob := at(f.Hourly.PrecipProb, i); prob != nil {
			p.Values["precip_prob"] = *prob
		}
		b.Hours = append(b.Hours, p)
	}
	return b, nil
}

// hourIndex returns the index of the last hourly entry that is not after t, so
// the current precipitation probability matches the current hour. -1 means
// "before the first entry".
func hourIndex(times []int64, t int64) int {
	idx := -1
	for i, v := range times {
		if v > t {
			break
		}
		idx = i
	}
	return idx
}

func at[T any](list []*T, i int) *T {
	if i < 0 || i >= len(list) {
		return nil
	}
	return list[i]
}

func atInt[T int | int64](list []T, i int) T {
	var zero T
	if i < 0 || i >= len(list) {
		return zero
	}
	return list[i]
}

func stamp(seconds int64) string {
	if seconds == 0 {
		return ""
	}
	return time.Unix(seconds, 0).UTC().Format(time.RFC3339)
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// beaufortBands are the upper limits of wind force 0 to 11 in km/h; everything
// above belongs to force 12.
var beaufortBands = []float64{1, 6, 12, 20, 29, 39, 50, 62, 75, 89, 103, 118}

// beaufort converts a wind speed in km/h to the Beaufort scale.
func beaufort(kmh float64) int {
	for i, limit := range beaufortBands {
		if kmh < limit {
			return i
		}
	}
	return 12
}

// symbol names the pictogram of a WMO weather code: sun, cloud, rain, snow or
// thunder, the file names in icons/.
func symbol(code int) string {
	switch {
	case code == 0, code == 1:
		return "sun"
	case code == 2, code == 3, code == 45, code == 48:
		return "cloud"
	case code >= 51 && code <= 67, code >= 80 && code <= 82:
		return "rain"
	case code >= 71 && code <= 77, code >= 85 && code <= 86:
		return "snow"
	case code >= 95:
		return "thunder"
	}
	return "cloud"
}

var weekdayNames = map[string][7]string{
	"de": {"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"},
	"en": {"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
}

func weekday(t time.Time, language string) string {
	names, ok := weekdayNames[language]
	if !ok {
		names = weekdayNames["en"]
	}
	return names[int(t.Weekday())]
}

// conditionText names a WMO weather code.
var conditions = map[int][2]string{ // [de, en]
	0:  {"Klar", "Clear"},
	1:  {"Überwiegend klar", "Mainly clear"},
	2:  {"Teilweise bewölkt", "Partly cloudy"},
	3:  {"Bedeckt", "Overcast"},
	45: {"Nebel", "Fog"},
	48: {"Reifnebel", "Depositing rime fog"},
	51: {"Leichter Nieselregen", "Light drizzle"},
	53: {"Nieselregen", "Moderate drizzle"},
	55: {"Starker Nieselregen", "Dense drizzle"},
	56: {"Leichter gefrierender Nieselregen", "Light freezing drizzle"},
	57: {"Starker gefrierender Nieselregen", "Dense freezing drizzle"},
	61: {"Leichter Regen", "Slight rain"},
	63: {"Regen", "Moderate rain"},
	65: {"Starker Regen", "Heavy rain"},
	66: {"Leichter gefrierender Regen", "Light freezing rain"},
	67: {"Starker gefrierender Regen", "Heavy freezing rain"},
	71: {"Leichter Schneefall", "Slight snow"},
	73: {"Schneefall", "Moderate snow"},
	75: {"Starker Schneefall", "Heavy snow"},
	77: {"Schneegriesel", "Snow grains"},
	80: {"Leichte Regenschauer", "Slight rain showers"},
	81: {"Regenschauer", "Moderate rain showers"},
	82: {"Starke Regenschauer", "Violent rain showers"},
	85: {"Leichte Schneeschauer", "Slight snow showers"},
	86: {"Starke Schneeschauer", "Heavy snow showers"},
	95: {"Gewitter", "Thunderstorm"},
	96: {"Gewitter mit leichtem Hagel", "Thunderstorm with slight hail"},
	99: {"Gewitter mit starkem Hagel", "Thunderstorm with heavy hail"},
}

func conditionText(code int, language string) string {
	entry, ok := conditions[code]
	if !ok {
		if language == "de" {
			return "Unbekannt"
		}
		return "Unknown"
	}
	if language == "de" {
		return entry[0]
	}
	return entry[1]
}
