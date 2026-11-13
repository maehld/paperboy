// Command departures is a Paperboy plugin (ADR 0053) for real-time public
// transport departures. The backend "efa" talks to the rapidJSON interface of
// an EFA installation (XML_STOPFINDER_REQUEST and XML_DM_REQUEST); by default
// that is the Westfalenfahrplan (NWL) with Lippe Mobil. It polls in the
// background and draws:
//
//   - "departures": line, destination, minutes or time, delay, cancellations.
//
// One plugin serves several stops; the widget picks one of them by name
// (ADR 0100). The widget has a text variant for character displays.
//
//	plugins:
//	  departures:
//	    command: [departures]
//	    config:
//	      stops:
//	        - { name: Marktplatz, stop: "City, Marktplatz" }
//	        - { name: Hauptbahnhof, stop: "City, Hauptbahnhof" }
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
	"stops":        {Type: "list", Required: true, Description: "the stops a widget can pick, as {name, stop} entries; a plain string uses itself as the name, and a name or ID is resolved once"},
	"lines":        {Type: "list", Description: "only these lines (e.g. [355, R82]); default all"},
	"directions":   {Type: "list", Description: "only destinations containing one of these words; default all"},
	"max":          {Type: "int", Default: 8, Description: "most departures shown per stop, 1 to 20"},
	"refresh":      {Type: "duration", Default: "60s", Description: "how often every stop is polled, at least 30s"},
	"walk_minutes": {Type: "int", Default: 0, Description: "hide departures leaving sooner than this many minutes"},
}

// stopSpec is one configured stop: the name a widget picks and the stop it
// stands for.
type stopSpec struct {
	Name  string
	Query string
}

// stop is a configured stop with what the stop finder and the departure
// monitor said about it.
type stop struct {
	spec stopSpec

	id       string // resolved once
	name     string // EFA's name, e.g. "City, Marktplatz"
	only     string // the stop without its locality, e.g. "Marktplatz"
	locality string // the locality of the stop, e.g. "City"

	data    []departure
	err     error
	lastLog string // last error written to stderr
}

// stopView is one stop as a request shows it.
type stopView struct {
	list     []departure
	err      error
	loaded   bool // data arrived at least once
	name     string
	only     string
	locality string
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
	stops   []*stop
	lines   []string
	dirs    []string
	max     int
	refresh time.Duration
	walk    time.Duration

	mu    sync.Mutex
	views map[string]*stopView // of the current request, by configured name
	ready chan struct{}

	req      *plugin.Request
	lang     string
	location *time.Location
}

func (p *departures) Describe() plugin.Description {
	both := []string{plugin.KindImage, plugin.KindText}
	names := make([]any, 0, len(p.stops))
	for _, s := range p.stops {
		names = append(names, s.spec.Name)
	}
	first := ""
	if len(p.stops) > 0 {
		first = p.stops[0].spec.Name
	}
	return plugin.Description{
		Title:   "Departures",
		Version: version(),
		Config:  configKeys,
		Widgets: []plugin.WidgetInfo{
			{Widget: "departures", Title: "Real-time departures", Kinds: both, MinWidth: 200, MinHeight: 60,
				Sizes: []plugin.Span{{Columns: 3, Rows: 3}, {Columns: 3, Rows: 4}, {Columns: 6, Rows: 3}, {Columns: 6, Rows: 4}},
				Args: map[string]plugin.Arg{
					"stop":   {Type: "string", Default: first, Enum: names, Description: "one of the configured stops by name; default the first"},
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
	if _, ok := cfg["stop"]; ok {
		return errors.New("stop is no longer a config key, list it under stops: stops: [{name: \"<name>\", stop: \"<stop>\"}]")
	}
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
	stops, err := parseStops(cfg["stops"])
	if err != nil {
		return err
	}
	p.base, p.stops, p.max, p.refresh, p.walk = u.String(), stops, max, refresh, time.Duration(walk)*time.Minute
	p.lines, p.dirs = cfg.Strings("lines"), cfg.Strings("directions")
	return nil
}

// parseStops reads the configured stops: {name, stop} entries or a plain
// string, which uses itself as the name.
func parseStops(raw any) ([]*stop, error) {
	list, ok := raw.([]any)
	if !ok || len(list) == 0 {
		return nil, errors.New("stops: expected a list of {name, stop} entries")
	}
	var out []*stop
	seen := map[string]bool{}
	for i, item := range list {
		var spec stopSpec
		switch v := item.(type) {
		case string:
			spec.Query = strings.TrimSpace(v)
		case map[string]any:
			for key := range v {
				if key != "name" && key != "stop" {
					return nil, fmt.Errorf("stops[%d]: unknown key %q", i, key)
				}
			}
			name, _ := v["name"].(string)
			query, _ := v["stop"].(string)
			spec = stopSpec{Name: strings.TrimSpace(name), Query: strings.TrimSpace(query)}
		default:
			return nil, fmt.Errorf("stops[%d]: expected a {name, stop} entry", i)
		}
		if spec.Query == "" {
			return nil, fmt.Errorf("stops[%d]: stop is required: a stop name or ID", i)
		}
		if spec.Name == "" {
			spec.Name = spec.Query
		}
		if seen[spec.Name] {
			return nil, fmt.Errorf("stops[%d]: the name %q is used twice", i, spec.Name)
		}
		seen[spec.Name] = true
		out = append(out, &stop{spec: spec})
	}
	return out, nil
}

// Start polls every stop and reports a change when the shown departures move.
func (p *departures) Start(ctx context.Context, notify func(widgets ...string)) {
	p.ready = make(chan struct{})
	go func() {
		first := true
		last := make(map[string]string, len(p.stops))
		for {
			var wg sync.WaitGroup
			for _, s := range p.stops {
				wg.Add(1)
				go func(s *stop) {
					defer wg.Done()
					p.poll(ctx, s)
				}(s)
			}
			wg.Wait()
			now := time.Now()
			moved := false
			for _, s := range p.stops {
				sum := fingerprint(p.visible(p.loaded(s), now))
				if !first && sum != last[s.spec.Name] {
					moved = true
				}
				last[s.spec.Name] = sum
			}
			if moved {
				notify()
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

// poll reads one stop and keeps its last data and the other stops untouched
// when it fails.
func (p *departures) poll(ctx context.Context, s *stop) {
	deps, err := p.fetch(ctx, s)
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		if err.Error() != s.lastLog {
			fmt.Fprintln(os.Stderr, p.errorText(s, err))
			s.lastLog = err.Error()
		}
		s.err = err
		return
	}
	s.data, s.err, s.lastLog = deps, nil, ""
}

// errorText names the stop when there is more than one.
func (p *departures) errorText(s *stop, err error) string {
	if len(p.stops) == 1 {
		return fmt.Sprintf("departures: %v", err)
	}
	return fmt.Sprintf("departures: %s: %v", s.spec.Name, err)
}

// loaded is the stop's last data.
func (p *departures) loaded(s *stop) []departure {
	p.mu.Lock()
	defer p.mu.Unlock()
	return s.data
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

// resolve returns the stop's ID, asking the stop finder only once per stop.
func (p *departures) resolve(ctx context.Context, s *stop) (string, error) {
	p.mu.Lock()
	id := s.id
	p.mu.Unlock()
	if id != "" {
		return id, nil
	}
	if stopIDPattern.MatchString(s.spec.Query) {
		id = s.spec.Query
	} else {
		q := url.Values{
			"outputFormat": {"rapidJSON"}, "type_sf": {"any"}, "name_sf": {s.spec.Query},
			"locationServerActive": {"1"}, "language": {"de"},
		}
		body, err := p.get(ctx, "/XML_STOPFINDER_REQUEST", q)
		if err != nil {
			return "", err
		}
		loc, err := parseStopFinder(body)
		if err != nil {
			return "", fmt.Errorf("stop %q: %w", s.spec.Query, err)
		}
		id = loc.ID
		p.mu.Lock()
		s.name = loc.Name
		s.only = loc.stopNameOnly()
		if loc.Parent.Type == "locality" {
			s.locality = loc.Parent.Name
		}
		p.mu.Unlock()
	}
	p.mu.Lock()
	s.id = id
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

// fetch reads the departure monitor of one stop. More events than shown are
// requested, because the filters remove some.
func (p *departures) fetch(ctx context.Context, s *stop) ([]departure, error) {
	id, err := p.resolve(ctx, s)
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
		s.name = loc.Name
		s.only = loc.stopNameOnly()
	}
	if loc.Parent.Type == "locality" && loc.Parent.Name != "" {
		s.locality = loc.Parent.Name
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
	p.views = make(map[string]*stopView, len(p.stops))
	for _, s := range p.stops {
		p.views[s.spec.Name] = p.view(s, req.Now)
	}
	p.mu.Unlock()
	p.req, p.lang, p.location = req, req.Lang(), req.Location()
	return nil
}

// view builds what one request shows of a stop; p.mu must be held.
func (p *departures) view(s *stop, now time.Time) *stopView {
	v := &stopView{
		list:     p.visible(s.data, now),
		err:      s.err,
		loaded:   s.data != nil,
		name:     s.name,
		only:     s.only,
		locality: s.locality,
	}
	v.fill(s.spec)
	return v
}

// selected is the stop the widget picks, the first configured one by default.
func (p *departures) selected(w plugin.WidgetRequest) (*stopView, error) {
	name := w.Arg().String("stop", "")
	if name == "" && len(p.stops) > 0 {
		name = p.stops[0].spec.Name
	}
	v, ok := p.views[name]
	if !ok {
		return nil, fmt.Errorf("stop: %q is not one of the configured stops", name)
	}
	if v.err != nil && !v.loaded {
		return nil, v.err
	}
	return v, nil
}

// fill completes what EFA did not say: the name itself, the stop without its
// locality and, from "City, Stop", the locality.
func (v *stopView) fill(spec stopSpec) {
	if v.name == "" {
		v.name = spec.Query
	}
	if v.locality == "" {
		if city, _, ok := strings.Cut(v.name, ", "); ok && city != v.name {
			v.locality = city
		}
	}
	if v.only == "" {
		if _, after, ok := strings.Cut(v.name, ", "); ok && after != "" {
			v.only = after
		} else {
			v.only = v.name
		}
	}
}

// MaxAge: the countdown moves every minute, so results live half of it.
func (p *departures) MaxAge(*plugin.Request) time.Duration { return 30 * time.Second }
