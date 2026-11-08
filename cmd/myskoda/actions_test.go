package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/maehld/paperboy/pkg/plugin"
)

func TestActionStates(t *testing.T) {
	m := &myskoda{lang: "de"}
	m.data = fixture(t, "enyaq.json")
	req := &plugin.Request{Language: "de", Actions: []plugin.ActionCall{
		{Action: "charging", Args: map[string]any{"state": "start"}},
		{Action: "charging", Args: map[string]any{"state": "stop"}},
		{Action: "climate", Args: map[string]any{"state": "start"}},
		{Action: "climate", Args: map[string]any{"state": "stop"}},
		{Action: "climate", Args: map[string]any{"state": "start", "temperature": 40}},
		{Action: "unknown"},
	}}
	states, err := m.Actions(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != len(req.Actions) {
		t.Fatalf("got %d states for %d actions", len(states), len(req.Actions))
	}
	checks := []struct {
		title    string
		disabled string
	}{
		{"Laden starten", "Lädt bereits"},
		{"Laden stoppen", ""},
		{"Klima starten", "Klimatisierung läuft bereits"},
		{"Klima stoppen", ""},
		{"Klima starten", "outside 16 to 30"},
		{"", "unknown action"},
	}
	for i, c := range checks {
		if c.title != "" && states[i].Title != c.title {
			t.Errorf("state %d title = %q, want %q", i, states[i].Title, c.title)
		}
		if c.disabled == "" && states[i].Disabled != "" {
			t.Errorf("state %d unexpectedly disabled: %q", i, states[i].Disabled)
		}
		if c.disabled != "" && !strings.Contains(states[i].Disabled, c.disabled) {
			t.Errorf("state %d disabled = %q, want %q", i, states[i].Disabled, c.disabled)
		}
	}
}

func TestActionStatesWithoutData(t *testing.T) {
	m := &myskoda{lang: "en"}
	states, err := m.Actions(&plugin.Request{Language: "en",
		Actions: []plugin.ActionCall{{Action: "charging", Args: map[string]any{"state": "start"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || !strings.Contains(states[0].Disabled, "still loading") {
		t.Fatalf("%+v", states)
	}
}

func TestActionStatesSkipUnsupportedOperation(t *testing.T) {
	m := &myskoda{lang: "en"}
	v := fixture(t, "enyaq.json")
	v.Operations = []operation{{Name: "startCharging"}}
	m.data = v
	states, err := m.Actions(&plugin.Request{Language: "en",
		Actions: []plugin.ActionCall{
			{Action: "charging", Args: map[string]any{"state": "start"}},
			{Action: "charging", Args: map[string]any{"state": "stop"}},
		}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(states[1].Disabled, "does not support") {
		t.Fatalf("stop should be disabled: %+v", states[1])
	}
}

// commandServer offers a GET with the enyaq fixture and records the command
// requests, so a test can check endpoint and body.
type commandServer struct {
	*httptest.Server
	mu     sync.Mutex
	method string
	path   string
	body   map[string]any
	status int
	gets   int
}

func newCommandServer(t *testing.T) *commandServer {
	t.Helper()
	fixtureBody, err := os.ReadFile(filepath.Join("testdata", "enyaq.json"))
	if err != nil {
		t.Fatal(err)
	}
	cs := &commandServer{}
	cs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			cs.mu.Lock()
			cs.gets++
			cs.mu.Unlock()
			w.Write(fixtureBody)
			return
		}
		cs.mu.Lock()
		cs.method, cs.path = r.Method, r.URL.Path
		cs.body = nil
		if r.Body != nil {
			json.NewDecoder(r.Body).Decode(&cs.body)
		}
		status := cs.status
		cs.mu.Unlock()
		if status == 0 {
			status = http.StatusAccepted
		}
		w.WriteHeader(status)
		if status == http.StatusUnprocessableEntity {
			fmt.Fprint(w, `{"type":"https://public.api.connect.skoda-auto.cz/problems/operation-disabled","detail":"workshop mode"}`)
		}
	}))
	t.Cleanup(cs.Close)
	return cs
}

func commandPlugin(t *testing.T, cs *commandServer) *myskoda {
	t.Helper()
	m := &myskoda{client: cs.Client()}
	m.data = fixture(t, "enyaq.json")
	if err := m.Configure(plugin.Values{"api_key": "secret", "vin": "TMBJB9NY5RF999999", "base": cs.URL},
		&plugin.Request{}); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestInvokeCharging(t *testing.T) {
	cs := newCommandServer(t)
	m := commandPlugin(t, cs)
	out, err := m.Invoke(&plugin.Request{Language: "de"},
		plugin.ActionCall{Action: "charging", Args: map[string]any{"state": "stop"}})
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK || !out.Changed {
		t.Fatalf("%+v", out)
	}
	if out.Message != "Laden stoppen" {
		t.Fatalf("message = %q", out.Message)
	}
	if cs.method != http.MethodPost || cs.path != "/api/v1/vehicles/TMBJB9NY5RF999999/charging/stop" {
		t.Fatalf("command %s %s", cs.method, cs.path)
	}
	if cs.gets == 0 {
		t.Fatal("the state was not reloaded after the command")
	}
}

func TestInvokeClimateUsesTarget(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want float64
	}{
		{"plan value rounded", map[string]any{"state": "start", "temperature": 21.3}, 21.5},
		{"default from car", map[string]any{"state": "start"}, 22.5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cs := newCommandServer(t)
			m := commandPlugin(t, cs)
			out, err := m.Invoke(&plugin.Request{Language: "en"},
				plugin.ActionCall{Action: "climate", Args: c.args})
			if err != nil || !out.OK {
				t.Fatalf("%+v %v", out, err)
			}
			if cs.path != "/api/v1/vehicles/TMBJB9NY5RF999999/air-conditioning/start" {
				t.Fatalf("path = %s", cs.path)
			}
			temp, ok := cs.body["targetTemperature"].(map[string]any)
			if !ok {
				t.Fatalf("no targetTemperature: %+v", cs.body)
			}
			if temp["value"] != c.want || temp["unit"] != "CELSIUS" {
				t.Fatalf("targetTemperature = %+v, want %v", temp, c.want)
			}
		})
	}
}

func TestInvokeFailure(t *testing.T) {
	cs := newCommandServer(t)
	cs.status = http.StatusUnprocessableEntity
	m := commandPlugin(t, cs)
	out, err := m.Invoke(&plugin.Request{Language: "en"},
		plugin.ActionCall{Action: "charging", Args: map[string]any{"state": "start"}})
	if err != nil {
		t.Fatal(err)
	}
	if out.OK || out.Changed || !strings.Contains(out.Message, "disabled") {
		t.Fatalf("%+v", out)
	}
}

func TestInvokeUnknownAction(t *testing.T) {
	m := &myskoda{}
	if _, err := m.Invoke(&plugin.Request{}, plugin.ActionCall{Action: "nope"}); err == nil {
		t.Fatal("an unknown action should be an error")
	}
}
