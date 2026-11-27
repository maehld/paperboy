package main

import (
	"bytes"
	"errors"
	"image/png"
	"strings"
	"testing"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

// drawnSnapshot is a small home network with two devices and a firmware
// update, so the status line has a fact to show.
func drawnSnapshot() snapshot {
	return snapshot{
		loaded:  true,
		clients: 74,
		devices: []device{
			online("Keller-AP"),
			{Name: "OG-AP", State: "ONLINE", FirmwareUpdatable: true},
		},
		wan: healthSubsystem{Status: "ok", ISPName: "Deutsche Telekom AG"},
		www: healthSubsystem{Status: "ok", Latency: int64Ptr(11)},
	}
}

func renderedPlugin() *unifi {
	p := newPlugin()
	p.snap = drawnSnapshot()
	p.wanEnabled = true
	return p
}

func statusRequest(lang string) *plugin.Request {
	return &plugin.Request{Version: plugin.Version, Language: lang}
}

func statusWidget(width, height int) plugin.WidgetRequest {
	return plugin.WidgetRequest{
		Widget: "status", Width: width, Height: height,
		Style: plugin.Style{Padding: 8, Rule: 1, TextPx: 27.78},
	}
}

// renderStatus draws the status widget and returns the encoded PNG.
func renderStatus(t *testing.T, p *unifi, req *plugin.Request, w plugin.WidgetRequest, levels int) []byte {
	t.Helper()
	img := render.NewCanvas(w.Width, w.Height, levels)
	if err := p.Render(req, w, img); err != nil {
		t.Fatalf("render %dx%d: %v", w.Width, w.Height, err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func TestRenderKeepsRequestedSize(t *testing.T) {
	p := renderedPlugin()
	req := statusRequest("de")
	for _, c := range []struct {
		name          string
		width, height int
	}{
		{"2x2", 260, 116},
		{"3x2", 390, 116},
		{"6x1", 784, 58},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := statusWidget(c.width, c.height)
			data := renderStatus(t, p, req, w, 4)
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got := img.Bounds().Dx(); got != c.width {
				t.Errorf("width = %d, want %d", got, c.width)
			}
			if got := img.Bounds().Dy(); got != c.height {
				t.Errorf("height = %d, want %d", got, c.height)
			}
		})
	}
}

func TestRenderLevelsDiffer(t *testing.T) {
	p := renderedPlugin()
	req := statusRequest("de")
	w := statusWidget(390, 116)
	one := renderStatus(t, p, req, w, 2)
	four := renderStatus(t, p, req, w, 4)
	if bytes.Equal(one, four) {
		t.Error("one-bit and four-level images are identical")
	}
}

func TestRenderDefaultFocusClients(t *testing.T) {
	p := renderedPlugin()
	req := statusRequest("de")
	def := statusWidget(390, 116)
	clients := def
	clients.Args = map[string]any{"focus": "clients"}
	if !bytes.Equal(renderStatus(t, p, req, def, 4), renderStatus(t, p, req, clients, 4)) {
		t.Error("the default focus is not clients")
	}
}

func TestRenderFocusChangesValue(t *testing.T) {
	p := renderedPlugin()
	req := statusRequest("de")
	devices := statusWidget(390, 116)
	devices.Args = map[string]any{"focus": "devices"}
	clients := devices
	clients.Args = map[string]any{"focus": "clients"}
	if bytes.Equal(renderStatus(t, p, req, devices, 4), renderStatus(t, p, req, clients, 4)) {
		t.Error("focus devices and focus clients draw the same image")
	}
}

func TestRenderMinimumCell(t *testing.T) {
	p := renderedPlugin()
	w := statusWidget(120, 60)
	w.Style.Padding = 5
	img := render.NewCanvas(w.Width, w.Height, 4)
	if err := p.Render(statusRequest("de"), w, img); err != nil {
		t.Errorf("120x60: %v, want a drawing", err)
	}
}

func TestRenderDetailDropsStatusLine(t *testing.T) {
	p := renderedPlugin()
	req := statusRequest("de")
	with := statusWidget(390, 116)
	without := with
	without.Args = map[string]any{"detail": false}
	if bytes.Equal(renderStatus(t, p, req, with, 4), renderStatus(t, p, req, without, 4)) {
		t.Error("detail false draws the status line")
	}
}

func TestRenderLanguage(t *testing.T) {
	p := renderedPlugin()
	w := statusWidget(390, 116)
	de := renderStatus(t, p, statusRequest("de"), w, 4)
	en := renderStatus(t, p, statusRequest("en"), w, 4)
	if bytes.Equal(de, en) {
		t.Error("German and English draw the same image")
	}
}

func TestRenderWithoutData(t *testing.T) {
	p := newPlugin()
	w := statusWidget(390, 116)
	renderStatus(t, p, statusRequest("de"), w, 4)
}

func TestRenderTooSmall(t *testing.T) {
	p := renderedPlugin()
	w := plugin.WidgetRequest{Widget: "status", Width: 60, Height: 40}
	img := render.NewCanvas(w.Width, w.Height, 4)
	if err := p.Render(statusRequest("de"), w, img); !errors.Is(err, plugin.ErrTooSmall) {
		t.Errorf("60x40: err = %v, want ErrTooSmall", err)
	}
}

func TestRenderUnknownWidget(t *testing.T) {
	p := renderedPlugin()
	w := statusWidget(390, 116)
	w.Widget = "nope"
	img := render.NewCanvas(w.Width, w.Height, 4)
	if err := p.Render(statusRequest("de"), w, img); !errors.Is(err, plugin.ErrUnknownWidget) {
		t.Errorf("unknown widget: err = %v, want ErrUnknownWidget", err)
	}
}

func wanWidget(width, height int) plugin.WidgetRequest {
	return plugin.WidgetRequest{
		Widget: "wan", Width: width, Height: height,
		Style: plugin.Style{Padding: 8, Rule: 1, TextPx: 27.78},
	}
}

func TestRenderWANKeepsRequestedSize(t *testing.T) {
	for _, size := range [][2]int{{266, 120}, {400, 120}, {800, 60}} {
		p := renderedPlugin()
		w := wanWidget(size[0], size[1])
		img := render.NewCanvas(w.Width, w.Height, 4)
		if err := p.Render(statusRequest("de"), w, img); err != nil {
			t.Errorf("%dx%d: %v", size[0], size[1], err)
		}
	}
}

func TestRenderWANDownDiffers(t *testing.T) {
	p := renderedPlugin()
	w := wanWidget(400, 120)
	ok := renderStatus(t, p, statusRequest("de"), w, 4)

	p.snap.www.Status = "error"
	p.snap.www.Latency = nil
	down := renderStatus(t, p, statusRequest("de"), w, 4)
	if bytes.Equal(ok, down) {
		t.Error("a down connection draws the healthy image")
	}
}

func TestRenderWANTrafficAndIP(t *testing.T) {
	p := renderedPlugin()
	p.snap.wan.TxBytesR = 4961
	p.snap.wan.RxBytesR = 1520000
	p.snap.wan.WANIP = "203.0.113.9"
	w := wanWidget(400, 120)
	plain := renderStatus(t, p, statusRequest("de"), w, 4)

	w.Args = map[string]any{"traffic": true}
	traffic := renderStatus(t, p, statusRequest("de"), w, 4)
	if bytes.Equal(plain, traffic) {
		t.Error("traffic true draws the same image")
	}

	w.Args = map[string]any{"ip": true}
	ip := renderStatus(t, p, statusRequest("de"), w, 4)
	if bytes.Equal(plain, ip) {
		t.Error("ip true draws the same image")
	}
}

func TestRenderWANNeedsTheConfig(t *testing.T) {
	p := renderedPlugin()
	p.wanEnabled = false
	w := wanWidget(400, 120)
	img := render.NewCanvas(w.Width, w.Height, 4)
	err := p.Render(statusRequest("de"), w, img)
	if err == nil || !strings.Contains(err.Error(), "wan: true") {
		t.Errorf("err = %v, want a hint about wan: true", err)
	}
}

func TestRenderWANWithoutData(t *testing.T) {
	p := newPlugin()
	p.wanEnabled = true
	w := wanWidget(400, 120)
	renderStatus(t, p, statusRequest("de"), w, 4)
}

func TestRenderWANTooSmall(t *testing.T) {
	p := renderedPlugin()
	w := wanWidget(60, 40)
	img := render.NewCanvas(w.Width, w.Height, 4)
	if err := p.Render(statusRequest("de"), w, img); !errors.Is(err, plugin.ErrTooSmall) {
		t.Errorf("60x40: err = %v, want ErrTooSmall", err)
	}
}
