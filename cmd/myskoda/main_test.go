package main

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

func fixture(t *testing.T, name string) *vehicle {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var vr vehicleResponse
	if err := json.Unmarshal(body, &vr); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return &vr.Vehicle
}

func testPlugin(t *testing.T, name string) *myskoda {
	t.Helper()
	m := &myskoda{client: http.DefaultClient, lang: "de", loc: time.UTC}
	m.cur = fixture(t, name)
	m.req = &plugin.Request{
		Language: "de",
		Timezone: "UTC",
		Now:      time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC),
		Page:     plugin.PageInfo{Width: 800, Height: 480, PPI: 125},
	}
	return m
}

func TestFixtureValues(t *testing.T) {
	v := fixture(t, "enyaq.json")
	if v.Name != "Enyaq" || v.LicensePlate != "1MB 1234" {
		t.Fatalf("name and plate: %q %q", v.Name, v.LicensePlate)
	}
	if v.Charging == nil || v.Charging.Status == nil || v.Charging.Status.Battery.SoC == nil {
		t.Fatal("charging status missing")
	}
	if got := *v.Charging.Status.Battery.SoC; got != 71 {
		t.Fatalf("SoC = %d, want 71", got)
	}
	if got := *v.Charging.Settings.TargetSoC; got != 80 {
		t.Fatalf("target = %d, want 80", got)
	}
	if v.ChargingProfiles == nil || len(v.ChargingProfiles.Profiles) != 2 {
		t.Fatal("profiles missing")
	}
	if v.ParkingPosition == nil || v.ParkingPosition.GPS == nil {
		t.Fatal("position missing")
	}
	if v.AirConditioning == nil || v.AirConditioning.WindowHeating.Front == nil {
		t.Fatal("climate missing")
	}
	if v.Odometer == nil || v.Odometer.KM == nil || *v.Odometer.KM != 12753 {
		t.Fatal("odometer missing")
	}
}

func TestConfigure(t *testing.T) {
	const vin = "TMBJB9NY5RF999999"
	tests := []struct {
		name    string
		cfg     plugin.Values
		wantErr string
		refresh time.Duration
	}{
		{"ok", plugin.Values{"api_key": "k", "vin": vin}, "", time.Hour},
		{"lowercase vin", plugin.Values{"api_key": "k", "vin": strings.ToLower(vin)}, "", time.Hour},
		{"custom refresh", plugin.Values{"api_key": "k", "vin": vin, "refresh": "15m"}, "", 15 * time.Minute},
		{"below minimum", plugin.Values{"api_key": "k", "vin": vin, "refresh": "5m"}, "", minRefresh},
		{"no key", plugin.Values{"vin": vin}, "api_key is required", 0},
		{"empty key", plugin.Values{"api_key": " ", "vin": vin}, "api_key is empty", 0},
		{"no vin", plugin.Values{"api_key": "k"}, "vin is required", 0},
		{"short vin", plugin.Values{"api_key": "k", "vin": "ABC"}, "17 characters", 0},
		{"bad base", plugin.Values{"api_key": "k", "vin": vin, "base": "public.api"}, "http(s)", 0},
		{"unknown key", plugin.Values{"api_key": "k", "vin": vin, "token": "x"}, "unknown keys: token", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &myskoda{}
			err := m.Configure(tt.cfg, &plugin.Request{})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if m.refresh != tt.refresh {
				t.Fatalf("refresh = %s, want %s", m.refresh, tt.refresh)
			}
			if m.vin != vin {
				t.Fatalf("vin = %q", m.vin)
			}
			if m.base != defaultBase {
				t.Fatalf("base = %q", m.base)
			}
		})
	}
}

func TestFetch(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "enyaq.json"))
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/api/v1/vehicles/TMBJB9NY5RF999999"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got := r.Header.Get("X-API-Key"); got != "secret" {
			t.Errorf("X-API-Key = %q", got)
		}
		if inc := r.URL.Query().Get("include"); !strings.Contains(inc, "chargingProfiles") || !strings.Contains(inc, "parkingPosition") {
			t.Errorf("include = %q", inc)
		}
		w.Header().Set("X-API-Key-Expires-At", expires.Format(http.TimeFormat))
		w.Header().Set("RateLimit-Remaining", "19")
		w.Write(body)
	}))
	defer srv.Close()

	m := &myskoda{client: srv.Client(), apiKey: "secret", vin: "TMBJB9NY5RF999999", base: srv.URL, refresh: time.Hour}
	ans, err := m.fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ans.vehicle == nil || ans.vehicle.Name != "Enyaq" {
		t.Fatalf("vehicle = %+v", ans.vehicle)
	}
	if !ans.expires.Equal(expires) {
		t.Fatalf("expiry = %s, want %s", ans.expires, expires)
	}
	if ans.wait != 0 {
		t.Fatalf("wait = %s", ans.wait)
	}
}

func TestFetchProblems(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		headers  map[string]string
		want     string
		wantWait time.Duration
	}{
		{
			name:   "expired key",
			status: http.StatusUnauthorized,
			body:   `{"type":"https://public.api.connect.skoda-auto.cz/problems/api-key-expired","detail":"expired"}`,
			want:   "MyŠkoda app",
		},
		{
			name:   "key not authorized",
			status: http.StatusForbidden,
			body:   `{"type":"https://public.api.connect.skoda-auto.cz/problems/api-key-not-authorized"}`,
			want:   "not authorized",
		},
		{
			name:     "rate limit",
			status:   http.StatusTooManyRequests,
			body:     `{"type":"https://public.api.connect.skoda-auto.cz/problems/rate-limit-exceeded"}`,
			headers:  map[string]string{"Retry-After": "120"},
			want:     "rate limit",
			wantWait: 2 * time.Minute,
		},
		{
			name:     "quota exhausted",
			status:   http.StatusOK,
			body:     `{"vehicle":{"vin":"TMBJB9NY5RF999999"}}`,
			headers:  map[string]string{"RateLimit-Remaining": "0", "RateLimit-Reset": "600"},
			wantWait: 630 * time.Second,
		},
		{
			name:   "other",
			status: http.StatusBadGateway,
			body:   `{"detail":"upstream"}`,
			want:   "HTTP 502: upstream",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tt.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			m := &myskoda{client: srv.Client(), apiKey: "k", vin: "TMBJB9NY5RF999999", base: srv.URL}
			ans, err := m.fetch(context.Background())
			if tt.status == http.StatusOK {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			if tt.wantWait != 0 && ans.wait != tt.wantWait {
				t.Fatalf("wait = %s, want %s", ans.wait, tt.wantWait)
			}
		})
	}
}

func TestFetchWrongVIN(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"vehicle":{"vin":"TMBJB9NY5RF000000"}}`))
	}))
	defer srv.Close()
	m := &myskoda{client: srv.Client(), apiKey: "k", vin: "TMBJB9NY5RF999999", base: srv.URL}
	if _, err := m.fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "instead of") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseExpiry(t *testing.T) {
	want := time.Date(2026, 12, 1, 8, 30, 0, 0, time.UTC)
	for _, s := range []string{want.Format(http.TimeFormat), want.Format(time.RFC3339)} {
		got, ok := parseExpiry(s)
		if !ok || !got.Equal(want) {
			t.Fatalf("%q: %v %v", s, got, ok)
		}
	}
	if _, ok := parseExpiry("soon"); ok {
		t.Fatal("garbage parsed")
	}
}

func TestDownloadImage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "" {
			t.Error("the API key must not go to the image host")
		}
		img := image.NewGray(image.Rect(0, 0, 40, 20))
		for y := 0; y < 20; y++ {
			for x := 0; x < 40; x++ {
				img.SetGray(x, y, color.Gray{Y: uint8(x * 6)})
			}
		}
		png.Encode(w, img)
	}))
	defer srv.Close()
	m := &myskoda{client: srv.Client()}
	img, err := m.downloadImage(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 40 || b.Dy() != 20 {
		t.Fatalf("bounds = %v", b)
	}
}

func TestStateTexts(t *testing.T) {
	m := testPlugin(t, "enyaq.json")
	if got := m.chargeState(); got != "Lädt" {
		t.Fatalf("charging = %q", got)
	}
	if got := m.climateState(); got != "Heizt" {
		t.Fatalf("climate = %q", got)
	}
	if got := m.lockState(); got != "Verriegelt" {
		t.Fatalf("lock = %q", got)
	}
	if got := m.remaining(); got != "1 h 15" {
		t.Fatalf("remaining = %q", got)
	}
	if got := m.captured("2026-11-08T09:20:00Z"); got != "Stand 09:20" {
		t.Fatalf("captured = %q", got)
	}
	if got := m.captured("2026-11-07T18:05:00Z"); got != "Stand 07.11. 18:05" {
		t.Fatalf("captured yesterday = %q", got)
	}
	if got := m.plugText(); got != "Kabel verriegelt" {
		t.Fatalf("plug = %q", got)
	}
	if got := m.temperature(m.cur.AirConditioning.Target); got != "22,5 °C" {
		t.Fatalf("temperature = %q", got)
	}
	if got := m.thousands(12753); got != "12.753" {
		t.Fatalf("thousands = %q", got)
	}
}

func TestUnknownEnumsAreShownAsTheyAre(t *testing.T) {
	m := testPlugin(t, "enyaq.json")
	unknown := "SOMETHING_NEW"
	m.cur.Charging.Status.State = &unknown
	if got := m.chargeState(); got != unknown {
		t.Fatalf("charging = %q", got)
	}
	m.cur.AirConditioning.State = &unknown
	if got := m.climateState(); got != unknown {
		t.Fatalf("climate = %q", got)
	}
	m.cur.Status.Overall.DoorsLocked = &unknown
	if got := m.lockState(); got != unknown {
		t.Fatalf("lock = %q", got)
	}
}

func TestMissingParts(t *testing.T) {
	m := testPlugin(t, "partial.json")
	if _, ok := m.soc(); ok {
		t.Fatal("SoC reported for a car without charging")
	}
	if got := m.chargeState(); got != "Unbekannt" {
		t.Fatalf("charging = %q", got)
	}
	if got := m.remaining(); got != "" {
		t.Fatalf("remaining = %q", got)
	}
	if m.currentProfile() != nil {
		t.Fatal("profile without chargingProfiles")
	}
	if got := m.cur.Charging; got != nil {
		t.Fatalf("charging = %+v", got)
	}
}

func TestProfileTimes(t *testing.T) {
	m := testPlugin(t, "enyaq.json")
	p := m.cur.ChargingProfiles.Profiles[0]
	if got := m.profileTimes(p); got != "22:00–06:00, Mo Di Mi Do Fr 04:30" {
		t.Fatalf("times = %q", got)
	}
	if got := m.profileTimes(m.cur.ChargingProfiles.Profiles[1]); got != "keine Ladezeiten" {
		t.Fatalf("times = %q", got)
	}
}

// apiServer serves the fixture with its render URL pointing back to itself, so
// the whole plugin runs against a real HTTP peer.
func apiServer(t *testing.T) *httptest.Server {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "enyaq.json"))
	if err != nil {
		t.Fatal(err)
	}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/vehicles/TMBJB9NY5RF999999":
			if r.Header.Get("X-API-Key") != "secret" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Write([]byte(strings.ReplaceAll(string(body), "https://render.example/enyaq.png", srv.URL+"/render.png")))
		case "/render.png":
			img := image.NewGray(image.Rect(0, 0, 160, 80))
			for y := 0; y < 80; y++ {
				for x := 0; x < 160; x++ {
					img.SetGray(x, y, color.Gray{Y: uint8(x * 255 / 160)})
				}
			}
			png.Encode(w, img)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestProtocol(t *testing.T) {
	srv := apiServer(t)
	m := &myskoda{client: srv.Client()}
	req := &plugin.Request{Language: "de", Timezone: "Europe/Berlin",
		Now:  time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC),
		Page: plugin.PageInfo{Width: 800, Height: 480, PPI: 125}}

	cfg := plugin.Handle(m, &plugin.Request{Action: plugin.ActionConfigure,
		Config: map[string]any{"api_key": "secret", "vin": "tmbjb9ny5rf999999", "base": srv.URL}})
	if cfg.Error != "" {
		t.Fatal(cfg.Error)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m.Start(ctx, func(...string) {})
	for i := 0; i < 200; i++ {
		m.mu.Lock()
		has := m.image != nil && m.image.img != nil
		m.mu.Unlock()
		if has {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	m.mu.Lock()
	hasImage := m.image != nil && m.image.img != nil
	m.mu.Unlock()
	if !hasImage {
		t.Fatal("render image was not loaded")
	}

	desc := plugin.Handle(m, &plugin.Request{Action: plugin.ActionDescribe})
	if desc.Error != "" || desc.Description == nil {
		t.Fatalf("%+v", desc)
	}
	names := map[string]bool{}
	for _, w := range desc.Description.Widgets {
		names[w.Widget] = true
	}
	for _, want := range []string{"battery", "charging", "profiles", "location", "climate", "card", "car"} {
		if !names[want] {
			t.Errorf("describe lacks %q", want)
		}
	}

	req.Action, req.Levels = plugin.ActionRender, 4
	req.Widgets = []plugin.WidgetRequest{{Widget: "card", Width: 600, Height: 400,
		Style: plugin.Style{Padding: 5, Rule: 2, TextPx: 20}}}
	msg := plugin.Handle(m, req)
	if msg.Error != "" || len(msg.Widgets) != 1 || msg.Widgets[0].Error != "" || len(msg.Widgets[0].PNG) == 0 {
		t.Fatalf("%+v", msg)
	}
	if msg.MaxAge != int(time.Hour/time.Second) {
		t.Fatalf("max_age = %d", msg.MaxAge)
	}

	req.Action = plugin.ActionText
	req.Widgets = []plugin.WidgetRequest{{Widget: "battery", Width: 20, Height: 4}}
	msg = plugin.Handle(m, req)
	if msg.Error != "" || len(msg.Widgets) != 1 || len(msg.Widgets[0].Lines) == 0 {
		t.Fatalf("%+v", msg)
	}
	if err := msg.CheckVersion(); err != nil {
		t.Fatal(err)
	}
}

func TestProtocolWithoutData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"type":"https://public.api.connect.skoda-auto.cz/problems/api-key-expired"}`))
	}))
	defer srv.Close()
	m := &myskoda{client: srv.Client()}
	if msg := plugin.Handle(m, &plugin.Request{Action: plugin.ActionConfigure,
		Config: map[string]any{"api_key": "k", "vin": "TMBJB9NY5RF999999", "base": srv.URL}}); msg.Error != "" {
		t.Fatal(msg.Error)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m.Start(ctx, func(...string) {})
	msg := plugin.Handle(m, &plugin.Request{Action: plugin.ActionRender, Levels: 2,
		Widgets: []plugin.WidgetRequest{{Widget: "battery", Width: 200, Height: 100}}})
	if msg.Error == "" || !strings.Contains(msg.Error, "MyŠkoda app") {
		t.Fatalf("%+v", msg)
	}
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for " + what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// stateServer serves the fixture and counts API and image requests.
func stateServer(t *testing.T) (srv *httptest.Server, api, images *atomic.Int64) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "enyaq.json"))
	if err != nil {
		t.Fatal(err)
	}
	api, images = new(atomic.Int64), new(atomic.Int64)
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/render.png" {
			images.Add(1)
			png.Encode(w, image.NewGray(image.Rect(0, 0, 60, 30)))
			return
		}
		api.Add(1)
		w.Write([]byte(strings.ReplaceAll(string(body), "https://render.example/enyaq.png", srv.URL+"/render.png")))
	}))
	t.Cleanup(srv.Close)
	return srv, api, images
}

func TestStateSurvivesRestart(t *testing.T) {
	const vin = "TMBJB9NY5RF999999"
	dir := t.TempDir()
	t.Setenv(plugin.StateDirEnv, dir)
	srv, api, images := stateServer(t)
	cfg := plugin.Values{"api_key": "secret", "vin": vin, "base": srv.URL, "refresh": "1h"}

	first := &myskoda{client: srv.Client()}
	if err := first.Configure(cfg, &plugin.Request{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	first.Start(ctx, func(...string) {})
	waitFor(t, func() bool {
		first.mu.Lock()
		defer first.mu.Unlock()
		return first.data != nil && first.image != nil && first.image.img != nil
	}, "the first load")
	waitFor(t, func() bool {
		_, err := os.Stat(filepath.Join(dir, "state.json"))
		return err == nil
	}, "the state file")
	cancel()

	second := &myskoda{client: srv.Client()}
	if err := second.Configure(cfg, &plugin.Request{}); err != nil {
		t.Fatal(err)
	}
	if second.data == nil {
		t.Fatal("the restart did not take over the state")
	}
	if second.image == nil || second.image.img == nil {
		t.Fatal("the restart did not take over the render image")
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	second.Start(ctx2, func(...string) {})
	time.Sleep(200 * time.Millisecond)
	if got := api.Load(); got != 1 {
		t.Fatalf("%d API requests, want 1: the restart must wait for the interval", got)
	}
	if got := images.Load(); got != 1 {
		t.Fatalf("%d image downloads, want 1", got)
	}
}

func TestRateLimitWaitSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(plugin.StateDirEnv, dir)
	var mu sync.Mutex
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"type":"https://public.api.connect.skoda-auto.cz/problems/rate-limit-exceeded"}`))
	}))
	defer srv.Close()
	cfg := plugin.Values{"api_key": "secret", "vin": "TMBJB9NY5RF999999", "base": srv.URL, "refresh": "1m"}

	first := &myskoda{client: srv.Client()}
	if err := first.Configure(cfg, &plugin.Request{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	first.Start(ctx, func(...string) {})
	waitFor(t, func() bool {
		_, err := os.Stat(filepath.Join(dir, "state.json"))
		return err == nil
	}, "the state file after the 429")
	cancel()

	second := &myskoda{client: srv.Client()}
	if err := second.Configure(cfg, &plugin.Request{}); err != nil {
		t.Fatal(err)
	}
	if second.loadErr == nil || !strings.Contains(second.loadErr.Error(), "rate limit") {
		t.Fatalf("the restart forgot the error: %v", second.loadErr)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	second.Start(ctx2, func(...string) {})
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Fatalf("%d requests, want 1: the wait must survive the restart", hits)
	}
}

func TestStoredStateOfAnotherVehicleIsIgnored(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(plugin.StateDirEnv, dir)
	other := &myskoda{apiKey: "secret"}
	s := storedState{Version: stateVersion, VIN: "OTHER0000000000000", Key: other.fingerprint(),
		Vehicle: &vehicle{VIN: "OTHER0000000000000", Name: "Other"}}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	m := &myskoda{client: http.DefaultClient}
	if err := m.Configure(plugin.Values{"api_key": "secret", "vin": "TMBJB9NY5RF999999"}, &plugin.Request{}); err != nil {
		t.Fatal(err)
	}
	if m.data != nil {
		t.Fatal("the state of another vehicle was used")
	}
}

func TestBrokenStateIsIgnored(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(plugin.StateDirEnv, dir)
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &myskoda{client: http.DefaultClient}
	if err := m.Configure(plugin.Values{"api_key": "secret", "vin": "TMBJB9NY5RF999999"}, &plugin.Request{}); err != nil {
		t.Fatal(err)
	}
	if m.data != nil {
		t.Fatal("a broken state file was used")
	}
}
