package paperboy

import (
	"context"
	"log/slog"
	"strings"

	"github.com/maehld/paperboy/internal/plan"
)

// planConditions asks the plugins a plan's rules name for the state of their
// conditions (ADR 0103). It runs outside the state lock, because a plugin
// request is I/O. A failing plugin, an unknown condition and a plugin that is
// not configured leave their conditions inactive, so missing data never
// selects an alert page; the plan falls through to its next rule instead.
func (s *Server) planConditions(ctx context.Context, cfg *Config, planName string) plan.Conditions {
	if planName == "" {
		return nil
	}
	p, _ := s.plans.get(cfg.Plans, planName, cfg.Timezone)
	if p == nil {
		return nil
	}
	return s.evaluateConditions(ctx, p)
}

// evaluateConditions asks every plugin named in the plan's conditions once and
// returns the answers keyed by "plugin/condition". A nil result means the plan
// uses none.
func (s *Server) evaluateConditions(ctx context.Context, p *plan.Plan) plan.Conditions {
	plugins := map[string]bool{}
	for _, r := range p.Rules {
		for _, ref := range r.When.Conditions {
			if plugin, _, ok := strings.Cut(ref, "/"); ok {
				plugins[plugin] = true
			}
		}
	}
	if len(plugins) == 0 {
		return nil
	}
	conds := plan.Conditions{}
	for name := range plugins {
		active, err := s.widgets.Conditions(ctx, name)
		if err != nil {
			slog.Debug("plugin conditions unavailable", "plugin", name, "error", err)
			continue
		}
		for condition, on := range active {
			conds[name+"/"+condition] = on
		}
	}
	return conds
}
