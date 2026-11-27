package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/maehld/paperboy/sdk/plugin"
)

func TestConfigure(t *testing.T) {
	p := newPlugin()
	req := &plugin.Request{Version: plugin.Version}
	if err := p.Configure(plugin.Values{"url": "https://router.local", "api_key": "k"}, req); err != nil {
		t.Errorf("minimal config: %v", err)
	}
	cases := []struct {
		name string
		cfg  plugin.Values
		want string
	}{
		{"missing url", plugin.Values{"api_key": "k"}, "url"},
		{"http url", plugin.Values{"url": "http://router.local", "api_key": "k"}, "https"},
		{"not a url", plugin.Values{"url": "router.local", "api_key": "k"}, "https"},
		{"missing key", plugin.Values{"url": "https://router.local"}, "api_key"},
		{"empty key", plugin.Values{"url": "https://router.local", "api_key": " "}, "api_key"},
		{"fast refresh", plugin.Values{"url": "https://router.local", "api_key": "k", "refresh": "10s"}, "1m"},
		{"unknown", plugin.Values{"url": "https://router.local", "api_key": "k", "nope": true}, "nope"},
	}
	for _, c := range cases {
		err := p.Configure(c.cfg, req)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
	if err := p.Configure(plugin.Values{"url": "https://router.local", "api_key": "k"}, &plugin.Request{Version: 8}); err == nil {
		t.Error("protocol 8 accepted")
	}
}

func TestDescribe(t *testing.T) {
	d := newPlugin().Describe()
	var names []string
	for _, v := range d.Values {
		names = append(names, v.Name)
	}
	want := []string{"state", "wan-state", "clients", "devices", "offline", "worst", "latency", "download", "upload"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("values = %v, want %v", names, want)
	}
	if v := d.Values[0]; v.Type != plugin.ValueEnum || strings.Join(v.Enum, ",") != "ok,attention,degraded" {
		t.Errorf("state spec = %+v", v)
	}
	if v := d.Values[6]; v.Name != "latency" || v.Unit != "ms" {
		t.Errorf("latency spec = %+v", v)
	}
	if len(d.Actions) != 0 || len(d.Blobs) != 0 {
		t.Errorf("actions = %+v, blobs = %v, want none", d.Actions, d.Blobs)
	}
	if _, ok := any(newPlugin()).(plugin.Stater); ok {
		t.Error("the plugin keeps state")
	}
	if len(d.Widgets) != 2 || d.Widgets[0].Widget != "status" || d.Widgets[1].Widget != "wan" {
		t.Fatalf("widgets = %+v", d.Widgets)
	}
	w := d.Widgets[0]
	if w.MinWidth != 120 || w.MinHeight != 60 {
		t.Errorf("min size = %dx%d, want 120x60", w.MinWidth, w.MinHeight)
	}
	var spans []string
	for _, s := range w.Sizes {
		spans = append(spans, fmt.Sprintf("%dx%d", s.Columns, s.Rows))
	}
	if strings.Join(spans, ",") != "2x2,3x2,6x1" {
		t.Errorf("sizes = %v", spans)
	}
	if focus := w.Args["focus"]; focus.Type != "string" || focus.Default != "clients" {
		t.Errorf("focus arg = %+v", focus)
	}
	if detail := w.Args["detail"]; detail.Type != "bool" || detail.Default != true {
		t.Errorf("detail arg = %+v", detail)
	}
}

func TestValuesWithoutData(t *testing.T) {
	p := newPlugin()
	got, err := p.Values(&plugin.Request{Values: []string{"state", "wan-state", "clients", "devices", "offline", "worst", "latency", "download", "upload"}})
	if err != nil {
		t.Fatalf("values: %v", err)
	}
	if v := got["state"].Enum; v != nil {
		t.Errorf("state = %v, want an empty answer", v)
	}
	for _, name := range []string{"clients", "devices", "offline", "worst", "latency", "download", "upload"} {
		if v := got[name].Text; v == nil || *v != "–" {
			t.Errorf("%s = %v, want a dash", name, got[name])
		}
	}
	if _, err := p.Values(&plugin.Request{Values: []string{"nope"}}); !errors.Is(err, plugin.ErrUnknownValue) {
		t.Errorf("unknown value: err = %v", err)
	}
}
