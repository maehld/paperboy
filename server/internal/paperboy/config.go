// Package paperboy serves ePaper devices running the Paperboy firmware over the
// device API (ADR 0041, 0042). A page is a grid of plugin widgets (ADR 0049)
// and plans choose the page (ADR 0025); the configuration is YAML (ADR 0026)
// in a fixed instance directory (ADR 0037).
package paperboy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	_ "time/tzdata" // embed IANA zones so the configured timezone needs no system tzdata

	"github.com/maehld/paperboy/server/internal/duration"
	"github.com/maehld/paperboy/server/internal/ident"
	"github.com/maehld/paperboy/server/internal/page"
	"github.com/maehld/paperboy/server/internal/widget"
)

// Duration is the duration type of the configuration (ADR 0026). It reads a
// Go duration string such as "5m" and keeps it readable in JSON.
type Duration = duration.Duration

// Profile describes one display type (ADR 0026); a page names it to say which
// panel it draws for. A text display type (ADR 0044) uses Width and Height for
// columns and rows.
type Profile struct {
	Kind       string   `json:"kind"`              // "image" or "text"
	Charset    string   `json:"charset,omitempty"` // text displays: utf8, ascii or hd44780
	Width      int      `json:"width"`
	Height     int      `json:"height"`
	Levels     int      `json:"levels"`                // gray tones of an image display: 2, 4 or 16
	PPI        float64  `json:"ppi,omitempty"`         // image displays: pixels per inch (ADR 0050)
	MinRefresh Duration `json:"min_refresh,omitempty"` // configured floor; 0 = default of the power source (ADR 0051)
	Power      string   `json:"power"`                 // auto, battery or mains (ADR 0051)
	// Rotation turns composed pages clockwise before the firmware paints them
	// (ADR 0102): 0, 90, 180 or 270 degrees relative to the orientation the
	// firmware was built for. A portrait page on a landscape panel needs 90 or
	// 270; a device may override it for its mounting.
	Rotation int `json:"rotation,omitempty"`
	// ScrollStep is the time between two marquee steps of a text display
	// (ADR 0118); 300 ms unless the display type sets scroll.step.
	ScrollStep time.Duration `json:"scroll_step,omitempty"`
	// Grid is the default grid of a new image page of this type (ADR 0128):
	// six by eight unless the display type names another one. A page owns its
	// own grid, so this only decides where the editor starts. Nil for text.
	Grid *[2]int `json:"grid,omitempty"`
	// Header is the band every page of this type shows (ADR 0168); a page may
	// hide it with header: false. Nil means no band.
	Header *page.Header `json:"header,omitempty"`
}

// validRotation reports whether n is one of the quarter turns a device and a
// display type may name (ADR 0102).
func validRotation(n int) bool { return n == 0 || n == 90 || n == 180 || n == 270 }

// rotation returns the effective rotation of d (ADR 0102): the device's
// override, else the display type's.
func (c *Config) rotation(d *Device) int {
	if d.Rotation != nil {
		return *d.Rotation
	}
	return c.Profiles[d.Profile].Rotation
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

// DefaultScrollStep is the marquee step of a text display (ADR 0118).
const DefaultScrollStep = 300 * time.Millisecond

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
	Profile string // display type
	Refresh Duration
	Battery *Battery // overrides the defaults
	Plan    string   // page plan under plans/; empty = notice page
	Hold    bool     // firmware: hold keeps the device on its firmware version (ADR 0043)
	Power   string   // auto, battery or mains; empty = the display type's (ADR 0051)
	// Actions allows the device to run actions: "allow" (default) or "deny".
	Actions string
	// Touch says the operator wants the device's digitiser used. Empty = use
	// what the firmware reported, which defaults to false (ADR 0082).
	Touch *bool
	// AlertMethods lists the methods an alert may use: beep, blink or both
	// (ADR 0132). nil or an empty list keeps the device silent.
	AlertMethods *[]string
	// Rotation overrides the display type's rotation for this mounting
	// (ADR 0102). nil = the display type's.
	Rotation *int
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

	minRefresh     = time.Minute
	defaultRefresh = 5 * time.Minute

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
	Refresh     Duration       // default interval of a device; a plan refresh window may override it
	Pages       string         // page templates: <dir>/pages
	Plans       string         // page plans: <dir>/plans
	DisplaysDir string         // display type files: <dir>/displays (ADR 0071)
	DevicesDir  string         // device files: <dir>/devices (ADR 0070)
	PluginsDir  string         // plugin programs: <dir>/plugins (ADR 0049)
	FirmwareDir string         // firmware images: <dir>/firmware (ADR 0043)
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
// instance that never set one. Hosts limits the Host header the UI answers to
// (ADR 0136); an empty list accepts every host.
type UIConfig struct {
	Password string   // ui.password, ${VAR} already expanded from the environment
	Hosts    []string // ui.hosts, lower case and without a port
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

// touch reports whether Paperboy may use the device digitiser
// (ADR 0082): the device file wins, otherwise what the firmware reported, and
// without both the digitiser stays unused.
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
		PageTimeout any      `yaml:"page_timeout"`       // removed by ADR 0130; rejected with a hint
		MenuShow    any      `yaml:"menu_show_duration"` // removed by ADR 0161; rejected with a hint
		Battery     *Battery `yaml:"battery"`
	} `yaml:"defaults"`
	Firmware struct {
		Update          string  `yaml:"update"`
		MinBatteryVolts float64 `yaml:"min_battery_volts"`
		Version         string  `yaml:"version"` // pin for downloads and offers (ADR 0060)
	} `yaml:"firmware"`
	Download *downloadFile `yaml:"download"`
	UI       struct {
		Password string   `yaml:"password"` // operator UI password (ADR 0096)
		Hosts    []string `yaml:"hosts"`    // host names the UI answers to (ADR 0136)
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

func validName(s string) bool { return ident.Valid(s) }

// refresh returns the regular sleep time of the device. Plans may override it.
func (d *Device) refresh() time.Duration { return time.Duration(d.Refresh) }
