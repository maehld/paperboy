// Package paperboy serves ePaper devices running the Paperboy firmware over the
// device API (ADR 0041, 0042). Pages are
// rendered from templates and chosen by plans (ADR 0024, 0025); the
// configuration is YAML (ADR 0026) in a fixed instance directory (ADR 0037).
package paperboy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/maehld/paperboy/internal/builtin"
	"github.com/maehld/paperboy/internal/widget"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // embed IANA zones so the configured timezone needs no system tzdata

	"go.yaml.in/yaml/v3"

	"github.com/maehld/paperboy/internal/page"
)

// Duration accepts Go duration strings such as "5m" in YAML.
type Duration time.Duration

func (d *Duration) UnmarshalYAML(v *yaml.Node) error {
	var s string
	if err := v.Decode(&s); err != nil {
		return fmt.Errorf("expected duration as string, e.g. \"5m\"")
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }

// MarshalJSON keeps durations readable in API responses such as /api/profiles.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

// Profile describes one display type (ADR 0026); the key is also the file
// suffix of its page templates (pages/<page>.<display type>.yaml). A text
// display type (ADR 0044) uses Width and Height for columns and rows.
type Profile struct {
	Kind       string   `json:"kind"`              // "image" or "text"
	Charset    string   `json:"charset,omitempty"` // text displays: utf8, ascii or hd44780
	Width      int      `json:"width"`
	Height     int      `json:"height"`
	Gray       bool     `json:"gray"`                  // four gray levels instead of black/white
	PPI        float64  `json:"ppi,omitempty"`         // image displays: pixels per inch (ADR 0050)
	MinRefresh Duration `json:"min_refresh,omitempty"` // configured floor; 0 = default of the power source (ADR 0051)
	Power      string   `json:"power"`                 // auto, battery or mains (ADR 0051)
}

// Power sources (ADR 0051). auto follows what the hardware reports and falls
// back to battery when it reports nothing.
const (
	powerAuto    = "auto"
	powerBattery = "battery"
	powerMains   = "mains"
)

// mainsImageMinRefresh is the default floor of a mains-powered image display:
// an ePaper redraw takes seconds, shorter intervals would only queue up.
const mainsImageMinRefresh = 10 * time.Second

func validPower(p string) bool { return p == powerAuto || p == powerBattery || p == powerMains }

const (
	kindImage = "image"
	kindText  = "text"
)

// text reports whether the display type shows lines of characters.
func (p Profile) text() bool { return p.Kind == kindText }

// Device is one physical display. Its ID is stable and used in state/devices.json, logs
// and the devices API; the device API registers it by its Wi-Fi MAC address.
type Device struct {
	ID      string
	Type    string // protocol; only "paperboy" for now
	MAC     string
	Address string // reserved for OpenDisplay, which is not supported yet
	Buttons bool
	Profile string // display type
	Refresh Duration
	Battery *Battery // overrides the defaults
	Plan    string   // page plan under plans/; empty = notice page
	Hold    bool     // firmware: hold keeps the device on its firmware version (ADR 0043)
	Power   string   // auto, battery or mains; empty = the display type's (ADR 0051)
}

// FirmwareConfig controls firmware updates offered in the sync (ADR 0043) and
// which images Paperboy downloads (ADR 0060).
type FirmwareConfig struct {
	Update          string  // "auto" or "off"
	MinBatteryVolts float64 // no offer below this voltage
	Version         string  // pinned image version ("v1.0.0" or "1.0.0"); empty = newest
}

const (
	firmwareAuto         = "auto"
	firmwareOff          = "off"
	firmwareHold         = "hold"
	defaultFirmwareVolts = 3.6
	firmwareFailureLimit = 3 // failed attempts per version before the offer stops
	firmwareDirName      = "firmware"
)

// Battery configures the low-battery warning (ADR 0020). A threshold
// of 0 disables that stage.
type Battery struct {
	WarnVolts       float64 `yaml:"warn_volts" json:"warn_volts"`             // banner on every page below this
	CriticalVolts   float64 `yaml:"critical_volts" json:"critical_volts"`     // full-screen warning below this
	HysteresisVolts float64 `yaml:"hysteresis_volts" json:"hysteresis_volts"` // a stage ends this far above its threshold; 0 = 0.1
}

func (b *Battery) validate(owner string) error {
	if b == nil {
		return nil
	}
	if b.WarnVolts < 0 || b.CriticalVolts < 0 || b.HysteresisVolts < 0 || b.WarnVolts > 5 || b.CriticalVolts > 5 {
		return fmt.Errorf("%s: invalid battery thresholds", owner)
	}
	if b.WarnVolts > 0 && b.CriticalVolts > 0 && b.CriticalVolts >= b.WarnVolts {
		return fmt.Errorf("%s: critical_volts must be below warn_volts", owner)
	}
	if b.HysteresisVolts == 0 {
		b.HysteresisVolts = 0.1
	}
	return nil
}

// mergeBattery lets a device override single default thresholds; zero values
// keep the default.
func mergeBattery(base, override *Battery) *Battery {
	if override == nil || base == nil {
		if override != nil {
			return override
		}
		return base
	}
	merged := *base
	if override.WarnVolts > 0 {
		merged.WarnVolts = override.WarnVolts
	}
	if override.CriticalVolts > 0 {
		merged.CriticalVolts = override.CriticalVolts
	}
	if override.HysteresisVolts > 0 {
		merged.HysteresisVolts = override.HysteresisVolts
	}
	return &merged
}

const (
	TypePaperboy = "paperboy"

	minRefresh         = time.Minute
	defaultRefresh     = 5 * time.Minute
	defaultPageTimeout = 5 * time.Minute
)

// Fixed names of the instance directory (ADR 0037).
const (
	configFileName  = "paperboy.yaml"
	pagesDirName    = "pages"
	plansDirName    = "plans"
	pluginsDirName  = "plugins"
	stateDirName    = "state"
	devicesFileName = "devices.json"
	dataFileName    = "data.json"
)

// Config is the validated runtime configuration.
type Config struct {
	Dir         string         // instance directory from -config; holds pages, plans, plugins and state (ADR 0037, 0049)
	Language    string         // "de" or "en"; built-in texts and number formats
	Timezone    *time.Location // plans and built-in dates
	Refresh     Duration       // default interval of a device; a plan rule may override it
	PageTimeout Duration
	Pages       string // page templates: <dir>/pages
	Plans       string // page plans: <dir>/plans
	PluginsDir  string // plugin programs: <dir>/plugins (ADR 0049)
	FirmwareDir string // firmware images: <dir>/firmware (ADR 0043)
	Firmware    FirmwareConfig
	Profiles    map[string]Profile
	Devices     []Device
	Plugins     []Plugin // plugins (ADR 0049)
	Battery     *Battery // default low-battery warning; nil = off

	// degraded marks a start without a usable paperboy.yaml (ADR 0047). Such a
	// configuration is retried from disk even when the file did not change.
	degraded bool
}

// Plugin is one plugin (ADR 0049): a long-running program in the plugins
// directory that provides widgets, named after the plugin and — when it was
// downloaded — carrying its version in the file name (ADR 0060). Config is its
// section of paperboy.yaml, with ${VAR} already replaced from the environment.
type Plugin struct {
	Name    string
	Version string // pinned version, e.g. v2.1.0; empty = the highest available
	Timeout Duration
	Env     map[string]string
	Config  map[string]any
}

// texts returns the built-in image texts for the configured language.
func (c *Config) texts() texts { return texts{language: c.Language} }

// battery returns the effective battery warning settings for d, or nil.
func (c *Config) battery(d *Device) *Battery {
	if d.Battery != nil {
		return d.Battery
	}
	return c.Battery
}

// power returns the effective power source of d (ADR 0051): the device's
// setting, else the display type's; auto takes what the hardware reported
// and falls back to battery when it reported nothing.
func (c *Config) power(d *Device, reported string) string {
	p := d.Power
	if p == "" {
		p = c.Profiles[d.Profile].Power
	}
	if p == powerBattery || p == powerMains {
		return p
	}
	if reported == powerMains {
		return powerMains
	}
	return powerBattery
}

// minRefresh returns the shortest interval reported to d (ADR 0045, 0051).
// Battery devices never go below one minute or their min_refresh; mains
// devices go down to min_refresh, by default one second for text and ten
// seconds for image displays.
func (c *Config) minRefresh(d *Device, reported string) time.Duration {
	p := c.Profiles[d.Profile]
	set := time.Duration(p.MinRefresh)
	if c.power(d, reported) == powerBattery {
		return max(minRefresh, set)
	}
	switch {
	case set > 0:
		return set
	case p.text():
		return time.Second
	default:
		return mainsImageMinRefresh
	}
}

// fileConfig mirrors paperboy.yaml.
type fileConfig struct {
	Language string `yaml:"language"`  // de or en; default: system language
	Timezone string `yaml:"timezone"`  // IANA name; default: system timezone
	Server   any    `yaml:"server"`    // removed in v2; rejected with a hint
	Old      any    `yaml:"providers"` // removed in v2; rejected with a hint
	Defaults struct {
		Refresh     Duration `yaml:"refresh"`
		PageTimeout Duration `yaml:"page_timeout"`
		Battery     *Battery `yaml:"battery"`
	} `yaml:"defaults"`
	Firmware struct {
		Update          string  `yaml:"update"`
		MinBatteryVolts float64 `yaml:"min_battery_volts"`
		Version         string  `yaml:"version"` // pin for downloads and offers (ADR 0060)
	} `yaml:"firmware"`
	Plugins map[string]struct {
		Command any               `yaml:"command"` // removed by ADR 0060; rejected with a hint
		Version string            `yaml:"version"` // downloaded/pinned program version
		Timeout Duration          `yaml:"timeout"`
		Env     map[string]string `yaml:"env"`
		Config  map[string]any    `yaml:"config"`
	} `yaml:"plugins"`
	DisplayTypes map[string]struct {
		Kind       string   `yaml:"kind"`
		Charset    string   `yaml:"charset"`
		Size       [2]int   `yaml:"size"`
		GrayLevels int      `yaml:"gray_levels"`
		PPI        float64  `yaml:"ppi"`
		WidthMM    *int     `yaml:"width_mm"` // removed by ADR 0050, rejected with a hint
		MinRefresh Duration `yaml:"min_refresh"`
		Power      string   `yaml:"power"`
	} `yaml:"display_types"`
	Devices map[string]struct {
		DisplayType string   `yaml:"display_type"`
		Protocol    string   `yaml:"protocol"`
		MAC         string   `yaml:"mac"`
		Address     string   `yaml:"address"`
		Buttons     bool     `yaml:"buttons"`
		Plan        string   `yaml:"plan"`
		Refresh     Duration `yaml:"refresh"`
		Battery     *Battery `yaml:"battery"`
		Firmware    string   `yaml:"firmware"`
		Power       string   `yaml:"power"`
	} `yaml:"devices"`
}

// LoadConfig reads and validates the configuration of an instance directory
// (ADR 0037): <dir>/paperboy.yaml, with pages, plans, plugins and state as
// fixed subdirectories. Secrets stay out of the file: plugin config values
// may reference the environment as ${VAR} (ADR 0049).
func LoadConfig(dir string) (*Config, error) {
	fi, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("%s: -config expects the instance directory that contains %s, not a file", dir, configFileName)
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	path := filepath.Join(dir, configFileName)
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		if _, err := os.Stat(filepath.Join(dir, "paperboy.json")); err == nil {
			return nil, fmt.Errorf("%s: JSON configuration is no longer supported; convert it to %s (see paperboy.example.yaml)", filepath.Join(dir, "paperboy.json"), configFileName)
		}
		return nil, fmt.Errorf("%s: no %s in the instance directory", dir, configFileName)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f fileConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s: empty configuration", path)
		}
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: only one YAML document is allowed", path)
	}

	tz := time.Local
	if f.Timezone != "" {
		loc, err := time.LoadLocation(f.Timezone)
		if err != nil {
			return nil, fmt.Errorf("%s: timezone %q: %w", path, f.Timezone, err)
		}
		tz = loc
	}
	c := &Config{
		Dir:         dir,
		Language:    f.Language,
		Timezone:    tz,
		Refresh:     f.Defaults.Refresh,
		PageTimeout: f.Defaults.PageTimeout,
		Battery:     f.Defaults.Battery,
		Pages:       filepath.Join(dir, pagesDirName),
		Plans:       filepath.Join(dir, plansDirName),
		PluginsDir:  filepath.Join(dir, pluginsDirName),
		FirmwareDir: filepath.Join(dir, firmwareDirName),
		Firmware:    FirmwareConfig{Update: f.Firmware.Update, MinBatteryVolts: f.Firmware.MinBatteryVolts, Version: f.Firmware.Version},
		Profiles:    make(map[string]Profile, len(f.DisplayTypes)),
	}
	if f.Server != nil {
		return nil, fmt.Errorf("%s: the server section was removed in Paperboy v2: there is no data API and no token any more (ADR 0049); remove it", path)
	}
	if f.Old != nil {
		return nil, fmt.Errorf("%s: providers were replaced by plugins in Paperboy v2 (ADR 0049); move them to plugins with a config section", path)
	}
	for name, t := range f.DisplayTypes {
		c.Profiles[name] = Profile{
			Kind:       t.Kind,
			Charset:    t.Charset,
			Width:      t.Size[0],
			Height:     t.Size[1],
			Gray:       t.GrayLevels == 4,
			PPI:        t.PPI,
			MinRefresh: t.MinRefresh,
			Power:      t.Power,
		}
		if t.WidthMM != nil {
			return nil, fmt.Errorf("%s: display type %s: width_mm was replaced by ppi (ADR 0050): replace it with ppi = pixels × 25.4 / mm of the same side", path, name)
		}
		if t.Kind == kindText && (t.GrayLevels != 0 || t.PPI != 0) {
			return nil, fmt.Errorf("%s: display type %s: gray_levels and ppi do not apply to kind text", path, name)
		}
		if t.GrayLevels != 0 && t.GrayLevels != 1 && t.GrayLevels != 4 {
			return nil, fmt.Errorf("%s: display type %s: gray_levels must be 1 or 4", path, name)
		}
	}
	ids := make([]string, 0, len(f.Devices))
	for id := range f.Devices {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		d := f.Devices[id]
		switch d.Firmware {
		case "", firmwareHold:
		default:
			return nil, fmt.Errorf("%s: device %s: firmware %q: only hold is allowed", path, id, d.Firmware)
		}
		c.Devices = append(c.Devices, Device{
			ID:      id,
			Type:    d.Protocol,
			MAC:     d.MAC,
			Address: d.Address,
			Buttons: d.Buttons,
			Profile: d.DisplayType,
			Refresh: d.Refresh,
			Battery: mergeBattery(f.Defaults.Battery, d.Battery),
			Plan:    d.Plan,
			Hold:    d.Firmware == firmwareHold,
			Power:   d.Power,
		})
	}
	pluginNames := make([]string, 0, len(f.Plugins))
	for name := range f.Plugins {
		pluginNames = append(pluginNames, name)
	}
	sort.Strings(pluginNames)
	for _, name := range pluginNames {
		p := f.Plugins[name]
		if p.Command != nil {
			return nil, fmt.Errorf("%s: plugin %s: command is no longer supported (ADR 0060); name the program %s-v<version> in the plugins directory", path, name, name)
		}
		cfg, err := expandConfig(p.Config)
		if err != nil {
			return nil, fmt.Errorf("%s: plugin %s: config: %w", path, name, err)
		}
		c.Plugins = append(c.Plugins, Plugin{Name: name, Version: p.Version, Timeout: p.Timeout, Env: p.Env, Config: cfg})
	}
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

func (c *Config) validate() error {
	if c.Timezone == nil {
		c.Timezone = time.Local
	}
	if c.Language == "" {
		c.Language = detectLanguage()
	}
	if c.Language == "" {
		c.Language = "en"
	}
	if c.Language != "de" && c.Language != "en" {
		return fmt.Errorf("language %q: expected de or en", c.Language)
	}
	if c.Refresh == 0 {
		c.Refresh = Duration(defaultRefresh)
	}
	if time.Duration(c.Refresh) < minRefresh {
		return fmt.Errorf("defaults: refresh must be at least 1m")
	}
	if c.PageTimeout == 0 {
		c.PageTimeout = Duration(defaultPageTimeout)
	}
	if err := c.Battery.validate("battery"); err != nil {
		return err
	}
	switch c.Firmware.Update {
	case "":
		c.Firmware.Update = firmwareOff
	case firmwareOff, firmwareAuto:
	default:
		return fmt.Errorf("firmware: update %q: expected auto or off", c.Firmware.Update)
	}
	if c.Firmware.MinBatteryVolts == 0 {
		c.Firmware.MinBatteryVolts = defaultFirmwareVolts
	}
	if c.Firmware.MinBatteryVolts < 0 || c.Firmware.MinBatteryVolts > 5 {
		return fmt.Errorf("firmware: invalid min_battery_volts")
	}
	if v := strings.TrimPrefix(strings.TrimSpace(c.Firmware.Version), "v"); v != "" {
		if pinVersion(v) == nil {
			return fmt.Errorf("firmware: version %q: expected a version like 1.0.0", c.Firmware.Version)
		}
		c.Firmware.Version = v
	} else {
		c.Firmware.Version = ""
	}
	if len(c.Profiles) == 0 {
		return fmt.Errorf("display_types missing")
	}
	for name, p := range c.Profiles {
		if !validName(name) {
			return fmt.Errorf("display type %q: only a-z, 0-9 and -", name)
		}
		switch p.Kind {
		case "":
			p.Kind = kindImage
		case kindImage, kindText:
		default:
			return fmt.Errorf("display type %s: unknown kind %q: expected image or text", name, p.Kind)
		}
		if p.text() {
			if p.Width < 1 || p.Height < 1 || p.Width > 80 || p.Height > 24 {
				return fmt.Errorf("display type %s: size [columns, rows] must be at most [80, 24]", name)
			}
			if !page.ValidCharset(p.Charset) {
				return fmt.Errorf("display type %s: charset %q: expected utf8, ascii or hd44780", name, p.Charset)
			}
			if p.Charset == "" {
				p.Charset = page.CharsetUTF8
			}
		} else if p.Charset != "" {
			return fmt.Errorf("display type %s: charset only applies to kind text", name)
		} else if p.PPI < 20 || p.PPI > 1000 {
			return fmt.Errorf("display type %s: ppi is required for image displays: pixels per inch between 20 and 1000, e.g. 125 (ADR 0050)", name)
		}
		if p.Width < 1 || p.Height < 1 || p.Width > 4096 || p.Height > 4096 {
			return fmt.Errorf("display type %s: invalid size", name)
		}
		// An unset min_refresh stays 0: its default depends on the power
		// source, which may only be known from the device's report (ADR 0051).
		if p.MinRefresh != 0 && time.Duration(p.MinRefresh) < time.Second {
			return fmt.Errorf("display type %s: min_refresh must be at least 1s", name)
		}
		if p.Power == "" {
			p.Power = powerAuto
		}
		if !validPower(p.Power) {
			return fmt.Errorf("display type %s: power %q: expected auto, battery or mains", name, p.Power)
		}
		c.Profiles[name] = p
	}
	seenID := map[string]bool{}
	seenMAC := map[string]string{}
	for i := range c.Devices {
		d := &c.Devices[i]
		if !validName(d.ID) {
			return fmt.Errorf("device %q: id must use a-z, 0-9 and -", d.ID)
		}
		switch d.Type {
		case "", TypePaperboy:
			d.Type = TypePaperboy
			if d.Address != "" || d.Buttons {
				return fmt.Errorf("device %s: address/buttons belong to protocol opendisplay, which is not supported yet", d.ID)
			}
			mac, err := net.ParseMAC(d.MAC)
			if err != nil {
				return fmt.Errorf("device %s: invalid MAC address", d.ID)
			}
			d.MAC = normalizeMAC(mac.String())
		case "trmnl":
			return fmt.Errorf("device %s: protocol trmnl was removed (ADR 0042); flash the Paperboy firmware and set protocol: paperboy", d.ID)
		case "opendisplay":
			return fmt.Errorf("device %s: protocol opendisplay is not supported yet (TODO in README)", d.ID)
		case "text-http", "image-http":
			return fmt.Errorf("device %s: protocol %q was replaced by the device API (ADR 0044); set protocol: paperboy", d.ID, d.Type)
		default:
			return fmt.Errorf("device %s: unknown protocol %q", d.ID, d.Type)
		}
		if err := d.Battery.validate("device " + d.ID); err != nil {
			return err
		}
		if seenID[d.key()] {
			return fmt.Errorf("duplicate device %s", d.key())
		}
		seenID[d.key()] = true
		if other, ok := seenMAC[d.MAC]; ok {
			return fmt.Errorf("duplicate device %s: MAC %s is already used by %s", d.ID, d.MAC, other)
		}
		seenMAC[d.MAC] = d.ID
		if _, ok := c.Profiles[d.Profile]; !ok {
			return fmt.Errorf("device %s: unknown display_type %q", d.ID, d.Profile)
		}
		if d.Refresh == 0 {
			d.Refresh = c.Refresh
		}
		// The floor of the power source applies at runtime (ADR 0051).
		if time.Duration(d.Refresh) < time.Second {
			return fmt.Errorf("device %s: refresh must be at least 1s", d.ID)
		}
		if d.Power != "" && !validPower(d.Power) {
			return fmt.Errorf("device %s: power %q: expected auto, battery or mains", d.ID, d.Power)
		}
		if d.Plan != "" && !validName(d.Plan) {
			return fmt.Errorf("device %s: plan %q: only a-z, 0-9 and -", d.ID, d.Plan)
		}
	}
	for i := range c.Plugins {
		p := &c.Plugins[i]
		if !validName(p.Name) {
			return fmt.Errorf("plugin %q: name must use a-z, 0-9 and -", p.Name)
		}
		if p.Name == builtin.Name {
			return fmt.Errorf("plugin %q: the name is reserved for the built-in plugin", p.Name)
		}
		if p.Version != "" {
			v, ok := pluginPin(p.Version)
			if !ok {
				return fmt.Errorf("plugin %s: version %q: expected a version like v2.1.0", p.Name, p.Version)
			}
			p.Version = v
		}
		if p.Timeout < 0 {
			return fmt.Errorf("plugin %s: timeout must not be negative", p.Name)
		}
	}
	return nil
}

// pluginPin normalizes a pinned plugin version to the form in the file name
// (ADR 0060): "2.1.0" and "v2.1.0" both become "v2.1.0".
func pluginPin(pin string) (string, bool) {
	v := strings.TrimPrefix(strings.TrimSpace(pin), "v")
	if !firmwareVersionPattern.MatchString(v) {
		return "", false
	}
	return "v" + v, true
}

// pluginSpecs returns the plugins for the widget host, with the global
// language and timezone in their environment and the program resolved from the
// plugins directory (ADR 0060).
func (c *Config) pluginSpecs() map[string]widget.Spec {
	out := make(map[string]widget.Spec, len(c.Plugins))
	for _, p := range c.Plugins {
		env := map[string]string{"PAPERBOY_LANGUAGE": c.Language, "PAPERBOY_TIMEZONE": c.timezoneName()}
		for k, v := range p.Env {
			env[k] = v
		}
		program, version := pluginProgram(c.PluginsDir, p.Name, p.Version)
		command := []string{program}
		if program == "" {
			// Left unresolved so that the host reports the missing program in
			// the cell; a download may deliver it later.
			command = []string{p.Name}
		}
		out[p.Name] = widget.Spec{Name: p.Name, Command: command, Version: version,
			Timeout: time.Duration(p.Timeout), Env: env, Dir: c.PluginsDir, Config: p.Config}
	}
	return out
}

func (c *Config) timezoneName() string {
	if c.Timezone == nil {
		return "UTC"
	}
	return c.Timezone.String()
}

// profileNames lists the configured display types, sorted.
func (c *Config) profileNames() []string {
	names := make([]string, 0, len(c.Profiles))
	for name := range c.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// expandConfig replaces ${VAR} in the string values of a plugin's config with
// the environment, so secrets stay out of paperboy.yaml. An unset variable is
// an error; $$ writes a literal dollar sign.
func expandConfig(cfg map[string]any) (map[string]any, error) {
	out, err := expandValue(cfg)
	if err != nil || out == nil {
		return map[string]any{}, err
	}
	return out.(map[string]any), nil
}

func expandValue(v any) (any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case string:
		var missing []string
		s := os.Expand(strings.ReplaceAll(x, "$$", "\x00"), func(name string) string {
			val, ok := os.LookupEnv(name)
			if !ok || val == "" {
				missing = append(missing, name)
			}
			return val
		})
		if len(missing) > 0 {
			return nil, fmt.Errorf("environment variable %s is not set", strings.Join(missing, ", "))
		}
		return strings.ReplaceAll(s, "\x00", "$"), nil
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			r, err := expandValue(e)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			out[k] = r
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			r, err := expandValue(e)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	}
	return v, nil
}

// device returns the configured device with this MAC address.
func (c *Config) device(mac string) *Device {
	mac = normalizeMAC(mac)
	if mac == "" {
		return nil
	}
	for i := range c.Devices {
		if c.Devices[i].MAC == mac {
			return &c.Devices[i]
		}
	}
	return nil
}

// key identifies the device in the persisted state.
func (d *Device) key() string { return d.ID }

func normalizeMAC(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

func validName(s string) bool {
	if s == "" || len(s) > 40 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// refresh returns the regular sleep time of the device. Plans may override it.
func (d *Device) refresh() time.Duration { return time.Duration(d.Refresh) }
