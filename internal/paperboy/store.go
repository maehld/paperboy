package paperboy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// deviceState is persisted per device ID in state/devices.json.
type deviceState struct {
	APIKey         string    `json:"api_key"`
	Page           int       `json:"page"`
	LastInput      time.Time `json:"last_input,omitempty"`
	LastSeen       time.Time `json:"last_seen,omitempty"`
	BatteryVoltage string    `json:"battery_voltage,omitempty"`
	BatteryPercent *float64  `json:"battery_percent,omitempty"` // fuel gauge, if the hardware has one
	BatteryMAH     *float64  `json:"battery_mah,omitempty"`     // remaining capacity from the fuel gauge
	RSSI           string    `json:"rssi,omitempty"`
	Firmware       string    `json:"firmware,omitempty"`
	Hardware       string    `json:"hardware,omitempty"` // firmware image type, e.g. "sticky"
	WakeReason     string    `json:"wake_reason,omitempty"`
	BatteryLevel   string    `json:"battery_level,omitempty"` // "", "warn" or "critical"
	Rule           string    `json:"rule,omitempty"`          // name of the active plan rule

	// What the device was last pointed at; set after rendering, so a failed
	// render shows up as a notice instead of a page that was never drawn.
	LastPage     string    `json:"last_page,omitempty"`      // page template; empty for a notice or the battery warning
	LastPageKind string    `json:"last_page_kind,omitempty"` // "page", "notice" or "battery"
	LastPageHash string    `json:"last_page_hash,omitempty"` // content hash, the same as the sync response's image hash
	LastPageTime time.Time `json:"last_page_time,omitempty"`
	LastRefresh  int       `json:"last_refresh,omitempty"` // reported refresh in seconds
	LastError    string    `json:"last_error,omitempty"`   // last render or plan error

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

func loadState(path string) (map[string]*deviceState, error) {
	state := map[string]*deviceState{}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return state, nil
}

// migrateState moves old entries from their MAC address to the device ID
// (ADR 0026). Entries that belong to no configured device stay untouched, so a
// removed device keeps its key. It returns the number of moved entries.
func migrateState(state map[string]*deviceState, devices []Device) (int, error) {
	byMAC := make(map[string]string, len(devices))
	for i := range devices {
		if devices[i].MAC != "" {
			byMAC[devices[i].MAC] = devices[i].ID
		}
	}
	out := make(map[string]*deviceState, len(state))
	moved := 0
	for key, st := range state {
		id := key
		if target, ok := byMAC[normalizeMAC(key)]; ok {
			id, moved = target, moved+1
		}
		if old, ok := out[id]; ok && old != st {
			return 0, fmt.Errorf("devices.json: several entries map to device %q; remove the outdated one", id)
		}
		out[id] = st
	}
	clear(state)
	for id, st := range out {
		state[id] = st
	}
	return moved, nil
}
