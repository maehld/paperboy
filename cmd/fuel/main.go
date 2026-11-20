// Command fuel is a Paperboy plugin (ADR 0049) with the fuel prices of the
// Tankerkönig API. It fetches the stations around a location, draws the
// nearest ones, the cheapest one and the price history, and answers text
// pages with the cheapest price.
//
//	plugins:
//	  fuel:
//	    config: { apikey: ..., lat: 52.52, lon: 13.40 }
//
// The prices are the MTS-K data, published by Tankerkönig under CC BY 4.0.
// The API needs a personal key and allows one request per minute. The plugin
// fetches on demand and never more often than its refresh, which is at least
// five minutes.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

const (
	// defaultBase is the public Tankerkönig endpoint; base points tests and
	// other installations at a mirror.
	defaultBase = "https://creativecommons.tankerkoenig.de"
	// requestTimeout bounds the HTTP call.
	requestTimeout = 20 * time.Second
	// minRefresh is the shortest fetch interval. Tankerkönig asks home
	// automation for at most one request per five minutes.
	minRefresh = 5 * time.Minute
	// maxRadius is the largest search radius the API accepts, in km.
	maxRadius = 25.0
	// minSpan is the shortest price history the plugin keeps.
	minSpan = time.Hour
	// maxSpan is the longest price history the plugin keeps.
	maxSpan = 7 * 24 * time.Hour
	// maxStations is the largest list a widget or a value draws.
	maxStations = 10
	// maxPoints bounds one series of the stored history.
	maxPoints = 1500
	// stateVersion is the format of the stored history.
	stateVersion = 1
)

// The fuels the API reports.
const (
	fuelDiesel = "diesel"
	fuelE5     = "e5"
	fuelE10    = "e10"
)

// fuels is the fetch and storage order.
var fuels = []string{fuelDiesel, fuelE5, fuelE10}

func main() {
	plugin.Serve(&fuel{})
}

// price is one fuel price of the answer. Tankerkönig sends false when the
// station does not offer that fuel.
type price struct {
	Value float64
	Set   bool
}

func (p *price) UnmarshalJSON(data []byte) error {
	raw := bytes.TrimSpace(data)
	if bytes.Equal(raw, []byte("false")) || bytes.Equal(raw, []byte("null")) {
		*p = price{}
		return nil
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	*p = price{Value: v, Set: true}
	return nil
}

// station is one entry of the radius search.
type station struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Brand  string  `json:"brand"`
	Street string  `json:"street"`
	Place  string  `json:"place"`
	Dist   float64 `json:"dist"`
	Diesel price   `json:"diesel"`
	E5     price   `json:"e5"`
	E10    price   `json:"e10"`
	Open   bool    `json:"isOpen"`
}

// fuel returns the price of one fuel, and whether the station offers it.
func (s station) fuel(name string) (float64, bool) {
	switch name {
	case fuelE5:
		return s.E5.Value, s.E5.Set
	case fuelE10:
		return s.E10.Value, s.E10.Set
	}
	return s.Diesel.Value, s.Diesel.Set
}

// label is the name the widgets show: the station name, else the brand.
func (s station) label() string {
	if s.Name != "" {
		return s.Name
	}
	if s.Brand != "" {
		return s.Brand
	}
	return s.ID
}

// listAnswer is the answer of /json/list.php.
type listAnswer struct {
	OK       bool      `json:"ok"`
	Message  string    `json:"message"`
	Stations []station `json:"stations"`
}

// fetch reads every station in the radius with all three prices. One request
// answers every widget, so the plugin never asks per fuel.
func fetch(ctx context.Context, client *http.Client, base, apikey string, lat, lon, radius float64) ([]station, error) {
	u, err := url.Parse(strings.TrimRight(base, "/") + "/json/list.php")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("lat", strconv.FormatFloat(lat, 'f', -1, 64))
	q.Set("lng", strconv.FormatFloat(lon, 'f', -1, 64))
	q.Set("rad", strconv.FormatFloat(radius, 'f', -1, 64))
	q.Set("type", "all")
	q.Set("apikey", apikey)
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tankerkoenig: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var answer listAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		return nil, fmt.Errorf("tankerkoenig: %w", err)
	}
	if !answer.OK {
		msg := strings.TrimSpace(answer.Message)
		if msg == "" {
			msg = "request failed"
		}
		return nil, fmt.Errorf("tankerkoenig: %s", msg)
	}
	sort.SliceStable(answer.Stations, func(i, j int) bool {
		return answer.Stations[i].Dist < answer.Stations[j].Dist
	})
	return answer.Stations, nil
}

// configKeys is the configuration of the plugin.
var configKeys = map[string]plugin.Arg{
	"apikey":  {Type: "string", Required: true, Description: "personal Tankerkönig API key"},
	"lat":     {Type: "float", Required: true, Description: "latitude, e.g. 52.52"},
	"lon":     {Type: "float", Required: true, Description: "longitude, e.g. 13.40"},
	"radius":  {Type: "float", Default: 5.0, Description: "search radius in km, at most 25"},
	"refresh": {Type: "duration", Default: "10m", Description: "how often the prices are fetched, at least 5m"},
	"span":    {Type: "duration", Default: "168h", Description: "how much price history to keep, 1h to 168h"},
	"base":    {Type: "string", Default: defaultBase, Description: "Tankerkönig base URL"},
}

// fuel is the plugin.
type fuel struct {
	plugin.Demand

	apikey  string
	lat     float64
	lon     float64
	radius  float64
	refresh time.Duration
	span    time.Duration
	base    string

	// clock reads the time; tests replace it.
	clock func() time.Time

	mu      sync.Mutex
	data    []station
	fetched time.Time
	series  history
	loadErr error
	pending bool
	loaded  bool
	ready   chan struct{}

	// Snapshot of the current request.
	cur     []station
	curAt   time.Time
	curHist history
	req     *plugin.Request
	loc     *time.Location
	lang    string
}

func (f *fuel) now() time.Time {
	if f.clock != nil {
		return f.clock()
	}
	return time.Now()
}

func (f *fuel) Configure(cfg plugin.Values, req *plugin.Request) error {
	if req.Version != 0 && req.Version < 9 {
		return fmt.Errorf("fuel needs Paperboy protocol 9 or newer; this Paperboy reports %d", req.Version)
	}
	if err := cfg.Check(configKeys); err != nil {
		return err
	}
	f.apikey = strings.TrimSpace(cfg.String("apikey", ""))
	if f.apikey == "" {
		return errors.New("apikey is empty; create a key at https://creativecommons.tankerkoenig.de")
	}
	number := func(name string) (float64, error) {
		switch v := cfg[name].(type) {
		case float64:
			return v, nil
		case int:
			return float64(v), nil
		}
		return 0, fmt.Errorf("%s must be a number", name)
	}
	lat, err := number("lat")
	if err != nil {
		return err
	}
	lon, err := number("lon")
	if err != nil {
		return err
	}
	f.lat, f.lon = lat, lon
	if f.lat < -90 || f.lat > 90 || f.lon < -180 || f.lon > 180 {
		return errors.New("lat must be within -90..90 and lon within -180..180")
	}
	if _, ok := cfg["radius"]; ok {
		if f.radius, err = number("radius"); err != nil {
			return err
		}
	} else {
		f.radius = 5
	}
	if f.radius <= 0 || f.radius > maxRadius {
		return fmt.Errorf("radius must be within 0..%g km", maxRadius)
	}
	refresh, err := cfg.Duration("refresh", 10*time.Minute)
	if err != nil {
		return err
	}
	if refresh < minRefresh {
		return fmt.Errorf("refresh must be at least %s", minRefresh)
	}
	span, err := cfg.Duration("span", 7*24*time.Hour)
	if err != nil {
		return err
	}
	if span < minSpan || span > maxSpan {
		return fmt.Errorf("span must be within %s..%s", minSpan, maxSpan)
	}
	f.refresh, f.span = refresh, span
	f.base = cfg.String("base", defaultBase)
	f.series = history{}
	return nil
}

// Start fetches now and then on demand, never more often than refresh, and
// reports a change when what the widgets show differs.
func (f *fuel) Start(ctx context.Context, notify func(widgets ...string)) {
	f.ready = make(chan struct{})
	go func() {
		first := true
		var last string
		var next time.Time
		for {
			if !next.IsZero() {
				if !f.Wait(ctx, 0) {
					return
				}
				if wait := time.Until(next); wait > 0 {
					select {
					case <-ctx.Done():
						return
					case <-time.After(wait):
					}
					continue
				}
			}
			stations, err := f.load(ctx)
			now := f.now()
			f.mu.Lock()
			if err != nil {
				if f.loadErr == nil || f.loadErr.Error() != err.Error() {
					fmt.Fprintln(os.Stderr, "fuel:", err)
				}
				f.loadErr = err
			} else {
				f.data, f.fetched, f.loadErr = stations, now, nil
				if f.record(now, stations) {
					f.pending = true
				}
			}
			f.mu.Unlock()
			if err == nil {
				sum := fingerprintStations(stations)
				if !first && sum != last {
					notify()
				}
				last = sum
			}
			if first {
				close(f.ready)
				first = false
			}
			if err != nil {
				next = f.now().Add(time.Minute)
			} else {
				next = f.now().Add(f.refresh)
			}
		}
	}()
}

func (f *fuel) load(ctx context.Context) ([]station, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	return fetch(ctx, http.DefaultClient, f.base, f.apikey, f.lat, f.lon, f.radius)
}

// Prepare takes the snapshot every widget and value of the request reads.
func (f *fuel) Prepare(req *plugin.Request) error {
	select {
	case <-f.ready:
	case <-time.After(10 * time.Second):
		return errors.New("prices are still loading")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.data == nil {
		return f.loadErr
	}
	f.cur, f.curAt = f.data, f.fetched
	f.curHist = copyHistory(f.series)
	f.req, f.loc, f.lang = req, req.Location(), req.Lang()
	return nil
}

// MaxAge: a price stays until the plugin reports a change.
func (f *fuel) MaxAge(*plugin.Request) time.Duration { return f.refresh }

// fingerprintStations covers what the widgets show: the price and the open
// state of every station. A new price causes one changed event.
func fingerprintStations(stations []station) string {
	var b strings.Builder
	for _, s := range stations {
		fmt.Fprintf(&b, "%s:%t:%.3f:%t:%.3f:%t:%.3f:%t;", s.ID, s.Open,
			s.Diesel.Value, s.Diesel.Set, s.E5.Value, s.E5.Set, s.E10.Value, s.E10.Set)
	}
	return b.String()
}

// fingerprint identifies the key, the position and the radius of the stored
// history without keeping the key.
func (f *fuel) fingerprint() string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%.5f|%.5f|%.2f", f.apikey, f.lat, f.lon, f.radius)))
	return hex.EncodeToString(sum[:8])
}
