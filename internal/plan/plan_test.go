package plan

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

const samplePlan = `
rules:
  - name: morgen
    when: { from: "07:00", to: "09:00", days: [mo, di, mi, do, fr] }
    pages: [kalender-wetter, solar-overview]
  - name: tag
    when: { from: "09:00", to: "19:00" }
    pages: [solar-gross, solar-battery, solar-history]
    on_refresh: cycle
    refresh: 5m
  - name: nacht
    when: { from: "19:00", to: "07:00" }
    pages: [tueren]
    refresh: 30m
fallback: nacht
`

// berlin is the timezone used by the tests; plans get it from the
// configuration (ADR 0033).
func berlin(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func parse(t *testing.T, yaml string) *Plan {
	t.Helper()
	p, err := Parse(strings.NewReader(yaml), berlin(t))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func at(t *testing.T, year int, month time.Month, day, hour, minute int) time.Time {
	t.Helper()
	return time.Date(year, month, day, hour, minute, 0, 0, berlin(t))
}

func TestParse(t *testing.T) {
	p := parse(t, samplePlan)
	if len(p.Rules) != 3 || p.Fallback != "nacht" {
		t.Fatalf("plan: %+v", p)
	}
	tag := p.Rules[1]
	if tag.Mode() != RefreshCycle || time.Duration(tag.Refresh) != 5*time.Minute {
		t.Fatalf("tag: %+v", tag)
	}
	// German and English weekday names are both accepted.
	english := parse(t, `
rules:
  - name: weekend
    when: { days: [sat, sun] }
    pages: [wochenende]
`)
	if got := english.Rules[0].When.Days; len(got) != 2 || time.Weekday(got[0]) != time.Saturday || time.Weekday(got[1]) != time.Sunday {
		t.Fatalf("days: %v", got)
	}
}

// TestRefreshModes reads the three modes and defaults to current. The old
// rotate key still means cycle (ADR 0130).
func TestRefreshModes(t *testing.T) {
	p := parse(t, `
rules:
  - name: cycle
    pages: [a]
    on_refresh: cycle
  - name: first
    pages: [a]
    on_refresh: first
  - name: current
    pages: [a]
    on_refresh: current
  - name: default
    pages: [a]
  - name: old
    pages: [a]
    rotate: true
`)
	want := []RefreshMode{RefreshCycle, RefreshFirst, RefreshCurrent, RefreshCurrent, RefreshCycle}
	for i, mode := range want {
		if got := p.Rules[i].Mode(); got != mode {
			t.Fatalf("rule %s: got %q, want %q", p.Rules[i].Name, got, mode)
		}
	}
	sel, ok := p.Select(at(t, 2026, time.October, 21, 12, 0), nil)
	if !ok || sel.OnRefresh != RefreshCycle {
		t.Fatalf("selection: %+v %v", sel, ok)
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"unknown field":      "rules: [{name: a, pages: [x]}]\nextra: 1",
		"rules missing":      "rules: []",
		"timezone in plan":   "timezone: Europe/Berlin\nrules: [{name: a, pages: [x]}]",
		"duplicate rule":     "rules: [{name: a, pages: [x]}, {name: a, pages: [y]}]",
		"bad rule name":      "rules: [{name: Morgen, pages: [x]}]",
		"pages missing":      "rules: [{name: a}]",
		"bad page name":      "rules: [{name: a, pages: [Solar]}]",
		"duplicate page":     "rules: [{name: a, pages: [x, x]}]",
		"refresh too short":  "rules: [{name: a, pages: [x], refresh: 500ms}]",
		"unknown refresh":    "rules: [{name: a, pages: [x], on_refresh: rotate}]",
		"rotate conflict":    "rules: [{name: a, pages: [x], rotate: true, on_refresh: first}]",
		"data condition":     "rules: [{name: a, pages: [x], when: {data: [{path: tuer, equals: offen}]}}]",
		"from without to":    "rules: [{name: a, pages: [x], when: {from: \"07:00\"}}]",
		"from equals to":     "rules: [{name: a, pages: [x], when: {from: \"07:00\", to: \"07:00\"}}]",
		"bad time":           "rules: [{name: a, pages: [x], when: {from: \"7:00\", to: \"09:00\"}}]",
		"time out of range":  "rules: [{name: a, pages: [x], when: {from: \"25:00\", to: \"09:00\"}}]",
		"unknown weekday":    "rules: [{name: a, pages: [x], when: {days: [funday]}}]",
		"unknown fallback":   "rules: [{name: a, pages: [x]}]\nfallback: b",
		"timezone not given": "rules: [{name: a, pages: [x]}]\ntimezone: 5",
	}
	for name, yaml := range cases {
		if _, err := Parse(strings.NewReader(yaml), berlin(t)); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

func TestPlanActions(t *testing.T) {
	p := parse(t, `
rules:
  - name: tag
    pages: [a]
  - name: nacht
    pages: [b]
fallback: nacht
actions:
  - action: evcc/mode
    args: { loadpoint: 0, mode: now }
    id: charge-now
    title: Charge now
  - action: evcc/mode
    args: { loadpoint: 1, mode: now }
    id: charge-now-2
  - paperboy/refresh
`)
	if len(p.Actions) != 3 {
		t.Fatalf("actions: %+v", p.Actions)
	}
	if got := p.Actions[0].Key(); got != "charge-now" {
		t.Fatalf("identity with id: %q", got)
	}
	if got := p.Actions[2].Key(); got != "paperboy/refresh" {
		t.Fatalf("identity without id: %q", got)
	}
	if got := fmt.Sprint(p.Actions[0].Args["loadpoint"]); got != "0" {
		t.Fatalf("args: %v", p.Actions[0].Args)
	}
	if p.Actions[0].Title != "Charge now" {
		t.Fatalf("title: %q", p.Actions[0].Title)
	}
	// Every selection carries the plan's actions, matching rule and fallback.
	for _, now := range []time.Time{
		at(t, 2026, time.October, 21, 12, 0),
		at(t, 2026, time.October, 21, 23, 0),
	} {
		sel, ok := p.Select(now, nil)
		if !ok || len(sel.Actions) != 3 || sel.Actions[1].ID != "charge-now-2" {
			t.Fatalf("selection at %s: %+v %v", now.Format(time.RFC3339), sel, ok)
		}
	}
}

func TestPlanActionErrors(t *testing.T) {
	cases := map[string]string{
		"actions on a rule":      "rules: [{name: a, pages: [x], actions: [{action: a/b}]}]",
		"action missing":         "rules: [{name: a, pages: [x]}]\nactions: [{id: a}]",
		"action without plugin":  "rules: [{name: a, pages: [x]}]\nactions: [{action: refresh}]",
		"action with bad name":   "rules: [{name: a, pages: [x]}]\nactions: [{action: evcc/Mode}]",
		"bad id":                 "rules: [{name: a, pages: [x]}]\nactions: [{action: a/b, id: Charge}]",
		"missing id":             "rules: [{name: a, pages: [x]}]\nactions: [{action: a/b, id: x}, {action: a/b}]",
		"duplicate id":           "rules: [{name: a, pages: [x]}]\nactions: [{action: a/b, id: x}, {action: c/d, id: x}]",
		"duplicate action":       "rules: [{name: a, pages: [x]}]\nactions: [{action: a/b, id: x}, {action: a/b, id: y}]",
		"duplicate action mixed": "rules: [{name: a, pages: [x]}]\nactions: [{action: a/b, id: x, args: {a: 1, b: 2}}, {action: a/b, id: y, args: {b: 2, a: 1}}]",
	}
	for name, yaml := range cases {
		if _, err := Parse(strings.NewReader(yaml), berlin(t)); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

func TestSelect(t *testing.T) {
	p := parse(t, samplePlan)
	cases := []struct {
		at   time.Time
		rule string
	}{
		{at(t, 2026, time.October, 21, 6, 59), "nacht"},  // before the morning window
		{at(t, 2026, time.October, 21, 7, 0), "morgen"},  // from is inclusive
		{at(t, 2026, time.October, 21, 8, 59), "morgen"}, // Wednesday
		{at(t, 2026, time.October, 21, 9, 0), "tag"},     // to is exclusive
		{at(t, 2026, time.October, 21, 18, 59), "tag"},
		{at(t, 2026, time.October, 21, 19, 0), "nacht"},
		{at(t, 2026, time.October, 25, 8, 0), "nacht"}, // Sunday: fallback
		{at(t, 2026, time.October, 25, 12, 0), "tag"},  // Sunday noon
	}
	for _, c := range cases {
		sel, ok := p.Select(c.at, nil)
		if !ok || sel.Rule != c.rule {
			t.Fatalf("%s: got %q (%v), want %q", c.at.Format(time.RFC3339), sel.Rule, ok, c.rule)
		}
	}
	if sel, _ := p.Select(at(t, 2026, time.October, 21, 12, 0), nil); len(sel.Pages) != 3 || sel.OnRefresh != RefreshCycle {
		t.Fatalf("tag selection: %+v", sel)
	}
}

func TestSelectAcrossMidnightAndDays(t *testing.T) {
	p := parse(t, `
rules:
  - name: freitagnacht
    when: { from: "22:00", to: "06:00", days: [fr] }
    pages: [nacht]
`)
	// Saturday 03:00 belongs to the window that started on Friday.
	if sel, ok := p.Select(at(t, 2026, time.October, 24, 3, 0), nil); !ok || sel.Rule != "freitagnacht" {
		t.Fatalf("Saturday night: %+v %v", sel, ok)
	}
	// Friday 03:00 belongs to Thursday's window.
	if _, ok := p.Select(at(t, 2026, time.October, 23, 3, 0), nil); ok {
		t.Fatal("Friday morning must not match")
	}
	// Saturday 07:00 is outside the window.
	if _, ok := p.Select(at(t, 2026, time.October, 24, 7, 0), nil); ok {
		t.Fatal("Saturday morning must not match")
	}
}

func TestSelectFirstMatchWins(t *testing.T) {
	p := parse(t, `
rules:
  - name: genau
    when: { from: "12:00", to: "12:30" }
    pages: [a]
  - name: immer
    pages: [b]
`)
	if sel, _ := p.Select(at(t, 2026, time.October, 21, 12, 10), nil); sel.Rule != "genau" {
		t.Fatalf("first rule must win: %+v", sel)
	}
	if sel, _ := p.Select(at(t, 2026, time.October, 21, 13, 0), nil); sel.Rule != "immer" {
		t.Fatalf("always rule: %+v", sel)
	}
}

func TestSelectFallback(t *testing.T) {
	// Without a fallback, a gap leaves no selection.
	p := parse(t, `
rules:
  - name: tag
    when: { from: "09:00", to: "19:00" }
    pages: [a]
`)
	if _, ok := p.Select(at(t, 2026, time.October, 21, 20, 0), nil); ok {
		t.Fatal("no rule must match outside the window")
	}
	// A fallback applies regardless of its own time window.
	p = parse(t, `
rules:
  - name: tag
    when: { from: "09:00", to: "19:00" }
    pages: [a]
  - name: notfall
    when: { from: "09:00", to: "10:00" }
    pages: [b]
fallback: notfall
`)
	if sel, ok := p.Select(at(t, 2026, time.October, 21, 20, 0), nil); !ok || sel.Rule != "notfall" {
		t.Fatalf("fallback selection: %+v %v", sel, ok)
	}
}

// TestSelectConditions checks that a rule with value conditions only matches
// while a value answers one of its keys, together with its time window
// (ADR 0127).
func TestSelectConditions(t *testing.T) {
	p := parse(t, `
rules:
  - name: unterzucker
    when: { conditions: [{value: dexcom/state, is: [low, urgent]}] }
    pages: [zucker]
  - name: tag
    when: { from: "07:00", to: "22:00" }
    pages: [uebersicht]
`)
	now := at(t, 2026, time.October, 21, 12, 0)
	if sel, ok := p.Select(now, Values{"dexcom/state": "low"}); !ok || sel.Rule != "unterzucker" {
		t.Fatalf("matching key: %+v %v", sel, ok)
	}
	if sel, ok := p.Select(now, Values{"dexcom/state": "urgent"}); !ok || sel.Rule != "unterzucker" {
		t.Fatalf("second matching key: %+v %v", sel, ok)
	}
	if sel, ok := p.Select(now, Values{"dexcom/state": "normal"}); !ok || sel.Rule != "tag" {
		t.Fatalf("other key: %+v %v", sel, ok)
	}
	// A missing value counts as no match, never as a warning page.
	if sel, ok := p.Select(now, nil); !ok || sel.Rule != "tag" {
		t.Fatalf("nil values: %+v %v", sel, ok)
	}
	if p.Matches(0, now, Values{"dexcom/state": "low"}) != true || p.Matches(0, now, nil) {
		t.Fatal("Matches must consider values")
	}

	// Conditions combine with the time window as AND.
	p = parse(t, `
rules:
  - name: nachts
    when: { from: "22:00", to: "06:00", conditions: [{value: dexcom/state, is: low}] }
    pages: [zucker]
`)
	if _, ok := p.Select(at(t, 2026, time.October, 21, 12, 0), Values{"dexcom/state": "low"}); ok {
		t.Fatal("conditions must not override the time window")
	}
	if _, ok := p.Select(at(t, 2026, time.October, 21, 23, 0), nil); ok {
		t.Fatal("a missing value must not match")
	}
	// A scalar is is one key; several entries combine as AND.
	p = parse(t, `
rules:
  - name: a
    when: { conditions: [{value: dexcom/state, is: low}, {value: weather/sky, is: [rain, snow]}] }
    pages: [x]
`)
	if _, ok := p.Select(now, Values{"dexcom/state": "low", "weather/sky": "rain"}); !ok {
		t.Fatal("both conditions must match")
	}
	if _, ok := p.Select(now, Values{"dexcom/state": "low", "weather/sky": "sun"}); ok {
		t.Fatal("an unmatched second condition must not match")
	}
}

func TestConditionErrors(t *testing.T) {
	cases := map[string]string{
		"data":            `rules: [{name: a, pages: [x], when: {data: [{path: a}]}}]`,
		"old string":      `rules: [{name: a, pages: [x], when: {conditions: [dexcom/low]}}]`,
		"missing plugin":  `rules: [{name: a, pages: [x], when: {conditions: [{value: low, is: a}]}}]`,
		"bad name":        `rules: [{name: a, pages: [x], when: {conditions: [{value: dexcom/Low, is: a}]}}]`,
		"missing key":     `rules: [{name: a, pages: [x], when: {conditions: [{value: dexcom/state}]}}]`,
		"empty key list":  `rules: [{name: a, pages: [x], when: {conditions: [{value: dexcom/state, is: []}]}}]`,
		"duplicate value": `rules: [{name: a, pages: [x], when: {conditions: [{value: dexcom/state, is: low}, {value: dexcom/state, is: high}]}}]`,
		"unknown field":   `rules: [{name: a, pages: [x], when: {conditions: [{value: dexcom/state, is: low, label: Low}]}}]`,
	}
	for name, yaml := range cases {
		if _, err := Parse(strings.NewReader(yaml), berlin(t)); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
	// The hints name the replacement.
	if _, err := Parse(strings.NewReader(cases["data"]), berlin(t)); err == nil || !strings.Contains(err.Error(), "when.conditions") {
		t.Fatalf("data hint: %v", err)
	}
	if _, err := Parse(strings.NewReader(cases["old string"]), berlin(t)); err == nil || !strings.Contains(err.Error(), "no longer a condition") {
		t.Fatalf("old condition hint: %v", err)
	}
}

func TestUntilNextBoundary(t *testing.T) {
	p := parse(t, samplePlan)
	cases := []struct {
		at   time.Time
		want time.Duration
	}{
		{at(t, 2026, time.October, 21, 6, 30), 30 * time.Minute},
		{at(t, 2026, time.October, 21, 8, 30), 30 * time.Minute},
		{at(t, 2026, time.October, 21, 9, 0), 10 * time.Hour}, // window ends at 09:00, next is 19:00
		{at(t, 2026, time.October, 21, 12, 0), 7 * time.Hour},
		{at(t, 2026, time.October, 21, 19, 10), 11*time.Hour + 50*time.Minute},
	}
	for _, c := range cases {
		if got := p.UntilNextBoundary(c.at); got != c.want {
			t.Fatalf("%s: got %v, want %v", c.at.Format(time.RFC3339), got, c.want)
		}
	}
}

func TestUntilNextBoundaryDays(t *testing.T) {
	p := parse(t, `
rules:
  - name: montag
    when: { from: "07:00", to: "09:00", days: [mo] }
    pages: [a]
  - name: garnicht
    pages: [b]
`)
	now := at(t, 2026, time.October, 21, 12, 0) // Wednesday
	want := at(t, 2026, time.October, 26, 7, 0).Sub(now)
	if got := p.UntilNextBoundary(now); got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := parse(t, "rules: [{name: a, pages: [x]}]").UntilNextBoundary(now); got != 0 {
		t.Fatalf("a rule without times has no boundary: %v", got)
	}
}

func TestUntilNextBoundaryDST(t *testing.T) {
	p := parse(t, `
rules:
  - name: tag
    when: { from: "07:00", to: "09:00" }
    pages: [a]
`)
	// Spring forward on 2026-03-29: 02:00 CET becomes 03:00 CEST.
	if got := p.UntilNextBoundary(at(t, 2026, time.March, 29, 1, 30)); got != 4*time.Hour+30*time.Minute {
		t.Fatalf("spring: got %v", got)
	}
	// Fall back on 2026-10-25: 03:00 CEST becomes 02:00 CET.
	if got := p.UntilNextBoundary(at(t, 2026, time.October, 25, 1, 30)); got != 6*time.Hour+30*time.Minute {
		t.Fatalf("autumn: got %v", got)
	}
}
