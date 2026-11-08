package paperboy

import (
	_ "embed"
	"html/template"
	"log/slog"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"time"
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
	Version    string
	GoVersion  string
	Configured bool   // paperboy.yaml has a download section
	Source     string // release source
	Interval   string // "every 24 h"
	Running    bool   // a check is running right now
	LastCheck  string // time of the last check, "never" before the first one
	LastOK     string // time of the last successful check, empty when unknown
	Error      string // last error, empty when the check succeeded
	Plugins    []downloadPluginRow
	Firmware   []downloadFirmwareRow
	Devices    []downloadDeviceRow
	Issues     []string // ignored files in the firmware directory
}

// downloadPluginRow is one configured plugin with the program the resolver
// picked and the version pinned in paperboy.yaml.
type downloadPluginRow struct {
	Name    string
	Version string // downloaded version, empty for a hand-built or PATH program
	Pin     string // pinned version
	Program string // file name of the program, "not found" when unresolved
}

// downloadFirmwareRow is one image in the firmware directory.
type downloadFirmwareRow struct {
	Hardware string
	Version  string
	File     string
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
		v.Plugins = append(v.Plugins, row)
	}
	sort.Slice(v.Plugins, func(i, j int) bool { return v.Plugins[i].Name < v.Plugins[j].Name })
	for _, img := range s.firmware.installed() {
		v.Firmware = append(v.Firmware, downloadFirmwareRow{Hardware: img.Hardware, Version: img.Version, File: img.File})
	}
	for _, d := range s.deviceInfos() {
		v.Devices = append(v.Devices, downloadDeviceRow{ID: d.ID, Hardware: d.Hardware, Firmware: firmwareText(d)})
	}
	sort.Slice(v.Devices, func(i, j int) bool { return v.Devices[i].ID < v.Devices[j].ID })

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := downloadsTmpl.Execute(w, v); err != nil {
		slog.Error("downloads page rendering failed", "error", err)
	}
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
