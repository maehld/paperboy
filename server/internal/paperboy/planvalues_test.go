package paperboy

import (
	"context"
	"errors"
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/maehld/paperboy/sdk/plugin"
)

// writePlan writes a plan file into the instance of s.
func writePlan(t *testing.T, s *Server, name, body string) {
	t.Helper()
	if err := os.MkdirAll(s.config().Plans, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.config().Plans, name+".yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// planValuePlugin declares one enum value and answers a configurable key.
type planValuePlugin struct {
	key   string
	err   error
	calls int
}

func (*planValuePlugin) Describe() plugin.Description {
	return plugin.Description{Values: []plugin.ValueSpec{
		{Name: "state", Type: plugin.ValueEnum, Enum: []string{"normal", "low", "high", "urgent"}},
	}}
}

func (*planValuePlugin) Configure(plugin.Values, *plugin.Request) error { return nil }

func (*planValuePlugin) Render(*plugin.Request, plugin.WidgetRequest, *image.Paletted) error {
	return nil
}

func (p *planValuePlugin) Values(req *plugin.Request) (map[string]plugin.Value, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	if p.key == "" {
		return map[string]plugin.Value{"state": {}}, nil
	}
	return map[string]plugin.Value{"state": plugin.EnumValue(p.key)}, nil
}

// TestPlanValues feeds a plan's value conditions from the plugin host
// (ADR 0127): a matching key selects its rule, an empty or failing answer and
// a plugin that is not configured leave the condition without a match.
func TestPlanValues(t *testing.T) {
	s, _ := testServer(t)
	p := &planValuePlugin{key: "low"}
	s.widgets.Builtin["fake"] = p
	writePlan(t, s, "guard", `
rules:
  - name: niedrig
    when: { conditions: [{value: fake/state, is: [low, urgent]}] }
    pages: [niedrig]
  - name: tag
    pages: [tag]
`)
	cfg := s.config()
	dev := &cfg.Devices[0]
	values := s.planValues(context.Background(), cfg, "guard", dev.Profile, dev)
	if values["fake/state"] != "low" {
		t.Fatalf("plan values: %v", values)
	}
	pl, err := s.plans.get(cfg.Plans, "guard", cfg.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	if sel, ok := pl.Select(s.now(), values); !ok || sel.Rule != "niedrig" {
		t.Fatalf("matching key must select its rule: %+v %v", sel, ok)
	}

	// No key from the plugin: the condition does not match.
	p.key = ""
	if values := s.planValues(context.Background(), cfg, "guard", dev.Profile, dev); values != nil {
		t.Fatalf("an empty answer created a value: %v", values)
	}
	if sel, ok := pl.Select(s.now(), nil); !ok || sel.Rule != "tag" {
		t.Fatalf("a missing value must fall through: %+v %v", sel, ok)
	}

	// A failing plugin is omitted as well.
	p.err = errors.New("no data")
	if values := s.planValues(context.Background(), cfg, "guard", dev.Profile, dev); values != nil {
		t.Fatalf("a failing plugin created a value: %v", values)
	}

	// A plugin that is not configured answers no value either.
	writePlan(t, s, "unknown", `
rules:
  - name: niedrig
    when: { conditions: [{value: nope/state, is: low}] }
    pages: [niedrig]
  - name: tag
    pages: [tag]
`)
	if values := s.planValues(context.Background(), cfg, "unknown", dev.Profile, dev); values != nil {
		t.Fatalf("an unknown plugin created a value: %v", values)
	}

	// A plan without conditions asks no plugin and yields a nil snapshot, and
	// distinct rules ask each value once.
	writePlan(t, s, "plain", "rules: [{name: tag, pages: [tag]}]")
	before := p.calls
	if values := s.planValues(context.Background(), cfg, "plain", dev.Profile, dev); values != nil {
		t.Fatalf("plain plan asked for values: %v", values)
	}
	if p.calls != before {
		t.Fatalf("plain plan asked the plugin %d times", p.calls-before)
	}
	writePlan(t, s, "twice", `
rules:
  - name: a
    when: { conditions: [{value: fake/state, is: high}] }
    pages: [a]
  - name: b
    when: { conditions: [{value: fake/state, is: low}] }
    pages: [b]
`)
	p.key, p.err = "high", nil
	before = p.calls
	if values := s.planValues(context.Background(), cfg, "twice", dev.Profile, dev); values["fake/state"] != "high" {
		t.Fatalf("twice values: %v", values)
	}
	if got := p.calls - before; got != 1 {
		t.Fatalf("two rules naming one value asked the plugin %d times, want 1", got)
	}

	// A missing plan asks nothing.
	if values := s.planValues(context.Background(), cfg, "gone", dev.Profile, dev); values != nil {
		t.Fatalf("missing plan asked for values: %v", values)
	}
}
