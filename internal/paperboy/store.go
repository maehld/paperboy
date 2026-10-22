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
	FriendlyID     string    `json:"friendly_id"`
	Page           int       `json:"page"`
	LastInput      time.Time `json:"last_input,omitempty"`
	LastSeen       time.Time `json:"last_seen,omitempty"`
	BatteryVoltage string    `json:"battery_voltage,omitempty"`
	RSSI           string    `json:"rssi,omitempty"`
	Firmware       string    `json:"firmware,omitempty"`
	Model          string    `json:"model,omitempty"`
	WakeReason     string    `json:"wake_reason,omitempty"`
	BatteryLevel   string    `json:"battery_level,omitempty"` // TRMNL: "", "warn" or "critical"
	Rule           string    `json:"rule,omitempty"`          // TRMNL plan: name of the active rule
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

// migrateState moves TRMNL entries from their MAC address to the device ID
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
