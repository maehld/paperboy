package main

import (
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
