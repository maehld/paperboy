package paperboy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/internal/download"
	"github.com/maehld/paperboy/internal/semver"
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
	update    paperboyUpdate    // what the last check learned about Paperboy itself (ADR 0101)
	notified  string            // version the journal already named, so a check does not repeat it
}

// paperboyUpdate is the result of looking at Paperboy's own release train: the
// newer version, or the reason why there is nothing to name (ADR 0101).
type paperboyUpdate struct {
	Checked  bool   // a check reached the hint
	Compared bool   // the running version could be compared with the releases
	Found    bool   // the source carries a release of Paperboy's train in the channel
	Version  string // a newer version than the running one, without the leading v
	URL      string // the release page, empty when the source has none
}

func newDownloadStatus() *downloadStatus {
	return &downloadStatus{plugins: map[string]string{}, firmware: map[string]string{}}
}

// view returns a copy for the dashboard.
func (d *downloadStatus) view() (running bool, lastCheck, lastOK time.Time, lastError string, plugins, firmware map[string]string, update paperboyUpdate) {
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
	return d.running, d.lastCheck, d.lastOK, d.lastError, plugins, firmware, d.update
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

// notice records what a check learned about Paperboy's own train and answers
// whether the journal should name the version: one line per release, not one
// per check (ADR 0101).
func (d *downloadStatus) notice(u paperboyUpdate) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.update = u
	if u.Version == "" || u.Version == d.notified {
		return false
	}
	d.notified = u.Version
	return true
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
	_, last, _, _, _, _, _ := s.download.view()
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

// downloadClient returns the client of one release train. Every train has its
// own cache directory, so a tag that both a plugin and a firmware repository
// would use cannot collide when the trains split into repositories (ADR 0090).
func (s *Server) downloadClient(cfg *Config, train string) *download.Client {
	return download.New(cfg.Download.Source, cfg.Download.Token,
		filepath.Join(cfg.Dir, stateDirName, "download", train))
}

// downloadOnce fetches the programs of configured plugins (missing or newer
// than the one in use) and the firmware images of the newest bundle (ADR 0060,
// 0062). Both halves run even when one of them fails.
func (s *Server) downloadOnce(ctx context.Context, cfg *Config) error {
	var errs []error
	changed, err := s.downloadPlugins(ctx, cfg)
	if err != nil {
		errs = append(errs, err)
	} else if changed {
		// A new file changes the resolved command; the host restarts the plugin
		// on its next use (ADR 0049).
		s.configurePlugins(s.config())
	}
	if err := s.downloadFirmware(ctx, cfg); err != nil {
		errs = append(errs, err)
	}
	// The hint about Paperboy itself only reads the release list and installs
	// nothing, so it runs after both halves and never fails the check (ADR 0101).
	s.checkPaperboyRelease(ctx, cfg)
	return errors.Join(errs...)
}

// checkPaperboyRelease names the newest release of Paperboy's own train when it
// is newer than the running binary. The channel follows the running version: a
// final version hears about the final releases, a prerelease about the
// prereleases too, so a stable instance is never pointed at an rc (ADR 0101).
// Nothing is downloaded, installed or restarted, and a source without Paperboy
// releases answers no hint instead of an error.
func (s *Server) checkPaperboyRelease(ctx context.Context, cfg *Config) {
	running, ok := runningVersion()
	if !ok {
		// A local build carries no release version to compare with (ADR 0101).
		s.download.notice(paperboyUpdate{Checked: true})
		return
	}
	channel := channelStable
	if running.Prerelease() {
		channel = channelLatest
	}
	releases, err := s.downloadClient(cfg, "paperboy").List(ctx)
	if err != nil {
		slog.Warn("paperboy release list unavailable", "train", "paperboy", "error", err)
		return
	}
	update := paperboyUpdate{Checked: true, Compared: true}
	if rel, ok := paperboyTrainRelease(releases, channel); ok {
		update.Found = true
		if newest, ok := semver.Parse(rel.Version); ok && semver.Compare(newest, running) > 0 {
			update.Version = rel.Version
			update.URL = releasePage(cfg.Download.Source, rel.Tag)
		}
	}
	if s.download.notice(update) {
		slog.Info("paperboy update available", "running", build.Version, "available", "v"+update.Version,
			"url", update.URL)
	}
}

// runningVersion parses the stamped version of this binary. A local build has
// none: its version is empty, "(devel)" or a pseudo-version derived from the
// commit, none of which names a release (ADR 0056, 0101).
func runningVersion() (semver.Version, bool) {
	if build.Version == "" || isPseudoVersion(build.Version) {
		return semver.Version{}, false
	}
	return semver.Parse(strings.TrimPrefix(build.Version, "v"))
}

// paperboyTrainRelease returns the newest release of Paperboy's own train: the
// tag of the form vX.Y.Z that no other train uses (ADR 0056), filtered by the
// channel (ADR 0101).
func paperboyTrainRelease(releases []download.Release, channel string) (download.Release, bool) {
	for _, rel := range releases {
		if !strings.HasPrefix(rel.Tag, "v") || !channelAllows(channel, rel.Prerelease) {
			continue
		}
		if !channelAllowsVersion(channel, rel.Version) {
			continue
		}
		if _, ok := semver.Parse(rel.Version); !ok {
			continue
		}
		return rel, true
	}
	return download.Release{}, false
}

// releasePage is the browsable page of a release tag, derived from the
// configured source so that a fork is linked as it is. Only the public GitHub
// source has such a page; an API endpoint or another host answers "" and the
// version stays plain text (ADR 0101).
func releasePage(source, tag string) string {
	source = strings.TrimSuffix(strings.TrimSpace(source), "/")
	if source == "" {
		source = download.OfficialSource
	}
	u, err := url.Parse(strings.TrimSuffix(source, "/releases"))
	if err != nil || (u.Host != "github.com" && u.Host != "www.github.com") {
		return ""
	}
	return "https://" + u.Host + "/" + strings.Trim(u.Path, "/") + "/releases/tag/" + url.PathEscape(tag)
}

// channelAllows reports whether a delivery fits the channel: stable follows the
// final releases only, latest takes prereleases too (ADR 0089).
func channelAllows(channel string, prerelease bool) bool {
	return channel == channelLatest || !prerelease
}

// channelAllowsVersion reports whether a version may be installed in the
// channel. stable never installs a version with a prerelease suffix, even when
// the release that carries it was not flagged, so a forgotten flag cannot
// smuggle an rc into stable (ADR 0089).
func channelAllowsVersion(channel, v string) bool {
	if channel == channelLatest {
		return true
	}
	parsed, ok := semver.Parse(v)
	return ok && !parsed.Prerelease()
}

// newestRelease returns the newest release that carries an asset pick accepts
// and that the channel allows; the list comes newest first (ADR 0089).
func newestRelease(releases []download.Release, channel string, pick func(download.Asset) bool) (download.Release, bool) {
	for _, rel := range releases {
		if !channelAllows(channel, rel.Prerelease) {
			continue
		}
		for _, a := range rel.Assets {
			if pick(a) {
				return rel, true
			}
		}
	}
	return download.Release{}, false
}

// prereleaseHint names the newest prerelease that carries a plugin whose zip the
// cache already read, so a missing plugin is explained instead of "not in the
// release zip" (ADR 0089). It downloads nothing.
func prereleaseHint(c *download.Client, releases []download.Release, channel, plugin string) string {
	if channel == channelLatest {
		return ""
	}
	for _, rel := range releases {
		if !rel.Prerelease {
			continue
		}
		if versions, ok := c.CachedVersions(rel.Tag); ok {
			if _, has := versions[plugin]; has {
				return rel.Tag
			}
		}
	}
	return ""
}

// downloadPlugins installs the programs of configured plugins: missing ones and
// newer versions from the channel's newest release, a pinned version from the
// release whose zip carries it (ADR 0062, 0075, 0089). An unversioned program
// and a pin stay where they are.
func (s *Server) downloadPlugins(ctx context.Context, cfg *Config) (bool, error) {
	c := s.downloadClient(cfg, "plugins")
	channel := cfg.Download.PluginChannel
	var missing, candidates []Plugin
	current := map[string]string{}
	pinned := map[string][]Plugin{}
	for _, p := range cfg.Plugins {
		program, programVersion := pluginProgram(cfg.PluginsDir, p.Name, p.Version)
		switch {
		case p.Version != "" && program == "":
			pinned[p.Version] = append(pinned[p.Version], p)
		case p.Version != "":
			slog.Info("plugin left alone", "plugin", p.Name, "reason", "version pinned", "version", p.Version)
		case program == "":
			missing = append(missing, p)
		case programVersion == "":
			// An unversioned program in plugins/ or one on PATH belongs to the
			// operator and wins over every download (ADR 0060).
			slog.Info("plugin left alone", "plugin", p.Name, "reason", "unversioned program wins",
				"program", program)
		default:
			candidates = append(candidates, p)
			current[p.Name] = programVersion
		}
	}
	if len(missing)+len(candidates)+len(pinned) == 0 {
		return false, nil
	}
	// The release list is fetched once and serves the channel and every pin
	// (ADR 0089); it is cached with its ETag.
	releases, err := c.List(ctx)
	if err != nil {
		return false, err
	}
	// The zip of the newest release this check looked at is the one the cache
	// keeps; earlier ones are dropped afterwards (ADR 0090).
	var archive string
	defer func() { keepNewest(c, archive) }()
	changed := false
	for _, pin := range sortedKeys(pinned) {
		rel, pinnedArchive, targets, err := s.pinnedPlugins(ctx, c, releases, pinned[pin], pin)
		if err != nil {
			return changed, err
		}
		archive = pinnedArchive
		installed, err := s.installPlugins(ctx, cfg, c, rel, archive, targets)
		if err != nil {
			return changed, err
		}
		changed = changed || len(installed) > 0
	}
	if len(missing)+len(candidates) > 0 {
		rel, ok := newestRelease(releases, channel, func(a download.Asset) bool {
			return a.Name == download.PluginAsset()
		})
		if !ok {
			return changed, fmt.Errorf("plugins: no release with %s in channel %s", download.PluginAsset(), channel)
		}
		versions, pluginArchive, err := c.PluginZip(ctx, rel)
		if err != nil {
			return changed, err
		}
		archive = pluginArchive
		targets := map[string]string{}
		for _, p := range missing {
			releaseVersion, ok := versions[p.Name]
			if !ok {
				// A release whose zip still ships unversioned programs carries
				// no version to install (ADR 0075), and a plugin that the
				// channel leaves out is not an error.
				if hint := prereleaseHint(c, releases, channel, p.Name); hint != "" {
					slog.Warn("plugin only in a prerelease", "plugin", p.Name, "release", hint, "channel", channel)
				} else {
					slog.Warn("plugin not in the release zip", "plugin", p.Name, "release", rel.Tag)
				}
				continue
			}
			if !channelAllowsVersion(channel, releaseVersion) {
				slog.Info("plugin left alone", "plugin", p.Name, "version", "v"+releaseVersion,
					"channel", channel, "reason", "prerelease")
				continue
			}
			targets[p.Name] = releaseVersion
		}
		for _, p := range candidates {
			releaseVersion, ok := versions[p.Name]
			if !ok {
				continue
			}
			if !channelAllowsVersion(channel, releaseVersion) {
				if pluginVersionNewer(releaseVersion, current[p.Name]) {
					slog.Info("plugin update left alone", "plugin", p.Name, "version", "v"+releaseVersion,
						"channel", channel, "reason", "prerelease")
				}
				continue
			}
			if !pluginVersionNewer(releaseVersion, current[p.Name]) {
				continue
			}
			// A version this run already installed but that no longer resolves
			// was deleted on purpose: keep the older program (ADR 0062).
			if recorded, ok := s.download.pluginVersion(p.Name); ok && recorded == "v"+releaseVersion {
				slog.Info("plugin update left deleted", "plugin", p.Name, "version", recorded,
					"reason", "deleted program falls back to the older version")
				changed = true // the resolved program changed even without a download
				continue
			}
			targets[p.Name] = releaseVersion
		}
		if len(targets) == 0 {
			return changed, nil
		}
		installed, err := s.installPlugins(ctx, cfg, c, rel, archive, targets)
		if err != nil {
			return changed, err
		}
		changed = changed || len(installed) > 0
	}
	return changed, nil
}

// pinnedPlugins finds the newest release whose plugin zip names each of the
// plugins with the pinned version: a plugin pin names a plugin version, not a
// release tag, and it bypasses the channel (ADR 0075, 0089).
func (s *Server) pinnedPlugins(ctx context.Context, c *download.Client, releases []download.Release, plugins []Plugin, pin string) (download.Release, string, map[string]string, error) {
	for _, rel := range releases {
		if _, ok := rel.Asset(download.PluginAsset()); !ok {
			continue // a release of another train carries no plugin zip
		}
		versions, archive, err := c.PluginZip(ctx, rel)
		if err != nil {
			return download.Release{}, "", nil, err
		}
		want, _ := semver.Parse(strings.TrimPrefix(pin, "v"))
		targets := map[string]string{}
		for _, p := range plugins {
			pv, ok := versions[p.Name]
			if !ok {
				continue
			}
			if got, ok := semver.Parse(pv); ok && semver.Compare(got, want) == 0 {
				targets[p.Name] = pv
			}
		}
		if len(targets) == len(plugins) {
			return rel, archive, targets, nil
		}
	}
	return download.Release{}, "", nil, fmt.Errorf("plugins: no release carries %s", pin)
}

// pluginVersionNewer reports whether the version a zip ships is higher than the
// one in use, e.g. "1.4.0-rc.1" against "v1.4.0-rc.0" (ADR 0075, 0089).
func pluginVersionNewer(next, current string) bool {
	nextVersion, ok := semver.Parse(next)
	if !ok {
		slog.Warn("plugin version not understood", "version", next)
		return false
	}
	currentVersion, ok := semver.Parse(strings.TrimPrefix(current, "v"))
	if !ok {
		return true
	}
	return semver.Compare(nextVersion, currentVersion) > 0
}

// installPlugins unpacks the named programs of a release's plugin zip into the
// plugins directory, each under the version the zip names it with (ADR 0075),
// and then drops the previous zip of the cache (ADR 0090).
func (s *Server) installPlugins(ctx context.Context, cfg *Config, c *download.Client, rel download.Release, archive string, targets map[string]string) (map[string]string, error) {
	archive, err := c.Ensure(ctx, rel, download.PluginAsset(), archive)
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
		plugin, version, ok := download.PluginEntry(name)
		return ok && targets[plugin] == version
	})
	if err != nil {
		return nil, err
	}
	found := map[string]bool{}
	installed := map[string]string{}
	for _, name := range names {
		plugin, installedVersion, _ := download.PluginEntry(name)
		found[plugin] = true
		target := filepath.Join(cfg.PluginsDir, name)
		if _, err := os.Stat(target); err == nil {
			continue // never overwrite (ADR 0060)
		}
		if err := os.Rename(filepath.Join(tmp, name), target); err != nil {
			return installed, err
		}
		installed[plugin] = "v" + installedVersion
		slog.Info("plugin downloaded", "plugin", plugin, "version", "v"+installedVersion, "release", rel.Tag)
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
// reports. A pin picks the release whose bundle carries that image version and
// bypasses the channel (ADR 0060, 0061, 0089).
func (s *Server) downloadFirmware(ctx context.Context, cfg *Config) error {
	c := s.downloadClient(cfg, "firmware")
	channel := cfg.Download.FirmwareChannel
	pin := cfg.Firmware.Version
	releases, err := c.List(ctx)
	if err != nil {
		return err
	}
	var archive string
	defer func() { keepNewest(c, archive) }()
	if pin == "" {
		rel, ok := newestRelease(releases, channel, func(a download.Asset) bool {
			return a.Name == download.FirmwareAsset
		})
		if !ok {
			return errors.New("firmware: no release with a bundle in channel " + channel)
		}
		entries, local, err := c.Bundle(ctx, rel)
		if err != nil {
			return err
		}
		archive = local
		if !bundleHas(entries, pin) {
			return fmt.Errorf("%s: no images", rel.Tag)
		}
		return s.installFirmwareFiles(ctx, cfg, c, rel, archive, pin, channel)
	}
	for _, rel := range releases {
		if _, ok := rel.Asset(download.FirmwareAsset); !ok {
			continue // a release of another train carries no bundle
		}
		entries, local, err := c.Bundle(ctx, rel)
		if err != nil {
			return err
		}
		if local == "" || !bundleHas(entries, pin) {
			continue
		}
		archive = local
		return s.installFirmwareFiles(ctx, cfg, c, rel, archive, pin, channel)
	}
	return fmt.Errorf("firmware: no release carries images of %s", pin)
}

// bundleHas reports whether the bundle carries an image of the given version
// (empty = any image).
func bundleHas(entries []string, pin string) bool {
	want, pinned := semver.Parse(pin)
	for _, entry := range entries {
		name := filepath.Base(entry)
		hw, ok := splitFirmwareName(name)
		if !ok {
			continue
		}
		_, ver, ok := parseFirmwareName(hw, name)
		if !ok {
			continue
		}
		if !pinned || semver.Compare(ver, want) == 0 {
			return true
		}
	}
	return false
}

// installFirmwareFiles extracts the images from the bundle into the firmware
// directory: all hardware types, only the pinned version, or only the versions
// the channel allows (ADR 0089). Existing files are never overwritten, and the
// previous bundle of the cache is dropped afterwards (ADR 0090).
func (s *Server) installFirmwareFiles(ctx context.Context, cfg *Config, c *download.Client, rel download.Release, archive, pin, channel string) error {
	archive, err := c.Ensure(ctx, rel, download.FirmwareAsset, archive)
	if err != nil {
		return err
	}
	want, pinned := semver.Parse(pin)
	installed := map[string]string{}
	names, err := download.Extract(archive, cfg.FirmwareDir, func(name string) bool {
		hw, ok := splitFirmwareName(name)
		if !ok {
			return false
		}
		imgVersion, ver, ok := parseFirmwareName(hw, name)
		if !ok {
			return false
		}
		if pinned {
			// A pin bypasses the channel: it names the exact image (ADR 0089).
			return semver.Compare(ver, want) == 0
		}
		return channelAllowsVersion(channel, imgVersion)
	})
	if err != nil {
		return err
	}
	for _, name := range names {
		hw, _ := splitFirmwareName(name)
		imgVersion, _, _ := parseFirmwareName(hw, name)
		installed[hw] = imgVersion
		slog.Info("firmware downloaded", "hardware", hw, "version", imgVersion)
	}
	s.download.installed(nil, installed)
	return nil
}

// keepNewest drops the previous archives of a train's cache, keeping the one
// this check touched: it was downloaded, verified and installed, so the older
// file is no longer needed (ADR 0090). Nothing is deleted when the check only
// answered from cache.json.
func keepNewest(c *download.Client, archive string) {
	if archive == "" {
		return
	}
	if _, err := os.Stat(archive); err != nil {
		return
	}
	c.Prune(archive)
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
