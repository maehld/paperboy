package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
)

func f(v float64) *float64 { return &v }

// testSnapshot is one moment of a Vitocal 250-A with two circuits, hot water
// and a heat pump compressor.
func testSnapshot() *snapshot {
	return &snapshot{
		Updated: time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC),
		Outside: f(4.2),
		Boiler:  f(52.0),
		Return:  f(38.0),
		Circuits: []circuit{
			{Index: 0, Room: f(21.5), Supply: f(41.8), Target: f(45.2),
				Program: "normal", Mode: "dhwAndHeating", Curve: &curve{Slope: 0.6, Shift: 4}},
			{Index: 1, Room: f(19.0), Supply: f(31.0), Target: f(33.4), Program: "reduced"},
		},
		DHW:        &dhw{Temp: f(48.0), Target: f(50.0), Charging: true, Mode: "balanced"},
		Compressor: &compressor{Active: true, Hours: f(13651.9), Starts: f(4012)},
	}
}

// testPlugin returns a plugin with a prepared snapshot, as Prepare leaves it.
func testPlugin(t *testing.T, lang string) *vicare {
	t.Helper()
	v := &vicare{
		client: &http.Client{}, clientID: "client", seedRefresh: "seed",
		base: defaultBase, iam: defaultIAM, refresh: 10 * time.Minute,
		model: "Vitocal 250-A", roles: []string{"type:heatpump"},
	}
	v.req = &plugin.Request{Language: lang, Timezone: "Europe/Berlin",
		Now: time.Date(2026, 11, 28, 10, 30, 0, 0, time.UTC)}
	v.loc, v.lang, v.cur = v.req.Location(), lang, testSnapshot()
	v.curModel, v.curRoles = v.model, v.roles
	return v
}

func TestConfigureChecksTheKeys(t *testing.T) {
	v := &vicare{}
	if err := v.Configure(plugin.Values{"client_id": "c", "refresh_token": "r"}, &plugin.Request{Version: 10}); err != nil {
		t.Fatal(err)
	}
	if v.clientID != "c" || v.seedRefresh != "r" || v.refresh != 10*time.Minute {
		t.Fatalf("configured %+v", v)
	}

	cases := []struct {
		name string
		cfg  plugin.Values
		want string
	}{
		{"missing client", plugin.Values{"refresh_token": "r"}, "client_id"},
		{"missing token", plugin.Values{"client_id": "c"}, "refresh_token"},
		{"empty client", plugin.Values{"client_id": " ", "refresh_token": "r"}, "client_id"},
		{"unknown key", plugin.Values{"client_id": "c", "refresh_token": "r", "vin": "x"}, "vin"},
		{"bad refresh", plugin.Values{"client_id": "c", "refresh_token": "r", "refresh": "soon"}, "refresh"},
	}
	for _, c := range cases {
		if err := (&vicare{}).Configure(c.cfg, &plugin.Request{Version: 10}); err == nil ||
			!strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v", c.name, err)
		}
	}
}

func TestConfigureRejectsAnOldProtocol(t *testing.T) {
	err := (&vicare{}).Configure(plugin.Values{"client_id": "c", "refresh_token": "r"}, &plugin.Request{Version: 8})
	if err == nil || !strings.Contains(err.Error(), "protocol 9") {
		t.Fatalf("err = %v", err)
	}
}

func TestConfigureRaisesAShortRefresh(t *testing.T) {
	v := &vicare{}
	if err := v.Configure(plugin.Values{"client_id": "c", "refresh_token": "r", "refresh": "1m"},
		&plugin.Request{Version: 10}); err != nil {
		t.Fatal(err)
	}
	if v.refresh != minRefresh {
		t.Fatalf("refresh = %v", v.refresh)
	}
}

func TestFingerprintFollowsTheConfiguration(t *testing.T) {
	a := &vicare{clientID: "c", seedRefresh: "r"}
	b := &vicare{clientID: "c", seedRefresh: "r", device: "0"}
	c := &vicare{clientID: "c", seedRefresh: "other"}
	if a.fingerprint() != (&vicare{clientID: "c", seedRefresh: "r"}).fingerprint() {
		t.Fatal("the same configuration must have the same fingerprint")
	}
	if a.fingerprint() == b.fingerprint() || a.fingerprint() == c.fingerprint() {
		t.Fatal("another configuration must have another fingerprint")
	}
}

func TestLoadStateTakesOverTheSnapshotAndTokens(t *testing.T) {
	v := &vicare{clientID: "c", seedRefresh: "r"}
	st := storedState{
		Version: stateVersion, Key: v.fingerprint(),
		Installation: "1", Gateway: "GW", Device: "0", Model: "Vitocal",
		Identified: true,
		Tokens:     &tokens{Access: "a", Refresh: "rotated", Expiry: time.Now().Add(time.Hour)},
		FetchedAt:  time.Now().Add(-time.Minute),
		Snapshot:   testSnapshot(),
	}
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.LoadState(data, nil); err != nil {
		t.Fatal(err)
	}
	if v.snap == nil || v.tokens == nil || v.tokens.Refresh != "rotated" ||
		v.installation != "1" || v.gateway != "GW" || v.device != "0" || !v.identified {
		t.Fatalf("loaded %+v", v)
	}

	other := &vicare{clientID: "c", seedRefresh: "another"}
	if err := other.LoadState(data, nil); err != nil {
		t.Fatal(err)
	}
	if other.snap != nil || other.tokens != nil {
		t.Fatal("a state of another configuration must be ignored")
	}
}

func TestStateReportsPendingOnlyOnce(t *testing.T) {
	v := &vicare{clientID: "c", seedRefresh: "r"}
	v.mu.Lock()
	v.fetched = time.Now()
	v.queueLocked()
	v.mu.Unlock()
	data := v.State()
	if data == nil {
		t.Fatal("no state")
	}
	var st storedState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	if st.Version != stateVersion || st.Key != v.fingerprint() {
		t.Fatalf("state = %+v", st)
	}
	if again := v.State(); again != nil {
		t.Fatal("the state was reported twice without a change")
	}
}

func TestDescribeDeclaresWidgetsAndValues(t *testing.T) {
	d := (&vicare{}).Describe()
	if d.Title != "ViCare" || d.Version != version() {
		t.Fatalf("description = %+v", d)
	}
	names := map[string]bool{}
	for _, w := range d.Widgets {
		names[w.Widget] = true
		if len(w.Sizes) == 0 || w.MinWidth == 0 || len(w.Kinds) == 0 {
			t.Errorf("%s: incomplete declaration %+v", w.Widget, w)
		}
	}
	for _, want := range []string{"heating", "temperatures", "dhw", "burner", "status", "overview", "curve"} {
		if !names[want] {
			t.Errorf("widget %q is not declared", want)
		}
	}
	values := map[string]bool{}
	for _, val := range d.Values {
		values[val.Name] = true
	}
	for _, want := range []string{"outside", "room", "flow", "flow_target", "dhw", "dhw_target",
		"burner", "program", "state", "curve_slope", "curve_shift"} {
		if !values[want] {
			t.Errorf("value %q is not declared", want)
		}
	}
}

func TestValues(t *testing.T) {
	v := testPlugin(t, "de")
	req := &plugin.Request{Language: "de", Values: []string{
		"outside", "room", "flow", "flow_target", "dhw", "dhw_target",
		"burner", "program", "state", "curve_slope", "curve_shift", "model", "updated",
	}}
	got, err := v.Values(req)
	if err != nil {
		t.Fatal(err)
	}
	if n := got["outside"].Number; n == nil || *n != 4.2 {
		t.Errorf("outside = %+v", got["outside"])
	}
	if n := got["flow"].Number; n == nil || *n != 41.8 {
		t.Errorf("flow = %+v", got["flow"])
	}
	if n := got["curve_slope"].Number; n == nil || *n != 0.6 {
		t.Errorf("curve_slope = %+v", got["curve_slope"])
	}
	if s := got["program"].Text; s == nil || *s != "Normal" {
		t.Errorf("program = %+v", got["program"])
	}
	if s := got["burner"].Text; s == nil || *s != "Kompressor an" {
		t.Errorf("burner = %+v", got["burner"])
	}
	if e := got["state"].Enum; e == nil || *e != "ok" {
		t.Errorf("state = %+v", got["state"])
	}
	if s := got["model"].Text; s == nil || *s != "Vitocal 250-A" {
		t.Errorf("model = %+v", got["model"])
	}
	if _, err := v.Values(&plugin.Request{Values: []string{"nope"}}); err == nil {
		t.Fatal("an unknown value must fail")
	}
}

func TestValuesReportErrors(t *testing.T) {
	v := testPlugin(t, "en")
	v.cur.Errors = []message{{Code: "F.518", Description: "sensor"}}
	got, err := v.Values(&plugin.Request{Language: "en", Values: []string{"state"}})
	if err != nil {
		t.Fatal(err)
	}
	if e := got["state"].Enum; e == nil || *e != "error" {
		t.Fatalf("state = %+v", got["state"])
	}
}
