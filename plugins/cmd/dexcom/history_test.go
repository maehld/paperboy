package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
)

// historySeries builds readings every five minutes ending at now.
func historySeries(now time.Time, days int) []reading {
	n := days * 288
	out := make([]reading, 0, n)
	for i := 0; i < n; i++ {
		at := now.Add(-time.Duration(n-1-i) * 5 * time.Minute)
		out = append(out, reading{at: at, value: 100 + float64(i%20), trend: "Flat"})
	}
	return out
}

// The compact history keeps every reading, its minute and its value.
func TestHistoryRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Minute)
	rs := historySeries(now, 3)
	hist := toHistory(rs)
	if len(hist) < 3*24 || len(hist) > 3*24+2 {
		t.Fatalf("%d hours stored, want about %d", len(hist), 3*24)
	}
	got := fromHistory(hist)
	if len(got) != len(rs) {
		t.Fatalf("%d readings restored, want %d", len(got), len(rs))
	}
	for i := range rs {
		if !got[i].at.Equal(rs[i].at) {
			t.Fatalf("reading %d at %v, want %v", i, got[i].at, rs[i].at)
		}
		if got[i].value != rs[i].value {
			t.Fatalf("reading %d = %g, want %g", i, got[i].value, rs[i].value)
		}
	}
}

// The minute of the history is the UTC minute, so an account in a half-hour
// zone keeps its instant.
func TestHistoryMinuteIsUTC(t *testing.T) {
	zone := time.FixedZone("", 5*3600+30*60)
	at := time.Date(2026, 11, 27, 17, 37, 0, 0, zone) // 12:07 UTC
	hist := toHistory([]reading{{at: at, value: 124}})
	if len(hist) != 1 || len(hist[0].Minutes) != 1 || hist[0].Minutes[0] != 7 {
		t.Fatalf("history minute = %+v, want 7", hist)
	}
	got := fromHistory(hist)
	if len(got) != 1 || !got[0].at.Equal(at) {
		t.Fatalf("restored %v, want %v", got, at)
	}
}

// A state of five weeks of readings stays below the 256 KiB limit.
func TestStateHistorySize(t *testing.T) {
	now := time.Now().Truncate(time.Minute)
	d := &dexcom{loaded: true, dirty: true}
	d.readings = historySeries(now, retentionDays)
	state := d.State()
	if len(state) == 0 {
		t.Fatal("no state")
	}
	if len(state) > 256<<10 {
		t.Fatalf("the state has %d bytes, more than the 256 KiB limit", len(state))
	}
	t.Logf("the state of %d days has %d bytes", retentionDays, len(state))
	var s storedState
	if err := json.Unmarshal(state, &s); err != nil {
		t.Fatal(err)
	}
	if s.Version != 2 || len(s.History) == 0 {
		t.Fatalf("state = version %d, %d hours", s.Version, len(s.History))
	}
	if len(s.History) > retentionDays*24+2 {
		t.Fatalf("%d hours stored, want about %d", len(s.History), retentionDays*24)
	}
}

// merge drops what is older than the retained window and keeps the rest.
func TestRetention(t *testing.T) {
	now := time.Now()
	kept := reading{at: now.Add(-retention + time.Hour), value: 110}
	old := reading{at: now.Add(-retention - time.Hour), value: 90}
	fresh := reading{at: now.Add(-time.Minute), value: 120}
	got := merge([]reading{old, kept}, []reading{fresh}, now)
	if len(got) != 2 || !got[0].at.Equal(kept.at) || !got[1].at.Equal(fresh.at) {
		t.Fatalf("merge = %+v, want the kept and the fresh reading", got)
	}
}

// The day values stay on 24 hours although the state keeps more.
func TestDayValuesStay24h(t *testing.T) {
	now := time.Now().Truncate(time.Minute)
	cut := now.Add(-24 * time.Hour)
	d := &dexcom{unit: unitMGDL, set: map[string]float64{}}
	for i := 0; i < 7*288; i++ {
		at := now.Add(-time.Duration(7*288-1-i) * 5 * time.Minute)
		value := 300.0
		if !at.Before(cut) {
			value = 100
		}
		d.readings = append(d.readings, reading{at: at, value: value, trend: "Flat"})
	}
	values, err := d.Values(&plugin.Request{Now: now, Language: "de",
		Values: []string{"avg", "in-band", "count", "min", "max"}})
	if err != nil {
		t.Fatal(err)
	}
	if v := values["avg"]; v.Number == nil || *v.Number != 100 {
		t.Errorf("avg = %+v, want the 24-hour average", v)
	}
	if v := values["count"]; v.Number == nil || *v.Number != 289 {
		t.Errorf("count = %+v, want the 289 readings of 24 hours", v)
	}
	if v := values["in-band"]; v.Number == nil || *v.Number != 100 {
		t.Errorf("in-band = %+v, want 100 %%", v)
	}
	if v := values["min"]; v.Number == nil || *v.Number != 100 {
		t.Errorf("min = %+v, want 100", v)
	}
}

// A version 1 state has no history. Its readings start one and the next state
// is version 2.
func TestLoadStateV1Migrates(t *testing.T) {
	now := time.Now().Truncate(time.Minute)
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = time.Now })
	d := &dexcom{accountName: "someone@example.com", region: "eu"}
	old := storedState{
		Version:   1,
		Region:    "eu",
		Account:   "someone@example.com",
		AccountID: "account",
		Session:   "session",
		FetchedAt: now,
		Readings:  toStored(historySeries(now, 1)),
	}
	raw, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.LoadState(raw, nil); err != nil {
		t.Fatal(err)
	}
	if len(d.readings) == 0 || !d.dirty {
		t.Fatalf("migration restored %d readings, dirty %v", len(d.readings), d.dirty)
	}
	state := d.State()
	var s storedState
	if err := json.Unmarshal(state, &s); err != nil {
		t.Fatal(err)
	}
	if s.Version != 2 || len(s.History) == 0 {
		t.Fatalf("state after the migration = version %d, %d hours", s.Version, len(s.History))
	}
}

// A version 2 state restores the verbose readings of 24 hours with their
// trends and the longer history.
func TestLoadStateKeepsHistory(t *testing.T) {
	now := time.Now().Truncate(time.Minute)
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = time.Now })
	rs := historySeries(now, 7)
	for i := range rs {
		rs[i].trend = ""
	}
	rs[len(rs)-1].trend = "FortyFiveUp"
	d := &dexcom{accountName: "someone@example.com", region: "eu"}
	saved := storedState{
		Version:   2,
		Region:    "eu",
		Account:   "someone@example.com",
		AccountID: "account",
		Session:   "session",
		FetchedAt: now,
		Readings:  toStored(rs),
		History:   toHistory(rs),
	}
	raw, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.LoadState(raw, nil); err != nil {
		t.Fatal(err)
	}
	if len(d.readings) != len(rs) {
		t.Fatalf("%d readings restored, want %d", len(d.readings), len(rs))
	}
	last := d.readings[len(d.readings)-1]
	if last.trend != "FortyFiveUp" {
		t.Errorf("newest trend = %q, want the trend of the verbose reading", last.trend)
	}
}
