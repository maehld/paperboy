package paperboy

import (
	"testing"
	"time"
)

// TestAlignRefresh covers the clock grid (ADR 0129).
func TestAlignRefresh(t *testing.T) {
	utc := func(h, m, s int) time.Time { return time.Date(2026, 10, 17, h, m, s, 0, time.UTC) }
	for _, tc := range []struct {
		name     string
		now      time.Time
		interval time.Duration
		floor    time.Duration
		want     time.Duration
	}{
		{"ten minutes", utc(10, 3, 20), 10 * time.Minute, time.Minute, 400 * time.Second},
		{"across the hour", utc(10, 52, 0), 15 * time.Minute, time.Minute, 8 * time.Minute},
		{"exactly on a slot", utc(10, 10, 0), 10 * time.Minute, time.Minute, 10 * time.Minute},
		{"slot inside the floor", utc(10, 9, 30), 10 * time.Minute, time.Minute, 10*time.Minute + 30*time.Second},
		{"hourly", utc(10, 3, 0), time.Hour, time.Minute, 57 * time.Minute},
		{"interval below the floor", utc(10, 3, 20), 30 * time.Second, time.Minute, 70 * time.Second},
		{"no interval", utc(10, 3, 0), 0, time.Minute, 0},
		{"longer than a day", utc(10, 3, 0), 25 * time.Hour, time.Minute, 0},
	} {
		if got := alignRefresh(tc.now, tc.interval, tc.floor, time.UTC); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}

	// A DST change keeps the interval and moves the wall clock with it: the
	// slots before the jump land on the wall clock after it.
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("no zoneinfo:", err)
	}
	zone := func(y int, month time.Month, d, h, m int) time.Time {
		return time.Date(y, month, d, h, m, 0, 0, berlin)
	}
	for _, tc := range []struct {
		name string
		now  time.Time
		want time.Duration
	}{
		{"before the spring jump", zone(2026, time.March, 29, 1, 58), 2 * time.Minute},
		{"after the spring jump", zone(2026, time.March, 29, 3, 5), 5 * time.Minute},
	} {
		got := alignRefresh(tc.now, 10*time.Minute, time.Minute, berlin)
		if got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
	if target := zone(2026, time.March, 29, 1, 58).Add(2 * time.Minute).In(berlin).Format("15:04"); target != "03:00" {
		t.Errorf("the slot after the jump lands on %s, want 03:00", target)
	}
}

// TestSyncAlignsTimerSleeps checks that a timer sync reports the rest of the
// interval instead of the whole interval, and that a plan boundary keeps its
// own sleep (ADR 0129).
func TestSyncAlignsTimerSleeps(t *testing.T) {
	s, now := planServer(t, testPlan)

	// Inside the five-minute window the grid is five minutes.
	*now = time.Date(2026, time.October, 21, 8, 3, 20, 0, time.UTC)
	dev := planDevice(t, s)
	if got := displayPage(t, s, dev)["refresh_s"].(float64); got != 100 {
		t.Fatalf("five minutes at 08:03:20 must sleep 100 s, got %v", got)
	}

	// Outside it the default thirty minutes align to :00 and :30.
	*now = time.Date(2026, time.October, 21, 20, 3, 20, 0, time.UTC)
	if got := displayPage(t, s, dev)["refresh_s"].(float64); got != 1600 {
		t.Fatalf("thirty minutes at 20:03:20 must sleep 1600 s, got %v", got)
	}

	// A window edge 30 s away caps the sleep at the one-minute floor; the cap
	// wins and is not stretched onto the grid.
	*now = time.Date(2026, time.October, 21, 18, 59, 30, 0, time.UTC)
	if got := displayPage(t, s, dev)["refresh_s"].(float64); got != 60 {
		t.Fatalf("the edge must keep its sleep: got %v", got)
	}
}
