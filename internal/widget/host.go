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
	"sort"
	"sync"
	"time"

	"github.com/maehld/paperboy/internal/page"
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

	mu       sync.Mutex
	ctx      context.Context // ends all plugin processes; nil = never
	specs    map[string]Spec // last configuration, for background refreshes
	env      Env
	cache    map[string]*entry
	procs    map[string]*process
	running  map[string]*sync.Mutex // one request per plugin at a time
	failing  map[string]time.Time   // plugin -> no new request before
	changes  map[string]int         // plugin -> number of "changed" events
	builtMu  sync.Mutex
	builtCfg map[string]bool
}

// entry is one cached widget and what is needed to render it again.
type entry struct {
	job      job
	img      image.Image
	lines    []string
	stale    bool
	expires  time.Time
	lastUsed time.Time
}

// job is one widget request with its request header.
type job struct {
	plugin string
	action string
	levels int
	page   plugin.PageInfo
	widget plugin.WidgetRequest
}

func (j job) key(env Env) string {
	args, _ := json.Marshal(j.widget.Args) // map keys are sorted
	style, _ := json.Marshal(j.widget.Style)
	page, _ := json.Marshal(j.page)
	return fmt.Sprintf("%s|%s|%s|%d|%dx%d|%s|%s|%s|%s|%s", j.plugin, j.widget.Widget, j.action, j.levels,
		j.widget.Width, j.widget.Height, args, style, page, env.Language, env.Timezone)
}

// group is the batching key: the request header without the widgets.
func (j job) group() string {
	page, _ := json.Marshal(j.page)
	return fmt.Sprintf("%s|%s|%d|%s", j.plugin, j.action, j.levels, page)
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
	// with the old configuration.
	for name, spec := range old {
		if now, ok := specs[name]; ok && sameSpec(now, spec) {
			continue
		}
		for k, e := range h.cache {
			if e.job.plugin == name {
				delete(h.cache, k)
			}
		}
	}
	h.mu.Unlock()
	for _, p := range stop {
		slog.Info("plugin stopped", "plugin", p.spec.Name, "reason", "configuration changed")
		go p.stop()
	}
}

// Fetch returns one result per widget of the batch. Cached results are used
// while they are valid; the rest of one plugin goes out in one request.
func (h *Host) Fetch(ctx context.Context, b page.Batch) []page.Result {
	h.mu.Lock()
	specs, env := h.specs, h.env
	h.mu.Unlock()
	action, levels := plugin.ActionRender, b.Levels
	if b.Text {
		action, levels = plugin.ActionText, 0
	}
	out := make([]page.Result, len(b.Widgets))
	byPlugin := map[string][]int{}
	jobs := make([]job, len(b.Widgets))
	for i, w := range b.Widgets {
		jobs[i] = job{plugin: w.Plugin, action: action, levels: levels, page: b.Page,
			widget: plugin.WidgetRequest{Widget: w.Widget, Width: w.Width, Height: w.Height, Args: w.Args, Style: w.Style}}
		byPlugin[w.Plugin] = append(byPlugin[w.Plugin], i)
	}
	names := make([]string, 0, len(byPlugin))
	for name := range byPlugin {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		idx := byPlugin[name]
		if handler, ok := h.Builtin[name]; ok {
			h.builtin(name, handler, jobs, idx, env, out)
			continue
		}
		spec, ok := specs[name]
		if !ok {
			for _, i := range idx {
				out[i] = page.Result{Err: fmt.Errorf("plugin %q is not configured", name)}
			}
			continue
		}
		h.fetchPlugin(ctx, spec, env, jobs, idx, out)
	}
	return out
}

func (h *Host) fetchPlugin(ctx context.Context, spec Spec, env Env, jobs []job, idx []int, out []page.Result) {
	now := h.now()
	var missing []job
	var missingIdx []int
	h.mu.Lock()
	for _, i := range idx {
		if e, ok := h.cache[jobs[i].key(env)]; ok {
			e.lastUsed = now
			if now.Before(e.expires) {
				out[i] = e.result(false)
				continue
			}
		}
		missing = append(missing, jobs[i])
		missingIdx = append(missingIdx, i)
	}
	h.mu.Unlock()
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
	return page.Result{Image: e.img, Lines: e.lines, Stale: stale || e.stale}
}

// run sends the jobs of one plugin and request header group, stores the
// results and returns them in order. err is set when the request failed as
// a whole.
func (h *Host) run(ctx context.Context, spec Spec, env Env, jobs []job) ([]page.Result, error) {
	lock := h.pluginLock(spec.Name)
	lock.Lock()
	defer lock.Unlock()

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
			Timezone: env.Timezone, Now: h.now(), Levels: first.levels, Page: first.page}
		for _, i := range idx {
			req.Widgets = append(req.Widgets, jobs[i].widget)
		}
		start := time.Now()
		msg, err := h.call(ctx, spec, req)
		if err == nil && len(msg.Widgets) != len(idx) {
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
			r, err := decode(j, msg.Widgets[n])
			if err != nil {
				err = fmt.Errorf("%s/%s: %w", spec.Name, j.widget.Widget, err)
				failed = cmp(failed, err)
				out[i] = page.Result{Err: err}
				continue
			}
			out[i] = r
			h.store(j, env, &entry{job: j, img: r.Image, lines: r.Lines, expires: expires, lastUsed: now})
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
	if j.action == plugin.ActionText {
		if r.Lines == nil {
			return page.Result{}, errors.New("no lines")
		}
		return page.Result{Lines: r.Lines}, nil
	}
	if len(r.PNG) == 0 {
		return page.Result{}, errors.New("no image")
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

// builtin answers widgets of an in-process plugin directly.
func (h *Host) builtin(name string, handler plugin.Handler, jobs []job, idx []int, env Env, out []page.Result) {
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
	for _, i := range idx {
		g := jobs[i].group()
		if _, ok := groups[g]; !ok {
			order = append(order, g)
		}
		groups[g] = append(groups[g], i)
	}
	for _, g := range order {
		ids := groups[g]
		first := jobs[ids[0]]
		req := plugin.Request{Version: plugin.Version, Action: first.action, Language: env.Language,
			Timezone: env.Timezone, Now: h.now(), Levels: first.levels, Page: first.page}
		for _, i := range ids {
			req.Widgets = append(req.Widgets, jobs[i].widget)
		}
		msg := plugin.Handle(handler, &req)
		for n, i := range ids {
			if msg.Error != "" {
				out[i] = page.Result{Err: errors.New(msg.Error)}
				continue
			}
			r, err := decode(jobs[i], msg.Widgets[n])
			if err != nil {
				r = page.Result{Err: fmt.Errorf("%s/%s: %w", name, jobs[i].widget.Widget, err)}
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
	lock := h.pluginLock(name)
	lock.Lock()
	defer lock.Unlock()
	msg, err := h.call(ctx, spec, plugin.Request{Version: plugin.Version, Action: plugin.ActionDescribe, Now: h.now()})
	if err != nil {
		return nil, err
	}
	if msg.Description == nil {
		return nil, errors.New("answer without description")
	}
	return msg.Description, nil
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
	lock := h.pluginLock(name)
	lock.Lock()
	defer lock.Unlock()
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
	lock := h.pluginLock(name)
	lock.Lock()
	defer lock.Unlock()
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
	h.mu.Lock()
	env, specs := h.env, h.specs
	var due []job
	for k, e := range h.cache {
		if now.Sub(e.lastUsed) > idleAfter {
			delete(h.cache, k)
			continue
		}
		if e.expires.Sub(now) < refreshAhead && !now.Before(h.failing[e.job.plugin]) {
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
}

func (h *Host) store(j job, env Env, e *entry) {
	h.mu.Lock()
	defer h.mu.Unlock()
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
