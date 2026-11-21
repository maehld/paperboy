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

//   - "state": the stored state and blobs, once after configure; protocol 5
//     only (ADR 0081).
//   - "describe": the widgets the plugin offers, the sizes they are made for,
//     their arguments and the plugin's configuration keys, for an editor.
//   - "render": draw widgets for an image display, each in exactly the given
//     pixel size and number of gray levels, returned as PNG.
//   - "actions": report the current state of concrete actions, one answer per
//     requested action (ADR 0057).
//   - "action": run exactly one concrete action and report the outcome.
//   - "values": the declared values a text page lays out (ADR 0111).
//
// Events: Paperboy sends {"event":"schedule","next_refresh":"..."} to tell a
// plugin when its data is next needed (ADR 0126). A plugin embeds Demand to
// receive the event and Wait to sleep until then.
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
//
// State (ADR 0081): a plugin keeps what it needs between restarts as JSON.
// Right after configure Paperboy sends one "state" request with the stored
// object and the declared blobs; every answer and event may return a new one
// in Message.State (missing = unchanged, null = forget it) and new
// Message.Blobs. Large values are declared in Description.Blobs and travel
// base64-coded. Paperboy stores both itself.
//
// Compatibility: the protocol changes only with a version and an ADR. Version
// 8 removed the text action; version 9 adds the schedule event, the enum value
// type and removes plan conditions. Otherwise it grows additively. Paperboy
// accepts every plugin that speaks at least MinVersion, ignores fields it does
// not know, and a new version may add fields and actions without changing the
// meaning of the existing ones.
//
// Environment: a plugin process sees PAPERBOY_LANGUAGE and PAPERBOY_TIMEZONE,
// and it runs in the plugins directory.
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

	"github.com/maehld/paperboy/sdk/render"
)

// Version is the protocol version a request carries. Version 3 (ADR 0050)
// added page.ppi and style.text_px and dropped style.text_size. Version 4
// (ADR 0057) added actions: describe.actions, the actions and action requests,
// and action results. Version 5 (ADR 0081) added the plugin state and blobs:
// the state request, Message.state, describe.blobs and the blobs of answers.
// Version 6 (ADR 0103) added conditions, which version 9 removes again in
// favour of enum values (ADR 0127). Version 7 (ADR 0111) added values:
// describe.values, the values request and Message.values, with arguments per
// value and the request's value_args (ADR 0120), and the unit progression of
// a value (ADR 0122). Version 8 (ADR 0125) removed the text action:
// ActionText, the Texter interface, Result.lines, WidgetRequest.scroll and the
// text display kind of a widget are gone. Version 9 (ADR 0126, 0127) adds the
// inbound schedule event, the enum value type and the labels a text cell may
// carry, and removes the conditions request and describe.conditions. A plugin
// below 8 is still accepted while it serves values, because nothing asks it
// for text; Paperboy sends a schedule only to a plugin that reports version 9
// or newer.
// Plugins report the version in every answer (Message.Version); Paperboy
// refuses a plugin that speaks less than MinVersion (ADR 0081).
const Version = 9

// MinVersion is the oldest protocol Paperboy accepts. Version 5 is the cut
// that introduced state and blobs; from here on the protocol grows additively,
// so a newer plugin is accepted too (ADR 0081).
const MinVersion = 5

// Actions of a request.
const (
	ActionConfigure = "configure"
	ActionLoadState = "state" // the stored state and blobs, once after configure (protocol 5)
	ActionDescribe  = "describe"
	ActionRender    = "render"
	ActionList      = "actions" // states of concrete actions
	ActionInvoke    = "action"  // run one concrete action
	// ActionValues asks for declared values a text page lays out (ADR 0111).
	ActionValues = "values"
)

// EventChanged tells Paperboy that widgets are out of date.
const EventChanged = "changed"

// EventSchedule tells a plugin when Paperboy next needs its data (ADR 0126).
// Paperboy sends it only to a plugin that reports protocol 9 or newer.
const EventSchedule = "schedule"

// Request is one line Paperboy writes to standard input.
type Request struct {
	ID       string          `json:"id"`
	Version  int             `json:"version"`
	Action   string          `json:"action"`
	Language string          `json:"language,omitempty"` // "de" or "en"
	Timezone string          `json:"timezone,omitempty"` // IANA name
	Now      time.Time       `json:"now"`
	Config   map[string]any  `json:"config,omitempty"` // configure only
	Levels   int             `json:"levels,omitempty"` // render: gray levels of the display; the canvas falls back to 2, 4 or 16
	Page     PageInfo        `json:"page"`
	Widgets  []WidgetRequest `json:"widgets,omitempty"`
	// Actions are the concrete actions of an actions or action request, in
	// the plan's order. Device is set for both requests.
	Actions []ActionCall `json:"actions,omitempty"`
	Device  *DeviceInfo  `json:"device,omitempty"`
	// ServerStatus is Paperboy's own view of the devices: the device the
	// request is for and the devices the page names. It is a privileged
	// object, separate from the page data, and only the built-in plugin
	// receives it for now (ADR 0115).
	ServerStatus *ServerStatus `json:"server_status,omitempty"`
	// Values are the declared value keys a values request asks for; one
	// request per plugin, page and argument set covers every key of that page
	// (ADR 0111, 0120). ValueArgs carries the arguments of that set.
	Values []string `json:"values,omitempty"`
	// ValueArgs are the arguments of every value in Values (ADR 0120). A
	// values request carries one set, because its answer is keyed by name.
	// A value that declares args reads and checks them here.
	ValueArgs Values `json:"value_args,omitempty"`
	// State is the JSON object the plugin stored last time; missing when it
	// has none. It arrives with the state request, once after configure
	// (ADR 0081).
	State json.RawMessage `json:"state,omitempty"`
	// Blobs are the files a plugin keeps besides its state, base64 in JSON.
	// They arrive with the state request only, because they can be large.
	Blobs map[string][]byte `json:"blobs,omitempty"`
}

// DeviceInfo identifies the display an action is meant for.
type DeviceInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Hardware string `json:"hardware,omitempty"` // firmware target, e.g. sticky
}

// ServerStatus is Paperboy's privileged view of the devices. It reaches only
// the built-in plugin for now, so a plugin process with a cached widget
// cannot show stale rows (ADR 0115). It carries no key, MAC or address.
type ServerStatus struct {
	// CurrentDeviceStatus is the device the request is for; nil in a preview
	// without one.
	CurrentDeviceStatus *DeviceStatus `json:"current_device_status,omitempty"`
	// SelectedDevicesStatus holds only the devices a page names in its widget
	// arguments, keyed by id. It does not list every configured device.
	SelectedDevicesStatus map[string]DeviceStatus `json:"selected_devices_status,omitempty"`
}

// DeviceStatus is one device in a ServerStatus, from the dashboard's point of
// view.
type DeviceStatus struct {
	ID       string `json:"id"`
	Display  string `json:"display,omitempty"`  // display type name
	Plan     string `json:"plan,omitempty"`     // plan file
	Hardware string `json:"hardware,omitempty"` // firmware target, e.g. sticky
	Firmware string `json:"firmware,omitempty"`
	// Status is ok, late, never, unregistered or unknown. "unknown" names an
	// id a widget asked for that no device file has.
	Status   string         `json:"status"`
	LastSeen time.Time      `json:"last_seen,omitempty"`
	SeenS    int64          `json:"seen_s,omitempty"` // seconds since the last contact; -1 when unknown
	Power    string         `json:"power,omitempty"`  // battery or mains
	RSSI     string         `json:"rssi,omitempty"`
	Battery  *BatteryStatus `json:"battery,omitempty"`
	// RuntimeS is the estimated battery runtime in seconds, 0 while there is
	// no estimate or the device runs on mains.
	RuntimeS int64 `json:"runtime_s,omitempty"`
	// RuntimeSpanS is the data behind the estimate, in seconds.
	RuntimeSpanS int64  `json:"runtime_span_s,omitempty"`
	Page         string `json:"page,omitempty"`      // last page or notice
	PageKind     string `json:"page_kind,omitempty"` // page, notice or battery
	Error        string `json:"error,omitempty"`     // last render or plan error
}

// BatteryStatus is the last battery reading of a device.
type BatteryStatus struct {
	Volts    float64  `json:"volts,omitempty"`
	Percent  *float64 `json:"percent,omitempty"`
	MAH      *float64 `json:"mah,omitempty"`
	MA       *float64 `json:"ma,omitempty"`
	Charging bool     `json:"charging,omitempty"`
	Level    string   `json:"level,omitempty"` // "", warn or critical
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

// WidgetRequest asks for one widget in an exact size in pixels.
type WidgetRequest struct {
	Widget string         `json:"widget"`
	Width  int            `json:"width"`
	Height int            `json:"height"`
	Args   map[string]any `json:"args,omitempty"`
	Style  Style          `json:"style"`
}

// ActionCall is one concrete action: the plugin's action name without the
// plugin prefix and the plan's arguments for it.
type ActionCall struct {
	Action string         `json:"action"`
	Args   map[string]any `json:"args,omitempty"`
}

// Arg returns the action's arguments.
func (c ActionCall) Arg() Values { return Values(c.Args) }

// Message is one line a plugin prints: the answer to request ID, or an event
// when ID is empty.
type Message struct {
	ID string `json:"id,omitempty"`
	// Version is the protocol version the plugin speaks. Handle fills it; a
	// plugin written by hand must set it, and a missing version counts as one
	// from before the field existed (ADR 0058).
	Version int            `json:"version,omitempty"`
	Error   string         `json:"error,omitempty"` // the request failed as a whole
	Widgets []Result       `json:"widgets,omitempty"`
	Actions []ActionState  `json:"actions,omitempty"` // answers to an actions request, in request order
	Result  *ActionOutcome `json:"result,omitempty"`  // answer to an action request
	// Values is the answer to a values request, keyed by the requested name
	// (ADR 0111). A missing name is an error for that cell.
	Values      map[string]Value `json:"values,omitempty"`
	MaxAge      int              `json:"max_age,omitempty"` // seconds the results stay valid; 0 = Paperboy's default
	Description *Description     `json:"description,omitempty"`
	Event       string           `json:"event,omitempty"`
	Changed     []string         `json:"changed,omitempty"` // widgets of a "changed" event; empty = all
	// State is the JSON object to store for the next start (ADR 0081). A
	// missing field means unchanged, a null value forgets the stored state.
	State *json.RawMessage `json:"state,omitempty"`
	// Blobs are changed files to store, base64 in JSON; missing = unchanged.
	Blobs map[string][]byte `json:"blobs,omitempty"`
}

// Result is one widget: a PNG, or an error.
type Result struct {
	PNG   []byte `json:"png,omitempty"` // base64 in JSON
	Error string `json:"error,omitempty"`
}

// Description is the answer to describe.
type Description struct {
	Title string `json:"title"`
	// Version is the plugin's own version, e.g. from a VERSION file next to
	// its source. Paperboy compares it with the file name of the program and
	// logs a mismatch (ADR 0075); empty means the plugin does not say.
	Version string       `json:"version,omitempty"`
	Widgets []WidgetInfo `json:"widgets"`
	// Values are the data keys a text page may lay out (ADR 0111). A page
	// asks for them with the values request.
	Values  []ValueSpec    `json:"values,omitempty"`
	Actions []ActionSpec   `json:"actions,omitempty"` // actions a plan may list (ADR 0057)
	Config  map[string]Arg `json:"config,omitempty"`  // keys of the plugin's config section
	// Blobs names the files the plugin keeps besides its state, e.g.
	// "render.png" (ADR 0081). Paperboy stores them under blobs/ of the
	// plugin's directory and logs files it does not expect.
	Blobs []string `json:"blobs,omitempty"`
}

// Kinds of displays a widget supports. Every widget is for an image display;
// a text page is built from values (ADR 0111, 0125).
const (
	KindImage = "image"
)

// WidgetInfo describes one widget.
type WidgetInfo struct {
	Widget string   `json:"widget"`
	Title  string   `json:"title"`
	Kinds  []string `json:"kinds"` // image
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

// Kinds of values (ADR 0111). The type is what Paperboy formats for a text
// page; a value is data, not text.
const (
	ValueNumber    = "number"
	ValueText      = "text"
	ValueDuration  = "duration" // seconds
	ValueTimestamp = "timestamp"
	ValueDirection = "direction" // 1 up, 0 flat, -1 down
	ValueEnum      = "enum"      // one key of ValueSpec.Enum (ADR 0127)
)

// Kinds of a value declaration. Only single values exist in protocol 7; list
// is reserved for the records of a later version (ADR 0111).
const (
	ValueKindValue = "value"
	ValueKindList  = "list"
)

// ValueSpec describes one value a plugin declares.
type ValueSpec struct {
	Name  string `json:"name"`  // the key in a page and in the values answer
	Title string `json:"title"` // display name for editors
	// Type is number, text, duration, timestamp, direction or enum.
	Type string `json:"type"`
	// Enum lists the allowed keys of an enum value (ADR 0127). Paperboy
	// rejects an answer that names another key. The keys carry no labels
	// here: a text cell shows its own label per key.
	Enum []string `json:"enum,omitempty"`
	// Unit is the base unit a plugin answers a number in, e.g. "W". A page
	// may override the text or hide it with "none" (ADR 0122).
	Unit string `json:"unit,omitempty"`
	// Precision is the number of decimals of the base unit.
	Precision int `json:"precision,omitempty"`
	// Progression are the larger units Paperboy may show the value in, in
	// order, e.g. kW and MW for a base unit of W (ADR 0122). Paperboy rounds
	// the value with a step's precision and takes the largest step whose
	// rounded absolute value is at least 1. Empty means the value always
	// stays in its base unit.
	Progression []UnitStep `json:"progression,omitempty"`
	// Kind is value (default) or list; protocol 7 only answers values.
	Kind string `json:"kind,omitempty"`
	// Args are the arguments the value reads, with the same shape as a
	// widget's arguments (ADR 0055, 0120). Empty means the value takes none.
	Args map[string]Arg `json:"args,omitempty"`
}

// UnitStep is one larger unit of a value's progression (ADR 0122).
type UnitStep struct {
	Unit      string  `json:"unit"`      // e.g. "kW"
	Factor    float64 `json:"factor"`    // to the base unit, e.g. 1000
	Precision int     `json:"precision"` // decimals in this step
}

// Value is one answer of a values request. Exactly one field is set; the
// matching one follows ValueSpec.Type. The pointers keep a zero number and an
// empty text apart from an absent field.
type Value struct {
	Number    *float64 `json:"number,omitempty"`
	Text      *string  `json:"text,omitempty"`
	Duration  *int64   `json:"duration,omitempty"`  // seconds
	Timestamp *string  `json:"timestamp,omitempty"` // RFC 3339
	Direction *int     `json:"direction,omitempty"` // 1 up, 0 flat, -1 down
	Enum      *string  `json:"enum,omitempty"`      // one key of ValueSpec.Enum
}

// NumberValue returns a value of type number.
func NumberValue(f float64) Value { return Value{Number: &f} }

// TextValue returns a value of type text.
func TextValue(s string) Value { return Value{Text: &s} }

// DurationValue returns a value of type duration, in seconds.
func DurationValue(d time.Duration) Value {
	s := int64(d / time.Second)
	return Value{Duration: &s}
}

// TimestampValue returns a value of type timestamp.
func TimestampValue(t time.Time) Value {
	s := t.Format(time.RFC3339)
	return Value{Timestamp: &s}
}

// DirectionValue returns a trend direction: 1 up, 0 flat, -1 down.
func DirectionValue(d int) Value { return Value{Direction: &d} }

// EnumValue returns a value of type enum, one key of the declaration's Enum.
func EnumValue(key string) Value { return Value{Enum: &key} }

// Type names the field a value carries, or "" when it carries none.
func (v Value) Type() string {
	switch {
	case v.Number != nil:
		return ValueNumber
	case v.Text != nil:
		return ValueText
	case v.Duration != nil:
		return ValueDuration
	case v.Timestamp != nil:
		return ValueTimestamp
	case v.Direction != nil:
		return ValueDirection
	case v.Enum != nil:
		return ValueEnum
	}
	return ""
}

// Time reads a timestamp value in loc.
func (v Value) Time(loc *time.Location) (time.Time, error) {
	if v.Timestamp == nil {
		return time.Time{}, errors.New("not a timestamp")
	}
	t, err := time.Parse(time.RFC3339, *v.Timestamp)
	if err != nil {
		return time.Time{}, fmt.Errorf("timestamp %q: %w", *v.Timestamp, err)
	}
	if loc != nil {
		t = t.In(loc)
	}
	return t, nil
}

// ErrUnknownValue is returned for a value name a plugin does not declare.
var ErrUnknownValue = errors.New("unknown value")

// ActionSpec describes one action a plugin offers (ADR 0057).
type ActionSpec struct {
	Name    string         `json:"name"`
	Title   string         `json:"title"`
	Args    map[string]Arg `json:"args,omitempty"` // the same schema as widget args
	Confirm *Confirm       `json:"confirm,omitempty"`
}

// Confirm says whether an action needs a confirmation before it runs and which
// text the confirmation shows. A nil Confirm means no confirmation.
type Confirm struct {
	Required bool   `json:"required"`
	Text     string `json:"text,omitempty"` // optional; Paperboy has a generic text
}

// ActionState is the answer for one ActionCall, in the request's order.
type ActionState struct {
	Title string `json:"title,omitempty"` // current title, overrides the spec
	// Disabled holds the reason why the action cannot run now; empty means
	// it is available. The menu shows the action hatched with this reason.
	Disabled string   `json:"disabled,omitempty"`
	Confirm  *Confirm `json:"confirm,omitempty"` // overrides the spec
}

// ActionOutcome is the answer to an action request. A protocol or plugin
// failure uses Message.Error; OK is false when the action itself failed, with
// Message telling the user why.
type ActionOutcome struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Changed bool   `json:"changed,omitempty"` // widgets are out of date after the run
}

// Arg describes one argument or configuration key.
type Arg struct {
	Type        string `json:"type"` // string, int, float, bool, duration, list
	Default     any    `json:"default,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
	// Enum lists the allowed values, if there is a fixed set (ADR 0055);
	// for a list, the allowed elements. A plugin may compute them from its
	// current data on every describe, as the calendar plugin does for its
	// calendar names. Optional: editors offer a choice,
	// Check and Handle reject other values.
	Enum []any `json:"enum,omitempty"`
	// EnumLabels names the values for the editor, keyed by fmt.Sprint of the
	// value. The stored value stays the key, so a missing or renamed label
	// never changes the page; without labels the editor shows the key.
	EnumLabels map[string]string `json:"enum_labels,omitempty"`
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

// Valuer is implemented by plugins whose values a text page lays out
// (ADR 0111). Values answers every name in req.Values; a name the plugin does
// not declare is an error. A value that declares args reads req.ValueArgs and
// checks them with Values.Check (ADR 0120).
type Valuer interface {
	Values(req *Request) (map[string]Value, error)
}

// Actioner is implemented by plugins that offer actions the device menu can
// run (ADR 0057). Actions answers each call of the request in order; Invoke
// runs the one call of an action request.
type Actioner interface {
	Actions(req *Request) ([]ActionState, error)
	Invoke(req *Request, call ActionCall) (ActionOutcome, error)
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

// Stater is implemented by plugins that keep state between restarts
// (protocol 5). LoadState receives the stored object and blobs once after
// configure; State returns the whole object to store, nil meaning "nothing
// changed" (ADR 0081). Paperboy stores it after every answer and event.
type Stater interface {
	LoadState(state json.RawMessage, blobs map[string][]byte) error
	State() json.RawMessage
}

// Blober is implemented by plugins that keep files besides their state, such
// as a downloaded image. Blobs returns the ones that changed since the last
// call, so the plugin must clear its dirty marks when it returns them; nil
// means unchanged (ADR 0081). Paperboy stores each valid name under blobs/.
type Blober interface {
	Blobs() map[string][]byte
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
	start := func() {
		if started {
			return
		}
		started = true
		if s, ok := h.(Starter); ok {
			s.Start(ctx, func(widgets ...string) {
				send(withState(h, Message{Version: Version, Event: EventChanged, Changed: widgets}))
			})
		}
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var head struct {
			ID    string `json:"id"`
			Event string `json:"event"`
		}
		if err := json.Unmarshal(line, &head); err != nil {
			if err := send(Message{Version: Version, Error: "invalid request: " + err.Error()}); err != nil {
				return err
			}
			continue
		}
		if head.ID == "" && head.Event != "" {
			var ev Event
			if err := json.Unmarshal(line, &ev); err != nil {
				continue // no id, so no answer; a broken event is ignored
			}
			if e, ok := h.(Eventer); ok {
				e.Event(ev)
			}
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
		if req.Action == ActionConfigure && m.Error == "" {
			// A plugin with state starts after the state request, so its
			// background work sees the restored state (ADR 0081).
			if _, ok := h.(Stater); !ok {
				start()
			}
		}
		if req.Action == ActionLoadState && m.Error == "" {
			start()
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
	case ActionLoadState:
		if s, ok := h.(Stater); ok {
			if err := s.LoadState(req.State, req.Blobs); err != nil {
				m.Error = err.Error()
			}
		}
	case ActionDescribe:
		d := h.Describe()
		m.Description = &d
	case ActionRender:
		results, err := handleWidgets(h, req)
		if err != nil {
			m.Error = err.Error()
			return m
		}
		m.Widgets = results
		if a, ok := h.(Ager); ok {
			m.MaxAge = int(a.MaxAge(req) / time.Second)
		}
	case ActionList, ActionInvoke:
		handleActions(h, req, &m)
	case ActionValues:
		handleValues(h, req, &m)
	default:
		m.Error = fmt.Sprintf("unknown action %q", req.Action)
	}
	if m.Error == "" {
		m = withState(h, m)
	}
	return m
}

// withState attaches what the plugin wants stored to an answer or an event.
func withState(h Handler, m Message) Message {
	if st, ok := h.(Stater); ok {
		if data := st.State(); len(data) > 0 {
			raw := json.RawMessage(data)
			m.State = &raw
		}
	}
	if b, ok := h.(Blober); ok {
		if blobs := b.Blobs(); len(blobs) > 0 {
			m.Blobs = blobs
		}
	}
	return m
}

// CheckVersion reports whether the answering plugin speaks the protocol of
// this package. A plugin that names no version was built before the field
// existed and is refused as well, so that a stale zip fails loudly instead of
// being drawn with the wrong assumptions (ADR 0058).
func (m Message) CheckVersion() error {
	switch {
	case m.Version >= MinVersion:
		return nil
	case m.Version == 0:
		return fmt.Errorf("plugin reports no protocol version; rebuild it against protocol version %d", Version)
	default:
		return fmt.Errorf("plugin speaks protocol version %d; Paperboy needs at least %d", m.Version, MinVersion)
	}
}

func handleWidgets(h Handler, req *Request) ([]Result, error) {
	// Any tone count is accepted: render.NewCanvas falls back to the next
	// supported palette, so a display type with another count cannot fail a
	// plugin that was built before it (ADR 0107).
	if req.Levels < 1 || req.Levels > 256 {
		return nil, fmt.Errorf("levels %d: expected 1 to 256", req.Levels)
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
		} else {
			out = append(out, renderOne(h, req, w))
		}
	}
	return out, nil
}

// handleActions answers an actions or action request. Answers are positional:
// the plugin returns one state per call, in the same order.
func handleActions(h Handler, req *Request, m *Message) {
	a, ok := h.(Actioner)
	if !ok {
		m.Error = "this plugin has no actions"
		return
	}
	for _, c := range req.Actions {
		if c.Action == "" {
			m.Error = "actions: action missing"
			return
		}
	}
	if req.Action == ActionInvoke {
		if len(req.Actions) != 1 {
			m.Error = fmt.Sprintf("action: expected exactly one action, got %d", len(req.Actions))
			return
		}
		out, err := a.Invoke(req, req.Actions[0])
		if err != nil {
			m.Error = err.Error()
			return
		}
		m.Result = &out
		return
	}
	states, err := a.Actions(req)
	if err != nil {
		m.Error = err.Error()
		return
	}
	if len(states) != len(req.Actions) {
		m.Error = fmt.Sprintf("actions: got %d states for %d actions", len(states), len(req.Actions))
		return
	}
	m.Actions = states
	if ag, ok := h.(Ager); ok {
		m.MaxAge = int(ag.MaxAge(req) / time.Second)
	}
}

// handleValues answers a values request (ADR 0111). A plugin without the
// Valuer interface has none, and a request without names is an error. Like a
// widget request it prepares the plugin first, because a values answer reads
// the same per-request snapshot a widget does.
func handleValues(h Handler, req *Request, m *Message) {
	v, ok := h.(Valuer)
	if !ok {
		m.Error = "this plugin has no values"
		return
	}
	if len(req.Values) == 0 {
		m.Error = "values: no names requested"
		return
	}
	if p, ok := h.(Preparer); ok {
		if err := p.Prepare(req); err != nil {
			m.Error = err.Error()
			return
		}
	}
	values, err := v.Values(req)
	if err != nil {
		m.Error = err.Error()
		return
	}
	if values == nil {
		values = map[string]Value{}
	}
	m.Values = values
	if a, ok := h.(Ager); ok {
		m.MaxAge = int(a.MaxAge(req) / time.Second)
	}
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

// ErrUnknownWidget is returned for a widget name the plugin does not know.
var ErrUnknownWidget = errors.New("unknown widget")

// ErrTooSmall is returned by a widget whose cell leaves no room to draw in.
// Paperboy draws the error as the cell's hatched notice, so a widget that
// cannot draw anything says why instead of leaving the cell blank.
var ErrTooSmall = errors.New("cell too small")

// TooSmall reports that w's cell is too small for the widget.
func TooSmall(w WidgetRequest) error {
	return fmt.Errorf("%w: %dx%d", ErrTooSmall, w.Width, w.Height)
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
