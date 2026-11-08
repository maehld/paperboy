package paperboy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/internal/download"
)

// Release checks run once a day by default; the interval is configurable down
// to an hour (ADR 0062). A check at startup happens right away.
const (
	defaultDownloadInterval = 24 * time.Hour
	minDownloadInterval     = time.Hour
)

// downloadStatus is what the dashboard shows about the release checks.
type downloadStatus struct {
	mu        sync.Mutex
	running   bool
	lastCheck time.Time
	lastOK    time.Time
	lastError string
	plugins   map[string]string // plugin -> installed version
	firmware  map[string]string // hardware -> installed version
}

func newDownloadStatus() *downloadStatus {
	return &downloadStatus{plugins: map[string]string{}, firmware: map[string]string{}}
}

// view returns a copy for the dashboard.
func (d *downloadStatus) view() (running bool, lastCheck, lastOK time.Time, lastError string, plugins, firmware map[string]string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	plugins = make(map[string]string, len(d.plugins))
	for k, v := range d.plugins {
		plugins[k] = v
	}
	firmware = make(map[string]string, len(d.firmware))
	for k, v := range d.firmware {
		firmware[k] = v
	}
	return d.running, d.lastCheck, d.lastOK, d.lastError, plugins, firmware
}

// begin marks a check as running; false when one is already running, so a
// second click on the dashboard and the background loop never overlap.
func (d *downloadStatus) begin() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.running {
		return false
	}
	d.running = true
	return true
}

func (d *downloadStatus) checked(now time.Time, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.running = false
	d.lastCheck = now
	if err != nil {
		d.lastError = err.Error()
		return
	}
	d.lastOK, d.lastError = now, ""
}

func (d *downloadStatus) installed(plugins, firmware map[string]string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for name, version := range plugins {
		d.plugins[name] = version
	}
	for hw, version := range firmware {
		d.firmware[hw] = version
	}
}

// pluginVersion returns the version a check installed for a plugin during this
// server run (ADR 0062).
func (d *downloadStatus) pluginVersion(name string) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	version, ok := d.plugins[name]
	return version, ok
}

// downloadLoop checks the release at startup and then when the configured
// interval has passed. It wakes every minute so that a configuration change is
// picked up quickly; the check itself only runs when one is due (ADR 0062).
func (s *Server) downloadLoop(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		s.checkDownloads(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// checkDownloads runs one release check when the interval has passed.
func (s *Server) checkDownloads(ctx context.Context) {
	if s.download == nil {
		return
	}
	cfg := s.config()
	if cfg.Download == nil {
		return
	}
	_, last, _, _, _, _ := s.download.view()
	if !last.IsZero() && s.now().Sub(last) < cfg.Download.Interval {
		return
	}
	if !s.download.begin() {
		return
	}
	s.finishDownloadCheck(cfg, s.downloadOnce(ctx, cfg))
}

// finishDownloadCheck records the outcome of one check (ADR 0062).
func (s *Server) finishDownloadCheck(cfg *Config, err error) {
	if err != nil {
		slog.Warn("release download incomplete", "error", err)
		s.download.checked(s.now(), err)
		return
	}
	s.download.checked(s.now(), nil)
	slog.Info("release check done", "source", cfg.Download.Source)
}

// downloadOnce fetches the programs of configured plugins (missing or newer
// than the one in use) and the firmware images of the newest bundle (ADR 0060,
// 0062). Both halves run even when one of them fails.
func (s *Server) downloadOnce(ctx context.Context, cfg *Config) error {
	c := download.New(cfg.Download.Source, cfg.Download.Token, filepath.Join(cfg.Dir, stateDirName, "download"))
	var errs []error
	changed, err := s.downloadPlugins(ctx, cfg, c)
	if err != nil {
		errs = append(errs, err)
	} else if changed {
		// A new file changes the resolved command; the host restarts the plugin
		// on its next use (ADR 0049).
		s.configurePlugins(s.config())
	}
	if err := s.downloadFirmware(ctx, cfg, c); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// downloadPlugins installs the programs of configured plugins: missing ones and
// newer versions from the newest release, a pinned version from the release
// whose zip carries it (ADR 0062, 0075). An unversioned program and a pin stay
// where they are.
func (s *Server) downloadPlugins(ctx context.Context, cfg *Config, c *download.Client) (bool, error) {
	var missing, candidates []Plugin
	current := map[string]string{}
	pinned := map[string][]Plugin{}
	for _, p := range cfg.Plugins {
		program, version := pluginProgram(cfg.PluginsDir, p.Name, p.Version)
		switch {
		case p.Version != "" && program == "":
			pinned[p.Version] = append(pinned[p.Version], p)
		case p.Version != "":
			slog.Info("plugin left alone", "plugin", p.Name, "reason", "version pinned", "version", p.Version)
		case program == "":
			missing = append(missing, p)
		case version == "":
			// An unversioned program in plugins/ or one on PATH belongs to the
			// operator and wins over every download (ADR 0060).
			slog.Info("plugin left alone", "plugin", p.Name, "reason", "unversioned program wins",
				"program", program)
		default:
			candidates = append(candidates, p)
			current[p.Name] = version
		}
	}
	if len(missing)+len(candidates)+len(pinned) == 0 {
		return false, nil
	}
	changed := false
	for _, pin := range sortedKeys(pinned) {
		rel, archive, targets, err := s.pinnedPlugins(ctx, c, pinned[pin], pin)
		if err != nil {
			return changed, err
		}
		installed, err := s.installPlugins(cfg, rel, archive, targets)
		if err != nil {
			return changed, err
		}
		changed = changed || len(installed) > 0
	}
	if len(missing)+len(candidates) > 0 {
		rel, ok, err := c.Latest(ctx, func(a download.Asset) bool { return a.Name == download.PluginAsset() })
		if err != nil {
			return changed, err
		}
		if !ok {
			return changed, fmt.Errorf("plugins: no release with %s", download.PluginAsset())
		}
		versions, archive, err := c.PluginZip(ctx, rel)
		if err != nil {
			return changed, err
		}
		targets := map[string]string{}
		for _, p := range missing {
			version, ok := versions[p.Name]
			if !ok {
				// A release whose zip still ships unversioned programs carries
				// no version to install (ADR 0075).
				slog.Warn("plugin not in the release zip", "plugin", p.Name, "release", rel.Tag)
				continue
			}
			targets[p.Name] = version
		}
		for _, p := range candidates {
			version, ok := versions[p.Name]
			if !ok || !pluginVersionNewer(version, current[p.Name]) {
				continue
			}
			// A version this run already installed but that no longer resolves
			// was deleted on purpose: keep the older program (ADR 0062).
			if recorded, ok := s.download.pluginVersion(p.Name); ok && recorded == "v"+version {
				slog.Info("plugin update left deleted", "plugin", p.Name, "version", recorded,
					"reason", "deleted program falls back to the older version")
				changed = true // the resolved program changed even without a download
				continue
			}
			targets[p.Name] = version
		}
		if len(targets) == 0 {
			return changed, nil
		}
		installed, err := s.installPlugins(cfg, rel, archive, targets)
		if err != nil {
			return changed, err
		}
		changed = changed || len(installed) > 0
	}
	return changed, nil
}

// pinnedPlugins finds the newest release whose plugin zip names each of the
// plugins with the pinned version: a plugin pin names a plugin version, not a
// release tag (ADR 0075).
func (s *Server) pinnedPlugins(ctx context.Context, c *download.Client, plugins []Plugin, pin string) (download.Release, string, map[string]string, error) {
	releases, err := c.List(ctx)
	if err != nil {
		return download.Release{}, "", nil, err
	}
	for _, rel := range releases {
		if _, ok := rel.Asset(download.PluginAsset()); !ok {
			continue // a release of another train carries no plugin zip
		}
		versions, archive, err := c.PluginZip(ctx, rel)
		if err != nil {
			return download.Release{}, "", nil, err
		}
		targets := map[string]string{}
		for _, p := range plugins {
			if version, ok := versions[p.Name]; ok && "v"+version == pin {
				targets[p.Name] = version
			}
		}
		if len(targets) == len(plugins) {
			return rel, archive, targets, nil
		}
	}
	return download.Release{}, "", nil, fmt.Errorf("plugins: no release carries %s", pin)
}

// pluginVersionNewer reports whether the version a zip ships is higher than the
// one in use, e.g. "1.4.0" against "v1.3.2" (ADR 0075).
func pluginVersionNewer(next, current string) bool {
	nextParts, ok := versionParts(next)
	if !ok {
		return false
	}
	currentParts, ok := versionParts(strings.TrimPrefix(current, "v"))
	if !ok {
		return true
	}
	return compareVersions(nextParts, currentParts) > 0
}

// installPlugins unpacks the named programs of a release's plugin zip into the
// plugins directory, each under the version the zip names it with (ADR 0075).
func (s *Server) installPlugins(cfg *Config, rel download.Release, archive string, targets map[string]string) (map[string]string, error) {
	if err := os.MkdirAll(cfg.PluginsDir, 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(cfg.PluginsDir, ".download-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	names, err := download.Extract(archive, tmp, func(name string) bool {
		plugin, version, ok := download.PluginEntry(name)
		return ok && targets[plugin] == version
	})
	if err != nil {
		return nil, err
	}
	found := map[string]bool{}
	installed := map[string]string{}
	for _, name := range names {
		plugin, version, _ := download.PluginEntry(name)
		found[plugin] = true
		target := filepath.Join(cfg.PluginsDir, name)
		if _, err := os.Stat(target); err == nil {
			continue // never overwrite (ADR 0060)
		}
		if err := os.Rename(filepath.Join(tmp, name), target); err != nil {
			return installed, err
		}
		installed[plugin] = "v" + version
		slog.Info("plugin downloaded", "plugin", plugin, "version", "v"+version, "release", rel.Tag)
	}
	if len(found) < len(targets) {
		var absent []string
		for name := range targets {
			if !found[name] {
				absent = append(absent, name)
			}
		}
		sort.Strings(absent)
		slog.Warn("plugin not in the release zip", "plugins", strings.Join(absent, ", "), "release", rel.Tag)
	}
	s.download.installed(installed, nil)
	return installed, nil
}

// downloadFirmware installs the images of the release's bundle: all of them,
// because a display type's name says nothing about the hardware a device
// reports. A pin picks the release whose bundle carries that image version
// (ADR 0060, 0061).
func (s *Server) downloadFirmware(ctx context.Context, cfg *Config, c *download.Client) error {
	pin := cfg.Firmware.Version
	if pin == "" {
		rel, ok, err := c.Latest(ctx, func(a download.Asset) bool {
			return a.Name == download.FirmwareAsset
		})
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("firmware: no release with a bundle")
		}
		return s.installFirmware(ctx, cfg, c, rel, "")
	}
	releases, err := c.List(ctx)
	if err != nil {
		return err
	}
	for _, rel := range releases {
		if _, ok := rel.Asset(download.FirmwareAsset); !ok {
			continue // a release of another train carries no bundle
		}
		entries, archive, err := c.Bundle(ctx, rel)
		if err != nil {
			return err
		}
		if archive == "" || !bundleHas(entries, pin) {
			continue
		}
		return s.installFirmwareFiles(cfg, archive, pin)
	}
	return fmt.Errorf("firmware: no release carries images of %s", pin)
}

// installFirmware unpacks a release's bundle; pin narrows it to that version.
func (s *Server) installFirmware(ctx context.Context, cfg *Config, c *download.Client, rel download.Release, pin string) error {
	entries, archive, err := c.Bundle(ctx, rel)
	if err != nil {
		return err
	}
	if archive == "" {
		return fmt.Errorf("%s: no images", rel.Tag)
	}
	if !bundleHas(entries, pin) {
		return fmt.Errorf("%s: no images of %s", rel.Tag, pin)
	}
	return s.installFirmwareFiles(cfg, archive, pin)
}

// bundleHas reports whether the bundle carries an image of the given version
// (empty = any image).
func bundleHas(entries []string, pin string) bool {
	for _, entry := range entries {
		name := filepath.Base(entry)
		hw, ok := splitFirmwareName(name)
		if !ok {
			continue
		}
		if version, _, ok := parseFirmwareName(hw, name); ok && (pin == "" || version == pin) {
			return true
		}
	}
	return false
}

// installFirmwareFiles extracts the images from the bundle into the firmware
// directory, all hardware types or only those of the pinned version. Existing
// files are never overwritten.
func (s *Server) installFirmwareFiles(cfg *Config, archive, pin string) error {
	installed := map[string]string{}
	names, err := download.Extract(archive, cfg.FirmwareDir, func(name string) bool {
		hw, ok := splitFirmwareName(name)
		if !ok {
			return false
		}
		version, _, ok := parseFirmwareName(hw, name)
		return ok && (pin == "" || version == pin)
	})
	if err != nil {
		return err
	}
	for _, name := range names {
		hw, _ := splitFirmwareName(name)
		version, _, _ := parseFirmwareName(hw, name)
		installed[hw] = version
		slog.Info("firmware downloaded", "hardware", hw, "version", version)
	}
	s.download.installed(nil, installed)
	return nil
}

// sortedKeys sorts the pins so that checks run in a stable order.
func sortedKeys(m map[string][]Plugin) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
