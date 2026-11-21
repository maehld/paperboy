package paperboy

import "time"

// alignRefresh returns the sleep that lands on the next multiple of interval
// after local midnight (ADR 0129). A device that sleeps this long wakes on the
// clock grid, e.g. at :00, :10 and :20 for ten minutes.
//
// The slot is the start of the sleep, not the paint: the device still spends a
// few seconds on the download and the panel, so it syncs a moment after the
// slot. Without that offset the answer would have to guess the cycle time, and
// a guess can land before the slot, which shows 09:59 instead of 10:00.
//
// A slot closer than the floor is skipped, because the device may not wake
// sooner. A slot is never in the past: the next multiple is strictly after
// now. It returns 0 when the interval cannot form a grid.
func alignRefresh(now time.Time, interval, floor time.Duration, loc *time.Location) time.Duration {
	// An interval that does not fit into a day has no clock grid: "every 36
	// hours" starts at an arbitrary point.
	if interval < time.Second || interval > 24*time.Hour {
		return 0
	}
	if loc == nil {
		loc = time.Local
	}
	local := now.In(loc)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	// The grid counts from local midnight as a duration, so a DST change keeps
	// the interval: the wall clock jumps and the slots jump with it.
	elapsed := local.Sub(midnight)
	target := midnight.Add((elapsed/interval + 1) * interval)
	sleep := target.Sub(now)
	if sleep < floor {
		// The next slot is too close for the power source: take the one after
		// it, so the device never wakes below its floor.
		steps := (floor - sleep + interval - 1) / interval
		sleep += steps * interval
	}
	if sleep < time.Second {
		return 0
	}
	return sleep
}
