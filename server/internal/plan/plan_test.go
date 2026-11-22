package plan

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const samplePlan = `
pages:
  - energie
  - prusa
  - wetter
  - { page: zucker, hidden: true }

on_refresh: cycle
refresh:
  default: 1h
  windows:
    - { from: "08:00", to: "16:00", interval: 10m }
    - { from: "22:00", to: "06:00", interval: 30m }

triggers:
  - when: { value: dexcom/state, is: [low, urgent] }
    page: zucker
    alert: refresh
  - when:
      - { value: prusalink/state, is: printing }
      - { value: weather/temperature, is: warm }
    page: prusa
    alert: once
  - when: [{ value: time, from: "06:00", to: "09:00" }]
    page: wetter
  - when: [{ value: date, from: "2026-12-24" }]
    page: wetter
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
	if len(p.Pages) != 4 || len(p.Triggers) != 4 {
		t.Fatalf("plan: %+v", p)
	}
	if p.Mode() != RefreshCycle {
		t.Fatalf("mode: %q", p.Mode())
	}
	if p.Pages[0].Hidden || p.Pages[1].Hidden || p.Pages[2].Hidden || !p.Pages[3].Hidden {
		t.Fatalf("hidden: %+v", p.Pages)
	}
	if p.Pages[3].Name != "zucker" {
		t.Fatalf("hidden page: %+v", p.Pages[3])
	}
	// One condition may be written as a mapping, several as a list.
	if len(p.Triggers[0].When) != 1 || p.Triggers[0].When[0].Value != "dexcom/state" {
		t.Fatalf("trigger 1: %+v", p.Triggers[0])
	}
	if len(p.Triggers[1].When) != 2 || len(p.Triggers[1].When[1].Is) != 1 {
		t.Fatalf("trigger 2: %+v", p.Triggers[1])
	}
	if len(p.Refresh.Windows) != 2 || p.Refresh.Windows[1].From != Clock(22*60) {
		t.Fatalf("windows: %+v", p.Refresh.Windows)
	}
	if len(p.Triggers[2].When) != 1 || p.Triggers[2].When[0].Kind() != ConditionTime {
		t.Fatalf("time condition: %+v", p.Triggers[2])
	}
	if len(p.Triggers[3].When) != 1 || p.Triggers[3].When[0].Kind() != ConditionDates ||
		p.Triggers[3].When[0].DateFrom == nil || *p.Triggers[3].When[0].DateFrom != "2026-12-24" {
		t.Fatalf("date condition: %+v", p.Triggers[3])
	}
}

// TestRefreshModes reads the three modes and defaults to current.
func TestRefreshModes(t *testing.T) {
	for _, tc := range []struct {
		key  string
		want RefreshMode
	}{
		{"", RefreshCurrent},
		{"on_refresh: current", RefreshCurrent},
		{"on_refresh: first", RefreshFirst},
		{"on_refresh: cycle", RefreshCycle},
	} {
		src := "pages: [a]\n" + tc.key
		if got := parse(t, src).Mode(); got != tc.want {
			t.Fatalf("%q: got %q, want %q", tc.key, got, tc.want)
		}
	}
}

// TestAlertModes reads the two alert modes and treats never and the missing key
// as quiet (ADR 0131).
func TestAlertModes(t *testing.T) {
	for _, tc := range []struct {
		key  string
		want AlertMode
	}{
		{"", AlertNever},
		{"alert: never", AlertNever},
		{"alert: once", AlertOnce},
		{"alert: refresh", AlertRefresh},
	} {
		p := parse(t, "pages: [a]\ntriggers:\n  - when: {value: x/y, is: ok}\n    page: a\n    "+tc.key+"\n")
		if got := p.Triggers[0].Alert; got != tc.want {
			t.Fatalf("%q: got %q, want %q", tc.key, got, tc.want)
		}
	}
	if err := parseErr(t, "pages: [a]\ntriggers:\n  - when: {value: x/y, is: ok}\n    page: a\n    alert: loud\n"); !strings.Contains(err, "once or refresh") {
		t.Fatalf("alert: %v", err)
	}
}

func TestInterval(t *testing.T) {
	p := parse(t, samplePlan)
	for _, tc := range []struct {
		when time.Time
		want time.Duration
	}{
		{at(t, 2026, time.October, 21, 8, 0), 10 * time.Minute},
		{at(t, 2026, time.October, 21, 15, 59), 10 * time.Minute},
		{at(t, 2026, time.October, 21, 16, 0), time.Hour},
		{at(t, 2026, time.October, 21, 21, 0), time.Hour},
		{at(t, 2026, time.October, 21, 22, 0), 30 * time.Minute},
		{at(t, 2026, time.October, 22, 5, 59), 30 * time.Minute},
		{at(t, 2026, time.October, 22, 6, 0), time.Hour},
	} {
		if got := p.Interval(tc.when); got != tc.want {
			t.Fatalf("%s: got %s, want %s", tc.when.Format("15:04"), got, tc.want)
		}
	}
	// A plan without a schedule leaves the device's refresh in place.
	if got := parse(t, "pages: [a]").Interval(at(t, 2026, time.October, 21, 12, 0)); got != 0 {
		t.Fatalf("no schedule: %s", got)
	}
}

func TestUntilNextChange(t *testing.T) {
	p := parse(t, samplePlan)
	for _, tc := range []struct {
		when time.Time
		want time.Duration
	}{
		{at(t, 2026, time.October, 21, 7, 50), 10 * time.Minute}, // to 08:00
		{at(t, 2026, time.October, 21, 8, 0), time.Hour},         // trigger ends at 09:00
		{at(t, 2026, time.October, 21, 23, 0), 7 * time.Hour},    // to 06:00
		{at(t, 2026, time.October, 22, 1, 0), 5 * time.Hour},     // to 06:00
		{at(t, 2026, time.October, 22, 7, 59), 1 * time.Minute},  // to 08:00
		{at(t, 2026, time.October, 21, 12, 0), 4 * time.Hour},    // to 16:00
		{at(t, 2026, time.October, 21, 21, 59), 1 * time.Minute}, // to 22:00
	} {
		if got := p.UntilNextChange(tc.when); got != tc.want {
			t.Fatalf("%s: got %s, want %s", tc.when.Format("Mon 15:04"), got, tc.want)
		}
	}
	if got := parse(t, "pages: [a]").UntilNextChange(at(t, 2026, time.October, 21, 12, 0)); got != 0 {
		t.Fatalf("no schedule: %s", got)
	}
	// A trigger window and a date also wake the device.
	triggers := parse(t, `
pages: [a]
triggers:
  - when: [{ value: time, from: "06:00", to: "09:00" }]
    page: a
  - when: [{ value: date, from: "2026-10-22" }]
    page: a
`)
	if got := triggers.UntilNextChange(at(t, 2026, time.October, 21, 5, 50)); got != 10*time.Minute {
		t.Fatalf("trigger start: %s", got)
	}
	if got := triggers.UntilNextChange(at(t, 2026, time.October, 21, 8, 50)); got != 10*time.Minute {
		t.Fatalf("trigger end: %s", got)
	}
	if got := triggers.UntilNextChange(at(t, 2026, time.October, 21, 20, 0)); got != 4*time.Hour {
		t.Fatalf("date start: %s", got)
	}
}

func TestEffective(t *testing.T) {
	p := parse(t, samplePlan)
	want := []string{"energie", "prusa", "wetter"}
	if got := p.Effective(""); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("without trigger: %v", got)
	}
	// The trigger page keeps its declared place.
	want = []string{"energie", "prusa", "wetter", "zucker"}
	if got := p.Effective("zucker"); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("with trigger: %v", got)
	}
	// A visible trigger page does not appear twice.
	want = []string{"energie", "prusa", "wetter"}
	if got := p.Effective("prusa"); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("visible trigger: %v", got)
	}
}

func TestTriggerMatches(t *testing.T) {
	p := parse(t, samplePlan)
	matching := func(values Values) []string {
		pages := []string{}
		for _, tr := range p.Matching(at(t, 2026, time.October, 21, 12, 0), values) {
			pages = append(pages, tr.Page)
		}
		return pages
	}
	if got := matching(Values{"dexcom/state": "low"}); len(got) != 1 || got[0] != "zucker" {
		t.Fatalf("low should match zucker: %v", got)
	}
	if got := matching(Values{"dexcom/state": "normal"}); len(got) != 0 {
		t.Fatalf("normal should not match: %v", got)
	}
	if got := matching(nil); len(got) != 0 {
		t.Fatalf("a missing value should not match: %v", got)
	}
	// Several conditions must all match; a missing one means no match.
	if got := matching(Values{"prusalink/state": "printing", "weather/temperature": "warm"}); len(got) != 1 || got[0] != "prusa" {
		t.Fatalf("both conditions should match: %v", got)
	}
	if got := matching(Values{"prusalink/state": "printing"}); len(got) != 0 {
		t.Fatalf("a missing condition should not match: %v", got)
	}
}

// TestTimeTrigger matches a window of the day, the weekday and both together.
func TestTimeTrigger(t *testing.T) {
	p := parse(t, `
pages: [a, b, c]
triggers:
  - when: [{ value: time, from: "06:00", to: "09:00" }]
    page: a
  - when: [{ value: time, from: "22:00", to: "06:00" }]
    page: b
  - when: [{ value: weekday, is: [wed] }]
    page: c
`)
	page := func(when time.Time) string {
		if trs := p.Matching(when, nil); len(trs) > 0 {
			return trs[0].Page
		}
		return ""
	}
	if got := page(at(t, 2026, time.October, 21, 7, 30)); got != "a" {
		t.Fatalf("wednesday morning: %q", got)
	}
	if got := page(at(t, 2026, time.October, 21, 9, 0)); got != "c" {
		t.Fatalf("the window ends at 09:00; wednesday remains: %q", got)
	}
	if got := page(at(t, 2026, time.October, 21, 23, 0)); got != "b" {
		t.Fatalf("wednesday night: %q", got)
	}
	if got := page(at(t, 2026, time.October, 22, 1, 0)); got != "b" {
		t.Fatalf("after midnight: %q", got)
	}
	if got := page(at(t, 2026, time.October, 22, 6, 0)); got != "a" {
		t.Fatalf("the night window ends and the morning window starts: %q", got)
	}
	if got := page(at(t, 2026, time.October, 22, 7, 30)); got != "a" {
		t.Fatalf("thursday morning: %q", got)
	}
	// The weekday is the local day now, also after midnight.
	if got := page(at(t, 2026, time.October, 22, 12, 0)); got != "" {
		t.Fatalf("thursday noon: %q", got)
	}
	// Both together: Wednesday morning only.
	both := parse(t, `
pages: [a]
triggers:
  - when:
      - { value: weekday, is: [wed] }
      - { value: time, from: "06:00", to: "09:00" }
    page: a
`)
	if len(both.Matching(at(t, 2026, time.October, 21, 7, 30), nil)) != 1 {
		t.Fatal("wednesday morning should match")
	}
	if len(both.Matching(at(t, 2026, time.October, 22, 7, 30), nil)) != 0 {
		t.Fatal("thursday morning should not match")
	}
}

// TestDateTrigger matches a local calendar day and nothing around it.
func TestDateTrigger(t *testing.T) {
	p := parse(t, `
pages: [a]
triggers:
  - when: [{ value: date, from: "2026-12-24", to: "2026-12-26" }]
    page: a
`)
	for _, tc := range []struct {
		when time.Time
		want bool
	}{
		{at(t, 2026, time.December, 24, 0, 0), true},
		{at(t, 2026, time.December, 24, 23, 59), true},
		{at(t, 2026, time.December, 25, 12, 0), true},
		{at(t, 2026, time.December, 26, 0, 0), false},
		{at(t, 2026, time.December, 23, 23, 59), false},
	} {
		if got := len(p.Matching(tc.when, nil)) == 1; got != tc.want {
			t.Fatalf("%s: matched=%v, want %v", tc.when.Format("2006-01-02 15:04"), got, tc.want)
		}
	}
	// Without to the window is one day.
	one := parse(t, "pages: [a]\ntriggers:\n  - when: [{ value: date, from: \"2026-12-24\" }]\n    page: a\n")
	if len(one.Matching(at(t, 2026, time.December, 24, 23, 0), nil)) != 1 {
		t.Fatal("the single day should match")
	}
	if len(one.Matching(at(t, 2026, time.December, 25, 0, 0), nil)) != 0 {
		t.Fatal("the next day should not match")
	}
}

func TestParseErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{"no pages", "on_refresh: cycle\n", "pages missing"},
		{"all hidden", "pages: [{page: a, hidden: true}]\n", "every page is hidden"},
		{"duplicate page", "pages: [a, a]\n", "duplicate page"},
		{"bad page name", "pages: [A]\n", "only a-z, 0-9 and -"},
		{"bad mode", "pages: [a]\non_refresh: rotate\n", "expected cycle, first or current"},
		{"overlap", "pages: [a]\nrefresh:\n  windows:\n    - {from: \"08:00\", to: \"16:00\", interval: 10m}\n    - {from: \"12:00\", to: \"18:00\", interval: 10m}\n", "overlap"},
		{"same edges", "pages: [a]\nrefresh:\n  windows:\n    - {from: \"08:00\", to: \"08:00\", interval: 10m}\n", "from and to must differ"},
		{"short interval", "pages: [a]\nrefresh:\n  windows:\n    - {from: \"08:00\", to: \"16:00\", interval: 500ms}\n", "at least 1s"},
		{"short default", "pages: [a]\nrefresh: 500ms\n", "at least 1s"},
		{"trigger page not in plan", "pages: [a]\ntriggers:\n  - when: {value: x/y, is: ok}\n    page: b\n", "is not in pages"},
		{"trigger page missing", "pages: [a]\ntriggers:\n  - when: {value: x/y, is: ok}\n", "page missing"},
		{"condition without keys", "pages: [a]\ntriggers:\n  - when: {value: x/y}\n    page: a\n", "is missing"},
		{"duplicate condition", "pages: [a]\ntriggers:\n  - when: [{value: x/y, is: ok}, {value: x/y, is: no}]\n    page: a\n", "duplicate condition"},
		{"bad condition value", "pages: [a]\ntriggers:\n  - when: {value: xy, is: ok}\n    page: a\n", "expected plugin/value"},
		{"trigger without when", "pages: [a]\ntriggers:\n  - page: a\n", "when missing"},
		{"empty condition", "pages: [a]\ntriggers:\n  - when: [{}]\n    page: a\n", "expected plugin/value"},
		{"half time", "pages: [a]\ntriggers:\n  - when: [{ value: time, from: \"06:00\" }]\n    page: a\n", "from and to must be given together"},
		{"equal edges", "pages: [a]\ntriggers:\n  - when: [{ value: time, from: \"06:00\", to: \"06:00\" }]\n    page: a\n", "from and to must differ"},
		{"bad date", "pages: [a]\ntriggers:\n  - when: [{ value: date, from: 2026-13-01 }]\n    page: a\n", "expected a date as"},
		{"date order", "pages: [a]\ntriggers:\n  - when: [{ value: date, from: \"2026-12-26\", to: \"2026-12-24\" }]\n    page: a\n", "must be in order"},
		{"mixed condition", "pages: [a]\ntriggers:\n  - when: [{ value: time, from: \"06:00\", to: \"09:00\", is: [x] }]\n    page: a\n", "takes from and to, not is"},
		{"old rules", "rules: [{name: a, pages: [a]}]\n", "rules and fallback are gone"},
		{"old fallback", "pages: [a]\nfallback: a\n", "rules and fallback are gone"},
		{"unknown key", "pages: [a]\nfoo: 1\n", "field foo not found"},
		{"unknown page key", "pages: [{page: a, hidden: true, x: 1}]\n", "field x not found"},
		{"unknown refresh key", "pages: [a]\nrefresh: {default: 1h, x: 1}\n", "field x not found"},
		{"second document", "pages: [a]\n---\npages: [b]\n", "only one YAML document"},
	} {
		if err := parseErr(t, tc.src); !strings.Contains(err, tc.want) {
			t.Fatalf("%s: got %q, want %q", tc.name, err, tc.want)
		}
	}
}

func parseErr(t *testing.T, yaml string) string {
	t.Helper()
	_, err := Parse(strings.NewReader(yaml), berlin(t))
	if err == nil {
		t.Fatalf("expected an error for %q", yaml)
	}
	return err.Error()
}

func TestJSONRoundTrip(t *testing.T) {
	p := parse(t, samplePlan)
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var back Plan
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if got := back.Effective("zucker"); strings.Join(got, ",") != "energie,prusa,wetter,zucker" {
		t.Fatalf("pages: %v", got)
	}
	if back.Refresh.Windows[1].Interval != Duration(30*time.Minute) {
		t.Fatalf("windows: %+v", back.Refresh.Windows)
	}
	if len(back.Triggers) != 4 || back.Triggers[1].When[1].Value != "weather/temperature" {
		t.Fatalf("triggers: %+v", back.Triggers)
	}
	if back.Mode() != RefreshCycle {
		t.Fatalf("mode: %q", back.Mode())
	}
}
