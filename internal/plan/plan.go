// Package plan parses display plans and decides which pages a device shows
// (ADR 0025). A plan is a list of rules; the first matching rule wins, and a
// rule combines a time window with a page list. Plans know nothing about
// display types: they only name pages, the template is chosen per device. The
// timezone comes from the service configuration (ADR 0033).
package plan

import (
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

func (c *Clock) UnmarshalYAML(v *yaml.Node) error {
	var s string
	if err := v.Decode(&s); err != nil {
		return fmt.Errorf("expected a time of day as \"HH:MM\"")
	}
	if len(s) != 5 || s[2] != ':' {
		return fmt.Errorf("expected a time of day as \"HH:MM\", got %q", s)
	}
	t, err := time.Parse("15:04", s)
	if err != nil {
		return fmt.Errorf("expected a time of day as \"HH:MM\", got %q", s)
	}
	*c = Clock(t.Hour()*60 + t.Minute())
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

// Plan is one file under plans/.
type Plan struct {
	Rules    []Rule       `yaml:"rules"`
	Fallback string       `yaml:"fallback"`
	Actions  []PlanAction `yaml:"actions"` // offered by the device menu (ADR 0057)

	loc *time.Location
}

// Rule shows Pages while When matches.
type Rule struct {
	Name    string   `yaml:"name"`
	When    When     `yaml:"when"`
	Pages   []string `yaml:"pages"`
	Rotate  bool     `yaml:"rotate"`
	Refresh Duration `yaml:"refresh"`
	// Actions belong to the plan, not to a rule. The field only exists to
	// reject the key with a hint instead of "field not found".
	Actions []any `yaml:"actions"`
}

// PlanAction is one action a plan offers to every device that uses it
// (ADR 0057). Action is "plugin/action"; Args are passed to the plugin
// unchanged. ID is optional and becomes mandatory when Action appears more
// than once. Title overrides the plugin's own title in the menu.
type PlanAction struct {
	Action string         `yaml:"action"`
	Args   map[string]any `yaml:"args"`
	ID     string         `yaml:"id"`
	Title  string         `yaml:"title"`
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

// When is a time window. Days apply to the day the window starts, so a window
// across midnight belongs to its start day. All conditions are combined.
type When struct {
	From *Clock `yaml:"from"`
	To   *Clock `yaml:"to"`
	Days []Day  `yaml:"days"`
	// Data conditions are specified in ADR 0025 but not implemented yet.
	// Rejecting them keeps operators from relying on something that is ignored.
	Data []any `yaml:"data"`
}

// Selection is the evaluated rule.
type Selection struct {
	Rule    string
	Pages   []string
	Rotate  bool
	Refresh time.Duration // 0 = use the device interval
	Actions []PlanAction  // the plan's actions, the same for every rule and page
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
// false when neither matches.
func (p *Plan) Select(now time.Time) (Selection, bool) {
	local := now.In(p.loc)
	for i := range p.Rules {
		if p.Rules[i].When.matches(local) {
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

// Matches reports whether the time window of rule i matches now, without the
// fallback. It lets a viewer tell a matching rule from the fallback.
func (p *Plan) Matches(i int, now time.Time) bool {
	if i < 0 || i >= len(p.Rules) {
		return false
	}
	return p.Rules[i].When.matches(now.In(p.loc))
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

func (w *When) matches(t time.Time) bool {
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
	return Selection{Rule: r.Name, Pages: r.Pages, Rotate: r.Rotate, Refresh: time.Duration(r.Refresh)}
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
		if !validAction(a.Action) {
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
	if len(r.Actions) > 0 {
		return fmt.Errorf("actions belong to the plan, not to a rule (ADR 0057)")
	}
	w := &r.When
	if len(w.Data) > 0 {
		return fmt.Errorf("when.data: data conditions are not implemented yet")
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

// validAction reports whether s is "plugin/action" with valid names.
func validAction(s string) bool {
	plugin, action, ok := strings.Cut(s, "/")
	return ok && validName(plugin) && validName(action)
}
