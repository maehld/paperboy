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
	t       *testing.T
	tags    []string
	files   map[string][]byte // "<tag>/<name>"
	notMod  atomic.Int64
	etag    string
	noEtag  bool
	sumsBad bool
}

func newReleaseServer(t *testing.T, tags ...string) (*releaseServer, *httptest.Server) {
	rs := &releaseServer{t: t, tags: tags, files: map[string][]byte{}, etag: `"v1"`}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/me/paperboy", func(w http.ResponseWriter, r *http.Request) {
		if rs.noEtag || r.Header.Get("If-None-Match") != rs.etag {
			w.Header().Set("ETag", rs.etag)
			type asset struct {
				Name string `json:"name"`
				URL  string `json:"browser_download_url"`
			}
			type release struct {
				TagName string  `json:"tag_name"`
				Assets  []asset `json:"assets"`
			}
			var out []release
			for _, tag := range rs.tags {
				rel := release{TagName: tag}
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
	c := New(srv.URL+"/repos/me/paperboy", "", t.TempDir())

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

func TestFetchVerifiesChecksum(t *testing.T) {
	rs, srv := newReleaseServer(t, "v2.1.0")
	rs.add(t, "v2.1.0", "paperboy-firmware-2.1.0.zip", []byte("bundle"))
	rs.files["v2.1.0/SHA256SUMS"] = []byte("0000  paperboy-firmware-2.1.0.zip\n")
	c := New(srv.URL+"/repos/me/paperboy", "", t.TempDir())

	rel, ok, err := c.Latest(context.Background(), func(a Asset) bool { return a.Name == "paperboy-firmware-2.1.0.zip" })
	if err != nil || !ok {
		t.Fatalf("latest: %v %v", ok, err)
	}
	if _, err := c.Fetch(context.Background(), rel, "paperboy-firmware-2.1.0.zip"); err == nil ||
		!strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("wrong checksum must fail: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "v2.1.0-paperboy-firmware-2.1.0.zip")); !os.IsNotExist(err) {
		t.Fatal("a file with a wrong checksum must be removed")
	}

	rs.add(t, "v2.1.0", "paperboy-firmware-2.1.0.zip", []byte("bundle"))
	c = New(srv.URL+"/repos/me/paperboy", "", t.TempDir()) // a check starts with fresh checksums
	again, _, err := c.Latest(context.Background(), func(a Asset) bool { return a.Name == "paperboy-firmware-2.1.0.zip" })
	if err != nil {
		t.Fatal(err)
	}
	path, err := c.Fetch(context.Background(), again, "paperboy-firmware-2.1.0.zip")
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
		"firmware/sticky-v1.0.0.bin": "image",
		"firmware/e1001-v1.0.0.bin":  "other hardware",
		"firmware/notes.txt":         "not an image",
		"firmware/../evil.bin":       "escape",
		"plugins/weather":            "program",
		"plugins/sub/calendar":       "too deep",
		"plugins/weather-v9.9.9":     "versioned",
	})
	if err := os.WriteFile(archive, data, 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "firmware")
	names, err := Extract(archive, dest, func(name string) bool {
		return name == "sticky-v1.0.0.bin" || name == "weather" || name == "evil.bin"
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "sticky-v1.0.0.bin" || names[1] != "weather" {
		t.Fatalf("installed: %v", names)
	}
	if _, err := os.Stat(filepath.Join(dest, "sticky-v1.0.0.bin")); err != nil {
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
