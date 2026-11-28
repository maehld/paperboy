package main

import (
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

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

// fixture serves recorded answers of both APIs, an application token and a
// generated picture instead of the character render.
func fixture(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	file := func(name string, replace string, w http.ResponseWriter) {
		b, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(strings.ReplaceAll(string(b), "RENDER", replace)))
	}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch {
		case r.URL.Path == "/token":
			if _, _, ok := r.BasicAuth(); !ok {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Write([]byte(`{"access_token":"tok","expires_in":86399,"token_type":"bearer"}`))
		case r.URL.Path == "/render.png":
			img := image.NewRGBA(image.Rect(0, 0, 200, 400))
			for y := 0; y < 400; y++ {
				for x := 0; x < 200; x++ {
					shade := uint8(255)
					if (x-100)*(x-100)+(y-250)*(y-250) < 120*120 {
						shade = 60
					}
					img.Set(x, y, color.RGBA{shade, shade, shade, 255})
				}
			}
			png.Encode(w, img)
		case r.URL.Path == "/api/v1/characters/profile":
			if r.URL.Query().Get("name") != "Thrall" || r.URL.Query().Get("region") != "eu" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if r.URL.Query().Get("realm") != "frostwolf" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			file("raiderio-thrall.json", srv.URL+"/render.png", w)
		case strings.HasSuffix(r.URL.Path, "/character-media"):
			file("blizzard-media.json", srv.URL+"/render.png", w)
		case strings.HasSuffix(r.URL.Path, "/equipment"):
			file("blizzard-equipment.json", "", w)
		case strings.Contains(r.URL.Path, "/profile/wow/character/"):
			if !strings.Contains(r.URL.Path, "frostwolf/thrall") {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if r.URL.Query().Get("namespace") != "profile-eu" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			file("blizzard-profile.json", "", w)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// started configures the plugin against the fixture.
func started(t *testing.T, cfg plugin.Values) (*armory, *httptest.Server) {
	t.Helper()
	srv, _ := fixture(t)
	base := plugin.Values{
		"base":       srv.URL,
		"api_base":   srv.URL,
		"oauth_base": srv.URL,
		"region":     "eu",
	}
	for k, v := range cfg {
		base[k] = v
	}
	a := &armory{}
	if err := a.Configure(base, &plugin.Request{Version: 10, Language: "de"}); err != nil {
		t.Fatal(err)
	}
	return a, srv
}

func fullConfig() plugin.Values {
	return plugin.Values{
		"characters":    []any{"Thrall@frostwolf"},
		"client_id":     "id",
		"client_secret": "secret",
		"refresh":       "30m",
	}
}

func widget(name string, w, h int, args plugin.Values) plugin.WidgetRequest {
	return plugin.WidgetRequest{Widget: name, Width: w, Height: h, Args: args,
		Style: plugin.Style{Padding: 8, Rule: 1, TextPx: 28}}
}

func request(widgets ...plugin.WidgetRequest) *plugin.Request {
	return &plugin.Request{Version: 10, Language: "de", Now: time.Now(),
		Widgets: widgets, Levels: 4, Page: plugin.PageInfo{Width: 800, Height: 480, PPI: 125}}
}

func TestParseName(t *testing.T) {
	cases := map[string]name{
		"Thrall@frostwolf": {char: "Thrall", realm: "frostwolf"},
		" Jaina ":          {char: "Jaina"},
		"Arthas@Lordaeron": {char: "Arthas", realm: "Lordaeron"},
	}
	for in, want := range cases {
		got, err := parseName(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got != want {
			t.Fatalf("%q: got %+v, want %+v", in, got, want)
		}
	}
	for _, in := range []string{"", "@frostwolf", "   "} {
		if _, err := parseName(in); err == nil {
			t.Fatalf("%q: want an error", in)
		}
	}
}

func TestTierName(t *testing.T) {
	for slug, want := range map[string]string{
		"manaforge-omega":         "Manaforge Omega",
		"the-nexus-king":          "The Nexus King",
		"vault-of-the-incarnates": "Vault of the Incarnates",
	} {
		if got := tierName(slug); got != want {
			t.Fatalf("%q: got %q, want %q", slug, got, want)
		}
	}
}

func TestContextSlug(t *testing.T) {
	for in, want := range map[string]string{
		"raid-mythic":    "Raid · Mythic",
		"raid-heroic":    "Raid · Heroic",
		"dungeon-heroic": "Dungeon · Heroic",
		"dungeon-mythic": "Dungeon · Mythic",
		"challenge-mode": "Mythic+",
		"trade-skill":    "Crafted",
		"":               "",
	} {
		if got := contextSlug(in); got != want {
			t.Fatalf("%q: got %q, want %q", in, got, want)
		}
	}
	if got := originText([]byte(`5`)); got != "Raid · Heroic" {
		t.Fatalf("numeric context: got %q", got)
	}
	if got := originText([]byte(`"raid-mythic"`)); got != "Raid · Mythic" {
		t.Fatalf("string context: got %q", got)
	}
}

func TestConfigure(t *testing.T) {
	a := &armory{}
	cfg := plugin.Values{
		"characters": []any{"Thrall@frostwolf", "Jaina"},
		"client_id":  "id",
	}
	if err := a.Configure(cfg, &plugin.Request{Language: "de"}); err != nil {
		t.Fatal(err)
	}
	if a.region != "eu" || a.base != defaultBase || a.refresh != 30*time.Minute {
		t.Fatalf("defaults: %+v", a)
	}
	if len(a.chars) != 2 || a.chars[1].realm != "" {
		t.Fatalf("characters: %+v", a.chars)
	}

	bad := plugin.Values{"refresh": "10s"}
	if err := (&armory{}).Configure(bad, &plugin.Request{}); err == nil {
		t.Fatal("a refresh below a minute must fail")
	}
	if err := (&armory{}).Configure(plugin.Values{"colour": "red"}, &plugin.Request{}); err == nil {
		t.Fatal("an unknown key must fail")
	}
}

func TestDescribe(t *testing.T) {
	a := &armory{}
	a.Configure(plugin.Values{"characters": []any{"Thrall@frostwolf"}}, &plugin.Request{})
	d := a.Describe()
	if d.Version != version() || d.Title == "" {
		t.Fatalf("describe: %+v", d)
	}
	names := map[string]bool{}
	for _, w := range d.Widgets {
		names[w.Widget] = true
		if w.MinWidth == 0 || w.MinHeight == 0 {
			t.Fatalf("widget %q: no minimum size", w.Widget)
		}
	}
	for _, want := range []string{"character", "mythic", "raid", "gear"} {
		if !names[want] {
			t.Fatalf("widget %q missing", want)
		}
	}
	if len(d.Values) != 10 {
		t.Fatalf("values: %d", len(d.Values))
	}
	arg := d.Widgets[0].Args["character"]
	if len(arg.Enum) != 1 || arg.Enum[0] != "Thrall@frostwolf" {
		t.Fatalf("character enum: %+v", arg.Enum)
	}
}

// ink counts the pixels that are not white.
func ink(img *image.Paletted) int {
	n := 0
	for _, px := range img.Pix {
		if px != 0 {
			n++
		}
	}
	return n
}

func TestRenderWidgets(t *testing.T) {
	a, _ := started(t, fullConfig())
	widgets := []plugin.WidgetRequest{
		widget("character", 533, 480, nil),
		widget("mythic", 267, 240, nil),
		widget("raid", 267, 240, nil),
		widget("gear", 800, 480, nil),
	}
	if err := a.Prepare(request(widgets...)); err != nil {
		t.Fatal(err)
	}
	for _, w := range widgets {
		img := render.NewCanvas(w.Width, w.Height, 4)
		if err := a.Render(request(w), w, img); err != nil {
			t.Fatalf("%s: %v", w.Widget, err)
		}
		if ink(img) < 200 {
			t.Fatalf("%s: the widget drew almost nothing (%d pixels)", w.Widget, ink(img))
		}
	}
}

func TestRenderTooSmall(t *testing.T) {
	a, _ := started(t, fullConfig())
	small := widget("character", 60, 40, nil)
	if err := a.Prepare(request(small)); err != nil {
		t.Fatal(err)
	}
	img := render.NewCanvas(60, 40, 4)
	err := a.Render(request(small), small, img)
	if err == nil || !strings.Contains(err.Error(), "cell too small") {
		t.Fatalf("want ErrTooSmall, got %v", err)
	}
}

func TestRenderDithersPortrait(t *testing.T) {
	a, _ := started(t, fullConfig())
	w := widget("character", 533, 480, nil)
	if err := a.Prepare(request(w)); err != nil {
		t.Fatal(err)
	}
	img := render.NewCanvas(w.Width, w.Height, 2)
	if err := a.Render(request(w), w, img); err != nil {
		t.Fatal(err)
	}
	// The render is a dark disc on white; error diffusion keeps both tones.
	dark, light := 0, 0
	for _, px := range img.Pix {
		if px == 0 {
			dark++
		} else {
			light++
		}
	}
	if dark < 1000 || light < 1000 {
		t.Fatalf("the picture did not dither: %d dark, %d light", dark, light)
	}
}

func TestWithoutBlizzardCredentials(t *testing.T) {
	a, _ := started(t, plugin.Values{"characters": []any{"Thrall@frostwolf"}})
	w := widget("gear", 800, 480, nil)
	if err := a.Prepare(request(w)); err != nil {
		t.Fatal(err)
	}
	img := render.NewCanvas(w.Width, w.Height, 4)
	if err := a.Render(request(w), w, img); err != nil {
		t.Fatalf("gear without credentials: %v", err)
	}
	if ink(img) < 100 {
		t.Fatal("the notice did not draw")
	}
	// The character card answers without a render and without a gear list.
	w = widget("character", 533, 480, nil)
	img = render.NewCanvas(w.Width, w.Height, 4)
	if err := a.Render(request(w), w, img); err != nil {
		t.Fatal(err)
	}
	if ink(img) < 100 {
		t.Fatal("the card did not draw")
	}
}

func TestValues(t *testing.T) {
	a, _ := started(t, fullConfig())
	req := &plugin.Request{Version: 10, Language: "de",
		Values:    []string{"name", "level", "class", "spec", "guild", "item-level", "achievements", "mythic-score", "raid-progress", "last-login"},
		ValueArgs: plugin.Values{"character": "Thrall@frostwolf"}}
	if err := a.Prepare(req); err != nil {
		t.Fatal(err)
	}
	got, err := a.Values(req)
	if err != nil {
		t.Fatal(err)
	}
	if *got["name"].Text != "Thrall" || *got["class"].Text != "Schamane" || *got["spec"].Text != "Elementar" {
		t.Fatalf("text values: %+v", got)
	}
	if *got["level"].Number != 80 || *got["item-level"].Number != 623 {
		t.Fatalf("numbers: %+v", got)
	}
	if *got["mythic-score"].Number != 2847.4 {
		t.Fatalf("score: %v", *got["mythic-score"].Number)
	}
	if *got["raid-progress"].Text != "8/8 H" {
		t.Fatalf("raid progress: %v", *got["raid-progress"].Text)
	}
	if ts, err := got["last-login"].Time(time.UTC); err != nil || ts.Year() != 2025 {
		t.Fatalf("last login: %v %v", ts, err)
	}
}

func TestUnknownValue(t *testing.T) {
	a, _ := started(t, fullConfig())
	req := &plugin.Request{Version: 10, Values: []string{"colour"},
		ValueArgs: plugin.Values{"character": "Thrall@frostwolf"}}
	if err := a.Prepare(req); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Values(req); err == nil {
		t.Fatal("an unknown value must fail")
	}
}

func TestMissingCharacter(t *testing.T) {
	a, _ := started(t, fullConfig())
	w := widget("character", 533, 480, plugin.Values{"character": "Nobody@frostwolf"})
	if err := a.Prepare(request(w)); err != nil {
		t.Fatal(err)
	}
	img := render.NewCanvas(w.Width, w.Height, 4)
	if err := a.Render(request(w), w, img); err == nil {
		t.Fatal("a missing character must fail")
	}
}

func TestTokenIsCached(t *testing.T) {
	srv, calls := fixture(t)
	a := &armory{region: "eu", oauth: srv.URL, clientID: "id", secret: "secret"}
	ctx := context.Background()
	for range 3 {
		if _, err := a.accessToken(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("%d token requests, want 1", n)
	}
}
