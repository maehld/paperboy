// Package plan parses display plans and decides which pages a device shows
// (ADR 0025). A plan is a list of rules; the first matching rule wins, and a
// rule combines a time window with a page list. Plans know nothing about
// display types: they only name pages, the template is chosen per device. The
// timezone comes from the service configuration (ADR 0033).
package plan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Duration accepts duration strings such as "5m" in YAML.
type Duration time.Duration

func (d *Duration) UnmarshalYAML(v *yaml.Node) error {
	var s string
	if err := v.Decode(&s); err != nil {
		return fmt.Errorf("expected duration as string, e.g. \"5m\"")
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

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

// Day is a weekday, written as a short name.
type Day time.Weekday

// Both English and German short names are accepted; plans are written by
// operators, the rest of the software stays English.
var dayNames = map[string]time.Weekday{
	"mon": time.Monday, "monday": time.Monday, "mo": time.Monday, "montag": time.Monday,
	"tue": time.Tuesday, "tuesday": time.Tuesday, "tu": time.Tuesday, "di": time.Tuesday, "dienstag": time.Tuesday,
	"wed": time.Wednesday, "wednesday": time.Wednesday, "we": time.Wednesday, "mi": time.Wednesday, "mittwoch": time.Wednesday,
	"thu": time.Thursday, "thursday": time.Thursday, "th": time.Thursday, "do": time.Thursday, "donnerstag": time.Thursday,
	"fri": time.Friday, "friday": time.Friday, "fr": time.Friday, "freitag": time.Friday,
	"sat": time.Saturday, "saturday": time.Saturday, "sa": time.Saturday, "samstag": time.Saturday,
	"sun": time.Sunday, "sunday": time.Sunday, "su": time.Sunday, "so": time.Sunday, "sonntag": time.Sunday,
}

func (d *Day) UnmarshalYAML(v *yaml.Node) error {
	var s string
	if err := v.Decode(&s); err != nil {
		return fmt.Errorf("expected a weekday such as \"mo\" or \"mon\"")
	}
	wd, ok := dayNames[strings.ToLower(s)]
	if !ok {
		return fmt.Errorf("unknown weekday %q", s)
	}
	*d = Day(wd)
	return nil
}

// Plan is one file under plans/. The JSON tags carry the same model to the
// editor API (ADR 0066).
type Plan struct {
	Rules    []Rule       `yaml:"rules" json:"rules"`
	Fallback string       `yaml:"fallback" json:"fallback,omitempty"`
	Actions  []PlanAction `yaml:"actions" json:"actions,omitempty"` // offered by the device menu (ADR 0057)

	loc *time.Location
}

// RefreshMode decides what a timed refresh does with the page cursor
// (ADR 0130). A rule change always starts at the first page.
type RefreshMode string

const (
	// RefreshCurrent keeps the page.
	RefreshCurrent RefreshMode = "current"
	// RefreshFirst shows the first page of the rule.
	RefreshFirst RefreshMode = "first"
	// RefreshCycle advances to the next page of the rule.
	RefreshCycle RefreshMode = "cycle"
)

// AlertMode says when a rule asks the device to alert (ADR 0131). The device
// decides what an alert is: a beep, a blink or both. The empty mode means
// never; a rule without the key stays quiet.
type AlertMode string

const (
	// AlertNever keeps the device quiet.
	AlertNever AlertMode = ""
	// AlertOnce alerts when the rule becomes active.
	AlertOnce AlertMode = "once"
	// AlertRefresh alerts when the rule becomes active and on every timed
	// refresh while it applies, so a low glucose value reminds at every
	// refresh without an alert on a button press.
	AlertRefresh AlertMode = "refresh"
)

// UnmarshalYAML accepts once and refresh, and never as the explicit way to
// stay quiet. Anything else, including the true of an early sketch, gets a
// hint that names the three modes.
func (b *AlertMode) UnmarshalYAML(v *yaml.Node) error {
	if v.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: alert: expected never, once or refresh", v.Line)
	}
	switch s := strings.ToLower(v.Value); s {
	case "never", "":
		*b = AlertNever
	case "once":
		*b = AlertOnce
	case "refresh":
		*b = AlertRefresh
	default:
		return fmt.Errorf("line %d: alert %q: expected never, once or refresh", v.Line, v.Value)
	}
	return nil
}

// Rule shows Pages while When matches.
type Rule struct {
	Name      string      `yaml:"name" json:"name"`
	When      When        `yaml:"when" json:"when"`
	Pages     []string    `yaml:"pages" json:"pages"`
	OnRefresh RefreshMode `yaml:"on_refresh" json:"on_refresh,omitempty"`
	Refresh   Duration    `yaml:"refresh" json:"refresh,omitempty"`
	// Alert asks the device to alert when the rule takes over or on every
	// timed refresh (ADR 0131).
	Alert AlertMode `yaml:"alert" json:"alert,omitempty"`
	// Rotate is the old name of on_refresh: cycle (ADR 0025, 0130). Parse and
	// the editor API keep accepting it; Encode writes on_refresh.
	Rotate bool `yaml:"rotate" json:"rotate,omitempty"`
	// Actions belong to the plan, not to a rule. The field only exists to
	// reject the key with a hint instead of "field not found".
	Actions []any `yaml:"actions" json:"-"`
}

// Mode returns the effective refresh mode. An empty mode means current; the
// old rotate: true means cycle (ADR 0130).
func (r Rule) Mode() RefreshMode {
	if r.OnRefresh != "" {
		return r.OnRefresh
	}
	if r.Rotate {
		return RefreshCycle
	}
	return RefreshCurrent
}

// PlanAction is one action a plan offers to every device that uses it
// (ADR 0057). Action is "plugin/action"; Args are passed to the plugin
// unchanged. ID is optional and becomes mandatory when Action appears more
// than once. Title overrides the plugin's own title in the menu.
type PlanAction struct {
	Action string         `yaml:"action" json:"action"`
	Args   map[string]any `yaml:"args" json:"args,omitempty"`
	ID     string         `yaml:"id" json:"id,omitempty"`
	Title  string         `yaml:"title" json:"title,omitempty"`
}

// Key is the plan-local identity of the action: its ID when set, otherwise
// plugin/action. Selection state uses it, so reordering the list or changing
// Args or Title keeps the identity (ADR 0057).
func (a PlanAction) Key() string {
	if a.ID != "" {
		return a.ID
	}
	return a.Action
}

// UnmarshalYAML accepts the short form "- plugin/action" next to the full
// mapping. Unknown keys are rejected by hand because the node decoder does not
// know the plan's strict mode.
func (a *PlanAction) UnmarshalYAML(v *yaml.Node) error {
	if v.Kind == yaml.ScalarNode {
		return v.Decode(&a.Action)
	}
	if v.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: expected an action as \"plugin/action\" or a mapping", v.Line)
	}
	for i := 0; i+1 < len(v.Content); i += 2 {
		switch v.Content[i].Value {
		case "action", "args", "id", "title":
		default:
			return fmt.Errorf("line %d: field %s not found in type plan.PlanAction", v.Content[i].Line, v.Content[i].Value)
		}
	}
	type plain PlanAction
	return v.Decode((*plain)(a))
}

// When is a time window and the conditions a rule needs. All conditions are
// combined, with the time window as AND.
type When struct {
	From *Clock `yaml:"from" json:"from,omitempty"`
	To   *Clock `yaml:"to" json:"to,omitempty"`
	Days []Day  `yaml:"days" json:"days,omitempty"`
	// Conditions are value conditions: a plugin value must answer one of the
	// listed keys (ADR 0127). Every entry must match; the keys within one
	// entry are alternatives. A missing value, a failing plugin and an
	// unknown key all count as no match, so a rule that warns about missing
	// data never fires.
	Conditions []Condition `yaml:"conditions" json:"conditions,omitempty"`
	// Data conditions from ADR 0025 pointed into a shared data document that
	// no longer exists (ADR 0049). Rejecting them keeps operators from relying
	// on something that is ignored.
	Data []any `yaml:"data" json:"data,omitempty"`
}

// Condition names one value and the keys that match it. A text cell may show
// the key with its own label, but a plan condition has no label: the file and
// the view keep the key (ADR 0127).
type Condition struct {
	Value string   `yaml:"value" json:"value"`
	Is    []string `yaml:"is" json:"is"`
}

// UnmarshalYAML accepts a mapping with value and is. The old string form names
// a condition and is rejected with a hint that shows the new shape.
func (c *Condition) UnmarshalYAML(v *yaml.Node) error {
	if v.Kind == yaml.ScalarNode {
		var ref string
		if err := v.Decode(&ref); err == nil {
			return fmt.Errorf("line %d: %q is no longer a condition; write { value: plugin/value, is: [key] } (ADR 0127)",
				v.Line, ref)
		}
	}
	if v.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: expected a condition as { value: plugin/value, is: [key] }", v.Line)
	}
	c.Value, c.Is = "", nil
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
		default:
			return fmt.Errorf("line %d: field %s not found in type plan.Condition", key.Line, key.Value)
		}
	}
	return nil
}

// MarshalYAML writes one key as a scalar and several as a list.
func (c Condition) MarshalYAML() (any, error) {
	return struct {
		Value string `yaml:"value"`
		Is    any    `yaml:"is"`
	}{c.Value, c.isValue()}, nil
}

// UnmarshalJSON accepts the same shape as the YAML decoder, rejects unknown
// fields and rejects the old string form with the same hint.
func (c *Condition) UnmarshalJSON(b []byte) error {
	if len(bytes.TrimSpace(b)) > 0 && bytes.TrimSpace(b)[0] == '"' {
		var ref string
		_ = json.Unmarshal(b, &ref)
		return fmt.Errorf("%q is no longer a condition; write {\"value\": \"plugin/value\", \"is\": [\"key\"]} (ADR 0127)", ref)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	c.Value, c.Is = "", nil
	for key := range raw {
		switch key {
		case "value", "is":
		default:
			return fmt.Errorf("field %s not found in type plan.Condition", key)
		}
	}
	if v, ok := raw["value"]; ok {
		if err := json.Unmarshal(v, &c.Value); err != nil {
			return fmt.Errorf("value: expected a name")
		}
	}
	is, ok := raw["is"]
	if !ok || len(is) == 0 {
		return nil
	}
	var one string
	if err := json.Unmarshal(is, &one); err == nil {
		c.Is = []string{one}
		return nil
	}
	if err := json.Unmarshal(is, &c.Is); err != nil {
		return fmt.Errorf("is: expected a key or a list of keys")
	}
	return nil
}

// MarshalJSON writes one key as a scalar and several as a list.
func (c Condition) MarshalJSON() ([]byte, error) {
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

// Values is the evaluated plan values keyed by "plugin/value" (ADR 0127). A
// missing key means the value answered nothing or failed, so its condition
// does not match.
type Values map[string]string

// Selection is the evaluated rule.
type Selection struct {
	Rule      string
	Pages     []string
	OnRefresh RefreshMode
	Refresh   time.Duration // 0 = use the device interval
	Alert     AlertMode
	Actions   []PlanAction // the plan's actions, the same for every rule and page
}

// Parse reads a plan and validates it. Time windows refer to loc; nil means
// the system timezone.
func Parse(r io.Reader, loc *time.Location) (*Plan, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	var p Plan
	if err := dec.Decode(&p); err != nil {
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

// Select returns the first matching rule, otherwise the fallback rule. ok is
// false when neither matches. values holds the current plan values keyed by
// "plugin/value"; nil means no value answered.
func (p *Plan) Select(now time.Time, values Values) (Selection, bool) {
	local := now.In(p.loc)
	for i := range p.Rules {
		if p.Rules[i].When.matches(local, values) {
			return p.selectRule(i), true
		}
	}
	if p.Fallback != "" {
		for i := range p.Rules {
			if p.Rules[i].Name == p.Fallback {
				return p.selectRule(i), true
			}
		}
	}
	return Selection{}, false
}

func (p *Plan) selectRule(i int) Selection {
	s := p.Rules[i].selection()
	s.Actions = p.Actions
	return s
}

// Matches reports whether rule i matches now, without the fallback. It lets a
// viewer tell a matching rule from the fallback. Time windows only; values
// are evaluated as well.
func (p *Plan) Matches(i int, now time.Time, values Values) bool {
	if i < 0 || i >= len(p.Rules) {
		return false
	}
	return p.Rules[i].When.matches(now.In(p.loc), values)
}

// UntilNextBoundary returns the time until the next window starts or ends, so
// a sleeping device wakes up when its rule changes. 0 means no window exists.
func (p *Plan) UntilNextBoundary(now time.Time) time.Duration {
	local := now.In(p.loc)
	var best time.Duration
	consider := func(t time.Time) {
		d := t.Sub(local)
		if d <= 0 || (best != 0 && d >= best) {
			return
		}
		best = d
	}
	y, m, d := local.Date()
	for i := range p.Rules {
		w := p.Rules[i].When
		if w.From == nil || w.To == nil {
			continue
		}
		from, to := int(*w.From), int(*w.To)
		for off := 0; off <= 8; off++ {
			day := time.Date(y, m, d+off, 0, 0, 0, 0, p.loc)
			if w.dayAllowed(day.Weekday()) {
				consider(atClock(day, from))
			}
			start := day.Weekday()
			if from > to { // the window started the day before
				start = prevDay(start)
			}
			if w.dayAllowed(start) {
				consider(atClock(day, to))
			}
		}
	}
	if best > 8*24*time.Hour {
		return 0
	}
	return best
}

func atClock(day time.Time, minutes int) time.Time {
	y, m, d := day.Date()
	return time.Date(y, m, d, minutes/60, minutes%60, 0, 0, day.Location())
}

func prevDay(d time.Weekday) time.Weekday { return (d + 6) % 7 }

func (w *When) dayAllowed(d time.Weekday) bool {
	if len(w.Days) == 0 {
		return true
	}
	for _, allowed := range w.Days {
		if time.Weekday(allowed) == d {
			return true
		}
	}
	return false
}

func (w *When) matches(t time.Time, values Values) bool {
	for _, cond := range w.Conditions {
		key, ok := values[cond.Value]
		if !ok {
			return false
		}
		found := false
		for _, want := range cond.Is {
			if key == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	m := t.Hour()*60 + t.Minute()
	if w.From == nil || w.To == nil {
		return w.dayAllowed(t.Weekday())
	}
	from, to := int(*w.From), int(*w.To)
	if from < to {
		return m >= from && m < to && w.dayAllowed(t.Weekday())
	}
	if m >= from {
		return w.dayAllowed(t.Weekday())
	}
	if m < to {
		return w.dayAllowed(prevDay(t.Weekday()))
	}
	return false
}

func (r *Rule) selection() Selection {
	return Selection{Rule: r.Name, Pages: r.Pages, OnRefresh: r.Mode(), Refresh: time.Duration(r.Refresh), Alert: r.Alert}
}

func (p *Plan) validate() error {
	if len(p.Rules) == 0 {
		return fmt.Errorf("rules missing")
	}
	seen := map[string]bool{}
	for i := range p.Rules {
		r := &p.Rules[i]
		if !validName(r.Name) {
			return fmt.Errorf("rule %d: name %q: only a-z, 0-9 and -", i, r.Name)
		}
		if seen[r.Name] {
			return fmt.Errorf("duplicate rule name %q", r.Name)
		}
		seen[r.Name] = true
		if err := r.validate(); err != nil {
			return fmt.Errorf("rule %s: %w", r.Name, err)
		}
	}
	if p.Fallback != "" && !seen[p.Fallback] {
		return fmt.Errorf("fallback %q: no such rule", p.Fallback)
	}
	if err := p.validateActions(); err != nil {
		return err
	}
	return nil
}

// validateActions checks the plan's action list (ADR 0057). Plugins, actions
// and args are checked by the server, which knows them; here only the shape,
// the identity and the duplicates are checked.
func (p *Plan) validateActions() error {
	count := map[string]int{}
	for i, a := range p.Actions {
		if a.Action == "" {
			return fmt.Errorf("actions[%d]: action missing", i)
		}
		if !validRef(a.Action) {
			return fmt.Errorf("actions[%d]: action %q: expected plugin/action", i, a.Action)
		}
		if a.ID != "" && !validName(a.ID) {
			return fmt.Errorf("actions[%d]: id %q: only a-z, 0-9 and -", i, a.ID)
		}
		count[a.Action]++
	}
	ids := map[string]bool{}
	seen := map[string]bool{}
	for i, a := range p.Actions {
		if count[a.Action] > 1 && a.ID == "" {
			return fmt.Errorf("actions[%d]: id required because %s appears more than once", i, a.Action)
		}
		key := a.Key()
		if ids[key] {
			return fmt.Errorf("actions[%d]: duplicate id %q", i, key)
		}
		ids[key] = true
		// json.Marshal writes map keys in sorted order, so the key tells
		// equal arguments from different ones.
		args, err := json.Marshal(a.Args)
		if err != nil {
			return fmt.Errorf("actions[%d]: args: %w", i, err)
		}
		dup := a.Action + " " + string(args)
		if seen[dup] {
			return fmt.Errorf("actions[%d]: duplicate action and args (%s)", i, a.Action)
		}
		seen[dup] = true
	}
	return nil
}

func (r *Rule) validate() error {
	if len(r.Pages) == 0 {
		return fmt.Errorf("pages missing")
	}
	pages := map[string]bool{}
	for _, name := range r.Pages {
		if !validName(name) {
			return fmt.Errorf("page %q: only a-z, 0-9 and -", name)
		}
		if pages[name] {
			return fmt.Errorf("duplicate page %q", name)
		}
		pages[name] = true
	}
	// Image displays never go below one minute; Paperboy applies the display
	// type's floor. Text displays may poll every second (ADR 0045).
	if r.Refresh != 0 && time.Duration(r.Refresh) < time.Second {
		return fmt.Errorf("refresh must be at least 1s")
	}
	switch r.Mode() {
	case RefreshCurrent, RefreshFirst, RefreshCycle:
	default:
		return fmt.Errorf("on_refresh %q: expected cycle, first or current", r.OnRefresh)
	}
	if r.Rotate && r.OnRefresh != "" && r.OnRefresh != RefreshCycle {
		return fmt.Errorf("rotate is the old name of on_refresh: cycle; remove rotate (ADR 0130)")
	}
	switch r.Alert {
	case AlertNever, AlertOnce, AlertRefresh:
	default:
		return fmt.Errorf("alert %q: expected never, once or refresh", r.Alert)
	}
	if len(r.Actions) > 0 {
		return fmt.Errorf("actions belong to the plan, not to a rule (ADR 0057)")
	}
	w := &r.When
	if len(w.Data) > 0 {
		return fmt.Errorf("when.data: there is no shared data document any more; name a value and its keys in when.conditions (ADR 0127)")
	}
	refs := map[string]bool{}
	for _, cond := range w.Conditions {
		if !validRef(cond.Value) {
			return fmt.Errorf("condition value %q: expected plugin/value", cond.Value)
		}
		if len(cond.Is) == 0 {
			return fmt.Errorf("condition %s: is missing", cond.Value)
		}
		if refs[cond.Value] {
			return fmt.Errorf("duplicate condition %q", cond.Value)
		}
		refs[cond.Value] = true
	}
	if (w.From == nil) != (w.To == nil) {
		return fmt.Errorf("when: from and to must be given together")
	}
	if w.From != nil && *w.From == *w.To {
		return fmt.Errorf("when: from and to must differ")
	}
	return nil
}

func validName(s string) bool {
	if s == "" || len(s) > 40 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// validRef reports whether s is "plugin/name" with valid names, as used by an
// action or a condition.
func validRef(s string) bool {
	plugin, name, ok := strings.Cut(s, "/")
	return ok && validName(plugin) && validName(name)
}
