package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

// Values answers the declared values of a text page from the last vehicle
// state (ADR 0111). A value the API did not report becomes a dash.
func (m *myskoda) Values(req *plugin.Request) (map[string]plugin.Value, error) {
	dash := plugin.TextValue("–")
	out := make(map[string]plugin.Value, len(req.Values))
	for _, name := range req.Values {
		switch name {
		case "battery-soc":
			if v, ok := m.soc(); ok {
				out[name] = plugin.NumberValue(float64(v))
				continue
			}
			out[name] = dash
		case "battery-range":
			if v, ok := m.rangeKM(); ok {
				out[name] = plugin.NumberValue(float64(v))
				continue
			}
			out[name] = dash
		case "charge-state":
			out[name] = plugin.TextValue(m.chargeState())
		case "charge-power":
			if v, ok := m.power(); ok {
				out[name] = plugin.NumberValue(v)
				continue
			}
			out[name] = dash
		case "charge-remaining":
			if d, ok := m.remainingDuration(); ok {
				out[name] = plugin.DurationValue(d)
				continue
			}
			out[name] = dash
		case "charge-target":
			if v, ok := m.targetSoC(); ok {
				out[name] = plugin.NumberValue(float64(v))
				continue
			}
			out[name] = dash
		case "car-name":
			out[name] = plugin.TextValue(m.carName())
		case "license-plate":
			if v := strings.TrimSpace(m.cur.LicensePlate); v != "" {
				out[name] = plugin.TextValue(v)
				continue
			}
			out[name] = dash
		case "lock-state":
			out[name] = plugin.TextValue(m.lockState())
		case "odometer":
			if o := m.cur.Odometer; o != nil && o.KM != nil {
				out[name] = plugin.NumberValue(float64(*o.KM))
				continue
			}
			out[name] = dash
		case "location":
			out[name] = plugin.TextValue(m.parkingText())
		case "latitude", "longitude":
			p := m.cur.ParkingPosition
			if p == nil || p.GPS == nil || p.State == "IN_MOTION" {
				out[name] = dash
				continue
			}
			v := p.GPS.Latitude
			if name == "longitude" {
				v = p.GPS.Longitude
			}
			out[name] = plugin.NumberValue(v)
		case "climate-state":
			if m.cur.AirConditioning == nil {
				out[name] = dash
				continue
			}
			out[name] = plugin.TextValue(m.climateState())
		case "climate-temperature":
			c := m.cur.AirConditioning
			if c == nil || c.Target.Value == nil {
				out[name] = dash
				continue
			}
			out[name] = plugin.NumberValue(*c.Target.Value)
		case "climate-reach":
			c := m.cur.AirConditioning
			if c == nil {
				out[name] = dash
				continue
			}
			if t, ok := m.time(c.ReachAt); ok {
				out[name] = plugin.TimestampValue(t)
				continue
			}
			out[name] = dash
		case "profiles":
			out[name] = plugin.TextValue(m.profileLines())
		case "captured":
			if s := m.capturedAt(); !s.IsZero() {
				out[name] = plugin.TimestampValue(s)
				continue
			}
			out[name] = dash
		default:
			return nil, fmt.Errorf("%w %q", plugin.ErrUnknownValue, name)
		}
	}
	return out, nil
}

// carName is the configured name, or the VIN when the API has none.
func (m *myskoda) carName() string {
	if m.cur.Name != "" {
		return m.cur.Name
	}
	return m.cur.VIN
}

// remainingDuration is the time until the battery is full, from the API's
// minutes or the moment it says the charge ends.
func (m *myskoda) remainingDuration() (time.Duration, bool) {
	c := m.cur.Charging
	if c == nil || c.Status == nil {
		return 0, false
	}
	if c.Status.RemainingMin != nil && *c.Status.RemainingMin > 0 {
		return time.Duration(*c.Status.RemainingMin) * time.Minute, true
	}
	if t, ok := m.time(c.Status.FullyChargedAt); ok {
		return max(t.Sub(m.req.Now), 0), true
	}
	return 0, false
}

// parkingText is the address of the parked car, or that it is moving.
func (m *myskoda) parkingText() string {
	p := m.cur.ParkingPosition
	if p == nil {
		return m.t("Keine Position", "No position")
	}
	if p.State == "IN_MOTION" {
		return m.t("Unterwegs", "In motion")
	}
	if strings.TrimSpace(p.Address) != "" {
		return strings.TrimSpace(p.Address)
	}
	return m.t("Geparkt", "Parked")
}

// capturedAt is the time the vehicle sent the newest part of its state.
func (m *myskoda) capturedAt() time.Time {
	var newest time.Time
	for _, s := range []string{m.capturedOfCharging(), m.capturedOfParking()} {
		if t, ok := m.time(s); ok && t.After(newest) {
			newest = t
		}
	}
	return newest
}

func (m *myskoda) capturedOfCharging() string {
	if m.cur.Charging == nil {
		return ""
	}
	return m.cur.Charging.Captured
}

func (m *myskoda) capturedOfParking() string {
	if m.cur.ParkingPosition == nil {
		return ""
	}
	return m.cur.ParkingPosition.Captured
}

// profileLines writes one line per charging profile: name, target and the
// times. The profile of the moment carries a star.
func (m *myskoda) profileLines() string {
	list := m.profileList(5)
	if len(list) == 0 {
		return m.t("Keine Ladepläne", "No charging plans")
	}
	current := m.currentProfile()
	lines := make([]string, 0, len(list))
	for _, p := range list {
		name := p.Name
		if current != nil && current.Name == p.Name {
			name = "* " + name
		}
		line := name
		if p.Settings.TargetSoC != nil {
			line += fmt.Sprintf(" %d %%", *p.Settings.TargetSoC)
		}
		if times := m.profileTimes(p); times != "" {
			line += " " + times
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
