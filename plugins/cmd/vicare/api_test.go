package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// tokenServer answers the token endpoint and counts its calls.
type tokenServer struct {
	mu      sync.Mutex
	calls   int
	refresh string // the refresh token in the last answer
	forms   []url.Values
}

func (t *tokenServer) handler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/idp/v3/token" {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	t.mu.Lock()
	t.calls++
	t.forms = append(t.forms, r.PostForm)
	refresh := t.refresh
	t.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"access_token":  "access-" + refresh,
		"refresh_token": refresh,
		"expires_in":    3600,
		"token_type":    "Bearer",
	})
}

func TestAccessRenewsAndKeepsTheRotatedRefreshToken(t *testing.T) {
	ts := &tokenServer{refresh: "r2"}
	srv := httptest.NewServer(http.HandlerFunc(ts.handler))
	defer srv.Close()

	v := &vicare{client: srv.Client(), base: srv.URL, iam: srv.URL, clientID: "client", seedRefresh: "seed"}
	access, err := v.access(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if access != "access-r2" {
		t.Fatalf("access = %q", access)
	}
	v.mu.Lock()
	got := v.tokens
	v.mu.Unlock()
	if got == nil || got.Refresh != "r2" || got.Access != "access-r2" {
		t.Fatalf("tokens = %+v", got)
	}
	if ts.calls != 1 {
		t.Fatalf("token calls = %d", ts.calls)
	}
	form := ts.forms[0]
	if form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != "seed" ||
		form.Get("client_id") != "client" {
		t.Fatalf("form = %v", form)
	}
	// The renewed refresh token wins over the seed on the next renewal.
	v.mu.Lock()
	v.tokens.Expiry = time.Now().Add(-time.Minute)
	v.mu.Unlock()
	ts.mu.Lock()
	ts.refresh = "r3"
	ts.mu.Unlock()
	if _, err := v.access(context.Background()); err != nil {
		t.Fatal(err)
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if got := ts.forms[len(ts.forms)-1].Get("refresh_token"); got != "r2" {
		t.Fatalf("second renewal used %q, want the rotated r2", got)
	}
}

func TestAccessUsesAValidToken(t *testing.T) {
	ts := &tokenServer{refresh: "r2"}
	srv := httptest.NewServer(http.HandlerFunc(ts.handler))
	defer srv.Close()

	v := &vicare{client: srv.Client(), base: srv.URL, iam: srv.URL, clientID: "client", seedRefresh: "seed"}
	v.tokens = &tokens{Access: "valid", Refresh: "stored", Expiry: time.Now().Add(time.Hour)}
	access, err := v.access(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if access != "valid" || ts.calls != 0 {
		t.Fatalf("access = %q, calls = %d", access, ts.calls)
	}
}

func TestTokenErrorIsReportedWithoutSecrets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "invalid_grant", "error_description": "refresh token expired",
		})
	}))
	defer srv.Close()

	v := &vicare{client: srv.Client(), base: srv.URL, iam: srv.URL, clientID: "client", seedRefresh: "seed"}
	_, err := v.access(context.Background())
	if err == nil || !strings.Contains(err.Error(), "refresh token expired") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "seed") {
		t.Fatalf("the error names the token: %v", err)
	}
}

func TestAPIRenewsOn401AndRetries(t *testing.T) {
	ts := &tokenServer{refresh: "r2"}
	var features int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/idp/v3/token":
			ts.handler(w, r)
		case strings.HasSuffix(r.URL.Path, "/features"):
			features++
			if features == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	v := &vicare{client: srv.Client(), base: srv.URL, iam: srv.URL, clientID: "client", seedRefresh: "seed"}
	v.tokens = &tokens{Access: "old", Refresh: "seed", Expiry: time.Now().Add(time.Hour)}
	body, status, _, err := v.apiGet(context.Background(), "/iot/v2/features")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || !strings.Contains(string(body), "data") {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	if features != 2 || ts.calls != 1 {
		t.Fatalf("features = %d, token calls = %d", features, ts.calls)
	}
}

func TestExpiredBodyRenews(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/idp/v3/token" {
			json.NewEncoder(w).Encode(map[string]any{
				"access_token": "new", "refresh_token": "seed", "expires_in": 3600,
			})
			return
		}
		got = append(got, r.Header.Get("Authorization"))
		if len(got) == 1 {
			json.NewEncoder(w).Encode(map[string]string{"error": "EXPIRED TOKEN"})
			return
		}
		w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	v := &vicare{client: srv.Client(), base: srv.URL, iam: srv.URL, clientID: "client", seedRefresh: "seed"}
	v.tokens = &tokens{Access: "old", Refresh: "seed", Expiry: time.Now().Add(time.Hour)}
	if _, status, _, err := v.apiGet(context.Background(), "/x"); err != nil || status != http.StatusOK {
		t.Fatalf("status = %d, err = %v", status, err)
	}
	if len(got) != 2 || got[0] != "Bearer old" || got[1] != "Bearer new" {
		t.Fatalf("authorization headers = %v", got)
	}
}

func installationsAnswer() string {
	return `{"data":[{"id":"111","gateways":[
		{"serial":"GW1","devices":[
			{"id":"0","modelId":"Vitocal 250-A","deviceType":"heating","status":"Online","roles":["type:heatpump"]},
			{"id":"1","modelId":"Vitovent 300","deviceType":"ventilation","status":"Online"}
		]}
	]}]}`
}

func TestResolvePicksTheHeatingDevice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/iot/v2/equipment/installations") {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(installationsAnswer()))
	}))
	defer srv.Close()

	v := &vicare{client: srv.Client(), base: srv.URL, iam: srv.URL, clientID: "client", seedRefresh: "seed"}
	v.tokens = &tokens{Access: "a", Refresh: "seed", Expiry: time.Now().Add(time.Hour)}
	inst, gw, dev, err := v.resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if inst.ID != "111" || gw.Serial != "GW1" || dev.ID != "0" || dev.ModelID != "Vitocal 250-A" {
		t.Fatalf("resolved %s %s %+v", inst.ID, gw.Serial, dev)
	}
	if !hasRole(dev.Roles, "type:heatpump") {
		t.Fatalf("roles = %v", dev.Roles)
	}
}

func TestResolveHonoursTheConfiguredIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(installationsAnswer()))
	}))
	defer srv.Close()

	v := &vicare{client: srv.Client(), base: srv.URL, iam: srv.URL, clientID: "client", seedRefresh: "seed",
		installation: "111", gateway: "GW1", device: "1"}
	v.tokens = &tokens{Access: "a", Refresh: "seed", Expiry: time.Now().Add(time.Hour)}
	_, gw, dev, err := v.resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gw.Serial != "GW1" || dev.ID != "1" || dev.DeviceType != "ventilation" {
		t.Fatalf("resolved %s %+v", gw.Serial, dev)
	}
}

func TestResolveRejectsAnUnknownID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(installationsAnswer()))
	}))
	defer srv.Close()

	v := &vicare{client: srv.Client(), base: srv.URL, iam: srv.URL, clientID: "client", seedRefresh: "seed",
		installation: "999"}
	v.tokens = &tokens{Access: "a", Refresh: "seed", Expiry: time.Now().Add(time.Hour)}
	if _, _, _, err := v.resolve(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "not in the account") {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchReadsFeaturesAndRateLimit(t *testing.T) {
	var limited bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if limited {
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]any{"statusCode": 429, "message": "too many"})
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/features") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("includeDevicesFeatures") != "true" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		w.Write([]byte(`{"data":[
			{"feature":"heating.sensors.temperature.outside","deviceId":"0",
			 "timestamp":"2026-11-28T10:00:00Z","properties":{"value":{"type":"number","unit":"celsius","value":4.2}}}
		]}`))
	}))
	defer srv.Close()

	v := &vicare{client: srv.Client(), base: srv.URL, iam: srv.URL, clientID: "client", seedRefresh: "seed",
		installation: "111", gateway: "GW1", device: "0", identified: true}
	v.tokens = &tokens{Access: "a", Refresh: "seed", Expiry: time.Now().Add(time.Hour)}

	snap, wait, err := v.fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap == nil || snap.Outside == nil || *snap.Outside != 4.2 {
		t.Fatalf("snapshot = %+v", snap)
	}
	if wait != 0 {
		t.Fatalf("wait = %v", wait)
	}
	limited = true
	_, wait, err = v.fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("err = %v", err)
	}
	if wait != 2*time.Minute {
		t.Fatalf("wait = %v", wait)
	}
}

func TestCodeFrom(t *testing.T) {
	for _, in := range []string{
		"abc123",
		"  abc123  ",
		"vicare://oauth-callback/everest?code=abc123&state=x",
		"https://example.invalid/?code=abc123&state=x",
		`"vicare://oauth-callback/everest?code=abc123"`,
	} {
		code, err := codeFrom(in)
		if err != nil || code != "abc123" {
			t.Errorf("codeFrom(%q) = %q, %v", in, code, err)
		}
	}
	if _, err := codeFrom("vicare://oauth-callback/everest?error=access_denied"); err == nil {
		t.Error("an error address must not become a code")
	}
	if _, err := codeFrom("  "); err == nil {
		t.Error("an empty answer must not become a code")
	}
}

func TestAuthorizeURLAndExchange(t *testing.T) {
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		form = r.PostForm
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "a", "refresh_token": "r", "expires_in": 3600,
		})
	}))
	defer srv.Close()

	v := &vicare{client: srv.Client(), iam: srv.URL, clientID: "client"}
	verifier, challenge, err := pkce()
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(v.authorizeURL(challenge, "st"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Path != "/idp/v3/authorize" || q.Get("client_id") != "client" ||
		q.Get("code_challenge") != challenge || q.Get("code_challenge_method") != "S256" ||
		q.Get("redirect_uri") != redirectURI || q.Get("response_type") != "code" ||
		q.Get("state") != "st" || !strings.Contains(q.Get("scope"), "offline_access") {
		t.Fatalf("authorize URL = %s", u)
	}
	if len(verifier) < 43 {
		t.Fatalf("verifier is too short: %d", len(verifier))
	}

	tok, err := v.exchangeCode(context.Background(), "the-code", verifier)
	if err != nil {
		t.Fatal(err)
	}
	if tok.Access != "a" || tok.Refresh != "r" || tok.Expiry.Before(time.Now()) {
		t.Fatalf("tokens = %+v", tok)
	}
	if form.Get("grant_type") != "authorization_code" || form.Get("code") != "the-code" ||
		form.Get("client_id") != "client" || form.Get("code_verifier") != verifier ||
		form.Get("redirect_uri") != redirectURI {
		t.Fatalf("form = %v", form)
	}
}
