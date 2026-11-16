package plugin

import (
	"context"
	"sync"
	"time"
)

// DemandLead is how long before the named moment Wait wakes, so a plugin can
// fetch before the device asks. It is not a promise: a fetch can take longer.
const DemandLead = 30 * time.Second

// Event is a line Paperboy sends to a plugin without a request id. The only
// event is EventSchedule (ADR 0126): NextRefresh names when Paperboy next
// needs the plugin's data. A missing NextRefresh means no display needs it now.
type Event struct {
	Event       string     `json:"event"`
	NextRefresh *time.Time `json:"next_refresh,omitempty"`
}

// Eventer receives the events Paperboy sends to a plugin. Unknown events are
// ignored, so a newer Paperboy can send more of them. Embed Demand in a
// handler to receive schedules without writing Event.
type Eventer interface {
	Event(Event)
}

// Demand holds the next moment Paperboy needs the plugin's data (ADR 0126).
// Embed it in a plugin's handler and call Wait in the plugin's Start loop:
//
//	type weather struct{ plugin.Demand }
//
//	func (w *weather) Start(ctx context.Context, notify func(...string)) {
//		for {
//			if !w.Wait(ctx, w.minRefresh) {
//				return
//			}
//			if err := w.fetch(ctx); err != nil {
//				continue
//			}
//			notify()
//		}
//	}
//
// The zero value is ready. Event implements Eventer; ServeIO calls it when a
// schedule line arrives.
type Demand struct {
	mu   sync.Mutex
	next time.Time
	has  bool
	wake chan struct{}
	last time.Time // the previous Wait return, for the floor
}

// Event records a schedule. Other events are ignored. A missing NextRefresh
// clears the demand: the plugin may idle until Paperboy sends a new one.
func (d *Demand) Event(ev Event) {
	if ev.Event != EventSchedule {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if ev.NextRefresh == nil {
		d.has, d.next = false, time.Time{}
	} else {
		d.has, d.next = true, *ev.NextRefresh
	}
	d.signalLocked()
}

// Wait blocks until Paperboy needs the plugin's data: the named next_refresh
// minus DemandLead. A new schedule wakes it. It returns no earlier than floor
// after this call, so the plugin's own minimum interval wins over a demand
// that comes sooner. With no demand it blocks until a schedule arrives or ctx
// ends. Wait returns false when ctx ends.
//
// Call Wait after each fetch: the floor then counts from the end of the fetch,
// not from the previous return.
func (d *Demand) Wait(ctx context.Context, floor time.Duration) bool {
	d.mu.Lock()
	// The caller asks for the next fetch now, after the previous one.
	d.last = time.Now()
	d.mu.Unlock()
	for {
		d.mu.Lock()
		if d.wake == nil {
			d.wake = make(chan struct{})
		}
		wake, has, next, last := d.wake, d.has, d.next, d.last
		d.mu.Unlock()

		if !has {
			select {
			case <-ctx.Done():
				return false
			case <-wake:
				continue
			}
		}

		at := next.Add(-DemandLead)
		if floor > 0 && !last.IsZero() {
			if f := last.Add(floor); f.After(at) {
				at = f
			}
		}
		now := time.Now()
		if !now.Before(at) {
			d.mu.Lock()
			// A schedule that arrived while the lock was open wins: loop and
			// compute with the new moment instead of returning on the old one.
			if d.wake == wake {
				d.mu.Unlock()
				return true
			}
			d.mu.Unlock()
			continue
		}

		timer := time.NewTimer(at.Sub(now))
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// signalLocked wakes every Wait. Callers hold d.mu.
func (d *Demand) signalLocked() {
	if d.wake != nil {
		close(d.wake)
	}
	d.wake = make(chan struct{})
}
