package stats

import (
	"testing"
	"time"
)

// TestTimerReportsSamples checks what a timer says after a few observations:
// the count, the total, the average, the largest, the last and the two
// percentiles of the nearest rank.
func TestTimerReportsSamples(t *testing.T) {
	r := NewRegistry()
	for _, d := range []time.Duration{1 * time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond, 4 * time.Millisecond} {
		r.Observe("op", d)
	}
	got := r.Timing("op")
	if got.Count != 4 || got.Total != 10*time.Millisecond || got.Avg != 2500*time.Microsecond {
		t.Fatalf("count/total/average: %+v", got)
	}
	if got.Last != 4*time.Millisecond || got.Max != 4*time.Millisecond {
		t.Fatalf("last/max: %+v", got)
	}
	// nearest rank: p50 of four samples is the second, p95 the fourth.
	if got.P50 != 2*time.Millisecond || got.P95 != 4*time.Millisecond {
		t.Fatalf("percentiles: %+v", got)
	}

	done := r.Start("measure")
	done()
	if n := r.Timing("measure").Count; n != 1 {
		t.Fatalf("Start recorded %d observations, want 1", n)
	}
}

// TestTimerKeepsRecentSamples checks that a timer ages out old samples: the
// count keeps growing, the percentile does not remember a spike from long ago.
func TestTimerKeepsRecentSamples(t *testing.T) {
	r := NewRegistry()
	r.Observe("op", time.Hour)
	for i := 0; i < 2*reservoir; i++ {
		r.Observe("op", time.Millisecond)
	}
	got := r.Timing("op")
	if got.Count != 2*reservoir+1 {
		t.Fatalf("count = %d, want %d", got.Count, 2*reservoir+1)
	}
	if got.Max != time.Hour {
		t.Fatalf("max must keep the spike: %v", got.Max)
	}
	if got.P95 != time.Millisecond {
		t.Fatalf("p95 must come from the recent samples: %v", got.P95)
	}
}

// TestTimingsAreSorted checks the order of the list the page renders.
func TestTimingsAreSorted(t *testing.T) {
	r := NewRegistry()
	r.Observe("b", time.Millisecond)
	r.Observe("a", time.Millisecond)
	list := r.Timings()
	if len(list) != 2 || list[0].Name != "a" || list[1].Name != "b" {
		t.Fatalf("timings = %+v", list)
	}
}

// TestNilRegistry checks that an uninstrumented server serves the page: a nil
// registry accepts timings and reports none.
func TestNilRegistry(t *testing.T) {
	var r *Registry
	r.Observe("op", time.Millisecond)
	r.Start("op")()
	if got := r.Timing("op"); got.Count != 0 {
		t.Fatalf("nil registry reported %+v", got)
	}
	if list := r.Timings(); list != nil {
		t.Fatalf("nil registry listed %+v", list)
	}
}

// TestGaugeTurnsCounterIntoRate checks the CPU percentage: the first view only
// records a baseline, the next one divides the used time by the window.
func TestGaugeTurnsCounterIntoRate(t *testing.T) {
	var g Gauge
	start := time.Date(2026, time.October, 21, 8, 0, 0, 0, time.UTC)
	if _, _, ok := g.Observe(10, start); ok {
		t.Fatal("the first observation must not report a rate")
	}
	percent, window, ok := g.Observe(11, start.Add(4*time.Second))
	if !ok || window != 4*time.Second {
		t.Fatalf("second observation: %v %v %v", percent, window, ok)
	}
	if percent != 25 {
		t.Fatalf("percent = %v, want 25 (one of four seconds)", percent)
	}
	// A counter that goes backwards (a replaced process) reports nothing.
	if _, _, ok := g.Observe(1, start.Add(5*time.Second)); ok {
		t.Fatal("a decreasing counter must not report a rate")
	}
}
