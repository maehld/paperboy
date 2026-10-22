// Package paperboy serves ePaper devices running the TRMNL firmware. Pages are
// rendered from templates and chosen by plans (ADR 0024, 0025); the
// configuration is YAML (ADR 0026).
package paperboy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // embed IANA zones so the configured timezone needs no system tzdata

	"go.yaml.in/yaml/v3"

	"github.com/maehld/paperboy/internal/data"
	"github.com/maehld/paperboy/internal/provider"
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
// suffix of its page templates (pages/<page>.<display type>.yaml).
type Profile struct {
	Width      int      `json:"width"`
	Height     int      `json:"height"`
	Gray       bool     `json:"gray"`        // four gray levels instead of black/white
	WidthMM    int      `json:"width_mm"`    // physical panel width; lets providers pick a layout
	MinRefresh Duration `json:"min_refresh"` // reported intervals never go below this
}

// Device is one physical display. Its ID is stable and used in state.json, logs
// and the devices API; the TRMNL API identifies it by its Wi-Fi MAC address.
type Device struct {
	ID      string
	Type    string // protocol; only "trmnl" for now
	MAC     string
	Address string // reserved for OpenDisplay, which is not supported yet
	Buttons bool
	Profile string // display type
	Refresh Duration
	Battery *Battery // overrides the defaults
	Plan    string   // page plan under plans/; empty = notice page
}

// Battery configures the low-battery warning for TRMNL devices. A threshold
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
	TypeTRMNL = "trmnl"

	minRefresh         = time.Minute
	defaultRefresh     = 5 * time.Minute
	defaultPageTimeout = 5 * time.Minute
	defaultMinRefresh  = time.Minute
)

// Config is the validated runtime configuration.
type Config struct {
	UploadToken  string
	PublicURL    string
	Language     string         // "de" or "en"; built-in texts and number formats
	Timezone     *time.Location // plans and built-in dates
	Refresh      Duration       // default interval of a device; a plan rule may override it
	PageTimeout  Duration
	DataDir      string // from directories.data; empty = -data or paperboy-data
	Pages        string // page templates; empty = <data>/pages
	Plans        string // page plans; empty = <data>/plans
	ProvidersDir string // provider programs; empty = <data>/providers
	Profiles     map[string]Profile
	Devices      []Device
	Providers    []Provider // exec providers (ADR 0036)
	Battery      *Battery   // default low-battery warning; nil = off
}

// Provider is one exec provider (ADR 0036): a program in the providers
// directory that prints the new value of a data branch.
type Provider struct {
	Name     string
	Command  []string // argument vector, never a shell string
	Path     string   // data branch the provider owns
	Interval Duration // pause between two runs
	Timeout  Duration // hard limit for one run
	Env      map[string]string
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

// minRefresh returns the shortest interval reported for a display type.
func (c *Config) minRefresh(profile string) time.Duration {
	p := c.Profiles[profile]
	return max(minRefresh, time.Duration(p.MinRefresh))
}

// fileConfig mirrors paperboy.yaml.
type fileConfig struct {
	Language string `yaml:"language"` // de or en; default: system language
	Timezone string `yaml:"timezone"` // IANA name; default: system timezone
	Server   struct {
		PublicURL      string `yaml:"public_url"`
		UploadToken    string `yaml:"upload_token"`
		UploadTokenEnv string `yaml:"upload_token_env"`
	} `yaml:"server"`
	Directories struct {
		Pages     string `yaml:"pages"`
		Plans     string `yaml:"plans"`
		Data      string `yaml:"data"`
		Providers string `yaml:"providers"`
	} `yaml:"directories"`
	Defaults struct {
		Refresh     Duration `yaml:"refresh"`
		PageTimeout Duration `yaml:"page_timeout"`
		Battery     *Battery `yaml:"battery"`
	} `yaml:"defaults"`
	Providers map[string]struct {
		Command  []string          `yaml:"command"`
		Path     string            `yaml:"path"`
		Interval Duration          `yaml:"interval"`
		Timeout  Duration          `yaml:"timeout"`
		Env      map[string]string `yaml:"env"`
	} `yaml:"providers"`
	DisplayTypes map[string]struct {
		Kind       string   `yaml:"kind"`
		Size       [2]int   `yaml:"size"`
		GrayLevels int      `yaml:"gray_levels"`
		WidthMM    int      `yaml:"width_mm"`
		MinRefresh Duration `yaml:"min_refresh"`
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
	} `yaml:"devices"`
}

// LoadConfig reads and validates the YAML configuration. Relative directories
// are resolved against the directory of the file. The environment variable
// PAPERBOY_UPLOAD_TOKEN (formerly DISPLAYD_UPLOAD_TOKEN, still accepted)
// overrides the configured token so secrets can stay out of files.
func LoadConfig(path string) (*Config, error) {
	if strings.EqualFold(filepath.Ext(path), ".json") {
		return nil, fmt.Errorf("%s: JSON configuration is no longer supported; convert it to YAML (see paperboy.example.yaml)", path)
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

	base := filepath.Dir(path)
	tz := time.Local
	if f.Timezone != "" {
		loc, err := time.LoadLocation(f.Timezone)
		if err != nil {
			return nil, fmt.Errorf("%s: timezone %q: %w", path, f.Timezone, err)
		}
		tz = loc
	}
	c := &Config{
		UploadToken:  f.Server.UploadToken,
		PublicURL:    f.Server.PublicURL,
		Language:     f.Language,
		Timezone:     tz,
		Refresh:      f.Defaults.Refresh,
		PageTimeout:  f.Defaults.PageTimeout,
		Battery:      f.Defaults.Battery,
		DataDir:      resolveDir(base, f.Directories.Data),
		Pages:        resolveDir(base, f.Directories.Pages),
		Plans:        resolveDir(base, f.Directories.Plans),
		ProvidersDir: resolveDir(base, f.Directories.Providers),
		Profiles:     make(map[string]Profile, len(f.DisplayTypes)),
	}
	if name := f.Server.UploadTokenEnv; name != "" {
		if token := os.Getenv(name); token != "" {
			c.UploadToken = token
		} else if c.UploadToken == "" {
			return nil, fmt.Errorf("%s: upload_token_env %q is empty", path, name)
		}
	}
	for _, name := range []string{"DISPLAYD_UPLOAD_TOKEN", "PAPERBOY_UPLOAD_TOKEN"} {
		if token := os.Getenv(name); token != "" {
			c.UploadToken = token
		}
	}
	for name, t := range f.DisplayTypes {
		c.Profiles[name] = Profile{
			Width:      t.Size[0],
			Height:     t.Size[1],
			Gray:       t.GrayLevels == 4,
			WidthMM:    t.WidthMM,
			MinRefresh: t.MinRefresh,
		}
		if t.Kind == "text" {
			return nil, fmt.Errorf("%s: display type %s: kind text follows with ADR 0027", path, name)
		}
		if t.Kind != "" && t.Kind != "image" {
			return nil, fmt.Errorf("%s: display type %s: unknown kind %q", path, name, t.Kind)
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
		})
	}
	names := make([]string, 0, len(f.Providers))
	for name := range f.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		p := f.Providers[name]
		c.Providers = append(c.Providers, Provider{
			Name:     name,
			Command:  p.Command,
			Path:     p.Path,
			Interval: p.Interval,
			Timeout:  p.Timeout,
			Env:      p.Env,
		})
	}
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

func resolveDir(base, p string) string {
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
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
	if len(c.UploadToken) < 16 {
		return fmt.Errorf("upload_token missing or shorter than 16 characters")
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
	c.PublicURL = strings.TrimRight(c.PublicURL, "/")
	if err := c.Battery.validate("battery"); err != nil {
		return err
	}
	if len(c.Profiles) == 0 {
		return fmt.Errorf("display_types missing")
	}
	for name, p := range c.Profiles {
		if !validName(name) {
			return fmt.Errorf("display type %q: only a-z, 0-9 and -", name)
		}
		if p.Width < 1 || p.Height < 1 || p.Width > 4096 || p.Height > 4096 {
			return fmt.Errorf("display type %s: invalid size", name)
		}
		if p.MinRefresh == 0 {
			p.MinRefresh = Duration(defaultMinRefresh)
		}
		if time.Duration(p.MinRefresh) < time.Second {
			return fmt.Errorf("display type %s: min_refresh must be at least 1s", name)
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
		case "", TypeTRMNL:
			d.Type = TypeTRMNL
			if d.Address != "" || d.Buttons {
				return fmt.Errorf("device %s: address/buttons belong to protocol opendisplay, which is not supported yet", d.ID)
			}
			mac, err := net.ParseMAC(d.MAC)
			if err != nil {
				return fmt.Errorf("device %s: invalid MAC address", d.ID)
			}
			d.MAC = normalizeMAC(mac.String())
		case "opendisplay":
			return fmt.Errorf("device %s: protocol opendisplay is not supported yet (TODO in README)", d.ID)
		case "text-http", "image-http":
			return fmt.Errorf("device %s: protocol %q follows with ADR 0027", d.ID, d.Type)
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
		if time.Duration(d.Refresh) < minRefresh {
			return fmt.Errorf("device %s: refresh must be at least 1m", d.ID)
		}
		if d.Plan != "" && !validName(d.Plan) {
			return fmt.Errorf("device %s: plan %q: only a-z, 0-9 and -", d.ID, d.Plan)
		}
	}
	if err := c.validateProviders(); err != nil {
		return err
	}
	return nil
}

// validateProviders checks the exec providers (ADR 0036): every provider needs a
// command and owns exactly one data branch.
func (c *Config) validateProviders() error {
	for i := range c.Providers {
		p := &c.Providers[i]
		if !validName(p.Name) {
			return fmt.Errorf("provider %q: name must use a-z, 0-9 and -", p.Name)
		}
		if len(p.Command) == 0 || p.Command[0] == "" {
			return fmt.Errorf("provider %s: command missing", p.Name)
		}
		if p.Path == "" {
			return fmt.Errorf("provider %s: path missing; a provider writes one branch, not the whole document", p.Name)
		}
		p.Path = strings.Trim(p.Path, "/")
		if p.Path == "" {
			return fmt.Errorf("provider %s: path must name a branch, not the whole document", p.Name)
		}
		if err := data.ValidPath(p.Path); err != nil {
			return fmt.Errorf("provider %s: path %q: %w", p.Name, p.Path, err)
		}
		spec := provider.Spec{Interval: time.Duration(p.Interval), Timeout: time.Duration(p.Timeout)}
		if err := spec.Validate(); err != nil {
			return fmt.Errorf("provider %s: %w", p.Name, err)
		}
		p.Interval, p.Timeout = Duration(spec.Interval), Duration(spec.Timeout)
		for _, other := range c.Providers[:i] {
			if pathsOverlap(p.Path, other.Path) {
				return fmt.Errorf("providers %s and %s: paths %q and %q overlap; each branch has exactly one owner",
					other.Name, p.Name, other.Path, p.Path)
			}
		}
	}
	return nil
}

// pathsOverlap reports whether one path contains the other, which would let two
// providers overwrite the same data.
func pathsOverlap(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

// device returns the configured TRMNL device with this MAC address.
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
