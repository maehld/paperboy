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
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/internal/atomicfile"
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
	proto   int // protocol version of the last answer, for events
	pending map[string]chan plugin.Message
	done    chan struct{} // closed when the process has exited
	stderr  *tailBuffer
	started time.Time // for the runtime page (ADR 0104)

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
// answer would block every later request. The answer time and the outcome are
// what the runtime page reports (ADR 0104).
func (h *Host) call(ctx context.Context, spec Spec, req plugin.Request) (plugin.Message, error) {
	p, err := h.process(spec)
	if err != nil {
		h.note(spec.Name, err)
		return plugin.Message{}, err
	}
	start := time.Now()
	m, err := p.request(ctx, req)
	h.registry().Observe("plugin."+spec.Name+".answer", time.Since(start))
	h.note(spec.Name, err)
	if err != nil && (p.exited() || errors.Is(err, errTimeout) || errors.Is(err, errWrite)) {
		h.drop(p)
	}
	return m, err
}

var (
	errTimeout = errors.New("no answer in time; plugin restarted")
	errWrite   = errors.New("write request")
)

// configure sends the plugin its configuration; the plugin is only used when
// it accepts it. It then receives what it stored last time in one state
// request, so configure stays the request it always was and the state and the
// blobs do not travel with every widget request (ADR 0081).
func (p *process) configure(spec Spec, env Env) error {
	// Read the stored state before the configure request: a plugin may answer
	// configure with a state of its own, which would replace the files. The
	// state still reaches the plugin only after configure, in the state
	// request (ADR 0081).
	state, blobs := p.loadState()
	if _, err := p.request(context.Background(), plugin.Request{Version: plugin.Version, Action: plugin.ActionConfigure,
		Language: env.Language, Timezone: env.Timezone, Now: time.Now(), Config: spec.Config}); err != nil {
		return err
	}
	_, err := p.request(context.Background(), plugin.Request{Version: plugin.Version, Action: plugin.ActionLoadState,
		Language: env.Language, Timezone: env.Timezone, Now: time.Now(), State: state, Blobs: blobs})
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
		p.mu.Lock()
		p.proto = m.Version
		p.mu.Unlock()
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

func (p *process) protocol() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.proto
}

// sendEvent writes one line to the plugin without a request id; no answer
// follows. The caller checks the protocol version first (ADR 0126).
func (p *process) sendEvent(ev plugin.Event) error {
	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	if _, err := p.stdin.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("%w: %w", errWrite, p.exitError(err))
	}
	return nil
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
		delete(h.sent, spec.Name)
	}
	base := h.ctx
	if base == nil {
		base = context.Background()
	}
	if base.Err() != nil {
		return nil, base.Err()
	}
	p, err := startProcess(base, spec, h.expire, h.recordExit)
	if err != nil {
		return nil, err
	}
	if err := p.configure(spec, h.env); err != nil {
		go p.stop()
		return nil, fmt.Errorf("configure: %w", err)
	}
	// A new process may be another program, after a crash, a timeout or a
	// replaced file: its declarations and the results the old one cached do
	// not belong together (issue #285). Callers hold h.mu.
	h.nextDeclarations(spec.Name)
	for k, e := range h.cache {
		if e.job.plugin == spec.Name {
			delete(h.cache, k)
		}
	}
	if h.procs == nil {
		h.procs = map[string]*process{}
	}
	h.procs[spec.Name] = p
	p.started = h.now()
	if h.starts == nil {
		h.starts = map[string]int{}
	}
	h.starts[spec.Name]++
	return p, nil
}

func (h *Host) drop(p *process) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.procs[p.spec.Name] == p {
		delete(h.procs, p.spec.Name)
	}
}

func startProcess(base context.Context, spec Spec, changed func(string, []string), onExit func(*process, error)) (*process, error) {
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
	go func() {
		if err := p.read(stdout, changed); onExit != nil {
			onExit(p, err)
		}
	}()
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
		case !validStateObject(data):
			slog.Warn("ignoring plugin state that is not a JSON object", "plugin", p.spec.Name, "file", path)
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
			if err := os.Remove(filepath.Join(dir, stateFileName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				slog.Warn("forgetting plugin state", "plugin", p.spec.Name, "error", err)
			}
		case !validStateObject(data):
			slog.Warn("ignoring plugin state that is not a JSON object", "plugin", p.spec.Name)
		case len(data) > maxStateBytes:
			slog.Warn("ignoring plugin state that is too large", "plugin", p.spec.Name, "bytes", len(data))
		default:
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
		if !p.blobRoom(name) {
			slog.Warn("ignoring a plugin blob beyond the limit", "plugin", p.spec.Name, "name", name, "limit", maxBlobs)
			continue
		}
		if err := writeStateFile(filepath.Join(dir, blobsDirName, name), data); err != nil {
			slog.Warn("storing a plugin blob", "plugin", p.spec.Name, "name", name, "error", err)
		}
	}
}

// validStateObject reports whether data can be stored as plugin state: a JSON
// object, or null to forget it (ADR 0081).
func validStateObject(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if !json.Valid(trimmed) {
		return false
	}
	return len(trimmed) > 0 && (trimmed[0] == '{' || string(trimmed) == "null")
}

// blobRoom reports whether one more blob fits: an existing name may always be
// replaced, a new one needs a free slot (ADR 0081).
func (p *process) blobRoom(name string) bool {
	dir := filepath.Join(p.spec.StateDir, blobsDirName)
	if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
		return true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() && validBlobName(e.Name()) {
			count++
		}
	}
	return count < maxBlobs
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
	return atomicfile.WriteFileIn(path, data, 0o700, 0o600)
}

// read dispatches the plugin's lines: answers to their request, events to
// the cache.
func (p *process) read(stdout io.Reader, changed func(string, []string)) error {
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
	return err
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

// stderrLines is how many lines of a plugin's error output the runtime page
// shows (issue #306).
const stderrLines = 50

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

// lines returns the last n complete lines of the buffer.
func (b *tailBuffer) lines(n int) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	text := strings.TrimRight(string(b.buf), "\n")
	if text == "" {
		return nil
	}
	all := strings.Split(text, "\n")
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return all
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

// inheritedEnvironment names the variables a plugin inherits from Paperboy.
// The list is an allowlist: Paperboy itself carries secrets such as
// PAPERBOY_UI_PASSWORD and PAPERBOY_DOWNLOAD_TOKEN, and a plugin has no business
// reading them (ADR 0134). A plugin adds its own values through spec.Env.
var inheritedEnvironment = []string{
	"PATH", "HOME", "USER", "LOGNAME", "SHELL", "TMPDIR",
	"LANG", "LANGUAGE", "LC_ALL", "LC_CTYPE", "LC_MESSAGES", "LC_NUMERIC", "LC_TIME",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY",
	"http_proxy", "https_proxy", "no_proxy", "all_proxy",
	// Windows needs these to start a program and to load system libraries.
	"SystemRoot", "SystemDrive", "windir", "ComSpec", "PATHEXT",
	"TEMP", "TMP", "USERPROFILE", "HOMEDRIVE", "HOMEPATH", "APPDATA",
	"LOCALAPPDATA", "USERNAME",
}

// environ returns the allowlisted environment plus the configured keys. A
// configured key replaces an inherited one with the same name, and on Windows
// the comparison ignores case, so a plugin never sees two values for one key.
// The result is sorted, so a plugin sees a stable order.
func environ(extra map[string]string) []string {
	values := make(map[string]string, len(inheritedEnvironment)+len(extra))
	names := make(map[string]string, len(inheritedEnvironment)+len(extra))
	for _, key := range inheritedEnvironment {
		if v, ok := os.LookupEnv(key); ok {
			canonical := environmentKey(key)
			values[canonical] = v
			names[canonical] = key
		}
	}
	for key, v := range extra {
		canonical := environmentKey(key)
		values[canonical] = v
		names[canonical] = key
	}
	out := make([]string, 0, len(values))
	for canonical, v := range values {
		out = append(out, names[canonical]+"="+v)
	}
	sort.Strings(out)
	return out
}

// environmentKey folds a variable name for the duplicate check. Windows
// environment names are case-insensitive.
func environmentKey(key string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(key)
	}
	return key
}
