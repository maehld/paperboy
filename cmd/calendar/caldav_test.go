package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestCredentialHost pins where the app password may travel (ADR 0141).
func TestCredentialHost(t *testing.T) {
	for _, tc := range []struct {
		from, to string
		want     bool
	}{
		{"caldav.example.com", "caldav.example.com", true},
		{"CALDAV.EXAMPLE.COM", "caldav.example.com", true},
		{"caldav.example.com:8443", "caldav.example.com", true},
		{"caldav.icloud.com", "p42-caldav.icloud.com", true},
		{"p42-caldav.icloud.com", "icloud.com", true},
		{"caldav.example.com", "p42-caldav.icloud.com", false},
		{"caldav.example.com", "caldav.example.com.evil.net", false},
		{"caldav.example.com", "noticloud.com", false},
		{"caldav.example.com", "icloud.com.evil.net", false},
	} {
		if got := credentialHost(tc.from, tc.to); got != tc.want {
			t.Errorf("credentialHost(%q, %q) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

// TestCredentialsDropOnForeignRedirect sends the app password to a configured
// server and follows it to another host: the target must see no credentials
// (ADR 0141).
func TestCredentialsDropOnForeignRedirect(t *testing.T) {
	var leaked bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			leaked = true
		}
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ok")
	}))
	defer target.Close()
	foreign, err := url.Parse(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	// A different host name for the same listener; userinfo would turn into an
	// Authorization header without the explicit drop.
	port := foreign.Port()
	foreignURL := "http://leak:secret@localhost:" + port + "/next"

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("the first request needs the credentials")
		}
		http.Redirect(w, r, foreignURL, http.StatusMovedPermanently)
	}))
	defer origin.Close()

	u, err := url.Parse(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	final, data, status, err := newClient("me@example.com", "app-pass").
		do(context.Background(), http.MethodGet, u, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(data) != "ok" || final.Path != "/next" {
		t.Fatalf("redirect: %d %q %v", status, data, final)
	}
	if leaked {
		t.Fatal("the app password reached another host")
	}
	if final.User != nil {
		t.Fatalf("the redirect kept userinfo: %v", final.User)
	}
}

// TestCredentialsStayOnSameHost keeps the password for a redirect on the same
// host, which a reverse proxy or a well-known path uses.
func TestCredentialsStayOnSameHost(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/end", http.StatusFound)
			return
		}
		io.WriteString(w, "ok")
	}))
	defer srv.Close()
	u, err := url.Parse(srv.URL + "/start")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, status, err := newClient("u", "p").do(context.Background(), http.MethodGet, u, "", nil); err != nil || status != http.StatusOK {
		t.Fatalf("redirect: %d %v", status, err)
	}
	if len(seen) != 2 || seen[0] == "" || seen[1] != seen[0] {
		t.Fatalf("credentials across a same-host redirect: %v", seen)
	}
	if !strings.HasPrefix(seen[0], "Basic ") {
		t.Fatalf("authorization header: %q", seen[0])
	}
}
