package paperboy

import (
	_ "embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/maehld/paperboy/server/internal/semver"
)

// The release trains have a page each (ADR 0154): the check that installs them,
// what the instance runs, and the size of the directory. The cleanup of that
// directory lives on the maintenance page (ADR 0090).

//go:embed ui/plugins.html
var pluginsPageTemplate string

var pluginsPageTmpl = pageTemplate("plugins", pluginsPageTemplate, nil)

//go:embed ui/firmware.html
var firmwarePageTemplate string

var firmwarePageTmpl = pageTemplate("firmware", firmwarePageTemplate, nil)

// trainPage is one release train. The check belongs to both trains at once, so
// the page names both channels and both bundles (ADR 0060, 0089).
type trainPage struct {
	Version    string
	GoVersion  string
	Nav        []navItem // the rail, with the active section (ADR 0152)
	Breadcrumb []crumb   // the path to this page
	Self       string    // this page's URI, so a poll keeps its query string (ADR 0106)

	Kind  string // "plugins" or "firmware", for the ids of the page
	Title string

	Configured      bool   // paperboy.yaml has a download section
	Source          string // release source
	Interval        string // "every 24 h"
	PluginChannel   string // stable or latest (ADR 0089)
	FirmwareChannel string
	PluginBundle    string // release tag of the last plugin bundle the check used
	FirmwareBundle  string
	Running         bool   // a check is running right now
	LastCheck       string // time of the last check, "never" before the first one
	LastOK          string // time of the last successful check, empty when unknown
	Error           string // last error, empty when the check succeeded
	Updates         string // what the check says about Paperboy itself (ADR 0101)
	UpdateURL       string // release page of that version, empty when unknown

	Plugins   []downloadPluginRow
	Firmware  []downloadFirmwareRow
	Inventory string   // size of the train's directory
	Issues    []string // ignored files in the firmware directory
}

// downloadPluginRow is one configured plugin with the program the resolver
// picked and whether paperboy.yaml pins its version.
type downloadPluginRow struct {
	Name           string
	Version        string // downloaded version, empty for a hand-built or PATH program
	Pinned         bool   // paperboy.yaml pins the version
	Program        string // file name of the program, "not found" when unresolved
	Downloaded     string // when the program file arrived, empty when unknown
	OutsideChannel bool   // a prerelease while the channel delivers finals only
}

// downloadFirmwareRow is one image in the firmware directory.
type downloadFirmwareRow struct {
	Hardware       string
	Version        string
	File           string
	Downloaded     string // when the image arrived, empty when unknown
	Pinned         bool   // firmware.version names this image
	OutsideChannel bool
}

// handlePluginsPage shows the plugin train: the check, the configured plugins
// and the size of plugins/.
func (s *Server) handlePluginsPage(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	loc := timezoneOf(cfg)
	version, goVersion := editorVersion(cfg)
	v := trainPage{Version: version, GoVersion: goVersion, Self: r.URL.RequestURI(),
		Kind: "plugins", Title: "Plugins",
		Nav:        navFor("Plugins"),
		Breadcrumb: crumbPath(crumb{Label: "Plugins"})}
	s.fillCheck(&v, cfg, loc)
	specs := cfg.pluginSpecs()
	for _, p := range cfg.Plugins {
		row := downloadPluginRow{Name: p.Name, Program: "not found"}
		if spec, ok := specs[p.Name]; ok {
			row.Version = spec.Version
			if len(spec.Command) > 0 {
				if spec.Command[0] != p.Name {
					row.Program = filepath.Base(spec.Command[0])
				}
				row.Downloaded = fileTime(spec.Command[0], s.now(), loc)
			}
		}
		_, row.Pinned = pluginPin(p.Version)
		// A pin bypasses the channel (ADR 0089), so the note would contradict
		// the marker.
		row.OutsideChannel = cfg.Download != nil && !row.Pinned &&
			outsideChannel(cfg.Download.PluginChannel, row.Version)
		v.Plugins = append(v.Plugins, row)
	}
	sort.Slice(v.Plugins, func(i, j int) bool { return v.Plugins[i].Name < v.Plugins[j].Name })
	files, bytes := inventorySize(cfg.PluginsDir)
	v.Inventory = inventoryText(files, bytes)
	s.renderTrain(w, pluginsPageTmpl, v)
}

// handleFirmwarePage shows the firmware train: the check, the images the
// devices run and the size of firmware/.
func (s *Server) handleFirmwarePage(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	loc := timezoneOf(cfg)
	version, goVersion := editorVersion(cfg)
	v := trainPage{Version: version, GoVersion: goVersion, Self: r.URL.RequestURI(),
		Kind: "firmware", Title: "Firmware",
		Nav:        navFor("Firmware"),
		Breadcrumb: crumbPath(crumb{Label: "Firmware"}),
		Issues:     s.firmware.invalidNames()}
	s.fillCheck(&v, cfg, loc)
	pinned, hasPin := pinVersion(cfg.Firmware.Version)
	for _, img := range s.firmware.installed() {
		row := downloadFirmwareRow{Hardware: img.Hardware, Version: img.Version, File: img.File,
			Downloaded: fileTime(img.path, s.now(), loc),
			Pinned:     hasPin && semver.Compare(pinned, img.ver) == 0}
		row.OutsideChannel = cfg.Download != nil && !row.Pinned &&
			outsideChannel(cfg.Download.FirmwareChannel, row.Version)
		v.Firmware = append(v.Firmware, row)
	}
	files, bytes := inventorySize(cfg.FirmwareDir)
	v.Inventory = inventoryText(files, bytes)
	s.renderTrain(w, firmwarePageTmpl, v)
}

// fillCheck puts the state of the release check into a train page. One check
// covers both trains, so a page that shows its own channel also names the
// other one (ADR 0060).
func (s *Server) fillCheck(v *trainPage, cfg *Config, loc *time.Location) {
	if cfg.Download == nil {
		return
	}
	v.Configured = true
	v.Source = cfg.Download.Source
	v.Interval = downloadIntervalText(cfg.Download.Interval)
	v.PluginChannel = cfg.Download.PluginChannel
	v.FirmwareChannel = cfg.Download.FirmwareChannel
	running, lastCheck, lastOK, lastErr, _, _, update := s.download.view()
	v.Running = running
	v.LastCheck = downloadTime(lastCheck, s.now(), loc)
	v.LastOK = downloadTime(lastOK, s.now(), loc)
	v.Error = lastErr
	v.Updates, v.UpdateURL = paperboyUpdateText(update)
	v.PluginBundle, v.FirmwareBundle = s.download.bundles()
}

func (s *Server) renderTrain(w http.ResponseWriter, tmpl *template.Template, v trainPage) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := tmpl.Execute(w, v); err != nil {
		slog.Error("release train page rendering failed", "train", v.Kind, "error", err)
	}
}

// timezoneOf is the location the configuration uses for its texts, UTC before
// the configuration named one.
func timezoneOf(cfg *Config) *time.Location {
	if cfg.Timezone == nil {
		return time.UTC
	}
	return cfg.Timezone
}

// fileTime prints when a program or image was written into the instance: for a
// file Paperboy downloaded that is the time of the download, for one dropped in
// by hand the time of the copy. Empty when the file cannot be read.
func fileTime(path string, now time.Time, loc *time.Location) string {
	info, err := os.Stat(path)
	if err != nil || info.ModTime().IsZero() {
		return ""
	}
	return downloadTime(info.ModTime(), now, loc)
}

// outsideChannel reports whether a version in use is one the channel would not
// deliver: an rc while the channel follows the final releases (ADR 0089).
func outsideChannel(channel, running string) bool {
	if channel != channelStable || running == "" {
		return false
	}
	parsed, ok := semver.Parse(strings.TrimPrefix(running, "v"))
	return ok && parsed.Prerelease()
}

// paperboyUpdateText is the line about Paperboy's own release: the newer
// version, which the template links, or why there is none (ADR 0101).
func paperboyUpdateText(update paperboyUpdate) (text, url string) {
	switch {
	case !update.Checked:
		return "not checked yet", ""
	case !update.Compared:
		return "dev build, no comparison", ""
	case !update.Found:
		return "no release in the channel", ""
	case update.Version != "":
		return update.Version + " is available", update.URL
	}
	return "up to date", ""
}

// inventoryText prints the size of one inventory directory for the page.
func inventoryText(files, bytes int64) string {
	if files == 0 {
		return "empty"
	}
	noun := "files"
	if files == 1 {
		noun = "file"
	}
	return fmt.Sprintf("%d %s, %s", files, noun, byteText(bytes))
}

// downloadIntervalText prints the check interval for the page.
func downloadIntervalText(d time.Duration) string {
	if d <= 0 {
		d = defaultDownloadInterval
	}
	if d%time.Hour == 0 {
		return "every " + strconv.Itoa(int(d.Hours())) + " h"
	}
	return "every " + strconv.Itoa(int(d.Minutes())) + " min"
}

// byteText prints a size for the pages.
func byteText(bytes int64) string {
	switch {
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.0f kB", float64(bytes)/(1<<10))
	}
	return fmt.Sprintf("%d B", bytes)
}
