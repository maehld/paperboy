package paperboy

import (
	"bytes"
	"net/http"
	"os"
	"strings"
	"testing"
)

// titleBarNav is the header navigation every operator page has to show, with
// one active tab.
func titleBarNav(active string) string {
	link := func(href, label string) string {
		class := ""
		if label == active {
			class = ` class="active"`
		}
		return "<a" + class + ` href="` + href + `">` + label + "</a>"
	}
	return "<nav>" + link("/ui", "Devices") + " · " + link("/ui/downloads", "Downloads") +
		" · " + link("/ui/plugins", "Widgets") + " · " + link("/ui/editor", "Editor") + "</nav>"
}

// TestOperatorPagesShareTheTitleBar keeps the brand, the tabs and the version
// line from drifting apart between the pages (issue 120: the plan preview put
// the device into the h1), and checks that the plan preview names its device,
// plan and display type in the muted line instead.
func TestOperatorPagesShareTheTitleBar(t *testing.T) {
	s, _ := planServer(t, testPlan)
	writeDeviceFile(t, s.config().Dir, "test", "display_type: small\nmac: \"AA:BB:CC:DD:EE:01\"\nplan: zimmer\n")
	writeDisplayFile(t, s.config().Dir, "small", "kind: image\nsize: [40, 20]\nppi: 125\n")

	for _, page := range []struct{ path, active string }{
		{"/ui", "Devices"},
		{"/ui/downloads", "Downloads"},
		{"/ui/plugins", "Widgets"},
		{"/ui/editor", "Editor"},
		{"/ui/editor/page?name=eins&display=small", "Editor"},
		{"/ui/editor/plan?name=zimmer", "Editor"},
		{"/ui/editor/device?id=test", "Editor"},
		{"/ui/editor/display?name=small", "Editor"},
		{"/ui/plan?device=test", "Devices"},
	} {
		rec := htmlRequest(t, s, page.path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", page.path, rec.Code, rec.Body)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `<h1 class="brand"><img src="/ui/logo.svg" alt="Paperboy"`) {
			t.Fatalf("%s: the h1 does not show the brand", page.path)
		}
		if !strings.Contains(body, titleBarNav(page.active)) {
			t.Fatalf("%s: the navigation differs from the other pages", page.path)
		}
	}

	body := htmlRequest(t, s, "/ui/plan?device=test").Body.String()
	for _, want := range []string{"device test · plan zimmer · display type small", "Plan zimmer · test · Paperboy"} {
		if !strings.Contains(body, want) {
			t.Fatalf("plan preview misses %q", want)
		}
	}
}

// TestOperatorPagesLinkTheSharedStylesheet keeps the look in one place: every
// page links /ui/ui.css instead of carrying its own copy of the header, the
// buttons and the bar (ADR 0076).
func TestOperatorPagesLinkTheSharedStylesheet(t *testing.T) {
	s, _ := planServer(t, testPlan)
	writeDeviceFile(t, s.config().Dir, "test", "display_type: small\nmac: \"AA:BB:CC:DD:EE:01\"\nplan: zimmer\n")
	writeDisplayFile(t, s.config().Dir, "small", "kind: image\nsize: [40, 20]\nppi: 125\n")

	for _, path := range []string{
		"/ui", "/ui/downloads", "/ui/plugins", "/ui/plugins",
		"/ui/editor", "/ui/editor/page?name=eins&display=small", "/ui/editor/plan?name=zimmer",
		"/ui/editor/device?id=test", "/ui/editor/display?name=small", "/ui/plan?device=test",
	} {
		body := htmlRequest(t, s, path).Body.String()
		if !strings.Contains(body, `<link rel="stylesheet" href="/ui/ui.css">`) {
			t.Fatalf("%s: the shared stylesheet is not linked", path)
		}
	}

	rec := htmlRequest(t, s, "/ui/ui.css")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("stylesheet: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

// TestOperatorPagesCarryTheLogo keeps the brand from drifting apart: every page
// shows the lockup in the header and links the small cut as its icon, and both
// are served from copies of the design files in docs/logo (issue 149).
func TestOperatorPagesCarryTheLogo(t *testing.T) {
	s, _ := planServer(t, testPlan)
	writeDeviceFile(t, s.config().Dir, "test", "display_type: small\nmac: \"AA:BB:CC:DD:EE:01\"\nplan: zimmer\n")
	writeDisplayFile(t, s.config().Dir, "small", "kind: image\nsize: [40, 20]\nppi: 125\n")

	pages := []string{
		"/ui", "/ui/downloads", "/ui/plugins",
		"/ui/editor", "/ui/editor/page?name=eins&display=small", "/ui/editor/plan?name=zimmer",
		"/ui/editor/device?id=test", "/ui/editor/display?name=small", "/ui/plan?device=test",
	}
	for _, path := range pages {
		body := htmlRequest(t, s, path).Body.String()
		for _, want := range []string{
			`<h1 class="brand"><img src="/ui/logo.svg" alt="Paperboy"`,
			`<link rel="icon" type="image/svg+xml" href="/ui/favicon.svg">`,
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s misses %q", path, want)
			}
		}
	}

	// The served files are the design files: an embedded copy cannot drift.
	for _, asset := range []struct{ path, design string }{
		{"/ui/logo.svg", "../../docs/logo/paperboy-lockup.svg"},
		{"/ui/favicon.svg", "../../docs/logo/paperboy-small.svg"},
	} {
		rec := htmlRequest(t, s, asset.path)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/svg+xml" {
			t.Fatalf("%s: %d %q", asset.path, rec.Code, rec.Header().Get("Content-Type"))
		}
		design, err := os.ReadFile(asset.design)
		if err != nil {
			t.Fatalf("%s: %v", asset.design, err)
		}
		if !bytes.Equal(rec.Body.Bytes(), design) {
			t.Fatalf("%s differs from %s", asset.path, asset.design)
		}
	}
}

// TestEditorBarsShareThePattern verifies that every editor shows one action bar
// with the link back to its list, the object name and Delete, Reload and Save in
// that order, so no editor drifts from the others (ADR 0076).
func TestEditorBarsShareThePattern(t *testing.T) {
	s, _ := planServer(t, testPlan)
	writeDeviceFile(t, s.config().Dir, "test", "display_type: small\nmac: \"AA:BB:CC:DD:EE:01\"\nplan: zimmer\n")
	writeDisplayFile(t, s.config().Dir, "small", "kind: image\nsize: [40, 20]\nppi: 125\n")

	for _, page := range []struct{ path, back, name string }{
		{"/ui/editor/page?name=eins&display=small", `<a href="/ui/editor?tab=pages">← Pages</a>`, "<h2>page eins</h2>"},
		{"/ui/editor/plan?name=zimmer", `<a href="/ui/editor?tab=plans">← Plans</a>`, "<h2>plan zimmer</h2>"},
		{"/ui/editor/device?id=test", `<a href="/ui/editor?tab=devices">← Devices</a>`, "<h2>device test</h2>"},
		{"/ui/editor/display?name=small", `<a href="/ui/editor?tab=displays">← Display types</a>`, "<h2>display type small</h2>"},
	} {
		body := htmlRequest(t, s, page.path).Body.String()
		if strings.Count(body, `class="bar"`) != 1 {
			t.Fatalf("%s: expected exactly one action bar", page.path)
		}
		for _, want := range []string{page.back, page.name} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s: the bar misses %q", page.path, want)
			}
		}
		del, reload, save := strings.Index(body, `id="delete"`), strings.Index(body, `id="reload"`), strings.Index(body, `id="save"`)
		if del < 0 || reload < del || save < reload {
			t.Fatalf("%s: the actions are not in the order Delete, Reload, Save", page.path)
		}
	}

	// The widgets page has no object and no action of its own, so it has no
	// bar: its status sits next to the display type switch (ADR 0076).
	body := htmlRequest(t, s, "/ui/plugins").Body.String()
	for _, unwanted := range []string{`class="bar"`, `id="reload"`, `id="save"`, `id="delete"`} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("the widgets page grew an action bar (%s)", unwanted)
		}
	}
	if !strings.Contains(body, `id="status"`) {
		t.Fatal("the widgets page lost its status line")
	}
}
