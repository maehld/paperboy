package paperboy

import (
	_ "embed"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Maintenance is where Paperboy deletes what it no longer needs (ADR 0154): the
// files of the two inventories, the state of devices that have no device file
// and the editor backups of objects that are gone. Every action previews first
// and touches nothing outside plugins/, firmware/ and state/.

//go:embed ui/maintenance.html
var maintenanceTemplate string

var maintenanceTmpl = pageTemplate("maintenance", maintenanceTemplate, nil)

// staleDeviceAge is how long the state of a device without a device file stays.
// A device that registers is unclaimed for a while and must keep its state
// until the operator claims it (ADR 0080), so only old entries go.
const staleDeviceAge = 30 * 24 * time.Hour

// maintenanceView is the cleanup page: what can go, and what the last action
// removed.
type maintenanceView struct {
	Version    string
	GoVersion  string
	Nav        []navItem // the rail, with the active section (ADR 0152)
	Breadcrumb []crumb   // the path to this page
	Self       string    // this page's URI

	Preview string // which cleanup is open: "", "inventory", "devices" or "backups"
	Notice  string // result of the last action, empty before one
	Error   string // why the last action failed, empty when it worked

	Inventory inventorySection
	Devices   deviceSection
	Backups   backupSection
}

// inventorySection is the state of plugins/ and firmware/ and what a prune
// would delete (ADR 0090).
type inventorySection struct {
	Plugins  string // "7 files, 82 MB"
	Firmware string
	Files    []pruneFile // what the preview lists
	Kept     int         // files the prune keeps
}

// deviceSection is the state directory: one file per MAC, including the
// unclaimed hardware that the dashboard offers to claim (ADR 0080).
type deviceSection struct {
	States  int           // state files in total
	Orphans []staleDevice // without a device file, unseen for staleDeviceAge
	Age     string        // staleDeviceAge as the page says it
}

// staleDevice is one state entry the cleanup would drop.
type staleDevice struct {
	Key      string // file name in state/devices/, the MAC without separators
	MAC      string
	Hardware string
	SeenText string // when it was last seen
}

// backupSection is state/editor: up to editorBackups copies per object.
type backupSection struct {
	Dirs    int            // backup directories in total
	Orphans []orphanBackup // directories whose object no longer exists
}

// orphanBackup is one backup directory that no object uses anymore.
type orphanBackup struct {
	Kind   string // pages, plans, devices or displays
	Label  string // the object name, a page file label for a variant
	Files  int    // copies in the directory
	Size   string // their size
	Newest string // when the newest copy was written
}

// handleMaintenance shows the cleanup page and its previews.
func (s *Server) handleMaintenance(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	loc := timezoneOf(cfg)
	version, goVersion := editorVersion(cfg)
	v := maintenanceView{Version: version, GoVersion: goVersion, Self: r.URL.RequestURI(),
		Nav:        navFor("Maintenance"),
		Breadcrumb: crumbPath(crumb{Label: "Maintenance"}),
		Preview:    r.URL.Query().Get("preview")}
	v.Inventory = s.inventorySection(cfg)
	v.Devices = s.deviceSection(cfg, s.now(), loc)
	v.Backups = s.backupSection(cfg, s.now(), loc)
	if done := r.URL.Query().Get("done"); done != "" {
		v.Notice = maintenanceNotice(done, r.URL.Query().Get("removed"), r.URL.Query().Get("failed"))
	}
	v.Error = r.URL.Query().Get("error")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := maintenanceTmpl.Execute(w, v); err != nil {
		slog.Error("maintenance page rendering failed", "error", err)
	}
}

// inventorySection counts the two inventories and, when the page asks for it,
// lists what the prune would delete.
func (s *Server) inventorySection(cfg *Config) inventorySection {
	pluginFiles, pluginBytes, firmwareFiles, firmwareBytes := inventorySummary(cfg)
	remove, kept := s.prunePlan(cfg)
	return inventorySection{
		Plugins:  inventoryText(pluginFiles, pluginBytes),
		Firmware: inventoryText(firmwareFiles, firmwareBytes),
		Files:    remove,
		Kept:     kept,
	}
}

// deviceSection counts the state files and lists the ones that are old and
// unclaimed. Deletion goes through forgetDeviceState, so the running server
// drops them from memory too.
func (s *Server) deviceSection(cfg *Config, now time.Time, loc *time.Location) deviceSection {
	section := deviceSection{Age: daysText(staleDeviceAge)}
	s.mu.Lock()
	section.States = len(s.state)
	s.mu.Unlock()
	for _, u := range s.unclaimedDevices(cfg) {
		seen := u.LastSeen
		if seen.IsZero() {
			seen = u.FirstSeen
		}
		if seen.IsZero() || now.Sub(seen) < staleDeviceAge {
			continue
		}
		section.Orphans = append(section.Orphans, staleDevice{Key: u.Key, MAC: u.MAC,
			Hardware: u.Hardware, SeenText: agoText(seen, now)})
	}
	return section
}

// backupSection finds the backup directories whose object is gone: a deleted
// page, plan, device or display type leaves its copies behind.
func (s *Server) backupSection(cfg *Config, now time.Time, loc *time.Location) backupSection {
	root := filepath.Join(cfg.Dir, stateDirName, "editor")
	section := backupSection{}
	kinds := map[string]string{
		"pages":    cfg.Pages,
		"plans":    cfg.Plans,
		"devices":  cfg.DevicesDir,
		"displays": cfg.DisplaysDir,
	}
	dirs, err := os.ReadDir(root)
	if err != nil {
		return section
	}
	for _, kindDir := range dirs {
		dir, ok := kinds[kindDir.Name()]
		if !kindDir.IsDir() || !ok {
			continue
		}
		labels, err := os.ReadDir(filepath.Join(root, kindDir.Name()))
		if err != nil {
			continue
		}
		for _, labelDir := range labels {
			if !labelDir.IsDir() {
				continue
			}
			section.Dirs++
			dirPath := filepath.Join(root, kindDir.Name(), labelDir.Name())
			if _, err := os.Stat(filepath.Join(dir, labelDir.Name()+".yaml")); err == nil {
				continue
			}
			copies, err := os.ReadDir(dirPath)
			if err != nil {
				continue
			}
			orphan := orphanBackup{Kind: kindDir.Name(), Label: labelDir.Name(), Files: len(copies)}
			var bytes int64
			var newest time.Time
			for _, c := range copies {
				info, err := c.Info()
				if err != nil {
					continue
				}
				bytes += info.Size()
				if info.ModTime().After(newest) {
					newest = info.ModTime()
				}
			}
			orphan.Size, orphan.Newest = byteText(bytes), downloadTime(newest, now, loc)
			section.Orphans = append(section.Orphans, orphan)
		}
	}
	sort.Slice(section.Orphans, func(i, j int) bool {
		if section.Orphans[i].Kind != section.Orphans[j].Kind {
			return section.Orphans[i].Kind < section.Orphans[j].Kind
		}
		return section.Orphans[i].Label < section.Orphans[j].Label
	})
	return section
}

// handleMaintenanceDelete runs one cleanup. It touches nothing outside the
// three areas the preview named: a program that is still running holds its file
// on Windows, so a failed file stays and the next click tries again.
func (s *Server) handleMaintenanceDelete(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	kind := r.FormValue("kind")
	removed, failed := 0, 0
	switch kind {
	case "inventory":
		remove, _ := s.prunePlan(cfg)
		for _, f := range remove {
			if err := os.Remove(filepath.Join(f.dir, f.File)); err != nil {
				slog.Warn("inventory file not removable", "file", f.File, "error", err)
				failed++
				continue
			}
			slog.Info("inventory file deleted", "train", f.Train, "file", f.File)
			removed++
		}
	case "devices":
		for _, u := range s.deviceSection(cfg, s.now(), timezoneOf(cfg)).Orphans {
			if err := s.forgetDeviceState(u.Key); err != nil {
				slog.Warn("device state not removable", "key", u.Key, "error", err)
				failed++
				continue
			}
			slog.Info("device state deleted", "mac", u.MAC)
			removed++
		}
	case "backups":
		for _, old := range s.backupSection(cfg, s.now(), timezoneOf(cfg)).Orphans {
			dir := filepath.Join(cfg.Dir, stateDirName, "editor", old.Kind, old.Label)
			if err := os.RemoveAll(dir); err != nil {
				slog.Warn("editor backups not removable", "dir", dir, "error", err)
				failed++
				continue
			}
			slog.Info("editor backups deleted", "kind", old.Kind, "label", old.Label)
			removed++
		}
	default:
		http.Redirect(w, r, "/ui/system/maintenance?error=unknown+cleanup", http.StatusSeeOther)
		return
	}
	target := fmt.Sprintf("/ui/system/maintenance?done=%s&removed=%d&failed=%d", kind, removed, failed)
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// maintenanceNotice says what the last cleanup did.
func maintenanceNotice(kind, removed, failed string) string {
	what := map[string]string{"inventory": "files", "devices": "device states", "backups": "backup directories"}[kind]
	if what == "" {
		return ""
	}
	text := fmt.Sprintf("Removed %s %s", removed, what)
	if failed != "0" && failed != "" {
		text += fmt.Sprintf(", %s failed", failed)
	}
	return text + "."
}

// daysText prints a duration in days for the page: "30 days".
func daysText(d time.Duration) string {
	days := int(d.Hours() / 24)
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}
