package widget

import (
	"context"
	"image"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

// TestStatsReportsRequestsAndCache checks what the runtime page reads from the
// host: one row per plugin, the requests it answered, the wait and answer
// timings and the widgets the cache holds with their age (ADR 0104).
func TestStatsReportsRequestsAndCache(t *testing.T) {
	f := newFake(t)
	h := newHost(t, f.spec("ok"))
	ctx := context.Background()
	if res := h.Fetch(ctx, batch(w("fake", "a", 10, 10))); len(res) != 1 || res[0].Err != nil {
		t.Fatalf("first fetch: %+v", res)
	}
	if res := h.Fetch(ctx, batch(w("fake", "a", 10, 10))); len(res) != 1 || res[0].Err != nil {
		t.Fatalf("cached fetch: %+v", res)
	}

	now := time.Now()
	rows, cache := h.Stats(now)
	if len(rows) != 1 || rows[0].Name != "fake" {
		t.Fatalf("rows = %+v", rows)
	}
	row := rows[0]
	if !row.Running || row.PID == 0 || row.Starts != 1 {
		t.Fatalf("plugin state: %+v", row)
	}
	// The second fetch came from the cache, so the plugin answered once.
	if row.Requests != 1 || row.Errors != 0 {
		t.Fatalf("requests = %d, errors = %d", row.Requests, row.Errors)
	}
	if row.Answer.Count != 1 || row.Answer.Last <= 0 {
		t.Fatalf("answer timing = %+v", row.Answer)
	}
	if row.CacheEntries != 1 || row.CacheBytes == 0 {
		t.Fatalf("cache row = %+v", row)
	}
	if cache.Entries != 1 || cache.Images != 1 || cache.Bytes == 0 {
		t.Fatalf("cache = %+v", cache)
	}
	var entries int
	for _, b := range cache.Ages {
		entries += b.Entries
	}
	if entries != cache.Entries {
		t.Fatalf("age buckets hold %d of %d entries", entries, cache.Entries)
	}
	if cache.Oldest.IsZero() || now.Sub(cache.Oldest) > time.Minute {
		t.Fatalf("oldest entry = %v", cache.Oldest)
	}
}

// TestStatsCountsFailures checks that a plugin which cannot be started shows
// its error and that the host reports the plugin as failing.
func TestStatsCountsFailures(t *testing.T) {
	f := newFake(t)
	h := newHost(t, f.spec("badconfig"))
	if res := h.Fetch(context.Background(), batch(w("fake", "a", 10, 10))); len(res) != 1 || res[0].Err == nil {
		t.Fatalf("expected a failed fetch: %+v", res)
	}
	rows, _ := h.Stats(time.Now())
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	row := rows[0]
	if row.Requests != 1 || row.Errors != 1 || row.LastError == "" {
		t.Fatalf("counters = %+v", row)
	}
	if !row.Failing || row.RetryAt.IsZero() {
		t.Fatalf("failing state = %+v", row)
	}
	if row.Running {
		t.Fatalf("a rejected plugin must not count as running: %+v", row)
	}
}

// TestStatsListsBuiltin checks that the in-process plugin appears without a
// process and without cache entries.
func TestStatsListsBuiltin(t *testing.T) {
	h := newHost(t, nil)
	h.Builtin = map[string]plugin.Handler{"paperboy": nil}
	rows, cache := h.Stats(time.Now())
	if len(rows) != 1 || rows[0].Name != "paperboy" || !rows[0].Builtin || !rows[0].Running {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].PID != 0 || rows[0].CacheEntries != 0 {
		t.Fatalf("built-in row = %+v", rows[0])
	}
	if cache.Entries != 0 {
		t.Fatalf("cache = %+v", cache)
	}
}

// TestEntryBytes checks the memory a cached widget is charged with: the pixel
// buffer of an image, the bytes of its lines.
func TestEntryBytes(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 10, 10))
	if got := entryBytes(&entry{img: img}); got != int64(len(img.Pix)) {
		t.Fatalf("image bytes = %d, want %d", got, len(img.Pix))
	}
	if got := entryBytes(&entry{lines: []string{"abc", "de"}}); got != 5 {
		t.Fatalf("line bytes = %d, want 5", got)
	}
}
