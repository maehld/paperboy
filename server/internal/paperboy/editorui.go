package paperboy

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/maehld/paperboy/server/internal/page"
)

// The page editor on top of the page API (ADR 0066): an overview of the page
// files and one page opened for a display type (ADR 0068). Both are
// server-rendered templates; the editing itself is the embedded editor.js,
// which works on the JSON model of the API.

//go:embed ui/editor.html
var editorListTemplate string

var editorListTmpl = pageTemplate("editor", editorListTemplate, nil)

//go:embed ui/editor-page.html
var editorPageTemplate string

var editorPageTmpl = pageTemplate("editor-page", editorPageTemplate, template.FuncMap{
	"inc": func(i int) int { return i + 1 },
})

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
	Note     string // the row cannot be opened
	Missing  bool   // the page has no page file; its variants cannot be edited
	PageURL  string // the page file in the editor; empty when it is missing
	Variants []editorVariant
}

type editorListPage struct {
	Version    string
	GoVersion  string
	Nav        []navItem // the rail, with the active section (ADR 0150)
	Breadcrumb []crumb   // the path to this page
	Groups     []editorPageGroup
	Kinds      []uiOption         // kinds of the new-page form
	Grids      []uiOption         // character grids of a text page, by display type
	Plans      []editorPlanRow    // the plan files next to the pages (ADR 0069)
	Devices    []editorDeviceRow  // the device files (ADR 0070)
	Unclaimed  []editorUnclaimed  // state files whose MAC no device file uses (ADR 0080)
	Types      []editorDisplayRow // the display type files (ADR 0071)
	Tab        string             // the open sub-tab
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
	Version        string
	GoVersion      string
	Nav            []navItem // the rail, with the active section (ADR 0150)
	Breadcrumb     []crumb   // the path to this page
	Name           string
	Display        string // the preview type (ADR 0110)
	Text           bool
	Error          string
	File           string        // the label of the open file; empty: the page file
	FilePath       string        // pages/<name>[.<label>].yaml
	Notice         string        // the preview type is not the file the editor shows
	NoticeWarn     bool          // the notice is a warning, not a hint
	Orphan         bool          // the open variant has no page file
	NoPage         bool          // the page has no page file
	IsVariant      bool          // a variant is open; the page file owns the map
	VariantTypes   []displayLink // display types that name the open variant
	PageTitle      string        // title of the page file, shown read-only on a variant
	WidgetsPage    bool          // the widgets page without a page (ADR 0055)
	TextFallback   bool          // the Playground fell back from a text display type (ADR 0124)
	NoImageDisplay bool          // the Playground has no image display type to show (ADR 0124)
	TextPage       bool          // a text page: rows are edited instead of cells
	Cells          []page.Cell   // server-rendered boxes for the no-JS view
	Displays       []displayLink // display type switch of the widgets page
	Serves         []servesRow   // each configured display type and its file
	CreateTypes    []variantType // the display types the create dialog offers
	CreateLabel    string        // the label the create dialog starts with
	CreateOK       bool          // a display type waits for a variant
	ReassignPages  []uiOption    // the other pages with a page file (ADR 0110)
	Data           template.JS   // the starting state of the script
}

// servesRow is one configured display type in the Variants table: the file
// that serves it, the files it could be sent to and the way to preview it with
// the open file (ADR 0110).
type servesRow struct {
	Display  string
	Kind     string
	Value    string // the file that serves the type: a label, or empty for the page file
	URL      string // preview with the open file, or open the file that serves the type
	Preview  bool   // the URL previews the type with the open file
	Current  bool   // that preview is the type this view renders
	URLTitle string // what the link does, and for which file
	Options  []uiOption
	Title    string // the file that serves the type, or why none does
	Reason   bool   // the type is not served
}

// variantType is one entry of the display type select in the Create variant
// dialog. A type that a file already serves is disabled, because the select in
// Variants is the place to change it (ADR 0110).
type variantType struct {
	Name     string // display type
	Label    string // default label of the new file
	Served   string // path of the serving file; empty when a variant can be created
	Selected bool   // the dialog starts with this type
}

// displayLink is one entry of a display type switch: the widgets page for the
// playground and the variant block for a file that several types serve.
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
	Disabled bool
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
	PageHash    string              `json:"page_hash,omitempty"`  // hash of the page file, for a new variant
	PageExists  bool                `json:"page_exists"`          // the page file exists
	PageTitle   string              `json:"page_title,omitempty"` // the title of the page file, shown read-only on a variant
	Named       bool                `json:"named,omitempty"`      // the page names this variant
	Displays    []editorDisplayInfo `json:"displays,omitempty"`   // configured display types (ADR 0110)
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
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	Grid       *[2]int `json:"grid,omitempty"`        // default grid of a new page (ADR 0128)
	ScrollStep int     `json:"scroll_step,omitempty"` // ms between marquee steps (ADR 0118)
}

// displayNames lists the configured display types alphabetically.
func displayNames(cfg *Config) []string {
	return sortedDisplays(cfg.Profiles)
}

// imageDisplayNames lists the configured image display types alphabetically.
// The Playground offers only these: text pages are authored in the page editor
// (ADR 0124).
func imageDisplayNames(cfg *Config) []string {
	names := []string{}
	for _, n := range displayNames(cfg) {
		if !cfg.Profiles[n].text() {
			names = append(names, n)
		}
	}
	return names
}

func sortedDisplays(displays map[string]Profile) []string {
	return slices.Sorted(maps.Keys(displays))
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
// files next to it (ADR 0110). A page whose page file is missing stays listed
// in red; its variants are read-only until the operator creates the page file
// (ADR 0112).
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
	if shown.Rows > 0 {
		g.Grid = fmt.Sprintf("%d rows", shown.Rows)
	} else if shown.Grid != [2]int{} {
		g.Grid = fmt.Sprintf("%dx%d", shown.Grid[0], shown.Grid[1])
	}
	if page != nil {
		g.PageURL, _ = pageEditURL(*page, variants, displays, false)
	}
	for _, f := range files {
		if f.Label == "" {
			continue // the page file is the row, not a chip
		}
		v := editorVariant{Label: f.Label, Title: pageFileName(name, f.Label)}
		serves := servesTypes(variants, f.Label)
		unnamedFile := len(serves) == 0
		switch {
		case len(serves) > 0:
			v.Title += " — serves " + strings.Join(serves, ", ")
		default:
			v.Title += " — no display type names it"
		}
		if f.Error != "" {
			v.Title += " — " + f.Error
		}
		switch {
		case f.Orphan:
			v.Class = "orphan"
		case f.Error != "":
			v.Class = "broken"
		case unnamedFile:
			// A file the page does not name is still a candidate in the Served
			// by select; the style says that nothing uses it yet (ADR 0110).
			v.Class = "unused"
		}
		// A variant without a page file stays read-only: it cannot be used until
		// the operator creates the page file (ADR 0112).
		if page != nil {
			v.URL, _ = pageEditURL(f, variants, displays, false)
		}
		g.Variants = append(g.Variants, v)
	}
	switch {
	case page == nil:
		g.Missing = true
	case shown.Error != "":
		g.Error = pageFileName(name, shown.Label) + ": " + shown.Error
	case g.PageURL == "":
		g.Note = "No configured display type can render this file."
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

// variantTypeLinks lists the display types that name a variant, each as the
// preview of the open file. The type supplies the kind, pixels and ppi, so a
// type the file cannot render stays a name without a link (ADR 0110).
func variantTypeLinks(cfg *Config, name, label, preview string, open *editorPageFile, variants map[string]string) []displayLink {
	var out []displayLink
	for _, display := range servesTypes(variants, label) {
		link := displayLink{Name: display, Selected: display == preview}
		if open != nil {
			if p, ok := cfg.Profiles[display]; ok && fileFits(*open, p) {
				link.URL = objectURL("/ui/pages", name, url.Values{"file": {label}, "display": {display}})
			}
		}
		out = append(out, link)
	}
	return out
}

// reassignTargets lists the pages a variant can move to: every page with a page
// file except its own, because the moving entries go into that file's map
// (ADR 0110).
func reassignTargets(files []editorPageFile, name string) []uiOption {
	out := []uiOption{}
	for _, f := range files {
		if f.Label != "" || f.Name == name {
			continue
		}
		label := f.Name
		if f.Title != "" {
			label += " · " + f.Title
		}
		out = append(out, uiOption{Value: f.Name, Label: label})
	}
	return out
}

// fileFits reports whether a page file can render a display type: the same kind
// and, for text, rows that fit the character grid (ADR 0111).
func fileFits(f editorPageFile, p Profile) bool {
	if f.Error != "" || f.Kind == "" {
		return true // a broken file opens read-only, so any type can show it
	}
	if p.text() != (f.Kind == page.KindText) {
		return false
	}
	if !p.text() {
		return true
	}
	if f.Rows > 0 {
		return f.Rows <= p.Height && f.textWidth <= p.Width
	}
	return f.Grid == [2]int{p.Width, p.Height}
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
	return objectURL("/ui/pages", f.Name, params), ""
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

// noFile is the select value of a display type that no file of the page serves;
// it is never sent, because the option is disabled.
const noFile = "__none__"

// servesTable lists every display type with the files of the page that fit it
// (ADR 0110). The select writes the page's variants entry, so it holds the file
// that serves the type; "— none —" removes the entry. An entry whose display
// type is no longer configured keeps its row, so the operator can remove it.
// The type opens its preview with the open file, or the file that serves it:
// the editor never renders another file in place of the open one.
func servesTable(cfg *Config, name, label, preview string, files []editorPageFile, coverage []pageCoverageEntry, mapEntries map[string]string, pageExists bool) []servesRow {
	var pageFile *editorPageFile
	var variants []editorPageFile
	for i := range files {
		if files[i].Name != name {
			continue
		}
		if files[i].Label == "" {
			pageFile = &files[i]
		} else {
			variants = append(variants, files[i])
		}
	}
	rows := []servesRow{}
	for _, c := range coverage {
		p := cfg.Profiles[c.Display]
		row := servesRow{Display: c.Display, Kind: c.Kind, Title: pageFileName(name, c.File)}
		serves := !c.Missing && c.Error == ""
		switch {
		case serves:
			row.Value = c.File
			if c.File == label && pageExists {
				params := url.Values{"name": {name}, "display": {c.Display}}
				if label != "" {
					params.Set("file", label)
				}
				row.URL = objectURL("/ui/pages", name, params)
				row.Preview, row.Current = true, c.Display == preview
				row.URLTitle = "Preview " + c.Display + " with this file"
			} else if c.File != label {
				row.URL, _ = pageEditURL(editorPageFile{Name: name, Label: c.File}, coverageVariants(coverage), cfg.Profiles, false)
				row.URLTitle = "Edit " + pageFileName(name, c.File) + " for " + c.Display
			}
		case c.Error != "":
			row.Reason = true
			// The reason stays out of the column: the select carries it in its
			// tooltip, on one line and without a red block.
			row.Title = c.Error
			if c.File != "" {
				// The page names this file. It stays the value, so the row shows
				// what is broken and the select can drop the entry.
				row.Value = c.File
				if !c.Missing {
					row.Title = pageFileName(name, c.File) + ": " + c.Error
					row.URL, _ = pageEditURL(editorPageFile{Name: name, Label: c.File}, coverageVariants(coverage), cfg.Profiles, false)
					row.URLTitle = "Edit " + pageFileName(name, c.File) + " for " + c.Display
				}
			}
		}
		// The options are the files of the page that fit the type; a broken file
		// never is, so the select cannot point a type at a file that does not
		// parse (ADR 0110).
		if pageExists && pageFile != nil && pageFile.Error == "" && fileFits(*pageFile, p) {
			row.Options = append(row.Options, uiOption{Value: "", Label: "page file"})
		}
		for _, f := range variants {
			if f.Error != "" || !fileFits(f, p) {
				continue
			}
			row.Options = append(row.Options, uiOption{Value: f.Label, Label: f.Label})
		}
		// A file the map names stays an option, so its row is readable and can
		// be changed even when the file is gone.
		if row.Value != "" && !hasOption(row.Options, row.Value) {
			row.Options = append(row.Options, uiOption{Value: row.Value, Label: row.Value})
		}
		// Without the page file as an option, "none" is how the entry goes away.
		if !hasOption(row.Options, "") {
			row.Options = append(row.Options, uiOption{Value: noFile, Label: "— none —"})
		}
		selectOption(&row)
		rows = append(rows, row)
	}
	// An entry for a display type that no longer exists keeps a row, so the
	// operator sees and removes it (ADR 0110).
	for display, file := range mapEntries {
		if knownProfile(cfg.Profiles, display) {
			continue
		}
		row := servesRow{Display: display, Kind: "—", Value: file, Title: pageFileName(name, file), Reason: true}
		for _, f := range files {
			if f.Name == name && f.Label == file && f.Kind != "" {
				row.Kind = f.Kind
			}
		}
		row.Options = []uiOption{{Value: file, Label: file}, {Value: noFile, Label: "— none —"}}
		selectOption(&row)
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Display < rows[j].Display })
	return rows
}

// hasOption reports whether the select already offers a value.
func hasOption(options []uiOption, value string) bool {
	for _, o := range options {
		if o.Value == value {
			return true
		}
	}
	return false
}

// selectOption marks the current value and falls back to "none" when no option
// holds it.
func selectOption(row *servesRow) {
	for i := range row.Options {
		if row.Options[i].Value == row.Value {
			row.Options[i].Selected = true
			return
		}
	}
	for i := range row.Options {
		if row.Options[i].Value == noFile {
			row.Options[i].Selected = true
			row.Value = noFile
			return
		}
	}
}

// createVariantTypes lists the display types the Create variant dialog offers.
// Every configured type is a candidate: a variant is the exception for a type,
// also when the page file serves it today. The entry names the file that serves
// the type, so a new variant is a deliberate choice (ADR 0110).
func createVariantTypes(name string, coverage []pageCoverageEntry) []variantType {
	types := []variantType{}
	for _, c := range coverage {
		t := variantType{Name: c.Display, Label: c.Display}
		switch {
		case !c.Missing && c.Error == "":
			t.Served = pageFileName(name, c.File)
		case c.File != "":
			t.Label = c.File // a missing file is repaired under its own label
		}
		types = append(types, t)
	}
	// The dialog starts with the first type that no file serves, else with the
	// first one (ADR 0110).
	for i := range types {
		if types[i].Served == "" {
			types[i].Selected = true
			return types
		}
	}
	if len(types) > 0 {
		types[0].Selected = true
	}
	return types
}

// previewNotice says why the preview type is not served by the file the editor
// shows. A hand-written link can name any type; the editor keeps the notice
// short and still renders only the open file (ADR 0110).
func previewNotice(cfg *Config, name, label, display string, coverage []pageCoverageEntry, open editorPageFile) string {
	other := ""
	for _, c := range coverage {
		if c.Display != display {
			continue
		}
		if !c.Missing && c.Error == "" {
			if c.File == label {
				return ""
			}
			other = pageFileName(name, c.File)
		}
	}
	if !fileFits(open, cfg.Profiles[display]) {
		return fmt.Sprintf("this file does not render display type %q", display)
	}
	if other != "" {
		return fmt.Sprintf("display type %q is served by %s; this file is not used for it", display, other)
	}
	return fmt.Sprintf("this file is not the layout for display type %q", display)
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

// displayOfKind returns a configured display type of the given kind, so a
// caller can pick a preview or a character grid without knowing a name. An
// unknown or empty kind returns "".
func displayOfKind(displays map[string]Profile, kind string) string {
	for _, n := range sortedDisplays(displays) {
		if (displays[n].Kind == kindText) == (kind == page.KindText) {
			return n
		}
	}
	return ""
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
	switch tab {
	case "plans":
		v.Nav = navFor("Plans")
	case "devices":
		v.Nav = navFor("Devices")
	case "displays":
		v.Nav = navFor("Devices")
		v.Breadcrumb = []crumb{{Label: "Devices", URL: "/ui"}, crumb{Label: "Display types"}}
	default:
		v.Nav = navFor("Pages")
	}
	for _, p := range plans {
		row := editorPlanRow{Name: p.Name, Rules: p.Rules, Fallback: p.Fallback, Error: p.Error}
		if p.Error == "" {
			row.OpenURL = objectURL("/ui/plans", p.Name, nil)
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
			row.OpenURL = objectURL("/ui/devices", d.ID, nil)
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
			ClaimURL:  objectURL("/ui/devices", suggestDeviceID(u), url.Values{"new": {"1"}, "mac": {u.MAC}}),
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
			row.OpenURL = objectURL("/ui/display-types", d.Name, nil)
		}
		v.Types = append(v.Types, row)
	}
	// The new-page form asks for the kind: a page file serves its kind, not one
	// display type (ADR 0110). A text page still needs a character grid, so the
	// text display types name theirs.
	for _, kind := range []string{page.KindImage, page.KindText} {
		if displayOfKind(cfg.Profiles, kind) != "" {
			v.Kinds = append(v.Kinds, uiOption{Value: kind, Label: kind, Selected: len(v.Kinds) == 0})
		}
	}
	for _, n := range displayNames(cfg) {
		if p := cfg.Profiles[n]; p.text() {
			v.Grids = append(v.Grids, uiOption{Value: n, Label: fmt.Sprintf("%d×%d (%s)", p.Width, p.Height, n)})
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := editorListTmpl.Execute(w, v); err != nil {
		slog.Error("page overview rendering failed", "error", err)
	}
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
	v := editorPageView{Version: version, GoVersion: goVersion, Name: name, File: label,
		Nav:        navFor("Pages"),
		Breadcrumb: crumbPath(crumb{Label: "Pages", URL: "/ui/pages"}, crumb{Label: name}),
	}
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
	kindParam := q.Get("kind")
	if kindParam != "" && kindParam != page.KindImage && kindParam != page.KindText {
		v.Error = fmt.Sprintf("kind %q: expected image or text", kindParam)
		renderEditorPage(w, v, http.StatusBadRequest)
		return
	}
	if newPage && kindParam != "" {
		// The new-page dialog asks for the kind; the chosen display type keeps
		// the grid a new page starts with, its character grid for a text page
		// and its default grid for an image page (ADR 0110, 0128).
		if display != "" && cfg.Profiles[display].text() == (kindParam == page.KindText) {
			// keep the chosen display type and its grid
		} else {
			display = displayOfKind(cfg.Profiles, kindParam)
			if display == "" {
				v.Error = fmt.Sprintf("no display type of kind %q is configured", kindParam)
				renderEditorPage(w, v, http.StatusBadRequest)
				return
			}
		}
	}
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
	// A text page has no pixel grid: its rows are laid out on the character
	// grid of a text display (ADR 0111).
	if kind == page.KindText && !p.text() {
		v.Error = fmt.Sprintf("a text page needs a text display type; %s is an image display", display)
		renderEditorPage(w, v, http.StatusBadRequest)
		return
	}
	context := editorPageFile{Name: name, Label: label, Kind: kind}
	if kind == page.KindText {
		v.TextPage = true
		if open != nil && open.Error == "" {
			if data, err := os.ReadFile(path); err == nil {
				var t *page.Template
				if label == "" {
					t, err = page.Parse(bytes.NewReader(data))
				} else {
					t, err = page.ParseVariant(bytes.NewReader(data))
				}
				if err == nil {
					v.Cells = t.Cells
				}
			}
		}
	}
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
	// The page file owns the variants map, so only it lists the Variants table.
	// A variant points back to its page (ADR 0110). The display type switch
	// belongs to the widgets page only.
	v.IsVariant = label != ""
	if v.IsVariant {
		v.VariantTypes = variantTypeLinks(cfg, name, label, display, open, variants)
		v.PageTitle = name
		if pageFile != nil && pageFile.Title != "" {
			v.PageTitle = pageFile.Title
		}
		// A variant of a page can change its label or move to another page; a
		// file without a page file stays read-only (ADR 0110, 0112).
		if pageFile != nil {
			v.ReassignPages = reassignTargets(files, name)
		}
	} else {
		v.Serves = servesTable(cfg, name, label, display, files, coverage, variants, pageExists)
		v.CreateTypes = createVariantTypes(name, coverage)
		// Every configured type is a candidate: the dialog creates the file and
		// sends the type to it (ADR 0110).
		v.CreateOK = pageExists && len(v.CreateTypes) > 0
		for _, t := range v.CreateTypes {
			if t.Selected {
				v.CreateLabel = t.Label
			}
		}
	}
	named := false
	for _, l := range variants {
		if label != "" && l == label {
			named = true
		}
	}
	// A variant that no display type names never reaches a device. That is a
	// warning of its own, not the notice about a preview type (ADR 0110).
	unused := label != "" && pageExists && !named
	switch {
	case unused:
		v.Notice = "This variant is unused. It is not shown on any device."
		v.NoticeWarn = true
	case q.Get("display") != "" && !missing:
		v.Notice = previewNotice(cfg, name, label, display, coverage, context)
	}
	plans, err := planFiles(cfg.Plans, cfg.Timezone)
	if err != nil {
		slog.Warn("plan list for the page editor failed", "error", err)
	}
	usedBy := plansByPage(plans)[name]
	pageHash := ""
	if pageExists {
		pageHash = pageFile.Hash
	}
	v.Data = editorStateData(editorState{
		Name: name, Display: display, File: label, Kind: kind,
		Width: p.Width, Height: p.Height, New: missing, Reference: defaultGrid,
		Grid:       pageGrid(kind, p, *openOrBlank(open, name, label)),
		PageHash:   pageHash,
		PageExists: pageExists,
		PageTitle:  v.PageTitle,
		Named:      named,
		Displays:   displayInfos(cfg),
		UsedBy:     usedBy,
		// A variant carries no page rename: its own Rename changes the label,
		// and the page is renamed from its page file (ADR 0110).
		Rename: renameForPage(cfg, name, label),
		Orphan: orphanLabel(v.Orphan, label),
	})
	renderEditorPage(w, v, http.StatusOK)
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
// display, else the grid of the open file or the display type's default
// (ADR 0128).
func pageGrid(kind string, p Profile, open editorPageFile) [2]int {
	if kind == page.KindText {
		return [2]int{p.Width, p.Height}
	}
	if open.Grid != [2]int{} {
		return open.Grid
	}
	return gridOf(p)
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
		info := editorDisplayInfo{Name: name, Kind: kind, Width: p.Width, Height: p.Height}
		if p.text() {
			info.ScrollStep = int(p.ScrollStep / time.Millisecond)
		} else {
			g := gridOf(p)
			info.Grid = &g
		}
		out = append(out, info)
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
// page is always the image grid, because the Playground offers image display
// types only (ADR 0124). The types of the args are read in the script, where the
// plugin schema is known.
func widgetsStateData(display string, p Profile, widget string, span, grid [2]int, args map[string]string) template.JS {
	g := gridOf(p)
	if grid != [2]int{} {
		g = grid // a page's grid, so the preview matches its cell
	}
	state := editorState{Display: display, WidgetsPage: true, Widget: widget, Args: args,
		Reference: defaultGrid, Width: p.Width, Height: p.Height,
		Kind: page.KindImage, Grid: g}
	if span[0] > 0 && span[1] > 0 {
		state.Span = &span
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
