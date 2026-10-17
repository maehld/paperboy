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

// Device is one allowed display, identified by its Wi-Fi MAC address.
type Device struct {
	MAC          string    `json:"mac"`
	Name         string    `json:"name"`
	Profile      string    `json:"profile"`
	Refresh      Duration  `json:"refresh"`
	NightRefresh Duration  `json:"night_refresh"` // optional; used between night_start and night_end
	NightStart   ClockTime `json:"night_start"`
	NightEnd     ClockTime `json:"night_end"`
}

type Config struct {
	UploadToken string             `json:"upload_token"`
	PublicURL   string             `json:"public_url"`   // base URL for image links; empty = request host
	StaleAfter  Duration           `json:"stale_after"`  // mark images older than this; 0 = 30m
	PageTimeout Duration           `json:"page_timeout"` // back to page 0 after this; 0 = 5m
	Profiles    map[string]Profile `json:"profiles"`
	Devices     []Device           `json:"devices"`
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
		mac, err := net.ParseMAC(d.MAC)
		if err != nil {
			return fmt.Errorf("Gerät %q: ungültige MAC-Adresse", d.Name)
		}
		d.MAC = normalizeMAC(mac.String())
		if seen[d.MAC] {
			return fmt.Errorf("MAC-Adresse %s doppelt", d.MAC)
		}
		seen[d.MAC] = true
		if _, ok := c.Profiles[d.Profile]; !ok {
			return fmt.Errorf("Gerät %s: unbekanntes Profil %q", d.MAC, d.Profile)
		}
		if d.Refresh == 0 {
			d.Refresh = Duration(5 * time.Minute)
		}
		if time.Duration(d.Refresh) < minRefresh || (d.NightRefresh != 0 && time.Duration(d.NightRefresh) < minRefresh) {
			return fmt.Errorf("Gerät %s: Intervalle müssen mindestens 1m betragen", d.MAC)
		}
		if d.Name == "" {
			d.Name = d.MAC
		}
	}
	return nil
}

func (c *Config) device(mac string) *Device {
	mac = normalizeMAC(mac)
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
