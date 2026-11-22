package paperboy

import (
	"strings"
	"testing"
)

// TestDashboardPreviewZooms checks that a served page is a button and that the
// dialog it opens lives outside the polled region (issue #398).
func TestDashboardPreviewZooms(t *testing.T) {
	s := keyServer(t, "")
	rp := storeTestPage(s, "small", "eins")
	dev := s.config().Devices[0]

	s.mu.Lock()
	st := &deviceState{LastPage: "eins", LastPageKind: "page", LastPageHash: rp.hash}
	s.state[dev.stateKey()] = st
	s.mu.Unlock()

	body := htmlRequest(t, s, "/ui").Body.String()
	for _, want := range []string{
		`<dialog id="zoom" class="zoom">`,
		`src="/ui/zoom.js"`,
		`type="button" data-zoom data-kind="image"`,
		`data-page-url="/ui/pages/eins"`,
		`aria-label="Enlarge the page eins of test"`,
		`src="` + lastImagePath(dev.ID) + `?h=` + rp.hash + `"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the dashboard lacks %q", want)
		}
	}
}
