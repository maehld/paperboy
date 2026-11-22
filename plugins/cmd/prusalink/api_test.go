package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
)

const (
	testKey   = "secret"
	testNonce = "dcd98b7102dd2f0e8b11d0f600bfb0c093"
)

// printerFixture answers the three endpoints the plugin reads. It accepts the
// API key in X-Api-Key or, with digestOnly, only a correct Digest answer.
type printerFixture struct {
	mu         sync.Mutex
	requests   []string
	digestOnly bool
	fail       bool
	noJob      bool
	state      string
}

func newPrinterServer(t *testing.T) (*httptest.Server, *printerFixture) {
	t.Helper()
	f := &printerFixture{state: "PRINTING"}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return srv, f
}

func (f *printerFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	digestOnly, fail, noJob, state := f.digestOnly, f.fail, f.noJob, f.state
	f.mu.Unlock()

	if !f.authorized(r, digestOnly) {
		w.Header().Set("WWW-Authenticate",
			fmt.Sprintf(`Digest realm="PrusaLink", qop="auth", nonce="%s", opaque="opaque", algorithm=MD5`, testNonce))
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if fail {
		http.Error(w, "down", http.StatusServiceUnavailable)
		return
	}
	switch r.URL.Path {
	case "/api/v1/status":
		body := map[string]any{
			"printer": map[string]any{
				"state": state, "temp_nozzle": 214.9, "target_nozzle": 215.0,
				"temp_bed": 59.6, "target_bed": 60.0, "speed": 100, "flow": 95,
				"status_printer": map[string]any{"ok": true, "message": "OK"},
			},
		}
		if !noJob {
			body["job"] = map[string]any{"id": 42, "progress": 42.4, "time_remaining": 5025, "time_printing": 1325}
		}
		writeJSON(w, body)
	case "/api/v1/job":
		if noJob {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, map[string]any{
			"id": 42, "state": state, "progress": 42.4, "time_remaining": 5025, "time_printing": 1325,
			"file": map[string]any{"name": "BENCHY~1.GCO", "display_name": "Benchy_0.2mm_PLA_COREONE_1h23m.gcode"},
		})
	case "/api/v1/info":
		writeJSON(w, map[string]any{"name": "Core One", "serial": "CZPX0000X000XC00000", "mmu": false})
	default:
		http.NotFound(w, r)
	}
}

func (f *printerFixture) authorized(r *http.Request, digestOnly bool) bool {
	if !digestOnly && r.Header.Get("X-Api-Key") == testKey {
		return true
	}
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Digest ") {
		return false
	}
	p := authParams(header[len("Digest "):])
	if p["username"] != digestUser || p["nonce"] != testNonce {
		return false
	}
	ha1 := md5Hex("maker:" + p["realm"] + ":" + testKey)
	ha2 := md5Hex(r.Method + ":" + p["uri"])
	want := md5Hex(strings.Join([]string{ha1, p["nonce"], p["nc"], p["cnonce"], p["qop"], ha2}, ":"))
	return p["response"] == want
}

func (f *printerFixture) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func configured(t *testing.T, base string) *prusalink {
	t.Helper()
	p := newPlugin()
	cfg := plugin.Values{
		"url":     base,
		"api_key": testKey,
		"refresh": "1m",
	}
	if err := p.Configure(cfg, &plugin.Request{Version: plugin.Version}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	return p
}

func reloaded(t *testing.T, base string) *prusalink {
	t.Helper()
	p := configured(t, base)
	if _, failed := p.reload(context.Background()); failed {
		t.Fatalf("reload failed: %v", p.cur.err)
	}
	return p
}

func TestConfigure(t *testing.T) {
	p := newPlugin()
	req := &plugin.Request{Version: plugin.Version}
	if err := p.Configure(plugin.Values{"url": "http://printer", "api_key": "k"}, req); err != nil {
		t.Errorf("minimal config: %v", err)
	}
	cases := []struct {
		name string
		cfg  plugin.Values
		want string
	}{
		{"missing url", plugin.Values{"api_key": "k"}, "url"},
		{"bad url", plugin.Values{"url": "printer.local", "api_key": "k"}, "http(s)"},
		{"missing key", plugin.Values{"url": "http://printer"}, "api_key"},
		{"refresh", plugin.Values{"url": "http://printer", "api_key": "k", "refresh": "10s"}, "30s"},
		{"unknown", plugin.Values{"url": "http://printer", "api_key": "k", "nope": true}, "nope"},
	}
	for _, c := range cases {
		err := p.Configure(c.cfg, req)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
	if err := p.Configure(plugin.Values{"url": "http://printer", "api_key": "k"}, &plugin.Request{Version: 8}); err == nil {
		t.Error("protocol 8 accepted")
	}
}

func TestFetch(t *testing.T) {
	srv, f := newPrinterServer(t)
	p := reloaded(t, srv.URL)

	if got := p.cur.state(); got != "PRINTING" {
		t.Errorf("state = %q", got)
	}
	if got, ok := p.cur.progress(); !ok || got != 42.4 {
		t.Errorf("progress = %v, %v", got, ok)
	}
	if got, ok := p.cur.remaining(); !ok || got != 5025*time.Second {
		t.Errorf("remaining = %v, %v", got, ok)
	}
	if got := p.cur.jobName(); got != "Benchy_0.2mm_PLA_COREONE_1h23m.gcode" {
		t.Errorf("job = %q", got)
	}
	if got := p.cur.info.Name; got != "Core One" {
		t.Errorf("name = %q", got)
	}
	if got := f.count(); got != 3 {
		t.Errorf("requests = %d, want 3", got)
	}
}

func TestFetchIdleWithoutJob(t *testing.T) {
	srv, f := newPrinterServer(t)
	f.mu.Lock()
	f.state, f.noJob = "IDLE", true
	f.mu.Unlock()
	p := reloaded(t, srv.URL)

	if got := p.cur.state(); got != "IDLE" {
		t.Errorf("state = %q", got)
	}
	if p.cur.jobName() != "" {
		t.Errorf("job = %q, want none", p.cur.jobName())
	}
	if _, ok := p.cur.progress(); ok {
		t.Error("progress present without a job")
	}
}

func TestDigestAuth(t *testing.T) {
	srv, f := newPrinterServer(t)
	f.mu.Lock()
	f.digestOnly = true
	f.mu.Unlock()
	p := configured(t, srv.URL)
	if _, failed := p.reload(context.Background()); failed {
		t.Fatalf("reload: %v", p.cur.err)
	}
	// One 401 for the challenge, then status, job and info.
	if got := f.count(); got != 4 {
		t.Errorf("first requests = %d, want 4", got)
	}
	if _, failed := p.reload(context.Background()); failed {
		t.Fatalf("second reload: %v", p.cur.err)
	}
	// The cached challenge needs no new 401: status and job.
	if got := f.count(); got != 6 {
		t.Errorf("requests after second load = %d, want 6", got)
	}
}

func TestDigestWrongKey(t *testing.T) {
	srv, f := newPrinterServer(t)
	f.mu.Lock()
	f.digestOnly = true
	f.mu.Unlock()
	p := configured(t, srv.URL)
	p.apiKey = "wrong"
	_, failed := p.reload(context.Background())
	if !failed || !strings.Contains(p.cur.err.Error(), "api_key") {
		t.Fatalf("err = %v, want a hint about api_key", p.cur.err)
	}
}

func TestFailedFetchKeepsData(t *testing.T) {
	srv, f := newPrinterServer(t)
	p := reloaded(t, srv.URL)
	f.mu.Lock()
	f.fail = true
	f.mu.Unlock()
	changed, failed := p.reload(context.Background())
	if !failed {
		t.Error("failed = false")
	}
	if changed {
		t.Error("changed = true on the first failure")
	}
	if p.cur.offline() {
		t.Error("offline after one failed request")
	}
	if !p.cur.loaded || p.cur.state() != "PRINTING" {
		t.Errorf("data lost: %+v", p.cur)
	}

	changed, failed = p.reload(context.Background())
	if !failed {
		t.Error("second failure: failed = false")
	}
	if !changed {
		t.Error("second failure: changed = false")
	}
	if !p.cur.offline() {
		t.Error("one failure after the first keeps the widgets online")
	}
	if got := p.cur.signature(); got != "offline" {
		t.Errorf("signature = %q, want offline", got)
	}

	f.mu.Lock()
	f.fail = false
	f.mu.Unlock()
	if changed, failed := p.reload(context.Background()); failed || !changed {
		t.Errorf("recovery: changed = %v, failed = %v", changed, failed)
	}
	if p.cur.offline() || p.cur.failures != 0 {
		t.Errorf("still offline after an answer: %+v", p.cur)
	}
}

func TestValues(t *testing.T) {
	srv, _ := newPrinterServer(t)
	p := reloaded(t, srv.URL)
	req := &plugin.Request{Values: []string{"state", "progress", "time-remaining", "nozzle", "bed", "job"}}
	got, err := p.Values(req)
	if err != nil {
		t.Fatalf("values: %v", err)
	}
	if v := got["state"].Enum; v == nil || *v != "printing" {
		t.Errorf("state = %v", v)
	}
	if v := got["progress"].Number; v == nil || *v != 42.4 {
		t.Errorf("progress = %v", v)
	}
	if v := got["time-remaining"].Duration; v == nil || *v != 5025 {
		t.Errorf("remaining = %v", v)
	}
	if v := got["nozzle"].Number; v == nil || *v != 214.9 {
		t.Errorf("nozzle = %v", v)
	}
	if v := got["job"].Text; v == nil || !strings.HasPrefix(*v, "Benchy") {
		t.Errorf("job = %v", v)
	}
	if _, err := p.Values(&plugin.Request{Values: []string{"nope"}}); !errors.Is(err, plugin.ErrUnknownValue) {
		t.Errorf("unknown value: err = %v", err)
	}
}

func TestValuesOffline(t *testing.T) {
	srv, f := newPrinterServer(t)
	p := reloaded(t, srv.URL)
	f.mu.Lock()
	f.fail = true
	f.mu.Unlock()
	for range 2 {
		p.reload(context.Background())
	}
	got, err := p.Values(&plugin.Request{Values: []string{"state", "progress", "nozzle", "job"}})
	if err != nil {
		t.Fatalf("values: %v", err)
	}
	if v := got["state"].Enum; v == nil || *v != "offline" {
		t.Errorf("state = %v, want offline", v)
	}
	for _, name := range []string{"progress", "nozzle", "job"} {
		if v := got[name].Text; v == nil || *v != "–" {
			t.Errorf("%s = %v, want a dash", name, got[name])
		}
	}
}

func TestValuesWithoutData(t *testing.T) {
	p := newPlugin()
	got, err := p.Values(&plugin.Request{Values: []string{"state", "progress", "job"}})
	if err != nil {
		t.Fatalf("values: %v", err)
	}
	if v := got["state"].Enum; v == nil || *v != "offline" {
		t.Errorf("state = %v, want offline", v)
	}
	if v := got["progress"].Text; v == nil || *v != "–" {
		t.Errorf("progress = %v, want a dash", got["progress"])
	}
}

func TestSnapshotSignature(t *testing.T) {
	srv, _ := newPrinterServer(t)
	p := reloaded(t, srv.URL)
	sig := p.cur.signature()
	p.mu.Lock()
	p.cur.status.Job.Progress = floatPtr(42.1)
	p.mu.Unlock()
	if p.cur.signature() != sig {
		t.Error("a fraction of a percent changed the signature")
	}
	p.mu.Lock()
	p.cur.status.Job.Progress = floatPtr(43)
	p.mu.Unlock()
	if p.cur.signature() == sig {
		t.Error("a whole percent did not change the signature")
	}
}

func floatPtr(v float64) *float64 { return &v }
