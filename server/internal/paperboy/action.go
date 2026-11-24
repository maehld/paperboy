package paperboy

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/server/internal/page"
)

// The widget action mode (ADR 0162). A page cell names one action of its
// plugin; the plugin names the choices. A short home opens the mode, its
// options replace the cell, and a tap runs one option.

const (
	// actionLifetime is how long the action view waits for a tap.
	actionLifetime = 20 * time.Second
	// actionBudget bounds the plugin calls of one mode wake and one action.
	actionBudget = 5 * time.Second
)

// actionOption is one tile of the action view: a concrete call of the
// plugin's action, its label and the state the plugin answered.
type actionOption struct {
	Plugin   string         `json:"plugin"`
	Action   string         `json:"action"`
	Args     map[string]any `json:"args,omitempty"`
	Title    string         `json:"title,omitempty"`
	Disabled string         `json:"disabled,omitempty"`
	Cell     int            `json:"cell"`
	// Rect is the tile in page pixels: x0, y0, x1, y1. The drawing sets it.
	Rect [4]int `json:"rect"`
}

// actionState is the persisted action mode of one device.
type actionState struct {
	Open     bool           `json:"open,omitempty"`
	Page     string         `json:"page,omitempty"`     // page the options belong to
	Deadline time.Time      `json:"deadline,omitzero"`  // last input
	Screen   string         `json:"screen,omitempty"`   // hash of the served action view
	Resolved bool           `json:"resolved,omitempty"` // the plugin state is filled in
	Selected int            `json:"selected,omitempty"` // index into Options; -1 = no runnable option
	Options  []actionOption `json:"options,omitempty"`
}

// close drops all action state.
func (a *actionState) close() { *a = actionState{} }

// choose selects the first runnable option in reading order. A selected option
// that is still runnable stays, so a repaint keeps the selection (ADR 0163).
func (a *actionState) choose() {
	if a.Selected >= 0 && a.Selected < len(a.Options) && a.Options[a.Selected].Disabled == "" {
		return
	}
	a.Selected = -1
	for i := range a.Options {
		if a.Options[i].Disabled == "" {
			a.Selected = i
			return
		}
	}
}

// moveSelection moves the selection by step options and wraps. Disabled
// options are skipped, so one press can pass several of them (ADR 0163).
func (a *actionState) moveSelection(step int) {
	a.choose()
	if a.Selected < 0 {
		return
	}
	n := len(a.Options)
	for i := 1; i <= n; i++ {
		idx := ((a.Selected+step*i)%n + n) % n
		if a.Options[idx].Disabled == "" {
			a.Selected = idx
			return
		}
	}
}

// chosen returns the selected option when it can run.
func (a actionState) chosen() *actionOption {
	if a.Selected < 0 || a.Selected >= len(a.Options) || a.Options[a.Selected].Disabled != "" {
		return nil
	}
	return &a.Options[a.Selected]
}

// hit returns the option at a page point. A disabled tile is returned too, so
// the caller can leave the mode open.
func (a actionState) hit(x, y int) *actionOption {
	for i := range a.Options {
		r := a.Options[i].Rect
		if x >= r[0] && y >= r[1] && x < r[2] && y < r[3] {
			return &a.Options[i]
		}
	}
	return nil
}

// actionJob is one actionable cell while its options are resolved.
type actionJob struct {
	cell     int
	plugin   string
	action   string
	spec     plugin.ActionSpec
	args     []map[string]any // one concrete call per option
	labels   []string         // the enum label of each option; empty for a trigger
	rect     [4]int
	disabled string
}

// actionSpec finds one action in a plugin's catalog.
func actionSpec(d *plugin.Description, name string) (plugin.ActionSpec, bool) {
	for _, a := range d.Actions {
		if a.Name == name {
			return a, true
		}
	}
	return plugin.ActionSpec{}, false
}

// actionArgs separates the action's fixed arguments from the one required
// enum that becomes the choice. The action's args come only from the action
// block; the widget's args never fill them (ADR 0164).
func actionArgs(spec plugin.ActionSpec, given map[string]any) (fixed map[string]any, choiceName string, choices []any, reason string) {
	fixed = map[string]any{}
	names := make([]string, 0, len(spec.Args))
	for name := range spec.Args {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		arg := spec.Args[name]
		value, set := given[name]
		if set && !emptyArg(value) {
			fixed[name] = value
			continue
		}
		if !arg.Required {
			continue
		}
		if len(arg.Enum) == 0 || arg.Type == "list" {
			return nil, "", nil, fmt.Sprintf("required argument %q is missing", name)
		}
		if choiceName != "" {
			return nil, "", nil, "more than one choice argument"
		}
		choiceName, choices = name, arg.Enum
	}
	return fixed, choiceName, choices, ""
}

// emptyArg reports whether an argument value counts as not set. An empty
// required enum is the choice; an empty required string is missing.
func emptyArg(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	}
	return false
}

// resolveWidgetActions builds the tiles of every actionable cell. A plugin
// that is down or does not know the action keeps its cell visible with the
// reason, so a broken plugin never costs the device its page (ADR 0162).
func (s *Server) resolveWidgetActions(ctx context.Context, cfg *Config, d *Device, tmpl *page.Template, now time.Time) []actionOption {
	prof := cfg.Profiles[d.Profile]
	rects := tmpl.Rects(prof.Width, prof.Height)
	desc := map[string]*plugin.Description{}
	descErr := map[string]string{}
	var jobs []actionJob
	for i, cell := range tmpl.Cells {
		if cell.Action == nil || cell.Action.Name == "" {
			continue
		}
		name := cell.Plugin()
		r := rects[i]
		job := actionJob{cell: i, plugin: name, action: cell.Action.Name, rect: [4]int{r.Min.X, r.Min.Y, r.Max.X, r.Max.Y}}
		dd, ok := desc[name]
		if !ok {
			if reason, failed := descErr[name]; failed {
				job.disabled = reason
				jobs = append(jobs, job)
				continue
			}
			got, err := s.widgets.Describe(ctx, name)
			if err != nil {
				descErr[name] = shortError(err)
				job.disabled = descErr[name]
				jobs = append(jobs, job)
				continue
			}
			dd = got
			desc[name] = got
		}
		spec, known := actionSpec(dd, cell.Action.Name)
		if !known {
			job.disabled = fmt.Sprintf("unknown action %s/%s", name, cell.Action.Name)
			jobs = append(jobs, job)
			continue
		}
		fixed, choice, choices, reason := actionArgs(spec, cell.Action.Args)
		if reason != "" {
			job.spec, job.disabled = spec, reason
			jobs = append(jobs, job)
			continue
		}
		job.spec = spec
		if choice != "" && len(choices) == 0 {
			job.disabled = "the action offers no choices"
			jobs = append(jobs, job)
			continue
		}
		if choice == "" {
			job.args = []map[string]any{fixed}
			job.labels = []string{""}
		} else {
			for _, value := range choices {
				args := cloneArgs(fixed)
				args[choice] = value
				job.args = append(job.args, args)
				job.labels = append(job.labels, enumLabel(spec.Args[choice], value))
			}
		}
		jobs = append(jobs, job)
	}

	// One actions request per plugin for all its calls.
	byPlugin := map[string][]int{}
	for i, j := range jobs {
		if j.disabled == "" && len(j.args) > 0 {
			byPlugin[j.plugin] = append(byPlugin[j.plugin], i)
		}
	}
	states := map[int][]plugin.ActionState{}
	for name, indexes := range byPlugin {
		var calls []plugin.ActionCall
		for _, i := range indexes {
			j := &jobs[i]
			for _, args := range j.args {
				calls = append(calls, plugin.ActionCall{Action: j.action, Args: args})
			}
		}
		req := plugin.Request{Version: plugin.Version, Language: cfg.Language, Timezone: cfg.timezoneName(),
			Now: now, Page: plugin.PageInfo{Width: prof.Width, Height: prof.Height, PPI: prof.PPI},
			Device: &plugin.DeviceInfo{ID: d.ID, Hardware: s.hardwareOf(d)}, Actions: calls}
		got, err := s.widgets.Actions(ctx, name, req)
		if err != nil {
			reason := shortError(err)
			for _, i := range indexes {
				jobs[i].disabled, jobs[i].args = reason, nil
			}
			continue
		}
		cursor := 0
		for _, i := range indexes {
			j := &jobs[i]
			states[i] = got[cursor : cursor+len(j.args)]
			cursor += len(j.args)
		}
	}

	out := make([]actionOption, 0, len(jobs))
	for i, job := range jobs {
		if job.disabled != "" || len(job.args) == 0 {
			title := job.spec.Title
			if title == "" {
				title = job.action
			}
			if title == "" {
				title = job.disabled
			}
			out = append(out, actionOption{Plugin: job.plugin, Action: job.action, Title: title,
				Disabled: job.disabled, Cell: job.cell, Rect: job.rect})
			continue
		}
		got := states[i]
		for n, args := range job.args {
			title := job.labels[n]
			if st := got[n]; st.Title != "" {
				title = st.Title
			}
			if title == "" {
				title = job.spec.Title
			}
			if title == "" {
				title = job.action
			}
			out = append(out, actionOption{Plugin: job.plugin, Action: job.action, Args: args, Title: title,
				Disabled: got[n].Disabled, Cell: job.cell, Rect: job.rect})
		}
	}
	return out
}

// enumLabel names one enum value; a plugin without labels keeps the key.
func enumLabel(arg plugin.Arg, value any) string {
	key := fmt.Sprint(value)
	if arg.EnumLabels != nil {
		if label := arg.EnumLabels[key]; label != "" {
			return label
		}
	}
	return key
}

func cloneArgs(args map[string]any) map[string]any {
	out := make(map[string]any, len(args)+1)
	for k, v := range args {
		out[k] = v
	}
	return out
}

// runWidgetAction runs the tapped option. The sync waits for the answer, so
// the page it draws next shows the new state (ADR 0162). A virtual display
// names the tap but never runs the plugin.
func (x *syncSession) runWidgetAction() {
	if x.invoke == nil {
		return
	}
	if x.opt.virtual {
		slog.Info("virtual display: action not run", "device", x.d.ID, "action", x.invoke.Plugin+"/"+x.invoke.Action)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), actionBudget)
	defer cancel()
	prof := x.cfg.Profiles[x.d.Profile]
	req := plugin.Request{Version: plugin.Version, Language: x.cfg.Language, Timezone: x.cfg.timezoneName(),
		Now: x.now, Page: plugin.PageInfo{Width: prof.Width, Height: prof.Height, PPI: prof.PPI},
		Device:  &plugin.DeviceInfo{ID: x.d.ID, Hardware: x.s.hardwareOf(x.d)},
		Actions: []plugin.ActionCall{{Action: x.invoke.Action, Args: x.invoke.Args}}}
	out, err := x.s.widgets.Invoke(ctx, x.invoke.Plugin, req)
	if err != nil {
		x.actionFailed, x.actionMessage = true, shortError(err)
		slog.Warn("action failed", "device", x.d.ID, "action", x.invoke.Plugin+"/"+x.invoke.Action, "error", err)
		return
	}
	if !out.OK {
		x.actionFailed, x.actionMessage = true, out.Message
		slog.Info("action failed", "device", x.d.ID, "action", x.invoke.Plugin+"/"+x.invoke.Action, "message", out.Message)
		return
	}
	slog.Info("action run", "device", x.d.ID, "action", x.invoke.Plugin+"/"+x.invoke.Action, "ok", true, "message", out.Message)
}

// actionMode folds the input of one wake into the action mode (ADR 0162,
// 0163). It runs under the state lock: a short home opens the mode, a tap on
// the served action view runs one option, and a tap elsewhere leaves the mode.
// A device with a digitiser keeps that path. A device without one selects with
// next and previous and runs the selection with a short home.
func (x *syncSession) actionMode(st *deviceState, holds []int, cfg *Config) actionState {
	action := st.Action
	x.touch = cfg.touch(x.d, st.TouchReported)
	if x.req.Reason == "powercycle" || x.critical || x.textDisplay || x.d.Plan == "" ||
		x.d.Actions == actionsDeny {
		action.close()
		return action
	}
	// The deadline wake closes the mode without input.
	if x.req.Reason == "action" && len(x.req.Events) == 0 {
		action.close()
	}
	if action.Open && !x.now.Before(action.Deadline) {
		action.close()
	}
	home, homeHeld := homePress(x.buttons, holds)
	step := selectionStep(x.buttons)
	if action.Open {
		switch {
		case x.touch:
			// The touch path stays as ADR 0162: a page key leaves the mode,
			// a second home asks the plugin again.
			if pagePressed(x.buttons) {
				action.close()
			} else if home && !homeHeld {
				action.Deadline = x.now.Add(actionLifetime)
				action.Resolved = false
			}
		case homeHeld:
			// A long home closes the key-operated mode (ADR 0163).
			action.close()
		case home:
			// A second short home runs the selection; without a runnable
			// option it only closes the mode.
			if opt := action.chosen(); opt != nil {
				chosen := *opt
				x.invoke = &chosen
			}
			action.close()
		case step != 0:
			action.moveSelection(step)
			action.Deadline = x.now.Add(actionLifetime)
		}
	} else if home && !homeHeld && st.LastPage != "" {
		action = actionState{Open: true, Page: st.LastPage, Selected: -1,
			Deadline: x.now.Add(actionLifetime)}
	}
	for _, e := range x.req.Events {
		if e.Type != "touch" || !action.Open || !x.touch {
			continue
		}
		if action.Screen == "" || x.req.Shown != action.Screen {
			// The panel does not show the action view: the tap is stale.
			action.close()
			break
		}
		opt := action.hit(e.X, e.Y)
		if opt == nil {
			action.close()
			break
		}
		if opt.Disabled != "" {
			continue // a disabled tile neither runs nor closes
		}
		chosen := *opt
		x.invoke = &chosen
		action.close()
		break
	}
	return action
}
