// Package plugin is the protocol between Paperboy and fragment plugins
// (spike). A plugin is a long-running program: Paperboy starts it once and
// writes requests to its standard input, one JSON object per line. The plugin
// answers each request with one line on standard output that carries the same
// id. Between answers it may send events, such as "changed" when its data
// moved on. It keeps running until standard input closes. Two actions exist:
//
//   - "describe": the plugin lists the images it can draw, with the sizes it
//     supports and its arguments, so that an editor can offer them.
//   - "render": the plugin draws the requested images in exactly the given
//     pixel size and palette and returns them as PNG.
//
// Plugins written in Go use Serve and draw with pkg/render, so fragments share
// the fonts and gray levels of the page around them.
package plugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"sync"
	"time"

	"github.com/maehld/paperboy/pkg/render"
)

// Version is the protocol version a request carries.
const Version = 1

// Actions of a request.
const (
	ActionDescribe = "describe"
	ActionRender   = "render"
)

// Request is what Paperboy writes to standard input.
type Request struct {
	ID       string         `json:"id"`
	Version  int            `json:"version"`
	Action   string         `json:"action"`
	Language string         `json:"language,omitempty"` // "de" or "en"
	Timezone string         `json:"timezone,omitempty"` // IANA name
	Now      time.Time      `json:"now"`
	Levels   int            `json:"levels,omitempty"` // gray levels: 2 or 4
	Images   []ImageRequest `json:"images,omitempty"`
}

// ImageRequest asks for one image in an exact size.
type ImageRequest struct {
	Image  string         `json:"image"`
	Width  int            `json:"width"`
	Height int            `json:"height"`
	Args   map[string]any `json:"args,omitempty"`
}

// Message is one line a plugin prints: the answer to request ID, or an event
// when ID is empty.
type Message struct {
	ID          string       `json:"id,omitempty"`
	Error       string       `json:"error,omitempty"` // the request failed as a whole
	Render      *Response    `json:"render,omitempty"`
	Description *Description `json:"description,omitempty"`
	Event       string       `json:"event,omitempty"`  // "changed"
	Images      []string     `json:"images,omitempty"` // changed images; empty = all
}

// EventChanged tells Paperboy that the plugin's images are out of date.
const EventChanged = "changed"

// Response is the result of the render action. Images has one
// entry per requested image, in the same order.
type Response struct {
	Images []ImageResult `json:"images"`
	// MaxAge is how long Paperboy may reuse the images, in seconds; 0 means
	// the server's default.
	MaxAge int `json:"max_age,omitempty"`
}

// ImageResult is one rendered image or the reason it is missing.
type ImageResult struct {
	PNG   []byte `json:"png,omitempty"` // base64 in JSON
	Error string `json:"error,omitempty"`
}

// Description is what a plugin prints for the describe action.
type Description struct {
	Title  string      `json:"title"`
	Images []ImageInfo `json:"images"`
}

// ImageInfo describes one image a plugin can draw.
type ImageInfo struct {
	Image string `json:"image"`
	Title string `json:"title"`
	// Sizes are the grid spans the image is designed for, as columns x rows
	// of a page grid. They are hints for an editor; Paperboy only enforces
	// the minimum pixel size.
	Sizes     []Span         `json:"sizes,omitempty"`
	MinWidth  int            `json:"min_width,omitempty"`
	MinHeight int            `json:"min_height,omitempty"`
	Args      map[string]Arg `json:"args,omitempty"`
}

// Span is a size in grid cells.
type Span struct {
	Columns int `json:"columns"`
	Rows    int `json:"rows"`
}

// Arg describes one argument of an image.
type Arg struct {
	Type        string `json:"type"` // string, int, bool
	Default     any    `json:"default,omitempty"`
	Description string `json:"description,omitempty"`
}

// Handler is implemented by a plugin.
type Handler interface {
	Describe() Description
	// Prepare runs once per render request before the images.
	Prepare(req *Request) error
	// Render draws one image on a canvas of the requested size and palette.
	Render(req *Request, img ImageRequest, canvas *image.Paletted) error
}

// Starter is implemented by plugins with background work, such as refreshing
// their data. Start runs once before the first request; notify tells Paperboy
// that images changed (none = all), so it renders them again.
type Starter interface {
	Start(ctx context.Context, notify func(images ...string))
}

// Ager is implemented by plugins that set how long Paperboy may reuse their
// images.
type Ager interface {
	MaxAge(req *Request) time.Duration
}

// Serve answers requests from standard input until it closes. Requests are
// handled one at a time; events from Start may arrive in between.
func Serve(h Handler) {
	if err := ServeIO(context.Background(), h, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// ServeIO is Serve with explicit streams.
func ServeIO(ctx context.Context, h Handler, in io.Reader, out io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	enc := json.NewEncoder(out)
	send := func(m Message) error {
		mu.Lock()
		defer mu.Unlock()
		return enc.Encode(m)
	}
	if s, ok := h.(Starter); ok {
		s.Start(ctx, func(images ...string) {
			send(Message{Event: EventChanged, Images: images})
		})
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			if err := send(Message{Error: "invalid request: " + err.Error()}); err != nil {
				return err
			}
			continue
		}
		if err := send(handle(h, &req)); err != nil {
			return err
		}
	}
	return sc.Err()
}

func handle(h Handler, req *Request) (m Message) {
	m.ID = req.ID
	defer func() {
		if r := recover(); r != nil {
			m = Message{ID: req.ID, Error: fmt.Sprintf("panic: %v", r)}
		}
	}()
	switch req.Action {
	case ActionDescribe:
		d := h.Describe()
		m.Description = &d
	case ActionRender:
		r, err := renderAll(h, req)
		if err != nil {
			m.Error = err.Error()
		} else {
			m.Render = r
		}
	default:
		m.Error = fmt.Sprintf("unknown action %q", req.Action)
	}
	return m
}

func renderAll(h Handler, req *Request) (*Response, error) {
	if req.Levels != 2 && req.Levels != 4 {
		return nil, fmt.Errorf("levels %d: expected 2 or 4", req.Levels)
	}
	if err := h.Prepare(req); err != nil {
		return nil, err
	}
	resp := &Response{}
	if a, ok := h.(Ager); ok {
		resp.MaxAge = int(a.MaxAge(req) / time.Second)
	}
	for _, ir := range req.Images {
		resp.Images = append(resp.Images, renderOne(h, req, ir))
	}
	return resp, nil
}

func renderOne(h Handler, req *Request, ir ImageRequest) (res ImageResult) {
	defer func() {
		if r := recover(); r != nil {
			res = ImageResult{Error: fmt.Sprintf("panic: %v", r)}
		}
	}()
	if ir.Width < 1 || ir.Height < 1 || ir.Width > 4096 || ir.Height > 4096 {
		return ImageResult{Error: "invalid size"}
	}
	canvas := render.NewCanvas(ir.Width, ir.Height, req.Levels)
	if err := h.Render(req, ir, canvas); err != nil {
		return ImageResult{Error: err.Error()}
	}
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, canvas); err != nil {
		return ImageResult{Error: err.Error()}
	}
	return ImageResult{PNG: buf.Bytes()}
}

// Location returns the request's timezone, or the local one.
func (r *Request) Location() *time.Location {
	if r.Timezone != "" {
		if loc, err := time.LoadLocation(r.Timezone); err == nil {
			return loc
		}
	}
	return time.Local
}

// IntArg reads an integer argument with a default.
func (ir ImageRequest) IntArg(name string, def int) int {
	switch v := ir.Args[name].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return def
}

// BoolArg reads a boolean argument with a default.
func (ir ImageRequest) BoolArg(name string, def bool) bool {
	if v, ok := ir.Args[name].(bool); ok {
		return v
	}
	return def
}

// ErrUnknownImage is returned for an image name the plugin does not know.
var ErrUnknownImage = errors.New("unknown image")
