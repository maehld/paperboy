package main

import (
	"reflect"
	"testing"

	"github.com/maehld/paperboy/sdk/plugin"
)

func TestClothingAdvice(t *testing.T) {
	tests := []struct {
		name string
		t    today
		want clothingAdvice
	}{
		{
			name: "warm and dry",
			t:    today{ApparentMax: 26, ApparentMin: 18, Code: 0},
			want: clothingAdvice{Top: topTShirt, Legs: legsShorts, Outer: outerNone},
		},
		{
			name: "shorts need 24 degrees",
			t:    today{ApparentMax: 24, ApparentMin: 12},
			want: clothingAdvice{Top: topLongSleeve, Legs: legsShorts, Outer: outerLightJacket},
		},
		{
			name: "long trousers below 24 degrees",
			t:    today{ApparentMax: 23.9, ApparentMin: 12},
			want: clothingAdvice{Top: topLongSleeve, Legs: legsLong, Outer: outerLightJacket},
		},
		{
			name: "light jacket",
			t:    today{ApparentMax: 20, ApparentMin: 10},
			want: clothingAdvice{Top: topLongSleeve, Legs: legsLong, Outer: outerLightJacket},
		},
		{
			name: "jacket and scarf",
			t:    today{ApparentMax: 15, ApparentMin: 6},
			want: clothingAdvice{Top: topPullover, Legs: legsLong, Outer: outerJacket, Scarf: true},
		},
		{
			name: "pullover and jacket, a hat below 5 degrees",
			t:    today{ApparentMax: 15, ApparentMin: 4},
			want: clothingAdvice{Top: topPullover, Legs: legsLong, Outer: outerJacket, Scarf: true, Hat: true},
		},
		{
			name: "cold day",
			t:    today{ApparentMax: 5, ApparentMin: -2},
			want: clothingAdvice{Top: topPullover, Legs: legsLong, Outer: outerCoat, Thermals: true,
				Scarf: true, Hat: true, Gloves: true},
		},
		{
			name: "rain replaces the light jacket",
			t:    today{ApparentMax: 15, ApparentMin: 10, PrecipProb: ptr(60)},
			want: clothingAdvice{Top: topPullover, Legs: legsLong, Outer: outerRainJacket, Rain: true},
		},
		{
			name: "one millimetre is enough",
			t:    today{ApparentMax: 15, ApparentMin: 10, Precip: 1},
			want: clothingAdvice{Top: topPullover, Legs: legsLong, Outer: outerRainJacket, Rain: true},
		},
		{
			name: "rain over a winter coat",
			t:    today{ApparentMax: 5, ApparentMin: 0, Precip: 2},
			want: clothingAdvice{Top: topPullover, Legs: legsLong, Outer: outerCoat, Thermals: true,
				Rain: true, Scarf: true, Hat: true},
		},
		{
			name: "snow and a gale",
			t:    today{ApparentMax: 12, ApparentMin: 4, Snow: 1.2, Gusts: 8},
			want: clothingAdvice{Top: topPullover, Legs: legsLong, Outer: outerJacket,
				Snow: true, Wind: true, Gale: true, Scarf: true, Hat: true},
		},
		{
			name: "sun protection only on a clear day",
			t:    today{ApparentMax: 29, ApparentMin: 19, Code: 0},
			want: clothingAdvice{Top: topTShirt, Legs: legsShorts, Outer: outerNone, Heat: true},
		},
		{
			name: "no sun protection under clouds",
			t:    today{ApparentMax: 29, ApparentMin: 19, Code: 3},
			want: clothingAdvice{Top: topTShirt, Legs: legsShorts, Outer: outerNone},
		},
	}
	for _, tt := range tests {
		if got := clothingOf(tt.t); got != tt.want {
			t.Errorf("%s: clothingOf() = %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

func TestClothingDetails(t *testing.T) {
	w := &weather{req: &plugin.Request{Language: "de"}}
	a := clothingAdvice{Outer: outerCoat, Thermals: true, Rain: true, Snow: true,
		Wind: true, Gale: true, Heat: true}
	want := []string{"Regenjacke", "wasserdichte Schuhe", "kein Schirm", "Thermowäsche", "Sonnenschutz"}
	if got := w.clothingDetails(a); !reflect.DeepEqual(got, want) {
		t.Fatalf("clothingDetails() = %q, want %q", got, want)
	}
	a = clothingAdvice{Outer: outerJacket, Wind: true}
	if got := w.clothingDetails(a); !reflect.DeepEqual(got, []string{"winddicht"}) {
		t.Fatalf("clothingDetails() = %q, want windproof", got)
	}
}

func TestOutfitIcons(t *testing.T) {
	tests := []struct {
		name string
		a    clothingAdvice
		want []string
	}{
		{
			name: "warm",
			a:    clothingAdvice{Top: topTShirt, Legs: legsShorts},
			want: []string{"tshirt", "shorts"},
		},
		{
			name: "long sleeves and trousers",
			a:    clothingAdvice{Top: topLongSleeve, Legs: legsLong},
			want: []string{"pullover", "trousers"},
		},
		{
			name: "cold with accessories",
			a: clothingAdvice{Top: topPullover, Legs: legsLong, Outer: outerCoat,
				Scarf: true, Hat: true, Gloves: true},
			want: []string{"pullover", "trousers", "jacket", "scarf", "hat", "gloves"},
		},
	}
	for _, tt := range tests {
		if got := outfitIcons(tt.a); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: outfitIcons() = %q, want %q", tt.name, got, tt.want)
		}
	}
}
