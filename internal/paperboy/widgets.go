package paperboy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

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

// ---- widgets page ----

// handleWidgetsPage is the widgets page: the page editor without a page — the
// palette and the inspector of the editor around one widget, its rendered
// preview and the cell to paste into a page (ADR 0055, 0068). The link keeps
// the widget, its size and its args, so a view can be shared.
func (s *Server) handleWidgetsPage(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	q := r.URL.Query()
	display := editorDisplay(cfg, q.Get("display"))
	version, goVersion := editorVersion(cfg)
	v := editorPageView{Version: version, GoVersion: goVersion, Display: display, WidgetsPage: true}
	p, ok := cfg.Profiles[display]
	if !ok {
		v.Error = "no display type is configured; add one under displays/"
		renderEditorPage(w, v, http.StatusBadRequest)
		return
	}
	v.Text = p.text()
	// The display type switch keeps the widget, its size and its args, so
	// changing the display type does not drop the cell (issue #171).
	link := widgetLinkParams(q)
	for _, n := range displayNames(cfg) {
		link.Set("display", n)
		v.Displays = append(v.Displays, displayLink{Name: n, Selected: n == display,
			URL: "/ui/plugins?" + link.Encode()})
	}
	var span [2]int
	if sz, err := parseSize(q.Get("span")); err == nil {
		span = sz
	}
	v.Data = widgetsStateData(display, p, q.Get("widget"), span, widgetArgValues(q))
	renderEditorPage(w, v, http.StatusOK)
}

// widgetArgValues reads the non-empty arg.<name> values the operator chose for
// one widget; empty fields keep the default and a repeated key takes its last
// value (checkbox after its hidden "false").
func widgetArgValues(q url.Values) map[string]string {
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

// widgetLinkParams collects what the widgets page link carries besides the
// display type: the widget, its span and its args. The display type switch and
// `updateURL` in the script use it, so a switched or edited view keeps the cell.
func widgetLinkParams(q url.Values) url.Values {
	out := url.Values{}
	for _, k := range []string{"widget", "span"} {
		if v := strings.TrimSpace(q.Get(k)); v != "" {
			out.Set(k, v)
		}
	}
	for name, raw := range widgetArgValues(q) {
		out.Set("arg."+name, raw)
	}
	return out
}
