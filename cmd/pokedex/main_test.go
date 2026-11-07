package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

// fixture serves recorded PokéAPI answers (testdata, trimmed to the used
// fields) and a generated picture instead of the real artwork.
func fixture(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	file := func(name string, w http.ResponseWriter, replace string) {
		b, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		s := strings.ReplaceAll(string(b), "https://raw.githubusercontent.com", replace)
		w.Write([]byte(s))
	}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/api/pokemon/25", "/api/pokemon/pikachu":
			file("pokemon-25.json", w, srv.URL)
		case "/api/pokemon-species/pikachu":
			file("species-pikachu.json", w, srv.URL)
		case "/api/type/electric":
			file("type-electric.json", w, srv.URL)
		case "/PokeAPI/sprites/master/sprites/pokemon/other/official-artwork/25.png":
			img := image.NewNRGBA(image.Rect(0, 0, 120, 120))
			for y := 0; y < 120; y++ {
				for x := 0; x < 120; x++ {
					if (x-60)*(x-60)+(y-60)*(y-60) < 2500 {
						img.Set(x, y, color.NRGBA{R: uint8(x * 2), G: uint8(200 - y), B: 30, A: 255})
					}
				}
			}
			png.Encode(w, img)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func configured(t *testing.T, cfg map[string]any) (*pokedex, *atomic.Int32) {
	t.Helper()
	srv, calls := fixture(t)
	cfg["base"] = srv.URL + "/api"
	p := newPokedex(srv.Client())
	p.gap = 0
	p.now = func() time.Time { return time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC) }
	if err := p.Configure(cfg, &plugin.Request{Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	return p, calls
}

func request(lang string, w, h, ppi int) (*plugin.Request, plugin.WidgetRequest) {
	px := 16 * float64(ppi) / 72
	return &plugin.Request{Language: lang, Timezone: "UTC", Levels: 4, Page: plugin.PageInfo{Width: 800, Height: 480, PPI: float64(ppi)}},
		plugin.WidgetRequest{Widget: "card", Width: w, Height: h,
			Style: plugin.Style{Padding: int(4.5*float64(ppi)/72 + 0.5), Rule: 1, TextPx: px}}
}

func TestConfigure(t *testing.T) {
	bad := map[string]map[string]any{
		"fixed without pokemon": {"mode": "fixed"},
		"unknown mode":          {"mode": "daily"},
		"rotate too short":      {"rotate": "5m"},
		"generation 0":          {"generations": []any{0.0}},
		"generation 10":         {"generations": []any{10.0}},
		"unknown key":           {"pokmon": "pikachu"},
		"path in name":          {"pokemon": "../x"},
	}
	for name, cfg := range bad {
		p := newPokedex(nil)
		if err := p.Configure(cfg, &plugin.Request{}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	p := newPokedex(nil)
	if err := p.Configure(plugin.Values{"pokemon": "Pikachu"}, &plugin.Request{}); err != nil || p.fixed != "pikachu" {
		t.Fatalf("fixed default: %v %q", err, p.fixed)
	}
}

func TestRotation(t *testing.T) {
	p := newPokedex(nil)
	if err := p.Configure(plugin.Values{"generations": []any{1.0, 2.0}}, &plugin.Request{Timezone: "Europe/Berlin"}); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 11, 7, 3, 0, 0, 0, time.UTC)
	seen := map[string]bool{}
	for i := 0; i < 40; i++ {
		at := day.AddDate(0, 0, i)
		k := p.key(at)
		if k != p.key(at.Add(5*time.Hour)) {
			t.Fatal("pick changed within a day")
		}
		id, _ := atoiOK(k)
		if id < 1 || id > 251 {
			t.Fatalf("id %d outside generations 1 and 2", id)
		}
		seen[k] = true
	}
	if len(seen) < 10 {
		t.Errorf("only %d different picks in 40 days", len(seen))
	}
	// Daily rotation follows local midnight (UTC+1 in November).
	next := p.nextRotation(time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC))
	if want := time.Date(2026, 11, 7, 23, 0, 0, 0, time.UTC); !next.Equal(want) {
		t.Errorf("next rotation %v, want %v", next, want)
	}
}

func atoiOK(s string) (int, bool) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

func TestLoadCachesResponses(t *testing.T) {
	p, calls := configured(t, map[string]any{"pokemon": "25"})
	for i := 0; i < 3; i++ {
		m, err := p.load(context.Background(), "25")
		if err != nil {
			t.Fatal(err)
		}
		if m.name != "pikachu" || m.stats[5] != 90 || m.types[0] != "electric" || m.art == nil {
			t.Fatalf("unexpected mon %+v", m)
		}
	}
	// pokemon, species, type, artwork: once each.
	if calls.Load() != 4 {
		t.Errorf("%d requests, want 4", calls.Load())
	}
	if _, err := p.load(context.Background(), "9999"); err == nil {
		t.Error("unknown pokemon: no error")
	}
}

func TestLanguages(t *testing.T) {
	p, _ := configured(t, map[string]any{"pokemon": "pikachu"})
	m, err := p.load(context.Background(), "pikachu")
	if err != nil {
		t.Fatal(err)
	}
	if m.displayName("de") != "Pikachu" || m.typeName("electric", "de") != "Elektro" || m.typeName("electric", "en") != "Electric" {
		t.Errorf("names: %q %q %q", m.displayName("de"), m.typeName("electric", "de"), m.typeName("electric", "en"))
	}
	if m.displayName("fr") != m.displayName("en") {
		t.Error("unknown language does not fall back to English")
	}
	de, en := m.flavorText("de"), m.flavorText("en")
	if de == "" || en == "" || de == en || strings.ContainsAny(de+en, "\n\f") {
		t.Errorf("flavor texts: %q / %q", de, en)
	}
}

func TestText(t *testing.T) {
	p, _ := configured(t, map[string]any{"pokemon": "25"})
	req, w := request("de", 20, 4, 0)
	req.Page = plugin.PageInfo{Width: 20, Height: 4}
	if err := p.Prepare(req); err != nil {
		t.Fatal(err)
	}
	lines, err := p.Text(req, w)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Pikachu         #0025", "Elektro   0,4m·6,0kg", "KP35 Ang55 Vert40", "SpA50 SpV50 Init90"}
	if len(lines) < 4 {
		t.Fatalf("lines %q", lines)
	}
	for i, l := range want {
		if i < 2 {
			continue // widths of the first two lines are checked below
		}
		if lines[i] != l {
			t.Errorf("line %d = %q, want %q", i, lines[i], l)
		}
	}
	for _, l := range lines {
		if len([]rune(l)) > 20 {
			t.Errorf("line %q wider than 20", l)
		}
	}
	if !strings.HasPrefix(lines[0], "Pikachu") || !strings.HasSuffix(lines[0], "#0025") || !strings.Contains(lines[1], "Elektro") {
		t.Errorf("header lines %q", lines[:2])
	}
}

func TestRenderCard(t *testing.T) {
	p, _ := configured(t, map[string]any{"pokemon": "25"})
	for _, tc := range []struct {
		name         string
		ppi, w, h, l int
	}{
		{"e1001 2", 125, 800, 480, 2}, {"e1001 4", 125, 400, 240, 4},
		{"sticky 2", 236, 400, 240, 2}, {"sticky 4", 236, 800, 480, 4},
		{"narrow", 125, 200, 300, 4},
	} {
		req, w := request("en", tc.w, tc.h, tc.ppi)
		req.Levels = tc.l
		if err := p.Prepare(req); err != nil {
			t.Fatal(err)
		}
		img := render.NewCanvas(tc.w, tc.h, tc.l)
		if err := p.Render(req, w, img); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		dark, mid := 0, 0
		for _, i := range img.Pix {
			if i == 0 {
				dark++
			} else if int(i) < len(img.Palette)-1 {
				mid++
			}
		}
		if dark < tc.w*tc.h/50 {
			t.Errorf("%s: only %d black pixels, card looks empty", tc.name, dark)
		}
		if tc.l == 4 && mid == 0 {
			t.Errorf("%s: no gray levels used", tc.name)
		}
		if out := os.Getenv("POKEDEX_PNG"); out != "" {
			var buf bytes.Buffer
			png.Encode(&buf, img)
			os.WriteFile(strings.ReplaceAll(out, "NAME", strings.ReplaceAll(tc.name, " ", "-")), buf.Bytes(), 0o644)
		}
	}
	if err := p.Render(&plugin.Request{}, plugin.WidgetRequest{Widget: "nope"}, render.NewCanvas(10, 10, 2)); err == nil {
		t.Error("unknown widget: no error")
	}
}

func TestStartLoadsAhead(t *testing.T) {
	p, calls := configured(t, map[string]any{"pokemon": "25"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx, func(...string) {})
	deadline := time.Now().Add(5 * time.Second)
	for calls.Load() < 4 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() != 4 {
		t.Fatalf("start did not preload: %d requests", calls.Load())
	}
}
