package fragment

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/internal/provider"
	"github.com/maehld/paperboy/pkg/plugin"
)

// process is one running plugin.
type process struct {
	spec   Spec
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	cancel context.CancelFunc

	mu      sync.Mutex
	writeMu sync.Mutex
	next    int
	pending map[string]chan plugin.Message
	done    chan struct{} // closed when the process has exited
	stderr  *tailBuffer
}

// Start sets the context that bounds all plugin processes; when it ends the
// plugins are stopped.
func (h *Host) Start(ctx context.Context) {
	h.mu.Lock()
	h.ctx = ctx
	h.mu.Unlock()
	go func() {
		<-ctx.Done()
		h.mu.Lock()
		procs := h.procs
		h.procs = nil
		h.mu.Unlock()
		for _, p := range procs {
			p.stop()
		}
	}()
}

// Reconcile stops the plugins that are no longer configured or whose spec
// changed; they start again on the next request.
func (h *Host) Reconcile(specs map[string]Spec) {
	h.mu.Lock()
	var stop []*process
	for name, p := range h.procs {
		if spec, ok := specs[name]; !ok || !reflect.DeepEqual(spec, p.spec) {
			stop = append(stop, p)
			delete(h.procs, name)
		}
	}
	h.mu.Unlock()
	for _, p := range stop {
		slog.Info("plugin stopped", "plugin", p.spec.Name, "reason", "configuration changed")
		p.stop()
	}
}

// call sends one request to the plugin, starting it if needed, and waits for
// the answer. A timeout kills the process, because a plugin that does not
// answer would block every later request.
func (h *Host) call(ctx context.Context, spec Spec, req plugin.Request) (plugin.Message, error) {
	p, err := h.process(spec)
	if err != nil {
		return plugin.Message{}, err
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ch := make(chan plugin.Message, 1)
	p.mu.Lock()
	p.next++
	req.ID = strconv.Itoa(p.next)
	if p.pending == nil {
		p.pending = map[string]chan plugin.Message{}
	}
	p.pending[req.ID] = ch
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.pending, req.ID)
		p.mu.Unlock()
	}()

	line, err := json.Marshal(req)
	if err != nil {
		return plugin.Message{}, err
	}
	p.writeMu.Lock()
	_, err = p.stdin.Write(append(line, '\n'))
	p.writeMu.Unlock()
	if err != nil {
		h.drop(p)
		return plugin.Message{}, fmt.Errorf("write request: %w", p.exitError(err))
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case m := <-ch:
		if m.Error != "" {
			return m, errors.New(m.Error)
		}
		return m, nil
	case <-p.done:
		h.drop(p)
		return plugin.Message{}, p.exitError(errors.New("plugin exited"))
	case <-timer.C:
		h.drop(p)
		p.stop()
		return plugin.Message{}, fmt.Errorf("no answer within %s; plugin restarted", timeout)
	case <-ctx.Done():
		return plugin.Message{}, ctx.Err()
	}
}

// process returns the running process of a plugin or starts one.
func (h *Host) process(spec Spec) (*process, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if p, ok := h.procs[spec.Name]; ok {
		if reflect.DeepEqual(p.spec, spec) && !p.exited() {
			return p, nil
		}
		go p.stop()
		delete(h.procs, spec.Name)
	}
	base := h.ctx
	if base == nil {
		base = context.Background()
	}
	if base.Err() != nil {
		return nil, base.Err()
	}
	p, err := startProcess(base, spec, h.expire)
	if err != nil {
		return nil, err
	}
	if h.procs == nil {
		h.procs = map[string]*process{}
	}
	h.procs[spec.Name] = p
	return p, nil
}

func (h *Host) drop(p *process) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.procs[p.spec.Name] == p {
		delete(h.procs, p.spec.Name)
	}
}

func startProcess(base context.Context, spec Spec, changed func(string, []string)) (*process, error) {
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
	ctx, cancel := context.WithCancel(base)
	cmd := exec.CommandContext(ctx, program, spec.Command[1:]...)
	cmd.Dir = dir
	cmd.Env = provider.Environ(spec.Env)
	cmd.WaitDelay = 2 * time.Second
	p := &process{spec: spec, cmd: cmd, cancel: cancel, done: make(chan struct{}), stderr: &tailBuffer{limit: maxStderr}}
	cmd.Stderr = p.stderr
	if p.stdin, err = cmd.StdinPipe(); err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	slog.Info("plugin started", "plugin", spec.Name, "pid", cmd.Process.Pid)
	go p.read(stdout, changed)
	return p, nil
}

// read dispatches the plugin's lines: answers to their request, events to
// the cache.
func (p *process) read(stdout io.Reader, changed func(string, []string)) {
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64<<10), maxOutput)
	for sc.Scan() {
		var m plugin.Message
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			slog.Warn("plugin wrote an invalid line", "plugin", p.spec.Name, "error", err)
			continue
		}
		if m.ID == "" {
			if m.Event == plugin.EventChanged {
				slog.Debug("plugin images changed", "plugin", p.spec.Name, "images", m.Images)
				changed(p.spec.Name, m.Images)
			} else if m.Error != "" {
				slog.Warn("plugin error", "plugin", p.spec.Name, "error", m.Error)
			}
			continue
		}
		p.mu.Lock()
		ch := p.pending[m.ID]
		p.mu.Unlock()
		if ch != nil {
			ch <- m
		}
	}
	if err := sc.Err(); err != nil {
		slog.Warn("plugin output failed", "plugin", p.spec.Name, "error", err)
	}
	err := p.cmd.Wait()
	slog.Info("plugin exited", "plugin", p.spec.Name, "error", p.exitError(err))
	close(p.done)
}

func (p *process) exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// stop closes the plugin's input, which ends a well-behaved plugin, and kills
// it if it is still running after a grace period.
func (p *process) stop() {
	p.stdin.Close()
	select {
	case <-p.done:
	case <-time.After(3 * time.Second):
		p.cancel()
		<-p.done
	}
	p.cancel()
}

func (p *process) exitError(err error) error {
	msg := strings.TrimSpace(p.stderr.String())
	if err == nil {
		if msg == "" {
			return nil
		}
		return errors.New(msg)
	}
	if msg == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, msg)
}

// tailBuffer keeps the last limit bytes of the plugin's error output.
type tailBuffer struct {
	mu    sync.Mutex
	buf   []byte
	limit int
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.limit {
		b.buf = b.buf[len(b.buf)-b.limit:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}
