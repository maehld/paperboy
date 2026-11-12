package paperboy

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/maehld/paperboy/internal/download"
	"github.com/maehld/paperboy/internal/semver"
)

// pruneKeepVersions is the safety net of the inventory prune: the newest
// versions of a plugin and of a hardware type stay even when nothing refers to
// them (ADR 0090).
const pruneKeepVersions = 2

// pruneFile is one file of plugins/ or firmware/ that the prune action would
// delete.
type pruneFile struct {
	Train   string // "plugins" or "firmware"
	File    string
	Version string // empty for a name without a version
	dir     string
}

// prunePlan lists the files the prune action would delete and counts the ones
// it keeps. Kept are the program the resolver picks, whichever a pin names, the
// firmware a device reports or is being offered, names without a version (a
// hand-built program, an ignored file, the old firmware layout) and the newest
// versions of every plugin and hardware type (ADR 0090). Nothing is read from
// the network and nothing is deleted here.
func (s *Server) prunePlan(cfg *Config) (remove []pruneFile, kept int) {
	plugins, pluginKept := scanInventory(cfg.PluginsDir, "plugins", s.keepPluginPrograms(cfg))
	firmware, firmwareKept := scanInventory(cfg.FirmwareDir, "firmware", s.keepFirmwareImages(cfg))
	return append(plugins, firmware...), pluginKept + firmwareKept
}

// keepPluginPrograms answers the programs in plugins/ that must stay.
func (s *Server) keepPluginPrograms(cfg *Config) map[string]bool {
	keep := map[string]bool{}
	entries, err := os.ReadDir(cfg.PluginsDir)
	if err != nil {
		return keep
	}
	type program struct {
		file    string
		version semver.Version
	}
	byPlugin := map[string][]program{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		plugin, text, ok := download.PluginEntry(e.Name())
		parsed, parsedOK := semver.Parse(text)
		if !ok || !parsedOK {
			// A program built by hand carries no version and is not ours to
			// delete (ADR 0075).
			keep[e.Name()] = true
			continue
		}
		byPlugin[plugin] = append(byPlugin[plugin], program{e.Name(), parsed})
	}
	for _, p := range cfg.Plugins {
		// The resolved program: the highest version, or the pin.
		if program, _ := pluginProgram(cfg.PluginsDir, p.Name, p.Version); program != "" {
			keep[filepath.Base(program)] = true
		}
		// A pin keeps its file even when an unversioned program wins over it.
		if pin, ok := semver.Parse(strings.TrimPrefix(p.Version, "v")); ok {
			for _, prog := range byPlugin[p.Name] {
				if semver.Compare(prog.version, pin) == 0 {
					keep[prog.file] = true
				}
			}
		}
	}
	for _, programs := range byPlugin {
		sort.Slice(programs, func(i, j int) bool {
			return semver.Compare(programs[i].version, programs[j].version) > 0
		})
		for i, prog := range programs {
			if i < pruneKeepVersions {
				keep[prog.file] = true
			}
		}
	}
	return keep
}

// keepFirmwareImages answers the images in firmware/ that must stay.
func (s *Server) keepFirmwareImages(cfg *Config) map[string]bool {
	keep := map[string]bool{}
	entries, err := os.ReadDir(cfg.FirmwareDir)
	if err != nil {
		return keep
	}
	type image struct {
		file    string
		version semver.Version
	}
	byHardware := map[string][]image{}
	for _, e := range entries {
		if e.IsDir() {
			// The old <hardware>/ layout is still read and left alone (ADR 0060).
			keep[e.Name()] = true
			continue
		}
		hardware, ok := splitFirmwareName(e.Name())
		if !ok {
			keep[e.Name()] = true // an ignored name is not ours to delete
			continue
		}
		_, parsed, ok := parseFirmwareName(hardware, e.Name())
		if !ok {
			keep[e.Name()] = true
			continue
		}
		byHardware[hardware] = append(byHardware[hardware], image{e.Name(), parsed})
	}
	pin, pinned := semver.Parse(cfg.Firmware.Version)
	reported := s.reportedFirmware()
	for hardware, images := range byHardware {
		for _, img := range images {
			if pinned && semver.Compare(img.version, pin) == 0 {
				keep[img.file] = true
			}
			for _, text := range reported[hardware] {
				if version, ok := semver.Parse(text); ok && semver.Compare(version, img.version) == 0 {
					keep[img.file] = true
				}
			}
		}
		// The image the store picks: the highest, or the pin (ADR 0060).
		if img, _ := s.firmware.latest(hardware, cfg.Firmware.Version); img != nil {
			keep[img.File] = true
		}
		sort.Slice(images, func(i, j int) bool {
			return semver.Compare(images[i].version, images[j].version) > 0
		})
		for i, img := range images {
			if i < pruneKeepVersions {
				keep[img.file] = true
			}
		}
	}
	return keep
}

// reportedFirmware collects the firmware versions the devices report and the
// ones they are being offered, by hardware type: a device must not lose the
// image in the middle of an update (ADR 0090).
func (s *Server) reportedFirmware() map[string][]string {
	cfg := s.config()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string][]string{}
	for _, d := range cfg.Devices {
		st := s.state[d.stateKey()]
		if st == nil || st.Hardware == "" {
			continue
		}
		out[st.Hardware] = append(out[st.Hardware], st.Firmware, st.FirmwareOffered)
	}
	return out
}

// scanInventory splits the files of one directory into the ones a prune may
// delete and the ones it keeps.
func scanInventory(dir, train string, keep map[string]bool) (remove []pruneFile, kept int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if keep[e.Name()] {
			kept++
			continue
		}
		remove = append(remove, pruneFile{Train: train, File: e.Name(), Version: inventoryVersion(e.Name()), dir: dir})
	}
	sort.Slice(remove, func(i, j int) bool { return remove[i].File < remove[j].File })
	return remove, kept
}

// inventoryVersion names the version inside a program or image name, empty for
// a name that carries none.
func inventoryVersion(name string) string {
	if _, version, ok := download.PluginEntry(name); ok {
		return version
	}
	if hardware, ok := splitFirmwareName(name); ok {
		if version, _, ok := parseFirmwareName(hardware, name); ok {
			return version
		}
	}
	return ""
}

// inventorySummary counts the files and bytes of both inventories, newest
// numbers first, so the releases page can show the growth before someone
// prunes (ADR 0090).
func inventorySummary(cfg *Config) (pluginFiles, pluginBytes, firmwareFiles, firmwareBytes int64) {
	pluginFiles, pluginBytes = inventorySize(cfg.PluginsDir)
	firmwareFiles, firmwareBytes = inventorySize(cfg.FirmwareDir)
	return pluginFiles, pluginBytes, firmwareFiles, firmwareBytes
}

func inventorySize(dir string) (files, bytes int64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files++
		bytes += info.Size()
	}
	return files, bytes
}

// handlePrune deletes what the preview listed. It is the only place where
// Paperboy removes a file the operator did not create, so it touches nothing
// outside plugins/ and firmware/ (ADR 0090).
func (s *Server) handlePrune(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	remove, _ := s.prunePlan(cfg)
	deleted, failed := 0, 0
	for _, f := range remove {
		if err := os.Remove(filepath.Join(f.dir, f.File)); err != nil {
			// A program that is still running holds its file on Windows; the
			// next click tries again.
			slog.Warn("inventory file not removable", "file", f.File, "error", err)
			failed++
			continue
		}
		slog.Info("inventory file deleted", "train", f.Train, "file", f.File)
		deleted++
	}
	target := "/ui/downloads"
	if deleted > 0 || failed > 0 {
		target = fmt.Sprintf("/ui/downloads?pruned=%d&failed=%d", deleted, failed)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
