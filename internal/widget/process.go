package widget

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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
	state   json.RawMessage // stored state, sent with every request (ADR 0081)

	versionLogged bool // the protocol mismatch was already logged
}

func (h *Host) stopAll() {
	h.mu.Lock()
	procs := h.procs
	h.procs = nil
	h.mu.Unlock()
	for _, p := range procs {
		p.stop()
	}
}

func sameSpec(a, b Spec) bool { return reflect.DeepEqual(a, b) }

// call sends one request to the plugin, starting it if needed, and waits for
// the answer. A timeout kills the process, because a plugin that does not
// answer would block every later request.
func (h *Host) call(ctx context.Context, spec Spec, req plugin.Request) (plugin.Message, error) {
	p, err := h.process(spec)
	if err != nil {
		return plugin.Message{}, err
	}
	m, err := p.request(ctx, req)
	if err != nil && (p.exited() || errors.Is(err, errTimeout) || errors.Is(err, errWrite)) {
		h.drop(p)
	}
	return m, err
}

var (
	errTimeout = errors.New("no answer in time; plugin restarted")
	errWrite   = errors.New("write request")
)

// configure sends the plugin its configuration and what it stored last time;
// the plugin is only used when it accepts it.
func (p *process) configure(spec Spec, env Env) error {
	state, blobs := p.loadState()
	p.mu.Lock()
	p.state = state
	p.mu.Unlock()
	_, err := p.request(context.Background(), plugin.Request{Version: plugin.Version, Action: plugin.ActionConfigure,
		Language: env.Language, Timezone: env.Timezone, Now: time.Now(), Config: spec.Config,
		State: state, Blobs: blobs})
	return err
}

// request sends one request and waits for its answer.
func (p *process) request(ctx context.Context, req plugin.Request) (plugin.Message, error) {
	spec := p.spec
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
	if len(req.State) == 0 {
		// Every request carries the stored state (ADR 0081).
		req.State = p.state
	}
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
		return plugin.Message{}, fmt.Errorf("%w: %w", errWrite, p.exitError(err))
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case m := <-ch:
		if err := m.CheckVersion(); err != nil {
			p.warnVersion(err)
			return m, err
		}
		if m.Error != "" {
			return m, errors.New(m.Error)
		}
		return m, nil
	case <-p.done:
		return plugin.Message{}, p.exitError(errors.New("plugin exited"))
	case <-timer.C:
		go p.stop()
		return plugin.Message{}, fmt.Errorf("%w (%s)", errTimeout, timeout)
	case <-ctx.Done():
		return plugin.Message{}, ctx.Err()
	}
}

// process returns the running process of a plugin or starts one.
func (h *Host) process(spec Spec) (*process, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if p, ok := h.procs[spec.Name]; ok {
		if sameSpec(p.spec, spec) && !p.exited() {
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
	if err := p.configure(spec, h.env); err != nil {
		go p.stop()
		return nil, fmt.Errorf("configure: %w", err)
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
	// The plugins directory is optional: shipped plugins are found on PATH.
	dir := spec.Dir
	if fi, err := os.Stat(dir); dir == "" || err != nil || !fi.IsDir() {
		dir = ""
	}
	program, err := resolveCommand(dir, spec.Command[0])
	if err != nil {
		return nil, err
	}
	// Paperboy keeps the plugin's state and blobs in its own directory, which
	// exists before configure (ADR 0081).
	if spec.StateDir != "" {
		if err := os.MkdirAll(spec.StateDir, 0o700); err != nil {
			return nil, fmt.Errorf("state directory: %w", err)
		}
	}
	ctx, cancel := context.WithCancel(base)
	cmd := exec.CommandContext(ctx, program, spec.Command[1:]...)
	cmd.Dir = dir
	cmd.Env = environ(spec.Env)
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

// Limits of the state Paperboy keeps for a plugin (ADR 0081): JSON is small,
// blobs are a cache, so both stay far below what a line of the protocol can
// carry.
const (
	stateFileName = "state.json"
	blobsDirName  = "blobs"
	maxStateBytes = 256 << 10
	maxBlobBytes  = 1 << 20
	maxBlobs      = 4
)

// loadState reads the plugin's state and blobs before configure. Unusable
// files are reported and skipped, so a broken cache cannot keep a plugin from
// starting.
func (p *process) loadState() (json.RawMessage, map[string][]byte) {
	dir := p.spec.StateDir
	if dir == "" {
		return nil, nil
	}
	var state json.RawMessage
	path := filepath.Join(dir, stateFileName)
	if data, err := os.ReadFile(path); err == nil {
		switch {
		case len(data) > maxStateBytes:
			slog.Warn("ignoring plugin state that is too large", "plugin", p.spec.Name, "file", path)
		case !json.Valid(data):
			slog.Warn("ignoring plugin state that is not JSON", "plugin", p.spec.Name, "file", path)
		default:
			state = data
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("reading plugin state", "plugin", p.spec.Name, "error", err)
	}
	blobs := map[string][]byte{}
	entries, err := os.ReadDir(filepath.Join(dir, blobsDirName))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("reading plugin blobs", "plugin", p.spec.Name, "error", err)
	}
	for _, e := range entries {
		if e.IsDir() || !validBlobName(e.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, blobsDirName, e.Name()))
		if err != nil {
			slog.Warn("reading a plugin blob", "plugin", p.spec.Name, "name", e.Name(), "error", err)
			continue
		}
		if len(data) > maxBlobBytes || len(blobs) == maxBlobs {
			slog.Warn("ignoring a plugin blob that is too large or one too many", "plugin", p.spec.Name, "name", e.Name())
			continue
		}
		blobs[e.Name()] = data
	}
	return state, blobs
}

// storeState keeps what a plugin answered: state.json and the blobs, written
// for the next start (ADR 0081). Missing state means unchanged, null forgets
// it. The files belong to Paperboy, the plugin cannot write them.
func (p *process) storeState(m *plugin.Message) {
	dir := p.spec.StateDir
	if dir == "" || (m.State == nil && len(m.Blobs) == 0) {
		return
	}
	if m.State != nil {
		data := *m.State
		switch {
		case string(bytes.TrimSpace(data)) == "null":
			p.mu.Lock()
			p.state = nil
			p.mu.Unlock()
			if err := os.Remove(filepath.Join(dir, stateFileName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				slog.Warn("forgetting plugin state", "plugin", p.spec.Name, "error", err)
			}
		case !json.Valid(data):
			slog.Warn("ignoring plugin state that is not JSON", "plugin", p.spec.Name)
		case len(data) > maxStateBytes:
			slog.Warn("ignoring plugin state that is too large", "plugin", p.spec.Name, "bytes", len(data))
		default:
			p.mu.Lock()
			p.state = append(json.RawMessage(nil), data...)
			p.mu.Unlock()
			if err := writeStateFile(filepath.Join(dir, stateFileName), data); err != nil {
				slog.Warn("storing plugin state", "plugin", p.spec.Name, "error", err)
			}
		}
	}
	for name, data := range m.Blobs {
		if !validBlobName(name) {
			slog.Warn("ignoring a plugin blob whose name is not allowed", "plugin", p.spec.Name, "name", name)
			continue
		}
		if len(data) > maxBlobBytes {
			slog.Warn("ignoring a plugin blob that is too large", "plugin", p.spec.Name, "name", name, "bytes", len(data))
			continue
		}
		if err := writeStateFile(filepath.Join(dir, blobsDirName, name), data); err != nil {
			slog.Warn("storing a plugin blob", "plugin", p.spec.Name, "name", name, "error", err)
		}
	}
}

// validBlobName accepts the names Paperboy stores below blobs/: lower case
// letters, digits and . _ -, starting with a letter or digit.
func validBlobName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case (r == '.' || r == '_' || r == '-') && i > 0:
		default:
			return false
		}
	}
	return true
}

// writeStateFile replaces one file of the plugin's state atomically and
// readable only by its owner.
func writeStateFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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
		// Answers and events may both carry state and blobs (ADR 0081), so a
		// plugin can flush them without changing a widget.
		p.storeState(&m)
		if m.ID == "" {
			if m.Event == plugin.EventChanged {
				slog.Debug("plugin widgets changed", "plugin", p.spec.Name, "widgets", m.Changed)
				changed(p.spec.Name, m.Changed)
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

// warnVersion logs a protocol mismatch once per process: the cells fail with
// the error anyway, but the operator needs the reason once in the log
// (ADR 0058).
func (p *process) warnVersion(err error) {
	p.mu.Lock()
	logged := p.versionLogged
	p.versionLogged = true
	p.mu.Unlock()
	if !logged {
		slog.Error("plugin protocol mismatch", "plugin", p.spec.Name, "error", err)
	}
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

// resolveCommand finds the program of a plugin: absolute paths are used as
// they are, relative ones are looked up in the plugins directory, and a bare
// name falls back to PATH.
func resolveCommand(dir, name string) (string, error) {
	if filepath.IsAbs(name) {
		return name, nil
	}
	if dir != "" && strings.ContainsAny(name, `/\`) {
		return filepath.Join(dir, name), nil
	}
	if dir != "" {
		if p, err := exec.LookPath(filepath.Join(dir, name)); err == nil {
			return p, nil
		}
	}
	return exec.LookPath(name)
}

// environ returns the inherited environment plus the configured keys, sorted
// so that a plugin sees a stable order.
func environ(extra map[string]string) []string {
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := os.Environ()
	for _, k := range keys {
		out = append(out, k+"="+extra[k])
	}
	return out
}
