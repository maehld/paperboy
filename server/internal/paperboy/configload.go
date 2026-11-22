package paperboy

import (
	"bytes"
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

	"go.yaml.in/yaml/v3"

	"github.com/maehld/paperboy/server/internal/builtin"
	"github.com/maehld/paperboy/server/internal/download"
	"github.com/maehld/paperboy/server/internal/page"
	"github.com/maehld/paperboy/server/internal/semver"
)

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
	if f.Defaults.PageTimeout != nil {
		return nil, fmt.Errorf("%s: defaults.page_timeout was removed: the plan decides with on_refresh (ADR 0130); remove it", path)
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
	// Host names a browser uses to reach the UI (ADR 0136); the comparison
	// ignores case and the port, so "Paperboy.Local:8090" works too.
	c.UI.Hosts = append([]string(nil), f.UI.Hosts...)
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}
func (c *Config) validate() error {
	if c.Timezone == nil {
		c.Timezone = time.Local
	}
	hosts := make([]string, 0, len(c.UI.Hosts))
	for _, host := range c.UI.Hosts {
		h := hostOnly(host)
		if h == "" || strings.ContainsAny(h, "/\\@?#") {
			return fmt.Errorf("ui: hosts: %q is not a host name; give a name or an IP address without a scheme, port or path", host)
		}
		hosts = append(hosts, h)
	}
	c.UI.Hosts = hosts
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
		if p, ok := c.Profiles[d.Profile]; !ok {
			// A device whose display type is gone stays loaded: renaming the
			// type must not take the whole server down, and the operator can
			// repair the file in the editor (ADR 0079).
			d.Problem = fmt.Sprintf("display type %q is not configured", d.Profile)
		} else if err := checkDeviceRotation(d, p); err != nil {
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
		if p.ScrollStep == 0 {
			p.ScrollStep = DefaultScrollStep
		}
		if p.ScrollStep < 50*time.Millisecond {
			return fmt.Errorf("display type %s: scroll.step must be at least 50ms", name)
		}
		if p.PPI != 0 || p.Levels > 2 {
			return fmt.Errorf("display type %s: gray_levels and ppi do not apply to kind text", name)
		}
	} else if p.Charset != "" {
		return fmt.Errorf("display type %s: charset only applies to kind text", name)
	} else if p.ScrollStep != 0 {
		return fmt.Errorf("display type %s: scroll only applies to kind text", name)
	} else if p.PPI < 20 || p.PPI > 1000 {
		return fmt.Errorf("display type %s: ppi is required for image displays: pixels per inch between 20 and 1000, e.g. 125 (ADR 0050)", name)
	} else {
		if p.Levels == 0 {
			p.Levels = 2
		}
		if p.Levels != 2 && p.Levels != 4 && p.Levels != 16 {
			return fmt.Errorf("display type %s: gray_levels must be 1, 4 or 16", name)
		}
		if p.Grid == nil {
			g := defaultGrid
			p.Grid = &g
		}
		if p.Grid[0] < 1 || p.Grid[1] < 1 || p.Grid[0] > maxGrid || p.Grid[1] > maxGrid {
			return fmt.Errorf("display type %s: grid [%d, %d]: expected [columns, rows] between 1 and %d",
				name, p.Grid[0], p.Grid[1], maxGrid)
		}
	}
	if !validRotation(p.Rotation) {
		return fmt.Errorf("display type %s: rotation must be 0, 90, 180 or 270", name)
	}
	if p.Rotation != 0 && p.text() {
		return fmt.Errorf("display type %s: rotation does not apply to text displays", name)
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
	p, ok := c.Profiles[d.Profile]
	if !ok {
		return fmt.Errorf("device %s: unknown display_type %q", d.ID, d.Profile)
	}
	return checkDeviceRotation(d, p)
}

// checkDeviceRotation rejects a rotation the display type cannot use: text
// displays have no pixels to turn (ADR 0102).
func checkDeviceRotation(d *Device, p Profile) error {
	if d.Rotation != nil && *d.Rotation != 0 && p.text() {
		return fmt.Errorf("device %s: rotation does not apply to text displays", d.ID)
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
		mac, err := net.ParseMAC(d.MAC)
		if err != nil {
			return fmt.Errorf("device %s: invalid MAC address", d.ID)
		}
		d.MAC = normalizeMAC(mac.String())
	case "trmnl":
		return fmt.Errorf("device %s: protocol trmnl was removed (ADR 0042); flash the Paperboy firmware and set protocol: paperboy", d.ID)
	case "opendisplay":
		return fmt.Errorf("device %s: protocol opendisplay was removed (ADR 0119); set protocol: paperboy", d.ID)
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
	if d.Rotation != nil && !validRotation(*d.Rotation) {
		return fmt.Errorf("device %s: rotation must be 0, 90, 180 or 270", d.ID)
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
