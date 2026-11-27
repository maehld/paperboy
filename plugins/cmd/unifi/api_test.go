package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/maehld/paperboy/sdk/plugin"
)

const testKey = "secret"

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// controllerFixture answers the endpoints of the controller. It serves the
// devices and the health from the testdata fixtures and records the requests.
type controllerFixture struct {
	mu          sync.Mutex
	requests    []string
	sites       []siteInfo
	devices     []byte
	clients     int
	health      []byte
	healthFail  bool
	failDevices bool
}

func newControllerServer(t *testing.T) (*httptest.Server, *controllerFixture) {
	t.Helper()
	health, err := os.ReadFile(filepath.Join("testdata", "health.json"))
	if err != nil {
		t.Fatal(err)
	}
	devices, err := os.ReadFile(filepath.Join("testdata", "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	f := &controllerFixture{
		sites: []siteInfo{
			{ID: "11111111-1111-1111-1111-111111111111", Name: "Home", InternalReference: "home"},
			{ID: "22222222-2222-2222-2222-222222222222", Name: "Guest", InternalReference: "guest"},
		},
		devices: devices,
		clients: 74,
		health:  health,
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return srv, f
}

func (f *controllerFixture) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-API-KEY") != testKey {
		http.Error(w, "missing X-API-KEY", http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
	devices, clients, health, healthFail, failDevices := f.devices, f.clients, f.health, f.healthFail, f.failDevices
	f.mu.Unlock()

	if healthFail && strings.HasPrefix(r.URL.Path, "/proxy/network/api/") {
		http.Error(w, "down", http.StatusServiceUnavailable)
		return
	}
	if failDevices && strings.HasSuffix(r.URL.Path, "/devices") {
		http.Error(w, "down", http.StatusServiceUnavailable)
		return
	}
	switch {
	case r.URL.Path == "/proxy/network/integration/v1/sites":
		f.mu.Lock()
		sites := f.sites
		f.mu.Unlock()
		writeJSON(w, map[string]any{
			"offset": 0, "limit": 25, "count": len(sites), "totalCount": len(sites),
			"data": sites,
		})
	case strings.HasSuffix(r.URL.Path, "/devices"):
		w.Header().Set("Content-Type", "application/json")
		w.Write(devices)
	case strings.HasSuffix(r.URL.Path, "/clients"):
		writeJSON(w, map[string]any{
			"offset": 0, "limit": 1, "count": 1, "totalCount": clients,
			"data": []any{map[string]any{}},
		})
	case strings.HasPrefix(r.URL.Path, "/proxy/network/api/s/"):
		w.Header().Set("Content-Type", "application/json")
		w.Write(health)
	default:
		http.NotFound(w, r)
	}
}

func (f *controllerFixture) setClients(n int) {
	f.mu.Lock()
	f.clients = n
	f.mu.Unlock()
}

func (f *controllerFixture) setDevices(data []byte) {
	f.mu.Lock()
	f.devices = data
	f.mu.Unlock()
}

func (f *controllerFixture) setHealthFail(fail bool) {
	f.mu.Lock()
	f.healthFail = fail
	f.mu.Unlock()
}

func (f *controllerFixture) setFailDevices(fail bool) {
	f.mu.Lock()
	f.failDevices = fail
	f.mu.Unlock()
}

func (f *controllerFixture) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *controllerFixture) paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

// quietLogs keeps the expected error lines out of the test output.
func quietLogs(t *testing.T) {
	t.Helper()
	stderr = io.Discard
	t.Cleanup(func() { stderr = os.Stderr })
}

func configured(t *testing.T, srv *httptest.Server, site string) *unifi {
	t.Helper()
	p := newPlugin()
	p.client = srv.Client()
	cfg := plugin.Values{"url": srv.URL, "api_key": testKey, "refresh": "1m"}
	if site != "" {
		cfg["site"] = site
	}
	if err := p.Configure(cfg, &plugin.Request{Version: plugin.Version}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	return p
}

func fetched(t *testing.T, srv *httptest.Server, site string) *unifi {
	t.Helper()
	p := configured(t, srv, site)
	if !p.fetch(context.Background()) {
		t.Fatalf("first fetch reported no change")
	}
	return p
}

func TestFetchReadsSiteDevicesClientsAndHealth(t *testing.T) {
	srv, f := newControllerServer(t)
	p := fetched(t, srv, "")
	p.mu.Lock()
	snap := p.snap
	p.mu.Unlock()

	if !snap.loaded {
		t.Fatal("not loaded")
	}
	if snap.site.ID != f.sites[0].ID || snap.site.InternalReference != "home" {
		t.Errorf("site = %+v, want the first site", snap.site)
	}
	if len(snap.devices) != 1 || snap.devices[0].Name != "Keller-AP" || snap.devices[0].State != "ONLINE" {
		t.Errorf("devices = %+v", snap.devices)
	}
	if snap.clients != 74 {
		t.Errorf("clients = %d, want 74", snap.clients)
	}
	if snap.wan.Status != "ok" || snap.wan.ISPName != "Deutsche Telekom AG" {
		t.Errorf("wan = %+v", snap.wan)
	}
	if snap.www.Latency == nil || *snap.www.Latency != 10 {
		t.Errorf("www = %+v", snap.www)
	}
	if got := f.count(); got != 4 {
		t.Errorf("requests = %d, want 4: %v", got, f.paths())
	}
}

func TestResolveSite(t *testing.T) {
	srv, _ := newControllerServer(t)
	for _, c := range []struct{ name, site, wantRef, wantID string }{
		{"default", "", "home", "11111111-1111-1111-1111-111111111111"},
		{"by name", "Guest", "guest", "22222222-2222-2222-2222-222222222222"},
		{"by id", "22222222-2222-2222-2222-222222222222", "guest", "22222222-2222-2222-2222-222222222222"},
	} {
		p := fetched(t, srv, c.site)
		p.mu.Lock()
		site := p.snap.site
		p.mu.Unlock()
		if site.InternalReference != c.wantRef || site.ID != c.wantID {
			t.Errorf("%s: site = %+v", c.name, site)
		}
	}

	quietLogs(t)
	p := configured(t, srv, "nope")
	if p.fetch(context.Background()) {
		t.Error("unknown site fetched")
	}
	p.mu.Lock()
	loaded := p.snap.loaded
	p.mu.Unlock()
	if loaded {
		t.Error("loaded despite an unknown site")
	}
}

func TestDevicesAndClientsPaths(t *testing.T) {
	srv, f := newControllerServer(t)
	fetched(t, srv, "Guest")
	for _, want := range []string{
		"/proxy/network/integration/v1/sites",
		"/proxy/network/integration/v1/sites/22222222-2222-2222-2222-222222222222/devices?limit=200&offset=0",
		"/proxy/network/integration/v1/sites/22222222-2222-2222-2222-222222222222/clients?limit=1",
		"/proxy/network/api/s/guest/stat/health",
	} {
		if !hasPath(f.paths(), want) {
			t.Errorf("no request for %s: %v", want, f.paths())
		}
	}
}

func hasPath(paths []string, want string) bool {
	for _, p := range paths {
		// p is "GET <path>?<query>".
		rest := strings.TrimSuffix(strings.TrimPrefix(p, "GET "), "?")
		if rest == want {
			return true
		}
	}
	return false
}

func TestWANOffSkipsPrivateCall(t *testing.T) {
	quietLogs(t)
	srv, f := newControllerServer(t)
	p := newPlugin()
	p.client = srv.Client()
	cfg := plugin.Values{"url": srv.URL, "api_key": testKey, "refresh": "1m", "wan": false}
	if err := p.Configure(cfg, &plugin.Request{Version: plugin.Version}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if !p.fetch(context.Background()) {
		t.Fatal("first fetch reported no change")
	}
	for _, path := range f.paths() {
		if strings.Contains(path, "/proxy/network/api/") {
			t.Errorf("wan:false still calls the private endpoint: %v", f.paths())
		}
	}
	p.mu.Lock()
	snap := p.snap
	p.mu.Unlock()
	if !snap.loaded || len(snap.devices) != 1 || snap.clients != 74 {
		t.Errorf("documented half lost: %+v", snap)
	}
	got, err := p.Values(&plugin.Request{Values: []string{"state", "latency", "worst"}})
	if err != nil {
		t.Fatalf("values: %v", err)
	}
	if v := got["state"].Enum; v == nil || *v != "ok" {
		t.Errorf("state = %v, want ok", v)
	}
	if v := got["latency"].Text; v == nil || *v != "–" {
		t.Errorf("latency = %v, want a dash", v)
	}
	if v := got["worst"].Text; v == nil || *v != "" {
		t.Errorf("worst = %v, want an empty line", v)
	}
}

func TestDevicesReadsEveryPage(t *testing.T) {
	var offsets []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset := r.URL.Query().Get("offset")
		offsets = append(offsets, offset)
		var data []device
		if offset == "0" {
			for i := 0; i < devicePageSize; i++ {
				data = append(data, device{ID: "id", Name: "AP"})
			}
		} else {
			data = []device{{ID: "last", Name: "Last"}}
		}
		writeJSON(w, map[string]any{"totalCount": 201, "data": data})
	}))
	defer srv.Close()

	api := &integrationAPI{controller{base: srv.URL, key: testKey, http: srv.Client()}}
	got, err := api.devices(context.Background(), "site")
	if err != nil {
		t.Fatalf("devices: %v", err)
	}
	if len(got) != 201 {
		t.Errorf("devices = %d, want 201", len(got))
	}
	if len(offsets) != 2 || offsets[0] != "0" || offsets[1] != "200" {
		t.Errorf("offsets = %v, want [0 200]", offsets)
	}
}

func TestFailingPrivateCallKeepsDevicesAndClients(t *testing.T) {
	quietLogs(t)
	srv, f := newControllerServer(t)
	p := fetched(t, srv, "")

	// The devices and the clients move on, the private call fails.
	f.setDevices([]byte(`{"offset":0,"limit":25,"count":1,"totalCount":1,"data":[
		{"id":"aa","name":"OG-AP","model":"U7PG2","state":"ONLINE","firmwareUpdatable":true}]}`))
	f.setClients(80)
	f.setHealthFail(true)
	if !p.fetch(context.Background()) {
		t.Fatal("changed fetch reported no change")
	}

	p.mu.Lock()
	snap := p.snap
	p.mu.Unlock()
	if len(snap.devices) != 1 || snap.devices[0].Name != "OG-AP" {
		t.Errorf("devices = %+v, want the fresh list", snap.devices)
	}
	if snap.clients != 80 {
		t.Errorf("clients = %d, want 80", snap.clients)
	}
	if snap.wan.ISPName != "Deutsche Telekom AG" || snap.wan.Status != "ok" {
		t.Errorf("health lost: %+v", snap.wan)
	}
}

func TestPrivateCallLogsOnce(t *testing.T) {
	var log bytes.Buffer
	stderr = &log
	t.Cleanup(func() { stderr = os.Stderr })

	srv, f := newControllerServer(t)
	p := fetched(t, srv, "")
	f.setHealthFail(true)
	p.fetch(context.Background())
	p.fetch(context.Background())
	if got := strings.Count(log.String(), "unifi:"); got != 1 {
		t.Errorf("log lines = %d, want 1:\n%s", got, log.String())
	}

	// After a healthy fetch the same error logs again.
	f.setHealthFail(false)
	p.fetch(context.Background())
	f.setHealthFail(true)
	p.fetch(context.Background())
	if got := strings.Count(log.String(), "unifi:"); got != 2 {
		t.Errorf("log lines after recovery = %d, want 2:\n%s", got, log.String())
	}
}

func TestValues(t *testing.T) {
	srv, _ := newControllerServer(t)
	p := fetched(t, srv, "")
	req := &plugin.Request{Version: plugin.Version, Language: "de",
		Values: []string{"state", "clients", "devices", "offline", "worst", "latency", "download", "upload"}}
	got, err := p.Values(req)
	if err != nil {
		t.Fatalf("values: %v", err)
	}
	if v := got["state"].Enum; v == nil || *v != "ok" {
		t.Errorf("state = %v, want ok", v)
	}
	if v := got["clients"].Number; v == nil || *v != 74 {
		t.Errorf("clients = %v, want 74", v)
	}
	if v := got["devices"].Number; v == nil || *v != 1 {
		t.Errorf("devices = %v, want 1", v)
	}
	if v := got["offline"].Number; v == nil || *v != 0 {
		t.Errorf("offline = %v, want 0", v)
	}
	if v := got["worst"].Text; v == nil || *v != "Deutsche Telekom AG - 10 ms" {
		t.Errorf("worst = %v", v)
	}
	if v := got["latency"].Number; v == nil || *v != 10 {
		t.Errorf("latency = %v, want 10", v)
	}
	// The fixture has no speedtest, so both rates answer a dash.
	if v := got["download"].Text; v == nil || *v != "–" {
		t.Errorf("download = %v, want a dash", got["download"])
	}
	if v := got["upload"].Text; v == nil || *v != "–" {
		t.Errorf("upload = %v, want a dash", got["upload"])
	}

	// An offline device moves the state and the status line.
	p.mu.Lock()
	p.snap.devices = append(p.snap.devices, device{Name: "Keller-AP", State: "OFFLINE"})
	p.mu.Unlock()
	got, err = p.Values(req)
	if err != nil {
		t.Fatalf("values: %v", err)
	}
	if v := got["state"].Enum; v == nil || *v != "degraded" {
		t.Errorf("state = %v, want degraded", v)
	}
	if v := got["worst"].Text; v == nil || *v != "Keller-AP offline" {
		t.Errorf("worst = %v, want the offline line", v)
	}
	if v := got["offline"].Number; v == nil || *v != 1 {
		t.Errorf("offline = %v, want 1", v)
	}
}

func TestFetchKeepsDataOnFailure(t *testing.T) {
	quietLogs(t)
	srv, f := newControllerServer(t)
	p := fetched(t, srv, "")
	// A failing devices request keeps the whole last answer.
	f.setFailDevices(true)
	if p.fetch(context.Background()) {
		t.Error("failed fetch reported a change")
	}
	p.mu.Lock()
	snap := p.snap
	p.mu.Unlock()
	if len(snap.devices) != 1 || snap.devices[0].Name != "Keller-AP" {
		t.Errorf("data lost: %+v", snap.devices)
	}
}
