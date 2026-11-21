package reading

import (
	"math"
	"testing"
)

func TestLookup(t *testing.T) {
	spec, ok := Lookup("temperature")
	if !ok || spec.Unit != "°C" || spec.Precision != 1 {
		t.Fatalf("temperature: %+v %v", spec, ok)
	}
	if _, ok := Lookup("pressure"); ok {
		t.Fatal("an undeclared reading must not be found")
	}
}

func TestValid(t *testing.T) {
	spec, _ := Lookup("humidity")
	for _, c := range []struct {
		value float64
		want  bool
	}{
		{0, true},
		{45.2, true},
		{100, true},
		{-1, false},
		{100.1, false},
		{math.NaN(), false},
		{math.Inf(1), false},
		{math.Inf(-1), false},
	} {
		if got := spec.Valid(c.value); got != c.want {
			t.Errorf("Valid(%v) = %v, want %v", c.value, got, c.want)
		}
	}
}

func TestFormat(t *testing.T) {
	temp, _ := Lookup("temperature")
	humidity, _ := Lookup("humidity")
	if got := temp.Format(21.53); got != "21.5 °C" {
		t.Errorf("temperature: %q", got)
	}
	if got := humidity.Format(45.2); got != "45 %" {
		t.Errorf("humidity: %q", got)
	}
}
