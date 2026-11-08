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

// downloadInterval is how often the release is checked (ADR 0060). A check at
// startup happens right away.
const downloadInterval = 24 * time.Hour

// downloadStatus is what the dashboard shows about the release checks.
type downloadStatus struct {
	mu        sync.Mutex
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
func (d *downloadStatus) view() (lastCheck, lastOK time.Time, lastError string, plugins, firmware map[string]string) {
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
	return d.lastCheck, d.lastOK, d.lastError, plugins, firmware
}

func (d *downloadStatus) checked(now time.Time, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
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

// downloadLoop checks the release at startup and then once a day. It wakes
// every minute so that a configuration change is picked up quickly; the check
// itself only runs when one is due.
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
	last, _, _, _, _ := s.download.view()
	if !last.IsZero() && s.now().Sub(last) < downloadInterval {
		return
	}
	if err := s.downloadOnce(ctx, cfg); err != nil {
		slog.Warn("release download incomplete", "error", err)
		s.download.checked(s.now(), err)
		return
	}
	s.download.checked(s.now(), nil)
	slog.Info("release check done", "source", cfg.Download.Source)
}

// downloadOnce fetches what is missing from the release: the programs of
// configured plugins that have none, and the firmware images of the configured
// display types (ADR 0060). Both halves run even when one of them fails.
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

// downloadPlugins installs the programs of configured plugins that have none:
// a pinned version from its release, everything else from the newest one.
func (s *Server) downloadPlugins(ctx context.Context, cfg *Config, c *download.Client) (bool, error) {
	var unpinned []Plugin
	pinned := map[string][]Plugin{}
	for _, p := range cfg.Plugins {
		if program, _ := pluginProgram(cfg.PluginsDir, p.Name, p.Version); program != "" {
			continue
		}
		if p.Version != "" {
			pinned[p.Version] = append(pinned[p.Version], p)
		} else {
			unpinned = append(unpinned, p)
		}
	}
	if len(unpinned) == 0 && len(pinned) == 0 {
		return false, nil
	}
	changed := false
	for _, pin := range sortedKeys(pinned) {
		rel, ok, err := c.Tagged(ctx, strings.TrimPrefix(pin, "v"))
		if err != nil {
			return changed, err
		}
		if !ok {
			return changed, fmt.Errorf("plugins: no release with version %s", pin)
		}
		installed, err := s.installPlugins(ctx, cfg, c, rel, pinned[pin])
		if err != nil {
			return changed, err
		}
		changed = changed || len(installed) > 0
	}
	if len(unpinned) > 0 {
		rel, ok, err := c.Latest(ctx, func(a download.Asset) bool { return a.Name == download.PluginAsset() })
		if err != nil {
			return changed, err
		}
		if !ok {
			return changed, fmt.Errorf("plugins: no release with %s", download.PluginAsset())
		}
		installed, err := s.installPlugins(ctx, cfg, c, rel, unpinned)
		if err != nil {
			return changed, err
		}
		changed = changed || len(installed) > 0
	}
	return changed, nil
}

// installPlugins unpacks the named programs of the release's plugin zip into
// the plugins directory as <name>-v<version>.
func (s *Server) installPlugins(ctx context.Context, cfg *Config, c *download.Client, rel download.Release, plugins []Plugin) (map[string]string, error) {
	want := map[string]bool{}
	for _, p := range plugins {
		want[p.Name] = true
	}
	archive, err := c.Fetch(ctx, rel, download.PluginAsset())
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.PluginsDir, 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(cfg.PluginsDir, ".download-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	names, err := download.Extract(archive, tmp, func(name string) bool {
		return want[strings.TrimSuffix(name, ".exe")]
	})
	if err != nil {
		return nil, err
	}
	installed := map[string]string{}
	for _, name := range names {
		base, ext := strings.TrimSuffix(name, ".exe"), ""
		if base != name {
			ext = ".exe"
		}
		target := filepath.Join(cfg.PluginsDir, base+"-v"+rel.Version+ext)
		if _, err := os.Stat(target); err == nil {
			continue // never overwrite (ADR 0060)
		}
		if err := os.Rename(filepath.Join(tmp, name), target); err != nil {
			return installed, err
		}
		installed[base] = "v" + rel.Version
		slog.Info("plugin downloaded", "plugin", base, "version", "v"+rel.Version, "release", rel.Tag)
	}
	if len(installed) < len(want) {
		var missing []string
		for name := range want {
			if _, ok := installed[name]; !ok {
				missing = append(missing, name)
			}
		}
		sort.Strings(missing)
		slog.Warn("plugin not in the release zip", "plugins", strings.Join(missing, ", "), "release", rel.Tag)
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
			return strings.HasPrefix(a.Name, "paperboy-firmware-") && strings.HasSuffix(a.Name, ".zip")
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
