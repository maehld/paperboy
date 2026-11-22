package paperboy

import (
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/server/internal/stats"
	"github.com/maehld/paperboy/server/internal/widget"
)

// TestStatsPage checks the runtime page (ADR 0104): Paperboy's own numbers, the
// plugin table with the built-in plugin, the caches and the timings of the
// sync the test just ran.
func TestStatsPage(t *testing.T) {
	s, _ := planServer(t, testPlan)
	dev := planDevice(t, s)
	displayPage(t, s, dev)

	rec := htmlRequest(t, s, "/ui/system/stats")
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("stats page: %d %v", rec.Code, rec.Header())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<a href="/ui/system/stats" class="active" aria-current="page">Stats</a>`,
		"Paperboy <span",
		"Resident memory",
		"Plugins",
		`<td class="name">paperboy</td>`,
		"built-in",
		"What the plugins open",
		"Last exit",
		"Disk",
		"runs in the Paperboy process",
		"Widget cache",
		"Age",
		"Paperboy caches",
		"Rendered pages",
		"Page files",
		"Sync, whole request",
		"Syncs and polls",
		`<td class="name">test</td>`,
		`hx-trigger="every 10s`,
		`id="live"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("stats page misses %q:\n%s", want, body)
		}
	}

	// The sync was measured as a whole and per device.
	if got := s.stats.Timing("sync.total"); got.Count != 1 || got.Last <= 0 {
		t.Fatalf("sync.total = %+v", got)
	}
	if got := s.stats.Timing("sync.device:test"); got.Count != 1 {
		t.Fatalf("sync.device:test = %+v", got)
	}
	if got := s.stats.Timing("render.page"); got.Count == 0 {
		t.Fatalf("render.page was not measured: %+v", got)
	}
}

// TestPluginObservation covers the observation rows of the runtime page
// (stage 0 of issue #306): the lists are cut, the exit names its signal and the
// stderr tail travels along.
func TestPluginObservation(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	files := []string{"/a", "/b", "/c", "/d", "/e", "/f", "/g", "/h", "/i", "/j"}
	p := widget.PluginStat{
		Name: "evcc", Running: true, Proc: stats.Proc{Files: files, ReadBytes: 2048, WriteBytes: 4096},
		Stderr: []string{"one", "two"},
	}
	obs := pluginObservation(p, now)
	if obs.State != "running" || obs.Disk != "read 2.0 KiB · wrote 4.0 KiB" {
		t.Fatalf("observation: %+v", obs)
	}
	if len(obs.Files) != obsFiles || obs.FilesMore != len(files)-obsFiles {
		t.Fatalf("files = %d, more = %d", len(obs.Files), obs.FilesMore)
	}
	if obs.LastExit != "–" || obs.Note != "" {
		t.Fatalf("a running plugin without an exit: %+v", obs)
	}
	if got := strings.Join(obs.Stderr, ","); got != "one,two" {
		t.Fatalf("stderr = %q", got)
	}

	p.LastExit = &widget.ExitInfo{Code: -1, Signal: "SIGSYS", At: now.Add(-2 * time.Minute), Stderr: []string{"denied"}}
	obs = pluginObservation(p, now)
	if obs.LastExit != "SIGSYS · 2 min ago" {
		t.Fatalf("last exit = %q", obs.LastExit)
	}

	// A stopped plugin without system numbers says why the block is empty.
	p = widget.PluginStat{Name: "old", Proc: stats.Proc{}, Running: false}
	obs = pluginObservation(p, now)
	if obs.State != "stopped" || obs.Files != nil || obs.Conns != nil {
		t.Fatalf("stopped plugin: %+v", obs)
	}
}

// TestStatsPageEmptyInstance checks that an instance without devices and
// without plugins still renders the page.
func TestStatsPageEmptyInstance(t *testing.T) {
	cfg := &Config{Dir: t.TempDir(), Timezone: time.UTC,
		Profiles: map[string]Profile{"small": {Width: 40, Height: 20, PPI: 125}}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	body := htmlRequest(t, s, "/ui/system/stats").Body.String()
	if !strings.Contains(body, "No device has synced") || !strings.Contains(body, "Nothing measured yet") {
		t.Fatalf("empty instance page:\n%s", body)
	}
	if !strings.Contains(body, `<td class="name">paperboy</td>`) {
		t.Fatal("the built-in plugin must be listed")
	}
}

// TestStatsTextHelpers checks the units of the page: sizes, latencies and the
// age buckets.
func TestStatsTextHelpers(t *testing.T) {
	if got := bytesText(0); got != "0 B" {
		t.Fatalf("bytesText(0) = %q", got)
	}
	if got := bytesText(1536); got != "1.5 KiB" {
		t.Fatalf("bytesText(1536) = %q", got)
	}
	if got := bytesText(5 << 20); got != "5.0 MiB" {
		t.Fatalf("bytesText(5 MiB) = %q", got)
	}
	if got := latencyText(0); got != "–" {
		t.Fatalf("latencyText(0) = %q", got)
	}
	if got := latencyText(2500 * time.Microsecond); got != "2.5 ms" {
		t.Fatalf("latencyText(2.5ms) = %q", got)
	}
	if got := latencyText(1500 * time.Millisecond); got != "1.50 s" {
		t.Fatalf("latencyText(1.5s) = %q", got)
	}
	if got := spanText(90 * time.Minute); got != "90 min" {
		t.Fatalf("spanText(90m) = %q", got)
	}
	if got := percentText(3.25); got != "3.2 %" {
		t.Fatalf("percentText(3.25) = %q", got)
	}
	buckets := []widget.AgeBucket{{Within: time.Minute}, {Within: 5 * time.Minute}, {}}
	for i, want := range []string{"< 1 min", "1 min – 5 min", "≥ 5 min"} {
		if got := ageRange(i, buckets); got != want {
			t.Fatalf("ageRange(%d) = %q, want %q", i, got, want)
		}
	}
}
