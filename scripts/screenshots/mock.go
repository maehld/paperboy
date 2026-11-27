package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// mock answers every upstream service of the shipped plugins with demo data.
// The data follows the time of the run, so the pictures show a plausible day.
type mock struct {
	base string
	root string
	now  time.Time
}

func newMock(root string) *mock {
	return &mock{root: root, now: time.Now()}
}

func (m *mock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/v1/forecast":
		m.weather(w)
	case r.URL.Path == "/api/state":
		m.evccState(w)
	case r.URL.Path == "/api/history/energy":
		m.evccHistory(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/v1/vehicles/"):
		m.skoda(w)
	case r.URL.Path == "/api/v1/status", r.URL.Path == "/api/v1/job", r.URL.Path == "/api/v1/info":
		m.prusalink(w, r)
	case r.URL.Path == "/car.png":
		m.placeholder(w, "car")
	case r.URL.Path == "/artwork.png":
		m.placeholder(w, "creature")
	case r.URL.Path == "/info.0.json" || strings.HasSuffix(r.URL.Path, "/info.0.json"):
		m.xkcd(w)
	case r.URL.Path == "/IMG/wide.png" || r.URL.Path == "/comic.png":
		m.placeholder(w, "comic")
	case strings.HasPrefix(r.URL.Path, "/api/v2/"):
		m.pokedex(w, r)
	case strings.HasPrefix(r.URL.Path, "/feeds/"):
		m.feed(w, r)
	case strings.HasPrefix(r.URL.Path, "/cal"):
		m.caldav(w, r)
	case strings.HasPrefix(r.URL.Path, "/card"):
		m.carddav(w, r)
	case strings.HasPrefix(r.URL.Path, "/efa/"):
		m.efa(w, r)
	case strings.HasPrefix(r.URL.Path, "/proxy/network/"):
		m.unifi(w, r)
	case r.URL.Path == "/json/list.php":
		m.fuel(w)
	case r.URL.Path == "/General/AuthenticatePublisherAccount":
		fmt.Fprint(w, `"`+dexAccount+`"`)
	case r.URL.Path == "/General/LoginPublisherAccountById":
		fmt.Fprint(w, `"`+dexSession+`"`)
	case r.URL.Path == "/Publisher/ReadPublisherLatestGlucoseValues":
		m.dexcom(w)
	default:
		http.NotFound(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func round3(v float64) float64 {
	return float64(int(v*1000+0.5)) / 1000
}

func sha256Sum(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

// ---- weather ----

func (m *mock) weather(w http.ResponseWriter) {
	loc := berlin()
	now := m.now.In(loc)
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	_, offset := midnight.Zone()

	type hourly struct {
		Time       []int64    `json:"time"`
		Temp       []float64  `json:"temperature_2m"`
		Precip     []float64  `json:"precipitation"`
		PrecipProb []*float64 `json:"precipitation_probability"`
		Rain       []float64  `json:"rain"`
		Showers    []float64  `json:"showers"`
		Snowfall   []float64  `json:"snowfall"`
		Code       []int      `json:"weather_code"`
	}
	var h hourly
	for i := 0; i < 72; i++ {
		t := midnight.Add(time.Duration(i) * time.Hour)
		h.Time = append(h.Time, t.Unix())
		temp := 11 + 5*sin((float64(i)-9)/24*2*3.14159)
		h.Temp = append(h.Temp, round1(temp))
		prob, precip, rain, snow, code := 5.0, 0.0, 0.0, 0.0, 2
		if !t.Before(now) && t.Before(now.Add(2*time.Hour)) {
			prob, precip, rain, code = 15, 0, 0, 1
		}
		if !t.Before(now.Add(2*time.Hour)) && t.Before(now.Add(5*time.Hour)) {
			prob, precip, rain, code = 75, 0.5, 0.5, 61
		}
		if t.Hour() < 7 || t.Hour() > 20 {
			code = 0
			if t.Hour() < 5 {
				prob, precip, code = 30, 0.1, 45
			}
		}
		h.Precip = append(h.Precip, precip)
		probv := prob
		h.PrecipProb = append(h.PrecipProb, &probv)
		h.Rain = append(h.Rain, rain)
		h.Showers = append(h.Showers, 0)
		h.Snowfall = append(h.Snowfall, snow)
		h.Code = append(h.Code, code)
	}

	type daily struct {
		Time       []int64   `json:"time"`
		Code       []int     `json:"weather_code"`
		Max        []float64 `json:"temperature_2m_max"`
		Min        []float64 `json:"temperature_2m_min"`
		PrecipProb []float64 `json:"precipitation_probability_max"`
		PrecipSum  []float64 `json:"precipitation_sum"`
		Sunrise    []int64   `json:"sunrise"`
		Sunset     []int64   `json:"sunset"`
	}
	var d daily
	codes := []int{61, 3, 2, 0, 71}
	for i := 0; i < 5; i++ {
		day := midnight.AddDate(0, 0, i)
		d.Time = append(d.Time, day.Unix())
		d.Code = append(d.Code, codes[i])
		d.Max = append(d.Max, 16+float64(i%2))
		d.Min = append(d.Min, 7+float64(i%3))
		d.PrecipProb = append(d.PrecipProb, 70-float64(i)*10)
		d.PrecipSum = append(d.PrecipSum, 4.2-float64(i)*0.7)
		d.Sunrise = append(d.Sunrise, day.Add(7*time.Hour+25*time.Minute).Unix())
		d.Sunset = append(d.Sunset, day.Add(17*time.Hour+2*time.Minute).Unix())
	}

	current := map[string]any{
		"time": now.Unix(), "temperature_2m": 12.4, "apparent_temperature": 10.9,
		"relative_humidity_2m": 68, "precipitation": 0.0, "weather_code": 2,
		"wind_speed_10m": 14.2, "is_day": 1,
	}
	writeJSON(w, map[string]any{
		"current": current, "hourly": h, "daily": d,
		"utc_offset_seconds": offset, "timezone": "Europe/Berlin",
	})
}

func sin(v float64) float64 {
	// A small sine without importing math twice in the file.
	v = v - 6.283185307179586*float64(int(v/6.283185307179586))
	term, sum := v, v
	for i := 1; i < 8; i++ {
		term *= -v * v / float64((2*i)*(2*i+1))
		sum += term
	}
	return sum
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }

// ---- evcc ----

func (m *mock) evccState(w http.ResponseWriter) {
	now := m.now
	state := map[string]any{
		"siteTitle": "Zuhause",
		"pvPower":   9650, "homePower": 1140, "pvEnergy": 26009.5,
		"grid":       map[string]any{"power": -3150, "energy": 9697.5, "returnEnergy": 11141.6},
		"battery":    map[string]any{"power": -1180, "soc": 81, "capacity": 9.6, "energy": 6013.7, "returnEnergy": 6362.4},
		"tariffGrid": 0.27, "currency": "EUR",
		"statistics": map[string]any{
			"thisYear": map[string]any{"avgPrice": 0.1639, "chargedKWh": 145.3, "solarPercentage": 55.1},
			"total":    map[string]any{"avgPrice": 0.1705, "chargedKWh": 1234.5, "solarPercentage": 54.8},
		},
		"loadpoints": []any{map[string]any{
			"title": "Garage", "mode": "pv", "vehicleTitle": "Enyaq", "vehicleSoc": 64,
			"vehicleRange": 180, "effectiveLimitSoc": 80, "chargePower": 4180,
			"sessionEnergy": 5400, "sessionSolarPercentage": 90, "charging": true,
			"connected": true, "phasesActive": 1, "chargeRemainingDuration": 2400,
		}},
		"forecast": map[string]any{
			"grid": [][]float64{{float64(now.Add(15 * time.Minute).Unix()), float64(now.Add(45 * time.Minute).Unix()), 0.25}, {float64(now.Add(45 * time.Minute).Unix()), float64(now.Add(75 * time.Minute).Unix()), 0.31}},
			"solar": map[string]any{
				"timeseries": [][]float64{
					{float64(now.Add(-2 * time.Hour).Unix()), 800},
					{float64(now.Add(-1 * time.Hour).Unix()), 2100},
					{float64(now.Unix()), 3900},
					{float64(now.Add(1 * time.Hour).Unix()), 3100},
					{float64(now.Add(2 * time.Hour).Unix()), 1200},
				},
				"today":    map[string]any{"energy": 12000},
				"tomorrow": map[string]any{"energy": 8600},
			},
		},
	}
	writeJSON(w, state)
}

func (m *mock) evccHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	loc := berlin()
	from, _ := time.ParseInLocation(time.RFC3339, q.Get("from"), loc)
	to, _ := time.ParseInLocation(time.RFC3339, q.Get("to"), loc)
	group := q.Get("group")
	step := dayStep
	if q.Get("aggregate") == "hour" {
		step = time.Hour
	}
	if from.IsZero() || to.IsZero() || !to.After(from) {
		from, to = m.now.Add(-30*24*time.Hour), m.now
	}
	if step == dayStep {
		// Day buckets start at local midnight, as evcc aligns them.
		loc := from.In(berlin())
		from = time.Date(loc.Year(), loc.Month(), loc.Day(), 0, 0, 0, 0, berlin())
	} else {
		// The documentation picture shows a full day, not the minutes since
		// midnight.
		loc := m.now.In(berlin())
		from = time.Date(loc.Year(), loc.Month(), loc.Day(), 0, 0, 0, 0, berlin())
		to = from.Add(dayStep)
	}
	title := group
	if group == "loadpoint" {
		title = "Garage"
	}
	type point struct {
		Start        string  `json:"start"`
		Energy       float64 `json:"energy"`
		ReturnEnergy float64 `json:"returnEnergy"`
	}
	var data []point
	i := 0
	for t := from; t.Before(to); {
		i++
		if step == dayStep {
			t = from.AddDate(0, 0, i-1)
		} else {
			t = from.Add(time.Duration(i-1) * step)
		}
		e, ret := 0.0, 0.0
		switch group {
		case "pv":
			e = 8 + 14*maxf(0, sin(float64(t.YearDay())/366*6.283+1.2))
			if step == time.Hour {
				e = maxf(0, 4.2*sin((float64(t.Hour())-6)/12*3.14159))
			}
		case "home":
			e = 9 + 3*sin(float64(t.Hour())/24*6.283)
			if step == time.Hour {
				e = 0.6 + 0.4*sin((float64(t.Hour())-6)/12*3.14159)
			}
		case "grid":
			e = 4 + 3*sin(float64(t.YearDay())/30)
			ret = 6 + 4*sin(float64(t.YearDay())/40)
			if step == time.Hour {
				e, ret = 0.8, 1.4
			}
		case "battery":
			e = 3 + 2*sin(float64(t.YearDay())/25)
			ret = 2.5 + 2*sin(float64(t.YearDay())/33)
			if step == time.Hour {
				e, ret = 0.5, 0.7
			}
		case "loadpoint":
			if i%9 != 0 {
				e = 0
			} else {
				e = 14 + float64(i%5)
			}
			if step == time.Hour {
				e = 0
				if t.Hour() >= 10 && t.Hour() <= 14 {
					e = 3.4
				}
			}
		}
		// The current day bucket counts as far as the documentation moment
		// reaches; a picture taken after midnight would otherwise show 0. The
		// values keep the balance's two sides equal, so the T-account picture
		// does not suggest a bug: 24.5 + 2.1 + 3.1 = 9.6 + 8.8 + 4.2 + 7.1.
		if step == dayStep && sameDay(t, m.now) {
			switch group {
			case "pv":
				e = 24.5
			case "home":
				e = 9.6
			case "grid":
				e, ret = 2.1, 7.1
			case "battery":
				e, ret = 4.2, 3.1
			case "loadpoint":
				e = 8.8
			}
		}
		data = append(data, point{Start: t.Format(time.RFC3339), Energy: round3(e), ReturnEnergy: round3(ret)})
	}
	writeJSON(w, []any{map[string]any{"title": title, "group": group, "data": data}})
}

const dayStep = 24 * time.Hour

func sameDay(a, b time.Time) bool {
	loc := berlin()
	a, b = a.In(loc), b.In(loc)
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// ---- Myškoda ----

const skodaVIN = "TMBJB9NY5RF999999"

var timestampPattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z`)

func (m *mock) skoda(w http.ResponseWriter) {
	data, err := os.ReadFile(filepath.Join(m.root, "plugins", "cmd", "myskoda", "testdata", "enyaq.json"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data = timestampPattern.ReplaceAllFunc(data, func([]byte) []byte {
		return []byte(m.now.Add(-6 * time.Minute).UTC().Format("2006-01-02T15:04:05Z"))
	})
	data = regexp.MustCompile(`https://render\.example/[^"]*`).ReplaceAll(data, []byte(m.base+"/car.png"))
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// ---- Dexcom Share ----

const (
	dexAccount = "9b2f0000-1111-2222-3333-444455556666"
	dexSession = "5f1a7c62-1111-2222-3333-444455556666"
)

func (m *mock) dexcom(w http.ResponseWriter) {
	type reading struct {
		WT    string  `json:"WT"`
		ST    string  `json:"ST"`
		Value float64 `json:"Value"`
		Trend string  `json:"Trend"`
	}
	var out []reading
	// One day of five-minute values, ending two minutes ago.
	for i := 288; i >= 0; i-- {
		at := m.now.Add(-time.Duration(i) * 5 * time.Minute).Add(-2 * time.Minute)
		v := 128 + 32*sin(float64(i)/26) + float64((i*7)%9-4)
		// One peak and one dip a day, ramped over half an hour so the ranges
		// between them and the target band get readings too.
		if d := i - 166; d > -16 && d < 16 {
			a := d
			if a < 0 {
				a = -a
			}
			v += 130 * (1 - float64(a)/16)
		}
		if d := i - 44; d > -16 && d < 16 {
			a := d
			if a < 0 {
				a = -a
			}
			v -= 115 * (1 - float64(a)/16)
		}
		trend := "Flat"
		switch {
		case v < 70:
			trend = "FortyFiveDown"
		case v > 190:
			trend = "FortyFiveUp"
		}
		ms := at.UnixMilli()
		out = append(out, reading{
			WT:    fmt.Sprintf("/Date(%d)/", ms),
			ST:    fmt.Sprintf("/Date(%d)/", ms),
			Value: round1(v),
			Trend: trend,
		})
	}
	writeJSON(w, out)
}

// ---- Tankerkönig ----

func (m *mock) fuel(w http.ResponseWriter) {
	stations := []map[string]any{
		{"id": "station-1", "name": "Aral Tankstelle", "brand": "Aral", "street": "Hauptstraße 12", "place": "Münster", "dist": 0.4, "diesel": 1.789, "e5": 1.849, "e10": 1.829, "isOpen": true},
		{"id": "station-2", "name": "Shell Station", "brand": "Shell", "street": "Bahnhofsweg 3", "place": "Münster", "dist": 0.8, "diesel": 1.799, "e5": 1.859, "e10": 1.839, "isOpen": true},
		{"id": "station-3", "name": "Freie Tankstelle", "brand": "", "street": "Am Hafen 7", "place": "Münster", "dist": 1.6, "diesel": 1.769, "e5": 1.829, "e10": 1.809, "isOpen": true},
		{"id": "station-4", "name": "Esso", "brand": "Esso", "street": "Ringstraße 44", "place": "Münster", "dist": 2.3, "diesel": 1.829, "e5": 1.889, "e10": 1.869, "isOpen": true},
		{"id": "station-5", "name": "JET", "brand": "JET", "street": "Industriepark 9", "place": "Münster", "dist": 3.1, "diesel": 1.759, "e5": 1.819, "e10": 1.799, "isOpen": false},
		{"id": "station-6", "name": "Total Energies", "brand": "Total", "street": "Westallee 2", "place": "Münster", "dist": 4.2, "diesel": 1.839, "e5": 1.899, "e10": 1.879, "isOpen": true},
	}
	writeJSON(w, map[string]any{"ok": true, "stations": stations})
}

// ---- PrusaLink ----

// prusalink answers a printer in the middle of a print.
func (m *mock) prusalink(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/v1/status":
		writeJSON(w, map[string]any{
			"printer": map[string]any{
				"state": "PRINTING", "temp_nozzle": 214.9, "target_nozzle": 215.0,
				"temp_bed": 59.6, "target_bed": 60.0, "speed": 100, "flow": 95,
				"status_printer": map[string]any{"ok": true, "message": "OK"},
			},
			"job": map[string]any{"id": 42, "progress": 42.4, "time_remaining": 5025, "time_printing": 1325},
		})
	case "/api/v1/job":
		writeJSON(w, map[string]any{
			"id": 42, "state": "PRINTING", "progress": 42.4, "time_remaining": 5025, "time_printing": 1325,
			"file": map[string]any{
				"name": "BENCHY~1.GCO", "display_name": "Benchy_0.2mm_PLA_COREONE_1h23m.gcode",
				"path": "/usb", "display_path": "/Benchy",
			},
		})
	case "/api/v1/info":
		writeJSON(w, map[string]any{"name": "CORE One", "serial": "CZPX0000X000XC00000", "mmu": false})
	default:
		http.NotFound(w, r)
	}
}

// ---- xkcd ----

func (m *mock) xkcd(w http.ResponseWriter) {
	writeJSON(w, map[string]any{
		"month": "1", "num": 927, "link": "", "year": "2012", "news": "",
		"safe_title": "Standards", "transcript": "", "day": "4",
		"alt": "Fortunately, the charging one has been solved now that we've all standardized on mini-USB. Or is it micro-USB? Aaargh.",
		"img": m.base + "/comic.png", "title": "Standards",
	})
}

// ---- PokéAPI ----

var spritePattern = regexp.MustCompile(`https://raw\.githubusercontent\.com/[^"]*`)

func (m *mock) pokedex(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v2/")
	file := "pokemon-25.json"
	switch {
	case strings.HasPrefix(path, "pokemon-species/"):
		file = "species-pikachu.json"
	case strings.HasPrefix(path, "type/"):
		file = "type-electric.json"
	}
	data, err := os.ReadFile(filepath.Join(m.root, "plugins", "cmd", "pokedex", "testdata", file))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data = spritePattern.ReplaceAll(data, []byte(m.base+"/artwork.png"))
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// ---- RSS ----

func (m *mock) feed(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSuffix(filepath.Base(r.URL.Path), ".xml")
	type item struct {
		title, summary string
		age            time.Duration
	}
	var items []item
	switch name {
	case "heise":
		items = []item{
			{"Neue Batteriezellen kommen ohne Kobalt aus", "Ein Hersteller zeigt eine Zelle für E-Autos, die vollständig auf Kobalt verzichtet.", 22 * time.Minute},
			{"Open Source: Werkzeuge für den Heimserver", "Eine Übersicht über Selbstbau-Projekte für Daten, Fotos und Medien.", 2 * time.Hour},
			{"Wärmepumpen im Altbau: Was wirklich zählt", "Praxistest mit fünf Häusern aus den Jahren 1955 bis 1998.", 5 * time.Hour},
			{"Sicherheitslücke in Router-Firmware geschlossen", "Das Update ist ab sofort verfügbar.", 9 * time.Hour},
		}
	default:
		items = []item{
			{"Bundestag berät über neues Klimapaket", "Die Debatte beginnt am Mittag, eine Entscheidung fällt am Abend.", 12 * time.Minute},
			{"Bahnstrecke nach der Sperrung wieder offen", "Seit heute Morgen rollen die Züge wieder auf der Hauptstrecke.", 48 * time.Minute},
			{"Städte bauen mehr Radwege", "Ein Programm des Bundes fördert 300 Kilometer neue Wege.", 3 * time.Hour},
			{"Wetter: Sonne am Wochenende", "Nach einem regnerischen Freitag wird es am Samstag freundlicher.", 6 * time.Hour},
		}
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel>`)
	fmt.Fprintf(&b, "<title>%s</title><link>%s</link><description>Demo feed</description>", strings.Title(name), m.base)
	for _, it := range items {
		fmt.Fprintf(&b, "<item><title>%s</title><link>%s/%s</link><description>%s</description><pubDate>%s</pubDate></item>",
			xmlEscape(it.title), m.base, name, xmlEscape(it.summary),
			m.now.Add(-it.age).Format(time.RFC1123Z))
	}
	b.WriteString("</channel></rss>")
	w.Header().Set("Content-Type", "application/rss+xml")
	fmt.Fprint(w, b.String())
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

// ---- CalDAV and CardDAV ----

func (m *mock) caldav(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	if r.Method == "PROPFIND" {
		w.WriteHeader(http.StatusMultiStatus)
		switch path {
		case "/cal":
			fmt.Fprintf(w, `<?xml version="1.0"?><multistatus xmlns="DAV:"><response><href>%s/cal</href><propstat><prop><current-user-principal><href>%s/cal/1234/principal/</href></current-user-principal></prop><status>HTTP/1.1 200 OK</status></propstat></response></multistatus>`, m.base, m.base)
		case "/cal/1234/principal":
			fmt.Fprintf(w, `<?xml version="1.0"?><multistatus xmlns="DAV:"><response><href>%s/cal/1234/principal/</href><propstat><prop><calendar-home-set xmlns="urn:ietf:params:xml:ns:caldav"><href xmlns="DAV:">%s/cal/1234/calendars/</href></calendar-home-set></prop><status>HTTP/1.1 200 OK</status></propstat></response></multistatus>`, m.base, m.base)
		case "/cal/1234/calendars":
			fmt.Fprintf(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
<d:response><d:href>/cal/1234/calendars/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
<d:response><d:href>%s/cal/1234/calendars/home/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/><c:calendar/></d:resourcetype><d:displayname>Privat</d:displayname><c:supported-calendar-component-set><c:comp name="VEVENT"/></c:supported-calendar-component-set></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
<d:response><d:href>%s/cal/1234/calendars/work/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/><c:calendar/></d:resourcetype><d:displayname>Arbeit</d:displayname><c:supported-calendar-component-set><c:comp name="VEVENT"/></c:supported-calendar-component-set></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
</d:multistatus>`, m.base, m.base)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
		return
	}
	if r.Method == "REPORT" {
		var data string
		switch path {
		case "/cal/1234/calendars/home":
			data = m.privateICS()
		case "/cal/1234/calendars/work":
			data = m.workICS()
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusMultiStatus)
		fmt.Fprintf(w, `<?xml version="1.0"?><multistatus xmlns="DAV:"><response><href>%s/event.ics</href><propstat><prop><calendar-data xmlns="urn:ietf:params:xml:ns:caldav">%s</calendar-data></prop><status>HTTP/1.1 200 OK</status></propstat></response></multistatus>`, path, xmlEscape(data))
		return
	}
	w.WriteHeader(http.StatusMethodNotAllowed)
}

func (m *mock) carddav(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	if r.Method == "PROPFIND" {
		w.WriteHeader(http.StatusMultiStatus)
		switch path {
		case "/card":
			fmt.Fprintf(w, `<?xml version="1.0"?><multistatus xmlns="DAV:"><response><href>%s/card/</href><propstat><prop><current-user-principal><href>%s/card/1234/principal/</href></current-user-principal></prop><status>HTTP/1.1 200 OK</status></propstat></response></multistatus>`, m.base, m.base)
		case "/card/1234/principal":
			fmt.Fprintf(w, `<?xml version="1.0"?><multistatus xmlns="DAV:" xmlns:c="urn:ietf:params:xml:ns:carddav"><response><href>%s/card/1234/principal/</href><propstat><prop><c:addressbook-home-set><href>/card/1234/addressbooks/</href></c:addressbook-home-set></prop><status>HTTP/1.1 200 OK</status></propstat></response></multistatus>`, m.base)
		case "/card/1234/addressbooks":
			fmt.Fprintf(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:carddav">
<d:response><d:href>%s/card/1234/addressbooks/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
<d:response><d:href>%s/card/1234/addressbooks/all/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/><c:addressbook/></d:resourcetype><d:displayname>Kontakte</d:displayname></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
</d:multistatus>`, m.base, m.base)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
		return
	}
	if r.Method == "REPORT" && path == "/card/1234/addressbooks/all" {
		w.WriteHeader(http.StatusMultiStatus)
		fmt.Fprintf(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:carddav"><d:response><d:href>/card/1234/addressbooks/all/card.vcf</d:href><d:propstat><d:prop><c:address-data>%s</c:address-data></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`, xmlEscape(m.vcards()))
		return
	}
	w.WriteHeader(http.StatusMethodNotAllowed)
}

func berlin() *time.Location {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		return time.FixedZone("CET", 3600)
	}
	return loc
}

func (m *mock) privateICS() string {
	loc := berlin()
	now := m.now.In(loc)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//paperboy//screenshots//EN\r\n")
	add := func(uid, summary, location string, start, end time.Time, allDay bool) {
		if allDay {
			fmt.Fprintf(&b, "BEGIN:VEVENT\r\nUID:%s\r\nDTSTART;VALUE=DATE:%s\r\nDTEND;VALUE=DATE:%s\r\nSUMMARY:%s\r\nEND:VEVENT\r\n",
				uid, start.Format("20060102"), end.Format("20060102"), summary)
			return
		}
		fmt.Fprintf(&b, "BEGIN:VEVENT\r\nUID:%s\r\nDTSTART;TZID=Europe/Berlin:%s\r\nDTEND;TZID=Europe/Berlin:%s\r\nSUMMARY:%s\r\n", uid, start.Format("20060102T150405"), end.Format("20060102T150405"), summary)
		if location != "" {
			fmt.Fprintf(&b, "LOCATION:%s\r\n", location)
		}
		b.WriteString("END:VEVENT\r\n")
	}
	add("1", "Zahnarzt", "Praxis Dr. Meyer", day.Add(15*time.Hour), day.Add(16*time.Hour), false)
	add("2", "Sport", "Vereinshalle", day.AddDate(0, 0, 1).Add(18*time.Hour), day.AddDate(0, 0, 1).Add(19*time.Hour+30*time.Minute), false)
	add("3", "Müll rausbringen", "", day.AddDate(0, 0, 2), day.AddDate(0, 0, 3), true)
	add("4", "Elternabend", "Schule", day.AddDate(0, 0, 5).Add(19*time.Hour), day.AddDate(0, 0, 5).Add(21*time.Hour), false)
	add("5", "Kino", "Cineplex", day.AddDate(0, 0, 8).Add(20*time.Hour), day.AddDate(0, 0, 8).Add(22*time.Hour), false)
	b.WriteString("END:VCALENDAR\r\n")
	return b.String()
}

func (m *mock) workICS() string {
	loc := berlin()
	now := m.now.In(loc)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//paperboy//screenshots//EN\r\n")
	// The next weekday for the standup.
	d := day
	for d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
		d = d.AddDate(0, 0, 1)
	}
	fmt.Fprintf(&b, "BEGIN:VEVENT\r\nUID:10\r\nDTSTART;TZID=Europe/Berlin:%s\r\nDURATION:PT15M\r\nRRULE:FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR\r\nSUMMARY:Standup\r\nEND:VEVENT\r\n", d.Add(9*time.Hour).Format("20060102T150405"))
	fmt.Fprintf(&b, "BEGIN:VEVENT\r\nUID:11\r\nDTSTART;TZID=Europe/Berlin:%s\r\nDTEND;TZID=Europe/Berlin:%s\r\nSUMMARY:Projektbesprechung\r\nLOCATION:Raum 2.14\r\nEND:VEVENT\r\n",
		day.AddDate(0, 0, 1).Add(10*time.Hour).Format("20060102T150405"), day.AddDate(0, 0, 1).Add(11*time.Hour+30*time.Minute).Format("20060102T150405"))
	fmt.Fprintf(&b, "BEGIN:VEVENT\r\nUID:12\r\nDTSTART;TZID=Europe/Berlin:%s\r\nDTEND;TZID=Europe/Berlin:%s\r\nSUMMARY:Reise nach Hamburg\r\nEND:VEVENT\r\n",
		day.AddDate(0, 0, 9).Add(8*time.Hour).Format("20060102T150405"), day.AddDate(0, 0, 9).Add(17*time.Hour).Format("20060102T150405"))
	b.WriteString("END:VCALENDAR\r\n")
	return b.String()
}

func (m *mock) vcards() string {
	now := m.now
	birthday := func(days int, year int) string {
		t := now.AddDate(0, 0, days)
		return fmt.Sprintf("%04d-%02d-%02d", year, int(t.Month()), t.Day())
	}
	card := func(uid, name, bday string) string {
		return "BEGIN:VCARD\r\nVERSION:3.0\r\n" +
			"UID:" + uid + "\r\n" +
			"FN:" + name + "\r\n" +
			"N:" + name + ";;;;\r\n" +
			"BDAY:" + bday + "\r\n" +
			"END:VCARD\r\n"
	}
	return card("anna", "Anna Beispiel", birthday(3, 1985)) +
		card("ben", "Ben Weber", birthday(9, 1992)) +
		card("clara", "Clara Schmidt", birthday(17, 1978))
}

// ---- EFA departures ----

func (m *mock) efa(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch filepath.Base(r.URL.Path) {
	case "XML_STOPFINDER_REQUEST":
		writeJSON(w, map[string]any{"version": "11.0.6.72", "locations": []any{map[string]any{
			"id": "de:00000:1001", "isGlobalId": true, "name": "Demo, Marktplatz",
			"disassembledName": "Marktplatz", "type": "stop", "isBest": true,
			"parent": map[string]any{"name": "Demo", "type": "locality"},
		}}})
	case "XML_DM_REQUEST":
		loc := berlin()
		now := m.now.In(loc)
		lines := []struct {
			line, dest string
			minutes    int
			delay      int
			platform   string
		}{
			{"4", "Hauptbahnhof", 3, 1, "1"},
			{"1", "Rathaus", 6, 0, "2"},
			{"S 5", "Zentralstation", 11, 2, "3"},
			{"4", "Hauptbahnhof", 14, 0, "1"},
			{"R 82", "Flughafen", 19, 0, "4"},
			{"1", "Rathaus", 23, 3, "2"},
			{"N 2", "Stadtpark", 27, 0, "1"},
		}
		var events []any
		for _, l := range lines {
			planned := now.Add(time.Duration(l.minutes) * time.Minute).Truncate(time.Minute)
			event := map[string]any{
				"realtimeStatus":       []string{"MONITORED"},
				"departureTimePlanned": planned.Format(time.RFC3339),
				"location":             map[string]any{"properties": map[string]any{"platform": l.platform}},
				"transportation": map[string]any{
					"number": l.line, "disassembledName": l.line,
					"destination": map[string]any{"name": "Demo, " + l.dest},
				},
			}
			if l.delay > 0 {
				event["departureTimeEstimated"] = planned.Add(time.Duration(l.delay) * time.Minute).Format(time.RFC3339)
			}
			events = append(events, event)
		}
		writeJSON(w, map[string]any{
			"version": "11.0.6.72",
			"locations": []any{map[string]any{
				"id": "de:00000:1001", "name": "Demo, Marktplatz", "disassembledName": "Marktplatz",
				"type": "stop", "parent": map[string]any{"name": "Demo", "type": "locality"},
			}},
			"stopEvents": events,
		})
	default:
		http.NotFound(w, r)
	}
}

// ---- UniFi ----

// unifi answers the integration API and the private health endpoint of a
// controller with a small home network.
func (m *mock) unifi(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/proxy/network/integration/v1/sites":
		writeJSON(w, map[string]any{
			"offset": 0, "limit": 25, "count": 1, "totalCount": 1,
			"data": []any{map[string]any{
				"id":   "11111111-1111-1111-1111-111111111111",
				"name": "Zuhause", "internalReference": "default",
			}},
		})
	case strings.HasSuffix(r.URL.Path, "/devices"):
		writeJSON(w, map[string]any{
			"offset": 0, "limit": 25, "count": 2, "totalCount": 2,
			"data": []any{
				map[string]any{"id": "01", "name": "Keller-AP", "model": "U7PG2", "state": "ONLINE",
					"firmwareVersion": "6.6.55", "firmwareUpdatable": false, "ipAddress": "192.168.1.55"},
				map[string]any{"id": "02", "name": "OG-AP", "model": "U7PG2", "state": "ONLINE",
					"firmwareVersion": "6.6.55", "firmwareUpdatable": true, "ipAddress": "192.168.1.56"},
			},
		})
	case strings.HasSuffix(r.URL.Path, "/clients"):
		writeJSON(w, map[string]any{
			"offset": 0, "limit": 1, "count": 1, "totalCount": 74,
			"data": []any{map[string]any{}},
		})
	case strings.HasSuffix(r.URL.Path, "/stat/health"):
		writeJSON(w, map[string]any{
			"meta": map[string]any{"rc": "ok"},
			"data": []any{
				map[string]any{"subsystem": "wan", "status": "ok", "wan_ip": "203.0.113.9",
					"num_adopted": 1, "num_disconnected": 0, "num_sta": 74,
					"tx_bytes-r": 4961, "rx_bytes-r": 4976,
					"gw_name": "Feldberg 7a", "isp_name": "Deutsche Telekom AG", "asn": 3320},
				map[string]any{"subsystem": "www", "status": "ok", "latency": 10,
					"uptime": 237437, "drops": 1, "xput_up": 50.2, "xput_down": 250.5,
					"speedtest_ping": 8.0, "speedtest_lastrun": 1795810000,
					"speedtest_status": "Success"},
			},
		})
	default:
		http.NotFound(w, r)
	}
}

// ---- placeholder art ----
// placeholder draws the stand-in for the xkcd panel and the Pokémon sprite.
// The repository ships no third-party art, so the pictures of those two
// plugins show a drawn placeholder.
func (m *mock) placeholder(w http.ResponseWriter, kind string) {
	var img *image.RGBA
	switch kind {
	case "comic":
		img = comicArt()
	case "car":
		img = carArt()
	default:
		img = creatureArt()
	}
	w.Header().Set("Content-Type", "image/png")
	png.Encode(w, img)
}

// carArt is the side view the myskoda card shows.
func carArt() *image.RGBA {
	const w, h = 640, 300
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	body := color.RGBA{90, 140, 190, 255}
	dark := color.RGBA{40, 60, 80, 255}
	glass := color.RGBA{200, 225, 240, 255}
	ellipse(img, 320, 190, 230, 55, body)
	for x := 170; x < 450; x++ {
		for y := 110; y < 180; y++ {
			dx, dy := float64(x-310)/150, float64(y-190)/90
			if dx*dx+dy*dy <= 1 && y < 165 {
				img.Set(x, y, body)
			}
		}
	}
	// Windows.
	for x := 190; x < 290; x++ {
		for y := 125; y < 160; y++ {
			img.Set(x, y, glass)
		}
	}
	for x := 300; x < 420; x++ {
		for y := 125; y < 160; y++ {
			img.Set(x, y, glass)
		}
	}
	// Wheels.
	circle(img, 200, 240, 44, dark)
	circle(img, 200, 240, 20, color.RGBA{200, 200, 200, 255})
	circle(img, 450, 240, 44, dark)
	circle(img, 450, 240, 20, color.RGBA{200, 200, 200, 255})
	lineRect(img, 100, 185, 90, 175, 6, color.RGBA{240, 220, 120, 255})
	lineRect(img, 530, 185, 540, 180, 6, color.RGBA{220, 80, 80, 255})
	return img
}

func comicArt() *image.RGBA {
	const w, h = 740, 300
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	black := color.RGBA{30, 30, 30, 255}
	border := func(r image.Rectangle) {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.Set(x, r.Min.Y, black)
			img.Set(x, r.Max.Y-1, black)
		}
		for y := r.Min.Y; y < r.Max.Y; y++ {
			img.Set(r.Min.X, y, black)
			img.Set(r.Max.X-1, y, black)
		}
	}
	line := func(x0, y0, x1, y1 int) {
		steps := maxi(abs(x1-x0), abs(y1-y0))
		for i := 0; i <= steps; i++ {
			x := x0 + (x1-x0)*i/maxi(steps, 1)
			y := y0 + (y1-y0)*i/maxi(steps, 1)
			for dx := 0; dx < 3; dx++ {
				for dy := 0; dy < 3; dy++ {
					img.Set(x+dx, y+dy, black)
				}
			}
		}
	}
	border(image.Rect(10, 10, 240, 290))
	border(image.Rect(250, 10, 480, 290))
	border(image.Rect(490, 10, 730, 290))
	// Three rough panels: a diagram, a person and a machine.
	line(40, 220, 200, 220)
	line(60, 60, 120, 140)
	line(120, 140, 180, 70)
	line(120, 140, 120, 220)
	// A stick figure.
	circle(img, 365, 90, 28, black)
	line(365, 118, 365, 210)
	line(365, 150, 320, 190)
	line(365, 150, 410, 190)
	line(365, 210, 330, 260)
	line(365, 210, 400, 260)
	// A box with a lever.
	border(image.Rect(550, 90, 680, 250))
	line(615, 90, 615, 40)
	circle(img, 615, 34, 12, black)
	return img
}

func creatureArt() *image.RGBA {
	const w, h = 475, 475
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	body := color.RGBA{240, 196, 40, 255}
	dark := color.RGBA{90, 64, 20, 255}
	ellipse(img, 237, 300, 130, 110, body)
	ellipse(img, 237, 160, 105, 90, body)
	// Ears.
	triangle(img, 160, 120, 100, 10, 205, 75, body)
	triangle(img, 314, 120, 374, 10, 269, 75, body)
	// Cheeks and eyes.
	circle(img, 205, 155, 10, dark)
	circle(img, 269, 155, 10, dark)
	circle(img, 180, 205, 16, color.RGBA{230, 110, 90, 255})
	circle(img, 294, 205, 16, color.RGBA{230, 110, 90, 255})
	// Tail.
	lineRect(img, 340, 340, 420, 250, 22, body)
	return img
}

func circle(img *image.RGBA, cx, cy, r int, c color.Color) {
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			if (x-cx)*(x-cx)+(y-cy)*(y-cy) <= r*r {
				img.Set(x, y, c)
			}
		}
	}
}

func ellipse(img *image.RGBA, cx, cy, rx, ry int, c color.Color) {
	for y := cy - ry; y <= cy+ry; y++ {
		for x := cx - rx; x <= cx+rx; x++ {
			dx, dy := float64(x-cx)/float64(rx), float64(y-cy)/float64(ry)
			if dx*dx+dy*dy <= 1 {
				img.Set(x, y, c)
			}
		}
	}
}

func triangle(img *image.RGBA, x0, y0, x1, y1, x2, y2 int, c color.Color) {
	minX, maxX := minInt(x0, minInt(x1, x2)), maxInt(x0, maxInt(x1, x2))
	minY, maxY := minInt(y0, minInt(y1, y2)), maxInt(y0, maxInt(y1, y2))
	sign := func(ax, ay, bx, by, cx, cy int) int {
		return (ax-cx)*(by-cy) - (bx-cx)*(ay-cy)
	}
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			d1, d2, d3 := sign(x, y, x0, y0, x1, y1), sign(x, y, x1, y1, x2, y2), sign(x, y, x2, y2, x0, y0)
			neg := d1 < 0 || d2 < 0 || d3 < 0
			pos := d1 > 0 || d2 > 0 || d3 > 0
			if !(neg && pos) {
				img.Set(x, y, c)
			}
		}
	}
}

// lineRect draws a thick line as a rotated rectangle.
func lineRect(img *image.RGBA, x0, y0, x1, y1, width int, c color.Color) {
	steps := maxi(abs(x1-x0), abs(y1-y0))
	for i := 0; i <= steps; i++ {
		x := x0 + (x1-x0)*i/maxi(steps, 1)
		y := y0 + (y1-y0)*i/maxi(steps, 1)
		circle(img, x, y, width/2, c)
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxi(a, b int) int { return maxInt(a, b) }
