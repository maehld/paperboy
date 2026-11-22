package paperboy

import (
	"context"
	"log/slog"
	"strings"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/server/internal/page"
	"github.com/maehld/paperboy/server/internal/plan"
)

// planValues asks the plugins for the values a plan's triggers name (ADR 0127).
// It runs outside the state lock, because a plugin request is I/O. The answers
// become the plan.Values that select the trigger. A failing plugin, an unknown
// value and an empty answer are omitted, so missing data never shows a trigger
// page; the plan continues with its next trigger.
func (s *Server) planValues(ctx context.Context, cfg *Config, planName, profile string, d *Device) plan.Values {
	if planName == "" {
		return nil
	}
	p, _ := s.plans.get(cfg.Plans, planName, cfg.Timezone)
	if p == nil {
		return nil
	}
	return s.evaluatePlanValues(ctx, cfg, p, profile, d)
}

// evaluatePlanValues asks every value a plan's conditions name once and keys
// the answers by "plugin/value". It uses the normal value cache, so a text
// display that polls every second does not ask again every time. A nil result
// means the plan names none or no value answered.
func (s *Server) evaluatePlanValues(ctx context.Context, cfg *Config, p *plan.Plan, profile string, d *Device) plan.Values {
	var refs []string
	seen := map[string]bool{}
	for _, tr := range p.Triggers {
		for _, cond := range tr.When {
			if cond.Value == plan.BuiltinWeekday || cond.Value == plan.BuiltinTime || cond.Value == plan.BuiltinDate {
				continue // the plan evaluates them from the moment
			}
			if seen[cond.Value] {
				continue
			}
			seen[cond.Value] = true
			refs = append(refs, cond.Value)
		}
	}
	if len(refs) == 0 {
		return nil
	}
	requests := make([]page.ValueRequest, 0, len(refs))
	for _, ref := range refs {
		pluginName, name, ok := strings.Cut(ref, "/")
		if !ok {
			continue
		}
		requests = append(requests, page.ValueRequest{Plugin: pluginName, Name: name})
	}
	info := plugin.PageInfo{Number: 1, Count: 1}
	if p, ok := cfg.Profiles[profile]; ok {
		info.Width, info.Height = p.Width, p.Height
		if !p.text() {
			info.PPI = p.PPI
		}
	}
	batch := page.ValueBatch{Page: info, Values: requests}
	if d != nil {
		batch.Device = pluginDevice(d)
	}
	results := s.widgets.FetchValues(ctx, batch)
	values := plan.Values{}
	for i, req := range requests {
		if i >= len(results) {
			break
		}
		r := results[i]
		if r.Err != nil {
			slog.Debug("plan value unavailable", "plugin", req.Plugin, "value", req.Name, "error", r.Err)
			continue
		}
		if r.Value == nil || r.Value.Type() != plugin.ValueEnum || r.Value.Enum == nil || r.Spec.Type != plugin.ValueEnum {
			continue
		}
		values[req.Plugin+"/"+req.Name] = *r.Value.Enum
	}
	if len(values) == 0 {
		return nil
	}
	return values
}
