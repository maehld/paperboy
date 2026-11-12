package download

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// releaseServer serves a fake release API and the files of its releases.
type releaseServer struct {
	t      *testing.T
	tags   []string
	files  map[string][]byte // "<tag>/<name>"
	pre    map[string]bool   // tag -> published as a prerelease
	notMod atomic.Int64
	etag   string
	noEtag bool
}

func newReleaseServer(t *testing.T, tags ...string) (*releaseServer, *httptest.Server) {
	rs := &releaseServer{t: t, tags: tags, files: map[string][]byte{}, pre: map[string]bool{}, etag: `"v1"`}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/me/paperboy/releases", func(w http.ResponseWriter, r *http.Request) {
		if rs.noEtag || r.Header.Get("If-None-Match") != rs.etag {
			w.Header().Set("ETag", rs.etag)
			type asset struct {
				Name string `json:"name"`
				URL  string `json:"browser_download_url"`
			}
			type release struct {
				TagName    string  `json:"tag_name"`
				Prerelease bool    `json:"prerelease"`
				Assets     []asset `json:"assets"`
			}
			var out []release
			for _, tag := range rs.tags {
				rel := release{TagName: tag, Prerelease: rs.pre[tag]}
				for key := range rs.files {
					fileTag, name, ok := strings.Cut(key, "/")
					if ok && fileTag == tag {
						rel.Assets = append(rel.Assets, asset{Name: name, URL: rs.url(r, tag, name)})
					}
				}
				out = append(out, rel)
			}
			json.NewEncoder(w).Encode(out)
			return
		}
		rs.notMod.Add(1)
		w.WriteHeader(http.StatusNotModified)
	})
	mux.HandleFunc("/releases/download/", func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/releases/download/")
		data, ok := rs.files[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(data)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return rs, srv
}

func (rs *releaseServer) url(r *http.Request, tag, name string) string {
	return "http://" + r.Host + "/releases/download/" + tag + "/" + name
}

// add stores a file of a release; withSHA256 adds its checksum to SHA256SUMS.
func (rs *releaseServer) add(t *testing.T, tag, name string, data []byte) {
	rs.files[tag+"/"+name] = data
	sums := rs.sumFile(tag)
	sum := sha256.Sum256(data)
	sums = append(sums, []byte(hex.EncodeToString(sum[:])+"  "+name+"\n")...)
	rs.files[tag+"/SHA256SUMS"] = sums
}

// sumFile returns the checksums collected for the release so far.
func (rs *releaseServer) sumFile(tag string) []byte {
	return bytes.Clone(rs.files[tag+"/SHA256SUMS"])
}

// zipOf builds an archive with the given entries.
func zipOf(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestListUsesETag(t *testing.T) {
	rs, srv := newReleaseServer(t, "v2.1.0")
	rs.add(t, "v2.1.0", "paperboy-plugins-"+platformSuffix()+".zip", []byte("zip"))
	c := New(srv.URL+"/repos/me/paperboy/releases", "", t.TempDir())

	releases, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) != 1 || releases[0].Tag != "v2.1.0" || releases[0].Version != "2.1.0" {
		t.Fatalf("releases: %+v", releases)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "cache.json")); err != nil {
		t.Fatal("cache file missing")
	}
	again, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rs.notMod.Load() != 1 {
		t.Fatalf("second list must be conditional, got %d not-modified answers", rs.notMod.Load())
	}
	if len(again) != 1 || again[0].Tag != "v2.1.0" {
		t.Fatalf("cached releases: %+v", again)
	}
}

func TestVersionOf(t *testing.T) {
	for tag, want := range map[string]string{"v2.1.0": "2.1.0", "plugins-v2.2.0": "2.2.0", "firmware-v1.0.0": "1.0.0"} {
		if got := versionOf(tag); got != want {
			t.Fatalf("%s: %q", tag, got)
		}
	}
}

func TestPluginEntry(t *testing.T) {
	cases := map[string]struct {
		plugin, version string
		ok              bool
	}{
		"testplugin-v1.4.0":       {"testplugin", "1.4.0", true},
		"testplugin-v1.4.0.exe":   {"testplugin", "1.4.0", true},
		"testplugin-v2":           {"testplugin", "2", true},
		"testplugin-v1.4.0-rc.1":  {"testplugin", "1.4.0-rc.1", true},
		"testplugin-v1.4.0-bad/1": {"", "", false},
		"testplugin":              {"", "", false}, // an old zip ships unversioned programs
		"testplugin-vnext":        {"", "", false},
		"-v1.0.0":                 {"", "", false},
	}
	for name, want := range cases {
		plugin, version, ok := PluginEntry(name)
		if plugin != want.plugin || version != want.version || ok != want.ok {
			t.Fatalf("%s: %q %q %v", name, plugin, version, ok)
		}
	}
}

// TestPluginZipReadsTheVersionsInTheNames verifies that a plugin zip is read
// once and answers the version of every program it carries (ADR 0075).
func TestPluginZipReadsTheVersionsInTheNames(t *testing.T) {
	rs, srv := newReleaseServer(t, "v2.1.0")
	rs.add(t, "v2.1.0", PluginAsset(), zipOf(t, map[string]string{
		"plugins/weather-v1.4.0":  "weather",
		"plugins/calendar-v1.0.0": "calendar",
		"plugins/notes":           "unversioned",
	}))
	c := New(srv.URL+"/repos/me/paperboy/releases", "", t.TempDir())
	rel, ok, err := c.Latest(context.Background(), func(a Asset) bool { return a.Name == PluginAsset() })
	if err != nil || !ok {
		t.Fatalf("latest: %v %v", ok, err)
	}
	versions, path, err := c.PluginZip(context.Background(), rel)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "v2.1.0-"+PluginAsset() {
		t.Fatalf("archive: %q", path)
	}
	if len(versions) != 2 || versions["weather"] != "1.4.0" || versions["calendar"] != "1.0.0" {
		t.Fatalf("versions: %v", versions)
	}
	// The entry list outlives the zip (ADR 0090): after the cache dropped it,
	// the versions come from cache.json without a download, and Ensure fetches
	// the zip again only when it has to be unpacked.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	again, local, err := c.PluginZip(context.Background(), rel)
	if err != nil || again["weather"] != "1.4.0" || local != path {
		t.Fatalf("second read: %v %q %v", again, local, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a cached entry list must not download the zip again")
	}
	if _, err := c.Ensure(context.Background(), rel, PluginAsset(), local); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Ensure must fetch the zip again: %v", err)
	}
}

// TestListKeepsPrereleases verifies that the channel, not the list, decides
// about a prerelease (ADR 0089).
func TestListKeepsPrereleases(t *testing.T) {
	rs, srv := newReleaseServer(t, "v2.2.0-rc.1", "v2.1.0")
	rs.pre["v2.2.0-rc.1"] = true
	rs.add(t, "v2.2.0-rc.1", FirmwareAsset, []byte("rc"))
	rs.add(t, "v2.1.0", FirmwareAsset, []byte("final"))
	c := New(srv.URL+"/repos/me/paperboy/releases", "", t.TempDir())

	releases, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) != 2 || releases[0].Tag != "v2.2.0-rc.1" || !releases[0].Prerelease || releases[1].Prerelease {
		t.Fatalf("releases: %+v", releases)
	}
	if releases[0].Version != "2.2.0-rc.1" {
		t.Fatalf("version of a prerelease tag: %q", releases[0].Version)
	}
}

// TestCachedVersionsAnswersWithoutADownload reads a zip once and answers its
// versions afterwards, which keeps a pin's lookup free (ADR 0090).
func TestPruneKeepsOneZipPerAsset(t *testing.T) {
	rs, srv := newReleaseServer(t, "v2.1.0", "v2.0.0")
	for _, tag := range []string{"v2.1.0", "v2.0.0"} {
		rs.add(t, tag, PluginAsset(), zipOf(t, map[string]string{"plugins/weather-v1.0.0": "program"}))
	}
	c := New(srv.URL+"/repos/me/paperboy/releases", "", t.TempDir())
	releases, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, archive, err := c.PluginZip(context.Background(), releases[1])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Dir, "notes.txt"), []byte("stray"), 0o644); err != nil {
		t.Fatal(err)
	}
	c.Prune(archive)
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, e := range entries {
		left = append(left, e.Name())
	}
	if len(left) != 2 || left[0] != "cache.json" || left[1] != filepath.Base(archive) {
		t.Fatalf("cache after pruning: %v", left)
	}
}

func TestCachedVersionsAnswersWithoutADownload(t *testing.T) {
	rs, srv := newReleaseServer(t, "v2.1.0")
	rs.add(t, "v2.1.0", PluginAsset(), zipOf(t, map[string]string{"plugins/weather-v1.4.0-rc.1": "program"}))
	c := New(srv.URL+"/repos/me/paperboy/releases", "", t.TempDir())
	releases, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.CachedVersions("v2.1.0"); ok {
		t.Fatal("an unread release has no cached versions")
	}
	if _, _, err := c.PluginZip(context.Background(), releases[0]); err != nil {
		t.Fatal(err)
	}
	versions, ok := c.CachedVersions("v2.1.0")
	if !ok || versions["weather"] != "1.4.0-rc.1" {
		t.Fatalf("cached versions: %v %v", versions, ok)
	}
}

func TestBundleUsesConstantName(t *testing.T) {
	rs, srv := newReleaseServer(t, "v2.1.0")
	rs.add(t, "v2.1.0", FirmwareAsset, zipOf(t, map[string]string{"firmware/e1001-v1.0.0.bin": "image"}))
	c := New(srv.URL+"/repos/me/paperboy/releases", "", t.TempDir())
	rel, ok, err := c.Latest(context.Background(), func(a Asset) bool { return a.Name == FirmwareAsset })
	if err != nil || !ok {
		t.Fatalf("latest: %v %v", ok, err)
	}
	entries, path, err := c.Bundle(context.Background(), rel)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "v2.1.0-"+FirmwareAsset || len(entries) != 1 {
		t.Fatalf("bundle: %q %v", path, entries)
	}
	// The old versioned name is no longer a bundle (ADR 0063).
	legacy := Release{Tag: "v2.1.0", Assets: []Asset{{Name: "paperboy-firmware-2.1.0.zip"}}}
	if bundleName(legacy) != "" {
		t.Fatalf("legacy bundle name must not match: %q", bundleName(legacy))
	}
}

func TestFetchVerifiesChecksum(t *testing.T) {
	rs, srv := newReleaseServer(t, "v2.1.0")
	rs.add(t, "v2.1.0", FirmwareAsset, []byte("bundle"))
	rs.files["v2.1.0/SHA256SUMS"] = []byte("0000  " + FirmwareAsset + "\n")
	c := New(srv.URL+"/repos/me/paperboy/releases", "", t.TempDir())

	rel, ok, err := c.Latest(context.Background(), func(a Asset) bool { return a.Name == FirmwareAsset })
	if err != nil || !ok {
		t.Fatalf("latest: %v %v", ok, err)
	}
	if _, err := c.Fetch(context.Background(), rel, FirmwareAsset); err == nil ||
		!strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("wrong checksum must fail: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "v2.1.0-"+FirmwareAsset)); !os.IsNotExist(err) {
		t.Fatal("a file with a wrong checksum must be removed")
	}

	rs.add(t, "v2.1.0", FirmwareAsset, []byte("bundle"))
	c = New(srv.URL+"/repos/me/paperboy/releases", "", t.TempDir()) // a check starts with fresh checksums
	again, _, err := c.Latest(context.Background(), func(a Asset) bool { return a.Name == FirmwareAsset })
	if err != nil {
		t.Fatal(err)
	}
	path, err := c.Fetch(context.Background(), again, FirmwareAsset)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "bundle" {
		t.Fatalf("download: %q %v", data, err)
	}
}

func TestExtractKeepsFlatEntries(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "bundle.zip")
	data := zipOf(t, map[string]string{
		"firmware/sticky-v1.0.0.bin":         "image",
		"firmware/e1001-v1.0.0.bin":          "other hardware",
		"firmware/notes.txt":                 "not an image",
		"firmware/../evil.bin":               "escape",
		"plugins/weather":                    "program",
		"plugins/sub/calendar":               "too deep",
		"plugins/weather-v9.9.9":             "versioned",
		"firmware/lcd2004/lcd2004-1.0.0.bin": "old bundle layout",
	})
	if err := os.WriteFile(archive, data, 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "firmware")
	names, err := Extract(archive, dest, func(name string) bool {
		return name == "sticky-v1.0.0.bin" || name == "weather" || name == "evil.bin" || name == "lcd2004-1.0.0.bin"
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 3 || names[0] != "lcd2004-1.0.0.bin" || names[1] != "sticky-v1.0.0.bin" || names[2] != "weather" {
		t.Fatalf("installed: %v", names)
	}
	if _, err := os.Stat(filepath.Join(dest, "sticky-v1.0.0.bin")); err != nil {
		t.Fatal(err)
	}
	// An old bundle keeps its images below the hardware type; they land flat.
	if _, err := os.Stat(filepath.Join(dest, "lcd2004-1.0.0.bin")); err != nil {
		t.Fatal(err)
	}
	// Existing files are never overwritten.
	if _, err := Extract(archive, dest, func(string) bool { return true }); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dest, "sticky-v1.0.0.bin")); string(data) != "image" {
		t.Fatalf("file was replaced: %q", data)
	}
}

// platformSuffix returns the platform part of the plugin asset name.
func platformSuffix() string {
	name := PluginAsset()
	return strings.TrimSuffix(strings.TrimPrefix(name, "paperboy-plugins-"), ".zip")
}
