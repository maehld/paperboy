package main

import (
	"reflect"
	"testing"
)

func TestMonthTitles(t *testing.T) {
	titles := []string{"Standup", "Kino", "Sport"}
	// One line: a title, never only the count.
	if got, more := monthTitles(titles, 1); !reflect.DeepEqual(got, titles[:1]) || more != 0 {
		t.Errorf("one line = %q, %d", got, more)
	}
	// Two lines: one title and the count behind it.
	if got, more := monthTitles(titles, 2); !reflect.DeepEqual(got, titles[:1]) || more != 2 {
		t.Errorf("two lines = %q, %d", got, more)
	}
	// Everything fits: no count.
	if got, more := monthTitles(titles[:2], 2); !reflect.DeepEqual(got, titles[:2]) || more != 0 {
		t.Errorf("all fit = %q, %d", got, more)
	}
	if got, more := monthTitles(nil, 2); len(got) != 0 || more != 0 {
		t.Errorf("no titles = %q, %d", got, more)
	}
}
