// Package provider runs external programs that print JSON and writes their
// output into the shared data document (ADR 0036). A provider is a normal
// process: Paperboy starts it on an interval, reads its standard output and
// replaces the data branch it owns. Everything else stays as it is - no token,
// no port, no HTTP.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// maxOutput matches the write limit of the data API.
	maxOutput = 1 << 20
	// maxStderr caps how much error output reaches the log.
	maxStderr = 4 << 10
	// minInterval is the shortest allowed schedule, also for the default.
	minInterval = 10 * time.Second
	// minTimeout is the shortest allowed run time.
	minTimeout = time.Second
)

// Defaults of the provider section.
const (
	DefaultInterval = 15 * time.Minute
	DefaultTimeout  = 30 * time.Second
)

// Spec describes one provider from paperboy.yaml.
type Spec struct {
	Name     string
	Command  []string      // argument vector; the first element is resolved in Dir, then on PATH
	Path     string        // data branch the output replaces
	Interval time.Duration // pause between two runs
	Timeout  time.Duration // hard limit for one run
	Env      map[string]string
	Dir      string // providers directory: lookup root and working directory
}

// Sink receives a provider's output. *data.Document implements it.
type Sink interface {
	Put(path string, value any, at time.Time) error
}

// Runner executes one provider.
type Runner struct {
	Spec Spec
	Sink Sink
	Now  func() time.Time // defaults to time.Now
}

// Run executes the provider once at startup and then every interval until ctx
// is done. A failure only produces a log line: the previous data stays and its
// age grows, so pages show the stale notice instead of an error.
func (r *Runner) Run(ctx context.Context) {
	if r.Now == nil {
		r.Now = time.Now
	}
	interval := r.Spec.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	slog.Info("provider started", "provider", r.Spec.Name, "path", r.Spec.Path, "interval", interval)
	failing := false
	for {
		err := r.RunOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		switch {
		case err != nil && !failing:
			slog.Warn("provider failed", "provider", r.Spec.Name, "path", r.Spec.Path, "error", err)
			failing = true
		case err != nil:
			slog.Debug("provider still failing", "provider", r.Spec.Name, "error", err)
		case failing:
			slog.Info("provider recovered", "provider", r.Spec.Name)
			failing = false
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// RunOnce executes the provider a single time and writes its output. Every
// error leaves the document untouched.
func (r *Runner) RunOnce(ctx context.Context) error {
	spec := r.Spec
	if len(spec.Command) == 0 {
		return errors.New("no command configured")
	}
	dir := spec.Dir
	if dir != "" {
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			return fmt.Errorf("providers directory %s does not exist", dir)
		}
	} else {
		dir = "."
	}
	program, err := resolveCommand(dir, spec.Command[0])
	if err != nil {
		return err
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, program, spec.Command[1:]...)
	cmd.Dir = dir
	cmd.Env = environ(spec.Env)
	var stdout, stderr limitedBuffer
	stdout.limit = maxOutput + 1
	stderr.limit = maxStderr
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	start := r.Now()
	err = cmd.Run()
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("timed out after %s", timeout)
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, msg)
	}
	if stdout.overflow {
		return fmt.Errorf("output exceeds %d bytes", maxOutput)
	}
	value, err := decode(stdout.Bytes())
	if err != nil {
		return err
	}
	at, err := dataTime(value, r.Now())
	if err != nil {
		return err
	}
	if err := r.Sink.Put(spec.Path, value, at); err != nil {
		return err
	}
	slog.Debug("provider wrote data", "provider", spec.Name, "path", spec.Path,
		"bytes", stdout.Len(), "duration", r.Now().Sub(start))
	return nil
}

// dataTime returns the time of the write: a provider may report its own data
// time as a top-level "updated" RFC 3339 string, otherwise the run time counts.
// An invalid value is an error so that a broken provider does not silently look
// fresh.
func dataTime(value any, run time.Time) (time.Time, error) {
	m, ok := value.(map[string]any)
	if !ok {
		return run, nil
	}
	raw, ok := m["updated"]
	if !ok {
		return run, nil
	}
	s, ok := raw.(string)
	if !ok {
		return run, errors.New(`invalid "updated": expected an RFC 3339 string`)
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return run, fmt.Errorf(`invalid "updated": %w`, err)
	}
	return t, nil
}

// decode parses exactly one JSON value.
func decode(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("invalid JSON: trailing data after the value")
	}
	return value, nil
}

// resolveCommand finds the program of a provider: absolute paths are used as
// they are, relative ones are looked up in the providers directory, and a bare
// name falls back to PATH so that shipped helpers such as "curl" still work.
func resolveCommand(dir, name string) (string, error) {
	if filepath.IsAbs(name) {
		return name, nil
	}
	if strings.ContainsAny(name, `/\`) {
		return filepath.Join(dir, name), nil
	}
	if p, err := exec.LookPath(filepath.Join(dir, name)); err == nil {
		return p, nil
	}
	return exec.LookPath(name)
}

// environ returns the inherited environment plus the configured keys, sorted so
// that a provider sees a stable order.
func environ(extra map[string]string) []string {
	if len(extra) == 0 {
		return os.Environ()
	}
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

// limitedBuffer collects at most limit bytes and remembers that more arrived,
// so a chatty provider yields a clear error instead of unbounded memory.
type limitedBuffer struct {
	buf      bytes.Buffer
	limit    int
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit > 0 && b.buf.Len()+len(p) > b.limit {
		b.overflow = true
		if room := b.limit - b.buf.Len(); room > 0 {
			b.buf.Write(p[:room])
		}
		return len(p), nil // keep the child running; the caller reports the overflow
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) Bytes() []byte { return b.buf.Bytes() }

func (b *limitedBuffer) Len() int { return b.buf.Len() }

func (b *limitedBuffer) String() string { return b.buf.String() }

// Validate normalizes and checks one provider as it comes from the
// configuration; it is called by the configuration loader.
func (s *Spec) Validate() error {
	if s.Interval == 0 {
		s.Interval = DefaultInterval
	}
	if s.Timeout == 0 {
		s.Timeout = DefaultTimeout
	}
	if s.Interval < minInterval {
		return fmt.Errorf("interval must be at least %s", minInterval)
	}
	if s.Timeout < minTimeout {
		return fmt.Errorf("timeout must be at least %s", minTimeout)
	}
	if s.Timeout > s.Interval {
		return fmt.Errorf("timeout must not exceed interval")
	}
	return nil
}
