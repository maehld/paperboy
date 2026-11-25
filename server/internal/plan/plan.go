// Package plan parses display plans and answers which pages a device shows
// (ADR 0158). A plan is an ordered list of pages, a refresh schedule and
// ordered triggers. Plans know nothing about display types: they only name
// pages, and the server picks the file per device. The timezone comes from the
// service configuration (ADR 0033).
package plan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/maehld/paperboy/server/internal/duration"
	"github.com/maehld/paperboy/server/internal/ident"
	"github.com/maehld/paperboy/server/internal/strictyaml"
)

// Duration accepts duration strings such as "5m" in YAML.
type Duration = duration.Duration

// Clock is a local time of day written as "HH:MM".
type Clock int // minutes since midnight

func parseClock(s string) (Clock, error) {
	if len(s) != 5 || s[2] != ':' {
		return 0, fmt.Errorf("expected a time of day as \"HH:MM\", got %q", s)
	}
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, fmt.Errorf("expected a time of day as \"HH:MM\", got %q", s)
	}
	return Clock(t.Hour()*60 + t.Minute()), nil
}

func (c *Clock) UnmarshalYAML(v *yaml.Node) error {
	var s string
	if err := v.Decode(&s); err != nil {
		return fmt.Errorf("expected a time of day as \"HH:MM\"")
	}
	clock, err := parseClock(s)
	if err != nil {
		return err
	}
	*c = clock
	return nil
}

func (c Clock) MarshalYAML() (any, error) { return clockString(c), nil }

func clockString(c Clock) string { return fmt.Sprintf("%02d:%02d", int(c)/60, int(c)%60) }

// Date is a local calendar day written as YYYY-MM-DD. A trigger with dates
// matches the whole day (ADR 0158).
type Date string

func (d Date) check() error {
	if _, err := time.Parse("2006-01-02", string(d)); err != nil {
		return fmt.Errorf("expected a date as \"YYYY-MM-DD\", got %q", string(d))
	}
	return nil
}

func (d *Date) UnmarshalYAML(v *yaml.Node) error {
	if v.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: expected a date as \"YYYY-MM-DD\"", v.Line)
	}
	date := Date(v.Value)
	if err := date.check(); err != nil {
		return fmt.Errorf("line %d: %w", v.Line, err)
	}
	*d = date
	return nil
}

func (d Date) MarshalYAML() (any, error) { return string(d), nil }

func (d *Date) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("expected a date as \"YYYY-MM-DD\"")
	}
	date := Date(s)
	if err := date.check(); err != nil {
		return err
	}
	*d = date
	return nil
}

func (d Date) MarshalJSON() ([]byte, error) { return json.Marshal(string(d)) }

// matches reports whether the local day of at is the date.
func (d Date) matches(at time.Time) bool { return at.Format("2006-01-02") == string(d) }

// Plan is one file under plans/. The JSON tags carry the same model to the
// editor API (ADR 0066).
type Plan struct {
	// Pages is the rotation, in order. A hidden page is not in it and only a
	// trigger can show it.
	Pages []Page `yaml:"pages" json:"pages"`
	// OnRefresh decides what a timed refresh does and how a cursor resolves
	// when its page leaves the list (ADR 0158).
	OnRefresh RefreshMode `yaml:"on_refresh" json:"on_refresh,omitempty"`
	// Refresh is the default interval and its time windows.
	Refresh Refresh `yaml:"refresh" json:"refresh,omitempty"`
	// Triggers bring a page to the front while their condition holds; the
	// first matching one wins.
	Triggers []Trigger `yaml:"triggers" json:"triggers,omitempty"`
	// Actions existed for the device menu (ADR 0057). The key stays to reject
	// it with a hint (ADR 0161).
	Actions any `yaml:"actions" json:"-"`

	// The rule model of ADR 0025 is gone. These fields exist only to reject
	// the old keys with a hint.
	Rules    []any  `yaml:"rules" json:"-"`
	Fallback string `yaml:"fallback" json:"-"`

	loc *time.Location
}

// Page is one entry of the rotation. Hidden pages have no place in it, but a
// trigger may still name them.
type Page struct {
	Name   string `yaml:"page" json:"page"`
	Hidden bool   `yaml:"hidden" json:"hidden,omitempty"`
}

// UnmarshalYAML reads a page entry as a name or as {page: <name>, hidden: true}.
func (p *Page) UnmarshalYAML(v *yaml.Node) error {
	p.Name, p.Hidden = "", false
	switch v.Kind {
	case yaml.ScalarNode:
		return v.Decode(&p.Name)
	case yaml.MappingNode:
		if err := checkKeys(v, "page", "hidden"); err != nil {
			return err
		}
		type plain Page
		return v.Decode((*plain)(p))
	}
	return fmt.Errorf("line %d: expected a page name or {page: <name>, hidden: true}", v.Line)
}

// MarshalYAML writes a visible page as its name and a hidden one as a mapping.
func (p Page) MarshalYAML() (any, error) {
	if !p.Hidden {
		return p.Name, nil
	}
	return struct {
		Page   string `yaml:"page"`
		Hidden bool   `yaml:"hidden"`
	}{p.Name, true}, nil
}

// UnmarshalJSON reads the two forms the editor API uses.
func (p *Page) UnmarshalJSON(b []byte) error {
	p.Name, p.Hidden = "", false
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, &p.Name)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("expected a page name or {\"page\": \"name\", \"hidden\": true}")
	}
	for key := range raw {
		switch key {
		case "page", "hidden":
		default:
			return fmt.Errorf("field %s not found in type plan.Page", key)
		}
	}
	if v, ok := raw["page"]; ok {
		if err := json.Unmarshal(v, &p.Name); err != nil {
			return fmt.Errorf("page: expected a name")
		}
	}
	if v, ok := raw["hidden"]; ok {
		if err := json.Unmarshal(v, &p.Hidden); err != nil {
			return fmt.Errorf("hidden: expected true or false")
		}
	}
	return nil
}

// MarshalJSON keeps both forms of the file in the editor API.
func (p Page) MarshalJSON() ([]byte, error) {
	if !p.Hidden {
		return json.Marshal(p.Name)
	}
	return json.Marshal(struct {
		Page   string `json:"page"`
		Hidden bool   `json:"hidden"`
	}{p.Name, true})
}

// RefreshMode decides what a timed refresh does with the page cursor and how a
// lost cursor resolves (ADR 0158).
type RefreshMode string

const (
	// RefreshCurrent keeps the page.
	RefreshCurrent RefreshMode = "current"
	// RefreshFirst shows the first page.
	RefreshFirst RefreshMode = "first"
	// RefreshCycle advances to the next page.
	RefreshCycle RefreshMode = "cycle"
)

// AlertMode says when a trigger asks the device to alert (ADR 0131). The device
// decides what an alert is: a beep, a blink or both. The empty mode means
// never; a trigger without the key stays quiet.
type AlertMode string

const (
	// AlertNever keeps the device quiet.
	AlertNever AlertMode = ""
	// AlertOnce alerts when the trigger starts.
	AlertOnce AlertMode = "once"
	// AlertRefresh alerts when the trigger starts and on every sync without
	// button input while it holds, so a low glucose value reminds each time.
	AlertRefresh AlertMode = "refresh"
)

// UnmarshalYAML accepts once and refresh, and never as the explicit way to
// stay quiet.
func (a *AlertMode) UnmarshalYAML(v *yaml.Node) error {
	var s string
	if err := v.Decode(&s); err != nil {
		return fmt.Errorf("expected never, once or refresh")
	}
	mode := AlertMode(s)
	switch mode {
	case "never", AlertNever:
		*a = AlertNever
		return nil
	case AlertOnce, AlertRefresh:
		*a = mode
		return nil
	}
	return fmt.Errorf("expected never, once or refresh, got %q", s)
}

// Refresh is the plan's schedule: a default interval and the windows that
// override it. The zero value leaves the device's own refresh in place.
type Refresh struct {
	Default Duration `yaml:"default" json:"default,omitempty"`
	Windows []Window `yaml:"windows" json:"windows,omitempty"`
}

// Window is one time window of the refresh schedule. from is inclusive and to
// is exclusive; from later than to crosses midnight.
type Window struct {
	From     Clock    `yaml:"from" json:"from"`
	To       Clock    `yaml:"to" json:"to"`
	Interval Duration `yaml:"interval" json:"interval"`
}

// UnmarshalYAML reads `refresh: 10m` or `refresh: {default: …, windows: […]}`.
func (r *Refresh) UnmarshalYAML(v *yaml.Node) error {
	*r = Refresh{}
	switch v.Kind {
	case yaml.ScalarNode:
		if v.Tag == "!!null" {
			return nil
		}
		return v.Decode(&r.Default)
	case yaml.MappingNode:
		if err := checkKeys(v, "default", "windows"); err != nil {
			return err
		}
		for i := 0; i+1 < len(v.Content); i += 2 {
			switch v.Content[i].Value {
			case "default":
				if err := v.Content[i+1].Decode(&r.Default); err != nil {
					return fmt.Errorf("line %d: default: %w", v.Content[i].Line, err)
				}
			case "windows":
				if err := v.Content[i+1].Decode(&r.Windows); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return fmt.Errorf("line %d: expected a duration or {default: …, windows: […]}, got %s", v.Line, v.ShortTag())
}

// UnmarshalJSON reads the same two forms.
func (r *Refresh) UnmarshalJSON(b []byte) error {
	*r = Refresh{}
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, &r.Default)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("expected a duration or {\"default\": …, \"windows\": […]}")
	}
	for key := range raw {
		switch key {
		case "default", "windows":
		default:
			return fmt.Errorf("field %s not found in type plan.Refresh", key)
		}
	}
	if v, ok := raw["default"]; ok {
		if err := json.Unmarshal(v, &r.Default); err != nil {
			return err
		}
	}
	if v, ok := raw["windows"]; ok {
		if err := json.Unmarshal(v, &r.Windows); err != nil {
			return err
		}
	}
	return nil
}

// MarshalJSON writes a duration when it is the only value, else the map.
func (r Refresh) MarshalJSON() ([]byte, error) {
	if len(r.Windows) == 0 {
		return json.Marshal(r.Default)
	}
	return json.Marshal(struct {
		Default Duration `json:"default,omitempty"`
		Windows []Window `json:"windows"`
	}{r.Default, r.Windows})
}

// MarshalYAML writes a duration when it is the only value, else the map.
func (r Refresh) MarshalYAML() (any, error) {
	if len(r.Windows) == 0 {
		return r.Default, nil
	}
	return struct {
		Default Duration `yaml:"default,omitempty"`
		Windows []Window `yaml:"windows"`
	}{r.Default, r.Windows}, nil
}

// Trigger brings a page to the front while every condition holds (ADR 0158).
// Triggers are tried in order; the first matching one wins.
type Trigger struct {
	When  Conditions `yaml:"when" json:"when"`
	Page  string     `yaml:"page" json:"page"`
	Alert AlertMode  `yaml:"alert" json:"alert,omitempty"`
}

// matches reports whether every condition holds at the local moment.
func (t *Trigger) matches(at time.Time, values Values) bool {
	if len(t.When) == 0 {
		return false
	}
	for i := range t.When {
		if !t.When[i].matches(at, values) {
			return false
		}
	}
	return true
}

// Conditions is the condition list of a trigger: `when` accepts one condition
// or a list.
type Conditions []Condition

// UnmarshalYAML accepts one condition map or a list of them.
func (c *Conditions) UnmarshalYAML(v *yaml.Node) error {
	switch v.Kind {
	case yaml.MappingNode:
		var one Condition
		if err := one.UnmarshalYAML(v); err != nil {
			return err
		}
		*c = Conditions{one}
		return nil
	case yaml.SequenceNode:
		return v.Decode((*[]Condition)(c))
	}
	return fmt.Errorf("line %d: expected a condition or a list of conditions", v.Line)
}

// UnmarshalJSON accepts one condition object or a list of them.
func (c *Conditions) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '{' {
		var one Condition
		if err := one.UnmarshalJSON(b); err != nil {
			return err
		}
		*c = Conditions{one}
		return nil
	}
	var list []Condition
	if err := json.Unmarshal(b, &list); err != nil {
		return fmt.Errorf("expected a condition or a list of conditions")
	}
	*c = list
	return nil
}

// The built-in values of a plan. They need no plugin: the plan reads them from
// the moment. weekday takes keys, time and date take from and to (ADR 0158).
const (
	// BuiltinWeekday is the local weekday with the keys mon … sun.
	BuiltinWeekday = "weekday"
	// BuiltinTime is the local time of day.
	BuiltinTime = "time"
	// BuiltinDate is the local calendar day.
	BuiltinDate = "date"
)

// ConditionKind names what a condition tests.
type ConditionKind string

const (
	// ConditionValue asks an enum value for one of its keys.
	ConditionValue ConditionKind = "value"
	// ConditionTime matches a window of the day.
	ConditionTime ConditionKind = "time"
	// ConditionDates matches a range of local days.
	ConditionDates ConditionKind = "dates"
)

// Condition is one thing that must be true at a moment (ADR 0158). Its value
// names what it tests: an enum value answers one of the keys in is,
// paperboy/time takes a window of the day in from and to, and paperboy/date
// takes local days in from and to.
type Condition struct {
	Value    string   `yaml:"value" json:"value"`
	Is       []string `yaml:"is,omitempty" json:"is,omitempty"`
	From     *Clock   `yaml:"-" json:"-"`
	To       *Clock   `yaml:"-" json:"-"`
	DateFrom *Date    `yaml:"-" json:"-"`
	DateTo   *Date    `yaml:"-" json:"-"`
}

// Kind reports what the condition tests, from its value.
func (c Condition) Kind() ConditionKind {
	switch c.Value {
	case BuiltinTime:
		return ConditionTime
	case BuiltinDate:
		return ConditionDates
	}
	return ConditionValue
}

// Matches reports whether the condition holds at the local moment and the
// current plan values.
func (c Condition) Matches(at time.Time, values Values) bool { return c.matches(at, values) }

func (c Condition) matches(at time.Time, values Values) bool {
	switch c.Kind() {
	case ConditionDates:
		return c.dateMatches(at)
	case ConditionTime:
		return c.timeMatches(at)
	}
	key := values[c.Value]
	if c.Value == BuiltinWeekday {
		key = WeekdayKey(at)
	}
	for _, want := range c.Is {
		if key == want {
			return true
		}
	}
	return false
}

// timeMatches reports whether the local moment belongs to the window. from is
// inclusive and to exclusive; a later to than from crosses midnight and
// belongs to its start day.
func (c Condition) timeMatches(at time.Time) bool {
	if c.From == nil || c.To == nil {
		return false
	}
	minute := at.Hour()*60 + at.Minute()
	from, to := int(*c.From), int(*c.To)
	if from < to {
		return minute >= from && minute < to
	}
	return minute >= from || minute < to
}

// dateMatches reports whether the local day is inside [from, to). Without to
// the window is the single day of from.
func (c Condition) dateMatches(at time.Time) bool {
	if c.DateFrom == nil {
		return false
	}
	from, err := time.ParseInLocation("2006-01-02", string(*c.DateFrom), at.Location())
	if err != nil {
		return false
	}
	today := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, at.Location())
	if today.Before(from) {
		return false
	}
	if c.DateTo == nil {
		return today.Equal(from)
	}
	to, err := time.ParseInLocation("2006-01-02", string(*c.DateTo), at.Location())
	if err != nil {
		return false
	}
	return today.Before(to)
}

// validate rejects a condition whose value does not match its fields.
func (c Condition) validate() error {
	if c.Value != BuiltinWeekday && c.Value != BuiltinTime && c.Value != BuiltinDate && !validRef(c.Value) {
		return fmt.Errorf("value %q: expected plugin/value, weekday, time or date", c.Value)
	}
	switch c.Kind() {
	case ConditionTime:
		if len(c.Is) > 0 || c.DateFrom != nil || c.DateTo != nil {
			return fmt.Errorf("%s takes from and to, not is", BuiltinTime)
		}
		if (c.From == nil) != (c.To == nil) {
			return fmt.Errorf("from and to must be given together")
		}
		if c.From == nil {
			return fmt.Errorf("from and to are missing")
		}
		if *c.From == *c.To {
			return fmt.Errorf("from and to must differ")
		}
	case ConditionDates:
		if len(c.Is) > 0 || c.From != nil || c.To != nil {
			return fmt.Errorf("%s takes from and to, not is", BuiltinDate)
		}
		if c.DateFrom == nil {
			return fmt.Errorf("from is missing")
		}
		if err := c.DateFrom.check(); err != nil {
			return err
		}
		if c.DateTo != nil {
			if err := c.DateTo.check(); err != nil {
				return err
			}
			if string(*c.DateTo) <= string(*c.DateFrom) {
				return fmt.Errorf("from and to must be in order")
			}
		}
	default:
		if len(c.Is) == 0 {
			return fmt.Errorf("%s: is missing", c.Value)
		}
		if c.From != nil || c.To != nil || c.DateFrom != nil || c.DateTo != nil {
			return fmt.Errorf("%s is a value and takes is, not from or to", c.Value)
		}
		if c.Value == BuiltinWeekday {
			for _, key := range c.Is {
				if !weekdayKey(key) {
					return fmt.Errorf("%s: unknown weekday %q; expected mon … sun", BuiltinWeekday, key)
				}
			}
		}
	}
	return nil
}

// UnmarshalYAML reads a condition. The value decides whether from and to are
// times of day or dates, so they are read after the loop.
func (c *Condition) UnmarshalYAML(v *yaml.Node) error {
	if v.Kind == yaml.ScalarNode {
		var ref string
		if err := v.Decode(&ref); err == nil {
			return fmt.Errorf("line %d: %q is no longer a condition; write { value: plugin/value, is: [key] }, { value: time, from: \"06:00\", to: \"09:00\" } or { value: date, from: \"2026-12-24\" } (ADR 0158)",
				v.Line, ref)
		}
	}
	if v.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: expected a condition as a mapping", v.Line)
	}
	*c = Condition{}
	var from, to string
	for i := 0; i+1 < len(v.Content); i += 2 {
		key, val := v.Content[i], v.Content[i+1]
		switch key.Value {
		case "value":
			if err := val.Decode(&c.Value); err != nil {
				return fmt.Errorf("line %d: value: expected a name", val.Line)
			}
		case "is":
			switch val.Kind {
			case yaml.ScalarNode:
				var one string
				if err := val.Decode(&one); err != nil {
					return fmt.Errorf("line %d: is: expected a key or a list of keys", val.Line)
				}
				c.Is = []string{one}
			case yaml.SequenceNode:
				if err := val.Decode(&c.Is); err != nil {
					return fmt.Errorf("line %d: is: expected a key or a list of keys", val.Line)
				}
			default:
				return fmt.Errorf("line %d: is: expected a key or a list of keys", val.Line)
			}
		case "from":
			if val.Kind != yaml.ScalarNode {
				return fmt.Errorf("line %d: from: expected a time or a date", val.Line)
			}
			from = val.Value
		case "to":
			if val.Kind != yaml.ScalarNode {
				return fmt.Errorf("line %d: to: expected a time or a date", val.Line)
			}
			to = val.Value
		default:
			return fmt.Errorf("line %d: field %s not found in type plan.Condition", key.Line, key.Value)
		}
	}
	return c.parseRange(from, to)
}

// parseRange turns the written from and to into times of day or dates.
func (c *Condition) parseRange(from, to string) error {
	switch c.Kind() {
	case ConditionTime:
		if from != "" {
			clock, err := parseClock(from)
			if err != nil {
				return err
			}
			c.From = &clock
		}
		if to != "" {
			clock, err := parseClock(to)
			if err != nil {
				return err
			}
			c.To = &clock
		}
	case ConditionDates:
		if from != "" {
			d := Date(from)
			if err := d.check(); err != nil {
				return err
			}
			c.DateFrom = &d
		}
		if to != "" {
			d := Date(to)
			if err := d.check(); err != nil {
				return err
			}
			c.DateTo = &d
		}
	default:
		if from != "" || to != "" {
			return fmt.Errorf("%s is a value and takes is, not from or to", c.Value)
		}
	}
	return nil
}

// MarshalYAML writes the fields of the condition's form.
func (c Condition) MarshalYAML() (any, error) {
	n := newMap()
	n.Style = yaml.FlowStyle
	add(n, "value", str(c.Value))
	switch c.Kind() {
	case ConditionTime:
		add(n, "from", quoted(clockString(*c.From)))
		add(n, "to", quoted(clockString(*c.To)))
	case ConditionDates:
		add(n, "from", quoted(string(*c.DateFrom)))
		if c.DateTo != nil {
			add(n, "to", quoted(string(*c.DateTo)))
		}
	default:
		if len(c.Is) == 1 {
			add(n, "is", str(c.Is[0]))
		} else {
			is := newSeq(true)
			for _, key := range c.Is {
				is.Content = append(is.Content, str(key))
			}
			add(n, "is", is)
		}
	}
	return n, nil
}

// UnmarshalJSON accepts the same shape as the YAML decoder, rejects unknown
// fields and rejects the old string form with the same hint.
func (c *Condition) UnmarshalJSON(b []byte) error {
	if len(bytes.TrimSpace(b)) > 0 && bytes.TrimSpace(b)[0] == '"' {
		var ref string
		_ = json.Unmarshal(b, &ref)
		return fmt.Errorf("%q is no longer a condition; write a value, a time window or dates (ADR 0158)", ref)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*c = Condition{}
	for key := range raw {
		switch key {
		case "value", "is", "from", "to":
		default:
			return fmt.Errorf("field %s not found in type plan.Condition", key)
		}
	}
	if v, ok := raw["value"]; ok {
		if err := json.Unmarshal(v, &c.Value); err != nil {
			return fmt.Errorf("value: expected a name")
		}
	}
	if is, ok := raw["is"]; ok && len(is) > 0 {
		var one string
		if err := json.Unmarshal(is, &one); err == nil {
			c.Is = []string{one}
		} else if err := json.Unmarshal(is, &c.Is); err != nil {
			return fmt.Errorf("is: expected a key or a list of keys")
		}
	}
	var from, to string
	if v, ok := raw["from"]; ok {
		if err := json.Unmarshal(v, &from); err != nil {
			return fmt.Errorf("from: expected a time or a date")
		}
	}
	if v, ok := raw["to"]; ok {
		if err := json.Unmarshal(v, &to); err != nil {
			return fmt.Errorf("to: expected a time or a date")
		}
	}
	return c.parseRange(from, to)
}

// MarshalJSON writes the fields of the condition's form.
func (c Condition) MarshalJSON() ([]byte, error) {
	switch c.Kind() {
	case ConditionTime:
		return json.Marshal(struct {
			Value string `json:"value"`
			From  string `json:"from"`
			To    string `json:"to"`
		}{c.Value, clockString(*c.From), clockString(*c.To)})
	case ConditionDates:
		out := struct {
			Value string `json:"value"`
			From  string `json:"from"`
			To    string `json:"to,omitempty"`
		}{Value: c.Value, From: string(*c.DateFrom)}
		if c.DateTo != nil {
			out.To = string(*c.DateTo)
		}
		return json.Marshal(out)
	}
	return json.Marshal(struct {
		Value string `json:"value"`
		Is    any    `json:"is"`
	}{c.Value, c.isValue()})
}

func (c Condition) isValue() any {
	if len(c.Is) == 1 {
		return c.Is[0]
	}
	return c.Is
}

// WeekdayKey is the key of a weekday as weekday answers it.
func WeekdayKey(at time.Time) string { return weekdays[int(at.Weekday())%7] }

var weekdays = [...]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// weekdayKey reports whether key is one of the weekday keys.
func weekdayKey(key string) bool {
	for _, k := range weekdays {
		if k == key {
			return true
		}
	}
	return false
}

// Values is the evaluated plan values keyed by "plugin/value" (ADR 0127). A
// missing key means the value answered nothing or failed, so its condition
// does not match.
type Values map[string]string

// Parse reads a plan and validates it. Time windows refer to loc; nil means
// the system timezone.
func Parse(r io.Reader, loc *time.Location) (*Plan, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var p Plan
	if err := strictyaml.Decode(raw, &p); err != nil {
		return nil, err
	}
	if loc == nil {
		loc = time.Local
	}
	p.loc = loc
	if err := p.validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// Mode is the refresh mode with its default.
func (p *Plan) Mode() RefreshMode {
	if p.OnRefresh == "" {
		return RefreshCurrent
	}
	return p.OnRefresh
}

// Effective returns the page order a device sees while triggerPage is active:
// the visible pages, plus the trigger page at its declared place. An empty
// triggerPage gives the plain rotation.
func (p *Plan) Effective(triggerPage string) []string {
	out := make([]string, 0, len(p.Pages))
	for _, pg := range p.Pages {
		if pg.Hidden && pg.Name != triggerPage {
			continue
		}
		out = append(out, pg.Name)
	}
	return out
}

// Interval returns the refresh interval at now, or 0 when the device's own
// refresh applies.
func (p *Plan) Interval(now time.Time) time.Duration {
	m := now.In(p.zone()).Hour()*60 + now.In(p.zone()).Minute()
	for _, w := range p.Refresh.Windows {
		if w.contains(m) {
			return time.Duration(w.Interval)
		}
	}
	return time.Duration(p.Refresh.Default)
}

// UntilNextChange returns the time until the next refresh window edge or
// trigger edge, so a sleeping device wakes up when its interval or its page
// changes. 0 means no edge exists within eight days.
func (p *Plan) UntilNextChange(now time.Time) time.Duration {
	loc := p.zone()
	local := now.In(loc)
	base := local.Truncate(time.Minute)
	var best time.Duration
	consider := func(at time.Time) {
		if !at.After(base) {
			return
		}
		if dur := at.Sub(local); dur > 0 && (best == 0 || dur < best) {
			best = dur
		}
	}
	// Refresh windows have no weekdays.
	for _, w := range p.Refresh.Windows {
		for day := 0; day <= 1; day++ {
			consider(atClock(local.AddDate(0, 0, day), w.From))
			consider(atClock(local.AddDate(0, 0, day), w.To))
		}
	}
	for _, tr := range p.Triggers {
		for _, c := range tr.When {
			switch c.Kind() {
			case ConditionTime:
				for day := 0; day <= 1; day++ {
					consider(atClock(local.AddDate(0, 0, day), *c.From))
					consider(atClock(local.AddDate(0, 0, day), *c.To))
				}
			case ConditionDates:
				// A date range wakes the device at its start and its end.
				if c.DateFrom == nil {
					continue
				}
				start, err := time.ParseInLocation("2006-01-02", string(*c.DateFrom), loc)
				if err != nil {
					continue
				}
				consider(start)
				end := start.AddDate(0, 0, 1)
				if c.DateTo != nil {
					if to, err := time.ParseInLocation("2006-01-02", string(*c.DateTo), loc); err == nil {
						end = to
					}
				}
				consider(end)
			case ConditionValue:
				if c.Value == BuiltinWeekday {
					// The weekday changes at local midnight (issue #460).
					consider(atClock(local.AddDate(0, 0, 1), 0))
				}
			}
		}
	}
	if best > 8*24*time.Hour {
		return 0
	}
	return best
}

func atClock(day time.Time, c Clock) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), int(c)/60, int(c)%60, 0, 0, day.Location())
}

// Matching returns the triggers that match at and the current values, in file
// order. The first one whose page fits the display type wins.
func (p *Plan) Matching(at time.Time, values Values) []Trigger {
	local := at.In(p.zone())
	var out []Trigger
	for i := range p.Triggers {
		if p.Triggers[i].matches(local, values) {
			out = append(out, p.Triggers[i])
		}
	}
	return out
}

func (p *Plan) zone() *time.Location {
	if p.loc == nil {
		return time.Local
	}
	return p.loc
}

// contains reports whether the minute of day belongs to the window.
func (w Window) contains(minute int) bool {
	from, to := int(w.From), int(w.To)
	if from < to {
		return minute >= from && minute < to
	}
	return minute >= from || minute < to
}

func (p *Plan) validate() error {
	if len(p.Rules) > 0 || p.Fallback != "" {
		return fmt.Errorf("rules and fallback are gone: write an ordered pages list, a refresh schedule and triggers with when, page and alert (ADR 0158)")
	}
	if len(p.Pages) == 0 {
		return fmt.Errorf("pages missing")
	}
	names := map[string]bool{}
	visible := false
	for i, pg := range p.Pages {
		if !validName(pg.Name) {
			return fmt.Errorf("page %d: name %q: only a-z, 0-9 and -", i+1, pg.Name)
		}
		if names[pg.Name] {
			return fmt.Errorf("duplicate page %q", pg.Name)
		}
		names[pg.Name] = true
		if !pg.Hidden {
			visible = true
		}
	}
	if !visible {
		return fmt.Errorf("every page is hidden; at least one page must be in the rotation")
	}
	switch p.Mode() {
	case RefreshCurrent, RefreshFirst, RefreshCycle:
	default:
		return fmt.Errorf("on_refresh %q: expected cycle, first or current", p.OnRefresh)
	}
	if d := time.Duration(p.Refresh.Default); d != 0 && d < time.Second {
		return fmt.Errorf("refresh.default must be at least 1s")
	}
	for i, w := range p.Refresh.Windows {
		if w.From == w.To {
			return fmt.Errorf("refresh.windows[%d]: from and to must differ", i+1)
		}
		if d := time.Duration(w.Interval); d < time.Second {
			return fmt.Errorf("refresh.windows[%d]: interval must be at least 1s", i+1)
		}
	}
	if err := p.Refresh.validate(); err != nil {
		return err
	}
	for i := range p.Triggers {
		if err := p.Triggers[i].validate(names); err != nil {
			return fmt.Errorf("trigger %d: %w", i+1, err)
		}
	}
	if p.Actions != nil {
		return fmt.Errorf("actions was removed: actions move to the widget cells (ADR 0162); remove it")
	}
	return nil
}

// validate rejects overlapping windows: a schedule must be unambiguous.
func (r *Refresh) validate() error {
	for i := range r.Windows {
		for j := i + 1; j < len(r.Windows); j++ {
			if r.Windows[i].overlaps(r.Windows[j]) {
				return fmt.Errorf("refresh.windows[%d] and refresh.windows[%d] overlap", i+1, j+1)
			}
		}
	}
	return nil
}

// overlaps reports whether two windows share a minute. Both windows are
// minute sets on a day, so a window that crosses midnight wraps.
func (w Window) overlaps(other Window) bool {
	for minute := 0; minute < 24*60; minute++ {
		if w.contains(minute) && other.contains(minute) {
			return true
		}
	}
	return false
}

func (t *Trigger) validate(pages map[string]bool) error {
	if t.Page == "" {
		return fmt.Errorf("page missing")
	}
	if !pages[t.Page] {
		return fmt.Errorf("page %q is not in pages; a trigger only shows a page of the plan", t.Page)
	}
	if len(t.When) == 0 {
		return fmt.Errorf("when missing: a trigger needs at least one condition")
	}
	switch t.Alert {
	case AlertNever, AlertOnce, AlertRefresh:
	default:
		return fmt.Errorf("alert %q: expected never, once or refresh", t.Alert)
	}
	refs := map[string]bool{}
	for i := range t.When {
		cond := &t.When[i]
		if err := cond.validate(); err != nil {
			return fmt.Errorf("condition %d: %w", i+1, err)
		}
		if cond.Kind() != ConditionValue {
			continue
		}
		if refs[cond.Value] {
			return fmt.Errorf("duplicate condition %q", cond.Value)
		}
		refs[cond.Value] = true
	}
	return nil
}

func validName(s string) bool { return ident.Valid(s) }

// validRef reports whether s is "plugin/name" with valid names, as used by an
// action or a condition.
func validRef(s string) bool {
	plugin, name, ok := strings.Cut(s, "/")
	return ok && validName(plugin) && validName(name)
}

// checkKeys rejects a mapping key that the type does not know. A custom
// unmarshaler does not inherit the strict decoding of the file parser.
func checkKeys(v *yaml.Node, want ...string) error {
	for i := 0; i+1 < len(v.Content); i += 2 {
		key := v.Content[i].Value
		known := false
		for _, w := range want {
			if key == w {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("line %d: field %s not found in type plan", v.Content[i].Line, key)
		}
	}
	return nil
}
