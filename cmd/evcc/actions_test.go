package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

// fakeEvcc answers /api/state and evcc's mode endpoint, and remembers the
// modes the actions switch.
type fakeEvcc struct {
	mu     sync.Mutex
	modes  []string
	refuse bool
}

func (f *fakeEvcc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method == http.MethodGet && r.URL.Path == "/api/state" {
		fmt.Fprintf(w, `{"loadpoints":[{"title":"Wohnmobil","mode":%q},{"title":"Garage","mode":%q}]}`,
			f.modes[0], f.modes[1])
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if r.Method == http.MethodPost && len(parts) == 5 && parts[0] == "api" &&
		parts[1] == "loadpoints" && parts[3] == "mode" {
		if f.refuse {
			http.Error(w, "loadpoint is locked", http.StatusBadRequest)
			return
		}
		i, err := strconv.Atoi(parts[2])
		if err != nil || i < 0 || i >= len(f.modes) {
			http.Error(w, "unknown loadpoint", http.StatusNotFound)
			return
		}
		f.modes[i] = parts[4]
		return
	}
	http.NotFound(w, r)
}

func modeCall(index float64, mode string) plugin.ActionCall {
	return plugin.ActionCall{Action: "mode", Args: map[string]any{"loadpoint": index, "mode": mode}}
}

func newActionPlugin(t *testing.T, handler http.Handler, lang string) (*evcc, *plugin.Request) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	e := &evcc{client: srv.Client()}
	req := &plugin.Request{Language: lang, Timezone: "UTC", Now: time.Unix(1794002400, 0)}
	if err := e.Configure(plugin.Values{"url": srv.URL}, req); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e.Start(ctx, func(...string) {})
	select {
	case <-e.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("evcc state did not load")
	}
	return e, req
}

func TestDescribeModeAction(t *testing.T) {
	d := (&evcc{}).Describe()
	if len(d.Actions) != 1 || d.Actions[0].Name != "mode" {
		t.Fatalf("actions: %+v", d.Actions)
	}
	spec := d.Actions[0]
	if spec.Title == "" || spec.Confirm == nil || !spec.Confirm.Required {
		t.Fatalf("a mode change must be confirmed: %+v", spec)
	}
	if !spec.Args["loadpoint"].Required || !spec.Args["mode"].Required {
		t.Errorf("both args must be required: %+v", spec.Args)
	}
	if !spec.Args["mode"].Allows("now") || spec.Args["mode"].Allows("fast") {
		t.Errorf("mode enum: %v", spec.Args["mode"].Enum)
	}
}

func TestActionStates(t *testing.T) {
	f := &fakeEvcc{modes: []string{"pv", "off"}}
	e, req := newActionPlugin(t, f, "de")
	req.Actions = []plugin.ActionCall{
		modeCall(0, "now"),
		modeCall(1, "pv"),
		modeCall(0, "pv"),  // already active
		modeCall(5, "now"), // no such loadpoint
		modeCall(0, "fast"),
	}
	states, err := e.Actions(req)
	if err != nil {
		t.Fatal(err)
	}
	wantTitles := []string{"Wohnmobil: jetzt laden", "Garage: nur PV", "Wohnmobil: nur PV", "", "Wohnmobil: fast"}
	for i, want := range wantTitles {
		if states[i].Title != want {
			t.Errorf("state %d title: %q, want %q", i, states[i].Title, want)
		}
	}
	for _, i := range []int{0, 1} {
		if states[i].Disabled != "" {
			t.Errorf("state %d: available action disabled: %q", i, states[i].Disabled)
		}
	}
	if !strings.Contains(states[2].Disabled, "bereits") {
		t.Errorf("active mode: %q", states[2].Disabled)
	}
	if !strings.Contains(states[3].Disabled, "Ladepunkt 5") || !strings.Contains(states[3].Disabled, "2") {
		t.Errorf("unknown loadpoint: %q", states[3].Disabled)
	}
	if !strings.Contains(states[4].Disabled, "fast") {
		t.Errorf("invalid mode: %q", states[4].Disabled)
	}

	en, enReq := newActionPlugin(t, f, "en")
	enReq.Actions = []plugin.ActionCall{modeCall(0, "now")}
	states, err = en.Actions(enReq)
	if err != nil || states[0].Title != "Wohnmobil: charge now" {
		t.Fatalf("english title: %+v %v", states, err)
	}
}

func TestActionStatesWithoutData(t *testing.T) {
	req := &plugin.Request{Language: "en", Actions: []plugin.ActionCall{modeCall(0, "now")}}
	states, err := (&evcc{}).Actions(req)
	if err != nil || len(states) != 1 || !strings.Contains(states[0].Disabled, "no data") {
		t.Fatalf("no data: %+v %v", states, err)
	}
}

func TestActionStatesUnreachable(t *testing.T) {
	srv := httptest.NewServer(&fakeEvcc{modes: []string{"pv", "off"}})
	e := &evcc{client: srv.Client()}
	req := &plugin.Request{Language: "de"}
	if err := e.Configure(plugin.Values{"url": srv.URL}, req); err != nil {
		t.Fatal(err)
	}
	srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e.Start(ctx, func(...string) {})
	<-e.ready
	req.Actions = []plugin.ActionCall{modeCall(0, "now")}
	states, err := e.Actions(req)
	if err != nil || len(states) != 1 || !strings.Contains(states[0].Disabled, "nicht erreichbar") {
		t.Fatalf("unreachable: %+v %v", states, err)
	}
}

func TestInvokeMode(t *testing.T) {
	f := &fakeEvcc{modes: []string{"pv", "off"}}
	e, req := newActionPlugin(t, f, "de")
	out, err := e.Invoke(req, modeCall(0, "now"))
	if err != nil || !out.OK || !out.Changed {
		t.Fatalf("invoke: %+v %v", out, err)
	}
	if out.Message != "Wohnmobil: jetzt laden" {
		t.Errorf("message: %q", out.Message)
	}
	f.mu.Lock()
	mode := f.modes[0]
	f.mu.Unlock()
	if mode != "now" {
		t.Errorf("evcc mode: %q", mode)
	}
	e.mu.Lock()
	shown := e.data.Loadpoints[0].Mode
	e.mu.Unlock()
	if shown != "now" {
		t.Errorf("state was not refreshed: %q", shown)
	}
}

func TestInvokeRefused(t *testing.T) {
	f := &fakeEvcc{modes: []string{"pv", "off"}, refuse: true}
	e, req := newActionPlugin(t, f, "en")
	out, err := e.Invoke(req, modeCall(0, "now"))
	if err != nil || out.OK {
		t.Fatalf("refused: %+v %v", out, err)
	}
	if !strings.Contains(out.Message, "HTTP 400") || !strings.Contains(out.Message, "locked") {
		t.Errorf("message: %q", out.Message)
	}
}

func TestInvokeUnreachable(t *testing.T) {
	srv := httptest.NewServer(&fakeEvcc{modes: []string{"pv"}})
	e := &evcc{client: srv.Client(), base: srv.URL}
	srv.Close()
	out, err := e.Invoke(&plugin.Request{Language: "de"}, modeCall(0, "now"))
	if err != nil || out.OK || !strings.Contains(out.Message, "nicht erreichbar") {
		t.Fatalf("unreachable: %+v %v", out, err)
	}
}

func TestInvokeRejectsInvalidCall(t *testing.T) {
	e := &evcc{}
	out, err := e.Invoke(&plugin.Request{Language: "en"}, plugin.ActionCall{Action: "mode",
		Args: map[string]any{"loadpoint": 0.0}})
	if err != nil || out.OK || !strings.Contains(out.Message, "mode") {
		t.Fatalf("missing argument: %+v %v", out, err)
	}
	if _, err := e.Invoke(&plugin.Request{}, plugin.ActionCall{Action: "toggle"}); err == nil {
		t.Fatal("an unknown action must fail the request")
	}
}
