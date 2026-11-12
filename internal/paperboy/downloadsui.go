package paperboy

import (
	_ "embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/maehld/paperboy/internal/semver"
)

// The releases page took the download card off the dashboard (ADR 0060, 0062):
// a running check used to reload every device card, and what a check installed
// belongs next to the firmware images rather than into the device grid.

//go:embed ui/downloads.html
var downloadsTemplate string

var downloadsTmpl = template.Must(template.New("downloads").Parse(downloadsTemplate))

// downloadsView is the releases page: the checks, the programs they installed
// and the images the devices run.
type downloadsView struct {
	Version         string
	GoVersion       string
	Configured      bool   // paperboy.yaml has a download section
	Source          string // release source
	Interval        string // "every 24 h"
	PluginChannel   string // stable or latest (ADR 0089)
	FirmwareChannel string // stable or latest (ADR 0089)
	Running         bool   // a check is running right now
	LastCheck       string // time of the last check, "never" before the first one
	LastOK          string // time of the last successful check, empty when unknown
	Error           string // last error, empty when the check succeeded
	Plugins         []downloadPluginRow
	Firmware        []downloadFirmwareRow
	Devices         []downloadDeviceRow
	Issues          []string // ignored files in the firmware directory

	// The inventory of plugins/ and firmware/ and its prune action (ADR 0090).
	InventoryPlugins  string // "7 files, 82 MB"
	InventoryFirmware string
	Preview           bool        // the prune preview is open
	Prune             []pruneFile // what the preview would delete
	Notice            string      // result of the last prune, empty before one
}

// downloadPluginRow is one configured plugin with the program the resolver
// picked and the version pinned in paperboy.yaml.
type downloadPluginRow struct {
	Name           string
	Version        string // downloaded version, empty for a hand-built or PATH program
	Pin            string // pinned version
	Program        string // file name of the program, "not found" when unresolved
	OutsideChannel bool   // a prerelease while the channel delivers finals only
}

// downloadFirmwareRow is one image in the firmware directory.
type downloadFirmwareRow struct {
	Hardware       string
	Version        string
	File           string
	OutsideChannel bool
}

// downloadDeviceRow is one device and the firmware it reports.
type downloadDeviceRow struct {
	ID       string
	Hardware string
	Firmware string
}

// handleDownloads shows the release checks, their result and what runs where.
func (s *Server) handleDownloads(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	loc := cfg.Timezone
	if loc == nil {
		loc = time.UTC
	}
	version, goVersion := editorVersion(cfg)
	v := downloadsView{Version: version, GoVersion: goVersion, Issues: s.firmware.invalidNames()}
	if cfg.Download != nil {
		v.Configured = true
		v.Source = cfg.Download.Source
		v.Interval = downloadIntervalText(cfg.Download.Interval)
		v.PluginChannel = cfg.Download.PluginChannel
		v.FirmwareChannel = cfg.Download.FirmwareChannel
		running, lastCheck, lastOK, lastErr, _, _ := s.download.view()
		v.Running = running
		v.LastCheck = downloadTime(lastCheck, loc)
		v.LastOK = downloadTime(lastOK, loc)
		v.Error = lastErr
	}
	specs := cfg.pluginSpecs()
	for _, p := range cfg.Plugins {
		row := downloadPluginRow{Name: p.Name, Program: "not found"}
		if spec, ok := specs[p.Name]; ok {
			row.Version = spec.Version
			if len(spec.Command) > 0 && spec.Command[0] != p.Name {
				row.Program = filepath.Base(spec.Command[0])
			}
		}
		if pin, ok := pluginPin(p.Version); ok {
			row.Pin = pin
		}
		row.OutsideChannel = cfg.Download != nil && outsideChannel(cfg.Download.PluginChannel, row.Version)
		v.Plugins = append(v.Plugins, row)
	}
	sort.Slice(v.Plugins, func(i, j int) bool { return v.Plugins[i].Name < v.Plugins[j].Name })
	for _, img := range s.firmware.installed() {
		v.Firmware = append(v.Firmware, downloadFirmwareRow{Hardware: img.Hardware, Version: img.Version, File: img.File,
			OutsideChannel: cfg.Download != nil && outsideChannel(cfg.Download.FirmwareChannel, img.Version)})
	}
	for _, d := range s.deviceInfos() {
		v.Devices = append(v.Devices, downloadDeviceRow{ID: d.ID, Hardware: d.Hardware, Firmware: firmwareText(d)})
	}
	sort.Slice(v.Devices, func(i, j int) bool { return v.Devices[i].ID < v.Devices[j].ID })

	pluginFiles, pluginBytes, firmwareFiles, firmwareBytes := inventorySummary(cfg)
	v.InventoryPlugins = inventoryText(pluginFiles, pluginBytes)
	v.InventoryFirmware = inventoryText(firmwareFiles, firmwareBytes)
	if r.URL.Query().Get("prune") != "" {
		v.Preview = true
		v.Prune, _ = s.prunePlan(cfg)
	}
	v.Notice = pruneNotice(r.URL.Query().Get("pruned"), r.URL.Query().Get("failed"))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := downloadsTmpl.Execute(w, v); err != nil {
		slog.Error("downloads page rendering failed", "error", err)
	}
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

// byteText prints a size in the largest unit that keeps it readable.
func byteText(bytes int64) string {
	switch {
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.0f kB", float64(bytes)/(1<<10))
	}
	return fmt.Sprintf("%d B", bytes)
}

// pruneNotice is the line the releases page shows after a prune.
func pruneNotice(deleted, failed string) string {
	removed, err := strconv.Atoi(deleted)
	if deleted == "" || err != nil {
		return ""
	}
	noun := "files"
	if removed == 1 {
		noun = "file"
	}
	line := fmt.Sprintf("Deleted %d %s.", removed, noun)
	if locked, err := strconv.Atoi(failed); err == nil && locked > 0 {
		line += fmt.Sprintf(" %d could not be removed (a running program holds them) and stay for the next preview.", locked)
	}
	return line
}

// downloadIntervalText prints the configured interval as "every 24 h" or
// "every 90 min", so the page needs no logic.
func downloadIntervalText(d time.Duration) string {
	if d <= 0 {
		d = defaultDownloadInterval
	}
	if d%time.Hour == 0 {
		return "every " + strconv.Itoa(int(d.Hours())) + " h"
	}
	return "every " + strconv.Itoa(int(d.Minutes())) + " min"
}
