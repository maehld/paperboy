package widget

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

// scheduleWriter records the lines a fake plugin process receives.
type scheduleWriter struct {
	mu    sync.Mutex
	lines []string
}

func (w *scheduleWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lines = append(w.lines, string(p))
	return len(p), nil
}

func (w *scheduleWriter) Close() error { return nil }

func (w *scheduleWriter) events(t *testing.T) []plugin.Event {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]plugin.Event, 0, len(w.lines))
	for _, line := range w.lines {
		var ev plugin.Event
		if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &ev); err != nil {
			t.Fatalf("event line %q: %v", line, err)
		}
		out = append(out, ev)
	}
	return out
}

func TestPushSchedules(t *testing.T) {
	w := &scheduleWriter{}
	p := &process{spec: Spec{Name: "p"}, stdin: w, proto: 9}
	h := &Host{procs: map[string]*process{"p": p}, sent: map[string]*time.Time{}}
	at := time.Now().Add(10 * time.Minute).Round(time.Second)

	h.pushSchedules(map[string]time.Time{"p": at})
	events := w.events(t)
	if len(events) != 1 || events[0].Event != plugin.EventSchedule || events[0].NextRefresh == nil || !events[0].NextRefresh.Equal(at) {
		t.Fatalf("first schedule: %+v", events)
	}

	// A change within a minute is not worth a line.
	h.pushSchedules(map[string]time.Time{"p": at.Add(30 * time.Second)})
	if events = w.events(t); len(events) != 1 {
		t.Fatalf("a small change sent another event: %+v", events)
	}

	// A larger change is.
	h.pushSchedules(map[string]time.Time{"p": at.Add(2 * time.Minute)})
	if events = w.events(t); len(events) != 2 || !events[1].NextRefresh.Equal(at.Add(2*time.Minute)) {
		t.Fatalf("a larger change: %+v", events)
	}

	// No demand clears once.
	h.pushSchedules(map[string]time.Time{})
	if events = w.events(t); len(events) != 3 || events[2].NextRefresh != nil {
		t.Fatalf("cleared schedule: %+v", events)
	}
	h.pushSchedules(map[string]time.Time{})
	if events = w.events(t); len(events) != 3 {
		t.Fatalf("the clear was sent twice: %+v", events)
	}
}

func TestPushSchedulesNeedsProtocolNine(t *testing.T) {
	w := &scheduleWriter{}
	p := &process{spec: Spec{Name: "p"}, stdin: w, proto: 8}
	h := &Host{procs: map[string]*process{"p": p}, sent: map[string]*time.Time{}}
	h.pushSchedules(map[string]time.Time{"p": time.Now().Add(time.Hour)})
	if events := w.events(t); len(events) != 0 {
		t.Fatalf("a protocol 8 plugin got a schedule: %+v", events)
	}
}

func TestNeeded(t *testing.T) {
	h := &Host{}
	now := time.Now()
	if !h.needed("p", nil, now) {
		t.Error("without demand information the old refresh behavior must stay")
	}
	if h.needed("p", map[string]time.Time{}, now) {
		t.Error("an empty demand refreshed an entry")
	}
	if !h.needed("p", map[string]time.Time{"p": now.Add(20 * time.Second)}, now) {
		t.Error("a demand within refreshAhead was not refreshed")
	}
	if h.needed("p", map[string]time.Time{"p": now.Add(40 * time.Second)}, now) {
		t.Error("a demand after refreshAhead was refreshed")
	}
	if !h.needed("p", map[string]time.Time{"p": now.Add(-time.Minute)}, now) {
		t.Error("a past demand was not refreshed")
	}
}

func TestMaintainRefreshesOnlyWithDemand(t *testing.T) {
	f := newFake(t)
	h := newHost(t, f.spec("ok"))
	b := batch(w("fake", "a", 10, 5))
	h.Fetch(context.Background(), b)
	if n := runs(f.count); n != 1 {
		t.Fatalf("first fetch: %d requests", n)
	}
	for _, e := range h.cache {
		e.expires = time.Now()
		e.lastUsed = time.Now()
	}
	h.Demand = func() map[string]time.Time {
		return map[string]time.Time{"fake": time.Now().Add(time.Hour)}
	}
	h.maintain(context.Background())
	if n := runs(f.count); n != 1 {
		t.Fatalf("a far demand refreshed the entry: %d requests", n)
	}
	h.Demand = func() map[string]time.Time {
		return map[string]time.Time{"fake": time.Now().Add(10 * time.Second)}
	}
	h.maintain(context.Background())
	if n := runs(f.count); n != 2 {
		t.Fatalf("a due demand did not refresh: %d requests", n)
	}
}
