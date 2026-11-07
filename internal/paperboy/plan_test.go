package paperboy

import (
	"encoding/json"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

const planDeviceMAC = "AA:BB:CC:DD:EE:01"

// planServer starts a server whose single device uses the given plan. The
// clock is fixed at Wednesday 2026-10-21 08:00 UTC.
func planServer(t *testing.T, planYAML string) (*Server, *time.Time) {
	t.Helper()
	dir := t.TempDir()
	pages := filepath.Join(dir, "pages")
	for _, name := range []string{"eins", "zwei", "drei", "vier"} {
		writePageFile(t, filepath.Join(pages, name+".small.yaml"), textPage(name))
	}
	plans := filepath.Join(dir, "plans")
	writePageFile(t, filepath.Join(plans, "zimmer.yaml"), planYAML)

	cfg := &Config{Dir: dir, Timezone: time.UTC,
		Profiles: map[string]Profile{"small": {Width: 40, Height: 20, PPI: 125, Gray: true}},
		Devices: []Device{{ID: "test", MAC: planDeviceMAC, Profile: "small", Plan: "zimmer",
			Refresh: Duration(10 * time.Minute)}}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.October, 21, 8, 0, 0, 0, time.UTC) // Wednesday
	s.now = func() time.Time { return now }
	return s, &now
}

// textPage is a one-cell page with a text of the built-in plugin.
func textPage(text string) string {
	return "grid: [1, 1]\nstyle: {padding: 0}\ncells:\n  - {widget: paperboy/text, args: {text: \"" + text + "\"}}\n"
}

func writePageFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const testPlan = `
rules:
  - name: morgen
    when: { from: "07:00", to: "09:00" }
    pages: [eins, zwei]
  - name: tag
    when: { from: "09:00", to: "19:00" }
    pages: [drei, vier]
    rotate: true
    refresh: 5m
  - name: nacht
    when: { from: "19:00", to: "07:00" }
    pages: [vier]
    refresh: 30m
fallback: nacht
`

// testDevice is a registered device and the sync body it sends next.
type testDevice struct {
	key string
	req syncRequest
}

// register posts a registration body and returns the recorder.
func register(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/device/register", strings.NewReader(body)))
	return rec
}

func planDevice(t *testing.T, s *Server) *testDevice {
	t.Helper()
	rec := register(t, s, `{"mac":"`+planDeviceMAC+`","hardware":"sticky","firmware":"0.4.0"}`)
	var out struct{ Key, Device string }
	if err := json.Unmarshal(rec.Body.Bytes(), &out); rec.Code != http.StatusOK || err != nil || out.Key == "" {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	return &testDevice{key: out.Key, req: syncRequest{Firmware: "0.4.0", Hardware: "sticky", Reason: "timer"}}
}

// syncRaw posts a sync body with the given key.
func syncRaw(t *testing.T, s *Server, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/device/sync", strings.NewReader(body))
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// displayPage sends the device's sync and remembers the shown hash; events
// are consumed by the sync.
func displayPage(t *testing.T, s *Server, dev *testDevice) map[string]any {
	t.Helper()
	body, err := json.Marshal(dev.req)
	if err != nil {
		t.Fatal(err)
	}
	rec := syncRaw(t, s, dev.key, string(body))
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); rec.Code != http.StatusOK || err != nil {
		t.Fatalf("sync: %d %s", rec.Code, rec.Body)
	}
	dev.req.Events = nil
	content, ok := out["image"].(map[string]any)
	if !ok {
		content, _ = out["text"].(map[string]any)
	}
	dev.req.Shown, _ = content["hash"].(string)
	return out
}

// press sends one button wake with the given roles in order.
func press(t *testing.T, s *Server, dev *testDevice, roles ...string) map[string]any {
	t.Helper()
	dev.req.Reason = "button"
	for _, role := range roles {
		dev.req.Events = append(dev.req.Events, syncEvent{Type: "button", Button: role})
	}
	out := displayPage(t, s, dev)
	dev.req.Reason = "timer"
	return out
}

func imageOf(out map[string]any) map[string]any {
	return out["image"].(map[string]any)
}

// pagePath returns the image path of a sync response, e.g. /pages/small/eins/<hash>.png.
func pagePath(out map[string]any) string {
	return imageOf(out)["path"].(string)
}

func fetchImage(t *testing.T, s *Server, imagePath string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, imagePath, nil))
	return rec
}

func TestPlanServesRenderedPage(t *testing.T) {
	s, _ := planServer(t, testPlan)
	dev := planDevice(t, s)
	out := displayPage(t, s, dev)
	if got := pagePath(out); !strings.HasPrefix(got, "/pages/small/eins/") {
		t.Fatalf("first page: %q", got)
	}
	if out["refresh_s"].(float64) != 600 || out["listen_s"].(float64) != 0 {
		t.Fatalf("refresh/listen: %v", out)
	}
	rec := fetchImage(t, s, pagePath(out))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("image: %d %v", rec.Code, rec.Header())
	}
	if !strings.HasPrefix(rec.Body.String(), "\x89PNG") {
		t.Fatal("not a PNG")
	}
	// Unknown rendered pages are 404, never 500.
	if rec := fetchImage(t, s, "/pages/small/unbekannt/000000000000.png"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown rendered page: %d", rec.Code)
	}
}

func TestPlanRuleSwitchAndKeys(t *testing.T) {
	s, now := planServer(t, testPlan)
	dev := planDevice(t, s)
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/eins/") {
		t.Fatalf("morning page: %q", got)
	}
	out := press(t, s, dev, buttonNext)
	if !strings.Contains(pagePath(out), "/zwei/") || out["listen_s"].(float64) != listenAfterInput.Seconds() {
		t.Fatalf("next must page and keep the device listening: %v", out)
	}
	if got := pagePath(press(t, s, dev, buttonHome)); !strings.Contains(got, "/eins/") {
		t.Fatalf("home: %q", got)
	}
	// previous wraps around within the rule.
	if got := pagePath(press(t, s, dev, buttonPrevious)); !strings.Contains(got, "/zwei/") {
		t.Fatalf("previous must wrap: %q", got)
	}
	// Several presses apply in order: zwei -> eins -> zwei -> eins -> zwei.
	if got := pagePath(press(t, s, dev, buttonNext, buttonNext, buttonPrevious, buttonNext)); !strings.Contains(got, "/zwei/") {
		t.Fatalf("events in order: %q", got)
	}

	// 09:00 starts another rule: first page, even though the device paged before.
	*now = time.Date(2026, time.October, 21, 9, 0, 0, 0, time.UTC)
	out = displayPage(t, s, dev)
	if !strings.Contains(pagePath(out), "/drei/") {
		t.Fatalf("new rule must start at its first page: %v", out)
	}
	if st := s.state["test"]; st.Rule != "tag" || st.Page != 0 {
		t.Fatalf("state: %+v", st)
	}
}

func TestPlanRotation(t *testing.T) {
	s, now := planServer(t, testPlan)
	dev := planDevice(t, s)
	*now = time.Date(2026, time.October, 21, 10, 0, 0, 0, time.UTC)
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/drei/") {
		t.Fatalf("first day page: %q", got)
	}
	// Every timer wake moves on while the rule has rotate: true.
	for _, want := range []string{"vier", "drei", "vier"} {
		*now = now.Add(5 * time.Minute)
		if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/"+want+"/") {
			t.Fatalf("rotation: got %q, want %s", got, want)
		}
	}
	// A button press also moves on and rotation continues from there.
	if got := pagePath(press(t, s, dev, buttonNext)); !strings.Contains(got, "/drei/") {
		t.Fatalf("button in rotation: %q", got)
	}
	*now = now.Add(5 * time.Minute)
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/vier/") {
		t.Fatalf("rotation after button: %q", got)
	}
}

func TestPlanPageTimeout(t *testing.T) {
	s, now := planServer(t, testPlan)
	dev := planDevice(t, s)
	displayPage(t, s, dev)          // eins
	press(t, s, dev, buttonNext)    // zwei
	*now = now.Add(6 * time.Minute) // past page_timeout (5m)
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/eins/") {
		t.Fatalf("back to the first page after the timeout: %q", got)
	}
}

func TestPlanRefreshCappedAtBoundary(t *testing.T) {
	s, now := planServer(t, testPlan)
	dev := planDevice(t, s)
	*now = time.Date(2026, time.October, 21, 8, 59, 0, 0, time.UTC)
	if got := displayPage(t, s, dev)["refresh_s"].(float64); got != 60 {
		t.Fatalf("wake at the rule change: %v", got)
	}
	*now = time.Date(2026, time.October, 21, 18, 59, 0, 0, time.UTC)
	if got := displayPage(t, s, dev)["refresh_s"].(float64); got != 60 {
		t.Fatalf("wake at the evening change: %v", got)
	}
	*now = time.Date(2026, time.October, 21, 20, 0, 0, 0, time.UTC)
	if got := displayPage(t, s, dev)["refresh_s"].(float64); got != 1800 {
		t.Fatalf("night refresh: %v", got)
	}
}

func TestPlanNoticePages(t *testing.T) {
	// A page without a template must not fail the request.
	s, _ := planServer(t, `
rules:
  - name: tag
    pages: [fehlt]
`)
	dev := planDevice(t, s)
	out := displayPage(t, s, dev)
	if got := pagePath(out); !strings.HasPrefix(got, "/hints/") {
		t.Fatalf("missing template must show a notice: %q", got)
	}
	if rec := fetchImage(t, s, pagePath(out)); rec.Code != http.StatusOK {
		t.Fatalf("notice image: %d", rec.Code)
	}
	// A missing plan file also yields a notice page.
	s2, _ := planServer(t, testPlan)
	s2.config().Devices[0].Plan = "gibtsnicht"
	dev2 := planDevice(t, s2)
	out = displayPage(t, s2, dev2)
	if got := pagePath(out); !strings.HasPrefix(got, "/hints/") {
		t.Fatalf("missing plan must show a notice: %q", got)
	}
	if out["refresh_s"].(float64) != hintRefresh.Seconds() {
		t.Fatalf("notice refresh: %v", out["refresh_s"])
	}
}

// fillPlugin is a built-in test plugin whose widget is filled black or white.
type fillPlugin struct{ black *bool }

func (fillPlugin) Describe() plugin.Description                   { return plugin.Description{} }
func (fillPlugin) Configure(plugin.Values, *plugin.Request) error { return nil }
func (f fillPlugin) Render(_ *plugin.Request, _ plugin.WidgetRequest, img *image.Paletted) error {
	if *f.black {
		render.FillSolid(img, img.Rect)
	}
	return nil
}

func TestPlanRerendersOnWidgetChange(t *testing.T) {
	s, _ := planServer(t, `
rules:
  - name: tag
    pages: [widget]
`)
	black := false
	s.widgets.Builtin["fill"] = fillPlugin{black: &black}
	writePageFile(t, filepath.Join(s.config().Pages, "widget.small.yaml"), "grid: [1, 1]\ncells: [{widget: fill/area}]\n")
	dev := planDevice(t, s)
	first := pagePath(displayPage(t, s, dev))
	black = true
	second := pagePath(displayPage(t, s, dev))
	if first == second {
		t.Fatalf("a changed widget must change the image path: %q", first)
	}
	if rec := fetchImage(t, s, second); rec.Code != http.StatusOK {
		t.Fatalf("image after widget change: %d", rec.Code)
	}
}

func TestPlanLoaderKeepsLastValidVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zimmer.yaml")
	l := newPlanLoader()
	writePageFile(t, path, "rules: [{name: alt, pages: [eins]}]\n")
	p, err := l.get(dir, "zimmer", time.UTC)
	if err != nil || p == nil || p.Rules[0].Name != "alt" {
		t.Fatalf("load: %v %v", p, err)
	}
	// A broken change keeps the old plan and reports the error.
	writePageFile(t, path, "rules: [{name: neu}]\n")
	stamp := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	p2, err2 := l.get(dir, "zimmer", time.UTC)
	if p2 != p || err2 == nil {
		t.Fatalf("broken plan must keep the old version: %v %v", p2, err2)
	}
	// A valid change replaces it.
	writePageFile(t, path, "rules: [{name: neu, pages: [zwei]}]\n")
	stamp = stamp.Add(time.Minute)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	p3, err3 := l.get(dir, "zimmer", time.UTC)
	if err3 != nil || p3 == nil || p3.Rules[0].Name != "neu" {
		t.Fatalf("fixed plan: %v %v", p3, err3)
	}
}
