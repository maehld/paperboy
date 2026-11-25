package paperboy

import (
	"strings"
	"time"

	"github.com/maehld/paperboy/server/internal/builtin"
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
// (ADR 0126). It is the earliest expected contact around the trigger each
// device is on: the pages that fit its display type plus the plan's
// conditions. A device that is late or never seen adds nothing, so a
// powered-off device cannot keep a plugin polling.
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
		if !ok || st.LastSeen.IsZero() {
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
		// The pages of a plan are static, so every page is warm, hidden or
		// not, and a trigger page never arrives cold (ADR 0158).
		needed := map[string]bool{}
		for _, pg := range p.Pages {
			if s.fits(cfg, d.Profile, pg.Name) != nil {
				continue
			}
			t, err := s.pages.get(cfg.Pages, pg.Name, d.Profile)
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
			for _, part := range t.HeaderParts() {
				if plugin := part.Plugin(); plugin != "" && plugin != builtin.Name {
					needed[plugin] = true
				}
			}
		}
		// A trigger decides on every sync, so its values must answer even
		// while the device shows another page.
		for _, tr := range p.Triggers {
			for _, cond := range tr.When {
				plugin, _, ok := strings.Cut(cond.Value, "/")
				if ok && plugin != "" && plugin != builtin.Name {
					needed[plugin] = true
				}
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
