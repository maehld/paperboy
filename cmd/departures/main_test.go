package main

import (
	"context"
	"image"
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

// now is shortly before the first departure in the fixtures. The requests use
// Europe/Berlin, which is one hour ahead of the UTC times in the fixtures.
var now = time.Date(2026, 11, 7, 11, 30, 0, 0, time.UTC)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseStopFinder(t *testing.T) {
	loc, err := parseStopFinder(fixture(t, "stopfinder.json"))
	if err != nil || loc.ID != "de:00000:1001" || !strings.HasPrefix(loc.Name, "Beispiel") {
		t.Fatalf("%+v %v", loc, err)
	}
	if _, err := parseStopFinder([]byte(`{"locations":[{"id":"x","type":"street"}]}`)); err == nil {
		t.Error("a street is not a stop")
	}
}

func TestParseDepartures(t *testing.T) {
	deps, name, err := parseDepartures(fixture(t, "dm.json"))
	if err != nil || len(deps) != 10 || !strings.HasPrefix(name, "Beispiel") {
		t.Fatalf("%d %q %v", len(deps), name, err)
	}
	d := deps[0]
	if d.Line != "355" || d.Destination != "Beispielstadt, Depot" || d.delay() != 1 || d.Cancelled {
		t.Errorf("%+v delay %d", d, d.delay())
	}
	if !deps[3].Expected.IsZero() || deps[3].delay() != 0 {
		t.Errorf("without real-time data: %+v", deps[3])
	}

	deps, _, err = parseDepartures(fixture(t, "dm_disruption.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Sorted by the real time: delayed 355 (+7), R82 planned 11:50, then the
	// cancelled one planned 12:33.
	if deps[0].delay() != 7 || deps[1].Line != "R82" || !deps[2].Cancelled {
		t.Errorf("%+v", deps[:3])
	}
	if _, _, err := parseDepartures([]byte(`{"systemMessages":[{"type":"error","text":"boom"}]}`)); err == nil {
		t.Error("EFA error ignored")
	}
}

func configured(t *testing.T, cfg plugin.Values, dm string) (*departures, *plugin.Request, *int32) {
	t.Helper()
	var finds int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/XML_STOPFINDER_REQUEST":
			atomic.AddInt32(&finds, 1)
			w.Write(fixture(t, "stopfinder.json"))
		case "/XML_DM_REQUEST":
			if r.URL.Query().Get("name_dm") != "de:00000:1001" || r.URL.Query().Get("useRealtime") != "1" {
				http.Error(w, "bad query "+r.URL.RawQuery, 400)
				return
			}
			w.Write(fixture(t, dm))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	p := &departures{client: srv.Client()}
	cfg["base"] = srv.URL
	if _, ok := cfg["stop"]; !ok {
		cfg["stop"] = "Beispielstadt, Marktplatz" // resolved through the stop finder fixture
	}
	req := &plugin.Request{Language: "de", Timezone: "Europe/Berlin", Now: now,
		Page: plugin.PageInfo{Width: 400, Height: 480, PPI: 125}}
	if err := p.Configure(cfg, req); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p.Start(ctx, func(...string) {})
	if err := p.Prepare(req); err != nil {
		t.Fatal(err)
	}
	return p, req, &finds
}

func TestResolveOnceAndText(t *testing.T) {
	p, req, finds := configured(t, plugin.Values{}, "dm_disruption.json")
	if _, err := p.resolve(context.Background()); err != nil || atomic.LoadInt32(finds) != 1 {
		t.Fatalf("stop finder asked %d times: %v", atomic.LoadInt32(finds), err)
	}
	lines, err := p.Text(req, plugin.WidgetRequest{Widget: "departures", Width: 20, Height: 4})
	want := []string{"355 Depot      +7 11", "R82 Neustadt      20", "355 Hauptstraß Ausf.", "355 Depot   +1 14:05"}
	if err != nil || strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("%q (%v)", lines, err)
	}
	for _, l := range lines {
		if len([]rune(l)) != 20 {
			t.Errorf("line %q is not 20 characters", l)
		}
	}
	header, _ := p.Text(req, plugin.WidgetRequest{Widget: "departures", Width: 20, Height: 2, Args: map[string]any{"header": true}})
	if len(header) != 2 || !strings.HasPrefix(header[0], "Beispiel") {
		t.Errorf("header: %q", header)
	}
}

func TestFilters(t *testing.T) {
	p, _, _ := configured(t, plugin.Values{"lines": []any{"R82"}}, "dm_disruption.json")
	if len(p.cur) != 1 || p.cur[0].Line != "R82" {
		t.Fatalf("lines: %+v", p.cur)
	}
	p, _, _ = configured(t, plugin.Values{"directions": []any{"dorf"}, "max": 2}, "dm_disruption.json")
	if len(p.cur) != 2 || !strings.Contains(p.cur[0].Destination, "Musterdorf") {
		t.Fatalf("directions: %+v", p.cur)
	}
	// The delayed 355 leaves at 11:41, the R82 at 11:50: 15 minutes of
	// walking hide the first only.
	p, req, _ := configured(t, plugin.Values{"walk_minutes": 15}, "dm_disruption.json")
	if p.cur[0].Line != "R82" {
		t.Fatalf("walk_minutes: first is %+v", p.cur[0])
	}
	for _, d := range p.cur {
		if d.when().Before(req.Now.Add(15 * time.Minute)) {
			t.Errorf("not reachable: %+v", d)
		}
	}
}

func TestFingerprintIgnoresCountdown(t *testing.T) {
	p, req, _ := configured(t, plugin.Values{}, "dm.json")
	a := fingerprint(p.visible(p.data, req.Now))
	if b := fingerprint(p.visible(p.data, req.Now.Add(2*time.Minute))); a != b {
		t.Error("the countdown changed the fingerprint")
	}
	disrupted, _, _ := parseDepartures(fixture(t, "dm_disruption.json"))
	if a == fingerprint(p.visible(disrupted, req.Now)) {
		t.Error("a delay and a cancellation did not change the fingerprint")
	}
}

func TestRender(t *testing.T) {
	p, req, _ := configured(t, plugin.Values{}, "dm_disruption.json")
	for _, c := range []struct {
		ppi    float64
		levels int
		w, h   int
	}{{125, 4, 400, 300}, {236, 2, 400, 300}, {125, 2, 200, 120}} {
		req.Page.PPI = c.ppi
		style := plugin.Style{Padding: int(4.5 * c.ppi / 72), Rule: 1, TextPx: 16 * c.ppi / 72}
		img := render.NewCanvas(c.w, c.h, c.levels)
		w := plugin.WidgetRequest{Widget: "departures", Width: c.w, Height: c.h, Style: style, Args: map[string]any{"header": true}}
		if err := p.Render(req, w, img); err != nil {
			t.Fatalf("%+v: %v", c, err)
		}
		if !hasInk(img) {
			t.Errorf("%+v: empty widget", c)
		}
	}
	if err := p.Render(req, plugin.WidgetRequest{Widget: "departures", Width: 100, Height: 8, Style: plugin.Style{TextPx: 16}},
		render.NewCanvas(100, 8, 2)); err == nil {
		t.Error("tiny cell accepted")
	}
	if err := p.Render(req, plugin.WidgetRequest{Widget: "departures", Width: 100, Height: 100, Args: map[string]any{"time": "x"}},
		render.NewCanvas(100, 100, 2)); err == nil {
		t.Error("bad time mode accepted")
	}
}

func TestConfigure(t *testing.T) {
	for _, bad := range []plugin.Values{{"backend": "hafas"}, {"base": "westfalenfahrplan.de"}, {"refresh": "5s"},
		{"max": 0}, {"max": 99}, {"walk_minutes": -1}, {}, {"stop": " "}, {"token": "x"}} {
		if err := (&departures{}).Configure(bad, &plugin.Request{}); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	p := &departures{}
	if err := p.Configure(plugin.Values{"stop": "X"}, &plugin.Request{}); err != nil || p.stop != "X" || p.base != defaultBase {
		t.Errorf("defaults: %+v %v", p, err)
	}
}

func hasInk(img *image.Paletted) bool {
	for _, px := range img.Pix {
		if px != uint8(len(img.Palette)-1) {
			return true
		}
	}
	return false
}
