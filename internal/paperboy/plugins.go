package paperboy

import (
	"context"
	"net/http"
	"time"

	"github.com/maehld/paperboy/internal/fragment"
	"github.com/maehld/paperboy/internal/page"
	"github.com/maehld/paperboy/pkg/plugin"
)

// fragmentSource lets a page render ask the configured plugins for its image
// fragments (spike: plugin fragments).
func (s *Server) fragmentSource(cfg *Config, now time.Time) page.FragmentSource {
	if len(cfg.Plugins) == 0 {
		return nil
	}
	specs := cfg.pluginSpecs()
	env := fragment.Env{Language: cfg.Language, Timezone: cfg.Timezone.String(), Now: now}
	return func(reqs []page.FragmentRequest, gray bool) []page.Fragment {
		return s.plugins.Render(context.Background(), specs, reqs, gray, env)
	}
}

type pluginInfo struct {
	Name        string              `json:"name"`
	Description *plugin.Description `json:"description,omitempty"`
	Error       string              `json:"error,omitempty"`
}

// handlePlugins lists the configured plugins with the images they offer, the
// data an editor would build pages from.
func (s *Server) handlePlugins(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	list := []pluginInfo{}
	for _, p := range cfg.Plugins {
		name := p.Name
		spec := cfg.pluginSpecs()[name]
		info := pluginInfo{Name: name}
		d, err := fragment.Describe(r.Context(), spec)
		if err != nil {
			info.Error = err.Error()
		} else {
			info.Description = d
		}
		list = append(list, info)
	}
	writeJSON(w, http.StatusOK, map[string]any{"plugins": list})
}
