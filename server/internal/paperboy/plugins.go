package paperboy

import (
	"context"
	"net/http"
	"sort"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/server/internal/builtin"
	"github.com/maehld/paperboy/server/internal/page"
	"github.com/maehld/paperboy/server/internal/widget"
)

// configurePlugins hands the plugin section and the global language and
// timezone to the widget host; changed plugins restart on their next use.
func (s *Server) configurePlugins(cfg *Config) {
	s.widgets.Configure(cfg.pluginSpecs(), widget.Env{Language: cfg.Language, Timezone: cfg.timezoneName()})
}

// widgetSource lets a page ask the plugins for its widgets. The time spent
// here is the part of a page render that waits for plugins (ADR 0104). The
// request context is deliberately not used: a render that outlives the
// connection still fills the cache, so the next sync pays nothing for it.
func (s *Server) widgetSource() page.WidgetSource {
	return func(b page.Batch) []page.Result {
		done := s.stats.Start("render.widgets")
		defer done()
		return s.widgets.Fetch(context.Background(), b)
	}
}

// valueSource lets a text page ask the plugins for the values it lays out
// (ADR 0111). It waits for plugins like widgetSource.
func (s *Server) valueSource() page.ValueSource {
	return func(b page.ValueBatch) []page.Result {
		done := s.stats.Start("render.values")
		defer done()
		return s.widgets.FetchValues(context.Background(), b)
	}
}

// pluginsAnswer is the body of GET /api/plugins.
type pluginsAnswer struct {
	Plugins []pluginInfo `json:"plugins"`
}

type pluginInfo struct {
	Name        string              `json:"name"`
	Builtin     bool                `json:"builtin,omitempty"`
	Program     string              `json:"program,omitempty"` // resolved program, ADR 0060
	Version     string              `json:"version,omitempty"` // downloaded version, empty for a hand-built one
	Description *plugin.Description `json:"description,omitempty"`
	Error       string              `json:"error,omitempty"`
}

// deviceArgWidgets names the built-in widgets whose arguments take device ids.
// The editor offers the configured ids for them (ADR 0123).
var deviceArgWidgets = map[string][]string{
	"battery":       {"device"},
	"device-status": {"devices"},
	"reading":       {"device"},
}

// deviceChoices fills the enum of the built-in device arguments with the
// configured ids. An empty value is labelled as the device that shows the page.
// The built-in Describe stays enum-free, so a page that names a removed device
// still renders (ADR 0115, 0123).
func deviceChoices(d *plugin.Description, cfg *Config) {
	ids := make([]string, 0, len(cfg.Devices))
	for _, dev := range cfg.Devices {
		ids = append(ids, dev.ID)
	}
	sort.Strings(ids)
	enum := make([]any, len(ids))
	for i, id := range ids {
		enum[i] = id
	}
	labels := map[string]string{"": texts{language: cfg.Language}.deviceThatShows()}
	for i := range d.Widgets {
		w := &d.Widgets[i]
		for _, name := range deviceArgWidgets[w.Widget] {
			arg, ok := w.Args[name]
			if !ok {
				continue
			}
			arg.Enum = enum
			arg.EnumLabels = labels
			w.Args[name] = arg
		}
	}
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
			if name == builtin.Name {
				deviceChoices(d, cfg)
			}
			info.Description = d
		}
		list = append(list, info)
	}
	writeJSON(w, http.StatusOK, pluginsAnswer{Plugins: list})
}
