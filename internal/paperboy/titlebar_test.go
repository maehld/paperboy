package paperboy

import (
	"net/http"
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
		if !strings.Contains(body, "<h1>Paperboy</h1>") {
			t.Fatalf("%s: the h1 does not name Paperboy", page.path)
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
