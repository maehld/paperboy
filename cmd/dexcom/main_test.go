package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

const (
	testAppID   = "d89443d2-327c-4a6f-89e5-496bbb0317db"
	testAccount = "9b2f0000-1111-2222-3333-444455556666"
	testSession = "5f1a7c62-1111-2222-3333-444455556666"
)

// fakeShare imitates the parts of the service the plugin uses and counts the
// requests, so a test can tell a full read from an incremental one.
type fakeShare struct {
	mu        sync.Mutex
	values    []reading // oldest first
	refuse    bool
	expire    bool // answer SessionIdNotFound once
	auths     int
	logins    int
	reads     int
	lastCount int
	lastMin   int
}

func (f *fakeShare) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/General/AuthenticatePublisherAccount":
		f.auths++
		if f.refuse {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"Code":"AccountPasswordInvalid","Message":"Publisher account password failed"}`))
			return
		}
		json.NewEncoder(w).Encode(testAccount)
	case "/General/LoginPublisherAccountById":
		f.logins++
		if f.refuse {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"Code":"AccountPasswordInvalid","Message":"Publisher account password failed"}`))
			return
		}
		json.NewEncoder(w).Encode(testSession)
	case "/Publisher/ReadPublisherLatestGlucoseValues":
		f.reads++
		count, _ := strconv.Atoi(r.URL.Query().Get("maxCount"))
		minutes, _ := strconv.Atoi(r.URL.Query().Get("minutes"))
		f.lastCount, f.lastMin = count, minutes
		if f.expire {
			f.expire = false
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"Code":"SessionIdNotFound","Message":"Failed to find session object."}`))
			return
		}
		if r.URL.Query().Get("sessionId") != testSession {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"Code":"SessionIdNotFound","Message":"Failed to find session object."}`))
			return
		}
		cut := time.Now().Add(-time.Duration(minutes) * time.Minute)
		var out []string
		for i := len(f.values) - 1; i >= 0 && len(out) < count; i-- {
			v := f.values[i]
			if v.at.Before(cut) {
				continue
			}
			out = append(out, fmt.Sprintf(`{"WT":"/Date(%d)/","ST":"/Date(%d)/","Value":%g,"Trend":%q}`,
				v.at.UnixMilli(), v.at.UnixMilli(), v.value, v.trend))
		}
		w.Write([]byte("[" + strings.Join(out, ",") + "]"))
	default:
		http.NotFound(w, r)
	}
}

// series builds readings every five minutes ending at now, with a night low
// and a peak above the band.
func series(now time.Time, n int) []reading {
	out := make([]reading, 0, n)
	trends := []string{"Flat", "FortyFiveUp", "SingleUp", "FortyFiveDown"}
	for i := 0; i < n; i++ {
		at := now.Add(-time.Duration(n-1-i) * 5 * time.Minute)
		v := 120 + 60*math.Sin(float64(i)/18)
		if i == n/3 {
			v = 52
		}
		if i == n/2 {
			v = 295
		}
		out = append(out, reading{at: at, value: math.Round(v), trend: trends[i%len(trends)]})
	}
	return out
}

func newTestDexcom(t *testing.T, now time.Time) (*dexcom, *fakeShare, *plugin.Request) {
	t.Helper()
	f := &fakeShare{values: series(now, 288)}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)

	d := &dexcom{}
	req := &plugin.Request{
		Language: "de", Timezone: "Europe/Berlin", Now: now,
		Page: plugin.PageInfo{Width: 800, Height: 480, PPI: 125},
	}
	err := d.Configure(plugin.Values{
		"account_name": "someone@example.com",
		"password":     "secret",
		"app_id":       testAppID,
		"region":       "us",
	}, req)
	if err != nil {
		t.Fatal(err)
	}
	// Point the client at the test server instead of the real host.
	d.mu.Lock()
	d.api = &shareClient{base: srv.URL, appID: testAppID, http: srv.Client()}
	d.mu.Unlock()
	return d, f, req
}

func style() plugin.Style { return plugin.Style{Padding: 8, Rule: 1, TextPx: 16} }

func TestParseDexDate(t *testing.T) {
	cases := []struct {
		in   string
		want time.Time
		ok   bool
	}{
		{`/Date(1700000000000)/`, time.UnixMilli(1700000000000).UTC(), true},
		{`/Date(1700000000000+0000)/`, time.UnixMilli(1700000000000).UTC(), true},
		{`/Date(1700000000000-0400)/`, time.UnixMilli(1700000000000).In(time.FixedZone("", -4*3600)), true},
		{"2026-11-13T10:00:00Z", time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC), true},
		{"", time.Time{}, false},
		{"nonsense", time.Time{}, false},
	}
	for _, c := range cases {
		got, ok := parseDexDate(c.in)
		if ok != c.ok {
			t.Errorf("parseDexDate(%q) ok = %v, want %v", c.in, ok, c.ok)
			continue
		}
		if ok && !got.Equal(c.want) {
			t.Errorf("parseDexDate(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseReadings(t *testing.T) {
	body := `[{"WT":"/Date(1700000300000)/","Value":112,"Trend":"Flat"},
	          {"WT":"/Date(1700000000000)/","Value":105,"Trend":"FortyFiveUp"}]`
	rs, err := parseReadings([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0].value != 112 || rs[0].trend != "Flat" {
		t.Fatalf("%+v", rs)
	}
	quoted, _ := json.Marshal(body)
	rs, err = parseReadings(quoted)
	if err != nil || len(rs) != 2 {
		t.Fatalf("string-wrapped array: %+v %v", rs, err)
	}
	if _, err := parseReadings([]byte("<html>")); err == nil {
		t.Error("garbage parsed without error")
	}
}

func TestLimits(t *testing.T) {
	mg := defaults(unitMGDL)
	if mg.urgent != 55 || mg.low != 80 || mg.high != 280 || mg.min != 40 || mg.max != 300 {
		t.Errorf("mg/dL defaults = %+v", mg)
	}
	mmol := defaults(unitMMOL)
	if mmol.urgent != 3.1 || mmol.low != 4.4 || mmol.decimals != 1 {
		t.Errorf("mmol/L defaults = %+v", mmol)
	}
	if err := mg.check(); err != nil {
		t.Errorf("mg default check: %v", err)
	}
	bad := mg
	bad.high = 20
	if err := bad.check(); err == nil {
		t.Error("a band above its own high passed the check")
	}
	if detectUnit(112) != unitMGDL || detectUnit(5.9) != unitMMOL {
		t.Error("unit detection")
	}
}

// The configuration must reject a broken threshold order and a missing
// account or region.
func TestConfigureValidation(t *testing.T) {
	req := &plugin.Request{Language: "de", Now: time.Now()}
	base := plugin.Values{
		"account_name": "someone@example.com",
		"password":     "secret",
		"app_id":       testAppID,
		"region":       "us",
	}
	for name, mutate := range map[string]func(plugin.Values){
		"no account":      func(v plugin.Values) { delete(v, "account_name") },
		"both accounts":   func(v plugin.Values) { v["account_id"] = testAccount },
		"no region":       func(v plugin.Values) { delete(v, "region") },
		"bad region":      func(v plugin.Values) { v["region"] = "de" },
		"bad app id":      func(v plugin.Values) { v["app_id"] = "nope" },
		"bad order":       func(v plugin.Values) { v["low"] = 400 },
		"urgent too high": func(v plugin.Values) { v["urgent"] = 90 },
		"fast refresh":    func(v plugin.Values) { v["refresh"] = "5s" },
		"unknown key":     func(v plugin.Values) { v["colour"] = "red" },
	} {
		cfg := plugin.Values{}
		for k, val := range base {
			cfg[k] = val
		}
		mutate(cfg)
		if err := (&dexcom{}).Configure(cfg, req); err == nil {
			t.Errorf("%s: configuration accepted", name)
		}
	}
	if err := (&dexcom{}).Configure(base, req); err != nil {
		t.Errorf("the plain configuration was rejected: %v", err)
	}
}

func TestWidgets(t *testing.T) {
	now := time.Date(2026, 11, 13, 20, 0, 0, 0, time.UTC)
	d, f, req := newTestDexcom(t, now)
	if err := d.update(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.auths != 1 || f.logins != 1 {
		t.Errorf("%d authentications, %d logins", f.auths, f.logins)
	}
	if f.lastCount != maxReadings {
		t.Errorf("the first request asked for %d readings, want %d", f.lastCount, maxReadings)
	}

	sizes := []image.Rectangle{
		image.Rect(0, 0, 400, 240),
		image.Rect(0, 0, 266, 120),
		image.Rect(0, 0, 130, 90),
	}
	for _, size := range sizes {
		for _, widget := range []string{"glucose", "graph", "values"} {
			img := render.NewCanvas(size.Dx(), size.Dy(), 4)
			w := plugin.WidgetRequest{Widget: widget, Width: size.Dx(), Height: size.Dy(), Style: style()}
			if err := d.Render(req, w, img); err != nil {
				t.Fatalf("%s at %v: %v", widget, size, err)
			}
		}
	}

	img := render.NewCanvas(400, 240, 4)
	w := plugin.WidgetRequest{Widget: "graph", Width: 400, Height: 240, Style: style(),
		Args: map[string]any{"period": "hour"}}
	if err := d.Render(req, w, img); err != nil {
		t.Fatalf("hour graph: %v", err)
	}

	lines, err := d.Text(req, plugin.WidgetRequest{Widget: "glucose", Width: 20, Height: 1, Style: style()})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "mg/dL") {
		t.Errorf("text lines = %q", lines)
	}
}

// A missing account name, an urgent value and a stale reading all have to draw
// without an error.
func TestAlarmStates(t *testing.T) {
	now := time.Date(2026, 11, 13, 20, 0, 0, 0, time.UTC)
	d, f, req := newTestDexcom(t, now)
	f.values = []reading{
		{at: now.Add(-10 * time.Minute), value: 70, trend: "SingleDown"},
		{at: now.Add(-5 * time.Minute), value: 48, trend: "DoubleDown"},
	}
	if err := d.update(context.Background()); err != nil {
		t.Fatal(err)
	}
	lines, err := d.Text(req, plugin.WidgetRequest{Widget: "glucose", Width: 20, Height: 1, Style: style()})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lines[0], "Dringend!") {
		t.Errorf("the urgent state is not named: %q", lines[0])
	}
	img := render.NewCanvas(400, 240, 4)
	if err := d.Render(req, plugin.WidgetRequest{Widget: "glucose", Width: 400, Height: 240, Style: style()}, img); err != nil {
		t.Fatal(err)
	}
}

// After the first read, a poll asks for the values of the gap only.
func TestIncrementalFetch(t *testing.T) {
	now := time.Now()
	d, f, _ := newTestDexcom(t, now)
	if err := d.update(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := f.lastCount
	if first != maxReadings {
		t.Fatalf("first request asked for %d readings", first)
	}
	// Two more readings arrived, both newer than the stored ones.
	f.values = append(f.values, reading{at: now.Add(5 * time.Minute), value: 130, trend: "Flat"},
		reading{at: now.Add(10 * time.Minute), value: 135, trend: "Flat"})
	if err := d.update(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.lastCount > 4 {
		t.Errorf("the second request asked for %d readings, want at most 4", f.lastCount)
	}
	if f.lastMin > dayWindow {
		t.Errorf("the window %d minutes exceeds a day", f.lastMin)
	}
	if got := len(d.readings); got != len(f.values) {
		t.Errorf("%d readings stored, want %d", got, len(f.values))
	}
}

// The stored state keeps the session and the readings, so a restart costs no
// login and asks for the gap only.
func TestStateRoundTrip(t *testing.T) {
	now := time.Now()
	d, f, req := newTestDexcom(t, now)
	if err := d.update(context.Background()); err != nil {
		t.Fatal(err)
	}
	state := d.State()
	if len(state) == 0 {
		t.Fatal("no state")
	}

	second, f2, _ := newTestDexcom(t, now)
	if err := second.LoadState(state, nil); err != nil {
		t.Fatal(err)
	}
	if got := len(second.readings); got != len(d.readings) {
		t.Fatalf("%d readings restored, want %d", got, len(d.readings))
	}
	if second.session != testSession || second.resolved != testAccount {
		t.Errorf("session %q, account %q", second.session, second.resolved)
	}
	if _, err := second.sessionID(); err != nil {
		t.Fatalf("the restored session was not used: %v", err)
	}
	if f2.auths != 0 || f2.logins != 0 {
		t.Errorf("%d authentications, %d logins after the restart", f2.auths, f2.logins)
	}
	minutes, count := second.window()
	if count > maxReadings || minutes > dayWindow {
		t.Errorf("window after a restart = %d minutes, %d readings", minutes, count)
	}
	if count > 40 {
		t.Errorf("a restart asked for %d readings instead of the gap", count)
	}
	_ = req
	_ = f
}

// A state of another account or region is ignored.
func TestStateOfAnotherAccount(t *testing.T) {
	now := time.Now()
	d, _, req := newTestDexcom(t, now)
	if err := d.update(context.Background()); err != nil {
		t.Fatal(err)
	}
	state := d.State()

	other := &dexcom{}
	if err := other.Configure(plugin.Values{
		"account_name": "other@example.com", "password": "secret",
		"app_id": testAppID, "region": "eu",
	}, req); err != nil {
		t.Fatal(err)
	}
	if err := other.LoadState(state, nil); err != nil {
		t.Fatal(err)
	}
	if len(other.readings) != 0 || other.session != "" {
		t.Error("the state of another account or region was taken over")
	}
}

// A session the service forgot costs one new login, not a restart.
func TestExpiredSession(t *testing.T) {
	now := time.Now()
	d, f, _ := newTestDexcom(t, now)
	if err := d.update(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.logins != 1 {
		t.Fatalf("%d logins", f.logins)
	}
	f.expire = true
	if err := d.update(context.Background()); err != nil {
		t.Fatalf("the expired session was not renewed: %v", err)
	}
	if f.logins != 2 {
		t.Errorf("%d logins after the expiry, want 2", f.logins)
	}
}

// Refused credentials end up in the error, not in a silent default.
func TestRefusedCredentials(t *testing.T) {
	now := time.Now()
	d, f, _ := newTestDexcom(t, now)
	f.refuse = true
	err := d.update(context.Background())
	if err == nil {
		t.Fatal("refused credentials were accepted")
	}
	if !strings.Contains(err.Error(), "AccountPasswordInvalid") {
		t.Errorf("the error does not name the reason: %v", err)
	}
}

// The plugin as the widget host talks to it: describe, configure, render and
// text over the JSON lines of the protocol.
func TestProtocol(t *testing.T) {
	now := time.Now()
	f := &fakeShare{values: series(now, 288)}
	srv := httptest.NewServer(f)
	defer srv.Close()
	old := shareRegions["us"]
	shareRegions["us"] = srv.URL
	defer func() { shareRegions["us"] = old }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	d := &dexcom{}
	done := make(chan error, 1)
	go func() { done <- plugin.ServeIO(ctx, d, inR, outW) }()
	enc := json.NewEncoder(inW)
	dec := json.NewDecoder(outR)
	next := func() plugin.Message {
		t.Helper()
		var m plugin.Message
		if err := dec.Decode(&m); err != nil {
			t.Fatalf("read answer: %v", err)
		}
		if m.Error != "" {
			t.Fatalf("answer %s: %s", m.ID, m.Error)
		}
		if m.Version != plugin.Version {
			t.Fatalf("answer %s speaks version %d", m.ID, m.Version)
		}
		return m
	}

	send := func(r plugin.Request) {
		t.Helper()
		r.Version = plugin.Version
		if err := enc.Encode(r); err != nil {
			t.Fatalf("write request: %v", err)
		}
	}

	send(plugin.Request{ID: "1", Action: plugin.ActionDescribe, Language: "de", Now: now})
	m := next()
	if m.Description == nil || len(m.Description.Widgets) != 3 || m.Description.Version == "" {
		t.Fatalf("description = %+v", m.Description)
	}

	send(plugin.Request{ID: "2", Action: plugin.ActionConfigure, Language: "de", Timezone: "Europe/Berlin", Now: now,
		Config: map[string]any{
			"account_name": "someone@example.com", "password": "secret",
			"app_id": testAppID, "region": "us",
		}})
	next()

	if err := d.update(ctx); err != nil {
		t.Fatalf("update: %v", err)
	}

	send(plugin.Request{ID: "3", Action: plugin.ActionRender, Language: "de", Now: now, Levels: 4,
		Page:    plugin.PageInfo{Width: 800, Height: 480, PPI: 125},
		Widgets: []plugin.WidgetRequest{{Widget: "glucose", Width: 400, Height: 240, Style: style()}}})
	m = next()
	if len(m.Widgets) != 1 || len(m.Widgets[0].PNG) == 0 || m.Widgets[0].Error != "" {
		t.Fatalf("render answer = %+v", m.Widgets)
	}

	send(plugin.Request{ID: "4", Action: plugin.ActionText, Language: "de", Now: now,
		Page:    plugin.PageInfo{Width: 20, Height: 4},
		Widgets: []plugin.WidgetRequest{{Widget: "glucose", Width: 20, Height: 1}}})
	m = next()
	if len(m.Widgets) != 1 || len(m.Widgets[0].Lines) != 1 {
		t.Fatalf("text answer = %+v", m.Widgets)
	}

	send(plugin.Request{ID: "5", Action: plugin.ActionRender, Language: "de", Now: now, Levels: 4,
		Page:    plugin.PageInfo{Width: 800, Height: 480, PPI: 125},
		Widgets: []plugin.WidgetRequest{{Widget: "nope", Width: 100, Height: 100, Style: style()}}})
	m = next()
	if len(m.Widgets) != 1 || m.Widgets[0].Error == "" {
		t.Fatalf("an unknown widget did not fail: %+v", m.Widgets)
	}

	inW.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the plugin did not end with its input")
	}
}
