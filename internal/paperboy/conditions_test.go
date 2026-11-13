package paperboy

import (
	"context"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maehld/paperboy/pkg/plugin"
)

// conditionPlugin declares one condition and reports it as active or not.
type conditionPlugin struct{ active bool }

func (*conditionPlugin) Describe() plugin.Description {
	return plugin.Description{Conditions: []plugin.ConditionSpec{{Name: "low", Title: "Low"}}}
}
func (*conditionPlugin) Configure(plugin.Values, *plugin.Request) error { return nil }
func (*conditionPlugin) Render(*plugin.Request, plugin.WidgetRequest, *image.Paletted) error {
	return nil
}
func (c *conditionPlugin) Conditions(*plugin.Request) (map[string]bool, error) {
	return map[string]bool{"low": c.active}, nil
}

func writePlan(t *testing.T, s *Server, name, body string) {
	t.Helper()
	if err := os.MkdirAll(s.config().Plans, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.config().Plans, name+".yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSyncUsesTheConditionPage follows a whole wake: an active condition
// selects its rule's page, an inactive one lets the next rule win.
func TestSyncUsesTheConditionPage(t *testing.T) {
	s, _ := planServer(t, `
rules:
  - name: zucker
    when: { conditions: [fake/low] }
    pages: [eins]
  - name: tag
    pages: [zwei]
`)
	p := &conditionPlugin{active: true}
	s.widgets.Builtin["fake"] = p
	dev := planDevice(t, s)
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/eins/") {
		t.Fatalf("active condition must show its page, got %s", got)
	}
	p.active = false
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/zwei/") {
		t.Fatalf("inactive condition must fall through, got %s", got)
	}
}

// TestPlanConditions feeds a plan's condition names from the plugin host
// (ADR 0103): an active condition selects its rule, a plugin that is not
// configured or fails leaves the condition inactive.
func TestPlanConditions(t *testing.T) {
	s, _ := testServer(t)
	s.widgets.Builtin["fake"] = &conditionPlugin{active: true}
	writePlan(t, s, "guard", `
rules:
  - name: zucker
    when: { conditions: [fake/low] }
    pages: [zucker]
  - name: tag
    pages: [uebersicht]
`)
	cfg := s.config()
	conds := s.planConditions(context.Background(), cfg, "guard")
	if !conds["fake/low"] {
		t.Fatalf("conditions: %v", conds)
	}
	p, err := s.plans.get(cfg.Plans, "guard", cfg.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	if sel, ok := p.Select(s.now(), conds); !ok || sel.Rule != "zucker" {
		t.Fatalf("active condition must select its rule: %+v %v", sel, ok)
	}
	// A missing plugin leaves the condition inactive, so the next rule wins.
	writePlan(t, s, "unknown", `
rules:
  - name: zucker
    when: { conditions: [nope/low] }
    pages: [zucker]
  - name: tag
    pages: [uebersicht]
`)
	conds = s.planConditions(context.Background(), cfg, "unknown")
	p, err = s.plans.get(cfg.Plans, "unknown", cfg.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	if sel, ok := p.Select(s.now(), conds); !ok || sel.Rule != "tag" {
		t.Fatalf("unknown plugin must be inactive: %+v %v %v", sel, ok, conds)
	}
	// A plan without conditions asks no plugin and yields a nil snapshot.
	writePlan(t, s, "plain", "rules: [{name: tag, pages: [uebersicht]}]")
	if conds := s.planConditions(context.Background(), cfg, "plain"); conds != nil {
		t.Fatalf("plain plan asked for conditions: %v", conds)
	}
	// A missing plan asks nothing.
	if conds := s.planConditions(context.Background(), cfg, "gone"); conds != nil {
		t.Fatalf("missing plan asked for conditions: %v", conds)
	}
}
