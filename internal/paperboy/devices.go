package paperboy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/maehld/paperboy/internal/page"
)

// Devices live in files of their own (ADR 0070): devices/<id>.yaml, one file
// per device, parsed as strictly as pages and plans. The file name is the
// stable device ID; state/devices.json stays runtime state (cursor, battery,
// menu).

// deviceFile mirrors devices/<id>.yaml with what the operator wrote: an empty
// field keeps the default, so the editor writes back only what is set. The
// model is also the JSON model of the device API (ADR 0066).
type deviceFile struct {
	DisplayType string   `yaml:"display_type" json:"display_type"`
	Protocol    string   `yaml:"protocol,omitempty" json:"protocol,omitempty"`
	MAC         string   `yaml:"mac" json:"mac"`
	Address     string   `yaml:"address,omitempty" json:"address,omitempty"`
	Buttons     bool     `yaml:"buttons,omitempty" json:"buttons,omitempty"`
	Plan        string   `yaml:"plan,omitempty" json:"plan,omitempty"`
	Refresh     Duration `yaml:"refresh" json:"refresh,omitempty"`
	Power       string   `yaml:"power,omitempty" json:"power,omitempty"`
	Actions     string   `yaml:"actions,omitempty" json:"actions,omitempty"`
	MenuShow    Duration `yaml:"menu_show_duration" json:"menu_show_duration,omitempty"`
	Battery     *Battery `yaml:"battery,omitempty" json:"battery,omitempty"`
	Firmware    string   `yaml:"firmware,omitempty" json:"firmware,omitempty"`
}

// parseDeviceFile reads one device file strictly: an unknown key or a second
// document is an error, like in pages and plans.
func parseDeviceFile(data []byte) (deviceFile, error) {
	var f deviceFile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return f, errors.New("empty device file")
		}
		return f, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return f, errors.New("only one YAML document is allowed")
	}
	return f, nil
}

// validate checks what the file itself decides, before it becomes a Device.
func (f deviceFile) validate(id string) error {
	switch f.Firmware {
	case "", firmwareHold:
	default:
		return fmt.Errorf("device %s: firmware %q: only hold is allowed", id, f.Firmware)
	}
	return nil
}

// device builds the runtime device from a file; the default battery thresholds
// of paperboy.yaml fill an unset device override.
func (f deviceFile) device(id string, defaults *Battery) Device {
	return Device{
		ID:               id,
		Type:             f.Protocol,
		MAC:              f.MAC,
		Address:          f.Address,
		Buttons:          f.Buttons,
		Profile:          f.DisplayType,
		Refresh:          f.Refresh,
		Battery:          copyBattery(defaults, f.Battery),
		Plan:             f.Plan,
		Hold:             f.Firmware == firmwareHold,
		Power:            f.Power,
		Actions:          f.Actions,
		MenuShowDuration: f.MenuShow,
	}
}

// copyBattery merges a device's thresholds into the defaults without sharing a
// pointer: validation fills hysteresis in, which must not reach the defaults.
func copyBattery(defaults, override *Battery) *Battery {
	if override == nil && defaults == nil {
		return nil
	}
	if override == nil {
		d := *defaults
		return &d
	}
	o := *override
	return mergeBattery(defaults, &o)
}

// deviceFileNames lists the device files of a directory; a missing directory
// simply has no devices.
func deviceFileNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// deviceID strips the file suffix; the file name is the device ID.
func deviceID(file string) string {
	id, _ := strings.CutSuffix(file, ".yaml")
	return id
}

// loadDevices reads devices/<id>.yaml of an instance directory (ADR 0070). A
// device file that does not parse aborts the load, like an invalid
// paperboy.yaml; the caller keeps its last valid configuration (ADR 0047).
func loadDevices(dir string, defaults *Battery) ([]Device, error) {
	names, err := deviceFileNames(dir)
	if err != nil {
		return nil, err
	}
	devices := make([]Device, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		f, err := parseDeviceFile(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if err := f.validate(deviceID(name)); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		devices = append(devices, f.device(deviceID(name), defaults))
	}
	return devices, nil
}

// encodeDeviceFile writes a device file the way ParseDeviceFile reads it: a
// stable, hand-readable layout with only the values that are set.
func encodeDeviceFile(f *deviceFile) ([]byte, error) {
	root := mapping()
	set := func(key string, value *yaml.Node) {
		root.Content = append(root.Content, plainScalar(key), value)
	}
	protocol := f.Protocol
	if protocol == "" {
		protocol = TypePaperboy
	}
	set("display_type", plainScalar(f.DisplayType))
	set("protocol", plainScalar(protocol))
	set("mac", quotedScalar(f.MAC))
	if f.Address != "" {
		set("address", plainScalar(f.Address))
	}
	if f.Buttons {
		set("buttons", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
	}
	if f.Plan != "" {
		set("plan", plainScalar(f.Plan))
	}
	if f.Refresh != 0 {
		set("refresh", durationValue(time.Duration(f.Refresh)))
	}
	if f.Power != "" {
		set("power", plainScalar(f.Power))
	}
	if f.Actions != "" {
		set("actions", plainScalar(f.Actions))
	}
	if f.MenuShow != 0 {
		set("menu_show_duration", durationValue(time.Duration(f.MenuShow)))
	}
	if b := f.Battery; b != nil && (b.WarnVolts > 0 || b.CriticalVolts > 0 || b.HysteresisVolts > 0) {
		node := mapping()
		for _, field := range []struct {
			key   string
			value float64
		}{
			{"warn_volts", b.WarnVolts},
			{"critical_volts", b.CriticalVolts},
			{"hysteresis_volts", b.HysteresisVolts},
		} {
			if field.value > 0 {
				node.Content = append(node.Content, plainScalar(field.key), &yaml.Node{
					Kind: yaml.ScalarNode, Tag: "!!float",
					Value: strconv.FormatFloat(field.value, 'f', -1, 64)})
			}
		}
		set("battery", node)
	}
	if f.Firmware != "" {
		set("firmware", plainScalar(f.Firmware))
	}
	return marshalDeviceFile(root)
}

func mapping() *yaml.Node { return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"} }

func plainScalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

// quotedScalar keeps values such as a MAC address a string, not a number or
// something a YAML reader might reinterpret.
func quotedScalar(value string) *yaml.Node {
	n := plainScalar(value)
	n.Style = yaml.SingleQuotedStyle
	return n
}

func durationValue(d time.Duration) *yaml.Node { return plainScalar(durationText(d)) }

func marshalDeviceFile(node *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(node); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// editorDeviceFile is one device file in the overview of the editor.
type editorDeviceFile struct {
	ID      string `json:"id"`
	Display string `json:"display_type,omitempty"`
	MAC     string `json:"mac,omitempty"`
	Plan    string `json:"plan,omitempty"`
	Hash    string `json:"hash"`
	Error   string `json:"error,omitempty"`
}

// deviceFileList reads the device files of an instance directory for the
// editor; a file that does not parse still appears, with its error.
func deviceFileList(dir string) ([]editorDeviceFile, error) {
	names, err := deviceFileNames(dir)
	if err != nil {
		return nil, err
	}
	list := make([]editorDeviceFile, 0, len(names))
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		f := editorDeviceFile{ID: deviceID(name), Hash: fileHash(data)}
		if dev, err := parseDeviceFile(data); err != nil {
			f.Error = shortError(err)
		} else {
			f.Display, f.MAC, f.Plan = dev.DisplayType, dev.MAC, dev.Plan
		}
		list = append(list, f)
	}
	return list, nil
}

// deviceFilePath resolves devices/<id>.yaml; the ID is validated because it
// becomes a file name.
func deviceFilePath(dir, id string) (string, error) {
	if !page.ValidName(id) {
		return "", fmt.Errorf("device id %q: only a-z, 0-9 and -", id)
	}
	return filepath.Join(dir, id+".yaml"), nil
}

// devicesStamp summarizes the device files of a directory, so the
// configuration loader notices a change there even when paperboy.yaml did not
// change (ADR 0047, 0070).
func devicesStamp(dir string) string {
	names, err := deviceFileNames(dir)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, name := range names {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "%s:%d:%d;", name, info.Size(), info.ModTime().UnixNano())
	}
	return b.String()
}
