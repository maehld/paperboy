package paperboy

import (
	"strings"
	"time"

	"github.com/maehld/paperboy/internal/builtin"
	"github.com/maehld/paperboy/internal/plan"
)

// demandGrace is how long after the expected contact a device still counts as
// on schedule. It covers a fetch that starts before the contact and a slightly
// late sync. After that the demand clears until the device contacts again
// (ADR 0126).
const demandGrace = time.Minute

// minDemandGrace keeps a text display, which polls every second, from clearing
// and resending its schedule on one missed poll.
const minDemandGrace = 30 * time.Second

// pluginDemand names, per plugin, the next moment Paperboy needs its data
// (ADR 0126). It is the earliest expected contact around the rule each device
// is on: the pages that fit its display type plus the plan's conditions.
// Actions are resolved when the menu opens and add nothing. A device that is
// late or never seen adds nothing, so a powered-off device cannot keep a
// plugin polling.
func (s *Server) pluginDemand() map[string]time.Time {
	cfg := s.config()
	now := s.now()

	s.mu.Lock()
	states := make(map[string]deviceState, len(s.state))
	for key, st := range s.state {
		if st != nil {
			states[key] = *st
		}
	}
	s.mu.Unlock()

	out := map[string]time.Time{}
	for i := range cfg.Devices {
		d := &cfg.Devices[i]
		if d.Problem != "" || d.Plan == "" {
			continue
		}
		st, ok := states[d.stateKey()]
		if !ok || st.LastSeen.IsZero() || st.Rule == "" {
			continue
		}
		refresh := time.Duration(st.LastRefresh) * time.Second
		if refresh <= 0 {
			refresh = time.Duration(d.Refresh)
		}
		if refresh <= 0 {
			continue
		}
		next := st.LastSeen.Add(refresh)
		grace := min(refresh, demandGrace)
		if grace < minDemandGrace {
			grace = minDemandGrace
		}
		if now.After(next.Add(grace)) {
			continue // late: no demand until the device contacts again
		}
		p, _ := s.plans.get(cfg.Plans, d.Plan, cfg.Timezone)
		if p == nil {
			continue
		}
		rule := ruleNamed(p, st.Rule)
		if rule == nil {
			continue
		}

		needed := map[string]bool{}
		for _, name := range rule.Pages {
			if s.fits(cfg, d.Profile, name) != nil {
				continue
			}
			t, err := s.pages.get(cfg.Pages, name, d.Profile)
			if err != nil || t == nil {
				continue
			}
			for _, c := range t.Cells {
				if plugin := c.Plugin(); plugin != "" && plugin != builtin.Name {
					needed[plugin] = true
				}
				if plugin := c.ValuePlugin(); plugin != "" && plugin != builtin.Name {
					needed[plugin] = true
				}
			}
		}
		for _, cond := range rule.When.Conditions {
			plugin, _, ok := strings.Cut(cond.Value, "/")
			if ok && plugin != "" && plugin != builtin.Name {
				needed[plugin] = true
			}
		}
		for plugin := range needed {
			if old, ok := out[plugin]; !ok || next.Before(old) {
				out[plugin] = next
			}
		}
	}
	return out
}

// ruleNamed returns the rule a device last used, or nil when the plan changed.
func ruleNamed(p *plan.Plan, name string) *plan.Rule {
	for i := range p.Rules {
		if p.Rules[i].Name == name {
			return &p.Rules[i]
		}
	}
	return nil
}
