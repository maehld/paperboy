// Package download fetches plugin and firmware assets from releases
// (ADR 0060). It lists the releases of a repository, picks the newest one that
// carries a wanted asset, verifies a download against the release's
// SHA256SUMS, and unpacks the archives into the instance directories.
package download

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/internal/semver"
)

// FirmwareAsset is the firmware bundle of a release; its name is constant
// because the release tag carries the version (ADR 0063).
const FirmwareAsset = "paperboy-firmware.zip"

// OfficialSource is the repository Paperboy comes from.
const OfficialSource = "https://github.com/maehld/paperboy"

const (
	// maxAsset caps one downloaded file; bundles are a few megabytes.
	maxAsset = 64 << 20
	// maxEntry caps one file inside an archive.
	maxEntry = 32 << 20
)

// Asset is one file of a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Release is one release as far as Paperboy needs it.
type Release struct {
	Tag        string  `json:"tag"`        // v2.1.0, plugins-v2.2.0, firmware-v1.0.0
	Version    string  `json:"version"`    // tag without the train prefix: 2.1.0
	Prerelease bool    `json:"prerelease"` // published as a prerelease (ADR 0089)
	Assets     []Asset `json:"assets"`
}

// Asset returns the named asset of the release.
func (r Release) Asset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// Client talks to the release API of one repository.
type Client struct {
	Source string // https://github.com/maehld/paperboy, or the API base of a mirror
	Token  string // optional bearer token
	Dir    string // cache directory: <instance>/state/download
	HTTP   *http.Client

	mu   sync.Mutex
	sums map[string]map[string]string // tag -> SHA256SUMS by file name
}

// New returns a client for the given source; an empty source is the official
// repository.
func New(source, token, dir string) *Client {
	if source == "" {
		source = OfficialSource
	}
	return &Client{Source: source, Token: token, Dir: dir, HTTP: &http.Client{Timeout: 2 * time.Minute}}
}

// cacheFile is what the client remembers between checks (ADR 0060).
type cacheFile struct {
	ETag       string              `json:"etag,omitempty"`
	Releases   []Release           `json:"releases,omitempty"`
	Bundles    map[string][]string `json:"bundles,omitempty"`     // tag -> entries of its firmware bundle
	PluginZips map[string][]string `json:"plugin_zips,omitempty"` // tag -> entries of its plugin zip (ADR 0075)
}

// List returns the recent releases, newest first, drafts aside. Prereleases are
// part of the answer: the channel decides per train whether they may be used
// (ADR 0089). The answer is cached with its ETag, so a check that changes
// nothing costs one conditional request.
func (c *Client) List(ctx context.Context) ([]Release, error) {
	cache := c.readCache()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBase()+"?per_page=100", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	c.authorize(req)
	if cache.ETag != "" {
		req.Header.Set("If-None-Match", cache.ETag)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotModified:
		return cache.Releases, nil
	case http.StatusOK:
	default:
		return nil, fmt.Errorf("release list: %s", resp.Status)
	}
	var raw []struct {
		TagName    string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
		Draft      bool   `json:"draft"`
		Assets     []struct {
			Name    string `json:"name"`
			URL     string `json:"url"`                  // API URL: works for private repositories too
			Browser string `json:"browser_download_url"` // plain link, public repositories only
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("release list: %w", err)
	}
	releases := make([]Release, 0, len(raw))
	for _, r := range raw {
		if r.Draft {
			continue
		}
		rel := Release{Tag: r.TagName, Version: versionOf(r.TagName), Prerelease: r.Prerelease}
		for _, a := range r.Assets {
			u := a.URL
			if u == "" {
				u = a.Browser
			}
			rel.Assets = append(rel.Assets, Asset{Name: a.Name, URL: u})
		}
		releases = append(releases, rel)
	}
	cache.ETag = resp.Header.Get("ETag")
	cache.Releases = releases
	c.writeCache(cache)
	return releases, nil
}

// Latest returns the newest release that carries an asset matching pick, a
// prerelease included; callers that follow a channel filter before.
func (c *Client) Latest(ctx context.Context, pick func(Asset) bool) (Release, bool, error) {
	releases, err := c.List(ctx)
	if err != nil {
		return Release{}, false, err
	}
	for _, rel := range releases {
		for _, a := range rel.Assets {
			if pick(a) {
				return rel, true, nil
			}
		}
	}
	return Release{}, false, nil
}

// Fetch downloads an asset into the cache directory and verifies it against the
// release's SHA256SUMS. It returns the local file name.
func (c *Client) Fetch(ctx context.Context, rel Release, name string) (string, error) {
	asset, ok := rel.Asset(name)
	if !ok {
		return "", fmt.Errorf("%s: no asset %s", rel.Tag, name)
	}
	if strings.ContainsAny(name, `/\`) || name != filepath.Base(name) {
		return "", fmt.Errorf("%s: invalid asset name", name)
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return "", err
	}
	target := filepath.Join(c.Dir, rel.Tag+"-"+name)
	if err := c.download(ctx, asset.URL, target); err != nil {
		return "", err
	}
	sums, err := c.checksums(ctx, rel)
	if err != nil {
		os.Remove(target)
		return "", err
	}
	want, ok := sums[name]
	if !ok {
		os.Remove(target)
		return "", fmt.Errorf("%s: %s is missing in SHA256SUMS", rel.Tag, name)
	}
	got, err := fileSHA256(target)
	if err != nil {
		os.Remove(target)
		return "", err
	}
	if !strings.EqualFold(got, want) {
		os.Remove(target)
		return "", fmt.Errorf("%s: checksum mismatch for %s", rel.Tag, name)
	}
	return target, nil
}

// Bundle returns the entry names of the release's firmware bundle and the local
// zip; a bundle is only downloaded and read once per release. The answer needs
// no file on disk: the entries outlive a pruned zip (ADR 0090), and Ensure
// fetches the bundle again when it has to be unpacked.
func (c *Client) Bundle(ctx context.Context, rel Release) ([]string, string, error) {
	cache := c.readCache()
	if entries, ok := cache.Bundles[rel.Tag]; ok {
		return entries, filepath.Join(c.Dir, rel.Tag+"-"+bundleName(rel)), nil
	}
	name := bundleName(rel)
	if name == "" {
		return nil, "", fmt.Errorf("%s: no firmware bundle", rel.Tag)
	}
	local, err := c.Fetch(ctx, rel, name)
	if err != nil {
		return nil, "", err
	}
	entries, err := listZip(local)
	if err != nil {
		return nil, "", err
	}
	if cache.Bundles == nil {
		cache.Bundles = map[string][]string{}
	}
	cache.Bundles[rel.Tag] = entries
	c.writeCache(cache)
	return entries, local, nil
}

// bundleName returns the firmware bundle asset of a release.
func bundleName(rel Release) string {
	if _, ok := rel.Asset(FirmwareAsset); ok {
		return FirmwareAsset
	}
	return ""
}

// PluginZip returns the version of every program in the release's plugin zip by
// plugin name, plus the local zip; a zip is downloaded and read once per release
// (ADR 0075). A release without a plugin zip for this platform answers an error.
func (c *Client) PluginZip(ctx context.Context, rel Release) (map[string]string, string, error) {
	cache := c.readCache()
	local := filepath.Join(c.Dir, rel.Tag+"-"+PluginAsset())
	if entries, ok := cache.PluginZips[rel.Tag]; ok {
		return pluginVersions(entries), local, nil
	}
	if _, ok := rel.Asset(PluginAsset()); !ok {
		return nil, "", fmt.Errorf("%s: no plugin zip", rel.Tag)
	}
	local, err := c.Fetch(ctx, rel, PluginAsset())
	if err != nil {
		return nil, "", err
	}
	entries, err := listZip(local)
	if err != nil {
		return nil, "", err
	}
	if cache.PluginZips == nil {
		cache.PluginZips = map[string][]string{}
	}
	cache.PluginZips[rel.Tag] = entries
	c.writeCache(cache)
	return pluginVersions(entries), local, nil
}

// Ensure returns the local archive, fetching it when the cache no longer holds
// it (ADR 0090): the entry lists survive a pruned zip, the zip itself does not.
func (c *Client) Ensure(ctx context.Context, rel Release, name, local string) (string, error) {
	if local != "" {
		if _, err := os.Stat(local); err == nil {
			return local, nil
		}
	}
	return c.Fetch(ctx, rel, name)
}

// CachedVersions answers the plugin versions of a release whose zip was read
// before, without downloading anything (ADR 0089). ok is false for a release
// the cache does not know.
func (c *Client) CachedVersions(tag string) (map[string]string, bool) {
	entries, ok := c.readCache().PluginZips[tag]
	if !ok {
		return nil, false
	}
	return pluginVersions(entries), true
}

// Prune removes every archive in the cache directory except keep, so at most
// one file per asset stays (ADR 0090). Best effort: a file that a running
// program locks is logged and left for the next check, and cache.json is never
// touched.
func (c *Client) Prune(keep string) {
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		return
	}
	keep = filepath.Base(keep)
	cache := filepath.Base(c.cachePath())
	for _, e := range entries {
		if e.IsDir() || e.Name() == keep || e.Name() == cache {
			continue
		}
		if err := os.Remove(filepath.Join(c.Dir, e.Name())); err != nil {
			slog.Warn("download cache not removable", "file", e.Name(), "error", err)
			continue
		}
		slog.Info("download cache pruned", "file", e.Name())
	}
}

// pluginVersions maps every program of a plugin zip to the version its name
// carries; entries without a version (an old zip) are left out.
func pluginVersions(entries []string) map[string]string {
	versions := map[string]string{}
	for _, entry := range entries {
		name, version, ok := PluginEntry(path.Base(entry))
		if !ok {
			continue
		}
		versions[name] = version
	}
	return versions
}

// PluginEntry splits the name of a program from the plugin zip,
// <plugin>-v<version>[.exe], into plugin and version. An unversioned program of
// an older zip does not parse (ADR 0075).
func PluginEntry(file string) (plugin, ver string, ok bool) {
	base := strings.TrimSuffix(file, ".exe")
	name, rest, found := strings.Cut(base, "-v")
	if !found || name == "" {
		return "", "", false
	}
	if _, ok := semver.Parse(rest); !ok {
		return "", "", false
	}
	return name, rest, true
}

// Extract unpacks the entries of a zip that keep accepts into destDir, flat and
// without paths. Existing files are left alone. It returns what it installed.
func Extract(zipPath, destDir string, keep func(name string) bool) ([]string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, err
	}
	var installed []string
	for _, f := range zr.File {
		if !f.Mode().IsRegular() {
			continue // no directories, links or devices
		}
		clean := strings.ReplaceAll(f.Name, `\`, "/")
		name, dir := path.Base(clean), path.Dir(clean)
		switch {
		case dir == "plugins", dir == "firmware":
		case strings.HasPrefix(dir, "firmware/"):
			// An old bundle keeps its images below the hardware type
			// (ADR 0043, 0059); they are installed flat (ADR 0060).
			if strings.Contains(strings.TrimPrefix(dir, "firmware/"), "/") {
				continue
			}
		default:
			continue
		}
		if name == "." || strings.HasPrefix(name, ".") {
			continue
		}
		if !keep(name) {
			continue
		}
		target := filepath.Join(destDir, name)
		if _, err := os.Stat(target); err == nil {
			continue // never overwrite: a new version is a new name
		}
		if err := extractFile(f, target); err != nil {
			return installed, err
		}
		installed = append(installed, name)
	}
	sort.Strings(installed)
	return installed, nil
}

// extractFile writes one archive entry next to its target and renames it, so a
// reader never sees a half-written file.
func extractFile(f *zip.File, target string) error {
	if f.UncompressedSize64 > maxEntry {
		return fmt.Errorf("%s: entry too large", f.Name)
	}
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	tmp := target + ".part"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, io.LimitReader(r, maxEntry)); err != nil {
		file.Close()
		os.Remove(tmp)
		return err
	}
	if err := file.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// listZip returns the entry names of an archive.
func listZip(name string) ([]string, error) {
	zr, err := zip.OpenReader(name)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	entries := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		if f.Mode().IsRegular() {
			entries = append(entries, f.Name)
		}
	}
	return entries, nil
}

// download streams a URL into target.
func (c *Client) download(ctx context.Context, rawURL, target string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	c.authorize(req)
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", rawURL, resp.Status)
	}
	tmp := target + ".part"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, io.LimitReader(resp.Body, maxAsset)); err != nil {
		file.Close()
		os.Remove(tmp)
		return err
	}
	if err := file.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, target)
}

// checksums returns the SHA256SUMS of a release, read once per release.
func (c *Client) checksums(ctx context.Context, rel Release) (map[string]string, error) {
	c.mu.Lock()
	if c.sums == nil {
		c.sums = map[string]map[string]string{}
	}
	sums, ok := c.sums[rel.Tag]
	c.mu.Unlock()
	if ok {
		return sums, nil
	}
	asset, ok := rel.Asset("SHA256SUMS")
	if !ok {
		return nil, fmt.Errorf("%s: no SHA256SUMS", rel.Tag)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return nil, err
	}
	c.authorize(req)
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: SHA256SUMS: %s", rel.Tag, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	sums = parseSums(string(body))
	c.mu.Lock()
	c.sums[rel.Tag] = sums
	c.mu.Unlock()
	return sums, nil
}

// parseSums reads the "<hex>  <name>" lines of a SHA256SUMS file.
func parseSums(body string) map[string]string {
	sums := map[string]string{}
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := path.Base(strings.TrimPrefix(fields[1], "*"))
		sums[name] = strings.ToLower(fields[0])
	}
	return sums
}

// fileSHA256 returns the hex digest of a file.
func fileSHA256(name string) (string, error) {
	file, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// authorize adds the token when one is configured.
func (c *Client) authorize(req *http.Request) {
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
}

// apiBase returns the release API endpoint of the source: github.com URLs map
// to the REST API, anything else is used as the endpoint (with /releases
// appended when it does not name it already).
func (c *Client) apiBase() string {
	source := strings.TrimSuffix(strings.TrimSpace(c.Source), "/")
	if source == "" {
		source = OfficialSource
	}
	u, err := url.Parse(source)
	if err != nil {
		return source + "/releases"
	}
	if u.Host == "github.com" || u.Host == "www.github.com" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 2 {
			return "https://api.github.com/repos/" + parts[0] + "/" + parts[1] + "/releases"
		}
	}
	if strings.HasSuffix(source, "/releases") {
		return source
	}
	return source + "/releases"
}

// versionOf strips the train prefix of a tag: v2.1.0, plugins-v2.2.0 and
// firmware-v1.0.0 become 2.1.0, 2.2.0 and 1.0.0.
func versionOf(tag string) string {
	v := tag
	if _, rest, ok := strings.Cut(v, "-v"); ok {
		v = rest
	}
	return strings.TrimPrefix(v, "v")
}

// AssetName returns the plugin zip of this platform.
func PluginAsset() string {
	return "paperboy-plugins-" + runtime.GOOS + "-" + runtime.GOARCH + ".zip"
}

// readCache loads the cached release list; a broken file is ignored.
func (c *Client) readCache() cacheFile {
	var cache cacheFile
	data, err := os.ReadFile(c.cachePath())
	if err != nil {
		return cache
	}
	if err := json.Unmarshal(data, &cache); err != nil {
		slog.Warn("download cache unreadable", "file", c.cachePath(), "error", err)
		return cacheFile{}
	}
	return cache
}

func (c *Client) writeCache(cache cacheFile) {
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		slog.Warn("download cache not writable", "dir", c.Dir, "error", err)
		return
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(c.cachePath(), data, 0o644); err != nil {
		slog.Warn("download cache not writable", "file", c.cachePath(), "error", err)
	}
}

func (c *Client) cachePath() string { return filepath.Join(c.Dir, "cache.json") }
