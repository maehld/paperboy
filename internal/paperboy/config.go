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
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // embed IANA zones so the configured timezone needs no system tzdata

	"go.yaml.in/yaml/v3"

	"github.com/maehld/paperboy/internal/builtin"
	"github.com/maehld/paperboy/internal/download"
	"github.com/maehld/paperboy/internal/page"
	"github.com/maehld/paperboy/internal/semver"
	"github.com/maehld/paperboy/internal/widget"
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

// UnmarshalJSON reads the string form its YAML counterpart uses, so a duration
// survives the round trip through the editor API (ADR 0066, 0070).
func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("expected duration as string, e.g. \"5m\"")
	}
	if s == "" {
		*d = 0
		return nil
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

// MarshalJSON keeps durations readable in API responses such as /api/profiles.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(durationText(time.Duration(d))) }

// durationText writes a duration the way operators do: "5m" instead of "5m0s",
// but "1m30s" when the seconds matter.
func durationText(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	return s
}

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

// Device is one physical display. Its ID is stable and used in logs, the
// devices API and state file names; the device API registers it by its Wi-Fi
// MAC address, which also names its state file (ADR 0080).
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
	// Actions allows the device menu (ADR 0057): "allow" (default) or "deny".
	Actions string
	// MenuShowDuration is how long the menu stays open before it closes
	// itself; 0 = the default (ADR 0057).
	MenuShowDuration Duration
	// Touch turns the menu's tap-only controls on: the strip under the action
	// cells with the page arrows and the close box, drawn only for devices
	// whose digitiser the operator wants to use (ADR 0082). Empty = use what
	// the firmware reported, which defaults to false.
	Touch *bool
	// Problem names why the device cannot sync; the loader keeps such a device
	// so that renaming a display type leaves its file visible and repairable
	// (ADR 0079). Empty = usable.
	Problem string
}

// FirmwareConfig controls firmware updates offered in the sync (ADR 0043) and
// which images Paperboy downloads (ADR 0060).
type FirmwareConfig struct {
	Update          string  // "auto" or "off"
	MinBatteryVolts float64 // no offer below this voltage
	Version         string  // pinned image version ("v1.0.0" or "1.0.0"); empty = newest
}

// DownloadConfig enables fetching plugins and firmware from releases (ADR 0060).
// Its presence in paperboy.yaml is the opt-in.
type DownloadConfig struct {
	Source   string        // release origin; empty = the official repository
	Token    string        // optional bearer token for the release API
	Interval time.Duration // between release checks; default 24h, at least 1h

	// PluginChannel and FirmwareChannel say which deliveries a check may
	// install (ADR 0089): stable skips prereleases, latest takes them.
	PluginChannel   string
	FirmwareChannel string
}

// The two release channels (ADR 0089).
const (
	channelStable = "stable"
	channelLatest = "latest"
)

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

	// The device menu (ADR 0057) stays open this long by default and never
	// longer than menuMaxDuration, so a forgotten menu does not keep waking the
	// device.
	defaultMenuDuration = 30 * time.Second
	menuMaxDuration     = 60 * time.Second

	actionsAllow = "allow"
	actionsDeny  = "deny"
)

// Fixed names of the instance directory (ADR 0037).
const (
	configFileName  = "paperboy.yaml"
	pagesDirName    = "pages"
	plansDirName    = "plans"
	displaysDirName = "displays"
	devicesDirName  = "devices"
	pluginsDirName  = "plugins"
	stateDirName    = "state"
	// devicesStateDirName holds one state file per MAC (ADR 0080); the legacy
	// devices.json is no longer read.
	devicesStateDirName   = "devices"
	legacyDevicesFileName = "devices.json"
	dataFileName          = "data.json"
)

// Config is the validated runtime configuration.
type Config struct {
	Dir         string         // instance directory from -config; holds pages, plans, plugins and state (ADR 0037, 0049)
	Language    string         // "de" or "en"; built-in texts and number formats
	Timezone    *time.Location // plans and built-in dates
	Refresh     Duration       // default interval of a device; a plan rule may override it
	PageTimeout Duration
	MenuShow    Duration // default time the device menu stays open (ADR 0057)
	Pages       string   // page templates: <dir>/pages
	Plans       string   // page plans: <dir>/plans
	DisplaysDir string   // display type files: <dir>/displays (ADR 0071)
	DevicesDir  string   // device files: <dir>/devices (ADR 0070)
	PluginsDir  string   // plugin programs: <dir>/plugins (ADR 0049)
	FirmwareDir string   // firmware images: <dir>/firmware (ADR 0043)
	Firmware    FirmwareConfig
	Download    *DownloadConfig // nil = fetch nothing (ADR 0060)
	Profiles    map[string]Profile
	Devices     []Device
	Plugins     []Plugin // plugins (ADR 0049)
	Battery     *Battery // default low-battery warning; nil = off
	UI          UIConfig // operator UI password (ADR 0096)

	// degraded marks a start without a usable paperboy.yaml (ADR 0047). Such a
	// configuration is retried from disk even when the file did not change.
	degraded bool
}

// UIConfig is the operator UI section (ADR 0096). The password guards every
// route of the UI and its API; an empty password keeps the UI open like a v2
// instance that never set one.
type UIConfig struct {
	Password string // ui.password, ${VAR} already expanded from the environment
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

// touch reports whether the device menu draws the tap-only control strip
// (ADR 0082): the device file wins, otherwise what the firmware reported, and
// without both the menu stays button-only.
func (c *Config) touch(d *Device, reported bool) bool {
	if d.Touch != nil {
		return *d.Touch
	}
	return reported
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

// downloadFile mirrors the download section of paperboy.yaml (ADR 0060).
type downloadFile struct {
	Source          string   `yaml:"source"`           // empty = the official repository
	Token           string   `yaml:"token"`            // optional bearer token for the release API
	Interval        Duration `yaml:"interval"`         // between release checks
	PluginChannel   string   `yaml:"plugin_channel"`   // stable or latest (ADR 0089)
	FirmwareChannel string   `yaml:"firmware_channel"` // stable or latest (ADR 0089)
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
		MenuShow    Duration `yaml:"menu_show_duration"`
		Battery     *Battery `yaml:"battery"`
	} `yaml:"defaults"`
	Firmware struct {
		Update          string  `yaml:"update"`
		MinBatteryVolts float64 `yaml:"min_battery_volts"`
		Version         string  `yaml:"version"` // pin for downloads and offers (ADR 0060)
	} `yaml:"firmware"`
	Download *downloadFile `yaml:"download"`
	UI       struct {
		Password string `yaml:"password"` // operator UI password (ADR 0096)
	} `yaml:"ui"`
	Plugins map[string]struct {
		Command any               `yaml:"command"` // removed by ADR 0060; rejected with a hint
		Version string            `yaml:"version"` // downloaded/pinned program version
		Timeout Duration          `yaml:"timeout"`
		Env     map[string]string `yaml:"env"`
		Config  map[string]any    `yaml:"config"`
	} `yaml:"plugins"`
	DisplayTypes any `yaml:"display_types"` // moved to displays/ (ADR 0071); rejected with a hint
	Devices      any `yaml:"devices"`       // removed by ADR 0070; rejected with a hint
}

// hasSection reports whether the document names a top-level key, even without a
// value. YAML reads "download:" as null, which a pointer field cannot tell from
// a missing key, but for a section whose presence is the opt-in that difference
// matters (ADR 0060).
func hasSection(data []byte, key string) bool {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return false
	}
	m := doc.Content[0]
	if m.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return true
		}
	}
	return false
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
		MenuShow:    f.Defaults.MenuShow,
		Battery:     f.Defaults.Battery,
		Pages:       filepath.Join(dir, pagesDirName),
		Plans:       filepath.Join(dir, plansDirName),
		DisplaysDir: filepath.Join(dir, displaysDirName),
		DevicesDir:  filepath.Join(dir, devicesDirName),
		PluginsDir:  filepath.Join(dir, pluginsDirName),
		FirmwareDir: filepath.Join(dir, firmwareDirName),
		Firmware:    FirmwareConfig{Update: f.Firmware.Update, MinBatteryVolts: f.Firmware.MinBatteryVolts, Version: f.Firmware.Version},
	}
	if f.Server != nil {
		return nil, fmt.Errorf("%s: the server section was removed in Paperboy v2: there is no data API and no token any more (ADR 0049); remove it", path)
	}
	if f.Old != nil {
		return nil, fmt.Errorf("%s: providers were replaced by plugins in Paperboy v2 (ADR 0049); move them to plugins with a config section", path)
	}
	if f.DisplayTypes != nil {
		return nil, fmt.Errorf("%s: display types moved to files of their own: create displays/<name>.yaml for every display type and remove the display_types section (ADR 0071)", path)
	}
	c.Profiles, err = loadDisplays(c.DisplaysDir)
	if err != nil {
		return nil, err
	}
	if f.Devices != nil {
		return nil, fmt.Errorf("%s: devices moved to files of their own: create devices/<id>.yaml for every device and remove the devices section (ADR 0070)", path)
	}
	devices, err := loadDevices(filepath.Join(dir, devicesDirName), f.Defaults.Battery)
	if err != nil {
		return nil, err
	}
	c.Devices = devices
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
	// "download:" without a value decodes as null, which the pointer reads as a
	// missing section, but the section's presence is the opt-in (ADR 0060): a
	// bare key enables the checks with every default.
	if f.Download == nil && hasSection(data, "download") {
		f.Download = &downloadFile{}
	}
	if f.Download != nil {
		token := ""
		if f.Download.Token != "" {
			v, err := expandValue(f.Download.Token)
			if err != nil {
				return nil, fmt.Errorf("%s: download: token: %w", path, err)
			}
			token, _ = v.(string)
		}
		c.Download = &DownloadConfig{Source: f.Download.Source, Token: token,
			Interval:        time.Duration(f.Download.Interval),
			PluginChannel:   f.Download.PluginChannel,
			FirmwareChannel: f.Download.FirmwareChannel}
	}
	// The UI password is a secret like a plugin's, so ${VAR} works here too
	// (ADR 0031, 0096).
	if f.UI.Password != "" {
		v, err := expandValue(f.UI.Password)
		if err != nil {
			return nil, fmt.Errorf("%s: ui: password: %w", path, err)
		}
		c.UI.Password, _ = v.(string)
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
		if _, ok := semver.Parse(v); !ok {
			return fmt.Errorf("firmware: version %q: expected a version like 1.0.0 or 1.0.0-rc.1", c.Firmware.Version)
		}
		c.Firmware.Version = v
	} else {
		c.Firmware.Version = ""
	}
	if d := c.Download; d != nil {
		if d.Source == "" {
			d.Source = download.OfficialSource
		}
		var err error
		if d.PluginChannel, err = parseChannel(d.PluginChannel); err != nil {
			return fmt.Errorf("download: plugin_channel %q: %w", strings.TrimSpace(d.PluginChannel), err)
		}
		if d.FirmwareChannel, err = parseChannel(d.FirmwareChannel); err != nil {
			return fmt.Errorf("download: firmware_channel %q: %w", strings.TrimSpace(d.FirmwareChannel), err)
		}
		if d.Interval == 0 {
			d.Interval = defaultDownloadInterval
		}
		if d.Interval < minDownloadInterval {
			return fmt.Errorf("download: interval %s: expected at least %s", d.Interval, minDownloadInterval)
		}
		u, err := url.Parse(d.Source)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("download: source %q: expected an http(s) address", d.Source)
		}
	}
	if len(c.Profiles) == 0 {
		return fmt.Errorf("no display type: create displays/<name>.yaml (ADR 0071)")
	}
	for name, p := range c.Profiles {
		if err := validateProfile(name, &p); err != nil {
			return err
		}
		c.Profiles[name] = p
	}
	seenID := map[string]bool{}
	seenMAC := map[string]string{}
	for i := range c.Devices {
		d := &c.Devices[i]
		if err := c.normalizeDevice(d); err != nil {
			return err
		}
		if _, ok := c.Profiles[d.Profile]; !ok {
			// A device whose display type is gone stays loaded: renaming the
			// type must not take the whole server down, and the operator can
			// repair the file in the editor (ADR 0079).
			d.Problem = fmt.Sprintf("display type %q is not configured", d.Profile)
		}
		if seenID[d.key()] {
			return fmt.Errorf("duplicate device %s", d.key())
		}
		seenID[d.key()] = true
		if other, ok := seenMAC[d.MAC]; ok {
			return fmt.Errorf("duplicate device %s: MAC %s is already used by %s", d.ID, d.MAC, other)
		}
		seenMAC[d.MAC] = d.ID
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
				return fmt.Errorf("plugin %s: version %q: expected a version like v2.1.0 or v2.1.0-rc.1", p.Name, p.Version)
			}
			p.Version = v
		}
		if p.Timeout < 0 {
			return fmt.Errorf("plugin %s: timeout must not be negative", p.Name)
		}
	}
	return nil
}

// validateProfile checks one display type and fills its defaults; the loader
// and the display type API both use it, so a written file is as valid as a
// loaded one (ADR 0071).
func validateProfile(name string, p *Profile) error {
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
		if p.PPI != 0 || p.Gray {
			return fmt.Errorf("display type %s: gray_levels and ppi do not apply to kind text", name)
		}
	} else if p.Charset != "" {
		return fmt.Errorf("display type %s: charset only applies to kind text", name)
	} else if p.PPI < 20 || p.PPI > 1000 {
		return fmt.Errorf("display type %s: ppi is required for image displays: pixels per inch between 20 and 1000, e.g. 125 (ADR 0050)", name)
	}
	if p.Width < 1 || p.Height < 1 || p.Width > 4096 || p.Height > 4096 {
		return fmt.Errorf("display type %s: invalid size", name)
	}
	// An unset min_refresh stays 0: its default depends on the power source,
	// which may only be known from the device's report (ADR 0051).
	if p.MinRefresh != 0 && time.Duration(p.MinRefresh) < time.Second {
		return fmt.Errorf("display type %s: min_refresh must be at least 1s", name)
	}
	if p.Power == "" {
		p.Power = powerAuto
	}
	if !validPower(p.Power) {
		return fmt.Errorf("display type %s: power %q: expected auto, battery or mains", name, p.Power)
	}
	return nil
}

// validateDevice checks one device against the display types and fills its
// defaults; the ID is its file name (ADR 0070). The API uses it too, so a
// device the editor writes is as valid as one the server loaded.
func (c *Config) validateDevice(d *Device) error {
	if err := c.normalizeDevice(d); err != nil {
		return err
	}
	if _, ok := c.Profiles[d.Profile]; !ok {
		return fmt.Errorf("device %s: unknown display_type %q", d.ID, d.Profile)
	}
	return nil
}

// normalizeDevice checks what a device file decides on its own and fills the
// defaults; whether its display type exists is a separate question, so the
// loader can keep a device whose type was renamed (ADR 0079).
func (c *Config) normalizeDevice(d *Device) error {
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
	switch d.Actions {
	case "", actionsAllow:
		d.Actions = actionsAllow
	case actionsDeny:
	default:
		return fmt.Errorf("device %s: actions %q: expected allow or deny", d.ID, d.Actions)
	}
	if d.MenuShowDuration == 0 {
		d.MenuShowDuration = c.MenuShow
	}
	if d.MenuShowDuration == 0 {
		d.MenuShowDuration = Duration(defaultMenuDuration)
	}
	if time.Duration(d.MenuShowDuration) < 5*time.Second || time.Duration(d.MenuShowDuration) > menuMaxDuration {
		return fmt.Errorf("device %s: menu_show_duration must be between 5s and %s", d.ID, menuMaxDuration)
	}
	return nil
}

// pluginPin normalizes a pinned plugin version to the form in the file name
// (ADR 0060): "2.1.0" and "v2.1.0" both become "v2.1.0".
// pluginPin normalizes a configured plugin version ("v1.4.0" and "1.4.0" name
// the same program); the leading v travels in the file name.
func pluginPin(pin string) (string, bool) {
	v := strings.TrimPrefix(strings.TrimSpace(pin), "v")
	if _, ok := semver.Parse(v); !ok {
		return "", false
	}
	return "v" + v, true
}

// parseChannel reads a configured channel (ADR 0089): an empty value is the
// stable channel every instance has been following so far.
func parseChannel(name string) (string, error) {
	switch ch := strings.TrimSpace(name); ch {
	case "":
		return channelStable, nil
	case channelStable, channelLatest:
		return ch, nil
	default:
		return "", fmt.Errorf("expected %s or %s", channelStable, channelLatest)
	}
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
			Timeout: time.Duration(p.Timeout), Env: env, Dir: c.PluginsDir, Config: p.Config,
			StateDir: filepath.Join(c.Dir, stateDirName, pluginsDirName, p.Name)}
	}
	return out
}

func (c *Config) timezoneName() string {
	if c.Timezone == nil {
		return "UTC"
	}
	return c.Timezone.String()
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

// stateKey names the device's persisted state: its MAC without separators
// (ADR 0080). The configuration validates the MAC, so it is always available.
func (d *Device) stateKey() string {
	key, _ := deviceStateKey(d.MAC)
	return key
}

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
