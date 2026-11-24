package plan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/maehld/paperboy/server/internal/duration"
)

// Encode writes a plan as a plan file the way Parse reads it (ADR 0066). It
// rejects an invalid plan before anything is written. The layout is stable and
// hand-readable: the page list, the refresh schedule and one block per trigger.
func Encode(p *Plan) ([]byte, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	root := newMap()
	pages := newSeq(false)
	for _, pg := range p.Pages {
		if !pg.Hidden {
			pages.Content = append(pages.Content, str(pg.Name))
			continue
		}
		n := newMap()
		n.Style = yaml.FlowStyle
		add(n, "page", str(pg.Name))
		add(n, "hidden", valueNode(true))
		pages.Content = append(pages.Content, n)
	}
	add(root, "pages", pages)
	if mode := p.Mode(); mode != RefreshCurrent {
		add(root, "on_refresh", str(string(mode)))
	}
	switch {
	case len(p.Refresh.Windows) > 0:
		n := newMap()
		if p.Refresh.Default != 0 {
			add(n, "default", str(duration.Text(time.Duration(p.Refresh.Default))))
		}
		windows := newSeq(false)
		for _, w := range p.Refresh.Windows {
			wn := newMap()
			wn.Style = yaml.FlowStyle
			add(wn, "from", quoted(clockString(w.From)))
			add(wn, "to", quoted(clockString(w.To)))
			add(wn, "interval", str(duration.Text(time.Duration(w.Interval))))
			windows.Content = append(windows.Content, wn)
		}
		add(n, "windows", windows)
		add(root, "refresh", n)
	case p.Refresh.Default != 0:
		add(root, "refresh", str(duration.Text(time.Duration(p.Refresh.Default))))
	}
	if len(p.Triggers) > 0 {
		triggers := newSeq(false)
		for _, t := range p.Triggers {
			n := newMap()
			add(n, "when", conditionsNode(t.When))
			add(n, "page", str(t.Page))
			if t.Alert != AlertNever {
				add(n, "alert", str(string(t.Alert)))
			}
			triggers.Content = append(triggers.Content, n)
		}
		add(root, "triggers", triggers)
	}

	return marshalNode(root)
}

// conditionsNode writes the condition list in flow style. Each condition
// writes its own form in MarshalYAML.
func conditionsNode(conds Conditions) *yaml.Node {
	list := newSeq(true)
	for i := range conds {
		node, err := conds[i].MarshalYAML()
		if err != nil {
			continue // the plan was validated before Encode
		}
		n, ok := node.(*yaml.Node)
		if !ok {
			continue
		}
		list.Content = append(list.Content, n)
	}
	return list
}

// ---- JSON marshalling for the editor API (ADR 0066) ----

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
