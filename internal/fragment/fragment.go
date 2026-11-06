// Package fragment runs fragment plugins and caches their images (spike:
// plugin fragments). Paperboy starts a plugin with one JSON request on
// standard input (pkg/plugin), reads the PNG images from its answer and keeps
// them until the plugin's max_age runs out. A failing plugin keeps its last
// images, marked stale, and is not asked again for a minute.
package fragment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/internal/page"
	"github.com/maehld/paperboy/internal/provider"
	"github.com/maehld/paperboy/pkg/plugin"
)

const (
	// DefaultTimeout bounds one plugin run.
	DefaultTimeout = 20 * time.Second
	// DefaultMaxAge applies when a plugin does not send max_age.
	DefaultMaxAge = 5 * time.Minute
	// retryAfter is the pause after a failed run.
	retryAfter = time.Minute
	maxOutput  = 8 << 20
	maxStderr  = 4 << 10
)

// Spec describes one plugin from paperboy.yaml.
type Spec struct {
	Name    string
	Command []string
	Timeout time.Duration
	Env     map[string]string
	Dir     string // plugins directory: lookup root and working directory
}

// Env is the render environment that every request carries.
type Env struct {
	Language string
	Timezone string
	Now      time.Time
}

// Host runs plugins and caches their images. The zero value is ready to use.
type Host struct {
	mu      sync.Mutex
	cache   map[string]entry
	running map[string]*sync.Mutex // one run per plugin at a time
	failing map[string]time.Time   // plugin -> no new run before
}

type entry struct {
	img     image.Image
	expires time.Time
}

// Render returns one fragment per request. Requests for the same plugin that
// are not cached are sent in one run.
func (h *Host) Render(ctx context.Context, specs map[string]Spec, reqs []page.FragmentRequest, gray bool, env Env) []page.Fragment {
	levels := 2
	if gray {
		levels = 4
	}
	out := make([]page.Fragment, len(reqs))
	keys := make([]string, len(reqs))
	byPlugin := map[string][]int{}
	for i, r := range reqs {
		keys[i] = cacheKey(r, levels, env)
		byPlugin[r.Plugin] = append(byPlugin[r.Plugin], i)
	}
	for name, idx := range byPlugin {
		spec, ok := specs[name]
		if !ok {
			for _, i := range idx {
				out[i] = page.Fragment{Err: fmt.Errorf("plugin %q is not configured", name)}
			}
			continue
		}
		h.renderPlugin(ctx, spec, reqs, keys, idx, levels, env, out)
	}
	return out
}

func (h *Host) renderPlugin(ctx context.Context, spec Spec, reqs []page.FragmentRequest, keys []string, idx []int, levels int, env Env, out []page.Fragment) {
	lock := h.pluginLock(spec.Name)
	lock.Lock()
	defer lock.Unlock()

	now := time.Now()
	var missing []int
	for _, i := range idx {
		if e, ok := h.get(keys[i]); ok && now.Before(e.expires) {
			out[i] = page.Fragment{Image: e.img}
		} else {
			missing = append(missing, i)
		}
	}
	if len(missing) == 0 {
		return
	}
	var err error
	h.mu.Lock()
	blockedUntil := h.failing[spec.Name]
	h.mu.Unlock()
	if now.Before(blockedUntil) {
		err = errors.New("failed recently, retrying later")
	} else {
		err = h.run(ctx, spec, reqs, keys, missing, levels, env, out)
	}
	if err == nil {
		return
	}
	for _, i := range missing {
		if out[i].Image != nil {
			continue
		}
		if e, ok := h.get(keys[i]); ok {
			out[i] = page.Fragment{Image: e.img, Stale: true}
		} else if out[i].Err == nil {
			out[i] = page.Fragment{Err: err}
		}
	}
}

// run executes the plugin once for the missing requests and stores the
// results.
func (h *Host) run(ctx context.Context, spec Spec, reqs []page.FragmentRequest, keys []string, missing []int, levels int, env Env, out []page.Fragment) error {
	req := plugin.Request{Version: plugin.Version, Action: plugin.ActionRender, Language: env.Language,
		Timezone: env.Timezone, Now: env.Now, Levels: levels}
	for _, i := range missing {
		r := reqs[i]
		req.Images = append(req.Images, plugin.ImageRequest{Image: r.Image, Width: r.Width, Height: r.Height, Args: r.Args})
	}
	start := time.Now()
	raw, err := Exec(ctx, spec, req)
	if err != nil {
		h.fail(spec.Name, err)
		return err
	}
	var resp plugin.Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		err = fmt.Errorf("invalid response: %w", err)
		h.fail(spec.Name, err)
		return err
	}
	if len(resp.Images) != len(missing) {
		err = fmt.Errorf("%d images for %d requests", len(resp.Images), len(missing))
		h.fail(spec.Name, err)
		return err
	}
	maxAge := DefaultMaxAge
	if resp.MaxAge > 0 {
		maxAge = time.Duration(resp.MaxAge) * time.Second
	}
	expires := time.Now().Add(maxAge)
	var firstErr error
	for n, i := range missing {
		res := resp.Images[n]
		img, err := decodePNG(res)
		if err != nil {
			err = fmt.Errorf("%s: %w", reqs[i].Image, err)
			firstErr = cmpErr(firstErr, err)
			out[i] = page.Fragment{Err: err}
			continue
		}
		if b := img.Bounds(); b.Dx() != reqs[i].Width || b.Dy() != reqs[i].Height {
			err := fmt.Errorf("%s: image is %dx%d, requested %dx%d", reqs[i].Image, b.Dx(), b.Dy(), reqs[i].Width, reqs[i].Height)
			firstErr = cmpErr(firstErr, err)
			out[i] = page.Fragment{Err: err}
			continue
		}
		h.put(keys[i], entry{img: img, expires: expires})
		out[i] = page.Fragment{Image: img}
	}
	slog.Debug("plugin rendered", "plugin", spec.Name, "images", len(missing), "took", time.Since(start))
	if firstErr != nil {
		// Images that did fail fall back to their stale copies.
		for _, i := range missing {
			if out[i].Image == nil {
				if e, ok := h.get(keys[i]); ok {
					out[i] = page.Fragment{Image: e.img, Stale: true}
				}
			}
		}
		slog.Warn("plugin image failed", "plugin", spec.Name, "error", firstErr)
		return nil
	}
	h.recover(spec.Name)
	return nil
}

func cmpErr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

func decodePNG(res plugin.ImageResult) (image.Image, error) {
	if res.Error != "" {
		return nil, errors.New(res.Error)
	}
	if len(res.PNG) == 0 {
		return nil, errors.New("no image")
	}
	return png.Decode(bytes.NewReader(res.PNG))
}

// Describe asks a plugin which images it offers.
func Describe(ctx context.Context, spec Spec) (*plugin.Description, error) {
	raw, err := Exec(ctx, spec, plugin.Request{Version: plugin.Version, Action: plugin.ActionDescribe, Now: time.Now()})
	if err != nil {
		return nil, err
	}
	var d plugin.Description
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("invalid description: %w", err)
	}
	return &d, nil
}

// Exec runs the plugin with one request and returns its standard output.
func Exec(ctx context.Context, spec Spec, req plugin.Request) ([]byte, error) {
	if len(spec.Command) == 0 {
		return nil, errors.New("no command configured")
	}
	dir := spec.Dir
	if dir == "" {
		dir = "."
	}
	program, err := provider.ResolveCommand(dir, spec.Command[0])
	if err != nil {
		return nil, err
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(runCtx, program, spec.Command[1:]...)
	cmd.Dir = dir
	cmd.Env = provider.Environ(spec.Env)
	cmd.Stdin = bytes.NewReader(body)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("timed out after %s", timeout)
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > maxStderr {
			msg = msg[:maxStderr]
		}
		if msg == "" {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", err, msg)
	}
	if stdout.Len() > maxOutput {
		return nil, fmt.Errorf("output exceeds %d bytes", maxOutput)
	}
	return stdout.Bytes(), nil
}

func cacheKey(r page.FragmentRequest, levels int, env Env) string {
	args, _ := json.Marshal(r.Args) // map keys are sorted
	return fmt.Sprintf("%s|%s|%s|%dx%d|%d|%s|%s", r.Plugin, r.Image, args, r.Width, r.Height, levels, env.Language, env.Timezone)
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

func (h *Host) get(key string) (entry, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.cache[key]
	return e, ok
}

func (h *Host) put(key string, e entry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cache == nil {
		h.cache = map[string]entry{}
	}
	h.cache[key] = e
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
	h.failing[name] = time.Now().Add(retryAfter)
}

func (h *Host) recover(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, was := h.failing[name]; was {
		slog.Info("plugin recovered", "plugin", name)
		delete(h.failing, name)
	}
}
