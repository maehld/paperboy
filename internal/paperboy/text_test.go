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

// textServer starts a server with one 8x2 text display that uses the plan.
func textServer(t *testing.T, planYAML string) (*Server, *time.Time) {
	t.Helper()
	dir := t.TempDir()
	pages := filepath.Join(dir, "pages")
	writePageFile(t, filepath.Join(pages, "eins.lcd.yaml"), "lines: [\"PV {{ .solar.pv }}\", \"Größe\"]\n")
	writePageFile(t, filepath.Join(pages, "zwei.lcd.yaml"), "lines: [zwei]\n")
	writePageFile(t, filepath.Join(pages, "kaputt.lcd.yaml"), "lines: [a, b, c]\n")
	if planYAML != "" {
		writePageFile(t, filepath.Join(dir, "plans", "zimmer.yaml"), planYAML)
	}
	dev := Device{ID: "lcd", MAC: planDeviceMAC, Profile: "lcd", Refresh: Duration(time.Minute)}
	if planYAML != "" {
		dev.Plan = "zimmer"
	}
	cfg := &Config{Dir: dir, UploadToken: testToken, Timezone: time.UTC, Language: "de",
		Profiles: map[string]Profile{"lcd": {Kind: kindText, Charset: "ascii", Width: 8, Height: 2}},
		Devices:  []Device{dev}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.October, 21, 8, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	return s, &now
}

func pollText(t *testing.T, s *Server, key, etag string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/device/text", nil)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

const textPlan = `
rules:
  - name: immer
    when: { from: "06:00", to: "22:00" }
    pages: [eins, zwei, kaputt]
fallback: immer
`

func TestTextDeviceSyncAndPoll(t *testing.T) {
	s, _ := textServer(t, textPlan)
	if err := s.doc.Put("solar/pv", 42, time.Time{}); err != nil {
		t.Fatal(err)
	}
	dev := planDevice(t, s)
	out := displayPage(t, s, dev)
	if out["image"] != nil {
		t.Fatalf("text display got an image: %v", out)
	}
	text, _ := out["text"].(map[string]any)
	lines, _ := text["lines"].([]any)
	if len(lines) != 2 || lines[0] != "PV 42   " || lines[1] != "Groesse " {
		t.Fatalf("sync text: %v", out)
	}
	if out["listen_s"].(float64) != 0 {
		t.Fatalf("listen_s: %v", out["listen_s"])
	}

	if rec := pollText(t, s, "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("poll without key: %d", rec.Code)
	}
	rec := pollText(t, s, dev.key, "")
	var polled syncText
	if err := json.Unmarshal(rec.Body.Bytes(), &polled); rec.Code != http.StatusOK || err != nil {
		t.Fatalf("poll: %d %s", rec.Code, rec.Body)
	}
	if polled.Hash != text["hash"] || rec.Header().Get("ETag") != `"`+polled.Hash+`"` {
		t.Fatalf("poll hash %q etag %q, sync %v", polled.Hash, rec.Header().Get("ETag"), text["hash"])
	}
	if rec := pollText(t, s, dev.key, `"`+polled.Hash+`"`); rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Fatalf("unchanged poll: %d %s", rec.Code, rec.Body)
	}
	// New data changes the lines without a sync.
	if err := s.doc.Put("solar/pv", 7, time.Time{}); err != nil {
		t.Fatal(err)
	}
	rec = pollText(t, s, dev.key, `"`+polled.Hash+`"`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"PV 7    "`) {
		t.Fatalf("changed poll: %d %s", rec.Code, rec.Body)
	}

	// Polling never advances the cursor or writes the state file.
	before, _ := os.ReadFile(s.devicesPath)
	pollText(t, s, dev.key, "")
	after, _ := os.ReadFile(s.devicesPath)
	if string(before) != string(after) {
		t.Fatal("poll wrote the state file")
	}

	// A button press pages like on ePaper displays; the poll follows.
	dev.req.Reason = "button"
	dev.req.Events = []syncEvent{{Type: "button", Button: buttonNext}}
	displayPage(t, s, dev)
	if rec := pollText(t, s, dev.key, ""); !strings.Contains(rec.Body.String(), `"zwei    "`) {
		t.Fatalf("poll after next: %s", rec.Body)
	}
	// A broken template becomes notice lines and the device's last error.
	dev.req.Events = []syncEvent{{Type: "button", Button: buttonNext}}
	out = displayPage(t, s, dev)
	if !strings.Contains(strings.Join(anyStrings(out["text"].(map[string]any)["lines"]), "|"), "Seite") {
		t.Fatalf("notice lines: %v", out)
	}
	info := s.deviceInfos()[0]
	if info.LastError == "" || info.LastPageURL != "" || len(info.LastText) != 2 {
		t.Fatalf("device info: %+v", info)
	}
	ui := httptest.NewRecorder()
	s.Handler().ServeHTTP(ui, httptest.NewRequest(http.MethodGet, "/ui", nil))
	if !strings.Contains(ui.Body.String(), `<pre class="lcd">`) {
		t.Fatalf("dashboard shows no text: %s", ui.Body)
	}
}

func TestTextDeviceWithoutPlan(t *testing.T) {
	s, _ := textServer(t, "")
	dev := planDevice(t, s)
	rec := pollText(t, s, dev.key, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Kein") {
		t.Fatalf("no plan: %d %s", rec.Code, rec.Body)
	}
}

func TestTextPollRejectsImageDisplay(t *testing.T) {
	s, _ := planServer(t, testPlan)
	dev := planDevice(t, s)
	if rec := pollText(t, s, dev.key, ""); rec.Code != http.StatusConflict {
		t.Fatalf("image display poll: %d", rec.Code)
	}
}

func TestTextPreview(t *testing.T) {
	s, _ := textServer(t, "")
	rec := preview(t, s, "/api/preview/lcd/zwei", true)
	if rec.Code != http.StatusOK || rec.Body.String() != "zwei    \n        \n" ||
		!strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("preview: %d %q", rec.Code, rec.Body)
	}
}

func anyStrings(v any) []string {
	list, _ := v.([]any)
	out := make([]string, len(list))
	for i, s := range list {
		out[i], _ = s.(string)
	}
	return out
}
