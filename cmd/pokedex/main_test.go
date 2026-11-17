package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
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
			if strings.HasPrefix(r.URL.Path, "/api/pokemon/") || strings.HasPrefix(r.URL.Path, "/api/pokemon-species/") {
				genericMon(w, r.URL.Path)
				return
			}
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// genericMon answers any Pokémon that a seed may pick with a synthesized one
// of the same number and without artwork, so a test needs no fixture per
// candidate.
func genericMon(w http.ResponseWriter, path string) {
	n, err := strconv.Atoi(strings.TrimPrefix(path[strings.LastIndex(path, "/")+1:], "mon-"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if strings.HasPrefix(path, "/api/pokemon-species/") {
		fmt.Fprintf(w, `{"names":[{"name":"Mon %d","language":{"name":"en"}}],"flavor_text_entries":[]}`, n)
		return
	}
	var stats []string
	for _, name := range statOrder {
		stats = append(stats, fmt.Sprintf(`{"base_stat":%d,"stat":{"name":%q}}`, 40+n%100, name))
	}
	fmt.Fprintf(w, `{"id":%d,"name":"mon-%d","height":10,"weight":100,"species":{"name":"mon-%d"},
		"types":[{"slot":1,"type":{"name":"electric"}}],"stats":[%s],"sprites":{}}`,
		n, n, n, strings.Join(stats, ","))
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
		k := p.key(at, "")
		if k != p.key(at.Add(5*time.Hour), "") {
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

// TestSeedPicks checks that a seed gives its own pick of the interval and
// that the pick stays put within one interval.
func TestSeedPicks(t *testing.T) {
	p := newPokedex(nil)
	if err := p.Configure(plugin.Values{"generations": []any{1.0}}, &plugin.Request{Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	if p.key(at, "") != p.key(at, "") {
		t.Fatal("the unseeded pick is not stable")
	}
	same, shared := 0, 0
	for i := 0; i < 40; i++ {
		day := at.AddDate(0, 0, i)
		a, b := p.key(day, "kueche"), p.key(day, "flur")
		if a != p.key(day.Add(2*time.Hour), "kueche") {
			t.Fatalf("day %d: a seeded pick changed within the interval", i)
		}
		if a == b {
			same++
		}
		if a == p.key(day, "") {
			shared++
		}
	}
	// 151 candidates: about a quarter of the days match by chance.
	if same > 8 {
		t.Errorf("%d of 40 days picked the same Pokémon for both seeds", same)
	}
	if shared > 8 {
		t.Errorf("the seed had no effect on %d of 40 days", shared)
	}
}

// TestSeedsRenderDifferentCards draws two cards with different seeds and
// checks that they are loaded and drawn side by side.
func TestSeedsRenderDifferentCards(t *testing.T) {
	p, _ := configured(t, map[string]any{"mode": "random", "generations": []any{1.0}})
	req, w1 := request("en", 400, 240, 125)
	w2 := w1
	w1.Args = map[string]any{"seed": "kueche"}
	w2.Args = map[string]any{"seed": "flur"}
	req.Widgets = []plugin.WidgetRequest{w1, w2,
		{Widget: "card", Width: 400, Height: 240, Style: w1.Style}}
	var m1, m2 *mon
	for i := 0; i < 40; i++ {
		now := time.Date(2026, 11, 7+i, 12, 0, 0, 0, time.UTC)
		p.now = func() time.Time { return now }
		if err := p.Prepare(req); err != nil {
			t.Fatal(err)
		}
		if m1, _ = p.current(w1); m1 == nil {
			t.Fatalf("day %d: seed kueche not loaded", i)
		}
		if m2, _ = p.current(w2); m2 == nil {
			t.Fatalf("day %d: seed flur not loaded", i)
		}
		if m1.id != m2.id {
			break
		}
	}
	if m1.id == m2.id {
		t.Fatalf("both seeds picked %d on 40 days", m1.id)
	}
	for _, w := range []plugin.WidgetRequest{w1, w2} {
		img := render.NewCanvas(w.Width, w.Height, 4)
		if err := p.Render(req, w, img); err != nil {
			t.Fatalf("seed %v: %v", w.Args["seed"], err)
		}
	}
	if _, err := p.current(plugin.WidgetRequest{Widget: "card", Args: map[string]any{"seed": "unprepared"}}); err == nil {
		t.Error("a seed that was not prepared: no error")
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
	if _, err := p.load(context.Background(), "missingno"); err == nil {
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

func TestValues(t *testing.T) {
	p, _ := configured(t, map[string]any{"pokemon": "25"})
	req, _ := request("de", 20, 4, 0)
	req.Action = plugin.ActionValues
	req.Values = []string{"name", "number", "types", "height", "weight", "hp", "attack", "defense",
		"special-attack", "special-defense", "speed", "flavor"}
	if err := p.Prepare(req); err != nil {
		t.Fatal(err)
	}
	values, err := p.Values(req)
	if err != nil {
		t.Fatal(err)
	}
	if v := values["name"]; v.Text == nil || *v.Text != "Pikachu" {
		t.Fatalf("name = %+v", v)
	}
	if v := values["number"]; v.Number == nil || *v.Number != 25 {
		t.Fatalf("number = %+v", v)
	}
	if v := values["types"]; v.Text == nil || *v.Text != "Elektro" {
		t.Fatalf("types = %+v", v)
	}
	if v := values["height"]; v.Number == nil || *v.Number != 0.4 {
		t.Fatalf("height = %+v", v)
	}
	if v := values["weight"]; v.Number == nil || *v.Number != 6 {
		t.Fatalf("weight = %+v", v)
	}
	if v := values["speed"]; v.Number == nil || *v.Number != 90 {
		t.Fatalf("speed = %+v", v)
	}
	if v := values["flavor"]; v.Text == nil || *v.Text == "" {
		t.Fatalf("flavor = %+v", v)
	}
	if _, err := p.Values(&plugin.Request{Values: []string{"nope"}}); !errors.Is(err, plugin.ErrUnknownValue) {
		t.Fatalf("unknown value: %v", err)
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
		{"narrow", 125, 200, 300, 4}, {"e1003 16", 227, 936, 351, 16},
	} {
		req, w := request("en", tc.w, tc.h, tc.ppi)
		req.Levels = tc.l
		req.Widgets = []plugin.WidgetRequest{w}
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

// TestCardSwitches checks that stats and flavor leave their part of the card
// out, and that a card without stats keeps its text size.
func TestCardSwitches(t *testing.T) {
	p, _ := configured(t, map[string]any{"pokemon": "25"})
	prepare := func(args plugin.Values) (*plugin.Request, plugin.WidgetRequest) {
		req, w := request("en", 400, 300, 125)
		w.Args = args
		req.Widgets = []plugin.WidgetRequest{w}
		if err := p.Prepare(req); err != nil {
			t.Fatal(err)
		}
		return req, w
	}
	draw := func(args plugin.Values) *image.Paletted {
		req, w := prepare(args)
		img := render.NewCanvas(400, 300, 4)
		if err := p.Render(req, w, img); err != nil {
			t.Fatalf("card with %v: %v", args, err)
		}
		return img
	}
	all := draw(nil)
	noStats := draw(plugin.Values{"stats": false})
	noFlavor := draw(plugin.Values{"flavor": false})
	both := draw(plugin.Values{"stats": false, "flavor": false})
	for _, tc := range []struct {
		name string
		a, b *image.Paletted
	}{
		{"stats off", all, noStats}, {"flavor off", all, noFlavor}, {"both off", all, both},
	} {
		if bytes.Equal(tc.a.Pix, tc.b.Pix) {
			t.Errorf("%s: the card did not change", tc.name)
		}
	}
	dark := 0
	for _, i := range both.Pix {
		if i == 0 {
			dark++
		}
	}
	if dark < 400*300/200 {
		t.Errorf("a card without stats and flavor is almost empty: %d black pixels", dark)
	}

	// The freed stat rows go to the text: a short column keeps or grows its
	// text size instead of shrinking to fit a row budget it no longer uses.
	_, w := prepare(nil)
	m, err := p.current(w)
	if err != nil {
		t.Fatal(err)
	}
	col := image.Rect(0, 0, 200, 100)
	_, withStats := drawText(render.NewCanvas(200, 100, 4), col, m, "en", 16, 2, 1, true)
	_, without := drawText(render.NewCanvas(200, 100, 4), col, m, "en", 16, 2, 1, false)
	if without < withStats {
		t.Errorf("a card without stats shrank its text: %.1f < %.1f", without, withStats)
	}
}

// TestStackedCardCentersTheArtwork checks the card without stats: the header
// spans the width and the artwork follows underneath, centered, so a wide cell
// does not keep a small picture on the left and an empty half on the right.
func TestStackedCardCentersTheArtwork(t *testing.T) {
	p, _ := configured(t, map[string]any{"pokemon": "25"})
	req, w := request("en", 400, 240, 125)
	w.Args = plugin.Values{"stats": false, "flavor": false}
	req.Levels = 2
	req.Widgets = []plugin.WidgetRequest{w}
	if err := p.Prepare(req); err != nil {
		t.Fatal(err)
	}
	img := render.NewCanvas(400, 240, 2)
	if err := p.Render(req, w, img); err != nil {
		t.Fatal(err)
	}
	white := uint8(len(img.Palette) - 1)
	left, right := 400, -1
	for y := 100; y < 240; y++ { // below the header
		for x := 0; x < 400; x++ {
			if img.Pix[y*img.Stride+x] == white {
				continue
			}
			if x < left {
				left = x
			}
			if x > right {
				right = x
			}
		}
	}
	if right < 0 || right-left < 100 {
		t.Fatalf("no artwork below the header: %d..%d", left, right)
	}
	if d := (left) - (399 - right); d > 4 || d < -4 {
		t.Errorf("artwork is not centered: %d left, %d right", left, 399-right)
	}

	// The header keeps the full width: the rule under the name reaches both
	// edges, so a wider cell has a wider header band.
	req, w = request("en", 800, 240, 125)
	w.Args = plugin.Values{"stats": false, "flavor": false}
	req.Levels = 2
	req.Widgets = []plugin.WidgetRequest{w}
	if err := p.Prepare(req); err != nil {
		t.Fatal(err)
	}
	wide := render.NewCanvas(800, 240, 2)
	if err := p.Render(req, w, wide); err != nil {
		t.Fatal(err)
	}
	rule, count := 0, -1
	for y := 0; y < 100; y++ {
		dark := 0
		for x := 0; x < 800; x++ {
			if wide.Pix[y*wide.Stride+x] != white {
				dark++
			}
		}
		if dark > count {
			rule, count = y, dark
		}
	}
	if count < 700 {
		t.Errorf("the header rule spans %d px of 800 in row %d", count, rule)
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

// A cell the padding eats up is reported, so Paperboy draws its notice instead
// of an empty cell.
func TestTooSmallCell(t *testing.T) {
	p, _ := configured(t, map[string]any{"pokemon": "25"})
	req, w := request("en", 12, 8, 125)
	req.Levels, req.Widgets = 2, []plugin.WidgetRequest{w}
	if err := p.Prepare(req); err != nil {
		t.Fatal(err)
	}
	if err := p.Render(req, w, render.NewCanvas(12, 8, 2)); !errors.Is(err, plugin.ErrTooSmall) {
		t.Errorf("card in a 12x8 cell: error = %v, want ErrTooSmall", err)
	}
}
