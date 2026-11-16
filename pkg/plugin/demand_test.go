package plugin

import (
	"context"
	"testing"
	"time"
)

type demandHandler struct{ Demand }

var _ Eventer = (*demandHandler)(nil)

func past() *time.Time {
	t := time.Now().Add(-time.Second)
	return &t
}

func future(d time.Duration) *time.Time {
	t := time.Now().Add(d)
	return &t
}

func TestDemandNoDemandBlocks(t *testing.T) {
	var d Demand
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() { done <- d.Wait(ctx, 0) }()
	select {
	case <-done:
		t.Fatal("Wait returned without demand")
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	if <-done {
		t.Fatal("Wait returned true after the context ended")
	}
}

func TestDemandPastReturns(t *testing.T) {
	var d Demand
	d.Event(Event{Event: EventSchedule, NextRefresh: past()})
	if !d.Wait(context.Background(), 0) {
		t.Fatal("Wait did not return for a past schedule")
	}
}

func TestDemandCleared(t *testing.T) {
	var d Demand
	d.Event(Event{Event: EventSchedule, NextRefresh: future(time.Hour)})
	d.Event(Event{Event: EventSchedule})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() { done <- d.Wait(ctx, 0) }()
	select {
	case <-done:
		t.Fatal("Wait returned after the demand was cleared")
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	if <-done {
		t.Fatal("Wait returned true after the context ended")
	}
}

func TestDemandWakesOnNewSchedule(t *testing.T) {
	var d Demand
	d.Event(Event{Event: EventSchedule, NextRefresh: future(time.Hour)})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan bool, 1)
	go func() { done <- d.Wait(ctx, 0) }()
	time.Sleep(20 * time.Millisecond)
	d.Event(Event{Event: EventSchedule, NextRefresh: past()})
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("Wait returned false")
		}
	case <-time.After(time.Second):
		t.Fatal("Wait did not wake on a new schedule")
	}
}

func TestDemandFloor(t *testing.T) {
	var d Demand
	ctx := context.Background()
	d.Event(Event{Event: EventSchedule, NextRefresh: past()})
	if !d.Wait(ctx, 0) {
		t.Fatal("first Wait failed")
	}
	d.Event(Event{Event: EventSchedule, NextRefresh: past()})
	start := time.Now()
	if !d.Wait(ctx, 80*time.Millisecond) {
		t.Fatal("second Wait failed")
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Fatalf("Wait ignored the floor: %s", elapsed)
	}
}

func TestDemandLead(t *testing.T) {
	var d Demand
	start := time.Now()
	d.Event(Event{Event: EventSchedule, NextRefresh: future(DemandLead + 40*time.Millisecond)})
	if !d.Wait(context.Background(), 0) {
		t.Fatal("Wait failed")
	}
	elapsed := time.Since(start)
	if elapsed < 20*time.Millisecond || elapsed > time.Second {
		t.Fatalf("Wait did not use the lead: %s", elapsed)
	}
}

func TestDemandIgnoresOtherEvents(t *testing.T) {
	var d Demand
	d.Event(Event{Event: EventChanged})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() { done <- d.Wait(ctx, 0) }()
	select {
	case <-done:
		t.Fatal("an unrelated event created demand")
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	if <-done {
		t.Fatal("Wait returned true after the context ended")
	}
}

func TestDemandFloorStartsAtCall(t *testing.T) {
	var d Demand
	ctx := context.Background()
	d.Event(Event{Event: EventSchedule, NextRefresh: past()})
	if !d.Wait(ctx, 0) {
		t.Fatal("first Wait failed")
	}
	time.Sleep(60 * time.Millisecond) // a fetch between the two calls
	d.Event(Event{Event: EventSchedule, NextRefresh: past()})
	start := time.Now()
	if !d.Wait(ctx, 100*time.Millisecond) {
		t.Fatal("second Wait failed")
	}
	if elapsed := time.Since(start); elapsed < 80*time.Millisecond {
		t.Fatalf("the floor started at the previous return: %s", elapsed)
	}
}
