// Package displayd serves ePaper devices running the TRMNL firmware. Producers
// such as SolarAI upload one PNG per profile and page; devices wake up, fetch
// their current page and go back to deep sleep. The package deliberately does
// not import any SolarAI package so it can move into its own repository.
package displayd

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// Duration accepts Go duration strings such as "5m" in JSON.
type Duration time.Duration

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("Dauer als Zeichenkette erwartet, z. B. \"5m\"")
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

// ClockTime is a local time of day written as "22:00".
type ClockTime int // minutes since midnight

func (c *ClockTime) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	t, err := time.Parse("15:04", s)
	if err != nil {
		return fmt.Errorf("Uhrzeit im Format HH:MM erwartet: %q", s)
	}
	*c = ClockTime(t.Hour()*60 + t.Minute())
	return nil
}

// Profile describes one panel format; all devices of a profile share images.
type Profile struct {
	Width   int  `json:"width"`
	Height  int  `json:"height"`
	Gray    bool `json:"gray"`     // four gray levels instead of black/white
	WidthMM int  `json:"width_mm"` // physical panel width; lets producers pick a layout
}

// Device is one display. TRMNL devices fetch images themselves and are
// identified by their Wi-Fi MAC address; OpenDisplay devices are pushed to.
type Device struct {
	Type         string    `json:"type"` // "trmnl" (default) or "opendisplay"
	MAC          string    `json:"mac"`
	Address      string    `json:"address"` // OpenDisplay: host[:port]
	Buttons      bool      `json:"buttons"` // OpenDisplay: poll buttons over a persistent connection
	Name         string    `json:"name"`
	Profile      string    `json:"profile"`
	Refresh      Duration  `json:"refresh"`
	NightRefresh Duration  `json:"night_refresh"` // optional; used between night_start and night_end
	NightStart   ClockTime `json:"night_start"`
	NightEnd     ClockTime `json:"night_end"`
	Battery      *Battery  `json:"battery"` // TRMNL: overrides the global battery warning
}

// Battery configures the low-battery warning for TRMNL devices. A threshold
// of 0 disables that stage.
type Battery struct {
	WarnVolts       float64 `json:"warn_volts"`       // banner on every page below this
	CriticalVolts   float64 `json:"critical_volts"`   // full-screen warning below this
	HysteresisVolts float64 `json:"hysteresis_volts"` // a stage ends this far above its threshold; 0 = 0.1
}

func (b *Battery) validate(owner string) error {
	if b == nil {
		return nil
	}
	if b.WarnVolts < 0 || b.CriticalVolts < 0 || b.HysteresisVolts < 0 || b.WarnVolts > 5 || b.CriticalVolts > 5 {
		return fmt.Errorf("%s: ungültige Akkuschwellen", owner)
	}
	if b.WarnVolts > 0 && b.CriticalVolts > 0 && b.CriticalVolts >= b.WarnVolts {
		return fmt.Errorf("%s: critical_volts muss unter warn_volts liegen", owner)
	}
	if b.HysteresisVolts == 0 {
		b.HysteresisVolts = 0.1
	}
	return nil
}

const (
	TypeTRMNL       = "trmnl"
	TypeOpenDisplay = "opendisplay"
)

// key identifies the device in the persisted state.
func (d *Device) key() string {
	if d.Type == TypeOpenDisplay {
		return "opendisplay:" + d.Name
	}
	return d.MAC
}

type Config struct {
	UploadToken string             `json:"upload_token"`
	PublicURL   string             `json:"public_url"`   // base URL for image links; empty = request host
	StaleAfter  Duration           `json:"stale_after"`  // mark images older than this; 0 = 30m
	PageTimeout Duration           `json:"page_timeout"` // back to page 0 after this; 0 = 5m
	Profiles    map[string]Profile `json:"profiles"`
	Devices     []Device           `json:"devices"`
	Battery     *Battery           `json:"battery"` // low-battery warning for TRMNL devices; nil = off
}

// battery returns the effective battery warning settings for d, or nil.
func (c *Config) battery(d *Device) *Battery {
	if d.Battery != nil {
		return d.Battery
	}
	return c.Battery
}

const (
	minRefresh = time.Minute
	maxPages   = 16
)

// LoadConfig reads and validates the configuration. The environment variable
// DISPLAYD_UPLOAD_TOKEN overrides upload_token so secrets can stay out of files.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if token := os.Getenv("DISPLAYD_UPLOAD_TOKEN"); token != "" {
		c.UploadToken = token
	}
	return &c, c.validate()
}

func (c *Config) validate() error {
	if len(c.UploadToken) < 16 {
		return fmt.Errorf("upload_token fehlt oder ist kürzer als 16 Zeichen")
	}
	if c.StaleAfter == 0 {
		c.StaleAfter = Duration(30 * time.Minute)
	}
	if c.PageTimeout == 0 {
		c.PageTimeout = Duration(5 * time.Minute)
	}
	c.PublicURL = strings.TrimRight(c.PublicURL, "/")
	if err := c.Battery.validate("battery"); err != nil {
		return err
	}
	for name, p := range c.Profiles {
		if !validName(name) {
			return fmt.Errorf("Profilname %q: nur a–z, 0–9 und -", name)
		}
		if p.Width < 1 || p.Height < 1 || p.Width > 4096 || p.Height > 4096 {
			return fmt.Errorf("Profil %s: ungültige Größe", name)
		}
	}
	seen := map[string]bool{}
	for i := range c.Devices {
		d := &c.Devices[i]
		switch d.Type {
		case "", TypeTRMNL:
			d.Type = TypeTRMNL
			mac, err := net.ParseMAC(d.MAC)
			if err != nil {
				return fmt.Errorf("Gerät %q: ungültige MAC-Adresse", d.Name)
			}
			d.MAC = normalizeMAC(mac.String())
			if d.Name == "" {
				d.Name = d.MAC
			}
		case TypeOpenDisplay:
			if d.Address == "" || d.Name == "" {
				return fmt.Errorf("OpenDisplay-Gerät braucht name und address")
			}
			if _, _, err := net.SplitHostPort(d.Address); err != nil {
				d.Address = net.JoinHostPort(d.Address, "2446")
			}
			d.MAC = "" // only TRMNL devices may use the device API
		default:
			return fmt.Errorf("Gerät %q: unbekannter Typ %q", d.Name, d.Type)
		}
		if err := d.Battery.validate("Gerät " + d.Name); err != nil {
			return err
		}
		if seen[d.key()] {
			return fmt.Errorf("Gerät %s doppelt", d.key())
		}
		seen[d.key()] = true
		if _, ok := c.Profiles[d.Profile]; !ok {
			return fmt.Errorf("Gerät %s: unbekanntes Profil %q", d.Name, d.Profile)
		}
		if d.Refresh == 0 {
			d.Refresh = Duration(5 * time.Minute)
		}
		if time.Duration(d.Refresh) < minRefresh || (d.NightRefresh != 0 && time.Duration(d.NightRefresh) < minRefresh) {
			return fmt.Errorf("Gerät %s: Intervalle müssen mindestens 1m betragen", d.Name)
		}
	}
	return nil
}

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

// refresh returns the regular sleep time at local time now.
func (d *Device) refresh(now time.Time) time.Duration {
	if d.NightRefresh == 0 || d.NightStart == d.NightEnd {
		return time.Duration(d.Refresh)
	}
	m := ClockTime(now.Hour()*60 + now.Minute())
	night := m >= d.NightStart && m < d.NightEnd
	if d.NightStart > d.NightEnd { // window crosses midnight
		night = m >= d.NightStart || m < d.NightEnd
	}
	if night {
		return time.Duration(d.NightRefresh)
	}
	return time.Duration(d.Refresh)
}
