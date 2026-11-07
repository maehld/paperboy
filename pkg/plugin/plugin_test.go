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
	if m := next(); m.ID != "1" || m.Error == "" {
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
