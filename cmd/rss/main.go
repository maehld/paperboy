// Command rss is a Paperboy plugin (ADR 0049) that shows RSS 2.0, RSS 1.0
// (RDF) and Atom feeds. Every configured feed is fetched in the background
// with a conditional GET; the widgets draw the newest entries of one feed or
// of all feeds mixed, and the newest entry of a feed on its own.
//
//	plugins:
//	  rss:
//	    command: [rss]
//	    config:
//	      feeds:
//	        - { name: Tagesschau, url: "https://www.tagesschau.de/xml/rss2/" }
//	        - { name: heise, url: "https://www.heise.de/rss/heise-atom.xml" }
//
//	pages/nachrichten.yaml:
//	  cells:
//	    - { at: [0, 1], span: [6, 4], widget: rss/feed,
//	        args: { feed: Tagesschau, summary: true } }
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

const (
	defaultRefresh = 15 * time.Minute
	minRefresh     = 5 * time.Minute
	requestTimeout = 20 * time.Second
	maxFeedBytes   = 2 << 20
	// maxEntries bounds what one feed keeps in memory; the widgets show the
	// newest few anyway.
	maxEntries    = 60
	userAgent     = "paperboy-rss (+https://github.com/maehld/paperboy)"
	retryOnErrors = 2 * time.Minute
)

func main() {
	plugin.Serve(&rss{client: &http.Client{Timeout: requestTimeout}})
}

var configKeys = map[string]plugin.Arg{
	"feeds": {
		Type: "list", Required: true,
		Description: "feeds as {name, url} entries; a plain URL uses its host as the name",
	},
	"refresh": {
		Type: "duration", Default: "15m",
		Description: "how often every feed is fetched, at least 5m",
	},
}

var timeModes = []any{"relative", "clock", "none"}

// rss is the plugin.
type rss struct {
	client  *http.Client
	refresh time.Duration
	feeds   []feedSpec

	mu    sync.Mutex
	data  []*feedData // one per configured feed, in the same order
	ready chan struct{}
	snap  snapshot
	req   *plugin.Request
}

func (p *rss) Describe() plugin.Description {
	names := []any{"all"}
	for _, f := range p.feeds {
		names = append(names, f.Name)
	}
	both := []string{plugin.KindImage, plugin.KindText}
	feedArgs := map[string]plugin.Arg{
		"feed": {
			Type: "string", Default: "all", Enum: names,
			Description: "one configured feed by name, or all for the newest entries of every feed",
		},
		"items": {
			Type: "int", Default: 0,
			Description: "most entries to show; 0 fills the cell",
		},
		"summary": {
			Type: "bool", Default: false,
			Description: "one line of the entry's summary beneath its title",
		},
		"header": {
			Type: "bool", Default: false,
			Description: "the feed's name above the list",
		},
		"source": {
			Type: "bool", Default: false,
			Description: "the feed's name on every entry; on by default for all",
		},
		"time": {
			Type: "string", Default: "relative", Enum: timeModes,
			Description: "relative age, a clock time, or nothing",
		},
	}
	headlineArgs := map[string]plugin.Arg{
		"feed": {
			Type: "string", Default: "all", Enum: names,
			Description: "one configured feed by name, or all for the newest entry of every feed",
		},
		"summary": {
			Type: "bool", Default: true,
			Description: "the entry's summary beneath its title",
		},
		"time": {
			Type: "string", Default: "relative", Enum: timeModes,
			Description: "relative age, a clock time, or nothing",
		},
	}
	return plugin.Description{
		Title:   "RSS and Atom feeds",
		Version: version(),
		Config:  configKeys,
		Widgets: []plugin.WidgetInfo{
			{
				Widget: "feed", Title: "Newest entries of a feed", Kinds: both,
				MinWidth: 160, MinHeight: 50,
				Sizes: []plugin.Span{{Columns: 3, Rows: 3}, {Columns: 3, Rows: 4}, {Columns: 3, Rows: 6}, {Columns: 6, Rows: 2}, {Columns: 6, Rows: 3}, {Columns: 6, Rows: 4}, {Columns: 6, Rows: 6}},
				Args:  feedArgs,
			},
			{
				Widget: "headline", Title: "The newest entry, large", Kinds: both,
				MinWidth: 200, MinHeight: 70,
				Sizes: []plugin.Span{{Columns: 3, Rows: 2}, {Columns: 3, Rows: 3}, {Columns: 6, Rows: 2}, {Columns: 6, Rows: 3}, {Columns: 6, Rows: 4}},
				Args:  headlineArgs,
			},
		},
	}
}

func (p *rss) Configure(cfg plugin.Values, _ *plugin.Request) error {
	if err := cfg.Check(configKeys); err != nil {
		return err
	}
	refresh, err := cfg.Duration("refresh", defaultRefresh)
	if err != nil {
		return err
	}
	if refresh < minRefresh {
		return fmt.Errorf("refresh must be at least %s", minRefresh)
	}
	feeds, err := parseFeeds(cfg["feeds"])
	if err != nil {
		return err
	}
	p.feeds, p.refresh = feeds, refresh
	return nil
}

func parseFeeds(raw any) ([]feedSpec, error) {
	list, ok := raw.([]any)
	if !ok || len(list) == 0 {
		return nil, errors.New("feeds: expected a list of {name, url} entries")
	}
	var out []feedSpec
	seen := map[string]bool{}
	for i, item := range list {
		var spec feedSpec
		switch v := item.(type) {
		case string:
			spec = feedSpec{Name: hostName(v), URL: v}
		case map[string]any:
			unknown := ""
			for key := range v {
				if key != "name" && key != "url" {
					unknown = key
					break
				}
			}
			if unknown != "" {
				return nil, fmt.Errorf("feeds[%d]: unknown key %q", i, unknown)
			}
			name, _ := v["name"].(string)
			link, _ := v["url"].(string)
			spec = feedSpec{Name: strings.TrimSpace(name), URL: strings.TrimSpace(link)}
		default:
			return nil, fmt.Errorf("feeds[%d]: expected a {name, url} entry", i)
		}
		if spec.URL == "" {
			return nil, fmt.Errorf("feeds[%d]: url is required", i)
		}
		u, err := url.Parse(spec.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("feeds[%d]: %q is not an http(s) URL", i, spec.URL)
		}
		if spec.Name == "" {
			spec.Name = hostName(spec.URL)
		}
		if seen[spec.Name] {
			return nil, fmt.Errorf("feeds[%d]: the name %q is used twice", i, spec.Name)
		}
		seen[spec.Name] = true
		out = append(out, spec)
	}
	return out, nil
}

// hostName is the fallback name of a feed given as a plain URL.
func hostName(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

// Start loads every feed now and then after refresh (sooner after errors) and
// reports a change when an entry appears or disappears.
func (p *rss) Start(ctx context.Context, notify func(widgets ...string)) {
	p.ready = make(chan struct{})
	go func() {
		first := true
		for {
			changed, failed := p.reload(ctx)
			if changed && !first {
				notify()
			}
			if first {
				close(p.ready)
				first = false
			}
			wait := p.refresh
			if failed {
				wait = min(wait, retryOnErrors)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
	}()
}

// reload fetches all feeds in parallel and reports whether the shown entries
// changed and whether one of the fetches failed.
func (p *rss) reload(ctx context.Context) (changed, failed bool) {
	p.mu.Lock()
	old := p.signature()
	prev := p.data
	p.mu.Unlock()

	next := make([]*feedData, len(p.feeds))
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i, spec := range p.feeds {
		fd := &feedData{spec: spec}
		if prev != nil && i < len(prev) {
			// Keep what the last fetch left, so a failed feed keeps its
			// entries and an unchanged feed its validators.
			fd.entries, fd.etag, fd.modified = prev[i].entries, prev[i].etag, prev[i].modified
		}
		next[i] = fd
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.fetch(ctx, fd); err != nil {
				fd.err = err
				mu.Lock()
				failed = true
				mu.Unlock()
				fmt.Fprintf(os.Stderr, "rss: %s: %v\n", fd.spec.Name, err)
			}
		}()
	}
	wg.Wait()

	p.mu.Lock()
	p.data = next
	changed = old != p.signature()
	p.mu.Unlock()
	return changed, failed
}

// signature identifies what the widgets would show: the entries of every feed
// and whether that feed currently fails.
func (p *rss) signature() string {
	var b strings.Builder
	for _, fd := range p.data {
		if fd.err != nil {
			b.WriteString("!")
		}
		for _, e := range fd.entries {
			b.WriteString(e.key())
			b.WriteByte('|')
		}
		b.WriteByte('/')
	}
	return b.String()
}

func (p *rss) Prepare(req *plugin.Request) error {
	select {
	case <-p.ready:
	case <-time.After(requestTimeout):
		return errors.New("feeds are still loading")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.snap = buildSnapshot(p.data)
	p.req = req
	return nil
}

// buildSnapshot freezes what the widgets of one request draw from.
func buildSnapshot(data []*feedData) snapshot {
	s := snapshot{
		merged: mergedEntries(data),
		byFeed: make(map[string][]entry, len(data)),
		errs:   map[string]error{},
	}
	for _, fd := range data {
		s.byFeed[fd.spec.Name] = fd.entries
		if fd.err != nil {
			s.errs[fd.spec.Name] = fd.err
		}
	}
	return s
}

func (p *rss) MaxAge(*plugin.Request) time.Duration { return p.refresh }

// now is the request's time, or the clock when a caller leaves it empty.
func (p *rss) now(req *plugin.Request) time.Time {
	if req.Now.IsZero() {
		return time.Now()
	}
	return req.Now
}

// snapshot is what one request draws from.
type snapshot struct {
	merged []entry
	byFeed map[string][]entry
	errs   map[string]error
}

// entries returns the list a widget asks for: one feed by name or all feeds
// mixed.
func (s snapshot) entries(name string) ([]entry, error) {
	if name == "" || name == "all" {
		return s.merged, nil
	}
	list, ok := s.byFeed[name]
	if !ok {
		return nil, fmt.Errorf("unknown feed %q", name)
	}
	if len(list) == 0 {
		if err := s.errs[name]; err != nil {
			return nil, err
		}
	}
	return list, nil
}

// sourcesOn reports whether entries carry their feed's name: on for a mixed
// list when more than one feed is configured, unless the widget says no.
func (p *rss) sourcesOn(w plugin.WidgetRequest, name string) bool {
	if _, set := w.Args["source"]; set {
		return w.Arg().Bool("source", false)
	}
	return (name == "" || name == "all") && len(p.feeds) > 1
}
