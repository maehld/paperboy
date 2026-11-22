package paperboy

import (
	"bytes"
	"net/http"
	"os"
	"strings"
	"testing"
)

// railOf returns the rail of a page, so a test can check the section it marks
// without depending on the rest of the document.
func railOf(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, `<nav class="rail"`)
	if start < 0 {
		t.Fatalf("the page has no rail")
	}
	end := strings.Index(body[start:], "</nav>")
	if end < 0 {
		t.Fatalf("the rail is not closed")
	}
	return body[start : start+end]
}

// TestOperatorPagesShareTheShell keeps the brand, the rail and the section from
// drifting apart between the pages (ADR 0152; issue 120 put the device into the
// h1, issue 382 replaced the flat header).
func TestOperatorPagesShareTheShell(t *testing.T) {
	s, _ := planServer(t, testPlan)
	writeDeviceFile(t, s.config().Dir, "test", "display_type: small\nmac: \"AA:BB:CC:DD:EE:01\"\nplan: zimmer\n")
	writeDisplayFile(t, s.config().Dir, "small", "kind: image\nsize: [40, 20]\nppi: 125\n")

	for _, page := range []struct{ path, active string }{
		{"/ui", "Overview"},
		{"/ui/devices", "Devices"},
		{"/ui/devices/test", "Devices"},
		{"/ui/devices/test/plan", "Devices"},
		{"/ui/display-types", "Display types"},
		{"/ui/display-types/small", "Display types"},
		{"/ui/pages", "Pages"},
		{"/ui/pages/eins?display=small", "Pages"},
		{"/ui/plans", "Plans"},
		{"/ui/plans/zimmer", "Plans"},
		{"/ui/playground", "Playground"},
		{"/ui/system/plugins", "Plugins"},
		{"/ui/system/firmware", "Firmware"},
		{"/ui/system/maintenance", "Maintenance"},
		{"/ui/system/stats", "Stats"},
		{"/ui/system/styleguide", "Styleguide"},
	} {
		rec := htmlRequest(t, s, page.path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", page.path, rec.Code, rec.Body)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `<a class="brand" href="/ui"><img src="/ui/logo.svg" alt="Paperboy"`) {
			t.Fatalf("%s: the brand does not link to the dashboard", page.path)
		}
		rail := railOf(t, body)
		if !strings.Contains(rail, `class="active" aria-current="page">`+page.active+`</a>`) {
			t.Fatalf("%s: the rail does not mark %s: %s", page.path, page.active, rail)
		}
		if n := strings.Count(rail, `aria-current="page"`); n != 1 {
			t.Fatalf("%s: the rail marks %d sections", page.path, n)
		}
		// The title line belongs to the top bar, and the shell owns the only
		// main element: a page that styles its own main also restyles the
		// shell (issue 391).
		if title := strings.Index(body, `<nav class="titleline"`); title < 0 || title > strings.Index(body, `<div class="shell">`) {
			t.Fatalf("%s: the title line left the top bar", page.path)
		}
		if n := strings.Count(body, "<main"); n != 1 {
			t.Fatalf("%s: the page has %d main elements", page.path, n)
		}
	}

	// The rail groups its entries: the lists of the configuration and the
	// system tools carry a head, and no list hides behind another (ADR 0153).
	rail := railOf(t, htmlRequest(t, s, "/ui").Body.String())
	for _, want := range []string{
		`<span class="rail-head">Configuration</span>`,
		`<span class="rail-head">System</span>`,
		`href="/ui/devices"`, `href="/ui/display-types"`, `href="/ui/playground"`,
	} {
		if !strings.Contains(rail, want) {
			t.Fatalf("the rail misses %q: %s", want, rail)
		}
	}

	body := htmlRequest(t, s, "/ui/devices/test/plan").Body.String()
	for _, want := range []string{
		"Plan zimmer · test · Paperboy",
		`<a href="/ui/devices/test">test</a>`,
		`<h1>Plan</h1>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("plan preview misses %q", want)
		}
	}
}

// TestListsNameThemselvesInTheTitle keeps the top bar honest: every page but
// the overview names where the operator is, and a list is the first step of its
// editors (ADR 0152).
func TestListsNameThemselvesInTheBreadcrumb(t *testing.T) {
	s, _ := planServer(t, testPlan)
	writeDeviceFile(t, s.config().Dir, "test", "display_type: small\nmac: \"AA:BB:CC:DD:EE:01\"\nplan: zimmer\n")
	writeDisplayFile(t, s.config().Dir, "small", "kind: image\nsize: [40, 20]\nppi: 125\n")

	for _, page := range []struct{ path, title string }{
		{"/ui", `<h1>Overview</h1>`},
		{"/ui/pages", `<h1>Pages</h1>`},
		{"/ui/plans", `<h1>Plans</h1>`},
		{"/ui/devices", `<h1>Devices</h1>`},
		{"/ui/display-types", `<h1>Display types</h1>`},
		{"/ui/playground?display=small", `<h1>Playground</h1>`},
		{"/ui/system/plugins", `<h1>Plugins</h1>`},
		{"/ui/system/firmware", `<h1>Firmware</h1>`},
		{"/ui/system/maintenance", `<h1>Maintenance</h1>`},
		{"/ui/system/stats", `<h1>Stats</h1>`},
		{"/ui/system/styleguide", `<h1>Styleguide</h1>`},
	} {
		body := htmlRequest(t, s, page.path).Body.String()
		if !strings.Contains(body, `<nav class="titleline"`) {
			t.Fatalf("%s: the page has no title line", page.path)
		}
		if !strings.Contains(body, page.title) {
			t.Fatalf("%s: the title line does not end in %q", page.path, page.title)
		}
	}

}

// TestOperatorPagesLinkTheSharedStylesheet keeps the look in one place: every
// page links the tokens and /ui/ui.css instead of carrying its own colours and
// its own copy of the header and the buttons (ADR 0076, ADR 0152).
func TestOperatorPagesLinkTheSharedStylesheet(t *testing.T) {
	s, _ := planServer(t, testPlan)
	writeDeviceFile(t, s.config().Dir, "test", "display_type: small\nmac: \"AA:BB:CC:DD:EE:01\"\nplan: zimmer\n")
	writeDisplayFile(t, s.config().Dir, "small", "kind: image\nsize: [40, 20]\nppi: 125\n")

	const tokens = `<link rel="stylesheet" href="/ui/tokens.css">`
	const shared = `<link rel="stylesheet" href="/ui/ui.css">`
	for _, path := range []string{
		"/ui", "/ui/system/plugins", "/ui/playground", "/ui/playground",
		"/ui/pages", "/ui/pages/eins?display=small", "/ui/plans/zimmer",
		"/ui/devices/test", "/ui/display-types/small", "/ui/devices/test/plan",
		"/ui/system/stats",
	} {
		body := htmlRequest(t, s, path).Body.String()
		at := strings.Index(body, tokens)
		if at < 0 {
			t.Fatalf("%s: the tokens are not linked", path)
		}
		// The tokens define the values the stylesheet uses, so they come first.
		if css := strings.Index(body, shared); css < at {
			t.Fatalf("%s: the stylesheet is linked before the tokens", path)
		}
	}

	for _, path := range []string{"/ui/tokens.css", "/ui/ui.css"} {
		rec := htmlRequest(t, s, path)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/css") {
			t.Fatalf("%s: %d %q", path, rec.Code, rec.Header().Get("Content-Type"))
		}
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
		"/ui", "/ui/system/plugins", "/ui/playground",
		"/ui/pages", "/ui/pages/eins?display=small", "/ui/plans/zimmer",
		"/ui/devices/test", "/ui/display-types/small", "/ui/devices/test/plan",
		"/ui/system/stats",
	}
	for _, path := range pages {
		body := htmlRequest(t, s, path).Body.String()
		for _, want := range []string{
			`<a class="brand" href="/ui"><img src="/ui/logo.svg" alt="Paperboy"`,
			`<link rel="icon" type="image/svg+xml" href="/ui/favicon.svg">`,
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s misses %q", path, want)
			}
		}
	}

	// The served files are the design files: an embedded copy cannot drift.
	for _, asset := range []struct{ path, design string }{
		{"/ui/logo.svg", "../../../docs/logo/paperboy-lockup.svg"},
		{"/ui/favicon.svg", "../../../docs/logo/paperboy-small.svg"},
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

// TestEditorActionsShareThePattern verifies that every editor shows its object
// in the title line and Delete, Reload and Save in that order, so no editor
// drifts from the others (ADR 0076). The title line carries the way back since
// issue 397.
func TestEditorBarsShareThePattern(t *testing.T) {
	s, _ := planServer(t, testPlan)
	writeDeviceFile(t, s.config().Dir, "test", "display_type: small\nmac: \"AA:BB:CC:DD:EE:01\"\nplan: zimmer\n")
	writeDisplayFile(t, s.config().Dir, "small", "kind: image\nsize: [40, 20]\nppi: 125\n")

	for _, page := range []struct{ path, crumb, title string }{
		{"/ui/pages/eins?display=small", `<a href="/ui/pages">Pages</a>`, `<h1>Page eins</h1>`},
		{"/ui/plans/zimmer", `<a href="/ui/plans">Plans</a>`, `<h1>Plan zimmer</h1>`},
		{"/ui/devices/test", `<a href="/ui/devices">Devices</a>`, `<h1>Device test</h1>`},
		{"/ui/display-types/small", `<a href="/ui/display-types">Display types</a>`, `<h1>Display type small</h1>`},
	} {
		body := htmlRequest(t, s, page.path).Body.String()
		if strings.Count(body, `<nav class="titleline"`) != 1 {
			t.Fatalf("%s: expected exactly one title line", page.path)
		}
		for _, want := range []string{page.crumb, page.title} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s: the page misses %q", page.path, want)
			}
		}
		del, reload, save := strings.Index(body, `id="delete"`), strings.Index(body, `id="reload"`), strings.Index(body, `id="save"`)
		if del < 0 || reload < del || save < reload {
			t.Fatalf("%s: the actions are not in the order Delete, Reload, Save", page.path)
		}
	}

	// The widgets page has no object and no action of its own, so it has no
	// actions: its status sits in the title line (ADR 0076).
	body := htmlRequest(t, s, "/ui/playground").Body.String()
	for _, unwanted := range []string{`id="reload"`, `id="save"`, `id="delete"`} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("the widgets page grew an action (%s)", unwanted)
		}
	}
	if !strings.Contains(body, `id="status"`) {
		t.Fatal("the widgets page lost its status line")
	}
}
