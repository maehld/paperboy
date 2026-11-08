package page

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"

	"go.yaml.in/yaml/v3"
)

// Encode writes a template as a page file the way Parse reads it (ADR 0066).
// It fills the defaults Parse fills (kind, notices, spans), so what it writes
// reads back unchanged, and it rejects an invalid template before anything is
// written. The layout is stable and hand-readable: fixed key order, flow style
// for coordinates and one cell per line.
func Encode(t *Template) ([]byte, error) {
	c := *t
	c.Cells = append([]Cell(nil), t.Cells...)
	if err := c.validate(); err != nil {
		return nil, err
	}
	root := newMap()
	if c.Title != "" {
		add(root, "title", str(c.Title))
	}
	if c.Kind == KindText {
		add(root, "kind", str(c.Kind))
	}
	add(root, "grid", intSeq(c.Grid[0], c.Grid[1]))
	if s := styleNode(c.Style, false); len(s.Content) > 0 {
		add(root, "style", s)
	}
	if c.Notices == "none" {
		add(root, "notices", str(c.Notices))
	}
	cells := newSeq(false)
	for _, cell := range c.Cells {
		cells.Content = append(cells.Content, cellNode(cell))
	}
	add(root, "cells", cells)
	return marshalNode(root)
}

// cellNode is one cell in flow style, e.g.
// { at: [0, 0], span: [6, 1], widget: paperboy/header }.
func cellNode(c Cell) *yaml.Node {
	n := newMap()
	n.Style = yaml.FlowStyle
	add(n, "at", intSeq(c.At[0], c.At[1]))
	add(n, "span", intSeq(c.Span[0], c.Span[1]))
	add(n, "widget", str(c.Widget))
	if len(c.Args) > 0 {
		add(n, "args", valueNode(c.Args))
	}
	if c.Style != nil {
		if s := styleNode(*c.Style, true); len(s.Content) > 0 {
			add(n, "style", s)
		}
	}
	return n
}

// styleNode writes the fields of a page or cell style that differ from the
// defaults, so a saved page carries only what an operator chose.
func styleNode(s Style, flow bool) *yaml.Node {
	n := newMap()
	if flow {
		n.Style = yaml.FlowStyle
	}
	if s.Padding != nil {
		add(n, "padding", num(*s.Padding))
	}
	if s.Rule != 0 {
		add(n, "rule", num(s.Rule))
	}
	if s.TextSize != 0 {
		add(n, "text_size", num(s.TextSize))
	}
	if s.Divider != 0 {
		add(n, "divider", num(s.Divider))
	}
	return n
}

// valueNode encodes an arg value deterministically: maps keep a sorted key
// order, lists keep their order, and every structure stays in flow style so a
// cell remains one line.
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
		return num(x)
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

func newMap() *yaml.Node { return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"} }

func newSeq(flow bool) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	if flow {
		n.Style = yaml.FlowStyle
	}
	return n
}

// add appends one key/value pair to a mapping.
func add(n *yaml.Node, key string, v *yaml.Node) {
	n.Content = append(n.Content, str(key), v)
}

func str(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

func num(f float64) *yaml.Node {
	// Integral numbers read back as ints, which is what args and styles want.
	if f == float64(int64(f)) {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(int64(f), 10)}
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: strconv.FormatFloat(f, 'g', -1, 64)}
}

func intSeq(values ...int) *yaml.Node {
	n := newSeq(true)
	for _, v := range values {
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(v)})
	}
	return n
}

// marshalNode writes a node as YAML with two-space indentation, like the
// example pages.
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
