package paperboy

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// firmwareImage is one OTA image in <instance>/firmware/ (ADR 0043, 0060).
type firmwareImage struct {
	Hardware string
	Version  string // without the leading v, e.g. 1.0.0
	File     string // <hardware>-v<version>.bin
	MD5      string
	path     string
	parts    []int
}

// URLPath is where the device downloads the image.
func (img *firmwareImage) URLPath() string {
	return "/firmware/" + img.File
}

var firmwareVersionPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*$`)

// firmwareOldLayout marks an old firmware/<hardware>/ directory on the
// dashboard; its images are still read (ADR 0060).
const firmwareOldLayout = "old layout"

// parseFirmwareName returns the version of <hardware>-v<version>.bin. The v is
// optional so that hand-placed images of the old layout keep working (ADR 0043,
// 0060).
func parseFirmwareName(hardware, name string) (string, []int, bool) {
	rest, ok := strings.CutPrefix(name, hardware+"-")
	if !ok {
		return "", nil, false
	}
	version, ok := strings.CutSuffix(rest, ".bin")
	if !ok {
		return "", nil, false
	}
	version = strings.TrimPrefix(version, "v")
	if !firmwareVersionPattern.MatchString(version) {
		return "", nil, false
	}
	parts, ok := versionParts(version)
	return version, parts, ok
}

// pinVersion normalizes a configured firmware version ("v1.0.0" and "1.0.0"
// name the same image); the empty string means no pin.
func pinVersion(pin string) []int {
	pin = strings.TrimPrefix(strings.TrimSpace(pin), "v")
	if !firmwareVersionPattern.MatchString(pin) {
		return nil
	}
	parts, ok := versionParts(pin)
	if !ok {
		return nil
	}
	return parts
}

// versionParts splits a dot-separated numeric version.
func versionParts(version string) ([]int, bool) {
	var parts []int
	for _, p := range strings.Split(version, ".") {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		parts = append(parts, n)
	}
	return parts, true
}

// compareVersions compares part by part as numbers; missing parts count as 0.
func compareVersions(a, b []int) int {
	for i := 0; i < max(len(a), len(b)); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// sameVersion reports whether the device runs the image's version.
func sameVersion(reported string, img *firmwareImage) bool {
	parts, ok := versionParts(reported)
	return ok && compareVersions(parts, img.parts) == 0
}

// md5Entry caches the checksum of one file; mod and size detect changes.
type md5Entry struct {
	mod  time.Time
	size int64
	sum  string
}

// firmwareStore reads the firmware directory: the flat
// <hardware>-v<version>.bin files and, for older instances, the images below
// <hardware>/. Paperboy writes there too when it downloads a release
// (ADR 0060); checksums are cached until a file changes.
type firmwareStore struct {
	dir string

	mu       sync.Mutex
	sums     map[string]md5Entry
	reported map[string]bool // invalid names already logged
}

func newFirmwareStore(dir string) *firmwareStore {
	return &firmwareStore{dir: dir, sums: map[string]md5Entry{}, reported: map[string]bool{}}
}

// latest returns the highest valid image for a hardware type, or nil. A pinned
// version (ADR 0060) limits the answer to that image; higher files stay on disk
// unused. Invalid file names are logged once.
func (f *firmwareStore) latest(hardware, pin string) (*firmwareImage, []string) {
	if f == nil || !validName(hardware) {
		return nil, nil
	}
	want := pinVersion(pin)
	var best *firmwareImage
	var invalid []string
	scan := func(dir, label string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) && label == "" {
				slog.Error("firmware directory unreadable", "dir", dir, "error", err)
			}
			return
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasPrefix(e.Name(), hardware+"-") {
				continue
			}
			version, parts, ok := parseFirmwareName(hardware, e.Name())
			if !ok {
				invalid = append(invalid, label+e.Name())
				continue
			}
			if want != nil && compareVersions(parts, want) != 0 {
				continue
			}
			if best == nil || compareVersions(parts, best.parts) > 0 {
				best = &firmwareImage{Hardware: hardware, Version: version, File: e.Name(),
					path: filepath.Join(dir, e.Name()), parts: parts}
			}
		}
	}
	// The flat directory wins ties: it is scanned first and only a strictly
	// higher version replaces the best image.
	scan(f.dir, "")
	scan(filepath.Join(f.dir, hardware), hardware+"/")
	for _, name := range invalid {
		f.note(name, "firmware file ignored: expected <hardware>-v<version>.bin")
	}
	if best == nil {
		return nil, invalid
	}
	sum, err := f.checksum(best.path)
	if err != nil {
		slog.Error("firmware image unreadable", "file", best.path, "error", err)
		return nil, invalid
	}
	best.MD5 = sum
	return best, invalid
}

// invalidNames lists ignored files for the dashboard: entries that are no
// image name of any hardware type, and old <hardware>/ directories (still read,
// ADR 0060). It does not consult the configuration: an image is named after the
// hardware a device reports, which no display type has to match (ADR 0061).
func (f *firmwareStore) invalidNames() []string {
	if f == nil {
		return nil
	}
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		return nil
	}
	var all []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			f.note(name+"/", "firmware directory in the old layout is still read (ADR 0060)")
			all = append(all, name+"/ ("+firmwareOldLayout+")")
			sub, err := os.ReadDir(filepath.Join(f.dir, name))
			if err != nil {
				continue
			}
			for _, se := range sub {
				if _, _, ok := parseFirmwareName(name, se.Name()); !ok {
					all = append(all, name+"/"+se.Name())
					f.note(name+"/"+se.Name(), "firmware file ignored: expected <hardware>-v<version>.bin")
				}
			}
			continue
		}
		if _, ok := splitFirmwareName(name); !ok {
			all = append(all, name)
			f.note(name, "firmware file ignored: expected <hardware>-v<version>.bin")
		}
	}
	sort.Strings(all)
	return all
}

// note logs one ignored entry once.
func (f *firmwareStore) note(name, msg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reported[name] {
		return
	}
	f.reported[name] = true
	slog.Warn(msg, "file", name)
}

func (f *firmwareStore) checksum(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	f.mu.Lock()
	e, ok := f.sums[path]
	f.mu.Unlock()
	if ok && e.mod.Equal(info.ModTime()) && e.size == info.Size() {
		return e.sum, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	h := md5.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	f.mu.Lock()
	f.sums[path] = md5Entry{mod: info.ModTime(), size: info.Size(), sum: sum}
	f.mu.Unlock()
	return sum, nil
}

// splitFirmwareName returns the hardware part of an image name
// <hardware>-v<version>.bin (or the old <hardware>-<version>.bin).
func splitFirmwareName(name string) (string, bool) {
	for i := 1; i < len(name); i++ {
		if name[i] != '-' {
			continue
		}
		hw := name[:i]
		if !validName(hw) {
			continue
		}
		if _, _, ok := parseFirmwareName(hw, name); ok {
			return hw, true
		}
	}
	return "", false
}

// handleFirmware serves an OTA image without a token, like page images. Only
// <hardware>-v<version>.bin names are served; an image in an old <hardware>/
// directory is still found (ADR 0043, 0060).
func (s *Server) handleFirmware(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if name != filepath.Base(name) || strings.HasPrefix(name, ".") {
		http.NotFound(w, r)
		return
	}
	hw, ok := splitFirmwareName(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.firmware.dir, name)
	if _, err := os.Stat(path); err != nil {
		path = filepath.Join(s.firmware.dir, hw, name)
	}
	file, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, name, info.ModTime(), file)
}

// firmwareOffer is the update offer in the sync response.
type firmwareOffer struct {
	Version string `json:"version"`
	Path    string `json:"path"`
	MD5     string `json:"md5"`
}

// Firmware status values for the devices API and the dashboard.
const (
	fwCurrent   = "current"
	fwOffered   = "offered"
	fwAvailable = "available" // newer image exists, but no offer yet (updates off, battery)
	fwFailed    = "failed"
	fwHeld      = "held"
	fwNone      = "none"
)

// firmwareStatus summarizes a device's firmware against the newest image.
func firmwareStatus(d Device, st deviceState, img *firmwareImage) string {
	switch {
	case img == nil:
		return fwNone
	case sameVersion(st.Firmware, img):
		return fwCurrent
	case d.Hold:
		return fwHeld
	case st.FirmwareOffered == img.Version && st.FirmwareOfferedMD5 == img.MD5 && st.FirmwareFailures >= firmwareFailureLimit:
		return fwFailed
	case st.FirmwarePending && st.FirmwareOffered == img.Version:
		return fwOffered
	}
	return fwAvailable
}

// settleFirmwareLocked evaluates an outstanding offer: the device either runs
// the offered version now or the attempt failed. Callers hold s.mu.
func (s *Server) settleFirmwareLocked(d *Device, st *deviceState) {
	if !st.FirmwarePending {
		return
	}
	st.FirmwarePending = false
	parts, ok := versionParts(st.FirmwareOffered)
	if reported, rok := versionParts(st.Firmware); ok && rok && compareVersions(parts, reported) == 0 {
		st.FirmwareFailures = 0
		slog.Info("firmware updated", "device", d.ID, "firmware", st.Firmware)
		return
	}
	st.FirmwareFailures++
	slog.Warn("firmware update failed", "device", d.ID, "offered", st.FirmwareOffered,
		"running", st.Firmware, "attempt", st.FirmwareFailures, "limit", firmwareFailureLimit)
}

// offerFirmwareLocked decides whether this sync carries an update offer and
// logs the decision when it changes. cfg is the configuration snapshot the
// caller works with; callers hold s.mu.
func (s *Server) offerFirmwareLocked(d *Device, st *deviceState, cfg *Config, img *firmwareImage, volts float64, haveVolts bool, now time.Time) *firmwareOffer {
	reason := ""
	// Text displays run on mains power (ADR 0044) and report no battery.
	mains := cfg.Profiles[d.Profile].text()
	switch {
	case img == nil || sameVersion(st.Firmware, img):
	case cfg.Firmware.Update != firmwareAuto:
		reason = "updates off"
	case d.Hold:
		reason = "held"
	case mains:
		if st.FirmwareOffered == img.Version && st.FirmwareOfferedMD5 == img.MD5 && st.FirmwareFailures >= firmwareFailureLimit {
			reason = "failed " + strconv.Itoa(st.FirmwareFailures) + " times"
			break
		}
		return s.makeOfferLocked(d, st, img, now)
	case !haveVolts:
		reason = "battery unknown"
	case volts < cfg.Firmware.MinBatteryVolts || st.BatteryLevel != batteryOK:
		reason = "battery low"
	case st.FirmwareOffered == img.Version && st.FirmwareOfferedMD5 == img.MD5 && st.FirmwareFailures >= firmwareFailureLimit:
		reason = "failed " + strconv.Itoa(st.FirmwareFailures) + " times"
	default:
		return s.makeOfferLocked(d, st, img, now)
	}
	// Deliberate holds stay quiet; reasons the operator may not expect are logged
	// once per change, so the decision is visible without flooding the log.
	quiet := reason == "updates off" || reason == "held"
	if img != nil && reason != "" && !quiet && s.fwNotes[d.ID] != reason {
		slog.Info("firmware update withheld", "device", d.ID, "running", st.Firmware, "available", img.Version, "reason", reason)
	}
	s.fwNotes[d.ID] = reason
	return nil
}

// makeOfferLocked records and returns an update offer. Callers hold s.mu.
func (s *Server) makeOfferLocked(d *Device, st *deviceState, img *firmwareImage, now time.Time) *firmwareOffer {
	if st.FirmwareOffered != img.Version || st.FirmwareOfferedMD5 != img.MD5 {
		st.FirmwareFailures = 0
	}
	st.FirmwareOffered, st.FirmwareOfferedMD5, st.FirmwareOfferedTime = img.Version, img.MD5, now
	st.FirmwarePending = true
	s.fwNotes[d.ID] = ""
	slog.Info("firmware offered", "device", d.ID, "running", st.Firmware, "offered", img.Version)
	return &firmwareOffer{Version: img.Version, Path: img.URLPath(), MD5: img.MD5}
}
