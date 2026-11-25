package paperboy

import (
	"context"
	"errors"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// TestPlanValues feeds a plan's trigger conditions from the plugin host
// (ADR 0127): a matching key selects its trigger, an empty or failing answer and
// a plugin that is not configured leave the condition without a match.
func TestPlanValues(t *testing.T) {
	s, now := testServer(t)
	p := &planValuePlugin{key: "low"}
	s.widgets.Builtin["fake"] = p
	writePageFile(t, filepath.Join(s.config().Pages, "tag.yaml"), textPage("tag"))
	writePageFile(t, filepath.Join(s.config().Pages, "niedrig.yaml"), textPage("niedrig"))
	writePlan(t, s, "guard", `
pages: [tag, {page: niedrig, hidden: true}]
triggers:
  - when: { value: fake/state, is: [low, urgent] }
    page: niedrig
`)
	cfg := s.config()
	cfg.Devices[0].Plan = "guard"
	dev := &cfg.Devices[0]
	values := s.planValues(context.Background(), cfg, "guard", dev.Profile, dev)
	if values["fake/state"] != "low" {
		t.Fatalf("plan values: %v", values)
	}
	res := s.resolvePlan(cfg, dev, *now, values)
	if res.trigger != "niedrig" {
		t.Fatalf("matching key must select its trigger: %+v", res)
	}

	// No key from the plugin: the condition does not match.
	p.key = ""
	if values := s.planValues(context.Background(), cfg, "guard", dev.Profile, dev); values != nil {
		t.Fatalf("an empty answer created a value: %v", values)
	}
	if res := s.resolvePlan(cfg, dev, *now, nil); res.trigger != "" {
		t.Fatalf("a missing value must not trigger: %+v", res)
	}

	// A failing plugin is omitted as well.
	p.err = errors.New("no data")
	if values := s.planValues(context.Background(), cfg, "guard", dev.Profile, dev); values != nil {
		t.Fatalf("a failing plugin created a value: %v", values)
	}

	// A plugin that is not configured answers no value either.
	writePlan(t, s, "unknown", `
pages: [tag, {page: niedrig, hidden: true}]
triggers:
  - when: { value: nope/state, is: low }
    page: niedrig
`)
	if values := s.planValues(context.Background(), cfg, "unknown", dev.Profile, dev); values != nil {
		t.Fatalf("an unknown plugin created a value: %v", values)
	}

	// A plan without triggers asks no plugin and yields a nil snapshot, and
	// two triggers naming one value ask it once.
	writePlan(t, s, "plain", "pages: [tag]")
	before := p.calls
	if values := s.planValues(context.Background(), cfg, "plain", dev.Profile, dev); values != nil {
		t.Fatalf("plain plan asked for values: %v", values)
	}
	if p.calls != before {
		t.Fatalf("plain plan asked the plugin %d times", p.calls-before)
	}
	writePlan(t, s, "twice", `
pages: [tag, {page: niedrig, hidden: true}]
triggers:
  - when: { value: fake/state, is: high }
    page: tag
  - when: { value: fake/state, is: low }
    page: niedrig
`)
	p.key, p.err = "high", nil
	before = p.calls
	if values := s.planValues(context.Background(), cfg, "twice", dev.Profile, dev); values["fake/state"] != "high" {
		t.Fatalf("twice values: %v", values)
	}
	if got := p.calls - before; got != 1 {
		t.Fatalf("two triggers naming one value asked the plugin %d times, want 1", got)
	}

	// A missing plan asks nothing.
	if values := s.planValues(context.Background(), cfg, "gone", dev.Profile, dev); values != nil {
		t.Fatalf("missing plan asked for values: %v", values)
	}
}

// TestPlanValuesIgnoreAStaleAnswer pins issue #458: the host serves the last
// cached key of a failing plugin, and a plan condition must treat that like a
// missing answer instead of keeping its trigger page alive.
func TestPlanValuesIgnoreAStaleAnswer(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "plugin-state")
	writePageFile(t, stateFile, "low")
	for _, name := range []string{"eins", "alarm"} {
		writePageFile(t, filepath.Join(dir, "pages", name+".yaml"), textPage(name))
	}
	writePageFile(t, filepath.Join(dir, "plans", "zimmer.yaml"), `
pages: [eins, {page: alarm, hidden: true}]
triggers:
  - when: { value: test/state, is: [low] }
    page: alarm
`)
	if err := os.MkdirAll(filepath.Join(dir, pluginsDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	installTestPlugin(t, dir, "test")
	writeConfig(t, dir, `
language: de
plugins:
  test:
    env: { PAPERBOY_TEST_PLUGIN: "1" }
    config: { secret: "s3cret", state_file: '`+stateFile+`' }
`)
	writeDisplayFile(t, dir, "e1001", reloadDisplay)
	writeDeviceFile(t, dir, "flur", planDeviceFile)
	s := configServer(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	dev := planDevice(t, s)
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/alarm/") {
		t.Fatalf("trigger page: %q", got)
	}
	// The plugin loses its data. The cache still holds the last key, but it is
	// older than max_age, so the host serves it as stale.
	writePageFile(t, stateFile, "gone")
	base := time.Date(2026, time.October, 21, 8, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return base.Add(2 * time.Second) }
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/eins/") {
		t.Fatalf("a stale answer kept the trigger page: %q", got)
	}
}
