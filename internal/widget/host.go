// Package widget runs Paperboy's plugins and caches their widgets
// (ADR 0049).
//
// A plugin is a long-running process (pkg/plugin). The host starts it on
// first use, configures it with its section of paperboy.yaml, sends requests
// as JSON lines and keeps the results until the plugin's max_age runs out or
// it reports a change. A background loop renders widgets that are still in
// use again before they expire, so a device never waits for a plugin. A plugin
// that exits or hangs is restarted; meanwhile its last results are served as
// stale, and a failing plugin is not asked again for a minute.
//
// Built-in plugins run in the Paperboy process and are not cached.
package widget

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/internal/page"
	"github.com/maehld/paperboy/internal/stats"
	"github.com/maehld/paperboy/pkg/plugin"
)

const (
	// DefaultTimeout bounds one request.
	DefaultTimeout = 30 * time.Second
	// DefaultMaxAge applies when a plugin does not send max_age.
	DefaultMaxAge = 5 * time.Minute
	// retryAfter is the pause after a failed request.
	retryAfter = time.Minute
	// idleAfter drops results nobody asked for in this time.
	idleAfter = time.Hour
	// maxEntries bounds the cache; the least recently used results go first.
	maxEntries = 1000
	// refreshAhead renders a result again this long before it expires.
	refreshAhead = 30 * time.Second
	// describeTTL keeps a describe answer for the value declarations of a
	// text page, so a poll every second does not ask the plugin every time.
	describeTTL = 5 * time.Minute
	// tick is the interval of the background loop.
	tick = 5 * time.Second

	maxOutput = 16 << 20
	maxStderr = 4 << 10
)

// Spec describes one plugin from paperboy.yaml.
type Spec struct {
	Name    string
	Command []string
	Version string // downloaded plugin version, e.g. v2.1.0; empty for a hand-built or PATH program
	Timeout time.Duration
	Env     map[string]string
	Dir     string         // plugins directory: lookup root and working directory
	Config  map[string]any // sent with configure; variables already expanded
	// StateDir is the plugin's directory under state/plugins/, created before
	// the start. Paperboy keeps state.json and blobs/ in it and the plugin
	// cannot write there (ADR 0081). Empty means the plugin runs without one.
	StateDir string
}

// Env is what every request carries besides the widgets.
type Env struct {
	Language string
	Timezone string
}

// Host runs plugins and caches their widgets.
type Host struct {
	// Builtin are plugins that run in the Paperboy process.
	Builtin map[string]plugin.Handler
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Demand names the next moment each plugin is needed (ADR 0126). Nil means
	// Paperboy does not know: the background loop then refreshes every due
	// entry as before and sends no schedule.
	Demand func() map[string]time.Time

	mu       sync.Mutex
	ctx      context.Context // ends all plugin processes; nil = never
	specs    map[string]Spec // last configuration, for background refreshes
	env      Env
	cache    map[string]*entry
	procs    map[string]*process
	sent     map[string]*time.Time  // plugin -> last schedule; nil = cleared
	running  map[string]*sync.Mutex // one request per plugin at a time
	failing  map[string]time.Time   // plugin -> no new request before
	changes  map[string]int         // plugin -> number of "changed" events
	mismatch map[string]bool        // plugin -> version mismatch already logged
	blobNote map[string]bool        // plugin -> undeclared blobs already logged
	descs    map[string]descEntry   // plugin -> last describe answer, for values
	lastExit map[string]ExitInfo    // plugin -> how its last process ended (issue #306)
	declGen  map[string]int         // plugin -> generation of its declaration; cached values of an older one are stale
	builtMu  sync.Mutex
	builtCfg map[string]bool

	// What the runtime page reports (ADR 0104): the request timings per plugin
	// and how its requests went.
	reg      *stats.Registry
	requests map[string]int64
	failures map[string]int64
	lastErr  map[string]string
	starts   map[string]int
	evicted  int64
}

// descEntry is a cached describe answer. It carries the value declarations a
// text page needs to format a value.
type descEntry struct {
	desc *plugin.Description
	at   time.Time
}

// declaration is a describe answer and the generation of its declaration. A
// value entry is only valid while its generation is current: a program that
// changed declares its values anew (issue #285).
type declaration struct {
	desc *plugin.Description
	gen  int
}

// entry is one cached widget or value and what is needed to render it again.
type entry struct {
	job      job
	img      image.Image
	value    *plugin.Value
	spec     plugin.ValueSpec
	declGen  int // generation of spec; values only
	stale    bool
	created  time.Time // when the plugin drew it; the runtime page ages the cache by it (ADR 0104)
	expires  time.Time
	lastUsed time.Time
}

// job is one widget request with its request header.
type job struct {
	plugin  string
	action  string
	levels  int
	page    plugin.PageInfo
	widget  plugin.WidgetRequest
	spec    plugin.ValueSpec   // action values: the declaration of the value
	declGen int                // action values: the generation of that declaration
	device  *plugin.DeviceInfo // action values: the display the page is for
}

func (j job) key(env Env) string {
	args, _ := json.Marshal(j.widget.Args) // map keys are sorted
	style, _ := json.Marshal(j.widget.Style)
	page, _ := json.Marshal(j.page)
	device, _ := json.Marshal(j.device)
	return fmt.Sprintf("%s|%s|%s|%d|%dx%d|%s|%s|%s|%s|%s|%s", j.plugin, j.widget.Widget, j.action, j.levels,
		j.widget.Width, j.widget.Height, args, style, page, env.Language, env.Timezone, device)
}

// group is the batching key: the request header without the widgets. A
// values request carries one argument set, because its answer is keyed by
// name, so values of different arguments go out in separate requests
// (ADR 0120).
func (j job) group() string {
	page, _ := json.Marshal(j.page)
	device, _ := json.Marshal(j.device)
	args := ""
	if j.action == plugin.ActionValues {
		a, _ := json.Marshal(j.widget.Args)
		args = "|" + string(a)
	}
	return fmt.Sprintf("%s|%s|%d|%s|%s%s", j.plugin, j.action, j.levels, page, device, args)
}

// recordExit keeps how a plugin process ended, so the runtime page can say why
// it is gone (issue #306). The stderr tail travels with it, because the process
// object is dropped afterwards.
func (h *Host) recordExit(p *process, err error) {
	info := exitInfo(err, h.now())
	info.Stderr = p.stderr.lines(stderrLines)
	h.mu.Lock()
	if h.lastExit == nil {
		h.lastExit = map[string]ExitInfo{}
	}
	h.lastExit[p.spec.Name] = info
	h.mu.Unlock()
}

func (h *Host) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// Start sets the context that bounds all plugin processes and runs the
// background loop until it ends.
func (h *Host) Start(ctx context.Context) {
	h.mu.Lock()
	h.ctx = ctx
	h.mu.Unlock()
	go func() {
		t := time.NewTicker(tick)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				h.stopAll()
				return
			case <-t.C:
				h.maintain(ctx)
			}
		}
	}()
}

// Configure applies a new configuration: plugins that are gone or changed
// stop and start again on the next request.
func (h *Host) Configure(specs map[string]Spec, env Env) {
	h.mu.Lock()
	old := h.specs
	h.specs, h.env = specs, env
	var stop []*process
	for name, p := range h.procs {
		if spec, ok := specs[name]; !ok || !sameSpec(spec, p.spec) {
			stop = append(stop, p)
			delete(h.procs, name)
		}
	}
	// Results of a changed or removed plugin are dropped: they were drawn
	// with the old configuration. An unchanged plugin keeps its describe
	// answer for its TTL; only a changed specification drops it.
	for name, spec := range old {
		if now, ok := specs[name]; ok && sameSpec(now, spec) {
			continue
		}
		h.nextDeclarations(name)
		for k, e := range h.cache {
			if e.job.plugin == name {
				delete(h.cache, k)
			}
		}
		delete(h.sent, name)
	}
	h.mu.Unlock()
	for _, p := range stop {
		slog.Info("plugin stopped", "plugin", p.spec.Name, "reason", "configuration changed")
		go p.stop()
	}
}

// maxParallelPlugins bounds how many plugin processes a page asks at once.
const maxParallelPlugins = 8

// forEachPlugin runs fn for every name in parallel, at most maxParallelPlugins
// at a time. Each plugin writes its own results, so a page waits for its
// slowest plugin instead of the sum of all.
func forEachPlugin(names []string, fn func(name string)) {
	sem := make(chan struct{}, maxParallelPlugins)
	var wg sync.WaitGroup
	for _, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			fn(name)
		}()
	}
	wg.Wait()
}

// Fetch returns one result per widget of the batch. Cached results are used
// while they are valid; the rest of one plugin goes out in one request.
func (h *Host) Fetch(ctx context.Context, b page.Batch) []page.Result {
	h.mu.Lock()
	specs, env := h.specs, h.env
	h.mu.Unlock()
	action, levels := plugin.ActionRender, b.Levels
	out := make([]page.Result, len(b.Widgets))
	byPlugin := map[string][]int{}
	for i, w := range b.Widgets {
		byPlugin[w.Plugin] = append(byPlugin[w.Plugin], i)
	}
	names := make([]string, 0, len(byPlugin))
	for name := range byPlugin {
		names = append(names, name)
	}
	sort.Strings(names)
	forEachPlugin(names, func(name string) {
		idx := byPlugin[name]
		jobs := make([]job, 0, len(idx))
		for _, i := range idx {
			w := b.Widgets[i]
			jobs = append(jobs, job{plugin: w.Plugin, action: action, levels: levels, page: b.Page,
				widget: plugin.WidgetRequest{Widget: w.Widget, Width: w.Width, Height: w.Height, Args: w.Args, Style: w.Style}})
		}
		if handler, ok := h.Builtin[name]; ok {
			// Only the built-in plugin gets Paperboy's device status: its
			// widgets are not cached, while a plugin process would keep stale
			// rows until its max_age runs out (ADR 0115).
			h.builtin(name, handler, jobs, idx, env, b.ServerStatus, out)
			return
		}
		spec, ok := specs[name]
		if !ok {
			for _, i := range idx {
				out[i] = page.Result{Err: fmt.Errorf("plugin %q is not configured", name)}
			}
			return
		}
		h.fetchPlugin(ctx, spec, env, jobs, idx, out)
	})
	return out
}

// FetchValues returns one result per value of the batch (ADR 0111). Values of
// one plugin go out in one request, and the cache, max_age and changed events
// work like for widgets. The declaration of each value travels with it, so the
// page can format it; a value the plugin does not declare is an error for that
// cell.
func (h *Host) FetchValues(ctx context.Context, b page.ValueBatch) []page.Result {
	h.mu.Lock()
	specs, env := h.specs, h.env
	h.mu.Unlock()
	out := make([]page.Result, len(b.Values))
	byPlugin := map[string][]int{}
	for i, v := range b.Values {
		byPlugin[v.Plugin] = append(byPlugin[v.Plugin], i)
	}
	names := make([]string, 0, len(byPlugin))
	for name := range byPlugin {
		names = append(names, name)
	}
	sort.Strings(names)
	forEachPlugin(names, func(name string) {
		idx := byPlugin[name]
		decl, err := h.description(ctx, name)
		if err != nil {
			for _, i := range idx {
				out[i] = page.Result{Err: err}
			}
			return
		}
		known := make(map[string]plugin.ValueSpec, len(decl.desc.Values))
		for _, v := range decl.desc.Values {
			known[v.Name] = v
		}
		var jobs []job
		var jobIdx []int
		for _, i := range idx {
			v := b.Values[i]
			spec, ok := known[v.Name]
			if !ok {
				out[i] = page.Result{Err: fmt.Errorf("plugin %q does not declare value %q", name, v.Name)}
				continue
			}
			jobs = append(jobs, job{plugin: name, action: plugin.ActionValues, page: b.Page, spec: spec, declGen: decl.gen,
				device: b.Device, widget: plugin.WidgetRequest{Widget: v.Name, Args: v.Args}})
			jobIdx = append(jobIdx, i)
		}
		if len(jobs) == 0 {
			return
		}
		if handler, ok := h.Builtin[name]; ok {
			// Values carry no server status: a page of values asks a plugin
			// for its data, and only widgets draw the device status (ADR 0115).
			h.builtin(name, handler, jobs, jobIdx, env, nil, out)
			return
		}
		spec, ok := specs[name]
		if !ok {
			for _, i := range jobIdx {
				out[i] = page.Result{Err: fmt.Errorf("plugin %q is not configured", name)}
			}
			return
		}
		h.fetchPlugin(ctx, spec, env, jobs, jobIdx, out)
	})
	return out
}

// description returns a cached describe answer, so a text page that polls
// every second does not ask the plugin for its value declarations every time.
func (h *Host) description(ctx context.Context, name string) (declaration, error) {
	h.mu.Lock()
	if e, ok := h.descs[name]; ok && h.now().Sub(e.at) < describeTTL {
		d := declaration{desc: e.desc, gen: h.declGen[name]}
		h.mu.Unlock()
		return d, nil
	}
	h.mu.Unlock()
	d, err := h.Describe(ctx, name)
	if err != nil {
		return declaration{}, err
	}
	h.mu.Lock()
	if h.descs == nil {
		h.descs = map[string]descEntry{}
	}
	gen := h.declGen[name]
	// A different declaration means the cached values were formatted by an
	// older program or an older answer: drop them (issue #285).
	if old, ok := h.descs[name]; ok && !reflect.DeepEqual(old.desc.Values, d.Values) {
		gen = h.nextDeclarations(name)
	}
	h.descs[name] = descEntry{desc: d, at: h.now()}
	h.mu.Unlock()
	return declaration{desc: d, gen: gen}, nil
}

// nextDeclarations drops a plugin's describe answer and marks the values it
// cached as stale, because the program or its declaration changed. Callers
// hold h.mu. It returns the new generation.
func (h *Host) nextDeclarations(name string) int {
	delete(h.descs, name)
	if h.declGen == nil {
		h.declGen = map[string]int{}
	}
	h.declGen[name]++
	return h.declGen[name]
}

// fetchPlugin answers the jobs of one plugin; jobs[n] belongs to the batch
// index idx[n], which is where its result goes.
func (h *Host) fetchPlugin(ctx context.Context, spec Spec, env Env, jobs []job, idx []int, out []page.Result) {
	now := h.now()
	var missing []job
	var missingIdx []int
	// A panic must not leave the lock behind: a recovered render still ends
	// in this process.
	func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		for n, i := range idx {
			if e, ok := h.cache[jobs[n].key(env)]; ok {
				e.lastUsed = now
				if e.job.action == plugin.ActionValues && e.declGen != h.declGen[jobs[n].plugin] {
					// The declaration changed after this value was made: its
					// unit and precision are outdated (issue #285).
					delete(h.cache, jobs[n].key(env))
				} else if now.Before(e.expires) {
					out[i] = e.result(false)
					continue
				}
			}
			missing = append(missing, jobs[n])
			missingIdx = append(missingIdx, i)
		}
	}()
	if len(missing) == 0 {
		return
	}
	results, err := h.run(ctx, spec, env, missing)
	for n, i := range missingIdx {
		if err == nil && results[n].Err == nil {
			out[i] = results[n]
			continue
		}
		reason := err
		if reason == nil {
			reason = results[n].Err
		}
		h.mu.Lock()
		e, ok := h.cache[missing[n].key(env)]
		h.mu.Unlock()
		if ok {
			out[i] = e.result(true)
		} else {
			out[i] = page.Result{Err: reason}
		}
	}
}

func (e *entry) result(stale bool) page.Result {
	return page.Result{Image: e.img, Value: e.value, Spec: e.spec, Stale: stale || e.stale}
}

// run sends the jobs of one plugin and request header group, stores the
// results and returns them in order. err is set when the request failed as
// a whole.
func (h *Host) run(ctx context.Context, spec Spec, env Env, jobs []job) ([]page.Result, error) {
	defer h.slot(spec.Name)()

	h.mu.Lock()
	blocked := h.now().Before(h.failing[spec.Name])
	gen := h.changes[spec.Name]
	h.mu.Unlock()
	if blocked {
		return nil, errors.New("failed recently, retrying later")
	}
	out := make([]page.Result, len(jobs))
	groups := map[string][]int{}
	var order []string
	for i, j := range jobs {
		g := j.group()
		if _, ok := groups[g]; !ok {
			order = append(order, g)
		}
		groups[g] = append(groups[g], i)
	}
	for _, g := range order {
		idx := groups[g]
		first := jobs[idx[0]]
		req := plugin.Request{Version: plugin.Version, Action: first.action, Language: env.Language,
			Timezone: env.Timezone, Now: h.now(), Levels: first.levels, Page: first.page, Device: first.device}
		if first.action == plugin.ActionValues {
			req.ValueArgs = plugin.Values(first.widget.Args)
			for _, i := range idx {
				req.Values = append(req.Values, jobs[i].widget.Widget)
			}
		} else {
			for _, i := range idx {
				req.Widgets = append(req.Widgets, jobs[i].widget)
			}
		}
		start := time.Now()
		msg, err := h.call(ctx, spec, req)
		if err == nil && first.action != plugin.ActionValues && len(msg.Widgets) != len(idx) {
			err = fmt.Errorf("%d results for %d widgets", len(msg.Widgets), len(idx))
		}
		if err != nil {
			h.fail(spec.Name, err)
			return nil, err
		}
		maxAge := DefaultMaxAge
		if msg.MaxAge > 0 {
			maxAge = time.Duration(msg.MaxAge) * time.Second
		}
		now := h.now()
		h.mu.Lock()
		expires := now.Add(maxAge)
		if h.changes[spec.Name] != gen {
			expires = time.Time{} // the data changed while the plugin drew
		}
		h.mu.Unlock()
		var failed error
		for n, i := range idx {
			j := jobs[i]
			var r page.Result
			var err error
			if first.action == plugin.ActionValues {
				r, err = decodeValue(j, msg.Values)
			} else {
				r, err = decode(j, msg.Widgets[n])
			}
			if err != nil {
				err = fmt.Errorf("%s/%s: %w", spec.Name, j.widget.Widget, err)
				failed = cmp(failed, err)
				out[i] = page.Result{Err: err}
				continue
			}
			out[i] = r
			h.store(j, env, &entry{job: j, img: r.Image, value: r.Value, spec: j.spec,
				created: now, expires: expires, lastUsed: now})
		}
		if failed != nil {
			slog.Warn("plugin widget failed", "plugin", spec.Name, "error", failed)
		}
		slog.Debug("plugin answered", "plugin", spec.Name, "widgets", len(idx), "took", time.Since(start))
	}
	h.recover(spec.Name)
	return out, nil
}

func cmp(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

func decode(j job, r plugin.Result) (page.Result, error) {
	if r.Error != "" {
		return page.Result{}, errors.New(r.Error)
	}
	if len(r.PNG) == 0 {
		return page.Result{}, errors.New("no image")
	}
	// The header names the canvas, so its size is checked before the pixels
	// are allocated: a small answer cannot ask for a huge image.
	cfg, err := png.DecodeConfig(bytes.NewReader(r.PNG))
	if err != nil {
		return page.Result{}, err
	}
	if cfg.Width != j.widget.Width || cfg.Height != j.widget.Height {
		return page.Result{}, fmt.Errorf("image is %dx%d, requested %dx%d", cfg.Width, cfg.Height, j.widget.Width, j.widget.Height)
	}
	img, err := png.Decode(bytes.NewReader(r.PNG))
	if err != nil {
		return page.Result{}, err
	}
	if b := img.Bounds(); b.Dx() != j.widget.Width || b.Dy() != j.widget.Height {
		return page.Result{}, fmt.Errorf("image is %dx%d, requested %dx%d", b.Dx(), b.Dy(), j.widget.Width, j.widget.Height)
	}
	return page.Result{Image: img}, nil
}

// decodeValue reads one answer of a values request. A missing name is an
// error for that cell, so a plugin can rarely fail one value without the page.
// An enum answer may be empty, which means the plugin has no key now; a key
// outside the declaration is an error (ADR 0127).
func decodeValue(j job, values map[string]plugin.Value) (page.Result, error) {
	v, ok := values[j.widget.Widget]
	if !ok {
		return page.Result{}, errors.New("no value")
	}
	if v.Type() == "" {
		if j.spec.Type == plugin.ValueEnum {
			return page.Result{Value: &v, Spec: j.spec}, nil
		}
		return page.Result{}, errors.New("value carries no number, text, duration, timestamp or direction")
	}
	if j.spec.Type == plugin.ValueEnum {
		if v.Type() != plugin.ValueEnum {
			return page.Result{}, fmt.Errorf("value is %s, declared enum", v.Type())
		}
		for _, key := range j.spec.Enum {
			if key == *v.Enum {
				return page.Result{Value: &v, Spec: j.spec}, nil
			}
		}
		return page.Result{}, fmt.Errorf("key %q is not one of %s", *v.Enum, strings.Join(j.spec.Enum, ", "))
	}
	return page.Result{Value: &v, Spec: j.spec}, nil
}

// builtin answers widgets of an in-process plugin directly.
func (h *Host) builtin(name string, handler plugin.Handler, jobs []job, idx []int, env Env, status *plugin.ServerStatus, out []page.Result) {
	h.builtMu.Lock()
	defer h.builtMu.Unlock()
	if h.builtCfg == nil {
		h.builtCfg = map[string]bool{}
	}
	if !h.builtCfg[name] {
		plugin.Handle(handler, &plugin.Request{Version: plugin.Version, Action: plugin.ActionConfigure})
		h.builtCfg[name] = true
	}
	groups := map[string][]int{}
	var order []string
	for n := range jobs {
		g := jobs[n].group()
		if _, ok := groups[g]; !ok {
			order = append(order, g)
		}
		groups[g] = append(groups[g], n)
	}
	for _, g := range order {
		pos := groups[g]
		first := jobs[pos[0]]
		req := plugin.Request{Version: plugin.Version, Action: first.action, Language: env.Language,
			Timezone: env.Timezone, Now: h.now(), Levels: first.levels, Page: first.page,
			Device: first.device, ServerStatus: status}
		if first.action == plugin.ActionValues {
			req.ValueArgs = plugin.Values(first.widget.Args)
			for _, p := range pos {
				req.Values = append(req.Values, jobs[p].widget.Widget)
			}
		} else {
			for _, p := range pos {
				req.Widgets = append(req.Widgets, jobs[p].widget)
			}
		}
		msg := plugin.Handle(handler, &req)
		for n, p := range pos {
			i := idx[p]
			if msg.Error != "" {
				out[i] = page.Result{Err: errors.New(msg.Error)}
				continue
			}
			var r page.Result
			var err error
			if first.action == plugin.ActionValues {
				r, err = decodeValue(jobs[p], msg.Values)
			} else {
				r, err = decode(jobs[p], msg.Widgets[n])
			}
			if err != nil {
				r = page.Result{Err: fmt.Errorf("%s/%s: %w", name, jobs[p].widget.Widget, err)}
			}
			out[i] = r
		}
	}
}

// Describe asks a plugin which widgets it offers.
func (h *Host) Describe(ctx context.Context, name string) (*plugin.Description, error) {
	if handler, ok := h.Builtin[name]; ok {
		d := handler.Describe()
		return &d, nil
	}
	h.mu.Lock()
	spec, ok := h.specs[name]
	h.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("plugin %q is not configured", name)
	}
	defer h.slot(name)()
	msg, err := h.call(ctx, spec, plugin.Request{Version: plugin.Version, Action: plugin.ActionDescribe, Now: h.now()})
	if err != nil {
		return nil, err
	}
	if msg.Description == nil {
		return nil, errors.New("answer without description")
	}
	// A downloaded program carries its version in the file name only; the
	// describe answer is the only check of the two (ADR 0060, 0075).
	if reported := msg.Description.Version; reported != "" && spec.Version != "" && "v"+reported != spec.Version {
		h.versionMismatch(name, spec.Version, reported)
	}
	h.checkBlobs(spec, msg.Description.Blobs)
	return msg.Description, nil
}

// checkBlobs names files in the plugin's blobs directory once that its describe
// answer does not declare, so a renamed blob does not linger unnoticed. Nothing
// is deleted (ADR 0081).
func (h *Host) checkBlobs(spec Spec, declared []string) {
	if spec.StateDir == "" {
		return
	}
	h.mu.Lock()
	if h.blobNote == nil {
		h.blobNote = map[string]bool{}
	}
	if h.blobNote[spec.Name] {
		h.mu.Unlock()
		return
	}
	h.blobNote[spec.Name] = true
	h.mu.Unlock()
	known := make(map[string]bool, len(declared))
	for _, name := range declared {
		known[name] = true
	}
	entries, err := os.ReadDir(filepath.Join(spec.StateDir, blobsDirName))
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() && !known[e.Name()] {
			slog.Warn("plugin blob is not declared by describe; remove it by hand when it is stale",
				"plugin", spec.Name, "name", e.Name())
		}
	}
}

// versionMismatch logs once per plugin that the version a program reports
// differs from the one in its file name.
func (h *Host) versionMismatch(name, file, reported string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.mismatch == nil {
		h.mismatch = map[string]bool{}
	}
	if h.mismatch[name] {
		return
	}
	h.mismatch[name] = true
	slog.Warn("plugin version differs from its file name", "plugin", name,
		"file_name", file, "reported", reported)
}

// Actions asks a plugin for the state of concrete actions (ADR 0057). The
// answer has one entry per call, in the same order. Unlike widgets, action
// states are not cached here: the menu asks again whenever it opens, and the
// answer depends on a device and a moment rather than on a page.
func (h *Host) Actions(ctx context.Context, name string, req plugin.Request) ([]plugin.ActionState, error) {
	req.Action = plugin.ActionList
	if handler, ok := h.Builtin[name]; ok {
		h.builtMu.Lock()
		defer h.builtMu.Unlock()
		msg := plugin.Handle(handler, &req)
		if msg.Error != "" {
			return nil, errors.New(msg.Error)
		}
		if len(msg.Actions) != len(req.Actions) {
			return nil, fmt.Errorf("%d states for %d actions", len(msg.Actions), len(req.Actions))
		}
		return msg.Actions, nil
	}
	spec, err := h.spec(name)
	if err != nil {
		return nil, err
	}
	defer h.slot(name)()
	req.Version = plugin.Version
	if req.Now.IsZero() {
		req.Now = h.now()
	}
	msg, err := h.call(ctx, spec, req)
	if err != nil {
		h.fail(name, err)
		return nil, err
	}
	if len(msg.Actions) != len(req.Actions) {
		err := fmt.Errorf("%d states for %d actions", len(msg.Actions), len(req.Actions))
		h.fail(name, err)
		return nil, err
	}
	h.recover(name)
	return msg.Actions, nil
}

// Invoke runs one concrete action (ADR 0057). The plugin decides whether it
// needs a confirmation; the menu has asked before calling this.
func (h *Host) Invoke(ctx context.Context, name string, req plugin.Request) (plugin.ActionOutcome, error) {
	req.Action = plugin.ActionInvoke
	if handler, ok := h.Builtin[name]; ok {
		h.builtMu.Lock()
		defer h.builtMu.Unlock()
		msg := plugin.Handle(handler, &req)
		if msg.Error != "" {
			return plugin.ActionOutcome{}, errors.New(msg.Error)
		}
		if msg.Result == nil {
			return plugin.ActionOutcome{}, errors.New("answer without result")
		}
		return *msg.Result, nil
	}
	spec, err := h.spec(name)
	if err != nil {
		return plugin.ActionOutcome{}, err
	}
	defer h.slot(name)()
	req.Version = plugin.Version
	if req.Now.IsZero() {
		req.Now = h.now()
	}
	msg, err := h.call(ctx, spec, req)
	if err != nil {
		h.fail(name, err)
		return plugin.ActionOutcome{}, err
	}
	if msg.Result == nil {
		err := errors.New("answer without result")
		h.fail(name, err)
		return plugin.ActionOutcome{}, err
	}
	if msg.Result.Changed {
		h.expire(name, nil)
	}
	h.recover(name)
	return *msg.Result, nil
}

// spec returns the configuration of a running plugin.
func (h *Host) spec(name string) (Spec, error) {
	h.mu.Lock()
	spec, ok := h.specs[name]
	h.mu.Unlock()
	if !ok {
		return Spec{}, fmt.Errorf("plugin %q is not configured", name)
	}
	return spec, nil
}

// maintain drops idle results and renders results again that are still in
// use and expire soon or were marked changed.
func (h *Host) maintain(ctx context.Context) {
	now := h.now()
	var demand map[string]time.Time
	if h.Demand != nil {
		demand = h.Demand()
	}
	h.mu.Lock()
	env, specs := h.env, h.specs
	var due []job
	for k, e := range h.cache {
		if now.Sub(e.lastUsed) > idleAfter {
			delete(h.cache, k)
			continue
		}
		if e.job.action == plugin.ActionValues && e.declGen != h.declGen[e.job.plugin] {
			delete(h.cache, k) // formatted with an outdated declaration (issue #285)
			continue
		}
		if e.expires.Sub(now) < refreshAhead && !now.Before(h.failing[e.job.plugin]) && h.needed(e.job.plugin, demand, now) {
			due = append(due, e.job)
		}
	}
	h.evict()
	h.mu.Unlock()
	byPlugin := map[string][]job{}
	for _, j := range due {
		byPlugin[j.plugin] = append(byPlugin[j.plugin], j)
	}
	for name, jobs := range byPlugin {
		spec, ok := specs[name]
		if !ok {
			continue
		}
		if _, err := h.run(ctx, spec, env, jobs); err != nil {
			slog.Debug("background refresh failed", "plugin", name, "error", err)
		}
	}
	h.pushSchedules(demand)
}

// needed reports whether the background loop may refresh a plugin's entry now:
// only when Paperboy needs the plugin within refreshAhead. Without demand
// information (nil) it keeps the behavior before ADR 0126 and refreshes every
// due entry.
func (h *Host) needed(plugin string, demand map[string]time.Time, now time.Time) bool {
	if demand == nil {
		return true
	}
	at, ok := demand[plugin]
	return ok && !at.After(now.Add(refreshAhead))
}

// pushSchedules tells every running protocol 9 plugin when its data is next
// needed, and clears the demand when nobody needs it. It sends only when the
// moment changes by more than a minute, so the 5-second loop stays quiet.
func (h *Host) pushSchedules(demand map[string]time.Time) {
	if demand == nil {
		return
	}
	h.mu.Lock()
	procs := make(map[string]*process, len(h.procs))
	for name, p := range h.procs {
		procs[name] = p
	}
	sent := make(map[string]*time.Time, len(h.sent))
	for name, at := range h.sent {
		sent[name] = at
	}
	h.mu.Unlock()

	for name, p := range procs {
		if p.protocol() < 9 {
			continue
		}
		at, has := demand[name]
		old, was := sent[name]
		if was {
			if !has && old == nil {
				continue
			}
			if has && old != nil && old.Sub(at).Abs() < time.Minute {
				continue
			}
		}
		ev := plugin.Event{Event: plugin.EventSchedule}
		if has {
			next := at
			ev.NextRefresh = &next
		}
		if err := p.sendEvent(ev); err != nil {
			slog.Debug("schedule event failed", "plugin", name, "error", err)
			h.drop(p)
			continue
		}
		h.mu.Lock()
		if h.procs[name] == p {
			if h.sent == nil {
				h.sent = map[string]*time.Time{}
			}
			if has {
				next := at
				h.sent[name] = &next
			} else {
				h.sent[name] = nil
			}
		}
		h.mu.Unlock()
	}
}

// evict keeps the cache below maxEntries; callers hold h.mu.
func (h *Host) evict() {
	if len(h.cache) <= maxEntries {
		return
	}
	type aged struct {
		key  string
		used time.Time
	}
	list := make([]aged, 0, len(h.cache))
	for k, e := range h.cache {
		list = append(list, aged{k, e.lastUsed})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].used.Before(list[j].used) })
	for _, a := range list[:len(list)-maxEntries] {
		delete(h.cache, a.key)
	}
	h.evicted += int64(len(list) - maxEntries)
}

func (h *Host) store(j job, env Env, e *entry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if j.action == plugin.ActionValues {
		if j.declGen != h.declGen[j.plugin] {
			return // the declaration changed while the plugin answered
		}
		e.declGen = j.declGen
	}
	if h.cache == nil {
		h.cache = map[string]*entry{}
	}
	if old, ok := h.cache[j.key(env)]; ok && old.lastUsed.After(e.lastUsed) {
		e.lastUsed = old.lastUsed
	}
	h.cache[j.key(env)] = e
}

// expire marks a plugin's results as changed after a "changed" event; the
// background loop renders them again.
func (h *Host) expire(name string, widgets []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.changes == nil {
		h.changes = map[string]int{}
	}
	h.changes[name]++
	for _, e := range h.cache {
		if e.job.plugin != name {
			continue
		}
		if len(widgets) > 0 && !contains(widgets, e.job.widget.Widget) {
			continue
		}
		e.expires = time.Time{}
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func (h *Host) pluginLock(name string) *sync.Mutex {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running == nil {
		h.running = map[string]*sync.Mutex{}
	}
	l, ok := h.running[name]
	if !ok {
		l = &sync.Mutex{}
		h.running[name] = l
	}
	return l
}

func (h *Host) fail(name string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.failing == nil {
		h.failing = map[string]time.Time{}
	}
	if _, was := h.failing[name]; !was {
		slog.Warn("plugin failed", "plugin", name, "error", err)
	}
	h.failing[name] = h.now().Add(retryAfter)
}

func (h *Host) recover(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, was := h.failing[name]; was {
		slog.Info("plugin recovered", "plugin", name)
		delete(h.failing, name)
	}
}
