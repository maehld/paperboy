package paperboy

import (
	_ "embed"
	"log/slog"
	"net/http"
	"sort"
	"time"
)

// aboutPage names what the installation runs and where its data comes from
// (docs/design/04-operator-ui.md): the Paperboy build, the licence, the
// configured plugins with their version and their data source, and the
// third-party code of the UI.
//
//go:embed ui/about.html
var aboutPage string

var aboutTmpl = pageTemplate("about", aboutPage, nil)

// aboutView is the content of the About page.
type aboutView struct {
	Version    string
	GoVersion  string
	Self       string
	Nav        []navItem
	Breadcrumb []crumb
	FileHint   string // the shell shows the source file of an editor page

	Revision string // short commit, with a +dirty marker for a changed worktree
	Built    string // the commit's time, in the configured timezone
	License  string
	Plugins  []aboutPlugin
	Code     []aboutCode
}

// aboutPlugin is one configured plugin with its version and data source.
type aboutPlugin struct {
	Name    string
	Version string
	Source  string // prose; the page shows it as it stands
	URL     string // the provider, empty when the source belongs to the operator
}

// aboutCode is one third-party component the software ships or links.
type aboutCode struct {
	Name    string
	License string
	URL     string
	Note    string // where it lives, empty for a linked library
}

// pluginSource names the data source of a shipped plugin. A source that runs
// in the operator's own network has no URL: nothing is fetched from a third
// party.
var pluginSources = map[string]aboutPlugin{
	"armory":     {Source: "Blizzard Entertainment and Raider.IO", URL: "https://develop.battle.net/documentation/world-of-warcraft/profile-apis"},
	"calendar":   {Source: "the operator's CalDAV, CardDAV and iCalendar sources"},
	"countdown":  {Source: "none, every date is an argument"},
	"departures": {Source: "the operator's EFA installation"},
	"dexcom":     {Source: "Dexcom Share", URL: "https://developer.dexcom.com/"},
	"evcc":       {Source: "the operator's evcc", URL: "https://evcc.io"},
	"fuel":       {Source: "Tankerkönig (MTS-K)", URL: "https://creativecommons.tankerkoenig.de"},
	"github":     {Source: "the GitHub REST API", URL: "https://docs.github.com/rest"},
	"myskoda":    {Source: "the MyŠkoda Public API", URL: "https://public.api.connect.skoda-auto.cz/docs"},
	"paperboy":   {Source: "none, it runs in Paperboy"},
	"pokedex":    {Source: "PokéAPI", URL: "https://pokeapi.co"},
	"prusalink":  {Source: "the operator's Prusa printer"},
	"rss":        {Source: "the feeds the operator names"},
	"unifi":      {Source: "the operator's UniFi controller"},
	"weather":    {Source: "Open-Meteo", URL: "https://open-meteo.com"},
	"xkcd":       {Source: "xkcd", URL: "https://xkcd.com"},
}

// aboutCodeList is the third-party code the software carries: the two files of
// the UI and the two Go modules (ADR 0013, 0024).
var aboutCodeList = []aboutCode{
	{Name: "htmx", License: "0BSD", URL: "https://htmx.org", Note: "in the operator UI"},
	{Name: "DotGothic16", License: "SIL Open Font License 1.1", URL: "https://fonts.google.com/specimen/DotGothic16", Note: "the pixel font of the text previews"},
	{Name: "golang.org/x/image", License: "BSD-3-Clause", URL: "https://pkg.go.dev/golang.org/x/image"},
	{Name: "go.yaml.in/yaml/v3", License: "Apache-2.0 and MIT", URL: "https://pkg.go.dev/go.yaml.in/yaml/v3"},
}

// handleAbout shows the About page.
func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	loc := timezoneOf(cfg)
	_, goVersion := editorVersion(cfg)
	v := aboutView{
		Version: buildName(build), GoVersion: goVersion, Self: r.URL.RequestURI(),
		Nav:        navFor("About"),
		Breadcrumb: crumbPath(crumb{Label: "About"}),
		Revision:   shortRevision(build.Revision, build.Modified),
		Built:      commitTime(build.At, loc),
		License:    "MIT",
		Code:       aboutCodeList,
	}

	// The built-in plugin runs in the Paperboy process and is never configured,
	// so it has no row of its own (ADR 0049).
	v.Plugins = append(v.Plugins, aboutPlugin{Name: "paperboy", Version: "built-in",
		Source: pluginSources["paperboy"].Source})

	specs := cfg.pluginSpecs()
	for _, p := range cfg.Plugins {
		row := aboutPlugin{Name: p.Name}
		if spec, ok := specs[p.Name]; ok {
			row.Version = spec.Version
		}
		if source, ok := pluginSources[p.Name]; ok {
			row.Source, row.URL = source.Source, source.URL
		}
		v.Plugins = append(v.Plugins, row)
	}
	sort.Slice(v.Plugins, func(i, j int) bool { return v.Plugins[i].Name < v.Plugins[j].Name })

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := aboutTmpl.Execute(w, v); err != nil {
		slog.Error("about page rendering failed", "error", err)
	}
}

// buildName is the release of the binary: the tag of a release build, "dev"
// for a working-tree build whose Go stamp is a pseudo-version (ADR 0056).
func buildName(v buildVersion) string {
	name := v.Version
	if name == "" || name == "(devel)" || isPseudoVersion(name) {
		return "dev"
	}
	return name
}

// shortRevision is the first characters of the commit hash, marked when the
// build carried uncommitted changes.
func shortRevision(revision string, modified bool) string {
	if revision == "" {
		return ""
	}
	if len(revision) > 9 {
		revision = revision[:9]
	}
	if modified {
		revision += "+dirty"
	}
	return revision
}

// commitTime formats the commit's time in the configured timezone.
func commitTime(at string, loc *time.Location) string {
	if at == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return ""
	}
	if loc == nil {
		loc = time.UTC
	}
	return t.In(loc).Format("2006-01-02 15:04")
}
