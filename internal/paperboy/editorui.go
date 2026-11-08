package paperboy

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/maehld/paperboy/internal/page"
)

// The page editor on top of the page API (ADR 0066): an overview of the page
// files and one page opened for a display type (ADR 0068). Both are
// server-rendered templates; the editing itself is the embedded editor.js,
// which works on the JSON model of the API.

//go:embed ui/editor.html
var editorListTemplate string

var editorListTmpl = template.Must(template.New("editor").Parse(editorListTemplate))

//go:embed ui/editor-page.html
var editorPageTemplate string

var editorPageTmpl = template.Must(template.New("editor-page").Parse(editorPageTemplate))

//go:embed ui/editor.js
var editorJS []byte

// editorVariant is one file of a page in the overview's variants column.
type editorVariant struct {
	Label    string // the display type, "shared" for pages/<name>.yaml
	URL      string // opens that file for its display type, empty when it cannot be opened
	Selected bool   // the file the row shows
	Title    string // why a file cannot be opened
	// DeleteURL is only set when neither the row nor the display type switch can
	// reach the file: a variant of an unknown display type and a shared page
	// next to a variant (ADR 0073).
	DeleteURL string
	Warn      string // what the delete confirmation says
}

// editorPageGroup is one page in the overview: the file the selected display
// type resolves to, with the files the page has next to it.
type editorPageGroup struct {
	Name      string
	File      string // pages/<name>[.<display type>].yaml, empty when there is none for the display type
	Shared    bool   // File is the shared page
	Kind      string
	Grid      string
	Title     string
	Error     string // why the shown file cannot be opened, empty when it can
	Variants  []editorVariant
	OpenURL   string
	DeleteURL string // DELETE of the shown file (ADR 0073)
	Warn      string // what the delete confirmation says
}

type editorListPage struct {
	Version   string
	GoVersion string
	Display   string // the display type shared pages are opened with
	Displays  []galleryDisplay
	Groups    []editorPageGroup
	Options   []galleryOption    // display types of the new-page form
	Plans     []editorPlanRow    // the plan files next to the pages (ADR 0069)
	Devices   []editorDeviceRow  // the device files (ADR 0070)
	Types     []editorDisplayRow // the display type files (ADR 0071)
	Tabs      []uiTab            // the object types as sub-tabs
	Tab       string             // the open sub-tab
}

// editorDisplayRow is one display type file in the overview.
type editorDisplayRow struct {
	Name      string
	Kind      string
	Size      string
	Density   string // ppi of an image display, charset of a text display
	Gray      string
	Power     string
	Error     string
	OpenURL   string
	DeleteURL string
	Warn      string
}

// editorDeviceRow is one device file in the overview.
type editorDeviceRow struct {
	ID        string
	Display   string
	MAC       string
	Plan      string
	Error     string
	OpenURL   string
	DeleteURL string
	Warn      string
}

// editorPlanRow is one plan file in the overview.
type editorPlanRow struct {
	Name      string
	Rules     int
	Fallback  string
	Error     string
	OpenURL   string
	DeleteURL string
	Warn      string
}

// editorPageView is the shell of one page; the script reads the model, the
// plugin list and the profiles from the API.
type editorPageView struct {
	Version    string
	GoVersion  string
	Name       string
	Display    string
	Text       bool
	Error      string
	Playground bool             // the widgets page without a page (ADR 0055)
	Displays   []galleryDisplay // display types this page can be opened for
	Tabs       []uiTab          // playground and gallery, on the widgets page only
	Data       template.JS      // the starting state of the script
}

// uiTab is one sub-tab of a view: the editor's object types and the widgets
// page's playground and gallery.
type uiTab struct {
	Name     string
	Label    string
	Count    int
	URL      string
	Selected bool
}

// editorState is what the script starts from, so it does not have to guess the
// kind, the size or the grid of a page that does not exist yet.
type editorState struct {
	Name       string            `json:"name"`
	Display    string            `json:"display"`
	Kind       string            `json:"kind"`  // image or text
	Width      int               `json:"width"` // characters of a text display, pixels of an image display
	Height     int               `json:"height"`
	New        bool              `json:"new"`    // there is no file yet
	Shared     bool              `json:"shared"` // a new page is written as pages/<name>.yaml
	Grid       [2]int            `json:"grid"`   // grid a new page starts with
	Reference  [2]int            `json:"reference"`
	Playground bool              `json:"playground,omitempty"`
	Widget     string            `json:"widget,omitempty"` // plugin/widget the playground starts with
	Span       *[2]int           `json:"span,omitempty"`   // its size on the reference grid
	Args       map[string]string `json:"args,omitempty"`   // the args from the link, still as text
	UsedBy     []string          `json:"usedBy,omitempty"` // plans that name the page, for the delete warning
}

// displayNames lists the configured display types alphabetically.
func displayNames(cfg *Config) []string {
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// editorDisplay picks the display type an overview or a new page works on: the
// requested one, else the first image display, else the first one.
func editorDisplay(cfg *Config, requested string) string {
	if _, ok := cfg.Profiles[requested]; ok {
		return requested
	}
	names := displayNames(cfg)
	for _, n := range names {
		if !cfg.Profiles[n].text() {
			return n
		}
	}
	if len(names) > 0 {
		return names[0]
	}
	return ""
}

// groupPageFiles shows every page once: the file the selected display type
// resolves to (its variant, else the shared page) with the files the page has
// in its variants column. A variant of a display type that is not configured
// stays listed, so an orphaned file stays visible and can be deleted, but it
// cannot be opened because a save would be rejected.
func groupPageFiles(files []editorPageFile, displays map[string]Profile, display string, usedBy map[string][]string) []editorPageGroup {
	order := []string{}
	byName := map[string][]editorPageFile{}
	for _, f := range files {
		if _, ok := byName[f.Name]; !ok {
			order = append(order, f.Name)
		}
		byName[f.Name] = append(byName[f.Name], f)
	}
	groups := make([]editorPageGroup, 0, len(order))
	for _, name := range order {
		groups = append(groups, pageRow(name, byName[name], displays, display, usedBy[name]))
	}
	return groups
}

// pageRow resolves one page for the selected display type: its variant, else
// the shared page, else nothing. The row's Open and Delete act on that file;
// its kind, grid and title describe it.
func pageRow(name string, files []editorPageFile, displays map[string]Profile, display string, usedBy []string) editorPageGroup {
	shown := -1
	if display != "" {
		for i, f := range files {
			if f.Display == display {
				shown = i
				break
			}
		}
	}
	if shown < 0 {
		for i, f := range files {
			if f.Display == "" {
				shown = i
				break
			}
		}
	}
	g := editorPageGroup{Name: name}
	for i, f := range files {
		openable := f.Error == "" && (f.Display == "" || knownProfile(displays, f.Display))
		v := editorVariant{Label: "shared", Selected: i == shown}
		if f.Display != "" {
			v.Label = f.Display
		}
		switch {
		case f.Error != "":
			v.Title = f.Error
		case f.Display != "" && !knownProfile(displays, f.Display):
			v.Title = fmt.Sprintf("display type %q is not configured", f.Display)
		}
		target := f.Display
		if target == "" {
			target = display
		}
		if openable && target != "" {
			v.URL = "/ui/editor/page?" + url.Values{"name": {name}, "display": {target}}.Encode()
		}
		// A variant is reachable through the display type switch, the shared page
		// through the row when it is the shown file; anything else keeps its own
		// Delete so no file is stranded.
		if i != shown && (f.Display == "" || !knownProfile(displays, f.Display)) {
			v.DeleteURL, v.Warn = pageDeleteURL(name, f.Display, f.Hash), pageDeleteWarning(name, f.Display, usedBy)
		}
		g.Variants = append(g.Variants, v)
	}
	// The variants come first, the shared page last: the variants are what the
	// page is for, and their order keeps the display types together.
	for i, v := range g.Variants {
		if v.Label != "shared" || i == len(g.Variants)-1 {
			continue
		}
		rest := append(append([]editorVariant{}, g.Variants[:i]...), g.Variants[i+1:]...)
		g.Variants = append(rest, v)
		break
	}
	if shown < 0 {
		g.Error = fmt.Sprintf("no file for display type %q and no shared page", display)
		return g
	}
	f := files[shown]
	g.File, g.Shared = pageFileName(name, f.Display), f.Display == ""
	g.Kind, g.Title = f.Kind, f.Title
	if f.Grid != [2]int{} {
		g.Grid = fmt.Sprintf("%dx%d", f.Grid[0], f.Grid[1])
	}
	g.Warn = pageDeleteWarning(name, f.Display, usedBy)
	switch {
	case f.Error != "":
		g.Error = f.Error
	case display == "":
		g.Error = "no display type configured to open it with"
	default:
		g.OpenURL = "/ui/editor/page?" + url.Values{"name": {name}, "display": {display}}.Encode()
	}
	g.DeleteURL = pageDeleteURL(name, f.Display, f.Hash)
	return g
}

// pageFileName names a page file the way the operator sees it; an empty
// display type is the shared page.
func pageFileName(name, display string) string {
	if display == "" {
		return "pages/" + name + ".yaml"
	}
	return "pages/" + name + "." + display + ".yaml"
}

// pageDeleteURL names exactly the file of a row, so a shared page is never
// mistaken for the variant of the selected display type.
func pageDeleteURL(name, display, hash string) string {
	return "/api/pages/" + url.PathEscape(name) + "?" + url.Values{
		"display": {display}, "hash": {hash}}.Encode()
}

// pageDeleteWarning is the confirmation text of a page: the file and the plans
// that name it (ADR 0073).
func pageDeleteWarning(name, display string, plans []string) string {
	msg := "Delete " + pageFileName(name, display) + "?"
	if len(plans) == 0 {
		return msg + "\n\nNo plan names this page."
	}
	return msg + "\n\nPlans that name it: " + strings.Join(plans, ", ") + "."
}

// planDeleteWarning is the confirmation text of a plan: the file and the
// devices that use it.
func planDeleteWarning(name string, devices []string) string {
	msg := "Delete plans/" + name + ".yaml?"
	if len(devices) == 0 {
		return msg + "\n\nNo device uses this plan."
	}
	return msg + "\n\nDevices that use it: " + strings.Join(devices, ", ") + "."
}

// deviceDeleteWarning is the confirmation text of a device.
func deviceDeleteWarning(id string) string {
	return "Delete devices/" + id + ".yaml?" +
		"\n\nThe device cannot sync until a device file with its MAC exists again."
}

// displayDeleteWarning is the confirmation text of a display type: the file and
// the page variants and devices that reference it.
func displayDeleteWarning(name string, pages, devices []displayReference) string {
	msg := "Delete displays/" + name + ".yaml?"
	var refs []string
	if len(pages) > 0 {
		refs = append(refs, "page variants: "+referenceLabels(pages))
	}
	if len(devices) > 0 {
		refs = append(refs, "devices: "+referenceLabels(devices))
	}
	if len(refs) == 0 {
		return msg + "\n\nNo page variant or device uses this display type."
	}
	return msg + "\n\nAlso uses it → " + strings.Join(refs, "; ") + "."
}

func referenceLabels(refs []displayReference) string {
	labels := make([]string, 0, len(refs))
	for _, r := range refs {
		labels = append(labels, r.Label)
	}
	return strings.Join(labels, ", ")
}

// plansByPage maps a page name to the plans whose rules name it, once each.
// The delete confirmation and the page editor name the consequences (ADR 0073).
func plansByPage(plans []editorPlanFile) map[string][]string {
	index := map[string][]string{}
	seen := map[string]map[string]bool{}
	for _, p := range plans {
		for _, pageName := range p.Pages {
			if seen[pageName] == nil {
				seen[pageName] = map[string]bool{}
			}
			if seen[pageName][p.Name] {
				continue
			}
			seen[pageName][p.Name] = true
			index[pageName] = append(index[pageName], p.Name)
		}
	}
	return index
}

func knownProfile(displays map[string]Profile, name string) bool {
	_, ok := displays[name]
	return ok
}

func editorVersion(cfg *Config) (version, goVersion string) {
	loc := cfg.Timezone
	if loc == nil {
		loc = time.UTC
	}
	return versionText(build, loc), build.Go
}

// handleEditor lists the page files by page name, with the variants of each
// page, the plans and the device files, one object type per sub-tab; the pages
// tab offers a new page (ADR 0068).
func (s *Server) handleEditor(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	q := r.URL.Query()
	display := editorDisplay(cfg, q.Get("display"))
	tab := q.Get("tab")
	switch tab {
	case "plans", "devices", "displays":
	default:
		tab = "pages"
	}
	files, err := pageFiles(cfg.Pages)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	version, goVersion := editorVersion(cfg)
	plans, err := planFiles(cfg.Plans, cfg.Timezone)
	if err != nil {
		slog.Warn("plan list for the page overview failed", "error", err)
	}
	v := editorListPage{Version: version, GoVersion: goVersion, Display: display, Tab: tab,
		Groups: groupPageFiles(files, cfg.Profiles, display, plansByPage(plans))}
	devicesByPlan := map[string][]string{}
	for _, d := range cfg.Devices {
		if d.Plan != "" {
			devicesByPlan[d.Plan] = append(devicesByPlan[d.Plan], d.ID)
		}
	}
	for _, p := range plans {
		row := editorPlanRow{Name: p.Name, Rules: p.Rules, Fallback: p.Fallback, Error: p.Error,
			DeleteURL: "/api/plans/" + url.PathEscape(p.Name) + "?" + url.Values{"hash": {p.Hash}}.Encode(),
			Warn:      planDeleteWarning(p.Name, devicesByPlan[p.Name])}
		if p.Error == "" {
			row.OpenURL = "/ui/editor/plan?" + url.Values{"name": {p.Name}}.Encode()
		}
		v.Plans = append(v.Plans, row)
	}
	devices, err := deviceFileList(cfg.DevicesDir)
	if err != nil {
		slog.Warn("device list for the page overview failed", "error", err)
	}
	for _, d := range devices {
		row := editorDeviceRow{ID: d.ID, Display: d.Display, MAC: d.MAC, Plan: d.Plan, Error: d.Error,
			DeleteURL: "/api/devices/" + url.PathEscape(d.ID) + "?" + url.Values{"hash": {d.Hash}}.Encode(),
			Warn:      deviceDeleteWarning(d.ID)}
		if d.Error == "" {
			row.OpenURL = "/ui/editor/device?" + url.Values{"id": {d.ID}}.Encode()
		}
		v.Devices = append(v.Devices, row)
	}
	displays, err := displayFileList(cfg.DisplaysDir)
	if err != nil {
		slog.Warn("display type list for the page overview failed", "error", err)
	}
	for _, d := range displays {
		row := editorDisplayRow{Name: d.Name, Kind: d.Kind, Power: d.Power, Error: d.Error,
			DeleteURL: "/api/displays/" + url.PathEscape(d.Name) + "?" + url.Values{"hash": {d.Hash}}.Encode()}
		pages, used := displayReferences(cfg, d.Name)
		row.Warn = displayDeleteWarning(d.Name, pages, used)
		if d.Size[0] > 0 && d.Size[1] > 0 {
			row.Size = fmt.Sprintf("%d × %d", d.Size[0], d.Size[1])
		}
		if d.Kind == kindText {
			row.Density = d.Charset
			if row.Density == "" {
				row.Density = page.CharsetUTF8
			}
			row.Gray = "—"
		} else {
			if d.PPI > 0 {
				row.Density = strconv.FormatFloat(d.PPI, 'f', -1, 64) + " ppi"
			}
			row.Gray = "1"
			if d.Gray == 4 {
				row.Gray = "4"
			}
		}
		if row.Power == "" {
			row.Power = powerAuto
		}
		if d.Error == "" {
			row.OpenURL = "/ui/editor/display?" + url.Values{"name": {d.Name}}.Encode()
		}
		v.Types = append(v.Types, row)
	}
	for _, n := range displayNames(cfg) {
		v.Displays = append(v.Displays, galleryDisplay{Name: n, Selected: n == display,
			URL: "/ui/editor?" + url.Values{"display": {n}}.Encode()})
		v.Options = append(v.Options, galleryOption{Value: n, Label: n, Selected: n == display})
	}
	v.Tabs = []uiTab{
		{Name: "pages", Label: "Pages", Count: len(v.Groups), URL: editorTabURL("pages", display)},
		{Name: "plans", Label: "Plans", Count: len(v.Plans), URL: editorTabURL("plans", "")},
		{Name: "devices", Label: "Devices", Count: len(v.Devices), URL: editorTabURL("devices", "")},
		{Name: "displays", Label: "Displays", Count: len(v.Types), URL: editorTabURL("displays", "")},
	}
	for i := range v.Tabs {
		v.Tabs[i].Selected = v.Tabs[i].Name == tab
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := editorListTmpl.Execute(w, v); err != nil {
		slog.Error("page overview rendering failed", "error", err)
	}
}

// editorTabURL links one sub-tab; the pages tab keeps the chosen display type.
func editorTabURL(tab, display string) string {
	params := url.Values{"tab": {tab}}
	if tab == "pages" && display != "" {
		params.Set("display", display)
	}
	return "/ui/editor?" + params.Encode()
}

// handleEditorPage opens one page for a display type. A page without a file
// opens as a new page when the overview asked for one (&new=1); a page that
// exists only as the variant of another display type is not an error, it is
// simply not there for this one.
func (s *Server) handleEditorPage(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	q := r.URL.Query()
	name := q.Get("name")
	version, goVersion := editorVersion(cfg)
	v := editorPageView{Version: version, GoVersion: goVersion, Name: name}
	if !page.ValidName(name) {
		v.Error = fmt.Sprintf("page name %q: only a-z, 0-9 and -", name)
		renderEditorPage(w, v, http.StatusBadRequest)
		return
	}
	display := q.Get("display")
	if display != "" && !knownProfile(cfg.Profiles, display) {
		v.Error = fmt.Sprintf("display type %q is not configured", display)
		renderEditorPage(w, v, http.StatusBadRequest)
		return
	}
	if display == "" {
		display = editorDisplay(cfg, "")
	}
	p, ok := cfg.Profiles[display]
	if !ok {
		v.Error = "no display type is configured; add one under displays/"
		renderEditorPage(w, v, http.StatusBadRequest)
		return
	}
	v.Display, v.Text = display, p.text()
	_, fileDisplay, err := existingPageFile(cfg.Pages, name, display)
	missing := errors.Is(err, fs.ErrNotExist)
	switch {
	case missing && q.Get("new") != "1":
		v.Error = fmt.Sprintf("there is no page %q for the display type %q yet", name, display)
		renderEditorPage(w, v, http.StatusNotFound)
		return
	case err != nil && !missing:
		v.Error = err.Error()
		renderEditorPage(w, v, http.StatusInternalServerError)
		return
	}
	// A page without a file starts as a new one; the shared flag makes it
	// pages/<name>.yaml instead of a variant (ADR 0068). Without a display type
	// the overview asks for a shared page.
	wantsShared := q.Get("shared") == "1" || (q.Get("new") == "1" && q.Get("display") == "")
	shared := fileDisplay == "" && (!missing || wantsShared)
	plans, err := planFiles(cfg.Plans, cfg.Timezone)
	if err != nil {
		slog.Warn("plan list for the page editor failed", "error", err)
	}
	v.Data = editorStateData(name, display, p, missing, shared, plansByPage(plans)[name])
	for _, n := range displayNames(cfg) {
		params := url.Values{"name": {name}, "display": {n}}
		if q.Get("new") == "1" {
			params.Set("new", "1")
		}
		if shared {
			params.Set("shared", "1")
		}
		v.Displays = append(v.Displays, galleryDisplay{Name: n, Selected: n == display,
			URL: "/ui/editor/page?" + params.Encode()})
	}
	renderEditorPage(w, v, http.StatusOK)
}

// editorStateData encodes the starting state for the script. The JSON is safe
// to inline: encoding/json escapes <, > and &, so a page title can never close
// the script element.
func editorStateData(name, display string, p Profile, missing, shared bool, usedBy []string) template.JS {
	state := editorState{Name: name, Display: display, Width: p.Width, Height: p.Height,
		New: missing, Shared: shared, Reference: defaultGrid, UsedBy: usedBy}
	if p.text() {
		state.Kind = page.KindText
		state.Grid = [2]int{p.Width, p.Height}
	} else {
		state.Kind = page.KindImage
		state.Grid = defaultGrid
	}
	data, err := json.Marshal(state)
	if err != nil { // never expected: only strings and numbers
		slog.Error("page editor state encoding failed", "error", err)
		return "{}"
	}
	return template.JS(data)
}

// playgroundStateData encodes the starting state of the playground: the display
// type plus the widget, its size and its args when the link carries them. The
// types of the args are read in the script, where the plugin schema is known.
func playgroundStateData(display string, p Profile, widget string, span [2]int, args map[string]string) template.JS {
	state := editorState{Display: display, Playground: true, Widget: widget, Args: args,
		Reference: defaultGrid, Width: p.Width, Height: p.Height}
	if span[0] > 0 && span[1] > 0 {
		state.Span = &span
	}
	if p.text() {
		state.Kind = page.KindText
		state.Grid = [2]int{p.Width, p.Height}
	} else {
		state.Kind = page.KindImage
		state.Grid = defaultGrid
	}
	data, err := json.Marshal(state)
	if err != nil { // never expected: only strings and numbers
		slog.Error("playground state encoding failed", "error", err)
		return "{}"
	}
	return template.JS(data)
}

func renderEditorPage(w http.ResponseWriter, v editorPageView, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := editorPageTmpl.Execute(w, v); err != nil {
		slog.Error("page editor rendering failed", "error", err)
	}
}

// handleEditorJS serves the editor script, embedded in the binary like the
// templates.
func (s *Server) handleEditorJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(editorJS)
}
