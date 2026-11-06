package fragment

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
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

func fakePlugin(mode string) {
	if f := os.Getenv("FAKE_COUNT"); f != "" {
		b, _ := os.ReadFile(f)
		os.WriteFile(f, append(b, 'x'), 0o644)
	}
	if mode == "fail" {
		os.Stderr.WriteString("broken")
		os.Exit(1)
	}
	var req plugin.Request
	json.NewDecoder(os.Stdin).Decode(&req)
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
	json.NewEncoder(os.Stdout).Encode(resp)
}

func fakeSpec(t *testing.T, mode, count string) map[string]Spec {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return map[string]Spec{"fake": {Name: "fake", Command: []string{exe}, Timeout: 10 * time.Second,
		Env: map[string]string{"FAKE_PLUGIN": mode, "FAKE_COUNT": count}}}
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
	h.Render(context.Background(), fakeSpec(t, "ok", count), reqs, true, Env{})
	for k, e := range h.cache {
		e.expires = time.Time{}
		h.cache[k] = e
	}
	out := h.Render(context.Background(), fakeSpec(t, "fail", count), reqs, true, Env{})
	if out[0].Image == nil || !out[0].Stale {
		t.Fatalf("want stale image, got %+v", out[0])
	}
	// Without a cached copy the error is reported; the plugin is not run again
	// right away.
	other := []page.FragmentRequest{{Plugin: "fake", Image: "z", Width: 3, Height: 3}}
	out = h.Render(context.Background(), fakeSpec(t, "fail", count), other, true, Env{})
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
