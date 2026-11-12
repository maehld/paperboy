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
	"os"
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

// editorVariant is one file of a page in the overview's variants column. Every
// file is a link that opens exactly it; only a file that cannot be opened at
// all keeps a Delete here (ADR 0093).
type editorVariant struct {
	Label string // the display type, "shared" for pages/<name>.yaml
	URL   string // opens exactly that file, empty when it cannot be opened
	Class string // "selected" for the file the row shows, "broken" for a parse error
	Title string // the file path and why it cannot be opened
	// DeleteURL is only set when the file cannot be opened in an editor, so the
	// list is the only place to remove it (ADR 0073).
	DeleteURL string
	Warn      string // what the delete confirmation says
}

// editorPageGroup is one page in the overview: the file the selected display
// type resolves to, with the files the page has next to it.
type editorPageGroup struct {
	Name     string
	Kind     string
	Grid     string
	Title    string
	Error    string // the shown file does not parse; the text names the file
	Note     string // no file for the selected display type; a page with variants only is normal
	Variants []editorVariant
	OpenURL  string
}

type editorListPage struct {
	Version   string
	GoVersion string
	Display   string // the display type shared pages are opened with
	Displays  []displayLink
	Groups    []editorPageGroup
	Options   []uiOption         // display types of the new-page form
	Plans     []editorPlanRow    // the plan files next to the pages (ADR 0069)
	Devices   []editorDeviceRow  // the device files (ADR 0070)
	Unclaimed []editorUnclaimed  // state files whose MAC no device file uses (ADR 0080)
	Types     []editorDisplayRow // the display type files (ADR 0071)
	Tabs      []uiTab            // the object types as sub-tabs
	Tab       string             // the open sub-tab
}

// editorUnclaimed is one state file without a device file: hardware that
// registered but was never claimed (ADR 0080).
type editorUnclaimed struct {
	MAC       string
	Hardware  string
	Firmware  string
	Address   string // source address of the last contact (ADR 0083)
	FirstSeen string
	LastSeen  string
	ClaimURL  string
	ForgetURL string
}

// editorDisplayRow is one display type file in the overview.
type editorDisplayRow struct {
	Name    string
	Kind    string
	Size    string
	Density string // ppi of an image display, charset of a text display
	Gray    string
	Power   string
	Error   string
	OpenURL string
}

// editorDeviceRow is one device file in the overview.
type editorDeviceRow struct {
	ID      string
	Display string
	MAC     string
	Address string // source address of the last contact (ADR 0083)
	Plan    string
	Problem string // the file parses but cannot sync, e.g. a missing display type (ADR 0079)
	Error   string
	OpenURL string
}

// editorPlanRow is one plan file in the overview.
type editorPlanRow struct {
	Name     string
	Rules    int
	Fallback string
	Error    string
	OpenURL  string
}

// editorPageView is the shell of one page; the script reads the model, the
// plugin list and the profiles from the API.
type editorPageView struct {
	Version     string
	GoVersion   string
	Name        string
	Display     string
	Text        bool
	Error       string
	WidgetsPage bool          // the widgets page without a page (ADR 0055)
	Displays    []displayLink // display types this page can be opened for
	Toggle      *fileToggle   // the other file of the page, shared or the variant (ADR 0093)
	Data        template.JS   // the starting state of the script
}

// fileToggle switches between the shared page and the variant of the open
// display type, both of which have to exist (ADR 0093).
type fileToggle struct {
	URL   string
	Label string
}

// uiTab is one sub-tab of a view: the editor's object types.
type uiTab struct {
	Name     string
	Label    string
	Count    int
	URL      string
	Selected bool
}

// displayLink is one entry of the display type switch a page shows.
type displayLink struct {
	Name     string
	URL      string
	Selected bool
}

// uiOption is one entry of a select in an editor form.
type uiOption struct {
	Name     string
	Value    string
	Label    string
	Selected bool
}

// editorState is what the script starts from, so it does not have to guess the
// kind, the size or the grid of a page that does not exist yet.
type editorState struct {
	Name          string            `json:"name"`
	Display       string            `json:"display"`
	File          string            `json:"file"`  // display type of the open file, empty for the shared page
	Kind          string            `json:"kind"`  // image or text
	Width         int               `json:"width"` // characters of a text display, pixels of an image display
	Height        int               `json:"height"`
	New           bool              `json:"new"`            // there is no file yet
	VariantExists bool              `json:"variant_exists"` // the display type has a variant next to the shared page
	Grid          [2]int            `json:"grid"`           // grid a new page starts with
	Reference     [2]int            `json:"reference"`
	WidgetsPage   bool              `json:"widgets_page,omitempty"`
	Widget        string            `json:"widget,omitempty"` // plugin/widget the widgets page starts with
	Span          *[2]int           `json:"span,omitempty"`   // its size on the reference grid
	Args          map[string]string `json:"args,omitempty"`   // the args from the link, still as text
	UsedBy        []string          `json:"usedBy,omitempty"` // plans that name the page, for the delete warning
	Rename        *renameAction     `json:"rename,omitempty"` // nil for a new page (ADR 0079)
	Orphan        string            `json:"orphan,omitempty"` // variant of a missing display type (ADR 0079)
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
// stays listed, so an orphaned file stays visible and opens through a configured
// display type of its kind (ADR 0079, 0092).
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

// pageRow shows one page for the selected display type: its kind, grid and
// title describe the file that type resolves to. Every file of the page is a
// link that opens exactly it, the shared page first and the resolved file in
// bold (ADR 0093).
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
	targets := make([]string, len(files))
	reasons := make([]string, len(files))
	for i, f := range files {
		targets[i], reasons[i] = pageFileTarget(f, displays, display)
	}
	g := editorPageGroup{Name: name}
	for i, f := range files {
		v := editorVariant{Label: "shared", Title: pageFileName(name, f.Display)}
		if f.Display != "" {
			v.Label = f.Display
		}
		if reasons[i] != "" {
			v.Title += " — " + reasons[i]
		}
		if f.Error != "" {
			v.Class = "broken"
		}
		if i == shown {
			v.Class = strings.TrimSpace(v.Class + " selected")
		}
		if targets[i] != "" {
			v.URL = pageEditURL(name, f.Display, targets[i])
		} else {
			v.DeleteURL, v.Warn = pageDeleteURL(name, f.Display, f.Hash), pageDeleteWarning(name, f.Display, usedBy)
		}
		g.Variants = append(g.Variants, v)
	}
	if shown < 0 {
		g.Note = fmt.Sprintf("no file for display type %q and no shared page", display)
		return g
	}
	f := files[shown]
	g.Kind, g.Title = f.Kind, f.Title
	if f.Grid != [2]int{} {
		g.Grid = fmt.Sprintf("%dx%d", f.Grid[0], f.Grid[1])
	}
	if targets[shown] != "" {
		g.OpenURL = pageEditURL(name, f.Display, targets[shown])
	}
	switch {
	case f.Error != "":
		g.Error = pageFileName(name, f.Display) + ": " + f.Error
	case display == "":
		g.Error = "no display type configured to open it with"
	case targets[shown] == "":
		g.Note = reasons[shown]
	}
	return g
}

// pageFileTarget names the display type a page file is opened with and why it
// cannot be opened at all. The file is addressed exactly, the display type only
// supplies kind, pixels and ppi (ADR 0093). A broken file still opens read-only
// when its display type is configured or the shared page fits a configured one;
// a variant of a display type that is gone opens through a type of its kind,
// except when it does not parse and its kind is unknown (ADR 0079).
func pageFileTarget(f editorPageFile, displays map[string]Profile, selected string) (target, why string) {
	if f.Display != "" {
		if knownProfile(displays, f.Display) {
			return f.Display, ""
		}
		if f.Error == "" {
			if open := displayOfKind(displays, f.Kind); open != "" {
				return open, fmt.Sprintf("display type %q is not configured; opened for %s", f.Display, open)
			}
			return "", fmt.Sprintf("display type %q is not configured and no configured type has its kind", f.Display)
		}
		return "", fmt.Sprintf("display type %q is not configured", f.Display)
	}
	// The shared page is opened for the selected display type when its kind
	// fits, else for any configured type of the file's kind.
	if selected != "" {
		if f.Error != "" {
			return selected, ""
		}
		if p, ok := displays[selected]; ok && p.text() == (f.Kind == page.KindText) {
			return selected, ""
		}
	}
	if f.Error == "" {
		if open := displayOfKind(displays, f.Kind); open != "" {
			return open, ""
		}
		return "", "no configured display type has its kind"
	}
	return "", "no display type configured"
}

// pageEditURL links exactly one file of a page: shared=1 for the shared page,
// file=<display type> for a variant; the display type supplies the kind, size
// and ppi the editor renders for (ADR 0093).
func pageEditURL(name, file, display string) string {
	params := url.Values{"name": {name}, "display": {display}}
	if file == "" {
		params.Set("shared", "1")
	} else {
		params.Set("file", file)
	}
	return "/ui/editor/page?" + params.Encode()
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

// referenceLabels lists the named references of a display type, e.g. for the
// rename confirmation.
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

// displayOfKind returns a configured display type of the given kind, so an
// orphaned page variant can still be opened and saved as a variant of it
// (ADR 0079).
func displayOfKind(displays map[string]Profile, kind string) string {
	names := make([]string, 0, len(displays))
	for name := range displays {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if displays[name].text() == (kind == page.KindText) {
			return name
		}
	}
	return ""
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
	for _, p := range plans {
		row := editorPlanRow{Name: p.Name, Rules: p.Rules, Fallback: p.Fallback, Error: p.Error}
		if p.Error == "" {
			row.OpenURL = "/ui/editor/plan?" + url.Values{"name": {p.Name}}.Encode()
		}
		v.Plans = append(v.Plans, row)
	}
	devices, err := deviceFileList(cfg.DevicesDir)
	if err != nil {
		slog.Warn("device list for the page overview failed", "error", err)
	}
	addresses := s.deviceClientIPs(cfg)
	for _, d := range devices {
		row := editorDeviceRow{ID: d.ID, Display: d.Display, MAC: d.MAC, Plan: d.Plan, Error: d.Error}
		if key, err := deviceStateKey(d.MAC); err == nil {
			row.Address = addresses[key]
		}
		if d.Error == "" {
			row.OpenURL = "/ui/editor/device?" + url.Values{"id": {d.ID}}.Encode()
			if !knownProfile(cfg.Profiles, d.Display) {
				row.Problem = fmt.Sprintf("display type %q is not configured: the device answers with 503 until its file names a configured display type", d.Display)
			}
		}
		v.Devices = append(v.Devices, row)
	}
	for _, u := range s.unclaimedDevices(cfg) {
		v.Unclaimed = append(v.Unclaimed, editorUnclaimed{
			MAC: u.MAC, Hardware: u.Hardware, Firmware: u.Firmware, Address: u.ClientIP,
			FirstSeen: editorTime(cfg, u.FirstSeen), LastSeen: editorTime(cfg, u.LastSeen),
			ClaimURL:  "/ui/editor/device?" + url.Values{"new": {"1"}, "id": {suggestDeviceID(u)}, "mac": {u.MAC}}.Encode(),
			ForgetURL: "/api/state/devices/" + url.PathEscape(u.MAC),
		})
	}
	displays, err := displayFileList(cfg.DisplaysDir)
	if err != nil {
		slog.Warn("display type list for the page overview failed", "error", err)
	}
	for _, d := range displays {
		row := editorDisplayRow{Name: d.Name, Kind: d.Kind, Power: d.Power, Error: d.Error}
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
		v.Displays = append(v.Displays, displayLink{Name: n, Selected: n == display,
			URL: "/ui/editor?" + url.Values{"display": {n}}.Encode()})
		v.Options = append(v.Options, uiOption{Value: n, Label: n, Selected: n == display})
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
	// One file exactly: shared=1 is the shared page, file=<display type> a
	// variant. Neither resolves like the runtime: the variant of the display
	// type, else the shared page. source is the old name of an exact variant
	// (ADR 0079), kept for links written before ADR 0093.
	newPage := q.Get("new") == "1"
	sharedFile := q.Get("shared") == "1"
	file := q.Get("file")
	if file == "" {
		file = q.Get("source")
	}
	if file != "" && !page.ValidName(file) {
		v.Error = fmt.Sprintf("display type %q: only a-z, 0-9 and -", file)
		renderEditorPage(w, v, http.StatusBadRequest)
		return
	}
	fileDisplay := ""
	missing := false
	if file == "" && !sharedFile {
		_, fd, err := existingPageFile(cfg.Pages, name, display)
		fileDisplay = fd
		missing = errors.Is(err, fs.ErrNotExist)
		switch {
		case missing && !newPage:
			v.Error = fmt.Sprintf("there is no page %q for the display type %q yet", name, display)
			renderEditorPage(w, v, http.StatusNotFound)
			return
		case err != nil && !missing:
			v.Error = err.Error()
			renderEditorPage(w, v, http.StatusInternalServerError)
			return
		}
	} else {
		if !sharedFile {
			fileDisplay = file
		}
		path, err := pageFilePath(cfg.Pages, name, fileDisplay)
		if err != nil {
			v.Error = err.Error()
			renderEditorPage(w, v, http.StatusBadRequest)
			return
		}
		if _, err := os.Stat(path); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				v.Error = err.Error()
				renderEditorPage(w, v, http.StatusInternalServerError)
				return
			}
			if !newPage {
				label := "shared"
				if fileDisplay != "" {
					label = fileDisplay
				}
				v.Error = fmt.Sprintf("there is no page %q for the display type %q", name, label)
				renderEditorPage(w, v, http.StatusNotFound)
				return
			}
			missing = true
		}
	}
	orphan := ""
	if fileDisplay != "" && !knownProfile(cfg.Profiles, fileDisplay) {
		orphan = fileDisplay
	}
	// A page without a file starts as a new one; the shared flag makes it
	// pages/<name>.yaml instead of a variant (ADR 0068). Without a display type
	// the overview asks for a shared page.
	wantsShared := sharedFile || (newPage && q.Get("display") == "")
	shared := fileDisplay == "" && (!missing || wantsShared)
	variantExists := display != "" && pageFileExists(cfg.Pages, name, display)
	sharedExists := pageFileExists(cfg.Pages, name, "")
	switch {
	case !missing && shared && variantExists:
		v.Toggle = &fileToggle{URL: pageEditURL(name, display, display), Label: "Edit " + display + " variant"}
	case !missing && !shared && sharedExists:
		// The shared page may be of another kind than the open variant, so it
		// opens for a display type of its own kind.
		context := display
		if kind := pageFileKind(cfg.Pages, name, ""); kind != "" {
			if p, ok := cfg.Profiles[context]; ok && p.text() != (kind == page.KindText) {
				if open := displayOfKind(cfg.Profiles, kind); open != "" {
					context = open
				}
			}
		}
		v.Toggle = &fileToggle{URL: pageEditURL(name, "", context), Label: "Edit shared"}
	}
	plans, err := planFiles(cfg.Plans, cfg.Timezone)
	if err != nil {
		slog.Warn("plan list for the page editor failed", "error", err)
	}
	// A new variant resolved for the display type has no file to name, so its
	// target is the display type; an exact file stays itself.
	stateFile := fileDisplay
	if missing && !shared && stateFile == "" {
		stateFile = display
	}
	v.Data = editorStateData(name, display, stateFile, p, missing, variantExists, plansByPage(plans)[name], pageRenameAction(cfg, name, plansByPage(plans)[name]), orphan)
	for _, n := range displayNames(cfg) {
		params := url.Values{"name": {name}, "display": {n}}
		if newPage {
			params.Set("new", "1")
		}
		if shared {
			params.Set("shared", "1")
		}
		v.Displays = append(v.Displays, displayLink{Name: n, Selected: n == display,
			URL: "/ui/editor/page?" + params.Encode()})
	}
	renderEditorPage(w, v, http.StatusOK)
}

// pageFileExists reports whether one file of a page is there; an empty display
// type is the shared page.
func pageFileExists(dir, name, display string) bool {
	path, err := pageFilePath(dir, name, display)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// pageFileKind reads the kind of one page file; empty when it is missing or
// does not parse.
func pageFileKind(dir, name, display string) string {
	path, err := pageFilePath(dir, name, display)
	if err != nil {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	t, err := page.Parse(f)
	if err != nil {
		return ""
	}
	return t.Kind
}

// editorStateData encodes the starting state for the script. The JSON is safe
// to inline: encoding/json escapes <, > and &, so a page title can never close
// the script element.
func editorStateData(name, display, file string, p Profile, missing, variantExists bool, usedBy []string, rename *renameAction, orphan string) template.JS {
	state := editorState{Name: name, Display: display, File: file, Width: p.Width, Height: p.Height,
		New: missing, VariantExists: variantExists, Reference: defaultGrid, UsedBy: usedBy, Rename: rename, Orphan: orphan}
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

// widgetsStateData encodes the starting state of the widgets page: the display
// type plus the widget, its size and its args when the link carries them. The
// types of the args are read in the script, where the plugin schema is known.
func widgetsStateData(display string, p Profile, widget string, span [2]int, args map[string]string) template.JS {
	state := editorState{Display: display, WidgetsPage: true, Widget: widget, Args: args,
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
		slog.Error("widgets page state encoding failed", "error", err)
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

// editorTime formats a state time in the configured zone; empty for zero.
func editorTime(cfg *Config, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	loc := cfg.Timezone
	if loc == nil {
		loc = time.UTC
	}
	return t.In(loc).Format("2006-01-02 15:04")
}

// suggestDeviceID proposes a device id for unclaimed hardware, e.g.
// "sticky-ee99". The new-device form lets the operator change it.
func suggestDeviceID(u unclaimedDevice) string {
	var b strings.Builder
	for _, r := range strings.ToLower(u.Hardware) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			b.WriteRune(r)
		}
	}
	prefix := strings.Trim(b.String(), "-")
	if prefix == "" {
		prefix = "display"
	}
	mac := strings.ReplaceAll(u.MAC, ":", "")
	return prefix + "-" + strings.ToLower(mac[max(0, len(mac)-4):])
}
