package paperboy

import (
	_ "embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/maehld/paperboy/internal/stats"
	"github.com/maehld/paperboy/internal/widget"
)

// The runtime page answers what Paperboy costs and where a sync spends its
// time (ADR 0104). It reads live numbers only: no history is written, so the
// page shows what happened since the process started.

//go:embed ui/stats.html
var statsTemplate string

var statsTmpl = template.Must(template.New("stats").Parse(statsTemplate))

// statsFact is one row of the description lists: what is measured and its
// display-ready value.
type statsFact struct {
	Label string
	Value string
}

// statsPlugin is one row of the plugin table.
type statsPlugin struct {
	Name      string
	State     string // running, stopped, failing or built-in
	Program   string // version and program file
	PID       string
	Memory    string
	CPU       string
	Requests  string
	Answer    string // average and p95 answer time of the plugin
	Wait      string // average and p95 wait for another request of the same plugin
	Cache     string // entries and bytes
	LastError string
}

// statsAge is one row of the cache age table.
type statsAge struct {
	Range   string
	Entries string
	Images  string
	Bytes   string
}

// statsTiming is one row of the timing table.
type statsTiming struct {
	Operation string
	Count     string
	Last      string
	Avg       string
	P50       string
	P95       string
	Max       string
}

// statsSync is one row of the device table: the syncs and text polls of one
// device with their durations.
type statsSync struct {
	Device  string
	Syncs   string
	Last    string
	Avg     string
	P95     string
	Max     string
	Polls   string
	PollAvg string
}

// statsView is the whole page.
type statsView struct {
	Version   string
	GoVersion string
	Platform  string
	Paperboy  []statsFact
	Plugins   []statsPlugin
	Cache     []statsFact
	Ages      []statsAge
	PageCache []statsFact
	Timings   []statsTiming
	Syncs     []statsSync
}

// timingOrder lists the operations in the order a sync runs them; everything
// else follows sorted by name.
var timingOrder = []string{
	"sync.total", "sync.config", "sync.lock", "sync.conditions", "sync.plan",
	"sync.render", "render.widgets", "render.encode", "sync.action",
	"sync.state_write", "render.page", "poll.total",
}

// timingLabels name the operations for the operator.
var timingLabels = map[string]string{
	"sync.total":       "Sync, whole request",
	"sync.config":      "Configuration snapshot",
	"sync.lock":        "Waiting for the state lock",
	"sync.conditions":  "Plugin conditions",
	"sync.plan":        "Plan selection (state lock held)",
	"sync.render":      "Page render",
	"render.widgets":   "… waiting for plugins",
	"render.encode":    "… PNG encoding",
	"sync.action":      "Running a menu action",
	"sync.state_write": "Writing the state file",
	"render.page":      "Every page render (devices, preview, editor)",
	"poll.total":       "Text poll, whole request",
}

// handleStats shows the runtime numbers of Paperboy, its plugins and its
// caches (ADR 0104).
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	loc := cfg.Timezone
	if loc == nil {
		loc = time.UTC
	}
	now := s.now()
	view := s.statsView(now, loc)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := statsTmpl.Execute(w, view); err != nil {
		slog.Error("runtime page rendering failed", "error", err)
	}
}

func (s *Server) statsView(now time.Time, loc *time.Location) statsView {
	view := statsView{
		Version:   versionText(build, loc),
		GoVersion: build.Go,
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
	view.Paperboy = s.paperboyFacts(now, loc)
	view.Plugins, view.Cache, view.Ages = s.pluginFacts(now, loc)
	view.PageCache = s.pageCacheFacts(now)
	view.Timings, view.Syncs = s.timingFacts()
	return view
}

// paperboyFacts is what the process itself reports: version, uptime, memory
// and CPU.
func (s *Server) paperboyFacts(now time.Time, loc *time.Location) []statsFact {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	facts := []statsFact{
		{Label: "Started", Value: s.started.In(loc).Format("2006-01-02 15:04") + " · " + agoText(s.started, now)},
		{Label: "Goroutines", Value: strconv.Itoa(runtime.NumGoroutine())},
		{Label: "GOMAXPROCS", Value: strconv.Itoa(runtime.GOMAXPROCS(0))},
		{Label: "Heap in use", Value: bytesText(int64(mem.HeapInuse)) + " of " + bytesText(int64(mem.Sys)) + " system"},
		{Label: "Live heap", Value: bytesText(int64(mem.HeapAlloc)) + " in " + strconv.Itoa(int(mem.HeapObjects)) + " objects"},
		{Label: "Stack", Value: bytesText(int64(mem.StackInuse))},
		{Label: "GC", Value: fmt.Sprintf("%d cycles · next at %s", mem.NumGC, bytesText(int64(mem.NextGC)))},
		{Label: "Last GC pause", Value: latencyText(lastPause(mem))},
	}
	if p, ok := stats.Process(os.Getpid()); ok {
		if p.RSS > 0 {
			facts = append(facts, statsFact{Label: "Resident memory", Value: bytesText(int64(p.RSS))})
		}
		if p.CPUSeconds > 0 {
			text := latencyText(time.Duration(p.CPUSeconds*float64(time.Second))) + " in total"
			percent, window, measured := s.cpuPercent(p.CPUSeconds, now)
			if measured {
				text += " · " + percentText(percent) + " since the last view (" + spanText(window) + ")"
			} else {
				text += " · the next view shows the percentage"
			}
			facts = append(facts, statsFact{Label: "CPU", Value: text})
		}
	} else {
		facts = append(facts, statsFact{Label: "Resident memory", Value: "not reported on " + runtime.GOOS})
	}
	return facts
}

// lastPause is the duration of the most recent garbage collection.
func lastPause(mem runtime.MemStats) time.Duration {
	if mem.NumGC == 0 {
		return 0
	}
	return time.Duration(mem.PauseNs[(mem.NumGC+255)%256])
}

// cpuPercent turns the cumulative CPU time of Paperboy into a rate over the
// time since the last view of the page.
func (s *Server) cpuPercent(total float64, now time.Time) (float64, time.Duration, bool) {
	s.cpuMu.Lock()
	defer s.cpuMu.Unlock()
	return s.cpu.Observe(total, now)
}

// pluginCPUPercent does the same for one plugin process, keyed by its PID so a
// restart starts a new measurement.
func (s *Server) pluginCPUPercent(pid int, cpu float64, now time.Time) (float64, time.Duration, bool) {
	s.cpuMu.Lock()
	defer s.cpuMu.Unlock()
	g := s.pluginCPU[pid]
	if g == nil {
		g = &stats.Gauge{}
		s.pluginCPU[pid] = g
	}
	return g.Observe(cpu, now)
}

// pluginFacts builds the plugin table, the cache totals and the age buckets.
func (s *Server) pluginFacts(now time.Time, loc *time.Location) ([]statsPlugin, []statsFact, []statsAge) {
	rows, cache := s.widgets.Stats(now)
	plugins := make([]statsPlugin, 0, len(rows))
	live := map[int]bool{}
	for _, p := range rows {
		row := statsPlugin{
			Name:      p.Name,
			Program:   programText(p.Version, p.Program),
			Requests:  requestText(p.Requests, p.Errors),
			Answer:    timingText(p.Answer),
			Wait:      timingText(p.Wait),
			Cache:     strconv.Itoa(p.CacheEntries) + " · " + bytesText(p.CacheBytes),
			LastError: p.LastError,
		}
		switch {
		case p.Builtin:
			row.State = "built-in"
		case p.Running && p.Failing:
			row.State = "failing until " + p.RetryAt.In(loc).Format("15:04:05")
		case p.Running:
			row.State = "running since " + p.Started.In(loc).Format("15:04")
			if p.Starts > 1 {
				row.State += fmt.Sprintf(" · started %d times", p.Starts)
			}
		default:
			row.State = "not running"
		}
		if p.PID != 0 && p.Running {
			live[p.PID] = true
			row.PID = strconv.Itoa(p.PID)
			row.Memory = bytesText(int64(p.Proc.RSS))
			if p.Proc.RSS == 0 {
				row.Memory = "–"
			}
			if percent, window, ok := s.pluginCPUPercent(p.PID, p.Proc.CPUSeconds, now); ok {
				row.CPU = percentText(percent) + " · " + spanText(window)
			} else {
				row.CPU = "measuring"
			}
		}
		// A built-in plugin, or a platform without process numbers, has
		// nothing to show here; an empty cell would look like a zero.
		if row.Memory == "" {
			row.Memory = "–"
		}
		if row.CPU == "" {
			row.CPU = "–"
		}
		plugins = append(plugins, row)
	}
	// Forget the gauges of plugin processes that are gone.
	s.cpuMu.Lock()
	for pid := range s.pluginCPU {
		if !live[pid] {
			delete(s.pluginCPU, pid)
		}
	}
	s.cpuMu.Unlock()

	facts := []statsFact{
		{Label: "Widgets", Value: fmt.Sprintf("%d entries · %d with an image · %s", cache.Entries, cache.Images, bytesText(cache.Bytes))},
		{Label: "Stale", Value: strconv.Itoa(cache.Stale)},
		{Label: "Past expiry", Value: strconv.Itoa(cache.Expired)},
		{Label: "Oldest", Value: agoText(cache.Oldest, now)},
		{Label: "Evicted", Value: strconv.FormatInt(cache.Evicted, 10)},
	}
	ages := make([]statsAge, 0, len(cache.Ages))
	for i, b := range cache.Ages {
		ages = append(ages, statsAge{
			Range:   ageRange(i, cache.Ages),
			Entries: strconv.Itoa(b.Entries),
			Images:  strconv.Itoa(b.Images),
			Bytes:   bytesText(b.Bytes),
		})
	}
	return plugins, facts, ages
}

// ageRange names the age bucket of row i: its upper bound and the bound of the
// bucket above it.
func ageRange(i int, buckets []widget.AgeBucket) string {
	within := buckets[i].Within
	switch {
	case i == 0:
		return "< " + spanText(within)
	case within == 0:
		return "≥ " + spanText(buckets[i-1].Within)
	}
	return spanText(buckets[i-1].Within) + " – " + spanText(within)
}

// pageCacheFacts counts what Paperboy keeps in memory besides the widget cache:
// rendered pages, notice pages and the parsed page and plan files.
func (s *Server) pageCacheFacts(now time.Time) []statsFact {
	s.mu.Lock()
	var rendered, bytes int64
	var oldest time.Time
	for _, rp := range s.rendered {
		rendered++
		bytes += int64(len(rp.data) + len(rp.pix))
		if oldest.IsZero() || rp.at.Before(oldest) {
			oldest = rp.at
		}
	}
	var hints, hintBytes int64
	var oldestHint time.Time
	for _, h := range s.hints {
		hints++
		hintBytes += int64(len(h.data))
		if oldestHint.IsZero() || h.at.Before(oldestHint) {
			oldestHint = h.at
		}
	}
	s.mu.Unlock()
	pageFiles, pageBytes := s.pages.stats()
	planFiles, planBytes := s.plans.stats()
	return []statsFact{
		{Label: "Rendered pages", Value: fmt.Sprintf("%d · %s · oldest %s", rendered, bytesText(bytes), agoText(oldest, now))},
		{Label: "Notice pages", Value: fmt.Sprintf("%d · %s · oldest %s", hints, bytesText(hintBytes), agoText(oldestHint, now))},
		{Label: "Page files", Value: fmt.Sprintf("%d parsed · %s", pageFiles, bytesText(pageBytes))},
		{Label: "Plan files", Value: fmt.Sprintf("%d parsed · %s", planFiles, bytesText(planBytes))},
	}
}

// timingFacts splits the timers into the operations of a request and the rows
// per device.
func (s *Server) timingFacts() ([]statsTiming, []statsSync) {
	all := s.stats.Timings()
	byName := make(map[string]stats.Timing, len(all))
	devices := map[string]*statsSync{}
	device := func(id string) *statsSync {
		row := devices[id]
		if row == nil {
			row = &statsSync{Device: id}
			devices[id] = row
		}
		return row
	}
	for _, t := range all {
		switch {
		case strings.HasPrefix(t.Name, "sync.device:"):
			row := device(strings.TrimPrefix(t.Name, "sync.device:"))
			row.Syncs, row.Last = strconv.Itoa(t.Count), latencyText(t.Last)
			row.Avg, row.P95, row.Max = latencyText(t.Avg), latencyText(t.P95), latencyText(t.Max)
		case strings.HasPrefix(t.Name, "poll.device:"):
			row := device(strings.TrimPrefix(t.Name, "poll.device:"))
			row.Polls, row.PollAvg = strconv.Itoa(t.Count), latencyText(t.Avg)
		default:
			byName[t.Name] = t.Timing
		}
	}
	timings := make([]statsTiming, 0, len(byName))
	seen := map[string]bool{}
	add := func(name string, t stats.Timing) {
		seen[name] = true
		label := timingLabels[name]
		if label == "" {
			label = name
		}
		timings = append(timings, statsTiming{
			Operation: label,
			Count:     strconv.Itoa(t.Count),
			Last:      latencyText(t.Last),
			Avg:       latencyText(t.Avg),
			P50:       latencyText(t.P50),
			P95:       latencyText(t.P95),
			Max:       latencyText(t.Max),
		})
	}
	for _, name := range timingOrder {
		if t, ok := byName[name]; ok {
			add(name, t)
		}
	}
	rest := make([]string, 0, len(byName))
	for name := range byName {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	for _, name := range rest {
		add(name, byName[name])
	}
	syncs := make([]statsSync, 0, len(devices))
	for _, row := range devices {
		if row.Syncs == "" {
			row.Syncs = "0"
		}
		if row.Polls == "" {
			row.Polls = "0"
		}
		for _, text := range []*string{&row.Last, &row.Avg, &row.P95, &row.Max, &row.PollAvg} {
			if *text == "" {
				*text = "–"
			}
		}
		syncs = append(syncs, *row)
	}
	sort.Slice(syncs, func(i, j int) bool { return syncs[i].Device < syncs[j].Device })
	return timings, syncs
}

// programText names the version and the program file of a plugin.
func programText(version, program string) string {
	switch {
	case program == "" && version == "":
		return "in process"
	case version == "":
		return program
	case program == "":
		return version
	}
	return version + " · " + program
}

// requestText counts requests and names the errors when there were any.
func requestText(requests, errors int64) string {
	text := strconv.FormatInt(requests, 10)
	if errors > 0 {
		text += fmt.Sprintf(" · %d failed", errors)
	}
	return text
}

// timingText shows the average and the p95 of a timer.
func timingText(t stats.Timing) string {
	if t.Count == 0 {
		return "–"
	}
	return latencyText(t.Avg) + " avg · " + latencyText(t.P95) + " p95"
}

// bytesText prints a size with the unit that fits, e.g. "1.2 MiB".
func bytesText(n int64) string {
	switch {
	case n <= 0:
		return "0 B"
	case n < 1<<10:
		return strconv.FormatInt(n, 10) + " B"
	case n < 1<<20:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	case n < 1<<30:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
}

// latencyText prints a duration for the tables; zero means "no sample".
func latencyText(d time.Duration) string {
	switch {
	case d == 0:
		return "–"
	case d < time.Microsecond:
		return "< 1 µs"
	case d < time.Millisecond:
		return fmt.Sprintf("%d µs", d.Microseconds())
	case d < 10*time.Millisecond:
		return fmt.Sprintf("%.1f ms", float64(d)/float64(time.Millisecond))
	case d < time.Second:
		return fmt.Sprintf("%d ms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.2f s", d.Seconds())
	}
	return fmt.Sprintf("%.1f min", d.Minutes())
}

// spanText prints a window such as "1 min" or "24 h".
func spanText(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 24*time.Hour:
		if d%time.Hour == 0 {
			return fmt.Sprintf("%d h", int(d.Hours()))
		}
		return fmt.Sprintf("%.0f min", d.Minutes())
	}
	if d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%d d", int(d.Hours()/24))
	}
	return fmt.Sprintf("%.0f h", d.Hours())
}

// percentText prints a CPU share of one core.
func percentText(p float64) string {
	switch {
	case p <= 0:
		return "0 %"
	case p < 0.1:
		return "< 0.1 %"
	case p < 10:
		return fmt.Sprintf("%.1f %%", p)
	}
	return fmt.Sprintf("%.0f %%", p)
}
