// Package plugin is the protocol between Paperboy and its plugins (ADR 0049).
//
// A plugin is a long-running program that provides widgets: named pieces of a
// page such as "agenda" or "chart". Paperboy starts the plugin once and
// writes requests to its standard input, one JSON object per line; the plugin
// answers each request with one line on standard output that carries the same
// id. Between answers it may send events, such as "changed" when its data
// moved on. The plugin ends when standard input closes.
//
// Actions:
//
//   - "configure": the first request after the start, with the plugin's
//     section of paperboy.yaml. An error answer stops the plugin.
//   - "describe": the widgets the plugin offers, the sizes they are made for,
//     their arguments and the plugin's configuration keys, for an editor.
//   - "render": draw widgets for an image display, each in exactly the given
//     pixel size and number of gray levels, returned as PNG.
//   - "text": fill widgets of a text display, each with exactly the given
//     number of lines and columns.
//
// Plugins written in Go implement Handler, call Serve and draw with
// pkg/render, so widgets share the fonts and gray levels of Paperboy.
//
// Physical sizes (ADR 0050): every render request carries the display's pixel
// density in page.ppi, and each widget's style holds the page's padding, rule
// and base text height already converted to pixels. Widgets size their texts
// relative to Request.TextPx and convert other physical sizes with
// Request.PtPx and Request.MMPx, so a widget looks the same on a small dense
// panel and a large coarse one. Text displays get neither.
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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/pkg/render"
)

// Version is the protocol version a request carries. Version 3 (ADR 0050)
// added page.ppi and style.text_px and dropped style.text_size. Plugins report
// it in every answer (Message.Version); Paperboy refuses a plugin that speaks
// another version (ADR 0058).
const Version = 3

// Actions of a request.
const (
	ActionConfigure = "configure"
	ActionDescribe  = "describe"
	ActionRender    = "render"
	ActionText      = "text"
)

// EventChanged tells Paperboy that widgets are out of date.
const EventChanged = "changed"

// Request is one line Paperboy writes to standard input.
type Request struct {
	ID       string          `json:"id"`
	Version  int             `json:"version"`
	Action   string          `json:"action"`
	Language string          `json:"language,omitempty"` // "de" or "en"
	Timezone string          `json:"timezone,omitempty"` // IANA name
	Now      time.Time       `json:"now"`
	Config   map[string]any  `json:"config,omitempty"` // configure only
	Levels   int             `json:"levels,omitempty"` // render: gray levels, 2 or 4
	Page     PageInfo        `json:"page"`
	Widgets  []WidgetRequest `json:"widgets,omitempty"`
}

// Style carries the page's look so widgets match each other. Paperboy
// configures it in points and sends it converted to pixels with the
// display's PPI (ADR 0050); text displays get the zero value.
type Style struct {
	Padding int     `json:"padding"` // inner margin of a widget, px
	Rule    int     `json:"rule"`    // width of separator lines, px, at least 1
	TextPx  float64 `json:"text_px"` // base text height, px
}

// PageInfo describes the page a widget is drawn for.
type PageInfo struct {
	Title  string  `json:"title,omitempty"`
	Number int     `json:"number"` // 1-based position in the plan rule
	Count  int     `json:"count"`
	Width  int     `json:"width"`         // pixels or columns of the whole display
	Height int     `json:"height"`        // pixels or rows of the whole display
	PPI    float64 `json:"ppi,omitempty"` // pixels per inch; set for image displays, omitted for text displays
}

// WidgetRequest asks for one widget in an exact size: pixels for render,
// columns and rows for text.
type WidgetRequest struct {
	Widget string         `json:"widget"`
	Width  int            `json:"width"`
	Height int            `json:"height"`
	Args   map[string]any `json:"args,omitempty"`
	Style  Style          `json:"style"`
}

// Message is one line a plugin prints: the answer to request ID, or an event
// when ID is empty.
type Message struct {
	ID string `json:"id,omitempty"`
	// Version is the protocol version the plugin speaks. Handle fills it; a
	// plugin written by hand must set it, and a missing version counts as one
	// from before the field existed (ADR 0058).
	Version     int          `json:"version,omitempty"`
	Error       string       `json:"error,omitempty"` // the request failed as a whole
	Widgets     []Result     `json:"widgets,omitempty"`
	MaxAge      int          `json:"max_age,omitempty"` // seconds the results stay valid; 0 = Paperboy's default
	Description *Description `json:"description,omitempty"`
	Event       string       `json:"event,omitempty"`
	Changed     []string     `json:"changed,omitempty"` // widgets of a "changed" event; empty = all
}

// Result is one widget: a PNG for render, lines for text, or an error.
type Result struct {
	PNG   []byte   `json:"png,omitempty"` // base64 in JSON
	Lines []string `json:"lines,omitempty"`
	Error string   `json:"error,omitempty"`
}

// Description is the answer to describe.
type Description struct {
	Title   string         `json:"title"`
	Widgets []WidgetInfo   `json:"widgets"`
	Config  map[string]Arg `json:"config,omitempty"` // keys of the plugin's config section
}

// Kinds of displays a widget supports.
const (
	KindImage = "image"
	KindText  = "text"
)

// WidgetInfo describes one widget.
type WidgetInfo struct {
	Widget string   `json:"widget"`
	Title  string   `json:"title"`
	Kinds  []string `json:"kinds"` // image, text
	// Sizes are the grid spans the widget is designed for, as columns x
	// rows of the reference grid of 6 x 8 cells. They are hints for an
	// editor and the widget gallery; Paperboy enforces none of it.
	Sizes     []Span         `json:"sizes,omitempty"`
	MinWidth  int            `json:"min_width,omitempty"`  // pixels
	MinHeight int            `json:"min_height,omitempty"` // pixels
	Args      map[string]Arg `json:"args,omitempty"`
}

// Span is a size in grid cells.
type Span struct {
	Columns int `json:"columns"`
	Rows    int `json:"rows"`
}

// Arg describes one argument or configuration key.
type Arg struct {
	Type        string `json:"type"` // string, int, float, bool, duration, list
	Default     any    `json:"default,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
	// Enum lists the allowed values, if there is a fixed set (ADR 0055);
	// for a list, the allowed elements. Optional: editors offer a choice,
	// Check and Handle reject other values.
	Enum []any `json:"enum,omitempty"`
}

// Allows reports whether value (or, for a list, each element) is one of the
// arg's Enum values; any value is allowed without an Enum.
func (a Arg) Allows(value any) bool {
	if len(a.Enum) == 0 {
		return true
	}
	if list, ok := value.([]any); ok {
		for _, e := range list {
			if !a.Allows(e) {
				return false
			}
		}
		return true
	}
	s := fmt.Sprint(value)
	for _, e := range a.Enum {
		if fmt.Sprint(e) == s {
			return true
		}
	}
	return false
}

func (a Arg) enumText() string {
	parts := make([]string, len(a.Enum))
	for i, e := range a.Enum {
		parts[i] = fmt.Sprint(e)
	}
	return strings.Join(parts, ", ")
}

// CheckEnums rejects values outside the Enum of their arg; keys without a
// description are left alone.
func (v Values) CheckEnums(known map[string]Arg) error {
	names := make([]string, 0, len(v))
	for k := range v {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if a, ok := known[k]; ok && !a.Allows(v[k]) {
			return fmt.Errorf("%s: %v is not one of %s", k, v[k], a.enumText())
		}
	}
	return nil
}

// Handler is implemented by a plugin.
type Handler interface {
	Describe() Description
	// Configure receives the plugin's config section once after the start.
	Configure(cfg Values, req *Request) error
	// Render draws one widget on a canvas of the requested size and palette.
	Render(req *Request, w WidgetRequest, canvas *image.Paletted) error
}

// Texter is implemented by plugins with widgets for text displays. The
// result may be shorter than the requested size; Paperboy pads and cuts.
type Texter interface {
	Text(req *Request, w WidgetRequest) ([]string, error)
}

// Preparer is implemented by plugins that set up shared state once per
// request, before its widgets.
type Preparer interface {
	Prepare(req *Request) error
}

// Starter is implemented by plugins with background work, such as refreshing
// their data. Start runs once after a successful configure; notify tells
// Paperboy that widgets changed (none = all).
type Starter interface {
	Start(ctx context.Context, notify func(widgets ...string))
}

// Ager is implemented by plugins that set how long Paperboy may reuse their
// results.
type Ager interface {
	MaxAge(req *Request) time.Duration
}

// Serve answers requests from standard input until it closes and exits the
// process with status 1 on a broken stream.
func Serve(h Handler) {
	if err := ServeIO(context.Background(), h, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// ServeIO is Serve with explicit streams. Requests are handled one at a time;
// events from Start may arrive in between.
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
	started := false
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			if err := send(Message{Version: Version, Error: "invalid request: " + err.Error()}); err != nil {
				return err
			}
			continue
		}
		m := Handle(h, &req)
		if req.Action == ActionConfigure && m.Error == "" && !started {
			started = true
			if s, ok := h.(Starter); ok {
				s.Start(ctx, func(widgets ...string) {
					send(Message{Version: Version, Event: EventChanged, Changed: widgets})
				})
			}
		}
		if err := send(m); err != nil {
			return err
		}
	}
	return sc.Err()
}

// Handle answers one request. Serve uses it; Paperboy calls it directly for
// its built-in plugin. Every answer names the protocol version of this package,
// which is the version the plugin was built against.
func Handle(h Handler, req *Request) (m Message) {
	m.ID = req.ID
	m.Version = Version
	defer func() {
		if r := recover(); r != nil {
			m = Message{ID: req.ID, Version: Version, Error: fmt.Sprintf("panic: %v", r)}
		}
	}()
	switch req.Action {
	case ActionConfigure:
		if err := h.Configure(Values(req.Config), req); err != nil {
			m.Error = err.Error()
		}
	case ActionDescribe:
		d := h.Describe()
		m.Description = &d
	case ActionRender, ActionText:
		results, err := handleWidgets(h, req)
		if err != nil {
			m.Error = err.Error()
			return m
		}
		m.Widgets = results
		if a, ok := h.(Ager); ok {
			m.MaxAge = int(a.MaxAge(req) / time.Second)
		}
	default:
		m.Error = fmt.Sprintf("unknown action %q", req.Action)
	}
	return m
}

// CheckVersion reports whether the answering plugin speaks the protocol of
// this package. A plugin that names no version was built before the field
// existed and is refused as well, so that a stale zip fails loudly instead of
// being drawn with the wrong assumptions (ADR 0058).
func (m Message) CheckVersion() error {
	switch {
	case m.Version == Version:
		return nil
	case m.Version == 0:
		return fmt.Errorf("plugin reports no protocol version; rebuild it against protocol version %d", Version)
	default:
		return fmt.Errorf("plugin speaks protocol version %d, Paperboy speaks %d", m.Version, Version)
	}
}

func handleWidgets(h Handler, req *Request) ([]Result, error) {
	if req.Action == ActionRender && req.Levels != 2 && req.Levels != 4 {
		return nil, fmt.Errorf("levels %d: expected 2 or 4", req.Levels)
	}
	t, isTexter := h.(Texter)
	if req.Action == ActionText && !isTexter {
		return nil, errors.New("this plugin has no text widgets")
	}
	if p, ok := h.(Preparer); ok {
		if err := p.Prepare(req); err != nil {
			return nil, err
		}
	}
	out := make([]Result, 0, len(req.Widgets))
	args := map[string]map[string]Arg{}
	for _, wi := range h.Describe().Widgets {
		args[wi.Widget] = wi.Args
	}
	for _, w := range req.Widgets {
		if err := w.Arg().CheckEnums(args[w.Widget]); err != nil {
			out = append(out, Result{Error: err.Error()})
		} else if req.Action == ActionText {
			out = append(out, textOne(t, req, w))
		} else {
			out = append(out, renderOne(h, req, w))
		}
	}
	return out, nil
}

func renderOne(h Handler, req *Request, w WidgetRequest) (res Result) {
	defer func() {
		if r := recover(); r != nil {
			res = Result{Error: fmt.Sprintf("panic: %v", r)}
		}
	}()
	if w.Width < 1 || w.Height < 1 || w.Width > 4096 || w.Height > 4096 {
		return Result{Error: "invalid size"}
	}
	canvas := render.NewCanvas(w.Width, w.Height, req.Levels)
	if err := h.Render(req, w, canvas); err != nil {
		return Result{Error: err.Error()}
	}
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, canvas); err != nil {
		return Result{Error: err.Error()}
	}
	return Result{PNG: buf.Bytes()}
}

func textOne(t Texter, req *Request, w WidgetRequest) (res Result) {
	defer func() {
		if r := recover(); r != nil {
			res = Result{Error: fmt.Sprintf("panic: %v", r)}
		}
	}()
	if w.Width < 1 || w.Height < 1 || w.Width > 200 || w.Height > 100 {
		return Result{Error: "invalid size"}
	}
	lines, err := t.Text(req, w)
	if err != nil {
		return Result{Error: err.Error()}
	}
	if lines == nil {
		lines = []string{}
	}
	return Result{Lines: lines}
}

// ErrUnknownWidget is returned for a widget name the plugin does not know.
var ErrUnknownWidget = errors.New("unknown widget")

// Location returns the request's timezone, or the local one.
func (r *Request) Location() *time.Location {
	if r.Timezone != "" {
		if loc, err := time.LoadLocation(r.Timezone); err == nil {
			return loc
		}
	}
	return time.Local
}

// TextPx is the base text height in pixels for a widget, from the page style
// (ADR 0050). Widgets size their texts relative to it so that neighbouring
// widgets match; 16 when no style is set (text displays, tests).
func (r *Request) TextPx(w WidgetRequest) float64 {
	if w.Style.TextPx <= 0 {
		return 16
	}
	return w.Style.TextPx
}

// PtPx converts points (1/72 inch) to pixels with the display's PPI. Without
// a PPI (text displays) it returns 0.
func (r *Request) PtPx(pt float64) float64 { return pt * r.Page.PPI / 72 }

// MMPx converts millimetres to pixels with the display's PPI. Without a PPI
// (text displays) it returns 0.
func (r *Request) MMPx(mm float64) float64 { return mm * r.Page.PPI / 25.4 }

// Lang returns "de" or "en".
func (r *Request) Lang() string {
	if r.Language == "de" {
		return "de"
	}
	return "en"
}

// T picks the German or English text for the request's language.
func (r *Request) T(de, en string) string {
	if r.Lang() == "de" {
		return de
	}
	return en
}

// Values reads arguments and configuration keys with defaults.
type Values map[string]any

// Arg returns the widget's arguments.
func (w WidgetRequest) Arg() Values { return Values(w.Args) }

// Int reads an integer.
func (v Values) Int(name string, def int) int {
	switch x := v[name].(type) {
	case float64:
		return int(x)
	case int:
		return x
	}
	return def
}

// Float reads a number.
func (v Values) Float(name string, def float64) float64 {
	switch x := v[name].(type) {
	case float64:
		return x
	case int:
		return float64(x)
	}
	return def
}

// Bool reads a boolean.
func (v Values) Bool(name string, def bool) bool {
	if x, ok := v[name].(bool); ok {
		return x
	}
	return def
}

// String reads a string.
func (v Values) String(name, def string) string {
	if x, ok := v[name].(string); ok {
		return x
	}
	return def
}

// Strings reads a list of strings; a single string counts as a list of one.
func (v Values) Strings(name string) []string {
	switch x := v[name].(type) {
	case string:
		return []string{x}
	case []any:
		var out []string
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// Duration reads a duration such as "15m".
func (v Values) Duration(name string, def time.Duration) (time.Duration, error) {
	s, ok := v[name].(string)
	if !ok {
		if _, set := v[name]; set {
			return 0, fmt.Errorf("%s: expected a duration such as \"15m\"", name)
		}
		return def, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return d, nil
}

// Check rejects keys that are not in the description, so a typo in
// paperboy.yaml is an error instead of a silent default.
func (v Values) Check(known map[string]Arg) error {
	var unknown []string
	for k := range v {
		if _, ok := known[k]; !ok {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("unknown keys: %s", strings.Join(unknown, ", "))
	}
	for k, a := range known {
		if _, ok := v[k]; a.Required && !ok {
			return fmt.Errorf("%s is required", k)
		}
	}
	return v.CheckEnums(known)
}

// Columns puts left and right into one line of width characters; the left
// part is cut when both do not fit.
func Columns(left, right string, width int) string {
	r := []rune(right)
	room := width - len(r) - 1
	if room < 1 {
		return string([]rune(right)[:min(len(r), width)])
	}
	l := []rune(left)
	if len(l) > room {
		l = l[:room]
	}
	return string(l) + strings.Repeat(" ", width-len(l)-len(r)) + right
}

// Align places s in a line of width characters.
func Align(s string, width int, mode string) string {
	r := []rune(s)
	if len(r) >= width {
		return string(r[:width])
	}
	gap := width - len(r)
	switch mode {
	case "right":
		return strings.Repeat(" ", gap) + s
	case "center":
		return strings.Repeat(" ", gap/2) + s
	}
	return s
}
