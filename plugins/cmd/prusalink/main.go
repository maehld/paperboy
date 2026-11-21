// Command prusalink is a Paperboy plugin (ADR 0049) that shows the state of a
// Prusa 3D printer. It reads the local PrusaLink API of the printer and keeps
// the last answer, so a page never waits for the machine.
//
//	plugins:
//	  prusalink:
//	    config:
//	      url: http://prusa-core-one.local
//	      api_key: ${PRUSALINK_API_KEY}
//
//	pages/drucker.yaml:
//	  cells:
//	    - { at: [0, 0], span: [4, 4], widget: prusalink/progress }
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

	"github.com/maehld/paperboy/sdk/plugin"
)

const (
	defaultRefresh = time.Minute
	minRefresh     = 30 * time.Second
	requestTimeout = 15 * time.Second
	retryOnErrors  = 30 * time.Second
	// digestUser is the only user the PrusaLink API knows (ADR 0149).
	digestUser = "maker"
	userAgent  = "paperboy-prusalink (+https://github.com/maehld/paperboy)"
)

// valueKeys are the states the printer reports, as the keys of the state
// value (ADR 0127). A plan condition names one of them.
var valueKeys = []string{
	"idle", "busy", "printing", "paused", "finished",
	"stopped", "error", "attention", "ready",
}

func main() {
	plugin.Serve(newPlugin())
}

func newPlugin() *prusalink {
	return &prusalink{
		client: &http.Client{Timeout: requestTimeout},
		auth:   &digestAuth{},
	}
}

// prusalink is the plugin.
type prusalink struct {
	plugin.Demand

	client  *http.Client
	base    string
	apiKey  string
	refresh time.Duration
	auth    *digestAuth

	mu    sync.Mutex
	cur   snapshot
	ready chan struct{}
	snap  snapshot
}

var configKeys = map[string]plugin.Arg{
	"url": {
		Type: "string", Required: true,
		Description: "PrusaLink address of the printer, e.g. http://prusa-core-one.local",
	},
	"api_key": {
		Type: "string", Required: true,
		Description: "PrusaLink API key from the printer's network settings",
	},
	"refresh": {
		Type: "duration", Default: "1m",
		Description: "how often the printer is read, at least 30s",
	},
}

func (p *prusalink) Describe() plugin.Description {
	image := []string{plugin.KindImage}
	return plugin.Description{
		Title:   "PrusaLink printer",
		Version: version(),
		Config:  configKeys,
		Values: []plugin.ValueSpec{
			{Name: "state", Title: "Printer state", Type: plugin.ValueEnum, Enum: valueKeys},
			{Name: "progress", Title: "Print progress", Type: plugin.ValueNumber, Unit: "%", Precision: 0},
			{Name: "time-remaining", Title: "Time remaining", Type: plugin.ValueDuration},
			{Name: "time-printing", Title: "Time printing", Type: plugin.ValueDuration},
			{Name: "nozzle", Title: "Nozzle temperature", Type: plugin.ValueNumber, Unit: "°C", Precision: 0},
			{Name: "bed", Title: "Bed temperature", Type: plugin.ValueNumber, Unit: "°C", Precision: 0},
			{Name: "job", Title: "Print job", Type: plugin.ValueText},
		},
		Widgets: []plugin.WidgetInfo{
			{
				Widget: "printer", Title: "State, progress and temperatures", Kinds: image,
				MinWidth: 160, MinHeight: 70,
				Sizes: []plugin.Span{{Columns: 3, Rows: 2}, {Columns: 3, Rows: 3}, {Columns: 6, Rows: 2}, {Columns: 6, Rows: 3}},
			},
			{
				Widget: "progress", Title: "Progress as a ring", Kinds: image,
				MinWidth: 130, MinHeight: 90,
				Sizes: []plugin.Span{{Columns: 2, Rows: 2}, {Columns: 3, Rows: 2}, {Columns: 3, Rows: 3}},
			},
			{
				Widget: "temperatures", Title: "Nozzle and bed temperature", Kinds: image,
				MinWidth: 150, MinHeight: 70,
				Sizes: []plugin.Span{{Columns: 2, Rows: 2}, {Columns: 3, Rows: 2}, {Columns: 3, Rows: 3}},
			},
		},
	}
}

func (p *prusalink) Configure(cfg plugin.Values, req *plugin.Request) error {
	if req.Version != 0 && req.Version < 9 {
		return fmt.Errorf("prusalink needs Paperboy protocol 9 or newer; this Paperboy reports %d", req.Version)
	}
	if err := cfg.Check(configKeys); err != nil {
		return err
	}
	raw := strings.TrimRight(strings.TrimSpace(cfg.String("url", "")), "/")
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("url: %q is not an http(s) URL", raw)
	}
	refresh, err := cfg.Duration("refresh", defaultRefresh)
	if err != nil {
		return err
	}
	if refresh < minRefresh {
		return fmt.Errorf("refresh must be at least %s", minRefresh)
	}
	p.base = raw
	p.apiKey = strings.TrimSpace(cfg.String("api_key", ""))
	p.refresh = refresh
	return nil
}

// Start reads the printer now and then after refresh (sooner after errors),
// and reports a change when what the widgets show moves.
func (p *prusalink) Start(ctx context.Context, notify func(widgets ...string)) {
	p.mu.Lock()
	p.ready = make(chan struct{})
	ready := p.ready
	p.mu.Unlock()
	go func() {
		first := true
		var next time.Time
		for {
			if !next.IsZero() {
				if !p.Wait(ctx, 0) {
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
			changed, failed := p.reload(ctx)
			if changed && !first {
				notify()
			}
			if first {
				close(ready)
				first = false
			}
			wait := p.refresh
			if failed {
				wait = min(wait, retryOnErrors)
			}
			next = time.Now().Add(wait)
		}
	}()
}

// reload reads the status and, while a job exists, its file name. A failed
// request keeps the data it had, so the page never goes blank.
func (p *prusalink) reload(ctx context.Context) (changed, failed bool) {
	p.mu.Lock()
	old := p.cur.signature()
	prev := p.cur
	p.mu.Unlock()

	next := prev
	var st statusResponse
	if err := p.get(ctx, "/api/v1/status", &st); err != nil {
		next.err = err
		fmt.Fprintf(os.Stderr, "prusalink: %v\n", err)
		p.mu.Lock()
		p.cur = next
		p.mu.Unlock()
		return false, true
	}
	next.status = st
	next.loaded = true
	next.err = nil

	// The status carries the progress, the job answer the file name. The job
	// endpoint answers 204 when the printer knows no job.
	next.job = jobResponse{}
	if st.Job != nil {
		var job jobResponse
		err := p.get(ctx, "/api/v1/job", &job)
		switch {
		case err == nil:
			next.job = job
		case errors.Is(err, errNoJob):
		default:
			next.err = err
			failed = true
			next.job = prev.job
			fmt.Fprintf(os.Stderr, "prusalink: %v\n", err)
		}
	}
	if next.info.Name == "" {
		var info infoResponse
		if err := p.get(ctx, "/api/v1/info", &info); err != nil {
			fmt.Fprintf(os.Stderr, "prusalink: %v\n", err)
		} else {
			next.info = info
		}
	}

	p.mu.Lock()
	p.cur = next
	changed = old != next.signature()
	p.mu.Unlock()
	return changed, failed
}

// Prepare waits for the first answer and takes the snapshot the widgets draw.
func (p *prusalink) Prepare(*plugin.Request) error {
	p.mu.Lock()
	ready := p.ready
	p.mu.Unlock()
	if ready != nil {
		select {
		case <-ready:
		case <-time.After(requestTimeout):
			return errors.New("the printer did not answer yet")
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.cur.loaded && p.cur.err != nil {
		return p.cur.err
	}
	p.snap = p.cur
	return nil
}

func (p *prusalink) MaxAge(*plugin.Request) time.Duration { return p.refresh }
