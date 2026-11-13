// Package stats collects the runtime numbers the operator page shows: named
// timings with percentiles, CPU rates from cumulative counters and what the
// operating system reports about a process (ADR 0104).
//
// The instrumentation must never change what Paperboy does, so a timer costs
// one clock reading and a short lock around an append; reading a snapshot is
// the only place that sorts.
package stats

import (
	"math"
	"sort"
	"sync"
	"time"
)

// reservoir is how many recent durations a timer keeps. Percentiles come from
// these samples, so an old spike fades out instead of skewing the page forever.
const reservoir = 256

// Timing is what one named timer reports: how often it ran, the total, the
// average, the largest and the last duration, and two percentiles of the
// recent samples.
type Timing struct {
	Count int
	Total time.Duration
	Avg   time.Duration
	Max   time.Duration
	Last  time.Duration
	P50   time.Duration
	P95   time.Duration
}

// NamedTiming is one timer with its name, as Timings returns it.
type NamedTiming struct {
	Name string
	Timing
}

type timer struct {
	mu     sync.Mutex
	count  int
	total  time.Duration
	max    time.Duration
	last   time.Duration
	recent []time.Duration
	next   int
}

func (t *timer) add(d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.count++
	t.total += d
	t.last = d
	if d > t.max {
		t.max = d
	}
	if len(t.recent) < reservoir {
		t.recent = append(t.recent, d)
		return
	}
	t.recent[t.next] = d
	t.next = (t.next + 1) % reservoir
}

func (t *timer) snapshot() Timing {
	t.mu.Lock()
	defer t.mu.Unlock()
	tm := Timing{Count: t.count, Total: t.total, Max: t.max, Last: t.last}
	if count := time.Duration(t.count); count > 0 {
		tm.Avg = t.total / count
	}
	if len(t.recent) == 0 {
		return tm
	}
	recent := append([]time.Duration(nil), t.recent...)
	sort.Slice(recent, func(i, j int) bool { return recent[i] < recent[j] })
	tm.P50 = percentile(recent, 0.50)
	tm.P95 = percentile(recent, 0.95)
	return tm
}

// percentile picks the nearest rank of a sorted sample list: the smallest
// sample that at least p of the samples are at or below.
func percentile(sorted []time.Duration, p float64) time.Duration {
	i := int(math.Ceil(p*float64(len(sorted)))) - 1
	if i < 0 {
		i = 0
	}
	if i >= len(sorted) {
		i = len(sorted) - 1
	}
	return sorted[i]
}

// Registry holds the named timers of one component. A nil registry accepts
// timings and reports none, so an uninstrumented server still works.
type Registry struct {
	mu     sync.Mutex
	timers map[string]*timer
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{timers: map[string]*timer{}}
}

// Start measures one operation and records it under name when the returned
// function is called.
//
//	done := r.Start("sync.render")
//	...
//	done()
//
// In a function with several returns, defer r.Start(name)() is the shorter form.
func (r *Registry) Start(name string) func() {
	start := time.Now()
	return func() { r.Observe(name, time.Since(start)) }
}

// Observe records one duration under name.
func (r *Registry) Observe(name string, d time.Duration) {
	if r == nil {
		return
	}
	r.timer(name).add(d)
}

// Timing returns one timer; an unknown name answers a zero Timing.
func (r *Registry) Timing(name string) Timing {
	if r == nil {
		return Timing{}
	}
	r.mu.Lock()
	t, ok := r.timers[name]
	r.mu.Unlock()
	if !ok {
		return Timing{}
	}
	return t.snapshot()
}

// Timings returns every timer sorted by name.
func (r *Registry) Timings() []NamedTiming {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	list := make([]NamedTiming, 0, len(r.timers))
	for name, t := range r.timers {
		list = append(list, NamedTiming{Name: name, Timing: t.snapshot()})
	}
	r.mu.Unlock()
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

func (r *Registry) timer(name string) *timer {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.timers[name]
	if !ok {
		t = &timer{}
		r.timers[name] = t
	}
	return t
}

// Gauge turns a cumulative counter, such as the CPU seconds a process has
// used, into a rate between two observations. The caller serializes access;
// the value is meant for one process, not for many.
type Gauge struct {
	at    time.Time
	value float64
	set   bool
}

// Observe records value at now and returns the share of one core used since the
// previous observation, together with the window it was measured over. ok is
// false on the first observation, so a page can say "measuring" instead of
// inventing a number.
func (g *Gauge) Observe(value float64, now time.Time) (percent float64, window time.Duration, ok bool) {
	prev, prevAt, had := g.value, g.at, g.set
	g.value, g.at, g.set = value, now, true
	if !had {
		return 0, 0, false
	}
	window = now.Sub(prevAt)
	if window <= 0 {
		return 0, 0, false
	}
	used := value - prev
	if used < 0 { // the process was replaced or the counter was reset
		return 0, 0, false
	}
	return used / window.Seconds() * 100, window, true
}
