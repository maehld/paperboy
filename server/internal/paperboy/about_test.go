package paperboy

import (
	"strings"
	"testing"
	"time"
)

// TestAboutPage checks the About page: the build, the licence, the
// configured plugins with their data source and the third-party code.
func TestAboutPage(t *testing.T) {
	s, _ := planServer(t, testPlan)

	rec := htmlRequest(t, s, "/ui/system/about")
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("about page: %d %v", rec.Code, rec.Header())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<a href="/ui/system/about" class="active" aria-current="page">About</a>`,
		"Paperboy</h2>",
		"Licence",
		"MIT",
		"https://github.com/maehld/paperboy",
		"Plugins",
		"Data source",
		`<td class="name">paperboy</td>`,
		"built-in",
		"none, it runs in Paperboy",
		"Third-party code",
		"htmx",
		"0BSD",
		"DotGothic16",
		"golang.org/x/image",
		"docs/plugins",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("about page misses %q:\n%s", want, body)
		}
	}
}

// TestAboutPageNamesConfiguredPlugins checks that a configured plugin appears
// with its version and the name and link of its data source.
func TestAboutPageNamesConfiguredPlugins(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{Dir: dir, Timezone: time.UTC,
		Profiles: map[string]Profile{"small": {Width: 40, Height: 20, PPI: 125, Levels: 4}},
		Plugins:  []Plugin{{Name: "weather"}, {Name: "armory"}}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	body := htmlRequest(t, s, "/ui/system/about").Body.String()
	for _, want := range []string{
		`<td class="name">weather</td>`,
		`<a href="https://open-meteo.com" rel="noreferrer">Open-Meteo</a>`,
		`<td class="name">armory</td>`,
		"Blizzard Entertainment and Raider.IO",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("about page misses %q:\n%s", want, body)
		}
	}
}

func TestShortRevision(t *testing.T) {
	if got := shortRevision("0123456789abcdef", false); got != "012345678" {
		t.Fatalf("short revision: %q", got)
	}
	if got := shortRevision("0123456789abcdef", true); got != "012345678+dirty" {
		t.Fatalf("dirty revision: %q", got)
	}
	if got := shortRevision("", true); got != "" {
		t.Fatalf("empty revision: %q", got)
	}
}

func TestCommitTime(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	if got := commitTime("2026-11-28T07:30:00Z", loc); got != "2026-11-28 08:30" {
		t.Fatalf("commit time: %q", got)
	}
	if got := commitTime("", loc); got != "" {
		t.Fatalf("missing commit time: %q", got)
	}
	if got := commitTime("not a time", loc); got != "" {
		t.Fatalf("broken commit time: %q", got)
	}
}
