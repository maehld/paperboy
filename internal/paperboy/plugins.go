package paperboy

import (
	"context"
	"net/http"
	"sort"

	"github.com/maehld/paperboy/internal/builtin"
	"github.com/maehld/paperboy/internal/page"
	"github.com/maehld/paperboy/internal/widget"
	"github.com/maehld/paperboy/pkg/plugin"
)

// configurePlugins hands the plugin section and the global language and
// timezone to the widget host; changed plugins restart on their next use.
func (s *Server) configurePlugins(cfg *Config) {
	s.widgets.Configure(cfg.pluginSpecs(), widget.Env{Language: cfg.Language, Timezone: cfg.timezoneName()})
}

// widgetSource lets a page ask the plugins for its widgets. The time spent
// here is the part of a page render that waits for plugins (ADR 0104).
func (s *Server) widgetSource() page.WidgetSource {
	return func(b page.Batch) []page.Result {
		done := s.stats.Start("render.widgets")
		defer done()
		return s.widgets.Fetch(context.Background(), b)
	}
}

type pluginInfo struct {
	Name        string              `json:"name"`
	Builtin     bool                `json:"builtin,omitempty"`
	Program     string              `json:"program,omitempty"` // resolved program, ADR 0060
	Version     string              `json:"version,omitempty"` // downloaded version, empty for a hand-built one
	Description *plugin.Description `json:"description,omitempty"`
	Error       string              `json:"error,omitempty"`
}

// handlePlugins lists the plugins with the widgets they offer, the data an
// editor builds pages from.
func (s *Server) handlePlugins(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	names := []string{builtin.Name}
	for _, p := range cfg.Plugins {
		names = append(names, p.Name)
	}
	sort.Strings(names[1:])
	specs := cfg.pluginSpecs()
	list := []pluginInfo{}
	for _, name := range names {
		info := pluginInfo{Name: name, Builtin: name == builtin.Name}
		if spec, ok := specs[name]; ok {
			info.Version = spec.Version
			if len(spec.Command) > 0 {
				info.Program = spec.Command[0]
			}
		}
		d, err := s.widgets.Describe(r.Context(), name)
		if err != nil {
			info.Error = err.Error()
		} else {
			info.Description = d
		}
		list = append(list, info)
	}
	writeJSON(w, http.StatusOK, map[string]any{"plugins": list})
}
