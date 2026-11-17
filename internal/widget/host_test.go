package widget

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/internal/page"
	"github.com/maehld/paperboy/pkg/plugin"
)

// TestMain doubles as a fake plugin: with FAKE_PLUGIN set, the test binary
// speaks the line protocol by hand, so tests can break it.
func TestMain(m *testing.M) {
	if mode := os.Getenv("FAKE_PLUGIN"); mode != "" {
		fakePlugin(mode)
		return
	}
	os.Exit(m.Run())
}

func fakePlugin(mode string) {
	appendFile(os.Getenv("FAKE_STARTS"))
	if mode == "fail" {
		os.Stderr.WriteString("broken")
		os.Exit(1)
	}
	// The protocol version a plugin speaks; the modes below break it on
	// purpose (ADR 0058).
	version := plugin.Version
	switch mode {
	case "noversion":
		version = 0
	case "oldversion":
		version = plugin.MinVersion - 1
	case "newer":
		version = plugin.Version + 1
	}
	enc := json.NewEncoder(os.Stdout)
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		var head struct {
			ID    string `json:"id"`
			Event string `json:"event"`
		}
		json.Unmarshal(sc.Bytes(), &head)
		if head.ID == "" && head.Event != "" {
			continue // inbound events need no answer
		}
		var req plugin.Request
		json.Unmarshal(sc.Bytes(), &req)
		if req.Action == plugin.ActionConfigure {
			if mode == "badconfig" || req.Config["ok"] != true {
				enc.Encode(plugin.Message{ID: req.ID, Version: version, Error: "config rejected"})
				continue
			}
			if path := os.Getenv("FAKE_ENV"); path != "" {
				os.WriteFile(path, []byte(os.Getenv("PAPERBOY_STATE_DIR")), 0o644)
			}
			if mode == "stateonconfigure" {
				raw := json.RawMessage(`{"n":0}`)
				enc.Encode(plugin.Message{ID: req.ID, Version: version, State: &raw})
				continue
			}
			enc.Encode(plugin.Message{ID: req.ID, Version: version})
			continue
		}
		if req.Action == plugin.ActionLoadState {
			appendFile(os.Getenv("FAKE_STATE_CALLS"))
			// Record what Paperboy stored and sent (ADR 0081).
			if path := os.Getenv("FAKE_STATE"); path != "" {
				os.WriteFile(path, req.State, 0o644)
			}
			if path := os.Getenv("FAKE_BLOBS"); path != "" {
				names := make([]string, 0, len(req.Blobs))
				for name, data := range req.Blobs {
					names = append(names, name+"="+string(data))
				}
				sort.Strings(names)
				os.WriteFile(path, []byte(strings.Join(names, ",")), 0o644)
			}
			enc.Encode(plugin.Message{ID: req.ID, Version: version})
			continue
		}
		if req.Action == plugin.ActionDescribe {
			d := plugin.Description{Title: "fake", Values: []plugin.ValueSpec{
				{Name: "a", Type: plugin.ValueNumber, Precision: 1},
				{Name: "b", Type: plugin.ValueText},
				{Name: "bad", Type: plugin.ValueText},
				{Name: "echo", Type: plugin.ValueText},
				{Name: "state", Type: plugin.ValueEnum, Enum: []string{"normal", "low", "high"}},
				{Name: "badenum", Type: plugin.ValueEnum, Enum: []string{"normal", "low"}},
				{Name: "emptyenum", Type: plugin.ValueEnum, Enum: []string{"normal", "low"}},
			}}
			// FAKE_DECL names a file with "new": the fake declares "a" with
			// another unit and precision, as a replaced program would (issue
			// #285).
			if path := os.Getenv("FAKE_DECL"); path != "" {
				if b, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(b)) == "new" {
					d.Values[0] = plugin.ValueSpec{Name: "a", Type: plugin.ValueNumber, Unit: "W"}
				}
			}
			enc.Encode(plugin.Message{ID: req.ID, Version: version, Description: &d})
			continue
		}
		appendFile(os.Getenv("FAKE_COUNT"))
		if mode == "values" && req.Action == plugin.ActionValues {
			msg := plugin.Message{ID: req.ID, Version: version, MaxAge: maxAge(), Values: map[string]plugin.Value{}}
			for _, name := range req.Values {
				switch name {
				case "a":
					msg.Values[name] = plugin.NumberValue(12.5)
				case "b":
					msg.Values[name] = plugin.TextValue("text")
				case "echo":
					msg.Values[name] = plugin.TextValue(fmt.Sprint(req.ValueArgs))
				case "state":
					msg.Values[name] = plugin.EnumValue("low")
				case "badenum":
					msg.Values[name] = plugin.EnumValue("nope")
				case "emptyenum":
					msg.Values[name] = plugin.Value{}
				default:
					msg.Values[name] = plugin.Value{}
				}
			}
			enc.Encode(msg)
			continue
		}
		if mode == "hang" {
			continue
		}
		msg := plugin.Message{ID: req.ID, Version: version, MaxAge: 60}
		for _, w := range req.Widgets {
			width := w.Width
			if mode == "wrongsize" {
				width++
			}
			var buf bytes.Buffer
			png.Encode(&buf, image.NewGray(image.Rect(0, 0, width, w.Height)))
			msg.Widgets = append(msg.Widgets, plugin.Result{PNG: buf.Bytes()})
		}
		switch mode {
		case "state", "stateonconfigure":
			raw := json.RawMessage(`{"n":1}`)
			msg.State = &raw
			msg.Blobs = map[string][]byte{"render.png": []byte("png")}
		case "badstate":
			raw := json.RawMessage(`[1,2]`)
			msg.State = &raw
			msg.Blobs = map[string][]byte{"../evil": []byte("x")}
		case "manyblobs":
			msg.Blobs = map[string][]byte{"a.png": []byte("a"), "b.png": []byte("b"), "c.png": []byte("c"), "d.png": []byte("d"), "e.png": []byte("e")}
		}
		enc.Encode(msg)
		if mode == "changed" {
			enc.Encode(plugin.Message{Version: version, Event: plugin.EventChanged, Changed: []string{"a"}})
		}
	}
}

// maxAge is the max_age the fake sends; FAKE_MAXAGE keeps a value valid longer
// than describeTTL for the declaration tests.
func maxAge() int {
	if v, err := strconv.Atoi(os.Getenv("FAKE_MAXAGE")); err == nil && v > 0 {
		return v
	}
	return 60
}

func appendFile(path string) {
	if path == "" {
		return
	}
	b, _ := os.ReadFile(path)
	os.WriteFile(path, append(b, 'x'), 0o644)
}

func runs(path string) int {
	b, _ := os.ReadFile(path)
	return len(b)
}

type fake struct {
	t      *testing.T
	count  string
	starts string
}

func newFake(t *testing.T) *fake {
	dir := t.TempDir()
	return &fake{t: t, count: filepath.Join(dir, "count"), starts: filepath.Join(dir, "starts")}
}

func (f *fake) spec(mode string) map[string]Spec {
	exe, err := os.Executable()
	if err != nil {
		f.t.Fatal(err)
	}
	return map[string]Spec{"fake": {Name: "fake", Command: []string{exe}, Timeout: 10 * time.Second, Dir: f.t.TempDir(),
		Env:    map[string]string{"FAKE_PLUGIN": mode, "FAKE_COUNT": f.count, "FAKE_STARTS": f.starts},
		Config: map[string]any{"ok": true}}}
}

func newHost(t *testing.T, specs map[string]Spec) *Host {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h := &Host{}
	h.Start(ctx)
	h.Configure(specs, Env{Language: "de", Timezone: "UTC"})
	return h
}

func batch(widgets ...page.WidgetRequest) page.Batch {
	return page.Batch{Levels: 4, Widgets: widgets}
}

func w(plugin, widget string, width, height int) page.WidgetRequest {
	return page.WidgetRequest{Plugin: plugin, Widget: widget, Width: width, Height: height}
}

func TestHostCachesAndBatches(t *testing.T) {
	f := newFake(t)
	h := newHost(t, f.spec("ok"))
	b := batch(w("fake", "a", 10, 5), w("fake", "b", 7, 3))
	for range 2 {
		out := h.Fetch(context.Background(), b)
		for i, r := range out {
			if r.Image == nil || r.Image.Bounds().Dx() != b.Widgets[i].Width {
				t.Fatalf("widget %d: %+v", i, r)
			}
		}
	}
	if n := runs(f.count); n != 1 {
		t.Errorf("plugin got %d requests, want 1 (one batch, then cached)", n)
	}
	b.Levels = 2 // another palette is another result
	h.Fetch(context.Background(), b)
	if n, s := runs(f.count), runs(f.starts); n != 2 || s != 1 {
		t.Errorf("requests %d (want 2), starts %d (want 1)", n, s)
	}
}

func TestHostValues(t *testing.T) {
	f := newFake(t)
	h := newHost(t, f.spec("values"))
	b := page.ValueBatch{Page: plugin.PageInfo{Title: "p"}, Values: []page.ValueRequest{
		{Plugin: "fake", Name: "a"}, {Plugin: "fake", Name: "b"}}}
	for range 2 {
		out := h.FetchValues(context.Background(), b)
		if len(out) != 2 || out[0].Err != nil || out[1].Err != nil {
			t.Fatalf("values: %+v", out)
		}
		if out[0].Value == nil || out[0].Value.Number == nil || *out[0].Value.Number != 12.5 ||
			out[0].Spec.Type != plugin.ValueNumber || out[0].Spec.Precision != 1 {
			t.Fatalf("number: %+v %+v", out[0].Value, out[0].Spec)
		}
		if out[1].Value == nil || out[1].Value.Text == nil || *out[1].Value.Text != "text" {
			t.Fatalf("text: %+v", out[1].Value)
		}
	}
	if n := runs(f.count); n != 1 {
		t.Errorf("plugin got %d values requests, want 1 (one batch, then cached)", n)
	}
	out := h.FetchValues(context.Background(), page.ValueBatch{Values: []page.ValueRequest{{Plugin: "fake", Name: "nope"}}})
	if len(out) != 1 || out[0].Err == nil || !strings.Contains(out[0].Err.Error(), "does not declare") {
		t.Fatalf("undeclared value: %+v", out)
	}
	out = h.FetchValues(context.Background(), page.ValueBatch{Values: []page.ValueRequest{{Plugin: "fake", Name: "bad"}}})
	if len(out) != 1 || out[0].Err == nil || !strings.Contains(out[0].Err.Error(), "no number") {
		t.Fatalf("empty value: %+v", out)
	}
}

// declSpecs configures the fake with a declaration file: while it holds "new",
// the fake declares the value "a" with another unit and precision, as a
// replaced program would (issue #285).
func declSpecs(t *testing.T, mode string) (map[string]Spec, string) {
	t.Helper()
	f := newFake(t)
	specs := f.spec(mode)
	path := filepath.Join(t.TempDir(), "decl")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := specs["fake"]
	spec.Env["FAKE_DECL"] = path
	specs["fake"] = spec
	return specs, path
}

func fetchValue(t *testing.T, h *Host, name string) page.Result {
	t.Helper()
	out := h.FetchValues(context.Background(), page.ValueBatch{Values: []page.ValueRequest{{Plugin: "fake", Name: name}}})
	if len(out) != 1 || out[0].Err != nil {
		t.Fatalf("value %s: %+v", name, out)
	}
	return out[0]
}

// TestHostDropsDeclarationsOnRestart checks that a plugin started again after
// a crash uses the declaration of the new program for a value it cached before
// (issue #285).
func TestHostDropsDeclarationsOnRestart(t *testing.T) {
	specs, decl := declSpecs(t, "values")
	h := newHost(t, specs)
	if r := fetchValue(t, h, "a"); r.Spec.Unit != "" || r.Spec.Precision != 1 {
		t.Fatalf("old declaration: %+v", r.Spec)
	}

	// The program file is replaced and the process crashes; the same command
	// starts the new program on the next request.
	if err := os.WriteFile(decl, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	for name, p := range h.procs {
		delete(h.procs, name)
		go p.stop()
	}
	h.mu.Unlock()

	// Another value starts the new process; its declarations and the results
	// of the old program go away, so the cached "a" is made again.
	fetchValue(t, h, "b")
	if r := fetchValue(t, h, "a"); r.Spec.Unit != "W" || r.Spec.Precision != 0 {
		t.Errorf("cached value kept the declaration of the old program: %+v", r.Spec)
	}
}

// TestHostDropsValuesWhenDeclarationChanges checks that a cached value is made
// again when the plugin answers describe with another declaration, before its
// max_age runs out (issue #285).
func TestHostDropsValuesWhenDeclarationChanges(t *testing.T) {
	specs, decl := declSpecs(t, "values")
	spec := specs["fake"]
	spec.Env["FAKE_MAXAGE"] = "3600"
	specs["fake"] = spec
	h := newHost(t, specs)
	base := time.Now()
	h.Now = func() time.Time { return base }
	if r := fetchValue(t, h, "a"); r.Spec.Unit != "" || r.Spec.Precision != 1 {
		t.Fatalf("old declaration: %+v", r.Spec)
	}

	// The value stays cached, but the describe answer is read again after its
	// TTL and differs.
	if err := os.WriteFile(decl, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.Now = func() time.Time { return base.Add(describeTTL + time.Second) }
	if r := fetchValue(t, h, "a"); r.Spec.Unit != "W" || r.Spec.Precision != 0 {
		t.Errorf("cached value kept an outdated declaration: %+v", r.Spec)
	}
}

// TestHostEnumValues checks the answer of an enum value (ADR 0127): a declared
// key passes, an empty answer stays empty for a missing key, and a key outside
// the declaration is an error for that cell.
func TestHostEnumValues(t *testing.T) {
	f := newFake(t)
	h := newHost(t, f.spec("values"))
	out := h.FetchValues(context.Background(), page.ValueBatch{Values: []page.ValueRequest{
		{Plugin: "fake", Name: "state"},
		{Plugin: "fake", Name: "badenum"},
		{Plugin: "fake", Name: "emptyenum"},
	}})
	if len(out) != 3 {
		t.Fatalf("results: %+v", out)
	}
	if out[0].Err != nil || out[0].Value == nil || out[0].Value.Type() != plugin.ValueEnum || *out[0].Value.Enum != "low" {
		t.Fatalf("declared key: %+v %+v", out[0].Value, out[0].Err)
	}
	if out[1].Err == nil || !strings.Contains(out[1].Err.Error(), "not one of") {
		t.Fatalf("an undeclared key must be an error: %+v", out[1])
	}
	if out[2].Err != nil || out[2].Value == nil || out[2].Value.Type() != "" {
		t.Fatalf("an empty enum answer must stay empty: %+v %+v", out[2].Value, out[2].Err)
	}
}

func TestHostValuesGroupByArguments(t *testing.T) {
	f := newFake(t)
	h := newHost(t, f.spec("values"))
	args := func(n float64) map[string]any { return map[string]any{"n": n} }
	b := page.ValueBatch{Values: []page.ValueRequest{
		{Plugin: "fake", Name: "echo", Args: args(1)},
		{Plugin: "fake", Name: "echo", Args: args(2)},
	}}
	out := h.FetchValues(context.Background(), b)
	if len(out) != 2 || out[0].Err != nil || out[1].Err != nil {
		t.Fatalf("values: %+v", out)
	}
	if got := *out[0].Value.Text; got != "map[n:1]" {
		t.Errorf("first value = %q, want its own arguments", got)
	}
	if got := *out[1].Value.Text; got != "map[n:2]" {
		t.Errorf("second value = %q, want its own arguments", got)
	}
	if n := runs(f.count); n != 2 {
		t.Errorf("plugin got %d values requests, want 2 (one per argument set)", n)
	}
	// The same arguments come from the cache.
	h.FetchValues(context.Background(), b)
	if n := runs(f.count); n != 2 {
		t.Errorf("plugin got %d values requests after the cache hit, want 2", n)
	}
}

func TestHostValuesNotFirst(t *testing.T) {
	f := newFake(t)
	h := newHost(t, f.spec("values"))
	// The fake's values surround one of a plugin that is not configured, so
	// its batch indices are not its positions in jobs (issue #268).
	out := h.FetchValues(context.Background(), page.ValueBatch{Values: []page.ValueRequest{
		{Plugin: "fake", Name: "a"},
		{Plugin: "nope", Name: "x"},
		{Plugin: "fake", Name: "b"},
	}})
	if len(out) != 3 {
		t.Fatalf("results: %+v", out)
	}
	if out[0].Err != nil || out[0].Value == nil || out[0].Value.Number == nil || *out[0].Value.Number != 12.5 {
		t.Errorf("first value: %+v", out[0])
	}
	if out[1].Err == nil || !strings.Contains(out[1].Err.Error(), "not configured") {
		t.Errorf("middle value: %+v", out[1])
	}
	if out[2].Err != nil || out[2].Value == nil || out[2].Value.Text == nil || *out[2].Value.Text != "text" {
		t.Errorf("last value: %+v", out[2])
	}
	// The host is still usable, so no panic left the lock behind.
	again := h.FetchValues(context.Background(), page.ValueBatch{Values: []page.ValueRequest{
		{Plugin: "fake", Name: "b"}}})
	if len(again) != 1 || again[0].Err != nil {
		t.Fatalf("the host did not answer after the values: %+v", again)
	}
}

type builtinStub struct{}

func (builtinStub) Describe() plugin.Description { return plugin.Description{Title: "stub"} }

func (builtinStub) Configure(plugin.Values, *plugin.Request) error { return nil }

func (builtinStub) Render(*plugin.Request, plugin.WidgetRequest, *image.Paletted) error { return nil }

func TestHostBuiltinAfterPlugin(t *testing.T) {
	f := newFake(t)
	h := newHost(t, f.spec("ok"))
	h.Builtin = map[string]plugin.Handler{"paperboy": builtinStub{}}
	out := h.Fetch(context.Background(), page.Batch{Levels: 2, Widgets: []page.WidgetRequest{
		{Plugin: "fake", Widget: "a", Width: 10, Height: 1},
		{Plugin: "paperboy", Widget: "header", Width: 10, Height: 1},
	}})
	if len(out) != 2 || out[0].Err != nil || out[0].Image == nil {
		t.Fatalf("fake widget: %+v", out[0])
	}
	if out[1].Err != nil || out[1].Image == nil {
		t.Fatalf("builtin widget: %+v", out[1])
	}
}

// TestHostRecordsExitAndStderr checks that the runtime page can say how a
// plugin ended and show its last error output (issue #306).
func TestHostRecordsExitAndStderr(t *testing.T) {
	f := newFake(t)
	h := newHost(t, f.spec("fail"))
	if out := h.Fetch(context.Background(), batch(w("fake", "a", 1, 1))); out[0].Err == nil {
		t.Fatalf("a broken plugin must fail: %+v", out[0])
	}
	var row PluginStat
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rows, _ := h.Stats(time.Now())
		if len(rows) == 1 && rows[0].LastExit != nil {
			row = rows[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if row.LastExit == nil {
		t.Fatal("no exit recorded")
	}
	if row.LastExit.Code != 1 {
		t.Errorf("exit code = %d, want 1", row.LastExit.Code)
	}
	if row.Running {
		t.Error("the plugin is still marked running")
	}
	if got := strings.Join(row.Stderr, "\n"); !strings.Contains(got, "broken") {
		t.Errorf("stderr = %q, want the plugin's error output", got)
	}
}

func TestHostFailureKeepsStaleResult(t *testing.T) {
	f := newFake(t)
	h := newHost(t, f.spec("ok"))
	b := batch(w("fake", "a", 10, 5))
	h.Fetch(context.Background(), b)
	for _, e := range h.cache {
		e.expires = time.Time{}
	}
	h.Configure(f.spec("fail"), Env{Language: "de", Timezone: "UTC"})
	// A changed configuration drops the old results.
	if out := h.Fetch(context.Background(), b); out[0].Err == nil {
		t.Fatalf("want an error after the restart failed, got %+v", out[0])
	}
	h.Configure(f.spec("ok"), Env{Language: "de", Timezone: "UTC"})
	h.mu.Lock()
	delete(h.failing, "fake")
	h.mu.Unlock()
	h.Fetch(context.Background(), b)
	for _, e := range h.cache {
		e.expires = time.Time{}
	}
	h.stopAll()
	h.mu.Lock()
	spec := h.specs["fake"]
	spec.Command = []string{"/nonexistent/plugin"}
	h.specs["fake"] = spec
	h.mu.Unlock()
	out := h.Fetch(context.Background(), b)
	if out[0].Image == nil || !out[0].Stale {
		t.Fatalf("want the stale image, got %+v", out[0])
	}
}

func TestHostRejectsWrongSizeAndUnknownPlugins(t *testing.T) {
	f := newFake(t)
	h := newHost(t, f.spec("wrongsize"))
	out := h.Fetch(context.Background(), batch(w("fake", "a", 10, 5), w("nope", "a", 1, 1)))
	if out[0].Err == nil || !strings.Contains(out[0].Err.Error(), "requested 10x5") || out[1].Err == nil {
		t.Fatalf("want errors, got %+v", out)
	}
}

func TestHostRejectsOtherProtocolVersions(t *testing.T) {
	for mode, want := range map[string]string{
		"noversion":  "reports no protocol version",
		"oldversion": "needs at least 5",
		"ok":         "", // control: the same plugin with the right version works
	} {
		f := newFake(t)
		h := newHost(t, f.spec(mode))
		out := h.Fetch(context.Background(), batch(w("fake", "a", 1, 1)))
		if want == "" {
			if out[0].Err != nil {
				t.Errorf("%s: %v", mode, out[0].Err)
			}
			continue
		}
		if out[0].Err == nil || !strings.Contains(out[0].Err.Error(), want) {
			t.Errorf("%s: want %q, got %+v", mode, want, out[0])
		}
		h.mu.Lock()
		n := len(h.procs)
		h.mu.Unlock()
		if n != 0 {
			t.Errorf("%s: incompatible plugin still registered", mode)
		}
	}
}

func TestHostConfigureRejected(t *testing.T) {
	f := newFake(t)
	h := newHost(t, f.spec("badconfig"))
	out := h.Fetch(context.Background(), batch(w("fake", "a", 1, 1)))
	if out[0].Err == nil || !strings.Contains(out[0].Err.Error(), "config rejected") {
		t.Fatalf("want the configure error, got %+v", out[0])
	}
}

func TestHostKeepsProcessAndRestartsOnChange(t *testing.T) {
	f := newFake(t)
	specs := f.spec("ok")
	h := newHost(t, specs)
	for i := range 3 {
		if out := h.Fetch(context.Background(), batch(w("fake", "a", 10+i, 5))); out[0].Image == nil {
			t.Fatalf("render %d: %+v", i, out[0])
		}
	}
	if n := runs(f.starts); n != 1 {
		t.Errorf("plugin started %d times, want 1", n)
	}
	changed := specs["fake"]
	changed.Config = map[string]any{"ok": true, "more": 1}
	h.Configure(map[string]Spec{"fake": changed}, Env{Language: "de", Timezone: "UTC"})
	h.Fetch(context.Background(), batch(w("fake", "a", 10, 5)))
	if n := runs(f.starts); n != 2 {
		t.Errorf("plugin started %d times after a config change, want 2", n)
	}
}

func TestHostKeepsPluginState(t *testing.T) {
	f := newFake(t)
	stateDir := filepath.Join(t.TempDir(), "state", "fake")
	seen := filepath.Join(t.TempDir(), "state")
	blobs := filepath.Join(t.TempDir(), "blobs")
	env := filepath.Join(t.TempDir(), "env")
	specs := f.spec("state")
	s := specs["fake"]
	s.StateDir = stateDir
	s.Env["FAKE_STATE"], s.Env["FAKE_BLOBS"], s.Env["FAKE_ENV"] = seen, blobs, env
	specs["fake"] = s
	h := newHost(t, specs)
	if out := h.Fetch(context.Background(), batch(w("fake", "a", 10, 5))); out[0].Err != nil {
		t.Fatal(out[0].Err)
	}
	if fi, err := os.Stat(stateDir); err != nil || !fi.IsDir() {
		t.Fatalf("state directory: %v", err)
	}
	// The answer's state and blob are stored, readable by the owner only.
	data, err := os.ReadFile(filepath.Join(stateDir, "state.json"))
	if err != nil || string(data) != `{"n":1}` {
		t.Fatalf("state file: %q (%v)", data, err)
	}
	if fi, err := os.Stat(filepath.Join(stateDir, "state.json")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode: %v %v", fi, err)
	}
	if data, err := os.ReadFile(filepath.Join(stateDir, "blobs", "render.png")); err != nil || string(data) != "png" {
		t.Fatalf("blob: %q (%v)", data, err)
	}
	// The plugin has no writable directory of its own.
	if data, err := os.ReadFile(env); err != nil || string(data) != "" {
		t.Fatalf("PAPERBOY_STATE_DIR: %q (%v)", data, err)
	}
	// A restart, e.g. after a config change, hands the state and the blob back.
	changed := specs["fake"]
	changed.Config = map[string]any{"ok": true, "more": 1}
	h.Configure(map[string]Spec{"fake": changed}, Env{Language: "de", Timezone: "UTC"})
	if out := h.Fetch(context.Background(), batch(w("fake", "a", 10, 5))); out[0].Err != nil {
		t.Fatal(out[0].Err)
	}
	if data, err := os.ReadFile(seen); err != nil || string(data) != `{"n":1}` {
		t.Fatalf("state after the restart: %q (%v)", data, err)
	}
	if data, err := os.ReadFile(blobs); err != nil || string(data) != "render.png=png" {
		t.Fatalf("blobs after the restart: %q (%v)", data, err)
	}
}

func TestHostLoadsStoredStateBeforeConfigure(t *testing.T) {
	f := newFake(t)
	stateDir := filepath.Join(t.TempDir(), "state", "fake")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	stored := `{"n":1}`
	if err := os.WriteFile(filepath.Join(stateDir, "state.json"), []byte(stored), 0o600); err != nil {
		t.Fatal(err)
	}
	seen := filepath.Join(t.TempDir(), "state")
	specs := f.spec("stateonconfigure")
	s := specs["fake"]
	s.StateDir = stateDir
	s.Env["FAKE_STATE"] = seen
	specs["fake"] = s
	h := newHost(t, specs)
	if out := h.Fetch(context.Background(), batch(w("fake", "a", 10, 5))); out[0].Err != nil {
		t.Fatal(out[0].Err)
	}
	// The configure answer must not replace the stored state before it is
	// handed back in the state request (ADR 0081).
	if data, err := os.ReadFile(seen); err != nil || string(data) != stored {
		t.Fatalf("state passed to the plugin: %q (%v), want %q", data, err, stored)
	}
}

func TestHostIgnoresUnusableState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "fake")
	specs := newFake(t).spec("badstate")
	s := specs["fake"]
	s.StateDir = dir
	specs["fake"] = s
	h := newHost(t, specs)
	if out := h.Fetch(context.Background(), batch(w("fake", "a", 10, 5))); out[0].Err != nil {
		t.Fatalf("the widget must still render: %v", out[0].Err)
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json")); !os.IsNotExist(err) {
		t.Fatalf("state that is no object was stored: %v", err)
	}
	if entries, err := os.ReadDir(filepath.Join(dir, "blobs")); err == nil && len(entries) > 0 {
		t.Fatalf("a blob with an unsafe name was stored: %v", entries)
	}
}

func TestHostAcceptsNewerProtocol(t *testing.T) {
	f := newFake(t)
	h := newHost(t, f.spec("newer"))
	// The protocol grows additively, so a newer plugin runs (ADR 0081).
	if out := h.Fetch(context.Background(), batch(w("fake", "a", 10, 5))); out[0].Err != nil {
		t.Fatalf("a newer protocol must run: %v", out[0].Err)
	}
}

func TestHostRejectsProtocol4(t *testing.T) {
	f := newFake(t)
	h := newHost(t, f.spec("oldversion"))
	out := h.Fetch(context.Background(), batch(w("fake", "a", 10, 5)))
	if out[0].Err == nil || !strings.Contains(out[0].Err.Error(), "needs at least 5") {
		t.Fatalf("protocol 4 must be refused: %+v", out[0])
	}
}

func TestHostLimitsBlobs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "fake")
	specs := newFake(t).spec("manyblobs")
	s := specs["fake"]
	s.StateDir = dir
	specs["fake"] = s
	h := newHost(t, specs)
	if out := h.Fetch(context.Background(), batch(w("fake", "a", 10, 5))); out[0].Err != nil {
		t.Fatal(out[0].Err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "blobs"))
	if err != nil || len(entries) != maxBlobs {
		t.Fatalf("blobs stored: %d (%v)", len(entries), err)
	}
}

func TestValidStateObject(t *testing.T) {
	for _, ok := range []string{`{}`, ` {"a":1} `, `null`} {
		if !validStateObject([]byte(ok)) {
			t.Fatalf("%q is state", ok)
		}
	}
	for _, bad := range []string{``, `{`, `[1]`, `"x"`, `1`} {
		if validStateObject([]byte(bad)) {
			t.Fatalf("%q is no state object", bad)
		}
	}
}

func TestHostChangedEventRefreshesInBackground(t *testing.T) {
	f := newFake(t)
	h := newHost(t, f.spec("changed"))
	h.Fetch(context.Background(), batch(w("fake", "a", 4, 4)))
	waitFor(t, func() bool { return runs(f.count) >= 2 }, "background refresh after the changed event")
}

func TestHostRestartsHangingPlugin(t *testing.T) {
	f := newFake(t)
	specs := f.spec("hang")
	s := specs["fake"]
	s.Timeout = 300 * time.Millisecond
	specs["fake"] = s
	h := newHost(t, specs)
	out := h.Fetch(context.Background(), batch(w("fake", "a", 1, 1)))
	if out[0].Err == nil || !strings.Contains(out[0].Err.Error(), "no answer") {
		t.Fatalf("want timeout, got %+v", out[0])
	}
	h.mu.Lock()
	n := len(h.procs)
	h.mu.Unlock()
	if n != 0 {
		t.Error("hanging plugin still registered")
	}
}

func TestHostEvictsIdleResults(t *testing.T) {
	f := newFake(t)
	h := newHost(t, f.spec("ok"))
	now := time.Now()
	h.Now = func() time.Time { return now }
	h.Fetch(context.Background(), batch(w("fake", "a", 1, 1)))
	now = now.Add(2 * time.Hour)
	h.maintain(context.Background())
	if len(h.cache) != 0 {
		t.Fatalf("idle result kept: %d", len(h.cache))
	}
}

type builtinPlugin struct{}

func (builtinPlugin) Describe() plugin.Description                   { return plugin.Description{Title: "b"} }
func (builtinPlugin) Configure(plugin.Values, *plugin.Request) error { return nil }
func (builtinPlugin) Render(_ *plugin.Request, _ plugin.WidgetRequest, _ *image.Paletted) error {
	return nil
}

type statusPlugin struct {
	got *plugin.ServerStatus
}

func (statusPlugin) Describe() plugin.Description                   { return plugin.Description{Title: "s"} }
func (statusPlugin) Configure(plugin.Values, *plugin.Request) error { return nil }
func (p *statusPlugin) Render(req *plugin.Request, _ plugin.WidgetRequest, _ *image.Paletted) error {
	p.got = req.ServerStatus
	return nil
}

func TestHostBuiltinGetsServerStatus(t *testing.T) {
	p := &statusPlugin{}
	h := &Host{Builtin: map[string]plugin.Handler{"b": p}}
	b := batch(w("b", "x", 3, 2))
	b.ServerStatus = &plugin.ServerStatus{CurrentDeviceStatus: &plugin.DeviceStatus{ID: "kueche"}}
	if out := h.Fetch(context.Background(), b); out[0].Err != nil {
		t.Fatal(out[0].Err)
	}
	if p.got == nil || p.got.CurrentDeviceStatus == nil || p.got.CurrentDeviceStatus.ID != "kueche" {
		t.Fatalf("built-in got %+v", p.got)
	}
}

func TestHostBuiltin(t *testing.T) {
	h := &Host{Builtin: map[string]plugin.Handler{"b": builtinPlugin{}}}
	out := h.Fetch(context.Background(), batch(w("b", "x", 3, 2)))
	if out[0].Image == nil || out[0].Image.Bounds().Dx() != 3 {
		t.Fatalf("builtin: %+v", out[0])
	}
	if d, err := h.Describe(context.Background(), "b"); err != nil || d.Title != "b" {
		t.Fatalf("describe: %v %v", d, err)
	}
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for " + what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
