package plan

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestEncodeExamplesRoundTrip reads every example plan, writes it back and
// verifies that the result parses to the same plan (ADR 0066).
func TestEncodeExamplesRoundTrip(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "..", "examples", "plans", "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("example plans: %v (%d files)", err, len(files))
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			want, err := Parse(bytes.NewReader(raw), nil)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			data, err := Encode(want)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			got, err := Parse(bytes.NewReader(data), nil)
			if err != nil {
				t.Fatalf("reparse: %v\n%s", err, data)
			}
			if !reflect.DeepEqual(want.Pages, got.Pages) ||
				!reflect.DeepEqual(want.Refresh, got.Refresh) ||
				!reflect.DeepEqual(want.Triggers, got.Triggers) ||
				want.Mode() != got.Mode() ||
				!reflect.DeepEqual(want.Actions, got.Actions) {
				t.Fatalf("round trip changed the plan\nwant %+v\ngot  %+v\nyaml:\n%s", want, got, data)
			}
		})
	}
}

// TestEncodeKeepsPlanLayout verifies the readable layout of the sample plan.
func TestEncodeKeepsPlanLayout(t *testing.T) {
	p := parse(t, samplePlan)
	data, err := Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	for _, want := range []string{
		"pages:\n  - energie\n  - prusa\n  - wetter\n  - {page: zucker, hidden: true}\n",
		"on_refresh: cycle\n",
		"refresh:\n  default: 1h\n",
		`    - {from: "08:00", to: "16:00", interval: 10m}`,
		"triggers:\n  - when: [{value: dexcom/state, is: [low, urgent]}]\n    page: zucker\n    alert: refresh\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// TestEncodeWritesAlert verifies that the mode reaches the file, that a quiet
// trigger writes no key and that the editor API round-trips it (ADR 0131).
func TestEncodeWritesAlert(t *testing.T) {
	p := parse(t, `
pages: [x]
triggers:
  - when: {value: a/b, is: ok}
    page: x
    alert: refresh
  - when: {value: a/b, is: no}
    page: x
`)
	data, err := Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "alert: refresh\n") || strings.Count(string(data), "alert:") != 1 {
		t.Fatalf("layout:\n%s", data)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"alert":"refresh"`) {
		t.Fatalf("json: %s", raw)
	}
	var got Plan
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Triggers[0].Alert != AlertRefresh || got.Triggers[1].Alert != AlertNever {
		t.Fatalf("round trip: %+v", got.Triggers)
	}
}

// TestEncodeRejectsInvalidPlan verifies that invalid plans never reach a file.
func TestEncodeRejectsInvalidPlan(t *testing.T) {
	for name, p := range map[string]*Plan{
		"no pages":         {},
		"all hidden":       {Pages: []Page{{Name: "home", Hidden: true}}},
		"bad name":         {Pages: []Page{{Name: "Bad"}}},
		"duplicate page":   {Pages: []Page{{Name: "home"}, {Name: "home"}}},
		"bad refresh mode": {Pages: []Page{{Name: "home"}}, OnRefresh: "rotate"},
		"bad alert mode": {Pages: []Page{{Name: "home"}},
			Triggers: []Trigger{{When: Conditions{{Value: "a/b", Is: []string{"ok"}}}, Page: "home", Alert: "sometimes"}}},
		"missing trigger page": {Pages: []Page{{Name: "home"}},
			Triggers: []Trigger{{When: Conditions{{Value: "a/b", Is: []string{"ok"}}}}}},
		"overlapping windows": {Pages: []Page{{Name: "home"}}, Refresh: Refresh{Windows: []Window{
			{From: Clock(8 * 60), To: Clock(16 * 60), Interval: Duration(time.Minute)},
			{From: Clock(12 * 60), To: Clock(18 * 60), Interval: Duration(time.Minute)},
		}}},
	} {
		if _, err := Encode(p); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

// TestEncodeConditions verifies the written form and the JSON model of trigger
// conditions (ADR 0127): one key as a scalar, several as a list.
func TestEncodeConditions(t *testing.T) {
	p := parse(t, `
pages: [zucker]
triggers:
  - when: [{value: dexcom/state, is: [low, urgent]}, {value: weather/sky, is: rain}]
    page: zucker
`)
	data, err := Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `{value: dexcom/state, is: [low, urgent]}`) ||
		!strings.Contains(string(data), `{value: weather/sky, is: rain}`) {
		t.Fatalf("layout:\n%s", data)
	}
	again, err := Parse(bytes.NewReader(data), nil)
	if err != nil {
		t.Fatalf("reparse: %v\n%s", err, data)
	}
	if !reflect.DeepEqual(p.Triggers, again.Triggers) {
		t.Fatalf("round trip changed the plan\nwant %+v\ngot  %+v", p.Triggers, again.Triggers)
	}
	j, err := json.Marshal(p)
	if err != nil || !strings.Contains(string(j), `"when":[{"value":"dexcom/state","is":["low","urgent"]},{"value":"weather/sky","is":"rain"}]`) {
		t.Fatalf("json: %s %v", j, err)
	}
	// The editor API accepts one condition as an object and several as a list.
	var back Plan
	if err := json.Unmarshal([]byte(`{"pages":["x"],"triggers":[{"page":"x","when":{"value":"d/s","is":"low"}},{"page":"x","when":[{"value":"d/s","is":"low"},{"value":"w/s","is":["rain","snow"]}]}]}`), &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Triggers[0].When) != 1 || len(back.Triggers[1].When) != 2 || len(back.Triggers[1].When[1].Is) != 2 {
		t.Fatalf("json conditions: %+v", back.Triggers)
	}
}

// TestPlanJSONRoundTrip verifies the JSON model the editor API uses: clocks keep
// their written form, a page with and without hidden keeps its shape, and the
// result encodes to the same plan file.
func TestPlanJSONRoundTrip(t *testing.T) {
	p := parse(t, samplePlan)
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"pages":["energie","prusa","wetter",{"page":"zucker","hidden":true}]`,
		`"from":"08:00"`,
		`"default":"1h"`,
		`"on_refresh":"cycle"`,
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %s in %s", want, data)
		}
	}
	var got Plan
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Pages, got.Pages) || !reflect.DeepEqual(p.Triggers, got.Triggers) ||
		!reflect.DeepEqual(p.Refresh, got.Refresh) || p.Mode() != got.Mode() {
		t.Fatalf("JSON round trip changed the plan\nwant %+v\ngot  %+v", p, got)
	}
	yamlData, err := Encode(&got)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Parse(bytes.NewReader(yamlData), nil)
	if err != nil {
		t.Fatalf("reparse: %v\n%s", err, yamlData)
	}
	if !reflect.DeepEqual(p.Pages, again.Pages) || !reflect.DeepEqual(p.Triggers, again.Triggers) {
		t.Fatalf("plan changed\nwant %+v\ngot  %+v", p, again)
	}
}

// TestRefreshJSONAcceptsBothForms verifies the scalar and the map form the
// editor may send.
func TestRefreshJSONAcceptsBothForms(t *testing.T) {
	var r Refresh
	if err := json.Unmarshal([]byte(`"10m"`), &r); err != nil || r.Default != Duration(10*time.Minute) || len(r.Windows) != 0 {
		t.Fatalf("scalar: %+v %v", r, err)
	}
	if err := json.Unmarshal([]byte(`{"default":"1h","windows":[{"from":"08:00","to":"16:00","interval":"10m"}]}`), &r); err != nil {
		t.Fatal(err)
	}
	if r.Default != Duration(time.Hour) || len(r.Windows) != 1 || r.Windows[0].Interval != Duration(10*time.Minute) {
		t.Fatalf("map: %+v", r)
	}
	if err := json.Unmarshal([]byte(`{"default":"1h","x":1}`), &r); err == nil {
		t.Fatal("unknown key accepted")
	}
}
