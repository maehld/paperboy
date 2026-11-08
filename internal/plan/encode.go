package plan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Encode writes a plan as a plan file the way Parse reads it (ADR 0066). It
// rejects an invalid plan before anything is written. The layout is stable and
// hand-readable: one rule per block, the time window and the page list in flow
// style.
func Encode(p *Plan) ([]byte, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	root := newMap()
	rules := newSeq(false)
	for i := range p.Rules {
		r := &p.Rules[i]
		n := newMap()
		add(n, "name", str(r.Name))
		add(n, "when", whenNode(&r.When))
		pages := newSeq(true)
		for _, name := range r.Pages {
			pages.Content = append(pages.Content, str(name))
		}
		add(n, "pages", pages)
		if r.Rotate {
			add(n, "rotate", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
		}
		if r.Refresh != 0 {
			add(n, "refresh", str(durationString(time.Duration(r.Refresh))))
		}
		rules.Content = append(rules.Content, n)
	}
	add(root, "rules", rules)
	if p.Fallback != "" {
		add(root, "fallback", str(p.Fallback))
	}
	if len(p.Actions) > 0 {
		actions := newSeq(false)
		for _, a := range p.Actions {
			n := newMap()
			add(n, "action", str(a.Action))
			if len(a.Args) > 0 {
				add(n, "args", valueNode(a.Args))
			}
			if a.ID != "" {
				add(n, "id", str(a.ID))
			}
			if a.Title != "" {
				add(n, "title", str(a.Title))
			}
			actions.Content = append(actions.Content, n)
		}
		add(root, "actions", actions)
	}
	return marshalNode(root)
}

// whenNode writes a time window, e.g. {from: "06:00", to: "09:00", days: [mon, tue]}.
func whenNode(w *When) *yaml.Node {
	n := newMap()
	n.Style = yaml.FlowStyle
	if w.From != nil && w.To != nil {
		add(n, "from", quoted(clockString(*w.From)))
		add(n, "to", quoted(clockString(*w.To)))
	}
	if len(w.Days) > 0 {
		days := newSeq(true)
		for _, d := range w.Days {
			days.Content = append(days.Content, str(dayString(time.Weekday(d))))
		}
		add(n, "days", days)
	}
	return n
}

func clockString(c Clock) string { return fmt.Sprintf("%02d:%02d", int(c)/60, int(c)%60) }

// durationString writes a duration the way operators do: "5m" instead of
// "5m0s", but "1m30s" when the seconds matter.
func durationString(d time.Duration) string {
	s := d.String()
	switch {
	case strings.HasSuffix(s, "h0m0s"):
		return strings.TrimSuffix(s, "0m0s")
	case strings.HasSuffix(s, "m0s"):
		return strings.TrimSuffix(s, "0s")
	}
	return s
}

// dayShort lists the weekday names Encode writes, indexed like time.Weekday.
var dayShort = [...]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

func dayString(d time.Weekday) string { return dayShort[int(d)%7] }

// ---- YAML marshalling of the scalar types ----

func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }
func (c Clock) MarshalYAML() (any, error)    { return clockString(c), nil }
func (d Day) MarshalYAML() (any, error)      { return dayString(time.Weekday(d)), nil }

// ---- JSON marshalling for the editor API (ADR 0066) ----

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(durationString(time.Duration(d)))
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("expected duration as string, e.g. \"5m\"")
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

func (c Clock) MarshalJSON() ([]byte, error) { return json.Marshal(clockString(c)) }

func (c *Clock) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("expected a time of day as \"HH:MM\"")
	}
	clock, err := parseClock(s)
	if err != nil {
		return err
	}
	*c = clock
	return nil
}

func (d Day) MarshalJSON() ([]byte, error) { return json.Marshal(dayString(time.Weekday(d))) }

func (d *Day) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("expected a weekday such as \"mo\" or \"mon\"")
	}
	wd, ok := dayNames[strings.ToLower(s)]
	if !ok {
		return fmt.Errorf("unknown weekday %q", s)
	}
	*d = Day(wd)
	return nil
}

// ---- yaml.Node helpers ----

func newMap() *yaml.Node { return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"} }

func newSeq(flow bool) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	if flow {
		n.Style = yaml.FlowStyle
	}
	return n
}

func add(n *yaml.Node, key string, v *yaml.Node) {
	n.Content = append(n.Content, str(key), v)
}

func str(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s} }

// quoted is a string that must stay quoted, such as "06:00", which YAML would
// otherwise read as a number.
func quoted(s string) *yaml.Node {
	n := str(s)
	n.Style = yaml.DoubleQuotedStyle
	return n
}

// valueNode encodes an action arg deterministically: maps keep a sorted key
// order and stay in flow style, so an action remains a few lines.
func valueNode(v any) *yaml.Node {
	switch x := v.(type) {
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
	case string:
		return str(x)
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(x)}
	case int:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(x)}
	case int64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(x, 10)}
	case float64:
		if x == float64(int64(x)) {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(int64(x), 10)}
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: strconv.FormatFloat(x, 'g', -1, 64)}
	case []any:
		n := newSeq(true)
		for _, e := range x {
			n.Content = append(n.Content, valueNode(e))
		}
		return n
	case map[string]any:
		n := newMap()
		n.Style = yaml.FlowStyle
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			add(n, k, valueNode(x[k]))
		}
		return n
	}
	var n yaml.Node
	if err := n.Encode(v); err != nil {
		return str(fmt.Sprint(v))
	}
	return &n
}

func marshalNode(root *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
