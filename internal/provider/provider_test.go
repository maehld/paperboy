package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type write struct {
	path  string
	value any
	at    time.Time
}

// fakeSink records the writes of a provider.
type fakeSink struct {
	mu     sync.Mutex
	writes []write
	err    error
}

func (s *fakeSink) Put(path string, value any, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.writes = append(s.writes, write{path: path, value: value, at: at})
	return nil
}

func (s *fakeSink) last() (write, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.writes) == 0 {
		return write{}, false
	}
	return s.writes[len(s.writes)-1], true
}

// TestHelperProcess is not a real test: RunOnce starts the test binary itself
// with -test.run=TestHelperProcess, and this function behaves like an external
// provider, driven by environment variables.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("PROVIDER_HELPER") != "1" {
		return
	}
	switch os.Getenv("PROVIDER_HELPER_MODE") {
	case "sleep":
		time.Sleep(10 * time.Second)
	case "fail":
		fmt.Fprintln(os.Stderr, "boom")
		os.Exit(3)
	}
	os.Stdout.WriteString(os.Getenv("PROVIDER_HELPER_STDOUT"))
	os.Exit(0)
}

// helperSpec returns a provider specification that runs TestHelperProcess with
// the given payload.
func helperSpec(t *testing.T, stdout string) Spec {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Spec{
		Name:     "weather",
		Command:  []string{exe, "-test.run=TestHelperProcess"},
		Path:     "weather",
		Interval: time.Minute,
		Timeout:  10 * time.Second,
		Env:      map[string]string{"PROVIDER_HELPER": "1", "PROVIDER_HELPER_STDOUT": stdout},
		Dir:      t.TempDir(),
	}
}

var testNow = time.Date(2026, 10, 22, 9, 30, 0, 0, time.UTC)

func testRunner(t *testing.T, spec Spec, sink *fakeSink) *Runner {
	t.Helper()
	return &Runner{Spec: spec, Sink: sink, Now: func() time.Time { return testNow }}
}

func TestRunOnceWritesValue(t *testing.T) {
	sink := &fakeSink{}
	r := testRunner(t, helperSpec(t, `{"temp": 12.5, "text": "Bewölkt"}`), sink)
	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	w, ok := sink.last()
	if !ok {
		t.Fatal("nothing written")
	}
	if w.path != "weather" {
		t.Fatalf("path = %q", w.path)
	}
	if !w.at.Equal(testNow) {
		t.Fatalf("write time = %s, want the run time %s", w.at, testNow)
	}
	m, ok := w.value.(map[string]any)
	if !ok || m["temp"] != 12.5 || m["text"] != "Bewölkt" {
		t.Fatalf("value = %#v", w.value)
	}
}

func TestRunOnceUsesReportedDataTime(t *testing.T) {
	sink := &fakeSink{}
	payload := `{"updated": "2026-10-22T08:00:00Z", "temp": 1}`
	if err := testRunner(t, helperSpec(t, payload), sink).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	w, ok := sink.last()
	if !ok {
		t.Fatal("nothing written")
	}
	want := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)
	if !w.at.Equal(want) {
		t.Fatalf("write time = %s, want %s", w.at, want)
	}
	if m := w.value.(map[string]any); m["updated"] != "2026-10-22T08:00:00Z" {
		t.Fatalf("updated must stay in the data: %#v", w.value)
	}
}

func TestRunOnceRejectsInvalidDataTime(t *testing.T) {
	for _, payload := range []string{
		`{"updated": "yesterday", "temp": 1}`,
		`{"updated": 1234, "temp": 1}`,
	} {
		sink := &fakeSink{}
		err := testRunner(t, helperSpec(t, payload), sink).RunOnce(context.Background())
		if err == nil || !strings.Contains(err.Error(), "updated") {
			t.Fatalf("%s: error = %v", payload, err)
		}
		if _, ok := sink.last(); ok {
			t.Fatalf("%s: data must not be written", payload)
		}
	}
}

func TestRunOnceKeepsDataOnFailure(t *testing.T) {
	spec := helperSpec(t, "")
	spec.Env["PROVIDER_HELPER_MODE"] = "fail"
	sink := &fakeSink{}
	err := testRunner(t, spec, sink).RunOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v, want the stderr of the provider", err)
	}
	if _, ok := sink.last(); ok {
		t.Fatal("a failing provider must not write")
	}
}

func TestRunOnceKeepsDataOnInvalidJSON(t *testing.T) {
	for _, payload := range []string{"", "not json", `{"a": 1} {"b": 2}`} {
		sink := &fakeSink{}
		err := testRunner(t, helperSpec(t, payload), sink).RunOnce(context.Background())
		if err == nil || !strings.Contains(err.Error(), "invalid JSON") {
			t.Fatalf("%q: error = %v", payload, err)
		}
		if _, ok := sink.last(); ok {
			t.Fatalf("%q: data must not be written", payload)
		}
	}
}

func TestRunOnceTimesOut(t *testing.T) {
	spec := helperSpec(t, "")
	spec.Env["PROVIDER_HELPER_MODE"] = "sleep"
	spec.Timeout = 200 * time.Millisecond
	sink := &fakeSink{}
	start := time.Now()
	err := testRunner(t, spec, sink).RunOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timeout took %s", elapsed)
	}
	if _, ok := sink.last(); ok {
		t.Fatal("a timed out provider must not write")
	}
}

func TestRunOnceMissingProvidersDirectory(t *testing.T) {
	spec := helperSpec(t, `{}`)
	spec.Dir = filepath.Join(t.TempDir(), "missing")
	if err := testRunner(t, spec, &fakeSink{}).RunOnce(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunOnceSinkError(t *testing.T) {
	sink := &fakeSink{err: errors.New("conflict")}
	err := testRunner(t, helperSpec(t, `{"temp": 1}`), sink).RunOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sink := &fakeSink{}
	r := testRunner(t, helperSpec(t, `{"temp": 1}`), sink)
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, ok := sink.last(); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("provider never ran")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run ignored the cancelled context")
	}
}

func TestResolveCommand(t *testing.T) {
	dir := t.TempDir()
	// A provider program in the providers directory wins over PATH; on Windows
	// the executable extension is looked up.
	name := "mytool"
	if os.PathSeparator == '\\' {
		name += ".exe"
	}
	program := filepath.Join(dir, name)
	if err := os.WriteFile(program, []byte(""), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveCommand(dir, "mytool")
	if err != nil {
		t.Fatal(err)
	}
	if got != program {
		t.Fatalf("resolved %q, want %q", got, program)
	}
	abs := filepath.Join(dir, "other")
	if got, err := ResolveCommand(dir, abs); err != nil || got != abs {
		t.Fatalf("absolute path: %q %v", got, err)
	}
	if got, err := ResolveCommand(dir, "sub/tool"); err != nil || got != filepath.Join(dir, "sub", "tool") {
		t.Fatalf("relative path: %q %v", got, err)
	}
}

func TestDecode(t *testing.T) {
	if v, err := decode([]byte(`{"a": 1}`)); err != nil || v.(map[string]any)["a"] != 1.0 {
		t.Fatalf("valid JSON: %v %v", v, err)
	}
	for _, raw := range []string{"", "  ", "{} {}", "1 2", "[1,2"} {
		if _, err := decode([]byte(raw)); err == nil {
			t.Fatalf("%q decoded without error", raw)
		}
	}
}

func TestLimitedBuffer(t *testing.T) {
	var b limitedBuffer
	b.limit = 4
	b.Write([]byte("abc"))
	b.Write([]byte("def"))
	if !b.overflow || b.String() != "abcd" {
		t.Fatalf("overflow = %v, buffer = %q", b.overflow, b.String())
	}
}

func TestSpecValidate(t *testing.T) {
	spec := Spec{Command: []string{"weather"}, Path: "weather"}
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
	if spec.Interval != DefaultInterval || spec.Timeout != DefaultTimeout {
		t.Fatalf("defaults = %s / %s", spec.Interval, spec.Timeout)
	}
	for _, bad := range []Spec{
		{Interval: time.Second},
		{Interval: time.Minute, Timeout: 2 * time.Minute},
		{Interval: time.Minute, Timeout: time.Millisecond},
	} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("%+v accepted", bad)
		}
	}
}
