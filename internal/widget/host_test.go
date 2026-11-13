package widget

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
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
		appendFile(os.Getenv("FAKE_COUNT"))
		if mode == "hang" {
			continue
		}
		if req.Action == plugin.ActionConditions {
			msg := plugin.Message{ID: req.ID, Version: version, Conditions: map[string]bool{"low": true}}
			if mode == "condfail" {
				msg.Error = "no data"
			}
			enc.Encode(msg)
			continue
		}
		msg := plugin.Message{ID: req.ID, Version: version, MaxAge: 60}
		for _, w := range req.Widgets {
			if req.Action == plugin.ActionText {
				msg.Widgets = append(msg.Widgets, plugin.Result{Lines: []string{w.Widget}})
				continue
			}
			width := w.Width
			if mode == "wrongsize" {
				width++
			}
			var buf bytes.Buffer
			png.Encode(&buf, image.NewGray(image.Rect(0, 0, width, w.Height)))
			msg.Widgets = append(msg.Widgets, plugin.Result{PNG: buf.Bytes()})
		}
		switch mode {
		case "state":
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

func TestHostConditionsAreCached(t *testing.T) {
	f := newFake(t)
	h := newHost(t, f.spec("ok"))
	active, err := h.Conditions(context.Background(), "fake")
	if err != nil || !active["low"] || active["high"] {
		t.Fatalf("conditions: %v %v", active, err)
	}
	if _, err := h.Conditions(context.Background(), "fake"); err != nil {
		t.Fatal(err)
	}
	if n := runs(f.count); n != 1 {
		t.Errorf("plugin got %d conditions requests, want 1 (cached)", n)
	}
	// A changed event makes the next sync ask again (ADR 0103).
	h.expire("fake", nil)
	if _, err := h.Conditions(context.Background(), "fake"); err != nil {
		t.Fatal(err)
	}
	if n := runs(f.count); n != 2 {
		t.Errorf("plugin got %d conditions requests after changed, want 2", n)
	}
	if _, err := h.Conditions(context.Background(), "nope"); err == nil {
		t.Error("an unknown plugin must answer with an error")
	}
	// A failing plugin yields an error, so its conditions count as inactive.
	h2 := newHost(t, f.spec("condfail"))
	if _, err := h2.Conditions(context.Background(), "fake"); err == nil {
		t.Error("a failing conditions answer must be an error")
	}
}

func TestHostText(t *testing.T) {
	f := newFake(t)
	h := newHost(t, f.spec("ok"))
	out := h.Fetch(context.Background(), page.Batch{Text: true, Widgets: []page.WidgetRequest{w("fake", "agenda", 20, 3)}})
	if len(out[0].Lines) != 1 || out[0].Lines[0] != "agenda" {
		t.Fatalf("text: %+v", out[0])
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

func TestHostBuiltin(t *testing.T) {
	h := &Host{Builtin: map[string]plugin.Handler{"b": builtinPlugin{}}}
	out := h.Fetch(context.Background(), batch(w("b", "x", 3, 2)))
	if out[0].Image == nil || out[0].Image.Bounds().Dx() != 3 {
		t.Fatalf("builtin: %+v", out[0])
	}
	if d, err := h.Describe(context.Background(), "b"); err != nil || d.Title != "b" {
		t.Fatalf("describe: %v %v", d, err)
	}
	if out := h.Fetch(context.Background(), page.Batch{Text: true, Widgets: []page.WidgetRequest{w("b", "x", 3, 2)}}); out[0].Err == nil {
		t.Fatal("a plugin without text widgets must fail on text displays")
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
