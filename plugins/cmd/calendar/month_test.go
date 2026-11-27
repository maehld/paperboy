package main

import (
	"image"
	"reflect"
	"testing"
)

func TestMonthTitles(t *testing.T) {
	events := []monthEvent{{title: "Standup"}, {title: "Kino"}, {title: "Sport"}}
	// One line: a title, never only the count.
	if got, hidden := monthTitles(events, 1, true); !reflect.DeepEqual(got, events[:1]) || hidden != 0 {
		t.Errorf("one line = %v, %d", got, hidden)
	}
	// Two lines: one title and the count behind it.
	if got, hidden := monthTitles(events, 2, true); !reflect.DeepEqual(got, events[:1]) || hidden != 2 {
		t.Errorf("two lines = %v, %d", got, hidden)
	}
	// Without the count the free line carries another title.
	if got, hidden := monthTitles(events, 2, false); !reflect.DeepEqual(got, events[:2]) || hidden != 0 {
		t.Errorf("more off = %v, %d", got, hidden)
	}
	// Everything fits: no count.
	if got, hidden := monthTitles(events[:2], 2, true); !reflect.DeepEqual(got, events[:2]) || hidden != 0 {
		t.Errorf("all fit = %v, %d", got, hidden)
	}
	if got, hidden := monthTitles(nil, 2, true); len(got) != 0 || hidden != 0 {
		t.Errorf("no titles = %v, %d", got, hidden)
	}
}

func TestMonthBlocks(t *testing.T) {
	cell := image.Rect(10, 20, 50, 80)
	blocks := monthBlocks(cell, 3, 8, 4, 2)
	if len(blocks) != 3 {
		t.Fatalf("three events = %v", blocks)
	}
	// One row at the bottom, left to right.
	want := []image.Rectangle{
		image.Rect(13, 73, 21, 77),
		image.Rect(23, 73, 31, 77),
		image.Rect(33, 73, 41, 77),
	}
	if !reflect.DeepEqual(blocks, want) {
		t.Errorf("blocks = %v, want %v", blocks, want)
	}
	for _, b := range blocks {
		if !b.In(cell) {
			t.Errorf("block %v leaves the cell", b)
		}
	}
	// At most four blocks, and the width stays inside the cell.
	blocks = monthBlocks(cell, 6, 100, 4, 2)
	if len(blocks) != 4 {
		t.Errorf("six events = %v", blocks)
	}
	for _, b := range blocks {
		if b.Dx() != 7 || !b.In(cell) {
			t.Errorf("block %v does not fit", b)
		}
	}
	if got := monthBlocks(cell, 0, 8, 4, 2); len(got) != 0 {
		t.Errorf("no events = %v", got)
	}
}

// A month block is gray when its event has ended; an event that ends exactly
// now has ended, a zero-length event at now has not.
func TestMonthEventsMarkPast(t *testing.T) {
	p := agendaPlugin(t, ics(
		vevent("UID:a", "DTSTART;TZID=Europe/Berlin:20261103T070000", "DTEND;TZID=Europe/Berlin:20261103T080000", "SUMMARY:Früh"),
		vevent("UID:b", "DTSTART;TZID=Europe/Berlin:20261103T150000", "DTEND;TZID=Europe/Berlin:20261103T160000", "SUMMARY:Spät"),
		vevent("UID:c", "DTSTART;VALUE=DATE:20261102", "DTEND;VALUE=DATE:20261103", "SUMMARY:Gestern"),
	))
	byDay := p.eventsByDay(p.events, midnight(p.now.In(p.loc)).AddDate(0, 0, -1), 2)
	if len(byDay[0]) != 1 || byDay[0][0].title != "Gestern" || !byDay[0][0].past {
		t.Errorf("yesterday = %+v", byDay[0])
	}
	if len(byDay[1]) != 2 || byDay[1][0].title != "Früh" || !byDay[1][0].past || byDay[1][1].title != "Spät" || byDay[1][1].past {
		t.Errorf("today = %+v", byDay[1])
	}
	now := p.now.In(p.loc)
	if ended(parsedEvent{start: now, end: now}, now) {
		t.Error("zero-length event at now counts as ended")
	}
}
