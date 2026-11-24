package paperboy

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// writeDeviceFile writes devices/<id>.yaml into an instance directory (ADR 0070).
func writeDeviceFile(t *testing.T, dir, id, content string) {
	t.Helper()
	path := filepath.Join(dir, devicesDirName, id+".yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDeviceFileRoundTrip verifies that a device file survives parse and encode
// unchanged, that the encoder keeps the operator's layout and leaves unset
// fields out, and that parsing is strict.
func TestDeviceFileRoundTrip(t *testing.T) {
	src := `display_type: e1001
protocol: paperboy
mac: "AA:BB:CC:DD:EE:01"
plan: standard
refresh: 10m
power: mains
actions: deny
rotation: 180
battery: { warn_volts: 3.5, critical_volts: 3.3 }
alert_methods: [beep, blink]
firmware: hold
`
	f, err := parseDeviceFile([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	data, err := encodeDeviceFile(&f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "refresh: 10m\n") ||
		!strings.Contains(string(data), "rotation: 180\n") || !strings.Contains(string(data), "alert_methods: [beep, blink]\n") {
		t.Fatalf("durations, rotation and alert methods:\n%s", data)
	}
	again, err := parseDeviceFile(data)
	if err != nil {
		t.Fatalf("written device:\n%s\n%v", data, err)
	}
	if !reflect.DeepEqual(f, again) {
		t.Fatalf("round trip changed the model:\n%+v\n%+v\n%s", f, again, data)
	}

	// A minimal file writes only what it needs, and the default protocol is
	// filled in.
	minimal, err := encodeDeviceFile(&deviceFile{DisplayType: "sticky", MAC: "AA:BB:CC:DD:EE:02"})
	if err != nil {
		t.Fatal(err)
	}
	want := "display_type: sticky\nprotocol: paperboy\nmac: 'AA:BB:CC:DD:EE:02'\n"
	if string(minimal) != want {
		t.Fatalf("minimal device file:\n%s\nwant:\n%s", minimal, want)
	}

	// Unknown keys and a second document are errors, like in pages and plans.
	for _, bad := range []string{"color: red\n", "display_type: sticky\n---\nmac: x\n", ""} {
		if _, err := parseDeviceFile([]byte(bad)); err == nil {
			t.Fatalf("%q parses as a device", bad)
		}
	}
}

// TestAlertMethodsFile checks the list forms of alert_methods (ADR 0132):
// omitted keeps the default, empty mutes, and bad entries are rejected.
func TestAlertMethodsFile(t *testing.T) {
	const base = "display_type: e1001\nmac: \"aa:bb:cc:dd:ee:01\"\n"
	omitted, err := parseDeviceFile([]byte(base))
	if err != nil {
		t.Fatal(err)
	}
	if omitted.AlertMethods != nil {
		t.Fatalf("omitted key must stay nil: %+v", omitted.AlertMethods)
	}
	empty, err := parseDeviceFile([]byte(base + "alert_methods: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	if empty.AlertMethods == nil || len(*empty.AlertMethods) != 0 {
		t.Fatalf("empty list lost: %+v", empty.AlertMethods)
	}
	beep, err := parseDeviceFile([]byte(base + "alert_methods: [beep]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if beep.AlertMethods == nil || len(*beep.AlertMethods) != 1 || (*beep.AlertMethods)[0] != alertBeep {
		t.Fatalf("beep list: %+v", beep.AlertMethods)
	}
	for _, bad := range []string{"alert_methods: [beep, beep]\n", "alert_methods: [bell]\n"} {
		f, err := parseDeviceFile([]byte(base + bad))
		if err != nil {
			t.Fatal(err)
		}
		if err := f.validate("kueche"); err == nil {
			t.Fatalf("%q passes validation", bad)
		}
	}

	// The encoder omits the default and keeps an explicit empty list.
	data, err := encodeDeviceFile(&deviceFile{DisplayType: "e1001", MAC: "AA:BB:CC:DD:EE:01"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "alert_methods") {
		t.Fatalf("default must write no key:\n%s", data)
	}
	data, err = encodeDeviceFile(&deviceFile{DisplayType: "e1001", MAC: "AA:BB:CC:DD:EE:01", AlertMethods: &[]string{}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "alert_methods: []\n") {
		t.Fatalf("empty list:\n%s", data)
	}
}

// TestDeviceFileValidation verifies what the file alone decides and what needs
// the configuration.
func TestDeviceFileValidation(t *testing.T) {
	f, err := parseDeviceFile([]byte("display_type: e1001\nmac: \"aa:bb:cc:dd:ee:01\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &Config{Profiles: map[string]Profile{"e1001": {Width: 800, Height: 480, PPI: 125, Power: powerAuto}},
		Refresh: Duration(5 * time.Minute)}
	dev := f.device("kueche", nil)
	if err := cfg.validateDevice(&dev); err != nil {
		t.Fatal(err)
	}
	if dev.MAC != "AA:BB:CC:DD:EE:01" || dev.Profile != "e1001" || dev.Type != TypePaperboy ||
		dev.Refresh != Duration(5*time.Minute) || dev.Actions != actionsAllow {
		t.Fatalf("device defaults: %+v", dev)
	}

	removed, err := parseDeviceFile([]byte("display_type: e1001\nmac: \"aa:bb:cc:dd:ee:01\"\nmenu_show_duration: 45s\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := removed.validate("kueche"); err == nil || !strings.Contains(err.Error(), "removed") {
		t.Fatalf("menu_show_duration: %v", err)
	}

	hold, _ := parseDeviceFile([]byte("display_type: e1001\nmac: \"aa:bb:cc:dd:ee:01\"\nfirmware: pin\n"))
	if err := hold.validate("kueche"); err == nil || !strings.Contains(err.Error(), "only hold") {
		t.Fatalf("firmware pin: %v", err)
	}
	unknown, _ := parseDeviceFile([]byte("display_type: fehlt\nmac: \"aa:bb:cc:dd:ee:01\"\n"))
	dev = unknown.device("kueche", nil)
	if err := cfg.validateDevice(&dev); err == nil || !strings.Contains(err.Error(), "unknown display_type") {
		t.Fatalf("unknown display type: %v", err)
	}

	// A rotation is an override of the display type and accepts only quarter
	// turns; a text display has no pixels to turn (ADR 0102).
	rotated, _ := parseDeviceFile([]byte("display_type: e1001\nmac: \"aa:bb:cc:dd:ee:01\"\nrotation: 90\n"))
	dev = rotated.device("kueche", nil)
	if err := cfg.validateDevice(&dev); err != nil {
		t.Fatalf("rotation 90: %v", err)
	}
	if dev.Rotation == nil || *dev.Rotation != 90 {
		t.Fatalf("rotation lost: %+v", dev)
	}
	badRotation, _ := parseDeviceFile([]byte("display_type: e1001\nmac: \"aa:bb:cc:dd:ee:01\"\nrotation: 45\n"))
	dev = badRotation.device("kueche", nil)
	if err := cfg.validateDevice(&dev); err == nil || !strings.Contains(err.Error(), "rotation must be 0, 90, 180 or 270") {
		t.Fatalf("rotation 45: %v", err)
	}
	cfg.Profiles["lcd"] = Profile{Kind: kindText, Width: 20, Height: 4, Charset: "utf8", Power: powerAuto}
	textRotation, _ := parseDeviceFile([]byte("display_type: lcd\nmac: \"aa:bb:cc:dd:ee:01\"\nrotation: 90\n"))
	dev = textRotation.device("kueche", nil)
	if err := cfg.validateDevice(&dev); err == nil || !strings.Contains(err.Error(), "does not apply to text displays") {
		t.Fatalf("rotation on text: %v", err)
	}
}

// TestDeviceWithMissingDisplayTypeAnswers503: a device whose display type is
// not configured stays configured (ADR 0079). It still registers, and sync and
// text poll are answered with 503 rather than 401, so the firmware keeps its key
// while the operator repairs the file.
func TestDeviceWithMissingDisplayTypeAnswers503(t *testing.T) {
	cfg := &Config{Dir: t.TempDir(), Profiles: map[string]Profile{"small": {Width: 40, Height: 20, PPI: 125}},
		Devices: []Device{{ID: "test", MAC: "aa:bb:cc:dd:ee:01", Profile: "gone", Refresh: Duration(10 * time.Minute)}}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Devices[0].Problem == "" {
		t.Fatalf("device problem missing: %+v", cfg.Devices[0])
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	pair(t, s, "aa:bb:cc:dd:ee:01")
	rec := register(t, s, `{"mac":"aa:bb:cc:dd:ee:01","hardware":"sticky"}`)
	var reg struct{ Key, Device string }
	if err := json.Unmarshal(rec.Body.Bytes(), &reg); rec.Code != http.StatusOK || err != nil || reg.Key == "" {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	if rec := syncRaw(t, s, reg.Key, `{"reason":"timer"}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("sync: %d %s", rec.Code, rec.Body)
	}
	if rec := pollText(t, s, reg.Key, ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("text poll: %d %s", rec.Code, rec.Body)
	}
	// The contact is still recorded, so the dashboard shows the device as alive.
	if st := s.state["AABBCCDDEE01"]; st == nil || st.LastSeen.IsZero() {
		t.Fatalf("state: %+v", st)
	}
	// The devices API and the dashboard name the reason.
	info := s.deviceInfos()
	if len(info) != 1 || !strings.Contains(info[0].Problem, `"gone"`) {
		t.Fatalf("device info: %+v", info)
	}
	body := request(t, s, http.MethodGet, "/ui", nil, nil).Body.String()
	if !strings.Contains(body, "display type &#34;gone&#34; is not configured") {
		t.Fatalf("dashboard lacks the problem:\n%s", body)
	}
}

// TestDeviceAPI covers read, create, save, conflicts and validation of the
// device API (ADR 0066, 0070).
func TestDeviceAPI(t *testing.T) {
	s := editorServer(t)
	dir := s.config().DevicesDir

	if rec := request(t, s, http.MethodGet, "/api/devices/kueche", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing device: %d", rec.Code)
	}
	model := &deviceFile{DisplayType: "small", MAC: "aa:bb:cc:dd:ee:02", Plan: "tour", Refresh: Duration(10 * time.Minute)}
	var created editorDevice
	rec := request(t, s, http.MethodPut, "/api/devices/kueche", deviceSave{Device: model}, &created)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if created.Hash == "" || created.Path != "devices/kueche.yaml" || created.Device == nil || created.Device.Refresh != Duration(10*time.Minute) {
		t.Fatalf("created: %+v", created)
	}
	data, err := os.ReadFile(filepath.Join(dir, "kueche.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "refresh: 10m") || !strings.Contains(string(data), "display_type: small") {
		t.Fatalf("file:\n%s", data)
	}

	var read editorDevice
	if rec := request(t, s, http.MethodGet, "/api/devices/kueche", nil, &read); rec.Code != http.StatusOK ||
		read.Hash != created.Hash || read.Device == nil || read.Device.MAC != "aa:bb:cc:dd:ee:02" {
		t.Fatalf("read: %d %+v %s", rec.Code, read, rec.Body)
	}

	// A second save keeps a backup under state/editor; a stale hash is a
	// conflict afterwards.
	edited := *read.Device
	edited.Plan = "standard"
	if rec := request(t, s, http.MethodPut, "/api/devices/kueche", deviceSave{Hash: read.Hash, Device: &edited}, nil); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	if saved, err := os.ReadFile(filepath.Join(dir, "kueche.yaml")); err != nil || !strings.Contains(string(saved), "plan: standard") {
		t.Fatalf("saved file: %s %v", saved, err)
	}
	backups, err := filepath.Glob(filepath.Join(s.config().Dir, "state", "editor", "devices", "kueche", "*.yaml"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups: %v %v", backups, err)
	}
	if rec := request(t, s, http.MethodPut, "/api/devices/kueche", deviceSave{Hash: read.Hash, Device: &edited}, nil); rec.Code != http.StatusConflict {
		t.Fatalf("stale hash: %d %s", rec.Code, rec.Body)
	}

	// The API rejects what a reload would reject: an unknown display type, a
	// MAC that another device already uses, an invalid MAC and unknown fields.
	bad := edited
	bad.DisplayType = "nope"
	if rec := request(t, s, http.MethodPut, "/api/devices/kueche", deviceSave{Device: &bad}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown display type: %d", rec.Code)
	}
	bad = edited
	bad.MAC = "aa:bb:cc:dd:ee:01" // the test device of editorServer
	if rec := request(t, s, http.MethodPut, "/api/devices/kueche", deviceSave{Device: &bad}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("duplicate MAC: %d %s", rec.Code, rec.Body)
	}
	bad = edited
	bad.MAC = "nope"
	if rec := request(t, s, http.MethodPut, "/api/devices/kueche", deviceSave{Device: &bad}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid MAC: %d", rec.Code)
	}
	if rec := request(t, s, http.MethodPut, "/api/devices/kueche", `{"device":{"display_type":"small","mac":"aa:bb:cc:dd:ee:03"},"nope":1}`, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", rec.Code)
	}
	if rec := request(t, s, http.MethodPut, "/api/devices/Not%20An%20ID", deviceSave{Device: &edited}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: %d", rec.Code)
	}
	// The dashboard device list is a different endpoint and still answers.
	if rec := request(t, s, http.MethodGet, "/api/devices", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("device list: %d", rec.Code)
	}
}
