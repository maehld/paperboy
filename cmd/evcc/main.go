// Command evcc is a Paperboy plugin (ADR 0049) for evcc (https://evcc.io). It
// polls the evcc REST API (/api/state and /api/history/energy) in the
// background and draws:
//
//   - "site": PV, home, grid and home battery as tiles,
//   - "flow": an energy flow diagram, boxes for PV, grid, home, battery and
//     active loadpoints joined by arrows whose width shows the power,
//   - "loadpoint": one charging point with vehicle, mode, state of charge,
//     charge power and session energy,
//   - "forecast": the solar forecast and the grid price of the coming hours,
//   - "history": the energy of today, yesterday, this and last week, this and
//     last month and this year for PV, the battery, the grid, the home or one
//     charging point.
//
// "site", "flow", "loadpoint" and "history" have text variants for character
// displays.
//
//	plugins:
//	  evcc:
//	    command: [evcc]
//	    config: { url: "http://evcc:7070" }
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

func main() {
	plugin.Serve(&evcc{client: &http.Client{Timeout: 15 * time.Second}})
}

var configKeys = map[string]plugin.Arg{
	"url":             {Type: "string", Required: true, Description: "evcc base URL, e.g. http://evcc:7070"},
	"refresh":         {Type: "duration", Default: "30s", Description: "how often /api/state is read"},
	"history_refresh": {Type: "duration", Default: "15m", Description: "how often the energy history (/api/history/energy) is read"},
}

// state is the part of /api/state the widgets use. Newer evcc versions answer
// with the object directly, older ones wrap it in "result".
type state struct {
	SiteTitle       string      `json:"siteTitle"`
	PVPower         float64     `json:"pvPower"`
	HomePower       float64     `json:"homePower"`
	Grid            *gridMeter  `json:"grid"`
	GridPowerLegacy *float64    `json:"gridPower"`
	Battery         *battery    `json:"battery"`
	TariffGrid      *float64    `json:"tariffGrid"`
	TariffFeedIn    *float64    `json:"tariffFeedIn"`
	Currency        string      `json:"currency"`
	Loadpoints      []loadpoint `json:"loadpoints"`
	Forecast        *forecast   `json:"forecast"`
}

type gridMeter struct {
	Power float64 `json:"power"`
}

type battery struct {
	Power    float64 `json:"power"`
	SoC      float64 `json:"soc"`
	Capacity float64 `json:"capacity"`
}

type loadpoint struct {
	Title                   string  `json:"title"`
	Mode                    string  `json:"mode"`
	VehicleTitle            string  `json:"vehicleTitle"`
	VehicleSoC              float64 `json:"vehicleSoc"`
	VehicleRange            float64 `json:"vehicleRange"`
	EffectiveLimitSoC       float64 `json:"effectiveLimitSoc"`
	ChargePower             float64 `json:"chargePower"`
	SessionEnergy           float64 `json:"sessionEnergy"`
	SessionSolarPercentage  float64 `json:"sessionSolarPercentage"`
	Charging                bool    `json:"charging"`
	Connected               bool    `json:"connected"`
	ChargeRemainingDuration float64 `json:"chargeRemainingDuration"` // nanoseconds
	PhasesActive            int     `json:"phasesActive"`
}

type forecast struct {
	Grid  [][3]float64 `json:"grid"` // [from, to, price] in unix seconds
	Solar *struct {
		Timeseries [][2]float64 `json:"timeseries"` // [time, watts]
		Today      struct {
			Energy float64 `json:"energy"`
		} `json:"today"`
		Tomorrow struct {
			Energy float64 `json:"energy"`
		} `json:"tomorrow"`
	} `json:"solar"`
}

// gridPower returns the grid power; positive is import.
func (s *state) gridPower() float64 {
	if s.Grid != nil {
		return s.Grid.Power
	}
	if s.GridPowerLegacy != nil {
		return *s.GridPowerLegacy
	}
	return 0
}

type evcc struct {
	client  *http.Client
	base    string
	refresh time.Duration

	historyRefresh time.Duration

	mu      sync.Mutex
	data    *state
	at      time.Time
	loadErr error
	ready   chan struct{}

	hist      map[string][]dayValue
	histErr   error
	histReady chan struct{}

	cur  *state
	req  *plugin.Request
	lang string
}

func (e *evcc) Describe() plugin.Description {
	both := []string{plugin.KindImage, plugin.KindText}
	return plugin.Description{
		Title:   "evcc",
		Version: version(),
		Config:  configKeys,
		Actions: []plugin.ActionSpec{
			{Name: "mode", Title: "Charge mode", Args: actionArgs, Confirm: &plugin.Confirm{Required: true}},
		},
		Widgets: []plugin.WidgetInfo{
			{Widget: "site", Title: "Energy flow", Kinds: both, MinWidth: 200, MinHeight: 80,
				Sizes: []plugin.Span{{Columns: 3, Rows: 3}, {Columns: 3, Rows: 4}, {Columns: 6, Rows: 2}},
				Args: map[string]plugin.Arg{
					"columns": {Type: "int", Default: 2, Description: "tiles per row: 2 or 4", Enum: []any{2, 4}},
				}},
			{Widget: "flow", Title: "Energy flow diagram", Kinds: both, MinWidth: 240, MinHeight: 200,
				Sizes: []plugin.Span{{Columns: 3, Rows: 3}, {Columns: 3, Rows: 4}, {Columns: 6, Rows: 4}}},
			{Widget: "loadpoint", Title: "Charging point", Kinds: both, MinWidth: 200, MinHeight: 100,
				Sizes: []plugin.Span{{Columns: 3, Rows: 3}, {Columns: 3, Rows: 4}, {Columns: 6, Rows: 3}},
				Args: map[string]plugin.Arg{
					"index": {Type: "int", Default: 0, Description: "loadpoint, counted from 0"},
				}},
			{Widget: "history", Title: "Energy history", Kinds: both, MinWidth: 160, MinHeight: 100,
				Sizes: []plugin.Span{{Columns: 2, Rows: 4}, {Columns: 2, Rows: 7}, {Columns: 3, Rows: 3}, {Columns: 3, Rows: 4}, {Columns: 6, Rows: 2}},
				Args: map[string]plugin.Arg{
					"source": {Type: "string", Default: "pv", Description: "pv, battery, loadpoint, grid or home", Enum: []any{"pv", "battery", "loadpoint", "grid", "home"}},
					"metric": {Type: "string", Default: "energy", Description: "energy, or return for battery discharge and grid export", Enum: []any{"energy", "return"}},
					"index":  {Type: "int", Default: 0, Description: "loadpoint, counted from 0 (source loadpoint)"},
				}},
			{Widget: "forecast", Title: "Solar forecast and grid price", Kinds: []string{plugin.KindImage}, MinWidth: 240, MinHeight: 100,
				Sizes: []plugin.Span{{Columns: 6, Rows: 2}, {Columns: 6, Rows: 3}},
				Args: map[string]plugin.Arg{
					"hours": {Type: "int", Default: 24, Description: "hours ahead, 6 to 48"},
				}},
		},
	}
}

func (e *evcc) Configure(cfg plugin.Values, _ *plugin.Request) error {
	if err := cfg.Check(configKeys); err != nil {
		return err
	}
	u, err := url.Parse(strings.TrimRight(cfg.String("url", ""), "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("url: expected http(s)://host:port")
	}
	refresh, err := cfg.Duration("refresh", 30*time.Second)
	if err != nil {
		return err
	}
	if refresh < 10*time.Second {
		return errors.New("refresh must be at least 10s")
	}
	historyRefresh, err := cfg.Duration("history_refresh", 15*time.Minute)
	if err != nil {
		return err
	}
	if historyRefresh < time.Minute {
		return errors.New("history_refresh must be at least 1m")
	}
	e.base, e.refresh, e.historyRefresh = u.String(), refresh, historyRefresh
	return nil
}

// Start polls evcc and reports a change when a displayed value moved. The
// energy history has its own loop: a year of day buckets must not delay the
// state poll.
func (e *evcc) Start(ctx context.Context, notify func(widgets ...string)) {
	e.ready = make(chan struct{})
	e.histReady = make(chan struct{})
	go e.watchHistory(ctx, notify)
	go func() {
		first := true
		var last string
		for {
			s, err := e.fetch(ctx)
			e.mu.Lock()
			if err != nil {
				if e.loadErr == nil || err.Error() != e.loadErr.Error() {
					fmt.Fprintln(os.Stderr, "evcc:", err)
				}
				e.loadErr = err
			} else {
				e.data, e.at, e.loadErr = s, time.Now(), nil
			}
			e.mu.Unlock()
			if err == nil {
				sum := fingerprint(s)
				if !first && sum != last {
					notify()
				}
				last = sum
			}
			if first {
				close(e.ready)
				first = false
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(e.refresh):
			}
		}
	}()
}

// fingerprint covers the values the widgets show, rounded so that noise does
// not redraw the panels.
func fingerprint(s *state) string {
	r := func(w float64) int { return int(w / 50) } // 50 W steps
	var b strings.Builder
	fmt.Fprintf(&b, "%d %d %d", r(s.PVPower), r(s.HomePower), r(s.gridPower()))
	if s.Battery != nil {
		fmt.Fprintf(&b, " %d %d", r(s.Battery.Power), int(s.Battery.SoC))
	}
	for _, lp := range s.Loadpoints {
		fmt.Fprintf(&b, " %s %s %d %d %t %t %d", lp.Mode, lp.VehicleTitle, int(lp.VehicleSoC), r(lp.ChargePower),
			lp.Charging, lp.Connected, int(lp.SessionEnergy/100))
	}
	if s.TariffGrid != nil {
		fmt.Fprintf(&b, " %.3f", *s.TariffGrid)
	}
	return b.String()
}

func (e *evcc) fetch(ctx context.Context) (*state, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.base+"/api/state", nil)
	if err != nil {
		return nil, err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s/api/state", resp.StatusCode, e.base)
	}
	return parseState(body)
}

func parseState(body []byte) (*state, error) {
	var wrapped struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil && len(wrapped.Result) > 0 && wrapped.Result[0] == '{' {
		body = wrapped.Result
	}
	var s state
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, fmt.Errorf("invalid /api/state: %w", err)
	}
	return &s, nil
}

func (e *evcc) Prepare(req *plugin.Request) error {
	select {
	case <-e.ready:
	case <-time.After(10 * time.Second):
		return errors.New("evcc state is still loading")
	}
	// The history may be missing (an evcc without the energy database); its
	// widgets then show the reason and the others keep working.
	select {
	case <-e.histReady:
	case <-time.After(10 * time.Second):
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.data == nil {
		return e.loadErr
	}
	e.cur, e.req, e.lang = e.data, req, req.Lang()
	return nil
}

// MaxAge: values move all the time; changes are also reported as events.
func (e *evcc) MaxAge(*plugin.Request) time.Duration { return 2 * time.Minute }

func (e *evcc) loadpoint(w plugin.WidgetRequest) (*loadpoint, error) {
	i := w.Arg().Int("index", 0)
	if i < 0 || i >= len(e.cur.Loadpoints) {
		return nil, fmt.Errorf("loadpoint %d: evcc has %d", i, len(e.cur.Loadpoints))
	}
	return &e.cur.Loadpoints[i], nil
}
