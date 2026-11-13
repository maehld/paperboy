package widget

import (
	"image"
	"path/filepath"
	"sort"
	"time"

	"github.com/maehld/paperboy/internal/stats"
)

// ageBounds are the upper bounds of the cache age buckets, oldest bucket last:
// the runtime page answers how many images of which age the cache holds
// (ADR 0104).
var ageBounds = []time.Duration{time.Minute, 5 * time.Minute, time.Hour, 24 * time.Hour}

// AgeBucket counts the cached widgets and images of one age range. Within is
// the upper bound; zero marks the last bucket, which holds everything older
// than the previous bounds.
type AgeBucket struct {
	Within  time.Duration
	Entries int
	Images  int
	Bytes   int64
}

// PluginStat is one row of the runtime page: what a configured plugin costs,
// how its requests went and what it keeps in the cache (ADR 0104).
type PluginStat struct {
	Name         string
	Version      string // downloaded version from the program name, empty for a hand-built one
	Program      string // file name of the resolved program, empty for a built-in plugin
	Builtin      bool   // runs in the Paperboy process, no PID, not cached
	PID          int
	Running      bool
	Started      time.Time
	Starts       int // how often the host started the program
	Failing      bool
	RetryAt      time.Time
	Requests     int64
	Errors       int64
	LastError    string
	CacheEntries int
	CacheBytes   int64
	CacheStale   int
	LastUsed     time.Time
	Proc         stats.Proc // what the operating system reports, zero where it cannot
	Wait         stats.Timing
	Answer       stats.Timing
}

// CacheStat is the widget cache as a whole.
type CacheStat struct {
	Entries int
	Images  int
	Bytes   int64
	Stale   int
	Expired int // past their expiry, waiting for the background loop
	Evicted int64
	Oldest  time.Time
	Newest  time.Time
	Ages    []AgeBucket
}

// Stats snapshots the plugins and the cache of the host for the runtime page
// (ADR 0104). It reads the operating system's process numbers outside the
// host lock, because a /proc read must not hold up a widget request.
func (h *Host) Stats(now time.Time) ([]PluginStat, CacheStat) {
	h.mu.Lock()
	rows := make(map[string]*PluginStat, len(h.specs)+len(h.Builtin))
	for name, spec := range h.specs {
		row := &PluginStat{Name: name, Version: spec.Version}
		if len(spec.Command) > 0 {
			row.Program = filepath.Base(spec.Command[0])
		}
		rows[name] = row
	}
	for name := range h.Builtin {
		row, ok := rows[name]
		if !ok {
			row = &PluginStat{Name: name}
			rows[name] = row
		}
		row.Builtin, row.Running = true, true
	}
	procs := make(map[string]*process, len(h.procs))
	for name, p := range h.procs {
		procs[name] = p
	}
	requests, failures, lastErr, starts := h.requests, h.failures, h.lastErr, h.starts
	failing := make(map[string]time.Time, len(h.failing))
	for name, at := range h.failing {
		failing[name] = at
	}
	cache := CacheStat{Evicted: h.evicted, Ages: make([]AgeBucket, len(ageBounds)+1)}
	for i, within := range ageBounds {
		cache.Ages[i].Within = within
	}
	for _, e := range h.cache {
		size := entryBytes(e)
		row := rows[e.job.plugin]
		if row != nil {
			row.CacheEntries++
			row.CacheBytes += size
			if e.stale {
				row.CacheStale++
			}
			if e.lastUsed.After(row.LastUsed) {
				row.LastUsed = e.lastUsed
			}
		}
		cache.Entries++
		cache.Bytes += size
		if e.img != nil {
			cache.Images++
		}
		if e.stale {
			cache.Stale++
		}
		if !e.expires.IsZero() && e.expires.Before(now) {
			cache.Expired++
		}
		created := e.created
		if created.IsZero() {
			created = e.lastUsed
		}
		if cache.Oldest.IsZero() || created.Before(cache.Oldest) {
			cache.Oldest = created
		}
		if created.After(cache.Newest) {
			cache.Newest = created
		}
		b := ageBucketIndex(now.Sub(created))
		cache.Ages[b].Entries++
		cache.Ages[b].Bytes += size
		if e.img != nil {
			cache.Ages[b].Images++
		}
	}
	h.mu.Unlock()

	for name, row := range rows {
		if at, ok := failing[name]; ok {
			row.Failing, row.RetryAt = true, at
		}
		row.Requests, row.Errors, row.LastError = requests[name], failures[name], lastErr[name]
		row.Starts = starts[name]
		if p := procs[name]; p != nil && p.cmd.Process != nil {
			row.PID = p.cmd.Process.Pid
			row.Running = !p.exited()
			row.Started = p.started
		}
		row.Wait = h.registry().Timing("plugin." + name + ".wait")
		row.Answer = h.registry().Timing("plugin." + name + ".answer")
	}
	list := make([]PluginStat, 0, len(rows))
	for _, row := range rows {
		if row.PID != 0 && row.Running {
			row.Proc, _ = stats.Process(row.PID)
		}
		list = append(list, *row)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, cache
}

// ageBucketIndex picks the bucket of one age; the last bucket takes everything
// older than the last bound.
func ageBucketIndex(age time.Duration) int {
	for i, within := range ageBounds {
		if age < within {
			return i
		}
	}
	return len(ageBounds)
}

// entryBytes is the memory one cached widget holds: its image or its lines.
func entryBytes(e *entry) int64 {
	var n int64
	if e.img != nil {
		n += imageBytes(e.img)
	}
	for _, line := range e.lines {
		n += int64(len(line))
	}
	return n
}

// imageBytes is the pixel memory of a decoded widget image.
func imageBytes(img image.Image) int64 {
	switch v := img.(type) {
	case *image.Paletted:
		return int64(len(v.Pix))
	case *image.RGBA:
		return int64(len(v.Pix))
	case *image.NRGBA:
		return int64(len(v.Pix))
	case *image.Gray:
		return int64(len(v.Pix))
	}
	b := img.Bounds()
	return int64(b.Dx()) * int64(b.Dy()) * 4
}

// slot takes the request slot of a plugin and records how long the request
// waited for another request of the same plugin, which is where a sync spends
// time without the plugin working (ADR 0104).
func (h *Host) slot(name string) func() {
	lock := h.pluginLock(name)
	start := time.Now()
	lock.Lock()
	h.registry().Observe("plugin."+name+".wait", time.Since(start))
	return lock.Unlock
}

// registry returns the timers of the host, creating them on first use so that a
// host built as a struct literal still collects timings.
func (h *Host) registry() *stats.Registry {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.reg == nil {
		h.reg = stats.NewRegistry()
	}
	return h.reg
}

// note counts one request of a plugin and remembers its error for the runtime
// page.
func (h *Host) note(name string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.requests == nil {
		h.requests, h.failures, h.lastErr = map[string]int64{}, map[string]int64{}, map[string]string{}
	}
	h.requests[name]++
	if err == nil {
		return
	}
	h.failures[name]++
	h.lastErr[name] = err.Error()
}
