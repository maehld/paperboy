package paperboy

import (
	"encoding/json"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
	"github.com/maehld/paperboy/server/internal/plan"
)

const planDeviceMAC = "AA:BB:CC:DD:EE:01"

// planServer starts a server whose single device uses the given plan. The
// clock is fixed at Wednesday 2026-10-21 08:00 UTC.
func planServer(t *testing.T, planYAML string) (*Server, *time.Time) {
	t.Helper()
	dir := t.TempDir()
	pages := filepath.Join(dir, "pages")
	for _, name := range []string{"eins", "zwei", "drei", "vier", "alarm"} {
		writePageFile(t, filepath.Join(pages, name+".yaml"), textPage(name))
	}
	// A text page for a trigger whose page does not fit an image display.
	writePageFile(t, filepath.Join(pages, "textseite.yaml"), "kind: text\ncells:\n  - {at: [0, 0], text: hi}\n")
	plans := filepath.Join(dir, "plans")
	writePageFile(t, filepath.Join(plans, "zimmer.yaml"), planYAML)

	cfg := &Config{Dir: dir, Timezone: time.UTC,
		Profiles: map[string]Profile{"small": {Width: 40, Height: 20, PPI: 125, Levels: 4}},
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
	return "kind: image\ngrid: [1, 1]\nstyle: {padding: 0}\ncells:\n  - {widget: paperboy/text, args: {text: \"" + text + "\"}}\n"
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

// testPlan exercises the rotation, the hidden trigger page and the day window.
const testPlan = `
pages: [eins, zwei, drei, vier, {page: alarm, hidden: true}]
on_refresh: cycle
refresh:
  default: 30m
  windows:
    - { from: "07:00", to: "19:00", interval: 5m }
triggers:
  - when: { value: fake/state, is: [low, urgent] }
    page: alarm
    alert: refresh
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
	pair(t, s, planDeviceMAC)
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

// pressHeld sends one button wake with the same hold time for every role.
func pressHeld(t *testing.T, s *Server, dev *testDevice, hold int, roles ...string) map[string]any {
	t.Helper()
	dev.req.Reason = "button"
	for _, role := range roles {
		dev.req.Events = append(dev.req.Events, syncEvent{Type: "button", Button: role, HoldMS: hold})
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

// testAPIKey returns the key of the one device a test instance registered, so
// a page download passes the check of ADR 0097.
func testAPIKey(s *Server) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range s.state {
		if st.APIKey != "" {
			return st.APIKey
		}
	}
	return ""
}

func fetchImage(t *testing.T, s *Server, imagePath string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, imagePath, nil)
	if key := testAPIKey(s); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func withTriggerValue(t *testing.T, s *Server, key string) *planValuePlugin {
	t.Helper()
	p := &planValuePlugin{key: key}
	s.widgets.Builtin["fake"] = p
	return p
}

func TestPlanServesRenderedPage(t *testing.T) {
	s, _ := planServer(t, testPlan)
	dev := planDevice(t, s)
	out := displayPage(t, s, dev)
	if got := pagePath(out); !strings.HasPrefix(got, "/pages/small/eins/") {
		t.Fatalf("first page: %q", got)
	}
	// 08:00 is inside the 5m window.
	if out["refresh_s"].(float64) != 300 || out["listen_s"].(float64) != 0 {
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

// TestPlanPaging pages the visible list and never reaches a hidden page.
func TestPlanPaging(t *testing.T) {
	s, _ := planServer(t, testPlan)
	dev := planDevice(t, s)
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/eins/") {
		t.Fatalf("first page: %q", got)
	}
	out := press(t, s, dev, buttonNext)
	if !strings.Contains(pagePath(out), "/zwei/") || out["listen_s"].(float64) != listenAfterInput.Seconds() {
		t.Fatalf("next must page and keep the input window: %v", out)
	}
	// A long next jumps to the last visible page and a long previous back to
	// the first (ADR 0170).
	if got := pagePath(pressHeld(t, s, dev, 800, buttonNext)); !strings.Contains(got, "/vier/") {
		t.Fatalf("long next: %q", got)
	}
	if got := pagePath(pressHeld(t, s, dev, 800, buttonPrevious)); !strings.Contains(got, "/eins/") {
		t.Fatalf("long previous: %q", got)
	}
	// previous wraps around within the visible list; alarm stays hidden.
	if got := pagePath(press(t, s, dev, buttonPrevious)); !strings.Contains(got, "/vier/") {
		t.Fatalf("previous must wrap: %q", got)
	}
	// Several presses apply in order from vier: eins -> zwei -> eins -> zwei.
	if got := pagePath(press(t, s, dev, buttonNext, buttonNext, buttonPrevious, buttonNext)); !strings.Contains(got, "/zwei/") {
		t.Fatalf("events in order: %q", got)
	}
	if st := s.state["AABBCCDDEE01"]; st.Trigger != "" || st.PageName != "zwei" {
		t.Fatalf("state: %+v", st)
	}
}

// TestLongPageKeysReachTheTriggerPage checks that the extended list ends with
// the page of the active trigger (ADR 0158, 0170).
func TestLongPageKeysReachTheTriggerPage(t *testing.T) {
	s, _ := planServer(t, testPlan)
	withTriggerValue(t, s, "low")
	dev := planDevice(t, s)
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/alarm/") {
		t.Fatalf("trigger page: %q", got)
	}
	// The trigger page is the last page of the extended list.
	if got := pagePath(pressHeld(t, s, dev, 800, buttonPrevious)); !strings.Contains(got, "/eins/") {
		t.Fatalf("long previous must start at the first page: %q", got)
	}
	if got := pagePath(pressHeld(t, s, dev, 800, buttonNext)); !strings.Contains(got, "/alarm/") {
		t.Fatalf("long next must end on the trigger page: %q", got)
	}
}

// TestPlanTrigger pins its page, lets buttons page away and pins again on the
// next sync without input (ADR 0158).
func TestPlanTriggerPins(t *testing.T) {
	s, now := planServer(t, testPlan)
	p := withTriggerValue(t, s, "low")
	dev := planDevice(t, s)
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/alarm/") {
		t.Fatalf("trigger page: %q", got)
	}
	if st := s.state["AABBCCDDEE01"]; st.Trigger != "alarm" || st.PageBeforeTrigger != "eins" {
		t.Fatalf("state: %+v", st)
	}
	// The hidden page is in the extended list: next leaves the alarm page.
	if got := pagePath(press(t, s, dev, buttonNext)); !strings.Contains(got, "/eins/") {
		t.Fatalf("button away from the trigger: %q", got)
	}
	// The next timer sync pins it again.
	*now = now.Add(5 * time.Minute)
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/alarm/") {
		t.Fatalf("trigger must pin again: %q", got)
	}
	// The trigger ends: current went back to the page before it.
	p.key = "normal"
	*now = now.Add(5 * time.Minute)
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/eins/") {
		t.Fatalf("current must return to the page before the trigger: %q", got)
	}
	if st := s.state["AABBCCDDEE01"]; st.Trigger != "" || st.PageBeforeTrigger != "" {
		t.Fatalf("state after the trigger: %+v", st)
	}
}

// TestPlanTriggerEndCycle continues with the next page when the trigger page
// leaves the rotation and on_refresh is cycle.
func TestPlanTriggerEndCycle(t *testing.T) {
	s, now := planServer(t, `
pages: [eins, zwei, {page: alarm, hidden: true}, drei]
on_refresh: cycle
triggers:
  - when: { value: fake/state, is: low }
    page: alarm
`)
	p := withTriggerValue(t, s, "low")
	dev := planDevice(t, s)
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/alarm/") {
		t.Fatalf("trigger page: %q", got)
	}
	p.key = "normal"
	*now = now.Add(10 * time.Minute)
	// alarm sat between zwei and drei: cycle continues with drei.
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/drei/") {
		t.Fatalf("cycle after the trigger: %q", got)
	}
}

// TestPlanTriggerAlert alerts once on the transition and on every timer sync
// for refresh, but never on button input (ADR 0131, 0158).
func TestPlanTriggerAlert(t *testing.T) {
	s, now := planServer(t, `
pages: [eins, zwei]
triggers:
  - when: { value: fake/state, is: low }
    page: zwei
    alert: once
  - when: { value: fake/state, is: high }
    page: zwei
    alert: refresh
`)
	p := withTriggerValue(t, s, "low")
	dev := planDevice(t, s)
	s.config().Devices[0].AlertMethods = &[]string{alertBeep, alertBlink}
	alerted := func(out map[string]any) bool { return out["beep"] == true && out["blink"] == true }
	quiet := func(out map[string]any) bool { return out["beep"] == nil && out["blink"] == nil }
	if out := displayPage(t, s, dev); !alerted(out) {
		t.Fatalf("the trigger must alert: %v", out)
	}
	*now = now.Add(10 * time.Minute)
	if out := displayPage(t, s, dev); !quiet(out) {
		t.Fatalf("once must stay quiet while the trigger holds: %v", out)
	}
	if out := press(t, s, dev, buttonNext); !quiet(out) {
		t.Fatalf("a button press must stay quiet: %v", out)
	}

	// A refresh trigger alerts on every timer sync, but not on input.
	p.key = "high"
	*now = now.Add(10 * time.Minute)
	for i := 0; i < 2; i++ {
		if out := displayPage(t, s, dev); !alerted(out) {
			t.Fatalf("refresh must alert on timer sync %d: %v", i, out)
		}
		*now = now.Add(5 * time.Minute)
	}
	if out := press(t, s, dev, buttonNext); !quiet(out) {
		t.Fatalf("a button press on a refresh trigger must stay quiet: %v", out)
	}

	// The trigger ends, then a new one alerts again.
	p.key = "normal"
	*now = now.Add(5 * time.Minute)
	if out := displayPage(t, s, dev); !quiet(out) {
		t.Fatalf("an ended trigger must stay quiet: %v", out)
	}
	p.key = "low"
	*now = now.Add(5 * time.Minute)
	if out := displayPage(t, s, dev); !alerted(out) {
		t.Fatalf("the new trigger must alert: %v", out)
	}
}

// TestPlanAlertMethods narrows the sync answer to the device's alert methods
// (ADR 0132). An empty list mutes the device.
func TestPlanAlertMethods(t *testing.T) {
	s, _ := planServer(t, `
pages: [eins, zwei]
triggers:
  - when: { value: fake/state, is: low }
    page: zwei
    alert: refresh
`)
	withTriggerValue(t, s, "low")
	dev := planDevice(t, s)

	// An unset list keeps the device silent (ADR 0132).
	out := displayPage(t, s, dev)
	if out["beep"] != nil || out["blink"] != nil {
		t.Fatalf("an unset list must stay silent: %v", out)
	}
	s.config().Devices[0].AlertMethods = &[]string{alertBeep}
	out = displayPage(t, s, dev)
	if out["beep"] != true || out["blink"] != nil {
		t.Fatalf("beep only: %v", out)
	}
	s.config().Devices[0].AlertMethods = &[]string{}
	out = displayPage(t, s, dev)
	if out["beep"] != nil || out["blink"] != nil {
		t.Fatalf("an empty list must mute: %v", out)
	}
	s.config().Devices[0].AlertMethods = &[]string{alertBlink}
	out = displayPage(t, s, dev)
	if out["blink"] != true || out["beep"] != nil {
		t.Fatalf("blink only: %v", out)
	}
}

func TestPlanRotation(t *testing.T) {
	s, now := planServer(t, testPlan)
	dev := planDevice(t, s)
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/eins/") {
		t.Fatalf("first page: %q", got)
	}
	// Every timer wake moves on while the plan has on_refresh: cycle.
	for _, want := range []string{"zwei", "drei", "vier", "eins"} {
		*now = now.Add(5 * time.Minute)
		if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/"+want+"/") {
			t.Fatalf("rotation: got %q, want %s", got, want)
		}
	}
	// A button press also moves on and rotation continues from there.
	if got := pagePath(press(t, s, dev, buttonNext)); !strings.Contains(got, "/zwei/") {
		t.Fatalf("button in rotation: %q", got)
	}
	*now = now.Add(5 * time.Minute)
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/drei/") {
		t.Fatalf("rotation after button: %q", got)
	}
}

// TestPlanRefreshCurrent keeps a button-paged page over a timer refresh
// (ADR 0130).
func TestPlanRefreshCurrent(t *testing.T) {
	s, now := planServer(t, `
pages: [eins, zwei, drei]
`)
	dev := planDevice(t, s)
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/eins/") {
		t.Fatalf("first page: %q", got)
	}
	if got := pagePath(press(t, s, dev, buttonNext)); !strings.Contains(got, "/zwei/") {
		t.Fatalf("button page: %q", got)
	}
	*now = now.Add(10 * time.Minute)
	out := displayPage(t, s, dev)
	if got := pagePath(out); !strings.Contains(got, "/zwei/") {
		t.Fatalf("current must keep the page: %q", got)
	}
	if got := out["refresh_s"].(float64); got != 600 {
		t.Fatalf("refresh must follow the device: %v", got)
	}
}

// editPlan rewrites the plan of planServer and moves its stamp, so the loader
// reads the new file.
func editPlan(t *testing.T, s *Server, content string) {
	t.Helper()
	path := filepath.Join(s.config().Plans, "zimmer.yaml")
	writePageFile(t, path, content)
	stamp := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
}

// TestPlanEditKeepsThePage keeps a device on its page when the operator edits
// the plan file: the cursor holds the page, not its index (ADR 0133).
func TestPlanEditKeepsThePage(t *testing.T) {
	s, _ := planServer(t, "pages: [eins, zwei, drei]\n")
	dev := planDevice(t, s)
	displayPage(t, s, dev)
	if got := pagePath(press(t, s, dev, buttonNext)); !strings.Contains(got, "/zwei/") {
		t.Fatalf("next: %q", got)
	}
	// A page in front of the current one must not move the device.
	editPlan(t, s, "pages: [vier, eins, zwei, drei]\n")
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/zwei/") {
		t.Fatalf("a plan edit must keep the page: %q", got)
	}
	// The next press continues from the page, not from the old index.
	if got := pagePath(press(t, s, dev, buttonNext)); !strings.Contains(got, "/drei/") {
		t.Fatalf("next after the edit: %q", got)
	}
	// A reordered list keeps it too.
	editPlan(t, s, "pages: [drei, vier, eins, zwei]\n")
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/drei/") {
		t.Fatalf("a reordered list must keep the page: %q", got)
	}
	// A page the list loses is resolved by the mode: current has no page to
	// return to, so the first page applies.
	editPlan(t, s, "pages: [vier, eins]\n")
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/vier/") {
		t.Fatalf("a lost page must start at the first page: %q", got)
	}
}

// TestPlanRefreshFirst shows the first page at every timer refresh.
func TestPlanRefreshFirst(t *testing.T) {
	s, now := planServer(t, `
pages: [eins, zwei]
on_refresh: first
`)
	dev := planDevice(t, s)
	displayPage(t, s, dev)
	if got := pagePath(press(t, s, dev, buttonNext)); !strings.Contains(got, "/zwei/") {
		t.Fatalf("button page: %q", got)
	}
	*now = now.Add(10 * time.Minute)
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/eins/") {
		t.Fatalf("first must reset at the next refresh: %q", got)
	}
}

func TestPlanRefreshCappedAtBoundary(t *testing.T) {
	s, now := planServer(t, testPlan)
	dev := planDevice(t, s)
	*now = time.Date(2026, time.October, 21, 6, 59, 0, 0, time.UTC)
	if got := displayPage(t, s, dev)["refresh_s"].(float64); got != 60 {
		t.Fatalf("wake at the window start: %v", got)
	}
	*now = time.Date(2026, time.October, 21, 18, 59, 0, 0, time.UTC)
	if got := displayPage(t, s, dev)["refresh_s"].(float64); got != 60 {
		t.Fatalf("wake at the window end: %v", got)
	}
	*now = time.Date(2026, time.October, 21, 20, 0, 0, 0, time.UTC)
	if got := displayPage(t, s, dev)["refresh_s"].(float64); got != 1800 {
		t.Fatalf("night refresh: %v", got)
	}
}

func TestPlanNoticePages(t *testing.T) {
	// A page without a template must not fail the request.
	s, _ := planServer(t, "pages: [fehlt]\n")
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
	s, _ := planServer(t, "pages: [widget]\n")
	black := false
	s.widgets.Builtin["fill"] = fillPlugin{black: &black}
	writePageFile(t, filepath.Join(s.config().Pages, "widget.yaml"), "kind: image\ngrid: [1, 1]\ncells: [{widget: fill/area}]\n")
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
	writePageFile(t, path, "pages: [eins]\n")
	p, err := l.get(dir, "zimmer", time.UTC)
	if err != nil || p == nil || p.Pages[0].Name != "eins" {
		t.Fatalf("load: %v %v", p, err)
	}
	// A broken change keeps the old plan and reports the error.
	writePageFile(t, path, "pages: [eins, eins]\n")
	stamp := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	p2, err2 := l.get(dir, "zimmer", time.UTC)
	if p2 != p || err2 == nil {
		t.Fatalf("broken plan must keep the old version: %v %v", p2, err2)
	}
	// A valid change replaces it.
	writePageFile(t, path, "pages: [zwei]\n")
	stamp = stamp.Add(time.Minute)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	p3, err3 := l.get(dir, "zimmer", time.UTC)
	if err3 != nil || p3 == nil || p3.Pages[0].Name != "zwei" {
		t.Fatalf("fixed plan: %v %v", p3, err3)
	}
}

// TestResolvePlanDoesNotNeedTheStateLock pins ADR 0138: plan loading, trigger
// selection and the page reads happen outside the state lock.
func TestResolvePlanDoesNotNeedTheStateLock(t *testing.T) {
	s, now := planServer(t, testPlan)
	cfg := s.config()
	d := &cfg.Devices[0]

	s.mu.Lock()
	done := make(chan planResolution, 1)
	go func() { done <- s.resolvePlan(cfg, d, *now, nil) }()
	select {
	case res := <-done:
		if res.plan == nil || len(res.effective("")) != 4 {
			t.Fatalf("resolution under a held lock: %+v", res)
		}
	case <-time.After(2 * time.Second):
		s.mu.Unlock()
		t.Fatal("resolvePlan waits for the state lock")
	}
	s.mu.Unlock()
}

// TestResolvePlanSkipsMissingPages keeps the declared order and names the pages
// that do not fit; choosePlanPage records the reason for the journal.
func TestResolvePlanSkipsMissingPages(t *testing.T) {
	s, now := planServer(t, "pages: [eins, fehlt, zwei]\n")
	cfg := s.config()
	d := &cfg.Devices[0]
	res := s.resolvePlan(cfg, d, *now, nil)
	pages := res.effective("")
	if len(pages) != 2 || pages[0] != "eins" || pages[1] != "zwei" {
		t.Fatalf("fitting pages: %+v", pages)
	}
	if len(res.skipped) != 1 || !strings.Contains(res.skipped[0], "fehlt") {
		t.Fatalf("skipped: %+v", res.skipped)
	}
	s.mu.Lock()
	st := &deviceState{}
	choice := s.choosePlanPage(res, cfg, d, st, *now, true, false, nil, false, false, true)
	reason := s.skipNotes["small/fehlt"]
	s.mu.Unlock()
	if choice.name != "eins" || choice.pages != 2 {
		t.Fatalf("choice: %+v", choice)
	}
	if reason == "" {
		t.Fatal("the skipped page has no note")
	}
}

// TestPlanTriggerSkipsAPageOfAnotherKind lets the next matching trigger apply
// when its page does not fit the display type.
func TestPlanTriggerSkipsAPageOfAnotherKind(t *testing.T) {
	s, _ := planServer(t, `
pages: [eins, zwei, {page: textseite, hidden: true}]
triggers:
  - when: { value: fake/state, is: low }
    page: textseite
  - when: { value: fake/state, is: low }
    page: zwei
`)
	withTriggerValue(t, s, "low")
	res := s.resolvePlan(s.config(), &s.config().Devices[0], s.now(), plan.Values{"fake/state": "low"})
	if res.trigger != "zwei" {
		t.Fatalf("the text page must be skipped: %+v", res)
	}
}

// TestPlanTimeTrigger pins a hidden page inside a recurring window and lets
// the cursor go back to the page before it when the window ends (ADR 0158).
func TestPlanTimeTrigger(t *testing.T) {
	s, now := planServer(t, `
pages: [eins, {page: zwei, hidden: true}]
triggers:
  - when: [{ value: time, from: "07:00", to: "09:00" }]
    page: zwei
`)
	dev := planDevice(t, s)
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/zwei/") {
		t.Fatalf("inside the window: %q", got)
	}
	if st := s.state["AABBCCDDEE01"]; st.Trigger != "zwei" || st.PageBeforeTrigger != "eins" {
		t.Fatalf("state: %+v", st)
	}
	// 09:00 ends the window; current returns to the page before it.
	*now = time.Date(2026, time.October, 21, 9, 0, 0, 0, time.UTC)
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/eins/") {
		t.Fatalf("outside the window: %q", got)
	}
	if st := s.state["AABBCCDDEE01"]; st.Trigger != "" {
		t.Fatalf("state after the window: %+v", st)
	}
}

// TestPlanDateTrigger pins a page for one local calendar day.
func TestPlanDateTrigger(t *testing.T) {
	s, now := planServer(t, `
pages: [eins, {page: zwei, hidden: true}]
triggers:
  - when: [{ value: date, from: "2026-10-21", to: "2026-10-22" }]
    page: zwei
`)
	dev := planDevice(t, s)
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/zwei/") {
		t.Fatalf("on the date: %q", got)
	}
	// The next midnight ends it, and the cursor returns to the page before.
	*now = time.Date(2026, time.October, 22, 0, 0, 0, 0, time.UTC)
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/eins/") {
		t.Fatalf("after the date: %q", got)
	}
	// The date edge caps the sleep.
	*now = time.Date(2026, time.October, 21, 23, 59, 0, 0, time.UTC)
	if got := displayPage(t, s, dev)["refresh_s"].(float64); got != 60 {
		t.Fatalf("wake at midnight: %v", got)
	}
}

// TestPlanTimeTriggerCapsTheSleep wakes the device at the window edge.
func TestPlanTimeTriggerCapsTheSleep(t *testing.T) {
	s, now := planServer(t, `
pages: [eins, {page: zwei, hidden: true}]
triggers:
  - when: [{ value: time, from: "07:00", to: "09:00" }]
    page: zwei
`)
	dev := planDevice(t, s)
	*now = time.Date(2026, time.October, 21, 6, 59, 0, 0, time.UTC)
	if got := displayPage(t, s, dev)["refresh_s"].(float64); got != 60 {
		t.Fatalf("wake at the window start: %v", got)
	}
	*now = time.Date(2026, time.October, 21, 8, 59, 0, 0, time.UTC)
	if got := displayPage(t, s, dev)["refresh_s"].(float64); got != 60 {
		t.Fatalf("wake at the window end: %v", got)
	}
}

// TestPlanWeekdayTrigger checks the plan-built-in weekday.
func TestPlanWeekdayTrigger(t *testing.T) {
	s, now := planServer(t, `
pages: [eins, {page: zwei, hidden: true}]
triggers:
  - when: [{ value: weekday, is: [wed] }]
    page: zwei
`)
	dev := planDevice(t, s) // Wednesday 2026-10-21
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/zwei/") {
		t.Fatalf("wednesday: %q", got)
	}
	*now = time.Date(2026, time.October, 22, 8, 0, 0, 0, time.UTC) // Thursday
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/eins/") {
		t.Fatalf("thursday: %q", got)
	}
}

// TestPlanWeekdayTriggerWakesAtMidnight caps the sleep at the day change, so a
// weekday page leaves on time even when the interval has no midnight slot
// (issue #460).
func TestPlanWeekdayTriggerWakesAtMidnight(t *testing.T) {
	s, now := planServer(t, `
pages: [eins, {page: alarm, hidden: true}]
refresh: {default: 10h}
triggers:
  - when: [{ value: weekday, is: [wed] }]
    page: alarm
`)
	*now = time.Date(2026, time.October, 21, 20, 0, 0, 0, time.UTC) // Wednesday
	dev := planDevice(t, s)
	out := displayPage(t, s, dev)
	if got := pagePath(out); !strings.Contains(got, "/alarm/") {
		t.Fatalf("trigger page: %q", got)
	}
	// The weekday ends at midnight, four hours after this sync. The 10h grid
	// would not reach it.
	if got := out["refresh_s"].(float64); got != 4*60*60 {
		t.Fatalf("wake at midnight: %v", got)
	}
}

// TestHomeSyncsTheServedPage checks that home only syncs the page and never
// pages, opens a menu or schedules a wake (ADR 0161).
func TestHomeSyncsTheServedPage(t *testing.T) {
	s, _ := planServer(t, testPlan)
	dev := planDevice(t, s)
	displayPage(t, s, dev)       // eins
	press(t, s, dev, buttonNext) // zwei
	out := press(t, s, dev, buttonHome)
	if !strings.Contains(pagePath(out), "/zwei/") {
		t.Fatalf("home moved the page: %q", pagePath(out))
	}
	if out["listen_s"].(float64) != listenAfterInput.Seconds() {
		t.Fatalf("home must keep the input window: %v", out["listen_s"])
	}
	if out["wake"] != nil {
		t.Fatalf("home scheduled a wake: %v", out["wake"])
	}
	if got := pagePath(pressHeld(t, s, dev, 800, buttonHome)); !strings.Contains(got, "/zwei/") {
		t.Fatalf("a long home changed the page: %q", got)
	}
	// The timer cycles from the page home kept, so home did not advance it.
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/drei/") {
		t.Fatalf("the timer must cycle from the page home kept: %q", got)
	}
}

// TestHomeKeepsATriggerPage checks that home does not re-assert a trigger or
// page away from the page a button chose (ADR 0158, 0161).
func TestHomeKeepsATriggerPage(t *testing.T) {
	s, _ := planServer(t, testPlan)
	withTriggerValue(t, s, "low")
	dev := planDevice(t, s)
	displayPage(t, s, dev) // alarm, from the trigger
	press(t, s, dev, buttonNext)
	if got := pagePath(press(t, s, dev, buttonHome)); !strings.Contains(got, "/eins/") {
		t.Fatalf("home returned to the trigger page: %q", got)
	}
	if st := s.state["AABBCCDDEE01"]; st.Trigger != "alarm" {
		t.Fatalf("home dropped the trigger: %+v", st)
	}
	// A sync without input pins the trigger again.
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/alarm/") {
		t.Fatalf("the timer must pin the trigger again: %q", got)
	}
}

// TestPageRoles checks the button roles of one wake (ADR 0161).
func TestPageRoles(t *testing.T) {
	cases := []struct {
		name    string
		buttons []string
		holds   []int
		want    []string
	}{
		{"home never pages", []string{buttonHome}, []int{0}, nil},
		{"long home never pages", []string{buttonHome}, []int{800}, nil},
		{"next pages", []string{buttonNext}, []int{0}, []string{buttonNext}},
		{"previous pages", []string{buttonPrevious}, []int{0}, []string{buttonPrevious}},
		{"long next jumps to the last page", []string{buttonNext}, []int{800}, []string{roleLast}},
		{"long previous jumps to the first page", []string{buttonPrevious}, []int{800}, []string{roleFirst}},
		{"a hold below the threshold pages", []string{buttonNext}, []int{600}, []string{buttonNext}},
		{"next and home", []string{buttonNext, buttonHome}, []int{0, 0}, []string{buttonNext}},
	}
	for _, c := range cases {
		if got := pageRoles(c.buttons, c.holds); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
