package paperboy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// lcdPage is one text page for the 20×4 display type of the virtual tests.
func lcdPage(text string) string {
	return "kind: text\ngrid: [20, 4]\ncells: [{ span: [20, 4], widget: paperboy/text, args: { text: \"" + text + "\" } }]\n"
}

// virtualServer starts a server without devices: the virtual display brings
// its own plan and display type. The clock is fixed so paging is reproducible.
func virtualServer(t *testing.T, planYAML, actions string) (*Server, *time.Time, *menuPlugin) {
	t.Helper()
	dir := t.TempDir()
	pages := filepath.Join(dir, "pages")
	writePageFile(t, filepath.Join(pages, "eins.small.yaml"), textPage("eins"))
	writePageFile(t, filepath.Join(pages, "zwei.small.yaml"), textPage("zwei"))
	writePageFile(t, filepath.Join(pages, "eins.lcd.yaml"), lcdPage("eins"))
	writePageFile(t, filepath.Join(pages, "zwei.lcd.yaml"), lcdPage("zwei"))
	writePageFile(t, filepath.Join(dir, "plans", "zimmer.yaml"), planYAML+actions)
	cfg := &Config{Dir: dir, Language: "en", Timezone: time.UTC,
		Profiles: map[string]Profile{
			"small": {Width: 400, Height: 300, PPI: 125, Gray: true},
			"lcd":   {Kind: kindText, Width: 20, Height: 4, Charset: "utf8"},
		}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.November, 7, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	p := &menuPlugin{}
	s.widgets.Builtin["act"] = p
	return s, &now, p
}

// virtualSync posts one wake and returns the decoded answer.
func virtualSync(t *testing.T, s *Server, query string, body syncRequest) map[string]any {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/ui/virtual/sync?"+query, strings.NewReader(string(data)))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("virtual sync %s: %d %s", query, rec.Code, rec.Body)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// virtualPress sends one button and reports the shown hash of the answer.
func virtualPress(t *testing.T, s *Server, query, shown string, id int64, roles ...string) map[string]any {
	t.Helper()
	body := syncRequest{Reason: "button", Shown: shown}
	for _, role := range roles {
		body.Events = append(body.Events, syncEvent{ID: id, Type: "button", Button: role})
	}
	return virtualSync(t, s, query, body)
}

func debugOf(out map[string]any) map[string]any {
	debug, _ := out["debug"].(map[string]any)
	return debug
}

func imageHash(out map[string]any) string {
	hash, _ := imageOf(out)["hash"].(string)
	return hash
}

// TestVirtualDisplayPagesWithoutDevice checks the page, the first sync, the
// paging and that no device state appears anywhere.
func TestVirtualDisplayPagesWithoutDevice(t *testing.T) {
	s, _, _ := virtualServer(t, "rules:\n  - name: immer\n    pages: [eins, zwei]\n", "")

	rec := htmlRequest(t, s, "/ui/virtual?plan=zimmer&display=small")
	if rec.Code != http.StatusOK {
		t.Fatalf("page: %d %s", rec.Code, rec.Body)
	}
	for _, want := range []string{"Virtual display", "plan zimmer", "display type small", `data-sync="/ui/virtual/sync?display=small&amp;plan=zimmer"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("page misses %q: %s", want, rec.Body)
		}
	}

	query := "plan=zimmer&display=small&refresh=120"
	first := virtualSync(t, s, query, syncRequest{Reason: "timer"})
	if path := imageOf(first)["path"].(string); !strings.Contains(path, "/pages/small/eins/") {
		t.Fatalf("first page: %s", path)
	}
	if got := first["refresh_s"].(float64); got != 120 {
		t.Fatalf("refresh from the query: %v", first["refresh_s"])
	}
	if got := debugOf(first)["rule"]; got != "immer" {
		t.Fatalf("rule: %v", got)
	}
	if got := debugOf(first)["page_name"]; got != "eins" {
		t.Fatalf("page name: %v", got)
	}
	if got := debugOf(first)["pages"]; got != float64(2) {
		t.Fatalf("page count: %v", got)
	}

	second := virtualPress(t, s, query, imageHash(first), 1, buttonNext)
	if path := imageOf(second)["path"].(string); !strings.Contains(path, "/pages/small/zwei/") {
		t.Fatalf("next page: %s", path)
	}
	// A different display type is a different display: its own cursor.
	text := virtualSync(t, s, "plan=zimmer&display=lcd", syncRequest{Reason: "timer"})
	if lines := text["text"].(map[string]any)["lines"].([]any); !strings.HasPrefix(strings.TrimSpace(lines[0].(string)), "eins") {
		t.Fatalf("text page: %v", lines)
	}
	// The image display kept its position.
	again := virtualSync(t, s, query, syncRequest{Reason: "timer"})
	if path := imageOf(again)["path"].(string); !strings.Contains(path, "/pages/small/zwei/") {
		t.Fatalf("cursor lost: %s", path)
	}

	// Nothing was written and no device appeared.
	s.mu.Lock()
	states := len(s.state)
	s.mu.Unlock()
	if states != 0 {
		t.Fatalf("virtual display wrote device state: %d entries", states)
	}
	if entries, err := os.ReadDir(filepath.Join(s.config().Dir, "state", "devices")); err == nil && len(entries) > 0 {
		t.Fatalf("state/devices is not empty: %v", entries)
	}
	if body := htmlRequest(t, s, "/api/devices").Body.String(); strings.Contains(body, "virtual") {
		t.Fatalf("virtual display appears in /api/devices: %s", body)
	}
}

// TestVirtualDisplayShowsMenuButDoesNotRunActions follows the menu into the
// confirmation and stops before the plugin call (ADR 0091).
func TestVirtualDisplayShowsMenuButDoesNotRunActions(t *testing.T) {
	s, _, p := virtualServer(t, "rules:\n  - name: immer\n    pages: [eins]\n",
		"actions:\n  - action: act/go\n  - action: act/quick\n")
	query := "plan=zimmer&display=small"
	base := virtualSync(t, s, query, syncRequest{Reason: "timer"})
	list := virtualPress(t, s, query, imageHash(base), 1, buttonHome)
	if !strings.Contains(debugOf(list)["menu"].(string), "list") {
		t.Fatalf("menu did not open: %v", debugOf(list))
	}
	confirm := virtualPress(t, s, query, imageHash(list), 2, buttonHome)
	if !strings.Contains(debugOf(confirm)["menu"].(string), "confirm") {
		t.Fatalf("menu did not ask: %v", debugOf(confirm))
	}
	result := virtualPress(t, s, query, imageHash(confirm), 3, buttonHome)
	if len(p.runs) != 0 {
		t.Fatalf("the virtual display ran %v", p.runs)
	}
	if !strings.Contains(debugOf(result)["menu"].(string), "result") {
		t.Fatalf("no result banner: %v", debugOf(result))
	}
	s.mu.Lock()
	menu := s.virtual["zimmer/small"].Menu
	s.mu.Unlock()
	if !menu.Simulated || menu.Result != "State go" {
		t.Fatalf("dry-run result: %+v", menu)
	}
	// A stale press at the result screen acknowledges it without running
	// anything; the page returns to the plan's page.
	after := virtualPress(t, s, query, imageHash(base), 4, buttonHome)
	if pagePath(after) != imageOf(base)["path"] {
		t.Fatal("a press must acknowledge the result")
	}
	s.mu.Lock()
	open := s.virtual["zimmer/small"].Menu.Open
	s.mu.Unlock()
	if open {
		t.Fatal("the result banner must close")
	}
	if len(p.runs) != 0 {
		t.Fatalf("a stale press ran %v", p.runs)
	}
}

// TestVirtualDisplayLoadsItsImage checks that the browser fetches the picture
// through the UI side of the server (ADR 0091, 0097): a virtual display has no
// device key, and the device routes answer no page without one.
func TestVirtualDisplayLoadsItsImage(t *testing.T) {
	s, _, _ := virtualServer(t, "rules:\n  - name: immer\n    pages: [eins, zwei]\n", "")
	out := virtualSync(t, s, "plan=zimmer&display=small", syncRequest{Reason: "timer"})
	path := pagePath(out)
	if !strings.HasPrefix(path, "/ui/virtual/pages/small/eins/") {
		t.Fatalf("image path %q is not on the UI side", path)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("virtual image: %d %s", rec.Code, rec.Body)
	}
	// The same picture stays closed on the device route, which needs a key.
	rec = httptest.NewRecorder()
	device := "/pages/" + strings.TrimPrefix(path, "/ui/virtual/pages/")
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, device, nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("device route without a key: %d", rec.Code)
	}
}

// TestVirtualDisplayImageBehindThePassword keeps the picture behind ui.password
// like the rest of the UI (ADR 0096): the browser that opened the page has the
// credentials cached, everyone else does not.
func TestVirtualDisplayImageBehindThePassword(t *testing.T) {
	s, _, _ := virtualServer(t, "rules:\n  - name: immer\n    pages: [eins]\n", "")
	s.config().UI.Password = "geheim"
	data, err := json.Marshal(syncRequest{Reason: "timer"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/ui/virtual/sync?plan=zimmer&display=small", strings.NewReader(string(data)))
	req.SetBasicAuth("paperboy", "geheim")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sync with the password: %d %s", rec.Code, rec.Body)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	path := pagePath(out)
	req = httptest.NewRequest(http.MethodGet, path, nil)
	req.SetBasicAuth("paperboy", "geheim")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("image with the password: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("image without credentials: %d", rec.Code)
	}
}

// TestVirtualDisplayFetchesItsNotice checks that a notice page is fetched on
// the UI side as well.
func TestVirtualDisplayFetchesItsNotice(t *testing.T) {
	s, _, _ := virtualServer(t, "rules:\n  - name: immer\n    pages: [eins]\n", "")
	path := pagePath(virtualSync(t, s, "plan=fehlt&display=small", syncRequest{Reason: "timer"}))
	if !strings.HasPrefix(path, "/ui/virtual/hints/") {
		t.Fatalf("notice path %q is not on the UI side", path)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("virtual notice: %d %s", rec.Code, rec.Body)
	}
}

// TestVirtualDisplayOptionsRejected checks that a bad URL is answered with the
// status that names the problem instead of being guessed.
func TestVirtualDisplayOptionsRejected(t *testing.T) {
	s, _, _ := virtualServer(t, "rules:\n  - name: immer\n    pages: [eins]\n", "")
	cases := []struct {
		query  string
		status int
	}{
		{"", http.StatusBadRequest},
		{"plan=zimmer", http.StatusBadRequest},
		{"display=small", http.StatusBadRequest},
		{"plan=zimmer&display=unbekannt", http.StatusNotFound},
		{"plan=../zimmer&display=small", http.StatusBadRequest},
		{"plan=zimmer&display=small&menu=3", http.StatusBadRequest},
		{"plan=zimmer&display=small&menu=61", http.StatusBadRequest},
		{"plan=zimmer&display=small&refresh=0", http.StatusBadRequest},
		{"plan=zimmer&display=small&refresh=abc", http.StatusBadRequest},
		{"plan=zimmer&display=small&page_timeout=0", http.StatusBadRequest},
		{"plan=zimmer&display=small&power=auto", http.StatusBadRequest},
	}
	for _, c := range cases {
		if rec := htmlRequest(t, s, "/ui/virtual?"+c.query); rec.Code != c.status {
			t.Fatalf("%q: %d, want %d (%s)", c.query, rec.Code, c.status, rec.Body)
		}
		req := httptest.NewRequest(http.MethodPost, "/ui/virtual/sync?"+c.query, strings.NewReader(`{"reason":"timer"}`))
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != c.status {
			t.Fatalf("sync %q: %d, want %d (%s)", c.query, rec.Code, c.status, rec.Body)
		}
	}
}

// TestVirtualDisplayTextDisplay checks a text display: lines instead of an
// image, no listen window and the same paging.
func TestVirtualDisplayTextDisplay(t *testing.T) {
	s, _, _ := virtualServer(t, "rules:\n  - name: immer\n    pages: [eins, zwei]\n", "")
	query := "plan=zimmer&display=lcd"
	first := virtualSync(t, s, query, syncRequest{Reason: "timer"})
	if first["image"] != nil {
		t.Fatalf("a text display has no image: %v", first["image"])
	}
	if first["listen_s"].(float64) != 0 {
		t.Fatalf("listen_s: %v", first["listen_s"])
	}
	lines := first["text"].(map[string]any)["lines"].([]any)
	if len(lines) != 4 || strings.TrimSpace(lines[0].(string)) != "eins" {
		t.Fatalf("lines: %v", lines)
	}
	hash := first["text"].(map[string]any)["hash"].(string)
	next := virtualPress(t, s, query, hash, 1, buttonNext)
	if got := strings.TrimSpace(next["text"].(map[string]any)["lines"].([]any)[0].(string)); got != "zwei" {
		t.Fatalf("text page after next: %q", got)
	}
}

// TestVirtualDisplayResetDropsTheSession checks that Reset starts at the first
// page again.
func TestVirtualDisplayResetDropsTheSession(t *testing.T) {
	s, _, _ := virtualServer(t, "rules:\n  - name: immer\n    pages: [eins, zwei]\n", "")
	query := "plan=zimmer&display=small"
	first := virtualSync(t, s, query, syncRequest{Reason: "timer"})
	second := virtualPress(t, s, query, imageHash(first), 1, buttonNext)
	if path := imageOf(second)["path"].(string); !strings.Contains(path, "/zwei/") {
		t.Fatalf("next: %s", path)
	}
	req := httptest.NewRequest(http.MethodPost, "/ui/virtual/reset?"+query, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body)
	}
	s.mu.Lock()
	_, exists := s.virtual["zimmer/small"]
	s.mu.Unlock()
	if exists {
		t.Fatal("reset kept the session")
	}
	again := virtualSync(t, s, query, syncRequest{Reason: "powercycle"})
	if path := imageOf(again)["path"].(string); !strings.Contains(path, "/eins/") {
		t.Fatalf("page after reset: %s", path)
	}
}

// TestVirtualDisplayMissingPlanGetsNotice checks that a plan without a file is
// debugged as the notice a device would show.
func TestVirtualDisplayMissingPlanGetsNotice(t *testing.T) {
	s, _, _ := virtualServer(t, "rules:\n  - name: immer\n    pages: [eins]\n", "")
	out := virtualSync(t, s, "plan=fehlt&display=small", syncRequest{Reason: "timer"})
	if path := imageOf(out)["path"].(string); !strings.HasPrefix(path, "/ui/virtual/hints/") {
		t.Fatalf("notice page: %s", path)
	}
	if got := debugOf(out)["page_kind"]; got != "notice" {
		t.Fatalf("page kind: %v", got)
	}
	if err := debugOf(out)["error"]; err == nil || err == "" {
		t.Fatalf("notice must name the error: %v", debugOf(out))
	}
}

// TestVirtualDisplayServesScript keeps the script reachable.
func TestVirtualDisplayServesScript(t *testing.T) {
	s, _, _ := virtualServer(t, "rules:\n  - name: immer\n    pages: [eins]\n", "")
	rec := htmlRequest(t, s, "/ui/virtual.js")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "dataset.sync") {
		t.Fatalf("script: %d", rec.Code)
	}
}

// TestVirtualDisplayLinkedFromPlanAndDashboard checks that an operator reaches
// a device's plan as a virtual display without typing its parameters.
func TestVirtualDisplayLinkedFromPlanAndDashboard(t *testing.T) {
	s, _ := planServer(t, testPlan)
	link := `href="/ui/virtual?display=small&amp;plan=zimmer"`
	for _, path := range []string{"/ui/plan?device=test", "/ui"} {
		if body := htmlRequest(t, s, path).Body.String(); !strings.Contains(body, link) {
			t.Fatalf("%s misses the virtual display link: %s", path, body)
		}
	}
}
