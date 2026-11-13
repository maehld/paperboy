package paperboy

import (
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/internal/widget"
)

// TestStatsPage checks the runtime page (ADR 0104): Paperboy's own numbers, the
// plugin table with the built-in plugin, the caches and the timings of the
// sync the test just ran.
func TestStatsPage(t *testing.T) {
	s, _ := planServer(t, testPlan)
	dev := planDevice(t, s)
	displayPage(t, s, dev)

	rec := htmlRequest(t, s, "/ui/stats")
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("stats page: %d %v", rec.Code, rec.Header())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<a class="active" href="/ui/stats">Stats</a>`,
		"Paperboy <span",
		"Resident memory",
		"Plugins",
		`<td class="name">paperboy</td>`,
		"built-in",
		"Widget cache",
		"Age",
		"Paperboy caches",
		"Rendered pages",
		"Page files",
		"Sync, whole request",
		"Syncs and polls",
		`<td class="name">test</td>`,
		`<meta http-equiv="refresh" content="10">`,
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
	body := htmlRequest(t, s, "/ui/stats").Body.String()
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
