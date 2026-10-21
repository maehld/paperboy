package paperboy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const planDeviceMAC = "AA:BB:CC:DD:EE:01"

// planServer starts a server whose single device uses the given plan. The
// clock is fixed at Wednesday 2026-10-21 08:00 UTC.
func planServer(t *testing.T, planYAML string) (*Server, *time.Time) {
	t.Helper()
	dir := t.TempDir()
	pages := filepath.Join(dir, "pages")
	for _, name := range []string{"eins", "zwei", "drei", "vier"} {
		writePageFile(t, filepath.Join(pages, name+".small.yaml"), "rows: [1]\nblocks:\n  - text: \""+name+"\"\n")
	}
	plans := filepath.Join(dir, "plans")
	writePageFile(t, filepath.Join(plans, "zimmer.yaml"), planYAML)

	cfg := &Config{UploadToken: testToken, Pages: pages, Plans: plans, Timezone: time.UTC,
		Profiles: map[string]Profile{"small": {Width: 40, Height: 20, Gray: true}},
		Devices: []Device{{ID: "test", MAC: planDeviceMAC, Profile: "small", Plan: "zimmer",
			Refresh: Duration(10 * time.Minute)}}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.October, 21, 8, 0, 0, 0, time.UTC) // Wednesday
	s.now = func() time.Time { return now }
	return s, &now
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

func planDevice(t *testing.T, s *Server) map[string]string {
	t.Helper()
	key, _ := call(t, s, "/api/setup", map[string]string{"ID": planDeviceMAC})["api_key"].(string)
	if key == "" {
		t.Fatal("no api key")
	}
	return map[string]string{"ID": planDeviceMAC, "Access-Token": key, "Update-Source": "timer"}
}

func displayPage(t *testing.T, s *Server, dev map[string]string) map[string]any {
	t.Helper()
	return call(t, s, "/api/display", dev)
}

func pagePath(out map[string]any) string {
	return out["filename"].(string)
}

func fetchImage(t *testing.T, s *Server, imageURL string) *httptest.ResponseRecorder {
	t.Helper()
	u, err := url.Parse(imageURL)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, u.RequestURI(), nil))
	return rec
}

func TestPlanServesRenderedPage(t *testing.T) {
	s, _ := planServer(t, testPlan)
	dev := planDevice(t, s)
	out := displayPage(t, s, dev)
	if got := pagePath(out); !strings.HasPrefix(got, "pages/small/eins/") {
		t.Fatalf("first page: %q", got)
	}
	if out["refresh_rate"].(float64) != 600 {
		t.Fatalf("refresh: %v", out["refresh_rate"])
	}
	rec := fetchImage(t, s, out["image_url"].(string))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("image: %d %v", rec.Code, rec.Header())
	}
	if !strings.HasPrefix(rec.Body.String(), "\x89PNG") {
		t.Fatal("not a PNG")
	}
	// Unknown rendered pages are 404, never 500.
	if rec := fetchImage(t, s, "http://example.com/pages/small/unbekannt/000000000000.png"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown rendered page: %d", rec.Code)
	}
}

func TestPlanRuleSwitchAndKeys(t *testing.T) {
	s, now := planServer(t, testPlan)
	dev := planDevice(t, s)
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/eins/") {
		t.Fatalf("morning page: %q", got)
	}
	dev["Update-Source"] = "EXT1"
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/zwei/") {
		t.Fatalf("button must page: %q", got)
	}
	// Hold: first page of the rule.
	dev["special_function"] = "true"
	out := displayPage(t, s, dev)
	if !strings.Contains(pagePath(out), "/eins/") || out["action"] != "restart_playlist" {
		t.Fatalf("hold: %v", out)
	}
	delete(dev, "special_function")

	// 09:00 starts another rule: first page, even though the device paged before.
	dev["Update-Source"] = "EXT1"
	displayPage(t, s, dev) // eins -> zwei
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/eins/") {
		t.Fatalf("wrap around: %q", got)
	}
	*now = time.Date(2026, time.October, 21, 9, 0, 0, 0, time.UTC)
	dev["Update-Source"] = "timer"
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
	dev["Update-Source"] = "EXT1"
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/drei/") {
		t.Fatalf("button in rotation: %q", got)
	}
	dev["Update-Source"] = "timer"
	*now = now.Add(5 * time.Minute)
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/vier/") {
		t.Fatalf("rotation after button: %q", got)
	}
}

func TestPlanPageTimeout(t *testing.T) {
	s, now := planServer(t, testPlan)
	dev := planDevice(t, s)
	displayPage(t, s, dev) // eins
	dev["Update-Source"] = "EXT1"
	displayPage(t, s, dev) // zwei
	dev["Update-Source"] = "timer"
	*now = now.Add(6 * time.Minute) // past page_timeout (5m)
	if got := pagePath(displayPage(t, s, dev)); !strings.Contains(got, "/eins/") {
		t.Fatalf("back to the first page after the timeout: %q", got)
	}
}

func TestPlanRefreshCappedAtBoundary(t *testing.T) {
	s, now := planServer(t, testPlan)
	dev := planDevice(t, s)
	*now = time.Date(2026, time.October, 21, 8, 59, 0, 0, time.UTC)
	if got := displayPage(t, s, dev)["refresh_rate"].(float64); got != 60 {
		t.Fatalf("wake at the rule change: %v", got)
	}
	*now = time.Date(2026, time.October, 21, 18, 59, 0, 0, time.UTC)
	if got := displayPage(t, s, dev)["refresh_rate"].(float64); got != 60 {
		t.Fatalf("wake at the evening change: %v", got)
	}
	*now = time.Date(2026, time.October, 21, 20, 0, 0, 0, time.UTC)
	if got := displayPage(t, s, dev)["refresh_rate"].(float64); got != 1800 {
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
	if got := pagePath(out); !strings.HasPrefix(got, "hints/") {
		t.Fatalf("missing template must show a notice: %q", got)
	}
	if rec := fetchImage(t, s, out["image_url"].(string)); rec.Code != http.StatusOK {
		t.Fatalf("notice image: %d", rec.Code)
	}
	// A missing plan file also yields a notice page.
	s2, _ := planServer(t, testPlan)
	s2.cfg.Devices[0].Plan = "gibtsnicht"
	dev2 := planDevice(t, s2)
	out = displayPage(t, s2, dev2)
	if got := pagePath(out); !strings.HasPrefix(got, "hints/") {
		t.Fatalf("missing plan must show a notice: %q", got)
	}
	if out["refresh_rate"].(float64) != hintRefresh.Seconds() {
		t.Fatalf("notice refresh: %v", out["refresh_rate"])
	}
}

func TestPlanRerendersOnDataChange(t *testing.T) {
	s, _ := planServer(t, `
rules:
  - name: tag
    pages: [daten]
`)
	writePageFile(t, filepath.Join(s.cfg.Pages, "daten.small.yaml"),
		"rows: [1]\nblocks:\n  - text: \"Akku {{ percent .akku.soc }}\"\n")
	dev := planDevice(t, s)
	first := pagePath(displayPage(t, s, dev))
	if code, body := dataCall(t, s, http.MethodPut, "/api/data/akku", `{"soc":50}`); code != http.StatusNoContent {
		t.Fatalf("put data: %d %s", code, body)
	}
	second := pagePath(displayPage(t, s, dev))
	if first == second {
		t.Fatalf("changed data must change the filename: %q", first)
	}
	if rec := fetchImage(t, s, "http://example.com/"+second+".png"); rec.Code != http.StatusOK {
		t.Fatalf("image after data change: %d", rec.Code)
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
