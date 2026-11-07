package paperboy

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/maehld/paperboy/internal/builtin"
	"github.com/maehld/paperboy/internal/page"
	"github.com/maehld/paperboy/pkg/plugin"
)

// defaultGrid is the reference grid of the widget preview: the spans plugins
// declare are cells of it, like on the example pages.
var defaultGrid = [2]int{6, 8}

// widgetQuery is one widget preview request, resolved against a display type.
type widgetQuery struct {
	profile string
	p       Profile
	req     page.WidgetRequest
}

// parseSize reads "3x2".
func parseSize(s string) ([2]int, error) {
	a, b, ok := strings.Cut(s, "x")
	x, err1 := strconv.Atoi(a)
	y, err2 := strconv.Atoi(b)
	if !ok || err1 != nil || err2 != nil || x < 1 || y < 1 || x > 80 || y > 80 {
		return [2]int{}, fmt.Errorf("%q: expected <columns>x<rows>, e.g. 3x2", s)
	}
	return [2]int{x, y}, nil
}

// spanPixels turns a span on the reference grid into the pixel size it has
// on a page, using the same division as the page composer.
func spanPixels(p Profile, span, grid [2]int) (int, int) {
	return p.Width * span[0] / grid[0], p.Height * span[1] / grid[1]
}

// widgetArgs reads the query keys arg.<name>; a value that is valid JSON
// (numbers, booleans, lists) keeps its type, everything else is a string.
func widgetArgs(q url.Values) map[string]any {
	args := map[string]any{}
	for k, vs := range q {
		name, ok := strings.CutPrefix(k, "arg.")
		if !ok || len(vs) == 0 {
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(vs[0]), &v); err != nil {
			v = vs[0]
		}
		args[name] = v
	}
	return args
}

// parseWidgetQuery resolves display, size and arguments. Image displays take
// span (default 3x2) on grid (default 6x8) or width and height in pixels;
// text displays take span in characters, by default the whole display.
func (s *Server) parseWidgetQuery(cfg *Config, pluginName, widgetName string, q url.Values) (widgetQuery, error) {
	var wq widgetQuery
	if !page.ValidName(pluginName) || !page.ValidName(widgetName) {
		return wq, errors.New("invalid plugin or widget name")
	}
	wq.profile = q.Get("display")
	p, ok := cfg.Profiles[wq.profile]
	if !ok {
		return wq, fmt.Errorf("unknown display type %q", wq.profile)
	}
	wq.p = p
	w, h := 0, 0
	switch {
	case p.text():
		w, h = p.Width, p.Height
		if v := q.Get("span"); v != "" {
			size, err := parseSize(v)
			if err != nil {
				return wq, fmt.Errorf("span %w", err)
			}
			w, h = min(size[0], p.Width), min(size[1], p.Height)
		}
	case q.Get("width") != "" || q.Get("height") != "":
		var err1, err2 error
		w, err1 = strconv.Atoi(q.Get("width"))
		h, err2 = strconv.Atoi(q.Get("height"))
		if err1 != nil || err2 != nil || w < 1 || h < 1 || w > p.Width || h > p.Height {
			return wq, fmt.Errorf("width and height must be within the display (%dx%d)", p.Width, p.Height)
		}
	default:
		span, grid := [2]int{3, 2}, defaultGrid
		var err error
		if v := q.Get("span"); v != "" {
			if span, err = parseSize(v); err != nil {
				return wq, fmt.Errorf("span %w", err)
			}
		}
		if v := q.Get("grid"); v != "" {
			if grid, err = parseSize(v); err != nil {
				return wq, fmt.Errorf("grid %w", err)
			}
		}
		if span[0] > grid[0] || span[1] > grid[1] {
			return wq, errors.New("span is larger than the grid")
		}
		w, h = spanPixels(p, span, grid)
	}
	wq.req = page.WidgetRequest{Plugin: pluginName, Widget: widgetName, Args: widgetArgs(q), Width: w, Height: h}
	if !p.text() {
		wq.req.Style = page.DefaultStyle(s.display(cfg, p))
	}
	return wq, nil
}

// fetchWidget asks the plugin for one widget, like a page with one cell.
func (s *Server) fetchWidget(ctx context.Context, cfg *Config, wq widgetQuery) page.Result {
	d := s.display(cfg, wq.p)
	levels := 2
	if d.Gray {
		levels = 4
	}
	b := page.Batch{Text: d.Text, Levels: levels, Charset: d.Charset, Widgets: []page.WidgetRequest{wq.req},
		Page: plugin.PageInfo{Title: wq.req.Plugin + "/" + wq.req.Widget, Number: 1, Count: 1, Width: d.Width, Height: d.Height}}
	if !d.Text {
		b.Page.PPI = d.PPI
	}
	if out := s.widgets.Fetch(ctx, b); len(out) == 1 {
		return out[0]
	}
	return page.Result{Err: errors.New("no result")}
}

// handleWidget renders one widget of a plugin on its own: a PNG for image
// displays, text/plain for text displays. A failing widget is answered with
// the same notice a page shows in its cell.
func (s *Server) handleWidget(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	wq, err := s.parseWidgetQuery(cfg, r.PathValue("plugin"), r.PathValue("widget"), r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	res := s.fetchWidget(r.Context(), cfg, wq)
	label := wq.req.Plugin + "/" + wq.req.Widget
	if res.Err != nil {
		w.Header().Set("X-Widget-Error", firstLine(res.Err.Error()))
	}
	w.Header().Set("Cache-Control", "no-store")
	if wq.p.text() {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(strings.Join(page.TextCell(res, wq.p.Charset, wq.req.Width, wq.req.Height), "\n") + "\n"))
		return
	}
	levels := 2
	if wq.p.Gray {
		levels = 4
	}
	writePNG(w, encodePNG(page.WidgetImage(res, wq.req.Width, wq.req.Height, levels, label)))
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// ---- widget gallery ----

//go:embed ui/plugins.html
var pluginsTemplate string

var pluginsTmpl = template.Must(template.New("plugins").Parse(pluginsTemplate))

type galleryDisplay struct {
	Name     string
	URL      string
	Selected bool
}

type galleryPreview struct {
	Span     string // "3x2", empty for text displays
	ImageURL string
	Width    int
	Height   int
	Text     []string
	Warning  string // e.g. below the minimum size
}

type galleryWidget struct {
	Name     string
	Title    string
	Kinds    string
	Args     []string
	Previews []galleryPreview
	Note     string // why there is no preview
}

type galleryPlugin struct {
	Name    string
	Title   string
	Builtin bool
	Config  []string
	Error   string
	Widgets []galleryWidget
}

type galleryPage struct {
	Version   string
	GoVersion string
	Display   string
	Text      bool
	Grid      string
	Displays  []galleryDisplay
	Plugins   []galleryPlugin
}

// handleGallery shows every widget of every plugin in the sizes the plugin
// declares, on one display type: a preview of what a page editor offers.
func (s *Server) handleGallery(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	q := r.URL.Query()
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	display := q.Get("display")
	if _, ok := cfg.Profiles[display]; !ok {
		display = ""
		for _, n := range names { // the first image display, else the first one
			if !cfg.Profiles[n].text() {
				display = n
				break
			}
		}
		if display == "" && len(names) > 0 {
			display = names[0]
		}
	}
	grid := defaultGrid
	if g, err := parseSize(q.Get("grid")); err == nil {
		grid = g
	}
	gridText := fmt.Sprintf("%dx%d", grid[0], grid[1])
	loc := cfg.Timezone
	if loc == nil {
		loc = time.UTC
	}
	gp := galleryPage{Version: versionText(build, loc), GoVersion: build.Go, Display: display, Grid: gridText}
	for _, n := range names {
		gp.Displays = append(gp.Displays, galleryDisplay{Name: n, Selected: n == display,
			URL: "/ui/plugins?" + url.Values{"display": {n}, "grid": {gridText}}.Encode()})
	}
	if display == "" {
		s.renderGallery(w, gp)
		return
	}
	p := cfg.Profiles[display]
	gp.Text = p.text()
	plugins := []string{builtin.Name}
	for _, pl := range cfg.Plugins {
		plugins = append(plugins, pl.Name)
	}
	sort.Strings(plugins[1:])
	for _, name := range plugins {
		gp.Plugins = append(gp.Plugins, s.galleryPlugin(r.Context(), cfg, name, display, p, grid))
	}
	s.renderGallery(w, gp)
}

func (s *Server) renderGallery(w http.ResponseWriter, gp galleryPage) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := pluginsTmpl.Execute(w, gp); err != nil {
		slog.Error("widget gallery rendering failed", "error", err)
	}
}

func argList(args map[string]plugin.Arg) []string {
	var out []string
	for name, a := range args {
		s := name + " (" + a.Type
		if a.Default != nil {
			s += fmt.Sprintf(", default %v", a.Default)
		}
		if a.Required {
			s += ", required"
		}
		s += ")"
		if a.Description != "" {
			s += ": " + a.Description
		}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func (s *Server) galleryPlugin(ctx context.Context, cfg *Config, name, display string, p Profile, grid [2]int) galleryPlugin {
	gp := galleryPlugin{Name: name, Builtin: name == builtin.Name}
	d, err := s.widgets.Describe(ctx, name)
	if err != nil {
		gp.Error = err.Error()
		return gp
	}
	gp.Title, gp.Config = d.Title, argList(d.Config)
	kind := plugin.KindImage
	if p.text() {
		kind = plugin.KindText
	}
	for _, wi := range d.Widgets {
		gw := galleryWidget{Name: wi.Widget, Title: wi.Title, Kinds: strings.Join(wi.Kinds, ", "), Args: argList(wi.Args)}
		if len(wi.Kinds) > 0 && !contains(wi.Kinds, kind) {
			gw.Note = "not available for " + kind + " displays"
			gp.Widgets = append(gp.Widgets, gw)
			continue
		}
		if p.text() {
			wq, _ := s.parseWidgetQuery(cfg, name, wi.Widget, url.Values{"display": {display}})
			gw.Previews = append(gw.Previews, galleryPreview{Text: page.TextCell(s.fetchWidget(ctx, cfg, wq), p.Charset, wq.req.Width, wq.req.Height)})
			gp.Widgets = append(gp.Widgets, gw)
			continue
		}
		sizes := wi.Sizes
		if len(sizes) == 0 {
			sizes = []plugin.Span{{Columns: 3, Rows: 2}}
		}
		for _, sz := range sizes {
			span := [2]int{min(sz.Columns, grid[0]), min(sz.Rows, grid[1])}
			w, h := spanPixels(p, span, grid)
			spanText := fmt.Sprintf("%dx%d", span[0], span[1])
			pv := galleryPreview{Span: spanText, Width: w, Height: h,
				ImageURL: "/api/widget/" + name + "/" + wi.Widget + "?" +
					url.Values{"display": {display}, "span": {spanText}, "grid": {fmt.Sprintf("%dx%d", grid[0], grid[1])}}.Encode()}
			if w < wi.MinWidth || h < wi.MinHeight {
				pv.Warning = fmt.Sprintf("below the minimum %dx%d px", wi.MinWidth, wi.MinHeight)
			}
			gw.Previews = append(gw.Previews, pv)
		}
		gp.Widgets = append(gp.Widgets, gw)
	}
	return gp
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
