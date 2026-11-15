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

// editorVariant is one variant file of a page in the overview. The label is the
// file name after the page name, and the title says which display types the
// page sends to it (ADR 0110).
type editorVariant struct {
	Label string
	URL   string // the editor of that file; empty when no display type can render it
	Class string // "broken" for a parse error, "orphan" for a file without a page
	Title string // the file path, the display types it serves and why
}

// editorPageGroup is one page in the overview: its page file, when it exists,
// with the variant files next to it.
type editorPageGroup struct {
	Name     string
	Title    string
	Kind     string
	Grid     string
	Error    string // the shown file does not parse; the text names the file
	Note     string // the page has no file, or the row cannot be opened
	PageURL  string // the page file in the editor; empty when it is missing
	Variants []editorVariant
	Assign   *assignDialog // present when a file of the page is not reachable
}

// assignDialog moves one file that no page uses onto a page, or makes it a
// variant of a page that exists (ADR 0110).
type assignDialog struct {
	ID     string     // the dialog element
	Source string     // the page name the files belong to
	Files  []uiOption // the files of this page, by label
	Types  []uiOption // display types; empty value: the file becomes the page file
}

type editorListPage struct {
	Version   string
	GoVersion string
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
	Display     string // the preview type (ADR 0110)
	Text        bool
	Error       string
	File        string // the label of the open file; empty: the page file
	FilePath    string // pages/<name>[.<label>].yaml
	Warning     string // the open file is not the one that serves the preview type
	WarnURL     string // the file that serves it instead, when it exists
	WarnLabel   string
	Orphan      bool          // the open variant has no page file
	NoPage      bool          // the page has no page file
	WidgetsPage bool          // the widgets page without a page (ADR 0055)
	Displays    []displayLink // display types the open file can render
	Coverage    []coverageRow // the display types of the page (ADR 0110)
	Data        template.JS   // the starting state of the script
}

// coverageRow is one configured display type in the coverage panel of the page
// editor: the file that serves it, or why none does and whether a layout can be
// created for it.
type coverageRow struct {
	Display string
	Kind    string
	Served  string // the page or variant file that serves the type; empty: none
	Reason  string // why the type is not served
	Open    string // the editor link of the serving file, when it is not the open one
	Create  bool   // offer to create the layout for this type
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
	Name        string              `json:"name"`
	Display     string              `json:"display"` // preview type
	File        string              `json:"file"`    // label of the open file; empty: the page file
	Kind        string              `json:"kind"`    // image or text: the kind of the open file
	Width       int                 `json:"width"`   // characters of a text display, pixels of an image display
	Height      int                 `json:"height"`
	New         bool                `json:"new"`  // there is no file yet
	Grid        [2]int              `json:"grid"` // grid a new page starts with
	Reference   [2]int              `json:"reference"`
	PageHash    string              `json:"page_hash,omitempty"` // hash of the page file, for a new variant
	PageExists  bool                `json:"page_exists"`         // the page file exists
	Named       bool                `json:"named,omitempty"`     // the page names this variant
	Displays    []editorDisplayInfo `json:"displays,omitempty"`  // configured display types (ADR 0110)
	WidgetsPage bool                `json:"widgets_page,omitempty"`
	Widget      string              `json:"widget,omitempty"` // plugin/widget the widgets page starts with
	Span        *[2]int             `json:"span,omitempty"`   // its size on the reference grid
	Args        map[string]string   `json:"args,omitempty"`   // the args from the link, still as text
	UsedBy      []string            `json:"usedBy,omitempty"` // plans that name the page, for the delete warning
	Rename      *renameAction       `json:"rename,omitempty"` // nil for a new page (ADR 0079)
	Orphan      string              `json:"orphan,omitempty"` // the variant has no page file (ADR 0110)
}

// editorDisplayInfo tells the script the kind and the character grid of a
// display type, so it starts a new variant from the type and never from the
// open file (ADR 0110, issue #238).
type editorDisplayInfo struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// displayNames lists the configured display types alphabetically.
func displayNames(cfg *Config) []string {
	return sortedDisplays(cfg.Profiles)
}

func sortedDisplays(displays map[string]Profile) []string {
	names := make([]string, 0, len(displays))
	for name := range displays {
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

// groupPageFiles shows every page once, with the page file and the variant
// files next to it (ADR 0110). A page whose page file is missing stays listed:
// its variants are orphans, and the row offers to assign one as the page file.
func groupPageFiles(files []editorPageFile, displays map[string]Profile) []editorPageGroup {
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
		groups = append(groups, pageRow(name, byName[name], displays))
	}
	return groups
}

// pageRow shows one page: the page file when it exists, else its first variant.
// The variants column names every other file, with the display types the page
// sends to it (ADR 0110).
func pageRow(name string, files []editorPageFile, displays map[string]Profile) editorPageGroup {
	g := editorPageGroup{Name: name}
	var page *editorPageFile
	for i := range files {
		if files[i].Label == "" {
			page = &files[i]
			break
		}
	}
	variants := map[string]string{}
	if page != nil {
		variants = page.Variants
	}
	// The row shows the page file when it exists, else the first variant that
	// parses, else the first one.
	shown := page
	if shown == nil {
		shown = &files[0]
		for i := range files {
			if files[i].Error == "" {
				shown = &files[i]
				break
			}
		}
	}
	g.Kind, g.Title = shown.Kind, shown.Title
	if shown.Grid != [2]int{} {
		g.Grid = fmt.Sprintf("%dx%d", shown.Grid[0], shown.Grid[1])
	}
	if page != nil {
		g.PageURL, _ = pageEditURL(*page, variants, displays, false)
	}
	unnamed := false
	for _, f := range files {
		if f.Label == "" {
			continue // the page file is the row, not a chip
		}
		v := editorVariant{Label: f.Label, Title: pageFileName(name, f.Label)}
		serves := servesTypes(variants, f.Label)
		switch {
		case len(serves) > 0:
			v.Title += " — serves " + strings.Join(serves, ", ")
		default:
			v.Title += " — no display type names it"
			unnamed = true
		}
		if f.Error != "" {
			v.Class = "broken"
			v.Title += " — " + f.Error
		}
		if f.Orphan {
			v.Class = "orphan"
		}
		v.URL, _ = pageEditURL(f, variants, displays, false)
		g.Variants = append(g.Variants, v)
	}
	switch {
	case page == nil:
		g.Note = "No page file. Assign a variant to become it."
	case shown.Error != "":
		g.Error = pageFileName(name, shown.Label) + ": " + shown.Error
	case g.PageURL == "":
		g.Note = "No configured display type can render this file."
	}
	if page == nil || unnamed {
		g.Assign = assignDialogFor(name, files, displays)
	}
	return g
}

// servesTypes names the display types whose entry points at one label.
func servesTypes(variants map[string]string, label string) []string {
	var out []string
	for display, l := range variants {
		if l == label {
			out = append(out, display)
		}
	}
	sort.Strings(out)
	return out
}

// assignDialogFor offers the files of a page in the assign dialog: each can
// become the page file or the layout of a display type (ADR 0110).
func assignDialogFor(name string, files []editorPageFile, displays map[string]Profile) *assignDialog {
	d := &assignDialog{ID: "assign-" + name, Source: name}
	for _, f := range files {
		if f.Label != "" {
			d.Files = append(d.Files, uiOption{Value: f.Label, Label: pageFileName(name, f.Label)})
		}
	}
	for _, n := range sortedDisplays(displays) {
		d.Types = append(d.Types, uiOption{Value: n, Label: n})
	}
	return d
}

// fileFits reports whether a page file can render a display type: the same kind
// and, for text, the character grid of the display.
func fileFits(f editorPageFile, p Profile) bool {
	if f.Error != "" || f.Kind == "" {
		return true // a broken file opens read-only, so any type can show it
	}
	if p.text() != (f.Kind == page.KindText) {
		return false
	}
	return !p.text() || f.Grid == [2]int{p.Width, p.Height}
}

// pageEditURL links exactly one page file with the preview display type
// (ADR 0110). The display type only supplies kind, pixels and ppi; the file is
// what the editor reads and writes. An empty URL means no configured display
// type can render the file.
func pageEditURL(f editorPageFile, variants map[string]string, displays map[string]Profile, newPage bool) (string, string) {
	display, why := filePreview(f, variants, displays)
	if display == "" {
		return "", why
	}
	params := url.Values{"name": {f.Name}, "display": {display}}
	if f.Label != "" {
		params.Set("file", f.Label)
	}
	if newPage {
		params.Set("new", "1")
	}
	return "/ui/editor/page?" + params.Encode(), ""
}

// filePreview picks the display type a page file is opened for: one the page
// sends to it, else any configured type of its kind and grid (ADR 0110).
func filePreview(f editorPageFile, variants map[string]string, displays map[string]Profile) (display, why string) {
	names := sortedDisplays(displays)
	if len(names) == 0 {
		return "", "no display type is configured"
	}
	var served, fits []string
	for _, n := range names {
		if (f.Label == "" && variants[n] == "") || (f.Label != "" && variants[n] == f.Label) {
			served = append(served, n)
		}
		if fileFits(f, displays[n]) {
			fits = append(fits, n)
		}
	}
	for _, n := range served {
		if fileFits(f, displays[n]) {
			return n, ""
		}
	}
	for _, n := range fits {
		// A file that no type is sent to still opens for a type of its kind, so
		// an unused variant stays editable.
		return n, ""
	}
	if f.Error != "" {
		if len(served) > 0 {
			return served[0], ""
		}
		return names[0], ""
	}
	return "", "no configured display type has its kind or its character grid"
}

// pageFileName names a page file the way the operator sees it; an empty label
// is the page file.
func pageFileName(name, label string) string {
	if label == "" {
		return "pages/" + name + ".yaml"
	}
	return "pages/" + name + "." + label + ".yaml"
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
	v := editorListPage{Version: version, GoVersion: goVersion, Tab: tab,
		Groups: groupPageFiles(files, cfg.Profiles)}
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
			if d.Gray > 1 {
				row.Gray = strconv.Itoa(d.Gray)
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
		v.Options = append(v.Options, uiOption{Value: n, Label: n, Selected: n == editorDisplay(cfg, "")})
	}
	v.Tabs = []uiTab{
		{Name: "pages", Label: "Pages", Count: len(v.Groups), URL: editorTabURL("pages")},
		{Name: "plans", Label: "Plans", Count: len(v.Plans), URL: editorTabURL("plans")},
		{Name: "devices", Label: "Devices", Count: len(v.Devices), URL: editorTabURL("devices")},
		{Name: "displays", Label: "Displays", Count: len(v.Types), URL: editorTabURL("displays")},
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

// editorTabURL links one sub-tab; the pages tab no longer carries a display
// type of its own (ADR 0094).
func editorTabURL(tab string) string {
	return "/ui/editor?" + url.Values{"tab": {tab}}.Encode()
}

// handleEditorPage opens one page file with a preview display type (ADR 0110).
// The file is addressed exactly by name and label; the display type only
// selects the pixels the draft is rendered for. A page without a file opens as
// a new page when the overview asked for one (&new=1).
func (s *Server) handleEditorPage(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	q := r.URL.Query()
	name, label := q.Get("name"), q.Get("file")
	version, goVersion := editorVersion(cfg)
	v := editorPageView{Version: version, GoVersion: goVersion, Name: name, File: label}
	if !page.ValidName(name) {
		v.Error = fmt.Sprintf("page name %q: only a-z, 0-9 and -", name)
		renderEditorPage(w, v, http.StatusBadRequest)
		return
	}
	if label != "" && !page.ValidName(label) {
		v.Error = fmt.Sprintf("page file %q: only a-z, 0-9 and -", label)
		renderEditorPage(w, v, http.StatusBadRequest)
		return
	}
	display := q.Get("display")
	if display != "" && !knownProfile(cfg.Profiles, display) {
		v.Error = fmt.Sprintf("display type %q is not configured", display)
		renderEditorPage(w, v, http.StatusBadRequest)
		return
	}
	newPage := q.Get("new") == "1"
	files, err := pageFiles(cfg.Pages)
	if err != nil {
		v.Error = err.Error()
		renderEditorPage(w, v, http.StatusInternalServerError)
		return
	}
	var group []editorPageFile
	var open *editorPageFile
	for i := range files {
		if files[i].Name != name {
			continue
		}
		group = append(group, files[i])
		if files[i].Label == label {
			open = &files[i]
		}
	}
	path, err := pageFilePath(cfg.Pages, name, label)
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
		switch {
		case label != "":
			// A variant exists through the entry of its page. The editor
			// creates it from the coverage, so it starts from the display type
			// and never from another file (ADR 0110, issue #238).
			v.Error = fmt.Sprintf("there is no page file %s; create it from the coverage of the page", pageFileName(name, label))
			renderEditorPage(w, v, http.StatusNotFound)
			return
		case !newPage:
			v.Error = fmt.Sprintf("there is no page %q yet", name)
			renderEditorPage(w, v, http.StatusNotFound)
			return
		}
	}
	coverage, variants := s.pageCoverage(cfg, name)
	// The page file of the group decides the page; it is not necessarily the
	// open file (ADR 0110).
	var pageFile *editorPageFile
	for i := range group {
		if group[i].Label == "" {
			pageFile = &group[i]
			break
		}
	}
	pageExists := pageFile != nil
	v.NoPage = !pageExists
	v.Orphan = label != "" && !pageExists
	missing := open == nil
	if display == "" {
		if missing {
			display = editorDisplay(cfg, "")
		} else {
			display, _ = filePreview(*open, variants, cfg.Profiles)
		}
	}
	p, ok := cfg.Profiles[display]
	if !ok {
		v.Error = "no display type is configured; add one under displays/"
		renderEditorPage(w, v, http.StatusBadRequest)
		return
	}
	// The kind of the open file drives the palette; a new page starts from the
	// preview display type.
	kind := page.KindImage
	if p.text() {
		kind = page.KindText
	}
	if open != nil && open.Kind != "" {
		kind = open.Kind
	}
	context := editorPageFile{Name: name, Label: label, Kind: kind}
	switch {
	case open != nil:
		context.Grid = open.Grid
	case kind == page.KindText:
		context.Grid = [2]int{p.Width, p.Height}
	default:
		context.Grid = defaultGrid
	}
	v.Display, v.Text = display, p.text()
	v.FilePath = pageFileName(name, label)
	// The preview lists the display types the open file can render (ADR 0110).
	for _, n := range displayNames(cfg) {
		if !fileFits(context, cfg.Profiles[n]) {
			continue
		}
		params := url.Values{"name": {name}, "display": {n}}
		if label != "" {
			params.Set("file", label)
		}
		if missing {
			params.Set("new", "1")
		}
		v.Displays = append(v.Displays, displayLink{Name: n, Selected: n == display,
			URL: "/ui/editor/page?" + params.Encode()})
	}
	v.Coverage, v.Warning, v.WarnURL, v.WarnLabel = s.pageEditorCoverage(cfg, name, label, display, context, coverage, pageExists)
	plans, err := planFiles(cfg.Plans, cfg.Timezone)
	if err != nil {
		slog.Warn("plan list for the page editor failed", "error", err)
	}
	usedBy := plansByPage(plans)[name]
	pageHash := ""
	if pageExists {
		pageHash = pageFile.Hash
	}
	named := false
	for _, l := range variants {
		if label != "" && l == label {
			named = true
		}
	}
	v.Data = editorStateData(editorState{
		Name: name, Display: display, File: label, Kind: kind,
		Width: p.Width, Height: p.Height, New: missing, Reference: defaultGrid,
		Grid:       pageGrid(kind, p, *openOrBlank(open, name, label)),
		PageHash:   pageHash,
		PageExists: pageExists,
		Named:      named,
		Displays:   displayInfos(cfg),
		UsedBy:     usedBy,
		Rename:     pageRenameAction(cfg, name, usedBy),
		Orphan:     orphanLabel(v.Orphan, label),
	})
	renderEditorPage(w, v, http.StatusOK)
}

// pageEditorCoverage renders the coverage rows of the page and the warning for
// a preview type that the open file does not serve (ADR 0110). The editor never
// renders another file in place of the open one; it names the file to open.
func (s *Server) pageEditorCoverage(cfg *Config, name, label, display string, open editorPageFile, coverage []pageCoverageEntry, pageExists bool) (rows []coverageRow, warning, warnURL, warnLabel string) {
	var current *pageCoverageEntry
	for i := range coverage {
		c := &coverage[i]
		row := coverageRow{Display: c.Display, Kind: c.Kind, Reason: c.Error}
		serves := !c.Missing && c.Error == ""
		if serves {
			row.Served = pageFileName(name, c.File)
		}
		if pageExists && c.Error != "" && (c.Missing || c.File == "") {
			row.Create = true
		}
		if serves && c.File != label {
			row.Open, _ = pageEditURL(editorPageFile{Name: name, Label: c.File}, coverageVariants(coverage), cfg.Profiles, false)
		}
		if c.Display == display {
			current = c
		}
		rows = append(rows, row)
	}
	if !pageExists || current == nil {
		return rows, "", "", ""
	}
	fits := fileFits(open, cfg.Profiles[display])
	// Another file serves the preview type: the editor names it and offers to
	// open it, and never renders it in place of the open one (ADR 0110).
	other := !current.Missing && current.Error == "" && current.File != label
	switch {
	case fits && other:
		warning = fmt.Sprintf("Display type %q is served by %s; this file is not used for it", display, pageFileName(name, current.File))
	case !fits:
		warning = fmt.Sprintf("This file cannot render display type %q", display)
		if current.Error != "" {
			warning += ": " + current.Error
		}
	case current.Missing:
		warning = fmt.Sprintf("Display type %q has no file: %s", display, current.Error)
	case current.Error != "":
		warning = fmt.Sprintf("Display type %q is not served: %s", display, current.Error)
	}
	if other {
		warnLabel = "Open " + pageFileName(name, current.File)
		warnURL, _ = pageEditURL(editorPageFile{Name: name, Label: current.File}, coverageVariants(coverage), cfg.Profiles, false)
	}
	return rows, warning, warnURL, warnLabel
}

// coverageVariants rebuilds the map a page's coverage stands for, so a link to
// one variant file can pick its preview type.
func coverageVariants(coverage []pageCoverageEntry) map[string]string {
	variants := map[string]string{}
	for _, c := range coverage {
		if c.File != "" && !c.Missing {
			variants[c.Display] = c.File
		}
	}
	return variants
}

// openOrBlank returns the open file, or a blank one for a page that is not
// written yet.
func openOrBlank(open *editorPageFile, name, label string) *editorPageFile {
	if open != nil {
		return open
	}
	return &editorPageFile{Name: name, Label: label}
}

// pageGrid is the grid a new page starts with: the character grid of a text
// display, else the reference grid.
func pageGrid(kind string, p Profile, open editorPageFile) [2]int {
	if kind == page.KindText {
		return [2]int{p.Width, p.Height}
	}
	if open.Grid != [2]int{} {
		return open.Grid
	}
	return defaultGrid
}

// displayInfos lists the configured display types with the kind and character
// grid a new variant starts from (ADR 0110).
func displayInfos(cfg *Config) []editorDisplayInfo {
	out := []editorDisplayInfo{}
	for _, name := range displayNames(cfg) {
		p := cfg.Profiles[name]
		kind := page.KindImage
		if p.text() {
			kind = page.KindText
		}
		out = append(out, editorDisplayInfo{Name: name, Kind: kind, Width: p.Width, Height: p.Height})
	}
	return out
}

// orphanLabel names the page of a variant that has none, for the script.
func orphanLabel(orphan bool, label string) string {
	if !orphan {
		return ""
	}
	return label
}

// editorStateData encodes the starting state for the script. The JSON is safe
// to inline: encoding/json escapes <, > and &, so a page title can never close
// the script element.
func editorStateData(state editorState) template.JS {
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
