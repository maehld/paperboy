package paperboy

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// deviceState is persisted per device in state/devices/<MAC>.json (ADR 0080).
type deviceState struct {
	APIKey         string    `json:"api_key"`
	Page           int       `json:"page"`
	FirstSeen      time.Time `json:"first_seen,omitempty"`
	LastInput      time.Time `json:"last_input,omitempty"`
	LastSeen       time.Time `json:"last_seen,omitempty"`
	ClientIP       string    `json:"client_ip,omitempty"` // source address of the last register, sync or text poll
	BatteryVoltage string    `json:"battery_voltage,omitempty"`
	BatteryPercent *float64  `json:"battery_percent,omitempty"` // fuel gauge, if the hardware has one
	BatteryMAH     *float64  `json:"battery_mah,omitempty"`     // remaining capacity from the fuel gauge
	RSSI           string    `json:"rssi,omitempty"`
	Firmware       string    `json:"firmware,omitempty"`
	Hardware       string    `json:"hardware,omitempty"` // firmware image type, e.g. "sticky"
	WakeReason     string    `json:"wake_reason,omitempty"`
	BatteryLevel   string    `json:"battery_level,omitempty"`  // "", "warn" or "critical"
	Rule           string    `json:"rule,omitempty"`           // name of the active plan rule
	ReportedPower  string    `json:"reported_power,omitempty"` // power source the hardware last reported (ADR 0051)
	TouchReported  bool      `json:"reported_touch,omitempty"` // the firmware reports a digitiser (ADR 0080)

	// What the device was last pointed at; set after rendering, so a failed
	// render shows up as a notice instead of a page that was never drawn.
	LastPage     string    `json:"last_page,omitempty"`      // page template; empty for a notice or the battery warning
	LastPageKind string    `json:"last_page_kind,omitempty"` // "page", "notice" or "battery"
	LastPageHash string    `json:"last_page_hash,omitempty"` // content hash, the same as the sync response's image hash
	LastPageTime time.Time `json:"last_page_time,omitempty"`
	LastRefresh  int       `json:"last_refresh,omitempty"`  // reported refresh in seconds
	LastError    string    `json:"last_error,omitempty"`    // last render or plan error
	LastText     []string  `json:"last_text,omitempty"`     // text displays: the last served lines (ADR 0044)
	LastEventID  int64     `json:"last_event_id,omitempty"` // highest applied event ID (ADR 0064)

	// The device menu (ADR 0057): open/closed, phase, selection and the
	// resolved actions.
	Menu menuState `json:"menu,omitempty"`

	// Firmware updates (ADR 0043): the last offered version and its image
	// checksum, whether the offer is still unanswered, and failed attempts for
	// that version. Clearing firmware_failures lets Paperboy offer it again.
	FirmwareOffered     string    `json:"firmware_offered,omitempty"`
	FirmwareOfferedMD5  string    `json:"firmware_offered_md5,omitempty"`
	FirmwareOfferedTime time.Time `json:"firmware_offered_time,omitempty"`
	FirmwarePending     bool      `json:"firmware_pending,omitempty"`
	FirmwareFailures    int       `json:"firmware_failures,omitempty"`
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// deviceStateKey turns a MAC address into the key of its state file: upper case
// hex without separators, e.g. A0B1C2D3E4F5 (ADR 0080). The MAC is stable when
// a device file is renamed, so the state follows the hardware.
func deviceStateKey(mac string) (string, error) {
	hw, err := net.ParseMAC(strings.TrimSpace(mac))
	if err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(hw)), nil
}

// validStateKey reports whether a file name in state/devices/ is a device key.
func validStateKey(key string) bool {
	if len(key) != 12 && len(key) != 16 {
		return false
	}
	_, err := hex.DecodeString(key)
	return err == nil
}

// deviceStateFile names the state file of a device key.
func deviceStateFile(dir, key string) string {
	return filepath.Join(dir, strings.ToUpper(key)+".json")
}

// loadDeviceStates reads state/devices/ of an instance directory: one JSON file
// per device, named by its MAC (ADR 0080). A missing directory is an instance
// without device state. A file that cannot be read or parsed is reported and
// skipped, so one broken file cannot keep the other displays off the air.
func loadDeviceStates(dir string) (map[string]*deviceState, error) {
	state := map[string]*deviceState{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return state, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		base, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		key := strings.ToUpper(base)
		if !validStateKey(key) {
			slog.Warn("ignoring a state file whose name is not a MAC address", "file", filepath.Join(dir, e.Name()))
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			slog.Error("reading device state", "file", path, "error", err)
			continue
		}
		st := &deviceState{}
		if err := json.Unmarshal(data, st); err != nil {
			slog.Error("ignoring device state that does not parse", "file", path, "error", err)
			continue
		}
		state[key] = st
	}
	return state, nil
}

// formatMAC writes a MAC in the form the device files use, e.g.
// AA:BB:CC:DD:EE:01; an unparsable address comes back trimmed.
func formatMAC(mac string) string {
	key, err := deviceStateKey(mac)
	if err != nil {
		return strings.TrimSpace(mac)
	}
	return formatStateKey(key)
}

// formatStateKey writes a device key as a MAC address, e.g. A0:B1:C2:D3:E4:F5.
func formatStateKey(key string) string {
	raw, err := hex.DecodeString(key)
	if err != nil {
		return key
	}
	parts := make([]string, len(raw))
	for i, b := range raw {
		parts[i] = strings.ToUpper(hex.EncodeToString([]byte{b}))
	}
	return strings.Join(parts, ":")
}

// writeStateFile replaces one state file atomically; state holds API keys, so
// it is readable by the owner only (ADR 0080).
func writeStateFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// unclaimedDevice is a state file whose MAC no device file uses: hardware that
// registered but was never claimed (ADR 0080). The fields are copies, so a
// caller can show them without the state lock.
type unclaimedDevice struct {
	Key       string
	MAC       string
	Hardware  string
	Firmware  string
	ClientIP  string
	FirstSeen time.Time
	LastSeen  time.Time
}

// unclaimedDevices lists the state entries that no configured device owns,
// sorted by MAC. Callers take the configuration snapshot themselves.
func (s *Server) unclaimedDevices(cfg *Config) []unclaimedDevice {
	used := make(map[string]bool, len(cfg.Devices))
	for i := range cfg.Devices {
		used[cfg.Devices[i].stateKey()] = true
	}
	s.mu.Lock()
	out := make([]unclaimedDevice, 0, len(s.state))
	for key, st := range s.state {
		if used[key] {
			continue
		}
		out = append(out, unclaimedDevice{Key: key, MAC: formatStateKey(key),
			Hardware: st.Hardware, Firmware: st.Firmware, ClientIP: st.ClientIP,
			FirstSeen: st.FirstSeen, LastSeen: st.LastSeen})
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].MAC < out[j].MAC })
	return out
}

// deviceClientIPs returns the source address of the last contact of every
// configured device, keyed by its state key. Callers take the configuration
// snapshot themselves; the map is a copy, so no lock is needed afterwards.
func (s *Server) deviceClientIPs(cfg *Config) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(cfg.Devices))
	for i := range cfg.Devices {
		key := cfg.Devices[i].stateKey()
		if st := s.state[key]; st != nil && st.ClientIP != "" {
			out[key] = st.ClientIP
		}
	}
	return out
}

// forgetDeviceState drops the state file of one MAC, e.g. hardware that is no
// longer used. The next register writes a new file, so nothing is lost that
// cannot be rebuilt (ADR 0080).
func (s *Server) forgetDeviceState(key string) error {
	s.mu.Lock()
	delete(s.state, key)
	s.mu.Unlock()
	err := os.Remove(deviceStateFile(s.devicesDir, key))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
