// Command xkcd is a Paperboy plugin (ADR 0049) that shows an xkcd comic
// (https://xkcd.com, CC BY-NC 2.5). It reads the comic's JSON API, downloads
// the image from its img URL in the background and offers the widget "comic",
// optionally with title and alt text, and a text variant with the comic's
// number, title and wrapped alt text.
//
//	plugins:
//	  xkcd:
//	    command: [xkcd]
//	    config: { comic: latest }   # latest, random or a number
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

const (
	defaultBase    = "https://xkcd.com"
	requestTimeout = 30 * time.Second
	maxInfoBytes   = 1 << 20
	maxImageBytes  = 16 << 20
	// missingComic is the number xkcd does not have (an HTTP 404 joke).
	missingComic = 404
	retryAfter   = 5 * time.Minute
)

func main() {
	plugin.Serve(&xkcd{})
}

// info is the part of the xkcd JSON API the plugin uses.
type info struct {
	Num   int    `json:"num"`
	Title string `json:"title"`
	Alt   string `json:"alt"`
	Img   string `json:"img"`
}

// comic is a loaded comic with its decoded image.
type comic struct {
	info
	img image.Image
}

func get(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%s: response larger than %d bytes", url, limit)
	}
	return body, nil
}

// fetchInfo reads the info of comic n; n 0 is the latest comic.
func fetchInfo(ctx context.Context, client *http.Client, base string, n int) (info, error) {
	u := strings.TrimRight(base, "/") + "/info.0.json"
	if n > 0 {
		u = fmt.Sprintf("%s/%d/info.0.json", strings.TrimRight(base, "/"), n)
	}
	body, err := get(ctx, client, u, maxInfoBytes)
	if err != nil {
		return info{}, err
	}
	var in info
	if err := json.Unmarshal(body, &in); err != nil {
		return info{}, fmt.Errorf("xkcd: invalid response: %w", err)
	}
	if in.Num < 1 || in.Img == "" {
		return info{}, errors.New("xkcd: incomplete response")
	}
	return in, nil
}

// fetchComic loads the info and the image of comic n (0 = latest).
func fetchComic(ctx context.Context, client *http.Client, base string, n int) (*comic, error) {
	in, err := fetchInfo(ctx, client, base, n)
	if err != nil {
		return nil, err
	}
	body, err := get(ctx, client, in.Img, maxImageBytes)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("xkcd: comic %d: %w", in.Num, err)
	}
	return &comic{info: in, img: img}, nil
}

var configKeys = map[string]plugin.Arg{
	"comic":   {Type: "string", Default: "latest", Description: "latest, random or a comic number"},
	"refresh": {Type: "duration", Default: "1h", Description: "how often the latest comic is checked"},
	"rotate":  {Type: "duration", Default: "24h", Description: "how often a random comic is replaced"},
	"base":    {Type: "string", Default: defaultBase, Description: "xkcd base URL"},
}

// xkcd is the plugin.
type xkcd struct {
	mode    string // "latest", "random" or "number"
	number  int
	refresh time.Duration
	base    string
	pick    func(max int) int // random comic in 1..max, replaceable in tests

	mu      sync.Mutex
	data    *comic
	loadErr error
	ready   chan struct{}

	cur *comic
	req *plugin.Request
}

func (x *xkcd) Describe() plugin.Description {
	both := []string{plugin.KindImage, plugin.KindText}
	return plugin.Description{
		Title:  "xkcd comic",
		Config: configKeys,
		Widgets: []plugin.WidgetInfo{
			{Widget: "comic", Title: "Comic", Kinds: both, MinWidth: 200, MinHeight: 100,
				Sizes: []plugin.Span{{Columns: 6, Rows: 4}, {Columns: 6, Rows: 3}, {Columns: 6, Rows: 8}, {Columns: 3, Rows: 4}},
				Args: map[string]plugin.Arg{
					"title": {Type: "bool", Default: false, Description: "show number and title above the comic"},
					"alt":   {Type: "bool", Default: false, Description: "show the alt text below the comic"},
				}},
		},
	}
}

func (x *xkcd) Configure(cfg plugin.Values, req *plugin.Request) error {
	if err := cfg.Check(configKeys); err != nil {
		return err
	}
	x.base = cfg.String("base", defaultBase)
	switch c := cfg["comic"].(type) {
	case nil:
		x.mode = "latest"
	case string:
		switch c {
		case "latest", "random":
			x.mode = c
		default:
			n, err := strconv.Atoi(c)
			if err != nil {
				return fmt.Errorf("comic: expected latest, random or a number, got %q", c)
			}
			x.mode, x.number = "number", n
		}
	case float64:
		x.mode, x.number = "number", int(c)
	case int:
		x.mode, x.number = "number", c
	default:
		return errors.New("comic: expected latest, random or a number")
	}
	if x.mode == "number" && (x.number < 1 || x.number == missingComic) {
		return fmt.Errorf("comic: %d does not exist", x.number)
	}
	key, def := "refresh", time.Hour
	if x.mode == "random" {
		key, def = "rotate", 24*time.Hour
	}
	d, err := cfg.Duration(key, def)
	if err != nil {
		return err
	}
	if d < time.Minute {
		return fmt.Errorf("%s must be at least 1m", key)
	}
	x.refresh = d
	if x.pick == nil {
		x.pick = func(max int) int { return 1 + rand.IntN(max) }
	}
	return nil
}

// load fetches the comic the configuration asks for.
func (x *xkcd) load(ctx context.Context) (*comic, error) {
	switch x.mode {
	case "number":
		return fetchComic(ctx, http.DefaultClient, x.base, x.number)
	case "random":
		latest, err := fetchInfo(ctx, http.DefaultClient, x.base, 0)
		if err != nil {
			return nil, err
		}
		n := x.pick(latest.Num)
		if n == missingComic {
			n++
		}
		return fetchComic(ctx, http.DefaultClient, x.base, min(max(n, 1), latest.Num))
	}
	return fetchComic(ctx, http.DefaultClient, x.base, 0)
}

// Start loads the comic now and then every refresh (latest) or rotate
// (random) and reports a change when another comic is shown.
func (x *xkcd) Start(ctx context.Context, notify func(widgets ...string)) {
	x.ready = make(chan struct{})
	go func() {
		first := true
		for {
			c, err := x.load(ctx)
			wait := x.refresh
			x.mu.Lock()
			changed := false
			if err != nil {
				x.loadErr = err
				wait = min(wait, retryAfter)
				fmt.Fprintln(os.Stderr, "xkcd:", err)
			} else {
				changed = !first && (x.data == nil || x.data.Num != c.Num)
				x.data, x.loadErr = c, nil
			}
			x.mu.Unlock()
			if changed {
				notify()
			}
			if first {
				close(x.ready)
				first = false
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
	}()
}

func (x *xkcd) Prepare(req *plugin.Request) error {
	select {
	case <-x.ready:
	case <-time.After(20 * time.Second):
		return errors.New("comic is still loading")
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.data == nil {
		return x.loadErr
	}
	x.cur, x.req = x.data, req
	return nil
}

func (x *xkcd) MaxAge(*plugin.Request) time.Duration { return time.Hour }

// Text fills a character display: "#number title", then the wrapped alt text,
// cut with an ellipsis when it does not fit.
func (x *xkcd) Text(req *plugin.Request, w plugin.WidgetRequest) ([]string, error) {
	if w.Widget != "comic" {
		return nil, fmt.Errorf("%w %q for text displays", plugin.ErrUnknownWidget, w.Widget)
	}
	lines := append([]string{plugin.Align(fmt.Sprintf("#%d %s", x.cur.Num, x.cur.Title), w.Width, "left")},
		wrap(x.cur.Alt, w.Width)...)
	if len(lines) > w.Height {
		lines = lines[:w.Height]
		last := []rune(lines[w.Height-1])
		if w.Height > 1 {
			if len(last) >= w.Width {
				last = last[:max(w.Width-1, 0)]
			}
			lines[w.Height-1] = string(last) + "…"
		}
	}
	return lines, nil
}

// wrap breaks s at spaces into lines of at most width runes; longer words are
// split.
func wrap(s string, width int) []string {
	return wrapFunc(s, func(l string) bool { return len([]rune(l)) <= width }, width)
}

// wrapFunc breaks s into lines for which fits is true. A word that does not
// fit alone is split by runes, at most maxRunes per piece.
func wrapFunc(s string, fits func(string) bool, maxRunes int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		for !fits(word) && len([]rune(word)) > 1 {
			r := []rune(word)
			n := min(maxRunes, len(r)-1)
			for n > 1 && !fits(string(r[:n])) {
				n--
			}
			if line != "" {
				lines, line = append(lines, line), ""
			}
			lines = append(lines, string(r[:n]))
			word = string(r[n:])
		}
		switch {
		case line == "":
			line = word
		case fits(line + " " + word):
			line += " " + word
		default:
			lines, line = append(lines, line), word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
