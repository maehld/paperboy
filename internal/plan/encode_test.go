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
	files, err := filepath.Glob(filepath.Join("..", "..", "examples", "plans", "*.yaml"))
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
			if !reflect.DeepEqual(want.Rules, got.Rules) || want.Fallback != got.Fallback ||
				!reflect.DeepEqual(want.Actions, got.Actions) {
				t.Fatalf("round trip changed the plan\nwant %+v\ngot  %+v\nyaml:\n%s", want, got, data)
			}
		})
	}
}

// TestEncodeKeepsPlanLayout verifies the readable layout of a rule.
func TestEncodeKeepsPlanLayout(t *testing.T) {
	p, err := Parse(strings.NewReader(examplePlan), nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	for _, want := range []string{
		"  - name: morning\n",
		`    when: {from: "06:00", to: "09:00", days: [mon, tue]}`,
		"    pages: [home, week]\n",
		"    rotate: true\n",
		"    refresh: 5m\n",
		"fallback: evening\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

const examplePlan = `
rules:
  - name: morning
    when: { from: "06:00", to: "09:00", days: [mon, tue] }
    pages: [home, week]
    rotate: true
    refresh: 5m
  - name: evening
    pages: [home]
fallback: evening
`

// TestEncodeRejectsInvalidPlan verifies that invalid plans never reach a file.
func TestEncodeRejectsInvalidPlan(t *testing.T) {
	for name, p := range map[string]*Plan{
		"no rules":         {Fallback: "home"},
		"bad name":         {Rules: []Rule{{Name: "Bad", Pages: []string{"home"}}}},
		"duplicate page":   {Rules: []Rule{{Name: "a", Pages: []string{"home", "home"}}}},
		"half window":      {Rules: []Rule{{Name: "a", Pages: []string{"home"}, When: When{From: clock(600)}}}},
		"unknown fallback": {Rules: []Rule{{Name: "a", Pages: []string{"home"}}}, Fallback: "nope"},
	} {
		if _, err := Encode(p); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

func clock(minutes int) *Clock { c := Clock(minutes); return &c }

// TestPlanJSONRoundTrip verifies the JSON model the editor API uses: clocks
// and weekdays keep their written form, and the result encodes to the same
// plan file.
func TestPlanJSONRoundTrip(t *testing.T) {
	p, err := Parse(strings.NewReader(examplePlan), nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"from":"06:00"`, `"days":["mon","tue"]`, `"refresh":"5m"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %s in %s", want, data)
		}
	}
	var got Plan
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Rules, got.Rules) || p.Fallback != got.Fallback {
		t.Fatalf("JSON round trip changed the plan\nwant %+v\ngot  %+v", p.Rules, got.Rules)
	}
	yamlData, err := Encode(&got)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Parse(bytes.NewReader(yamlData), nil)
	if err != nil {
		t.Fatalf("reparse: %v\n%s", err, yamlData)
	}
	if !reflect.DeepEqual(p.Rules, again.Rules) {
		t.Fatalf("plan changed\nwant %+v\ngot  %+v", p.Rules, again.Rules)
	}
}

// TestDayJSONAcceptsNames verifies that the editor may send the weekday names
// operators know from the YAML files.
func TestDayJSONAcceptsNames(t *testing.T) {
	var d Day
	if err := json.Unmarshal([]byte(`"monday"`), &d); err != nil || time.Weekday(d) != time.Monday {
		t.Fatalf("monday: %v %v", d, err)
	}
	if err := json.Unmarshal([]byte(`"Mon"`), &d); err != nil || time.Weekday(d) != time.Monday {
		t.Fatalf("Mon: %v %v", d, err)
	}
	if err := json.Unmarshal([]byte(`"noday"`), &d); err == nil {
		t.Fatal("unknown weekday accepted")
	}
}
