package main

import (
	"errors"
	"image"
	"testing"
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

func TestRenderWidgets(t *testing.T) {
	srv, _ := newPrinterServer(t)
	p := reloaded(t, srv.URL)
	p.snap = p.cur
	req := &plugin.Request{Version: plugin.Version, Language: "de", Now: time.Now()}
	cases := []struct {
		widget string
		width  int
		height int
	}{
		{"printer", 400, 120},
		{"printer", 800, 180},
		{"progress", 266, 120},
		{"progress", 400, 180},
		{"temperatures", 266, 120},
		{"temperatures", 800, 120},
	}
	for _, c := range cases {
		w := plugin.WidgetRequest{Widget: c.widget, Width: c.width, Height: c.height, Style: plugin.Style{Padding: 6, Rule: 1, TextPx: 16}}
		img := render.NewCanvas(w.Width, w.Height, 4)
		if err := p.Render(req, w, img); err != nil {
			t.Errorf("%s %dx%d: %v", c.widget, c.width, c.height, err)
			continue
		}
		if !hasInk(img) {
			t.Errorf("%s %dx%d: empty image", c.widget, c.width, c.height)
		}
	}
}

func TestRenderIdle(t *testing.T) {
	srv, f := newPrinterServer(t)
	f.mu.Lock()
	f.state, f.noJob = "IDLE", true
	f.mu.Unlock()
	p := reloaded(t, srv.URL)
	p.snap = p.cur
	req := &plugin.Request{Version: plugin.Version, Language: "de", Now: time.Now()}
	for _, name := range []string{"printer", "progress", "temperatures"} {
		w := plugin.WidgetRequest{Widget: name, Width: 400, Height: 160, Style: plugin.Style{Padding: 6, Rule: 1, TextPx: 16}}
		img := render.NewCanvas(w.Width, w.Height, 4)
		if err := p.Render(req, w, img); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestRenderMonochrome(t *testing.T) {
	srv, _ := newPrinterServer(t)
	p := reloaded(t, srv.URL)
	p.snap = p.cur
	req := &plugin.Request{Version: plugin.Version, Language: "en", Now: time.Now()}
	for _, name := range []string{"printer", "progress", "temperatures"} {
		w := plugin.WidgetRequest{Widget: name, Width: 300, Height: 120, Style: plugin.Style{Padding: 4, Rule: 1, TextPx: 16}}
		if err := p.Render(req, w, render.NewCanvas(w.Width, w.Height, 1)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestRenderTooSmall(t *testing.T) {
	srv, _ := newPrinterServer(t)
	p := reloaded(t, srv.URL)
	p.snap = p.cur
	req := &plugin.Request{Version: plugin.Version, Now: time.Now()}
	for _, name := range []string{"printer", "progress", "temperatures"} {
		w := plugin.WidgetRequest{Widget: name, Width: 60, Height: 40, Style: plugin.Style{TextPx: 16}}
		if err := p.Render(req, w, render.NewCanvas(w.Width, w.Height, 4)); !errors.Is(err, plugin.ErrTooSmall) {
			t.Errorf("%s: err = %v, want ErrTooSmall", name, err)
		}
	}
	w := plugin.WidgetRequest{Widget: "unknown", Width: 400, Height: 160, Style: plugin.Style{TextPx: 16}}
	if err := p.Render(req, w, render.NewCanvas(w.Width, w.Height, 4)); !errors.Is(err, plugin.ErrUnknownWidget) {
		t.Errorf("err = %v, want ErrUnknownWidget", err)
	}
}

func TestRenderOffline(t *testing.T) {
	p := configured(t, "http://printer.invalid")
	p.snap = snapshot{loaded: true, failures: 2}
	req := &plugin.Request{Version: plugin.Version, Language: "de", Now: time.Now()}
	for _, name := range []string{"printer", "progress", "temperatures"} {
		w := plugin.WidgetRequest{Widget: name, Width: 400, Height: 160, Style: plugin.Style{Padding: 6, Rule: 1, TextPx: 16}}
		img := render.NewCanvas(w.Width, w.Height, 4)
		if err := p.Render(req, w, img); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !hasInk(img) {
			t.Errorf("%s: empty image", name)
		}
	}
}

func TestRenderWithoutData(t *testing.T) {
	p := newPlugin()
	for _, name := range []string{"printer", "progress", "temperatures"} {
		w := plugin.WidgetRequest{Widget: name, Width: 400, Height: 160, Style: plugin.Style{TextPx: 16}}
		img := render.NewCanvas(w.Width, w.Height, 4)
		if err := p.Render(&plugin.Request{}, w, img); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !hasInk(img) {
			t.Errorf("%s: empty image", name)
		}
	}
}

func hasInk(img *image.Paletted) bool {
	white := uint8(len(img.Palette) - 1)
	for _, px := range img.Pix {
		if px != white {
			return true
		}
	}
	return false
}
