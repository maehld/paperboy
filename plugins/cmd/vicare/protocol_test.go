package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
)

// rewrite sends every request of the plugin to the test server, whatever host
// the configuration or the token URL names.
type rewrite struct{ base string }

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	u, err := url.Parse(r.base)
	if err != nil {
		return nil, err
	}
	next := req.Clone(req.Context())
	next.URL.Scheme, next.URL.Host = u.Scheme, u.Host
	return http.DefaultTransport.RoundTrip(next)
}

// TestProtocol runs the plugin as Paperboy talks to it: describe, configure,
// state, render and values over the JSON lines of the protocol.
func TestProtocol(t *testing.T) {
	features := featuresJSON
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/idp/v3/token":
			json.NewEncoder(w).Encode(map[string]any{
				"access_token": "access", "refresh_token": "rotated", "expires_in": 3600,
			})
		case strings.HasPrefix(r.URL.Path, "/iot/v2/equipment/installations"):
			w.Write([]byte(installationsAnswer()))
		case strings.Contains(r.URL.Path, "/features"):
			w.Write([]byte(features))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	v := &vicare{client: &http.Client{Transport: rewrite{base: srv.URL}, Timeout: requestTimeout}}
	done := make(chan error, 1)
	go func() { done <- plugin.ServeIO(ctx, v, inR, outW) }()

	enc := json.NewEncoder(inW)
	dec := json.NewDecoder(outR)
	now := time.Now()
	send := func(r plugin.Request) {
		t.Helper()
		r.Version = plugin.Version
		if err := enc.Encode(r); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	next := func() plugin.Message {
		t.Helper()
		var m plugin.Message
		if err := dec.Decode(&m); err != nil {
			t.Fatalf("read: %v", err)
		}
		if m.Error != "" {
			t.Fatalf("answer %s: %s", m.ID, m.Error)
		}
		return m
	}

	send(plugin.Request{ID: "1", Action: plugin.ActionDescribe, Language: "de", Now: now})
	m := next()
	if m.Description == nil || len(m.Description.Widgets) != 7 || m.Description.Version == "" {
		t.Fatalf("description = %+v", m.Description)
	}

	send(plugin.Request{ID: "2", Action: plugin.ActionConfigure, Language: "de", Timezone: "Europe/Berlin", Now: now,
		Config: map[string]any{
			"client_id": "client", "refresh_token": "seed", "base": "https://api.invalid",
			"refresh": "10m",
		}})
	next()

	send(plugin.Request{ID: "3", Action: plugin.ActionLoadState, Now: now})
	next()

	send(plugin.Request{ID: "4", Action: plugin.ActionRender, Language: "de", Timezone: "Europe/Berlin", Now: now,
		Levels: 4, Page: plugin.PageInfo{Width: 800, Height: 480, PPI: 125},
		Widgets: []plugin.WidgetRequest{{Widget: "heating", Width: 400, Height: 120,
			Style: plugin.Style{Padding: 8, Rule: 1, TextPx: textPx}}}})
	m = next()
	if len(m.Widgets) != 1 || len(m.Widgets[0].PNG) == 0 || m.Widgets[0].Error != "" {
		t.Fatalf("render = %+v", m.Widgets)
	}
	var st storedState
	if m.State == nil {
		t.Fatal("the answer carries no state")
	}
	if err := json.Unmarshal(*m.State, &st); err != nil {
		t.Fatalf("state: %v", err)
	}
	if st.Tokens == nil || st.Tokens.Refresh != "rotated" || st.Installation != "111" ||
		st.Gateway != "GW1" || st.Device != "0" || st.Snapshot == nil {
		t.Fatalf("state = %+v", st)
	}
	if st.Key == "" {
		t.Fatal("the state carries no fingerprint")
	}

	send(plugin.Request{ID: "5", Action: plugin.ActionValues, Language: "en", Timezone: "Europe/Berlin", Now: now,
		Page: plugin.PageInfo{Width: 20, Height: 4}, Values: []string{"outside", "state", "program"}})
	m = next()
	if m.Values["outside"].Number == nil || *m.Values["outside"].Number != 4.2 {
		t.Fatalf("values = %+v", m.Values)
	}
	if e := m.Values["state"].Enum; e == nil || *e != "error" {
		t.Fatalf("state value = %+v", m.Values["state"])
	}

	send(plugin.Request{ID: "6", Action: plugin.ActionRender, Language: "de", Now: now, Levels: 4,
		Widgets: []plugin.WidgetRequest{{Widget: "nope", Width: 400, Height: 120}}})
	m = next()
	if len(m.Widgets) != 1 || m.Widgets[0].Error == "" {
		t.Fatalf("an unknown widget did not fail: %+v", m.Widgets)
	}

	inW.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the plugin did not end with its input")
	}
}
