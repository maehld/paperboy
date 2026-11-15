package paperboy

import (
	"encoding/json"
	"fmt"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/internal/page"
	"github.com/maehld/paperboy/pkg/plugin"
)

// textServer starts a server with one 8x2 text display that uses the plan.
func textServer(t *testing.T, planYAML string) (*Server, *time.Time) {
	t.Helper()
	dir := t.TempDir()
	pages := filepath.Join(dir, "pages")
	writePageFile(t, filepath.Join(pages, "eins.yaml"), `
title: Eins
kind: text
cells:
  - { at: [0, 0], span: [8, 1], value: value/pv }
  - { at: [0, 1], span: [8, 1], value: paperboy/title }
`)
	writePageFile(t, filepath.Join(pages, "zwei.yaml"), "title: Zwei\nkind: text\ncells:\n  - { at: [0, 0], span: [8, 1], value: paperboy/title }\n")
	writePageFile(t, filepath.Join(pages, "kaputt.yaml"), "kind: text\ncells:\n  - { at: [0, 0], span: [8, 1], value: paperboy/title, colour: red }\n")
	if planYAML != "" {
		writePageFile(t, filepath.Join(dir, "plans", "zimmer.yaml"), planYAML)
	}
	dev := Device{ID: "lcd", MAC: planDeviceMAC, Profile: "lcd", Refresh: Duration(time.Minute)}
	if planYAML != "" {
		dev.Plan = "zimmer"
	}
	cfg := &Config{Dir: dir, Timezone: time.UTC, Language: "de",
		Profiles: map[string]Profile{"lcd": {Kind: kindText, Charset: "ascii", Width: 8, Height: 2, Power: powerMains}},
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
	s.widgets.Builtin["value"] = &valuePlugin{}
	return s, &now
}

// valuePlugin is a built-in test plugin with one value "PV <value>".
type valuePlugin struct{ value int }

func (*valuePlugin) Describe() plugin.Description {
	return plugin.Description{Values: []plugin.ValueSpec{{Name: "pv", Type: plugin.ValueText}}}
}
func (*valuePlugin) Configure(plugin.Values, *plugin.Request) error { return nil }
func (*valuePlugin) Render(*plugin.Request, plugin.WidgetRequest, *image.Paletted) error {
	return nil
}
func (v *valuePlugin) Values(req *plugin.Request) (map[string]plugin.Value, error) {
	out := map[string]plugin.Value{}
	for _, name := range req.Values {
		if name != "pv" {
			return nil, fmt.Errorf("%w %q", plugin.ErrUnknownValue, name)
		}
		out[name] = plugin.TextValue(fmt.Sprintf("PV %d", v.value))
	}
	return out, nil
}

func pollText(t *testing.T, s *Server, key, shown string) *httptest.ResponseRecorder {
	t.Helper()
	path := "/api/device/text"
	if shown != "" {
		path += "?shown=" + shown
	}
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
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
    refresh: 2s
fallback: immer
`

func TestTextDeviceSyncAndPoll(t *testing.T) {
	s, _ := textServer(t, textPlan)
	value := s.widgets.Builtin["value"].(*valuePlugin)
	value.value = 42
	dev := planDevice(t, s)
	out := displayPage(t, s, dev)
	if out["image"] != nil {
		t.Fatalf("text display got an image: %v", out)
	}
	text, _ := out["text"].(map[string]any)
	lines, _ := text["lines"].([]any)
	if len(lines) != 2 || lines[0] != "PV 42   " || lines[1] != "Eins    " {
		t.Fatalf("sync text: %v", out)
	}
	if _, ok := text["scroll"]; ok {
		t.Fatalf("a page without a scrolling cell carries no scroll field: %v", text)
	}
	if out["refresh_s"].(float64) != 2 {
		t.Fatalf("refresh_s from the plan: %v", out["refresh_s"])
	}
	if out["listen_s"].(float64) != 0 || out["sleep"] != false {
		t.Fatalf("listen_s/sleep: %v %v", out["listen_s"], out["sleep"])
	}

	if rec := pollText(t, s, "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("poll without key: %d", rec.Code)
	}
	rec := pollText(t, s, dev.key, "")
	var polled syncText
	if err := json.Unmarshal(rec.Body.Bytes(), &polled); rec.Code != http.StatusOK || err != nil {
		t.Fatalf("poll: %d %s", rec.Code, rec.Body)
	}
	if polled.Hash != text["hash"] {
		t.Fatalf("poll hash %q, sync %v", polled.Hash, text["hash"])
	}
	if rec := pollText(t, s, dev.key, polled.Hash); rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("unchanged poll: %d %s", rec.Code, rec.Body)
	}
	// A changed widget changes the lines without a sync.
	value.value = 7
	rec = pollText(t, s, dev.key, polled.Hash)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"PV 7    "`) {
		t.Fatalf("changed poll: %d %s", rec.Code, rec.Body)
	}
	if info := s.deviceInfos()[0]; len(info.LastText) == 0 || info.LastText[0] != "PV 7    " {
		t.Fatalf("dashboard text not updated by the poll: %q", info.LastText)
	}

	// Polling never advances the cursor or writes the state file.
	statePath := deviceStateFile(s.devicesDir, s.config().Devices[0].stateKey())
	before, _ := os.ReadFile(statePath)
	pollText(t, s, dev.key, "")
	after, _ := os.ReadFile(statePath)
	if string(before) != string(after) {
		t.Fatal("poll wrote the state file")
	}

	// A button press pages like on ePaper displays; the poll follows.
	dev.req.Reason = "button"
	dev.req.Events = []syncEvent{{Type: "button", Button: buttonNext}}
	displayPage(t, s, dev)
	if rec := pollText(t, s, dev.key, ""); !strings.Contains(rec.Body.String(), `"Zwei    "`) {
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
	rec := preview(t, s, "/api/preview/lcd/zwei")
	if rec.Code != http.StatusOK || rec.Body.String() != "Zwei    \n        \n" ||
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

// Text displays run on mains power and report no battery; they still get
// firmware offers.
func TestTextDeviceFirmwareOffer(t *testing.T) {
	s, _ := textServer(t, textPlan)
	s.config().Firmware = FirmwareConfig{Update: firmwareAuto, MinBatteryVolts: 3.6}
	writePageFile(t, filepath.Join(s.config().FirmwareDir, "lcd2004-v0.2.1.bin"), "image")
	dev := planDevice(t, s)
	dev.req.Hardware, dev.req.Firmware = "lcd2004", "0.2.0"
	offer := offerOf(displayPage(t, s, dev))
	if offer["version"] != "0.2.1" {
		t.Fatalf("no offer for a text display: %v", offer)
	}
}

// TestPlanSkipsPagesForOtherDisplays verifies that a plan shared by image and
// text displays skips pages that do not fit the device (ADR 0049): an image
// page without a text variant and a text page with the wrong grid.
func TestPlanSkipsPagesForOtherDisplays(t *testing.T) {
	s, _ := textServer(t, "rules: [{name: immer, pages: [bild, schmal, zwei]}]\n")
	writePageFile(t, filepath.Join(s.config().Pages, "bild.yaml"), "kind: image\ngrid: [8, 2]\ncells: [{widget: paperboy/text, args: {text: bild}}]\n")
	writePageFile(t, filepath.Join(s.config().Pages, "schmal.yaml"), "kind: text\ncells:\n  - { at: [0, 0], span: [9, 1], value: paperboy/title }\n")
	dev := planDevice(t, s)
	out := displayPage(t, s, dev)
	if lines := anyStrings(out["text"].(map[string]any)["lines"]); lines[0] != "Zwei    " {
		t.Fatalf("first fitting page: %q", lines)
	}
	dev.req.Reason = "button"
	dev.req.Events = []syncEvent{{Type: "button", Button: buttonNext}}
	if lines := anyStrings(displayPage(t, s, dev)["text"].(map[string]any)["lines"]); lines[0] != "Zwei    " {
		t.Fatalf("paging must stay on the only fitting page: %q", lines)
	}
	if len(s.skipNotes) != 2 {
		t.Fatalf("skip notes: %v", s.skipNotes)
	}

	writePageFile(t, filepath.Join(s.config().Plans, "zimmer.yaml"), "rules: [{name: bilder, pages: [bild]}]\n")
	stamp := time.Now().Add(time.Minute)
	os.Chtimes(filepath.Join(s.config().Plans, "zimmer.yaml"), stamp, stamp)
	lines := strings.Join(anyStrings(displayPage(t, s, dev)["text"].(map[string]any)["lines"]), "|")
	if !strings.Contains(lines, "passende") {
		t.Fatalf("no fitting page must show a notice: %q", lines)
	}
}

func TestHashTextCoversScroll(t *testing.T) {
	static := []string{"Lange Ze", "zwei    "}
	a := []page.ScrollBox{{At: [2]int{0, 0}, Span: [2]int{8, 1}, Lines: []string{"Lange Zeile A"}, Direction: page.ScrollHorizontal}}
	b := []page.ScrollBox{{At: [2]int{0, 0}, Span: [2]int{8, 1}, Lines: []string{"Lange Zeile B"}, Direction: page.ScrollHorizontal}}
	if hashText(static, nil) != hashText(static, []page.ScrollBox{}) {
		t.Error("a page without scrolling must keep its hash")
	}
	if hashText(static, a) == hashText(static, b) {
		t.Error("a change behind the visible columns must change the hash")
	}
	if hashText(static, a) == hashText(static, nil) {
		t.Error("a scrolling box must change the hash")
	}
}

// TestTextResponseCarriesScrollFields pins the additive fields a text display
// reads: the box geometry, its full lines and window, the direction, the step
// and the alignment. scroll_changed stays false while that text is unchanged.
func TestTextResponseCarriesScrollFields(t *testing.T) {
	served := servedPage{
		lines: []string{"abc     ", "def     "},
		scrolls: []page.ScrollBox{{
			At: [2]int{1, 0}, Span: [2]int{4, 2},
			Lines: []string{"one", "two", "three"}, Window: []string{"1one", "2two"},
			Direction: page.ScrollVertical, Align: "right",
		}},
		hash: "h1",
	}
	profile := Profile{Kind: kindText, Charset: "ascii", Width: 8, Height: 2, ScrollStep: 300 * time.Millisecond}
	state := &deviceState{}
	out := textResponse(served, profile, state)
	if len(out.Scrolls) != 1 {
		t.Fatalf("scrolls: %+v", out.Scrolls)
	}
	box := out.Scrolls[0]
	if box.Row != 0 || box.Col != 1 || box.Width != 4 || box.Height != 2 ||
		box.StepMS != 300 || box.Direction != page.ScrollVertical || box.Align != "right" ||
		len(box.Text) != 3 || len(box.Window) != 2 {
		t.Fatalf("scroll box: %+v", box)
	}
	if !out.ScrollChanged {
		t.Fatal("the first answer must reset the offset")
	}
	if again := textResponse(served, profile, state); again.ScrollChanged {
		t.Fatal("the same scroll text must keep the offset")
	}
}
