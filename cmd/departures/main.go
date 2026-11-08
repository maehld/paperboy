// Command departures is a Paperboy plugin (ADR 0053) for real-time public
// transport departures. The backend "efa" talks to the rapidJSON interface of
// an EFA installation (XML_STOPFINDER_REQUEST and XML_DM_REQUEST); by default
// that is the Westfalenfahrplan (NWL) with Lippe Mobil. It polls in the
// background and draws:
//
//   - "departures": line, destination, minutes or time, delay, cancellations.
//
// The widget has a text variant for character displays.
//
//	plugins:
//	  departures:
//	    command: [departures]
//	    config: { stop: "City, Stop name", walk_minutes: 4 }
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
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

func main() {
	plugin.Serve(&departures{client: &http.Client{Timeout: 20 * time.Second}})
}

const (
	defaultBase = "https://www.westfalenfahrplan.de/nwl-efa"
	userAgent   = "paperboy-departures (+https://github.com/maehld/paperboy)"
)

var configKeys = map[string]plugin.Arg{
	"backend":      {Type: "string", Default: "efa", Description: "data source; only efa (rapidJSON interface of an EFA installation) exists", Enum: []any{"efa"}},
	"base":         {Type: "string", Default: defaultBase, Description: "EFA base URL, without /XML_DM_REQUEST"},
	"stop":         {Type: "string", Required: true, Description: "stop name or stop ID (e.g. de:00000:1001); a name is resolved once"},
	"lines":        {Type: "list", Description: "only these lines (e.g. [355, R82]); default all"},
	"directions":   {Type: "list", Description: "only destinations containing one of these words; default all"},
	"max":          {Type: "int", Default: 8, Description: "most departures shown, 1 to 20"},
	"refresh":      {Type: "duration", Default: "60s", Description: "how often the stop is polled, at least 30s"},
	"walk_minutes": {Type: "int", Default: 0, Description: "hide departures leaving sooner than this many minutes"},
}

// departure is one stop event, already reduced to what the widget shows.
type departure struct {
	Line        string
	Destination string
	Planned     time.Time
	Expected    time.Time // zero without real-time data
	Cancelled   bool
	Platform    string
}

// when is the time the vehicle really leaves.
func (d departure) when() time.Time {
	if d.Expected.IsZero() {
		return d.Planned
	}
	return d.Expected
}

// delay is the real-time delay in whole minutes; negative means early.
func (d departure) delay() int {
	if d.Expected.IsZero() {
		return 0
	}
	return int(math.Round(d.Expected.Sub(d.Planned).Minutes()))
}

type departures struct {
	client  *http.Client
	base    string
	stop    string
	lines   []string
	dirs    []string
	max     int
	refresh time.Duration
	walk    time.Duration

	mu       sync.Mutex
	stopID   string // resolved once
	stopName string
	stopOnly string // stop without its locality, e.g. "Marktplatz"
	locality string // locality of the monitored stop, e.g. "Bielefeld"
	data     []departure
	loadErr  error
	ready    chan struct{}

	cur      []departure
	curName  string
	curStop  string
	curLocal string
	req      *plugin.Request
	lang     string
	location *time.Location
}

func (p *departures) Describe() plugin.Description {
	both := []string{plugin.KindImage, plugin.KindText}
	return plugin.Description{
		Title:   "Departures",
		Version: version(),
		Config:  configKeys,
		Widgets: []plugin.WidgetInfo{
			{Widget: "departures", Title: "Real-time departures", Kinds: both, MinWidth: 200, MinHeight: 60,
				Sizes: []plugin.Span{{Columns: 3, Rows: 3}, {Columns: 3, Rows: 4}, {Columns: 6, Rows: 3}, {Columns: 6, Rows: 4}},
				Args: map[string]plugin.Arg{
					"header": {Type: "bool", Default: false, Description: "show the stop name above the departures"},
					"time":   {Type: "string", Default: "auto", Description: "auto: minutes, a clock time from one hour on; minutes; clock", Enum: []any{"auto", "minutes", "clock"}},
					"locality": {Type: "string", Default: "auto", Description: "locality of a destination (\"City, Stop\"): auto drops it when it is the monitored stop's locality; always drops it; never keeps it",
						Enum: []any{"auto", "always", "never"}},
					"locality_order": {Type: "string", Default: "after", Description: "where the locality sits: after the stop (\"Stop, City\"), before it (\"City, Stop\") or nowhere; for the header and the destinations",
						Enum: []any{"after", "before", "none"}},
				}},
		},
	}
}

func (p *departures) Configure(cfg plugin.Values, _ *plugin.Request) error {
	if err := cfg.Check(configKeys); err != nil {
		return err
	}
	if b := cfg.String("backend", "efa"); b != "efa" {
		return fmt.Errorf("backend: %q is not supported, only efa", b)
	}
	u, err := url.Parse(strings.TrimRight(cfg.String("base", defaultBase), "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("base: expected http(s)://host/path of the EFA installation")
	}
	refresh, err := cfg.Duration("refresh", 60*time.Second)
	if err != nil {
		return err
	}
	if refresh < 30*time.Second {
		return errors.New("refresh must be at least 30s")
	}
	max := cfg.Int("max", 8)
	if max < 1 || max > 20 {
		return errors.New("max must be between 1 and 20")
	}
	walk := cfg.Int("walk_minutes", 0)
	if walk < 0 || walk > 120 {
		return errors.New("walk_minutes must be between 0 and 120")
	}
	stop := strings.TrimSpace(cfg.String("stop", ""))
	if stop == "" {
		return errors.New("stop is required: a stop name or ID")
	}
	p.base, p.stop, p.max, p.refresh, p.walk = u.String(), stop, max, refresh, time.Duration(walk)*time.Minute
	p.lines, p.dirs = cfg.Strings("lines"), cfg.Strings("directions")
	return nil
}

// Start polls the stop and reports a change when the shown departures move.
func (p *departures) Start(ctx context.Context, notify func(widgets ...string)) {
	p.ready = make(chan struct{})
	go func() {
		first := true
		var last string
		for {
			deps, err := p.fetch(ctx, time.Now())
			p.mu.Lock()
			if err != nil {
				if p.loadErr == nil || err.Error() != p.loadErr.Error() {
					fmt.Fprintln(os.Stderr, "departures:", err)
				}
				p.loadErr = err
			} else {
				p.data, p.loadErr = deps, nil
			}
			p.mu.Unlock()
			if err == nil {
				sum := fingerprint(p.visible(deps, time.Now()))
				if !first && sum != last {
					notify()
				}
				last = sum
			}
			if first {
				close(p.ready)
				first = false
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(p.refresh):
			}
		}
	}()
}

// fingerprint covers what the widget shows, but not the countdown, which
// moves every minute anyway.
func fingerprint(deps []departure) string {
	var b strings.Builder
	for _, d := range deps {
		fmt.Fprintf(&b, "%s|%s|%d|%d|%t;", d.Line, d.Destination, d.Planned.Unix(), d.when().Unix(), d.Cancelled)
	}
	return b.String()
}

// visible applies the line, direction and walking-time filters and the
// maximum. Departures that already left are dropped.
func (p *departures) visible(all []departure, now time.Time) []departure {
	var out []departure
	for _, d := range all {
		if d.when().Before(now.Add(-30 * time.Second)) {
			continue
		}
		if p.walk > 0 && d.when().Before(now.Add(p.walk)) {
			continue
		}
		if len(p.lines) > 0 && !containsFold(p.lines, d.Line) {
			continue
		}
		if len(p.dirs) > 0 && !matchesAny(d.Destination, p.dirs) {
			continue
		}
		out = append(out, d)
	}
	if len(out) > p.max {
		out = out[:p.max]
	}
	return out
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(strings.TrimSpace(v), s) {
			return true
		}
	}
	return false
}

func matchesAny(s string, words []string) bool {
	s = strings.ToLower(s)
	for _, w := range words {
		if w = strings.ToLower(strings.TrimSpace(w)); w != "" && strings.Contains(s, w) {
			return true
		}
	}
	return false
}

var stopIDPattern = regexp.MustCompile(`^(de:\d+:\d+|\d{5,})`)

// resolve returns the stop ID, asking the stop finder only once.
func (p *departures) resolve(ctx context.Context) (string, error) {
	p.mu.Lock()
	id := p.stopID
	p.mu.Unlock()
	if id != "" {
		return id, nil
	}
	if stopIDPattern.MatchString(p.stop) {
		id = p.stop
	} else {
		q := url.Values{
			"outputFormat": {"rapidJSON"}, "type_sf": {"any"}, "name_sf": {p.stop},
			"locationServerActive": {"1"}, "language": {"de"},
		}
		body, err := p.get(ctx, "/XML_STOPFINDER_REQUEST", q)
		if err != nil {
			return "", err
		}
		loc, err := parseStopFinder(body)
		if err != nil {
			return "", fmt.Errorf("stop %q: %w", p.stop, err)
		}
		id = loc.ID
		p.mu.Lock()
		p.stopName = loc.Name
		p.stopOnly = loc.stopNameOnly()
		if loc.Parent.Type == "locality" {
			p.locality = loc.Parent.Name
		}
		p.mu.Unlock()
	}
	p.mu.Lock()
	p.stopID = id
	p.mu.Unlock()
	return id, nil
}

type location struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	DisassembledName string `json:"disassembledName"` // name without the locality, e.g. "Marktplatz"
	Type             string `json:"type"`
	Best             bool   `json:"isBest"`
	// Parent is the locality of a stop: {"name":"Bielefeld","type":"locality"}.
	Parent struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"parent"`
}

// stopNameOnly is the stop name without its locality: EFA's disassembledName
// ("Marktplatz") when present, else the part after "City, ".
func (l location) stopNameOnly() string {
	if l.DisassembledName != "" {
		return l.DisassembledName
	}
	if _, after, ok := strings.Cut(l.Name, ", "); ok && after != "" {
		return after
	}
	return l.Name
}

// parseStopFinder picks the best stop of an XML_STOPFINDER_REQUEST answer.
func parseStopFinder(body []byte) (*location, error) {
	var r struct {
		Locations []location `json:"locations"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("invalid stop finder answer: %w", err)
	}
	var first *location
	for i, l := range r.Locations {
		if l.Type != "stop" || l.ID == "" {
			continue
		}
		if l.Best {
			return &r.Locations[i], nil
		}
		if first == nil {
			first = &r.Locations[i]
		}
	}
	if first == nil {
		return nil, errors.New("no stop found")
	}
	return first, nil
}

func (p *departures) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.base+path+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s%s", resp.StatusCode, p.base, path)
	}
	return body, nil
}

// fetch reads the departure monitor. More events than shown are requested,
// because the filters remove some.
func (p *departures) fetch(ctx context.Context, _ time.Time) ([]departure, error) {
	id, err := p.resolve(ctx)
	if err != nil {
		return nil, err
	}
	q := url.Values{
		"outputFormat": {"rapidJSON"}, "coordOutputFormat": {"WGS84[dd.ddddd]"},
		"type_dm": {"stop"}, "name_dm": {id}, "mode": {"direct"},
		"useRealtime": {"1"}, "useProxFootSearch": {"0"}, "language": {"de"},
		"limit": {strconv.Itoa(max(p.max*3, 20))},
	}
	body, err := p.get(ctx, "/XML_DM_REQUEST", q)
	if err != nil {
		return nil, err
	}
	deps, loc, err := parseDepartures(body)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	if loc.Name != "" {
		p.stopName = loc.Name
		p.stopOnly = loc.stopNameOnly()
	}
	if loc.Parent.Type == "locality" && loc.Parent.Name != "" {
		p.locality = loc.Parent.Name
	}
	p.mu.Unlock()
	return deps, nil
}

// stopEvent is the part of an XML_DM_REQUEST event the widget uses.
type stopEvent struct {
	RealtimeStatus []string  `json:"realtimeStatus"`
	Planned        time.Time `json:"departureTimePlanned"`
	Estimated      time.Time `json:"departureTimeEstimated"`
	Location       struct {
		Properties struct {
			Platform string `json:"platform"`
		} `json:"properties"`
	} `json:"location"`
	Transportation struct {
		Number           string `json:"number"`
		DisassembledName string `json:"disassembledName"`
		Name             string `json:"name"`
		Destination      struct {
			Name string `json:"name"`
		} `json:"destination"`
	} `json:"transportation"`
}

// parseDepartures decodes an XML_DM_REQUEST answer, sorted by the real
// departure time, and returns the monitored stop with its locality.
func parseDepartures(body []byte) ([]departure, location, error) {
	var r struct {
		SystemMessages []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"systemMessages"`
		Locations  []location  `json:"locations"`
		StopEvents []stopEvent `json:"stopEvents"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, location{}, fmt.Errorf("invalid departure monitor answer: %w", err)
	}
	if len(r.StopEvents) == 0 {
		for _, m := range r.SystemMessages {
			if m.Type == "error" {
				return nil, location{}, fmt.Errorf("EFA: %s", m.Text)
			}
		}
	}
	var out []departure
	for _, e := range r.StopEvents {
		if e.Planned.IsZero() {
			continue
		}
		t := e.Transportation
		line := firstNonEmpty(t.Number, t.DisassembledName, t.Name)
		d := departure{Line: line, Destination: t.Destination.Name, Planned: e.Planned,
			Expected: e.Estimated, Platform: e.Location.Properties.Platform}
		for _, s := range e.RealtimeStatus {
			if strings.Contains(strings.ToUpper(s), "CANCEL") {
				d.Cancelled = true
			}
		}
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].when().Before(out[j].when()) })
	var loc location
	if len(r.Locations) > 0 {
		loc = r.Locations[0]
	}
	return out, loc, nil
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

func (p *departures) Prepare(req *plugin.Request) error {
	select {
	case <-p.ready:
	case <-time.After(10 * time.Second):
		return errors.New("departures are still loading")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.data == nil && p.loadErr != nil {
		return p.loadErr
	}
	p.cur = p.visible(p.data, req.Now)
	p.curName = p.stopName
	p.curStop = p.stopOnly
	p.curLocal = p.locality
	if p.curLocal == "" {
		p.curLocal, _, _ = strings.Cut(p.stopName, ", ") // "City, Stop" without a parent
		if p.curLocal == p.stopName {
			p.curLocal = ""
		}
	}
	if p.curStop == "" {
		if _, after, ok := strings.Cut(p.stopName, ", "); ok && after != "" {
			p.curStop = after
		} else {
			p.curStop = p.stopName
		}
	}
	p.req, p.lang, p.location = req, req.Lang(), req.Location()
	return nil
}

// MaxAge: the countdown moves every minute, so results live half of it.
func (p *departures) MaxAge(*plugin.Request) time.Duration { return 30 * time.Second }
