package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"image"
	"io"
	"testing"
	"time"
)

type testHandler struct{ started chan func(...string) }

func (h *testHandler) Describe() Description { return Description{Title: "t"} }
func (h *testHandler) Configure(cfg Values, _ *Request) error {
	return cfg.Check(map[string]Arg{"name": {Type: "string", Required: true}})
}
func (h *testHandler) Render(_ *Request, w WidgetRequest, img *image.Paletted) error {
	if w.Widget == "bad" {
		return errors.New("bad widget")
	}
	return nil
}
func (h *testHandler) Text(_ *Request, w WidgetRequest) ([]string, error) {
	return []string{Columns(w.Widget, "1", w.Width)}, nil
}
func (h *testHandler) Start(_ context.Context, notify func(...string)) { h.started <- notify }
func (h *testHandler) MaxAge(*Request) time.Duration                   { return time.Minute }

func TestServeIO(t *testing.T) {
	h := &testHandler{started: make(chan func(...string), 1)}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go ServeIO(context.Background(), h, inR, outW)
	enc := json.NewEncoder(inW)
	dec := bufio.NewScanner(outR)
	next := func() Message {
		if !dec.Scan() {
			t.Fatal("no answer")
		}
		var m Message
		if err := json.Unmarshal(dec.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	enc.Encode(Request{ID: "1", Action: ActionConfigure, Config: map[string]any{"nme": "x"}})
	if m := next(); m.ID != "1" || m.Error == "" || m.Version != Version {
		t.Fatalf("bad config accepted: %+v", m)
	}
	enc.Encode(Request{ID: "2", Action: ActionConfigure, Config: map[string]any{"name": "x"}})
	if m := next(); m.Error != "" {
		t.Fatalf("configure: %+v", m)
	}
	notify := <-h.started
	enc.Encode(Request{ID: "3", Action: ActionRender, Levels: 4, Widgets: []WidgetRequest{{Widget: "ok", Width: 3, Height: 2}, {Widget: "bad", Width: 3, Height: 2}}})
	m := next()
	if m.ID != "3" || len(m.Widgets) != 2 || len(m.Widgets[0].PNG) == 0 || m.Widgets[1].Error != "bad widget" || m.MaxAge != 60 {
		t.Fatalf("render: %+v", m)
	}
	enc.Encode(Request{ID: "4", Action: ActionText, Widgets: []WidgetRequest{{Widget: "pv", Width: 6, Height: 1}}})
	if m := next(); len(m.Widgets) != 1 || m.Widgets[0].Lines[0] != "pv   1" {
		t.Fatalf("text: %+v", m)
	}
	go notify("agenda")
	if m := next(); m.Event != EventChanged || m.Changed[0] != "agenda" {
		t.Fatalf("event: %+v", m)
	}
	inW.Close()
}

func TestCheckVersion(t *testing.T) {
	for _, ok := range []Message{{Version: Version}, {Version: Version, Error: "failed"}} {
		if err := ok.CheckVersion(); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
	}
	if err := (Message{}).CheckVersion(); err == nil {
		t.Error("a plugin without a version accepted")
	}
	if err := (Message{Version: Version + 1}).CheckVersion(); err == nil {
		t.Error("a newer plugin accepted")
	}
}

type enumHandler struct{ testHandler }

func (enumHandler) Describe() Description {
	return Description{Widgets: []WidgetInfo{{Widget: "w", Args: map[string]Arg{
		"align":   {Type: "string", Enum: []any{"left", "right"}},
		"columns": {Type: "int", Enum: []any{2, 4}},
	}}}}
}

func TestEnums(t *testing.T) {
	known := enumHandler{}.Describe().Widgets[0].Args
	for _, ok := range []Values{{}, {"align": "left"}, {"columns": float64(4)}, {"other": "x"}} {
		if err := ok.CheckEnums(known); err != nil {
			t.Errorf("%v: %v", ok, err)
		}
	}
	for _, bad := range []Values{{"align": "middle"}, {"columns": 3}} {
		if err := bad.CheckEnums(known); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	if err := (Values{"align": "up"}).Check(known); err == nil || err.Error() != "align: up is not one of left, right" {
		t.Errorf("check: %v", err)
	}
	m := Handle(&enumHandler{}, &Request{Action: ActionRender, Levels: 2, Widgets: []WidgetRequest{
		{Widget: "w", Width: 2, Height: 2, Args: map[string]any{"align": "right"}},
		{Widget: "w", Width: 2, Height: 2, Args: map[string]any{"columns": float64(3)}}}})
	if len(m.Widgets) != 2 || m.Widgets[0].Error != "" || m.Widgets[1].Error != "columns: 3 is not one of 2, 4" {
		t.Fatalf("handle: %+v", m)
	}
}

type actionHandler struct{ testHandler }

var modeArgs = map[string]Arg{"mode": {Type: "string", Required: true, Enum: []any{"now", "off"}}}

func (actionHandler) Describe() Description {
	return Description{
		Title: "a",
		Actions: []ActionSpec{
			{Name: "mode", Title: "Charge mode", Args: modeArgs, Confirm: &Confirm{Required: true, Text: "Change the mode?"}},
			{Name: "refresh", Title: "Refresh"},
		},
	}
}

func (actionHandler) Actions(req *Request) ([]ActionState, error) {
	out := make([]ActionState, len(req.Actions))
	for i, c := range req.Actions {
		out[i] = ActionState{Title: c.Action}
		if c.Action == "mode" {
			if err := c.Arg().Check(modeArgs); err != nil {
				out[i].Disabled = err.Error()
			}
		}
	}
	return out, nil
}

func (actionHandler) Invoke(_ *Request, call ActionCall) (ActionOutcome, error) {
	switch call.Action {
	case "fail":
		return ActionOutcome{}, errors.New("boom")
	case "no":
		return ActionOutcome{Message: "not now"}, nil
	}
	return ActionOutcome{OK: true, Message: "done", Changed: true}, nil
}

func TestActions(t *testing.T) {
	h := &actionHandler{}
	if d := h.Describe(); len(d.Actions) != 2 || d.Actions[0].Confirm == nil || !d.Actions[0].Confirm.Required {
		t.Fatalf("describe: %+v", d)
	}
	m := Handle(h, &Request{Action: ActionList, Device: &DeviceInfo{ID: "kueche"},
		Actions: []ActionCall{{Action: "mode", Args: map[string]any{"mode": "now"}}, {Action: "refresh"}}})
	if m.Error != "" || len(m.Actions) != 2 || m.Actions[0].Disabled != "" || m.Actions[1].Title != "refresh" || m.MaxAge != 60 {
		t.Fatalf("actions: %+v", m)
	}
	m = Handle(h, &Request{Action: ActionList,
		Actions: []ActionCall{{Action: "mode", Args: map[string]any{"mode": "later"}}}})
	if m.Error != "" || len(m.Actions) != 1 || m.Actions[0].Disabled == "" {
		t.Fatalf("invalid args must disable the action: %+v", m)
	}
	m = Handle(h, &Request{Action: ActionInvoke, Actions: []ActionCall{{Action: "refresh"}}})
	if m.Error != "" || m.Result == nil || !m.Result.OK || !m.Result.Changed || m.Result.Message != "done" {
		t.Fatalf("invoke: %+v", m)
	}
	m = Handle(h, &Request{Action: ActionInvoke, Actions: []ActionCall{{Action: "no"}}})
	if m.Error != "" || m.Result == nil || m.Result.OK || m.Result.Message != "not now" {
		t.Fatalf("failed action: %+v", m)
	}
	m = Handle(h, &Request{Action: ActionInvoke, Actions: []ActionCall{{Action: "fail"}}})
	if m.Error != "boom" || m.Result != nil {
		t.Fatalf("plugin error: %+v", m)
	}
	if m = Handle(h, &Request{Action: ActionInvoke}); m.Error == "" {
		t.Fatal("an action request must carry exactly one action")
	}
	if m = Handle(h, &Request{Action: ActionList}); m.Error != "" || len(m.Actions) != 0 {
		t.Fatalf("empty actions request: %+v", m)
	}
	// A plugin without an Actioner offers no actions.
	if m = Handle(&testHandler{}, &Request{Action: ActionList}); m.Error == "" {
		t.Fatal("a plugin without actions must be rejected")
	}
}

func TestActionStateCount(t *testing.T) {
	m := Handle(&actionFewerStates{}, &Request{Action: ActionList, Actions: []ActionCall{{Action: "a"}, {Action: "b"}}})
	if m.Error == "" || len(m.Actions) != 0 {
		t.Fatalf("state count mismatch: %+v", m)
	}
}

type actionFewerStates struct{ actionHandler }

func (actionFewerStates) Actions(*Request) ([]ActionState, error) {
	return []ActionState{{Title: "a"}}, nil
}

func TestVersion(t *testing.T) {
	if Version != 5 {
		t.Fatalf("protocol version = %d", Version)
	}
}

type stateHandler struct {
	testHandler
	state json.RawMessage
	blobs map[string][]byte
}

func (h *stateHandler) State() json.RawMessage   { return h.state }
func (h *stateHandler) Blobs() map[string][]byte { return h.blobs }

// TestStateTravelsWithEveryAnswer checks the protocol fields of ADR 0081: the
// state is its own field, a plugin answers with what should be stored, and a
// plugin without state adds nothing.
func TestStateTravelsWithEveryAnswer(t *testing.T) {
	h := &stateHandler{state: json.RawMessage(`{"n":1}`), blobs: map[string][]byte{"render.png": []byte("png")}}
	m := Handle(h, &Request{ID: "1", Action: ActionDescribe})
	if m.State == nil || string(*m.State) != `{"n":1}` {
		t.Fatalf("state: %v", m.State)
	}
	if string(m.Blobs["render.png"]) != "png" {
		t.Fatalf("blobs: %v", m.Blobs)
	}
	if m := Handle(&testHandler{}, &Request{ID: "2", Action: ActionDescribe}); m.State != nil || m.Blobs != nil {
		t.Fatalf("a plugin without state must not add fields: %+v", m)
	}
	// Paperboy sends the stored state with every request, so a plugin can read
	// it without another round trip.
	var req Request
	if err := json.Unmarshal([]byte(`{"id":"3","action":"render","state":{"vin":"x"},"blobs":{"render.png":"cG5n"}}`), &req); err != nil {
		t.Fatal(err)
	}
	if string(req.State) != `{"vin":"x"}` || string(req.Blobs["render.png"]) != "png" {
		t.Fatalf("request state: %s %v", req.State, req.Blobs)
	}
}
