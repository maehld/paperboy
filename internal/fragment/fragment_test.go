package fragment

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/internal/page"
	"github.com/maehld/paperboy/pkg/plugin"
)

// TestMain doubles as a fake plugin: with FAKE_PLUGIN set, the test binary
// answers one request and counts its runs in a file.
func TestMain(m *testing.M) {
	if mode := os.Getenv("FAKE_PLUGIN"); mode != "" {
		fakePlugin(mode)
		return
	}
	os.Exit(m.Run())
}

// fakePlugin speaks the line protocol by hand, so tests can break it.
func fakePlugin(mode string) {
	appendFile(os.Getenv("FAKE_STARTS"))
	if mode == "fail" {
		os.Stderr.WriteString("broken")
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		var req plugin.Request
		json.Unmarshal(sc.Bytes(), &req)
		appendFile(os.Getenv("FAKE_COUNT"))
		if mode == "hang" {
			continue
		}
		resp := plugin.Response{MaxAge: 60}
		for _, ir := range req.Images {
			w := ir.Width
			if mode == "wrongsize" {
				w++
			}
			var buf bytes.Buffer
			png.Encode(&buf, image.NewGray(image.Rect(0, 0, w, ir.Height)))
			resp.Images = append(resp.Images, plugin.ImageResult{PNG: buf.Bytes()})
		}
		enc.Encode(plugin.Message{ID: req.ID, Render: &resp})
		if mode == "changed" {
			enc.Encode(plugin.Message{Event: plugin.EventChanged, Images: []string{"a"}})
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

func fakeSpec(t *testing.T, mode, count string, starts ...string) map[string]Spec {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return map[string]Spec{"fake": {Name: "fake", Command: []string{exe}, Timeout: 10 * time.Second,
		Env: map[string]string{"FAKE_PLUGIN": mode, "FAKE_COUNT": count, "FAKE_STARTS": strings.Join(starts, "")}}}
}

func runs(t *testing.T, path string) int {
	b, _ := os.ReadFile(path)
	return len(b)
}

func TestHostCachesAndBatches(t *testing.T) {
	count := filepath.Join(t.TempDir(), "count")
	var h Host
	reqs := []page.FragmentRequest{{Plugin: "fake", Image: "a", Width: 10, Height: 5}, {Plugin: "fake", Image: "b", Width: 7, Height: 3}}
	out := h.Render(context.Background(), fakeSpec(t, "ok", count), reqs, true, Env{})
	for i, f := range out {
		if f.Image == nil || f.Image.Bounds().Dx() != reqs[i].Width {
			t.Fatalf("fragment %d: %+v", i, f)
		}
	}
	h.Render(context.Background(), fakeSpec(t, "ok", count), reqs, true, Env{})
	if n := runs(t, count); n != 1 {
		t.Errorf("plugin ran %d times, want 1 (one batch, then cached)", n)
	}
	// A different palette is a different fragment.
	h.Render(context.Background(), fakeSpec(t, "ok", count), reqs, false, Env{})
	if n := runs(t, count); n != 2 {
		t.Errorf("plugin ran %d times, want 2", n)
	}
}

func TestHostFailureKeepsStaleImage(t *testing.T) {
	count := filepath.Join(t.TempDir(), "count")
	var h Host
	reqs := []page.FragmentRequest{{Plugin: "fake", Image: "a", Width: 10, Height: 5}}
	h.Render(context.Background(), fakeSpec(t, "ok", "", count), reqs, true, Env{})
	for k, e := range h.cache {
		e.expires = time.Time{}
		h.cache[k] = e
	}
	out := h.Render(context.Background(), fakeSpec(t, "fail", "", count), reqs, true, Env{})
	if out[0].Image == nil || !out[0].Stale {
		t.Fatalf("want stale image, got %+v", out[0])
	}
	// Without a cached copy the error is reported; the plugin is not run again
	// right away.
	other := []page.FragmentRequest{{Plugin: "fake", Image: "z", Width: 3, Height: 3}}
	out = h.Render(context.Background(), fakeSpec(t, "fail", "", count), other, true, Env{})
	if out[0].Err == nil {
		t.Fatal("want error")
	}
	if n := runs(t, count); n != 2 {
		t.Errorf("plugin ran %d times, want 2 (backoff after failure)", n)
	}
}

func TestHostRejectsWrongSize(t *testing.T) {
	var h Host
	reqs := []page.FragmentRequest{{Plugin: "fake", Image: "a", Width: 10, Height: 5}, {Plugin: "nope", Image: "a", Width: 1, Height: 1}}
	out := h.Render(context.Background(), fakeSpec(t, "wrongsize", ""), reqs, true, Env{})
	if out[0].Err == nil || out[1].Err == nil {
		t.Fatalf("want errors, got %+v", out)
	}
}

func newHost(t *testing.T) *Host {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h := &Host{}
	h.Start(ctx)
	return h
}

func TestHostKeepsProcessRunning(t *testing.T) {
	dir := t.TempDir()
	count, starts := filepath.Join(dir, "count"), filepath.Join(dir, "starts")
	h := newHost(t)
	specs := fakeSpec(t, "ok", count, starts)
	for i := range 3 {
		reqs := []page.FragmentRequest{{Plugin: "fake", Image: "a", Width: 10 + i, Height: 5}}
		if out := h.Render(context.Background(), specs, reqs, true, Env{}); out[0].Image == nil {
			t.Fatalf("render %d: %+v", i, out[0])
		}
	}
	if n := runs(t, starts); n != 1 {
		t.Errorf("plugin started %d times, want 1", n)
	}
	if n := runs(t, count); n != 3 {
		t.Errorf("plugin got %d requests, want 3", n)
	}
	// A changed configuration restarts the plugin.
	specs["fake"] = Spec{Name: "fake", Command: specs["fake"].Command, Timeout: 9 * time.Second, Env: specs["fake"].Env}
	h.Reconcile(specs)
	h.Render(context.Background(), specs, []page.FragmentRequest{{Plugin: "fake", Image: "a", Width: 1, Height: 1}}, true, Env{})
	if n := runs(t, starts); n != 2 {
		t.Errorf("plugin started %d times after a change, want 2", n)
	}
}

func TestHostChangedEventExpiresImages(t *testing.T) {
	count := filepath.Join(t.TempDir(), "count")
	h := newHost(t)
	specs := fakeSpec(t, "changed", count)
	reqs := []page.FragmentRequest{{Plugin: "fake", Image: "a", Width: 4, Height: 4}}
	h.Render(context.Background(), specs, reqs, true, Env{})
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.mu.Lock()
		expired := false
		for _, e := range h.cache {
			expired = e.expires.IsZero()
		}
		h.mu.Unlock()
		if expired {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("changed event did not expire the image")
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.Render(context.Background(), specs, reqs, true, Env{})
	if n := runs(t, count); n != 2 {
		t.Errorf("plugin got %d requests, want 2", n)
	}
}

func TestHostRestartsHangingPlugin(t *testing.T) {
	h := newHost(t)
	specs := fakeSpec(t, "hang", "")
	s := specs["fake"]
	s.Timeout = 300 * time.Millisecond
	specs["fake"] = s
	out := h.Render(context.Background(), specs, []page.FragmentRequest{{Plugin: "fake", Image: "a", Width: 1, Height: 1}}, true, Env{})
	if out[0].Err == nil || !strings.Contains(out[0].Err.Error(), "no answer") {
		t.Fatalf("want timeout, got %+v", out[0])
	}
	h.mu.Lock()
	n := len(h.procs)
	h.mu.Unlock()
	if n != 0 {
		t.Errorf("hanging plugin still registered")
	}
}
