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
	"slices"
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
// A repeated key takes its last value, as a checkbox after its hidden
// "false" input does.
func widgetArgs(q url.Values) map[string]any {
	args := map[string]any{}
	for k, vs := range q {
		name, ok := strings.CutPrefix(k, "arg.")
		if !ok || len(vs) == 0 {
			continue
		}
		raw := vs[len(vs)-1]
		var v any
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			v = raw
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
	Anchor   string // "<plugin>-<widget>"
	Form     *galleryForm
	Snippet  string // cell for a page with the chosen args, when there are any
}

// galleryForm configures one widget's args; it is submitted with GET and
// reloads the gallery with the args applied to that widget only.
type galleryForm struct {
	Action string // URL path and anchor
	Hidden []galleryOption
	Fields []galleryField
	Spans  []galleryOption // empty on text displays
	Active bool            // the args of this form are applied
	Reset  string
}

type galleryField struct {
	Name        string // query key arg.<name>
	Label       string
	Type        string
	Description string
	Required    bool
	Select      bool
	Checkbox    bool // hidden "false" followed by a "true" checkbox
	Checked     bool
	Options     []galleryOption
	Value       string
	Placeholder string
}

type galleryOption struct {
	Name     string
	Value    string
	Label    string
	Selected bool
}

type galleryPlugin struct {
	Name    string
	Title   string
	Version string // downloaded program version (ADR 0060), empty for a hand-built one
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
	Tabs      []uiTab // playground and gallery
}

// handleGallery shows every widget of every plugin in the sizes the plugin
// declares, on one display type. Its sibling tab is the playground, which edits
// one widget with the palette and the inspector of the page editor.
func (s *Server) handleGallery(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	q := r.URL.Query()
	if q.Get("tab") == "playground" {
		s.handlePlayground(w, r)
		return
	}
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
	gp := galleryPage{Version: versionText(build, loc), GoVersion: build.Go, Display: display, Grid: gridText,
		Tabs: widgetTabs("gallery", display)}
	for _, n := range names {
		gp.Displays = append(gp.Displays, galleryDisplay{Name: n, Selected: n == display,
			URL: "/ui/plugins?" + url.Values{"display": {n}, "grid": {gridText}, "tab": {"gallery"}}.Encode()})
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
	specs := cfg.pluginSpecs()
	for _, name := range plugins {
		g := s.galleryPlugin(r.Context(), cfg, name, display, p, grid, q)
		g.Version = specs[name].Version
		gp.Plugins = append(gp.Plugins, g)
	}
	s.renderGallery(w, gp)
}

// widgetTabURL links one sub-tab of the widgets page.
func widgetTabURL(tab, display string) string {
	params := url.Values{"tab": {tab}}
	if display != "" {
		params.Set("display", display)
	}
	return "/ui/plugins?" + params.Encode()
}

// widgetTabs are the two faces of the widgets page: the playground edits one
// widget, the gallery lists all of them with their declared sizes.
func widgetTabs(selected, display string) []uiTab {
	tabs := []uiTab{
		{Name: "playground", Label: "Playground", URL: widgetTabURL("playground", display)},
		{Name: "gallery", Label: "Gallery", URL: widgetTabURL("gallery", display)},
	}
	for i := range tabs {
		tabs[i].Selected = tabs[i].Name == selected
	}
	return tabs
}

// handlePlayground is the widgets page without a page: the palette and the
// inspector of the page editor around one widget, its rendered preview and the
// cell to paste into a page (ADR 0055, 0068). The link keeps the widget, its
// size and its args, so a playground view can be shared.
func (s *Server) handlePlayground(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	q := r.URL.Query()
	display := editorDisplay(cfg, q.Get("display"))
	version, goVersion := editorVersion(cfg)
	v := editorPageView{Version: version, GoVersion: goVersion, Display: display, Playground: true,
		Tabs: widgetTabs("playground", display)}
	p, ok := cfg.Profiles[display]
	if !ok {
		v.Error = "no display type is configured; add one under displays/"
		renderEditorPage(w, v, http.StatusBadRequest)
		return
	}
	v.Text = p.text()
	for _, n := range displayNames(cfg) {
		v.Displays = append(v.Displays, galleryDisplay{Name: n, Selected: n == display,
			URL: "/ui/plugins?" + url.Values{"display": {n}, "tab": {"playground"}}.Encode()})
	}
	var span [2]int
	if sz, err := parseSize(q.Get("span")); err == nil {
		span = sz
	}
	v.Data = playgroundStateData(display, p, q.Get("widget"), span, galleryArgs(q))
	renderEditorPage(w, v, http.StatusOK)
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
		if len(a.Enum) > 0 {
			s += ", one of " + enumText(a.Enum)
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

func (s *Server) galleryPlugin(ctx context.Context, cfg *Config, name, display string, p Profile, grid [2]int, q url.Values) galleryPlugin {
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
	gridText := fmt.Sprintf("%dx%d", grid[0], grid[1])
	for _, wi := range d.Widgets {
		gw := galleryWidget{Name: wi.Widget, Title: wi.Title, Kinds: strings.Join(wi.Kinds, ", "), Args: argList(wi.Args),
			Anchor: name + "-" + wi.Widget}
		if len(wi.Kinds) > 0 && !contains(wi.Kinds, kind) {
			gw.Note = "not available for " + kind + " displays"
			gp.Widgets = append(gp.Widgets, gw)
			continue
		}
		active := q.Get("widget") == name+"/"+wi.Widget
		var chosen map[string]string
		span := ""
		if active {
			chosen = galleryArgs(q)
			span = q.Get("span")
		}
		gw.Form = galleryFormFor(name, wi, display, gridText, p.text(), chosen, span, active)
		argQuery := previewArgs(chosen, wi.Args)
		if p.text() {
			v := url.Values{"display": {display}}
			for k, a := range argQuery {
				v[k] = a
			}
			wq, _ := s.parseWidgetQuery(cfg, name, wi.Widget, v)
			gw.Previews = append(gw.Previews, galleryPreview{Text: page.TextCell(s.fetchWidget(ctx, cfg, wq), p.Charset, wq.req.Width, wq.req.Height)})
			if active {
				gw.Snippet = cellSnippet(name, wi.Widget, [2]int{p.Width, p.Height}, chosen, wi.Args)
			}
			gp.Widgets = append(gp.Widgets, gw)
			continue
		}
		sizes := wi.Sizes
		if len(sizes) == 0 {
			sizes = []plugin.Span{{Columns: 3, Rows: 2}}
		}
		if sz, err := parseSize(span); err == nil {
			sizes = []plugin.Span{{Columns: sz[0], Rows: sz[1]}}
		}
		for _, sz := range sizes {
			span := [2]int{min(sz.Columns, grid[0]), min(sz.Rows, grid[1])}
			w, h := spanPixels(p, span, grid)
			spanText := fmt.Sprintf("%dx%d", span[0], span[1])
			v := url.Values{"display": {display}, "span": {spanText}, "grid": {gridText}}
			for k, a := range argQuery {
				v[k] = a
			}
			pv := galleryPreview{Span: spanText, Width: w, Height: h,
				ImageURL: "/api/widget/" + name + "/" + wi.Widget + "?" + v.Encode()}
			if w < wi.MinWidth || h < wi.MinHeight {
				pv.Warning = fmt.Sprintf("below the minimum %dx%d px", wi.MinWidth, wi.MinHeight)
			}
			gw.Previews = append(gw.Previews, pv)
			if active && gw.Snippet == "" {
				gw.Snippet = cellSnippet(name, wi.Widget, span, chosen, wi.Args)
			}
		}
		gp.Widgets = append(gp.Widgets, gw)
	}
	return gp
}

// galleryArgs reads the non-empty arg.<name> values the operator chose for
// one widget; empty fields keep the default and a repeated key takes its
// last value (checkbox after its hidden "false").
func galleryArgs(q url.Values) map[string]string {
	out := map[string]string{}
	for k, vs := range q {
		name, ok := strings.CutPrefix(k, "arg.")
		if !ok || len(vs) == 0 {
			continue
		}
		raw := strings.TrimSpace(vs[len(vs)-1])
		if raw == "" {
			continue
		}
		out[name] = raw
	}
	return out
}

// argValue turns a form value into the value of an arg by its declared type:
// strings and durations stay strings, lists are split at commas, everything
// else is read as JSON when it parses (numbers, booleans).
func argValue(raw string, a plugin.Arg) any {
	switch a.Type {
	case "string", "duration":
		return raw
	case "list":
		out := []any{}
		for _, part := range strings.Split(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, jsonOrString(part))
			}
		}
		return out
	}
	return jsonOrString(raw)
}

func jsonOrString(s string) any {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	return v
}

// previewArgs encodes the chosen args as arg.<name> query values for
// /api/widget, as JSON so strings like "2" keep their type.
func previewArgs(chosen map[string]string, known map[string]plugin.Arg) url.Values {
	v := url.Values{}
	for name, raw := range chosen {
		b, _ := json.Marshal(argValue(raw, known[name]))
		v.Set("arg."+name, string(b))
	}
	return v
}

// isDefault reports whether a chosen value equals the declared default; a
// bool without a default defaults to false.
func isDefault(raw string, a plugin.Arg) bool {
	if a.Default == nil && a.Type == "bool" {
		return argValue(raw, a) == false
	}
	if a.Default == nil {
		return false
	}
	return fmt.Sprint(argValue(raw, a)) == fmt.Sprint(a.Default)
}

// cellSnippet is a page cell in YAML flow style with the args that differ
// from their defaults.
func cellSnippet(pluginName, widget string, span [2]int, chosen map[string]string, known map[string]plugin.Arg) string {
	var args []string
	for name, raw := range chosen {
		if isDefault(raw, known[name]) {
			continue
		}
		args = append(args, name+": "+yamlValue(argValue(raw, known[name])))
	}
	sort.Strings(args)
	s := fmt.Sprintf("{ at: [0, 0], span: [%d, %d], widget: %s/%s", span[0], span[1], pluginName, widget)
	if len(args) > 0 {
		s += ", args: { " + strings.Join(args, ", ") + " }"
	}
	return s + " }"
}

func yamlValue(v any) string {
	switch x := v.(type) {
	case string:
		if plainYAML(x) {
			return x
		}
		b, _ := json.Marshal(x)
		return string(b)
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = yamlValue(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// plainYAML reports whether s can be written unquoted and still reads back as
// the same string.
func plainYAML(s string) bool {
	if s == "" || jsonOrString(s) != any(s) {
		return false
	}
	switch strings.ToLower(s) {
	case "yes", "no", "on", "off", "null", "~", "true", "false", "y", "n":
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_-./", r)) {
			return false
		}
	}
	return true
}

func enumText(values []any) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprint(v)
	}
	return strings.Join(parts, ", ")
}

// galleryFormFor builds the form of one widget from its arg schema.
func galleryFormFor(pluginName string, wi plugin.WidgetInfo, display, grid string, text bool, chosen map[string]string, span string, active bool) *galleryForm {
	anchor := pluginName + "-" + wi.Widget
	f := &galleryForm{Action: "/ui/plugins#" + anchor, Active: active,
		Hidden: []galleryOption{{Name: "display", Value: display}, {Name: "grid", Value: grid}, {Name: "widget", Value: pluginName + "/" + wi.Widget}},
		Reset:  "/ui/plugins?" + url.Values{"display": {display}, "grid": {grid}}.Encode() + "#" + anchor}
	if !text {
		f.Spans = append(f.Spans, galleryOption{Value: "", Label: "declared sizes", Selected: span == ""})
		for _, sz := range wi.Sizes {
			v := fmt.Sprintf("%dx%d", sz.Columns, sz.Rows)
			f.Spans = append(f.Spans, galleryOption{Value: v, Label: v, Selected: v == span})
		}
		if !slices.ContainsFunc(f.Spans, func(o galleryOption) bool { return o.Selected }) {
			f.Spans = append(f.Spans, galleryOption{Value: span, Label: span, Selected: true})
		}
	}
	names := make([]string, 0, len(wi.Args))
	for n := range wi.Args {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		a := wi.Args[n]
		fd := galleryField{Name: "arg." + n, Label: n, Type: a.Type, Description: a.Description, Required: a.Required, Value: chosen[n]}
		def := ""
		if a.Default != nil {
			def = fmt.Sprint(a.Default)
		}
		var values []string
		if len(a.Enum) > 0 && a.Type != "list" {
			for _, e := range a.Enum {
				values = append(values, fmt.Sprint(e))
			}
		}
		if a.Type == "bool" && values == nil {
			fd.Checkbox = true
			v := fd.Value
			if v == "" {
				v = def
			}
			fd.Checked = jsonOrString(v) == true
		} else if values != nil {
			fd.Select = true
			label := "default"
			if def != "" {
				label += " (" + def + ")"
			}
			fd.Options = append(fd.Options, galleryOption{Label: label, Selected: fd.Value == ""})
			for _, v := range values {
				fd.Options = append(fd.Options, galleryOption{Value: v, Label: v, Selected: v == fd.Value})
			}
			if fd.Value != "" && !slices.Contains(values, fd.Value) {
				fd.Options = append(fd.Options, galleryOption{Value: fd.Value, Label: fd.Value + " (not allowed)", Selected: true})
				fd.Options[0].Selected = false
			}
		} else {
			fd.Placeholder = def
			if a.Type == "list" {
				fd.Placeholder = "comma-separated"
				if len(a.Enum) > 0 {
					fd.Placeholder += ": " + enumText(a.Enum)
				}
			}
		}
		f.Fields = append(f.Fields, fd)
	}
	return f
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
