package paperboy

import (
	"context"
	"fmt"
	"log/slog"
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
	Deadline time.Time      `json:"deadline,omitzero"`  // last tap
	Screen   string         `json:"screen,omitempty"`   // hash of the served action view
	Resolved bool           `json:"resolved,omitempty"` // the plugin state is filled in
	Options  []actionOption `json:"options,omitempty"`
}

// close drops all action state.
func (a *actionState) close() { *a = actionState{} }

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

// widgetActionArgs fills the arguments the widget also declares and finds the
// one remaining required enum argument. The page carries no action arguments;
// a widget names its subject, and the plugin names the choices (ADR 0162).
func widgetActionArgs(spec plugin.ActionSpec, widgetArgs map[string]any) (fixed map[string]any, choiceName string, choices []any, reason string) {
	fixed = map[string]any{}
	for name := range spec.Args {
		if v, ok := widgetArgs[name]; ok {
			fixed[name] = v
		}
	}
	for name, arg := range spec.Args {
		if _, ok := fixed[name]; ok {
			continue
		}
		if !arg.Required || len(arg.Enum) == 0 || arg.Type == "list" {
			continue
		}
		if choiceName != "" {
			return nil, "", nil, "more than one choice argument"
		}
		choiceName, choices = name, arg.Enum
	}
	return fixed, choiceName, choices, ""
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
		if cell.Action == "" {
			continue
		}
		name := cell.Plugin()
		r := rects[i]
		job := actionJob{cell: i, plugin: name, rect: [4]int{r.Min.X, r.Min.Y, r.Max.X, r.Max.Y}}
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
		spec, known := actionSpec(dd, cell.Action)
		if !known {
			job.disabled = fmt.Sprintf("unknown action %s/%s", name, cell.Action)
			jobs = append(jobs, job)
			continue
		}
		fixed, choice, choices, reason := widgetActionArgs(spec, cell.Args)
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
				calls = append(calls, plugin.ActionCall{Action: j.spec.Name, Args: args})
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
				title = job.spec.Name
			}
			if title == "" {
				title = job.disabled
			}
			out = append(out, actionOption{Plugin: job.plugin, Action: job.spec.Name, Title: title,
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
			out = append(out, actionOption{Plugin: job.plugin, Action: job.spec.Name, Args: args, Title: title,
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

// actionMode folds the input of one wake into the action mode (ADR 0162). It
// runs under the state lock: a short home opens the mode, next or previous
// leaves it and pages, a tap on the served action view runs one option, and a
// tap elsewhere leaves the mode.
func (x *syncSession) actionMode(st *deviceState, holds []int, cfg *Config) actionState {
	action := st.Action
	x.touch = cfg.touch(x.d, st.TouchReported)
	if x.req.Reason == "powercycle" || x.critical || x.textDisplay || x.d.Plan == "" ||
		x.d.Actions == actionsDeny || !x.touch {
		action.close()
		return action
	}
	paging := pagePressed(x.buttons)
	if paging {
		action.close()
	}
	// The deadline wake closes the mode without input.
	if x.req.Reason == "action" && len(x.req.Events) == 0 {
		action.close()
	}
	if action.Open && !x.now.Before(action.Deadline) {
		action.close()
	}
	if !paging && shortHome(x.buttons, holds) {
		if action.Open {
			// Ask the plugin again: a mode another device changed is shown
			// on the next home press.
			action.Deadline = x.now.Add(actionLifetime)
			action.Resolved = false
		} else if st.LastPage != "" {
			action = actionState{Open: true, Page: st.LastPage, Deadline: x.now.Add(actionLifetime)}
		}
	}
	for _, e := range x.req.Events {
		if e.Type != "touch" || !action.Open {
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
