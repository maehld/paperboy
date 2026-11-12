package main

import (
	"context"
	"image"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

const stateJSON = `{"siteTitle":"Home","pvPower":4200,"homePower":800,"pvEnergy":26009.5,
"grid":{"power":-2100,"energy":9697.5,"returnEnergy":11141.6},
"battery":{"power":-1300,"soc":81,"capacity":9.6,"energy":6013.7,"returnEnergy":6362.4},
"tariffGrid":0.27,"currency":"EUR",
"statistics":{"thisYear":{"avgPrice":0.1639,"chargedKWh":145.3,"solarPercentage":55.1},
"total":{"avgPrice":0.1705,"chargedKWh":1234.5,"solarPercentage":54.8}},
"loadpoints":[{"title":"Garage","mode":"pv","vehicleTitle":"Octavia","vehicleSoc":64,"vehicleRange":180,
"effectiveLimitSoc":80,"chargePower":3700,"sessionEnergy":5400,"sessionSolarPercentage":90,"charging":true,
"connected":true,"phasesActive":1,"chargeRemainingDuration":2400000000000}],
"forecast":{"grid":[[1794002400,1794006000,0.25],[1794006000,1794009600,0.31]],
"solar":{"timeseries":[[1794002400,1000],[1794006000,3000],[1794009600,0]],"today":{"energy":12000},"tomorrow":{"energy":8600}}}}`

func TestParseState(t *testing.T) {
	for _, body := range []string{stateJSON, `{"result":` + stateJSON + `}`} {
		s, err := parseState([]byte(body))
		if err != nil || s.gridPower() != -2100 || len(s.Loadpoints) != 1 || s.Battery.SoC != 81 {
			t.Fatalf("%+v %v", s, err)
		}
	}
	legacy, _ := parseState([]byte(`{"gridPower": 300}`))
	if legacy.gridPower() != 300 {
		t.Fatalf("legacy grid power: %v", legacy.gridPower())
	}
}

func newTestPlugin(t *testing.T) (*evcc, *plugin.Request) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/state":
			w.Write([]byte(stateJSON))
		case "/api/history/energy":
			writeHistoryFixture(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	e := &evcc{client: srv.Client()}
	req := &plugin.Request{Language: "de", Timezone: "UTC", Now: time.Unix(1794002400, 0), Page: plugin.PageInfo{Height: 480}}
	if err := e.Configure(plugin.Values{"url": srv.URL}, req); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e.Start(ctx, func(...string) {})
	if err := e.Prepare(req); err != nil {
		t.Fatal(err)
	}
	return e, req
}

func TestWidgets(t *testing.T) {
	e, req := newTestPlugin(t)
	style := plugin.Style{Padding: 8, Rule: 1, TextPx: 28.8}
	for _, name := range []string{"site", "loadpoint", "forecast", "values"} {
		img := render.NewCanvas(400, 200, 4)
		if err := e.Render(req, plugin.WidgetRequest{Widget: name, Width: 400, Height: 200, Style: style}, img); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !hasInk(img) {
			t.Errorf("%s: empty widget", name)
		}
	}
	if err := e.Render(req, plugin.WidgetRequest{Widget: "loadpoint", Width: 10, Height: 10, Args: map[string]any{"index": 3.0}},
		render.NewCanvas(10, 10, 2)); err == nil {
		t.Error("unknown loadpoint accepted")
	}
	lines, err := e.Text(req, plugin.WidgetRequest{Widget: "loadpoint", Width: 20, Height: 3})
	if err != nil || strings.Join(lines, "|") != "Octavia          64%|PV            3,7 kW|Sitzung      5,4 kWh" {
		t.Fatalf("text: %q %v", lines, err)
	}
	site, _ := e.Text(req, plugin.WidgetRequest{Widget: "site", Width: 20, Height: 4})
	if len(site) != 4 || site[2] != "Netz          2,1 kW" {
		t.Fatalf("site text: %q", site)
	}
}

func TestConfigure(t *testing.T) {
	e := &evcc{}
	for _, bad := range []plugin.Values{{}, {"url": "evcc:7070"}, {"url": "http://evcc", "refresh": "1s"}, {"url": "http://evcc", "token": "x"}} {
		if err := e.Configure(bad, &plugin.Request{}); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func hasInk(img *image.Paletted) bool {
	for _, p := range img.Pix {
		if p != uint8(len(img.Palette)-1) {
			return true
		}
	}
	return false
}
