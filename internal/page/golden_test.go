package page

import (
	"bytes"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maehld/paperboy/internal/data"
)

// fixedGoldenNow is the clock of the golden fixtures. It is deliberately not
// time.Now: the images and their hashes must be reproducible.
var fixedGoldenNow = time.Date(2026, 10, 21, 14, 20, 0, 0, time.UTC)

// referencePages lists the reference pages per display type: the four SolarAI
// pages and the weather page fed by an exec provider (ADR 0036).
var referencePages = []struct {
	name    string
	display string
}{
	{"solar-overview", "e1001"},
	{"solar-overview", "sticky"},
	{"solar-battery", "e1001"},
	{"solar-battery", "sticky"},
	{"solar-balance", "e1001"},
	{"solar-balance", "sticky"},
	{"solar-history", "e1001"},
	{"solar-history", "sticky"},
	{"wetter", "e1001"},
	{"wetter", "sticky"},
}

// goldenDocument builds the deterministic data document of the reference pages.
func goldenDocument(t *testing.T) *data.Document {
	t.Helper()
	doc, err := data.New(filepath.Join(t.TempDir(), "data.json"), func() time.Time {
		return fixedGoldenNow
	})
	if err != nil {
		t.Fatal(err)
	}
	putGolden(t, doc, "solar/current/latest", map[string]any{
		"time": "2026-10-21T14:20:00Z",
		"values": map[string]any{
			"pv_power":                4210.0,
			"load_power":              820.0,
			"grid_power":              -1330.0,
			"battery_soc":             81.5,
			"battery_power":           -1240.0,
			"battery_temperature":     22.4,
			"battery_soh":             97.0,
			"pv_today":                12.4,
			"import_today":            1.2,
			"export_today":            4.8,
			"battery_charge_today":    3.1,
			"battery_discharge_today": 2.7,
		},
	})
	putGolden(t, doc, "solar/history/points", goldenHistoryPoints())
	putGolden(t, doc, "solar/derived/balance", map[string]any{
		"consumption":      9.8,
		"autarky":          0.87,
		"self_consumption": 0.64,
		"approximate":      false,
	})
	putGolden(t, doc, "solar/derived/battery_remaining", map[string]any{
		"seconds":  12000,
		"charging": true,
	})
	putGolden(t, doc, "weather", goldenWeather())
	return doc
}

// goldenWeather is the weather branch as cmd/weather writes it (ADR 0036).
func goldenWeather() map[string]any {
	days := make([]any, 0, 5)
	conditions := []struct{ code, text string }{
		{"3", "Bedeckt"}, {"61", "Leichter Regen"}, {"2", "Teilweise bewölkt"},
		{"0", "Klar"}, {"80", "Leichte Regenschauer"},
	}
	labels := []string{"Mi", "Do", "Fr", "Sa", "So"}
	for i := range conditions {
		days = append(days, map[string]any{
			"date":        time.Date(2026, 10, 21+i, 0, 0, 0, 0, time.UTC).Format("2006-01-02"),
			"label":       labels[i],
			"min":         8.5 + float64(i),
			"max":         14.5 + float64(i),
			"precip_prob": 20 + 10*i,
			"text":        conditions[i].text,
		})
	}
	return map[string]any{
		"updated": "2026-10-21T14:20:00Z",
		"current": map[string]any{
			"time": "2026-10-21T14:20:00Z", "temp": 13.4, "apparent": 11.9,
			"humidity": 72, "precip": 0.2, "precip_prob": 35, "wind": 12.5,
			"beaufort": 3, "code": 3, "is_day": true, "symbol": "cloud", "text": "Bedeckt",
		},
		"today": map[string]any{
			"min": 8.5, "max": 15.6, "precip_prob": 60,
			"sunrise": "2026-10-21T05:45:00Z", "sunset": "2026-10-21T15:52:00Z",
			"code": 61, "text": "Leichter Regen",
		},
		"days":  days,
		"hours": goldenWeatherHours(),
	}
}

// goldenWeatherHours returns 24 hourly points from midnight to midnight with a
// temperature curve and a precipitation probability that follows it.
func goldenWeatherHours() []any {
	midnight := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	points := make([]any, 0, 24)
	for i := 0; i < 24; i++ {
		temp := 9.2 + 4.5*math.Sin((float64(i)-7)/24*2*math.Pi)
		prob := math.Max(0, 70-6*float64(i))
		points = append(points, map[string]any{
			"time": midnight.Add(time.Duration(i) * time.Hour).Format(time.RFC3339),
			"values": map[string]any{
				"temp":        math.Round(temp*10) / 10,
				"precip_prob": math.Round(prob),
			},
		})
	}
	return points
}

func putGolden(t *testing.T, doc *data.Document, path string, v any) {
	t.Helper()
	if err := doc.Put(path, v, fixedGoldenNow); err != nil {
		t.Fatal(err)
	}
}

// goldenHistoryPoints returns one point every 10 minutes from midnight to the
// fixture time: a smooth solar day curve and a load with morning and evening
// bumps.
func goldenHistoryPoints() []any {
	midnight := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	points := make([]any, 0, 87)
	for i := 0; i <= 86; i++ {
		minutes := i * 10
		pv := solarCurve(minutes)
		load := 350 + 35*math.Sin(float64(minutes)/60*math.Pi/3)
		load += 450 * math.Exp(-math.Pow(float64(minutes)-420, 2)/(2*60*60))
		load += 600 * math.Exp(-math.Pow(float64(minutes)-1140, 2)/(2*90*90))
		points = append(points, map[string]any{
			"time": midnight.Add(time.Duration(minutes) * time.Minute).Format(time.RFC3339),
			"values": map[string]any{
				"pv_power":   math.Round(pv),
				"load_power": math.Round(load),
			},
		})
	}
	return points
}

// solarCurve rises from 06:00 to a peak of 5200 W at 12:30 and falls back to
// zero at 18:00.
func solarCurve(minutes int) float64 {
	switch {
	case minutes < 360 || minutes > 1080:
		return 0
	case minutes <= 750:
		return 5200 * math.Sin(math.Pi/2*float64(minutes-360)/390)
	default:
		return 5200 * math.Sin(math.Pi/2*float64(1080-minutes)/330)
	}
}

// TestGoldenPages renders every reference page and compares it with the
// committed PNG. Run with UPDATE_GOLDEN=1 to rewrite the expected images.
func TestGoldenPages(t *testing.T) {
	doc := goldenDocument(t)
	const dir = "../../examples/pages"
	for _, p := range referencePages {
		t.Run(p.name+"."+p.display, func(t *testing.T) {
			tmpl, err := Load(dir, p.name, p.display)
			if err != nil {
				t.Fatal(err)
			}
			img, err := tmpl.RenderContext(doc, Display{
				Width:    800,
				Height:   480,
				Gray:     true,
				Language: "de",
				Timezone: time.UTC,
			}, Context{Now: fixedGoldenNow, Page: 1, Pages: 1})
			if err != nil {
				t.Fatal(err)
			}

			var buf bytes.Buffer
			enc := png.Encoder{CompressionLevel: png.BestCompression}
			if err := enc.Encode(&buf, img); err != nil {
				t.Fatal(err)
			}

			name := p.name + "." + p.display + ".png"
			golden := filepath.Join("testdata", "golden", name)
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("missing golden image %s: run with UPDATE_GOLDEN=1", golden)
			}
			if bytes.Equal(buf.Bytes(), want) {
				return
			}
			actual := filepath.Join("testdata", "golden", p.name+"."+p.display+".actual.png")
			if err := os.WriteFile(actual, buf.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Fatalf("image differs from %s; inspect %s and rerun with UPDATE_GOLDEN=1 if the change is intended",
				golden, actual)
		})
	}
}

// TestReferencePagesParse loads all reference templates to catch template
// syntax and validation errors without rendering.
func TestReferencePagesParse(t *testing.T) {
	const dir = "../../examples/pages"
	for _, p := range referencePages {
		t.Run(p.name+"."+p.display, func(t *testing.T) {
			tmpl, err := Load(dir, p.name, p.display)
			if err != nil {
				t.Fatal(err)
			}
			if len(tmpl.Rows) != len(tmpl.Blocks) || len(tmpl.Blocks) == 0 {
				t.Fatalf("page has %d rows and %d blocks", len(tmpl.Rows), len(tmpl.Blocks))
			}
		})
	}
}
