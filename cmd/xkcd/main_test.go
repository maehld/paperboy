package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

// strip draws a recorded-style comic stand-in: white background, a black
// frame, one-pixel diagonal lines and a few dark blocks.
func strip(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	for x := 0; x < w; x++ {
		img.Set(x, 0, color.Black)
		img.Set(x, h-1, color.Black)
	}
	for y := 0; y < h; y++ {
		img.Set(0, y, color.Black)
		img.Set(w-1, y, color.Black)
	}
	for i := 0; i < min(w, h); i++ {
		img.Set(i, i, color.Black)
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

// server serves the fixtures in testdata, with the image URLs pointing back to
// itself; hits counts the info requests.
func server(t *testing.T, latest string) (*httptest.Server, *atomic.Int32) {
	var hits atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture := func(name string) {
			hits.Add(1)
			b, err := os.ReadFile("testdata/" + name)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Write([]byte(strings.ReplaceAll(string(b), "IMG", srv.URL+"/img")))
		}
		switch r.URL.Path {
		case "/info.0.json":
			fixture(latest)
		case "/7/info.0.json":
			fixture("n7.json")
		case "/8/info.0.json":
			fixture("n8.json")
		case "/img/comic.png":
			w.Write(strip(640, 360))
		case "/img/wide.png":
			w.Write(strip(3000, 40))
		case "/img/tall.png":
			w.Write(strip(60, 4000))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func start(t *testing.T, cfg plugin.Values, notify func(...string)) (*xkcd, *plugin.Request) {
	x := &xkcd{}
	req := &plugin.Request{Language: "en", Timezone: "UTC", Now: time.Unix(1794002400, 0), Levels: 4,
		Page: plugin.PageInfo{Width: 800, Height: 480, PPI: 125}}
	if err := x.Configure(cfg, req); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	x.Start(ctx, notify)
	if err := x.Prepare(req); err != nil {
		t.Fatal(err)
	}
	return x, req
}

func widget(w, h int, args map[string]any) plugin.WidgetRequest {
	return plugin.WidgetRequest{Widget: "comic", Width: w, Height: h, Args: args,
		Style: plugin.Style{Padding: 8, Rule: 1, TextPx: 28}}
}

func TestConfigure(t *testing.T) {
	for cfg, ok := range map[string]bool{"latest": true, "random": true, "7": true, "404": false, "0": false, "x": false} {
		err := (&xkcd{}).Configure(plugin.Values{"comic": cfg}, &plugin.Request{})
		if (err == nil) != ok {
			t.Errorf("comic %q: %v", cfg, err)
		}
	}
	if err := (&xkcd{}).Configure(plugin.Values{"comic": 12.0}, &plugin.Request{}); err != nil {
		t.Errorf("numeric comic: %v", err)
	}
	if err := (&xkcd{}).Configure(plugin.Values{"refresh": "10s"}, &plugin.Request{}); err == nil {
		t.Error("short refresh accepted")
	}
	if err := (&xkcd{}).Configure(plugin.Values{"typo": 1}, &plugin.Request{}); err == nil {
		t.Error("unknown key accepted")
	}
}

func TestLatestAndNumber(t *testing.T) {
	srv, _ := server(t, "latest.json")
	x, _ := start(t, plugin.Values{"base": srv.URL}, func(...string) {})
	if x.cur.Num != 3200 || x.cur.Title != "Test Comic" {
		t.Fatalf("%+v", x.cur.info)
	}
	x, _ = start(t, plugin.Values{"base": srv.URL, "comic": 7}, func(...string) {})
	if x.cur.Num != 7 || x.cur.img.Bounds().Dx() != 3000 {
		t.Fatalf("%+v", x.cur.info)
	}
}

func TestRandom(t *testing.T) {
	srv, _ := server(t, "latest.json")
	x := &xkcd{pick: func(max int) int { return 8 }}
	req := &plugin.Request{}
	if err := x.Configure(plugin.Values{"base": srv.URL, "comic": "random", "rotate": "1h"}, req); err != nil {
		t.Fatal(err)
	}
	c, err := x.load(context.Background())
	if err != nil || c.Num != 8 {
		t.Fatalf("%+v %v", c, err)
	}
	// 404 does not exist: the next one is used (the fixture server has none, so it fails).
	x.pick = func(max int) int { return 404 }
	if _, err := x.load(context.Background()); err == nil || !strings.Contains(err.Error(), "/405/") {
		t.Fatalf("expected a request for 405, got %v", err)
	}
}

func TestChangedEvent(t *testing.T) {
	var current atomic.Value
	current.Store("latest.json")
	inner, _ := server(t, "latest.json")
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/info.0.json" && current.Load() == "n8.json" {
			path = "/8/info.0.json"
		}
		resp, err := http.Get(inner.URL + path)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		var buf bytes.Buffer
		buf.ReadFrom(resp.Body)
		w.Write(buf.Bytes())
	}))
	t.Cleanup(proxy.Close)

	changed := make(chan struct{}, 4)
	x := &xkcd{}
	req := &plugin.Request{}
	// The minimum refresh is a minute; set the interval directly for the test.
	if err := x.Configure(plugin.Values{"base": proxy.URL}, req); err != nil {
		t.Fatal(err)
	}
	x.refresh = 50 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	x.Start(ctx, func(...string) { changed <- struct{}{} })
	if err := x.Prepare(req); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changed:
		t.Fatal("changed reported without a new comic")
	case <-time.After(200 * time.Millisecond):
	}
	current.Store("n8.json")
	select {
	case <-changed:
	case <-time.After(3 * time.Second):
		t.Fatal("no changed event for the new comic")
	}
}

func TestFitImageExtremes(t *testing.T) {
	for _, c := range []struct{ sw, sh, w, h int }{
		{3000, 40, 784, 464}, {60, 4000, 784, 464}, {640, 360, 784, 464},
		{5000, 10, 100, 100}, {10, 5000, 100, 100}, {1, 1, 50, 50}, {800, 800, 30, 400},
	} {
		src, _ := png.Decode(bytes.NewReader(strip(c.sw, c.sh)))
		got := fitImage(src, c.w, c.h)
		gw, gh := got.Bounds().Dx(), got.Bounds().Dy()
		if gw < 1 || gh < 1 || gw > c.w || gh > c.h {
			t.Errorf("%+v: result %dx%d outside the box", c, gw, gh)
		}
		if gw != c.w && gh != c.h {
			t.Errorf("%+v: result %dx%d does not use the box", c, gw, gh)
		}
		ratio := float64(c.sw) / float64(c.sh)
		if gw > 4 && gh > 4 {
			if r := float64(gw) / float64(gh); r < ratio*0.9 || r > ratio*1.1 {
				t.Errorf("%+v: aspect ratio %.2f, want %.2f", c, r, ratio)
			}
		}
	}
}

func TestFitImageTransparency(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 20, 20))
	got := fitImage(src, 10, 10)
	if got.GrayAt(5, 5).Y != 255 {
		t.Fatalf("transparent pixel became %d, want white", got.GrayAt(5, 5).Y)
	}
}

func TestRender(t *testing.T) {
	srv, _ := server(t, "latest.json")
	for _, n := range []int{0, 7, 8} {
		cfg := plugin.Values{"base": srv.URL}
		if n > 0 {
			cfg["comic"] = n
		}
		x, req := start(t, cfg, func(...string) {})
		for _, levels := range []int{2, 4} {
			for _, args := range []map[string]any{nil, {"title": true}, {"alt": true}, {"title": true, "alt": true}} {
				for _, size := range [][2]int{{800, 480}, {400, 240}, {120, 60}, {20, 20}} {
					req.Levels = levels
					img := render.NewCanvas(size[0], size[1], levels)
					err := x.Render(req, widget(size[0], size[1], args), img)
					if err != nil && !errors.Is(err, plugin.ErrTooSmall) {
						t.Fatalf("comic %d %v: %v", n, args, err)
					}
				}
			}
		}
		// A cell the padding already eats up is reported, not drawn blank.
		if err := x.Render(req, widget(10, 10, nil), render.NewCanvas(10, 10, 4)); !errors.Is(err, plugin.ErrTooSmall) {
			t.Fatalf("comic %d in a 10x10 cell: error = %v, want ErrTooSmall", n, err)
		}
		img := render.NewCanvas(800, 480, 4)
		x.Render(req, widget(800, 480, nil), img)
		dark := 0
		for _, p := range img.Pix {
			if p != 3 {
				dark++
			}
		}
		if dark == 0 {
			t.Errorf("comic %d: nothing drawn", n)
		}
	}
}

func TestRenderViaProtocol(t *testing.T) {
	srv, _ := server(t, "latest.json")
	x, req := start(t, plugin.Values{"base": srv.URL}, func(...string) {})
	req.Action, req.Levels = plugin.ActionRender, 2
	req.Widgets = []plugin.WidgetRequest{widget(500, 300, map[string]any{"title": true, "alt": true})}
	m := plugin.Handle(x, req)
	if m.Error != "" || len(m.Widgets) != 1 || m.Widgets[0].Error != "" || len(m.Widgets[0].PNG) == 0 {
		t.Fatalf("%+v", m)
	}
}

func TestText(t *testing.T) {
	srv, _ := server(t, "latest.json")
	x, req := start(t, plugin.Values{"base": srv.URL}, func(...string) {})
	lines, err := x.Text(req, plugin.WidgetRequest{Widget: "comic", Width: 20, Height: 4})
	if err != nil || len(lines) != 4 {
		t.Fatalf("%q %v", lines, err)
	}
	if lines[0] != "#3200 Test Comic    " && !strings.HasPrefix(lines[0], "#3200 Test Comic") {
		t.Errorf("first line %q", lines[0])
	}
	if !strings.HasSuffix(lines[3], "…") {
		t.Errorf("cut alt text lacks an ellipsis: %q", lines[3])
	}
	for _, l := range lines {
		if len([]rune(l)) > 20 {
			t.Errorf("line %q wider than 20", l)
		}
	}
	long, _ := x.Text(req, plugin.WidgetRequest{Widget: "comic", Width: 20, Height: 30})
	if strings.HasSuffix(long[len(long)-1], "…") {
		t.Errorf("complete alt text ends with an ellipsis: %q", long)
	}
	if _, err := x.Text(req, plugin.WidgetRequest{Widget: "nope", Width: 20, Height: 4}); err == nil {
		t.Error("unknown widget accepted")
	}
}

func TestWrap(t *testing.T) {
	got := wrap("aa bb cc dddddddddd", 5)
	want := []string{"aa bb", "cc", "ddddd", "ddddd"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%q, want %q", got, want)
	}
}
