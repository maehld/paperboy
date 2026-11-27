package main

import (
	"image"
	"reflect"
	"testing"
)

func TestMonthTitles(t *testing.T) {
	titles := []string{"Standup", "Kino", "Sport"}
	// One line: a title, never only the count.
	if got, hidden := monthTitles(titles, 1, true); !reflect.DeepEqual(got, titles[:1]) || hidden != 0 {
		t.Errorf("one line = %q, %d", got, hidden)
	}
	// Two lines: one title and the count behind it.
	if got, hidden := monthTitles(titles, 2, true); !reflect.DeepEqual(got, titles[:1]) || hidden != 2 {
		t.Errorf("two lines = %q, %d", got, hidden)
	}
	// Without the count the free line carries another title.
	if got, hidden := monthTitles(titles, 2, false); !reflect.DeepEqual(got, titles[:2]) || hidden != 0 {
		t.Errorf("more off = %q, %d", got, hidden)
	}
	// Everything fits: no count.
	if got, hidden := monthTitles(titles[:2], 2, true); !reflect.DeepEqual(got, titles[:2]) || hidden != 0 {
		t.Errorf("all fit = %q, %d", got, hidden)
	}
	if got, hidden := monthTitles(nil, 2, true); len(got) != 0 || hidden != 0 {
		t.Errorf("no titles = %q, %d", got, hidden)
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
