package paperboy

import (
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/server/internal/builtin"
	"github.com/maehld/paperboy/server/internal/page"
)

// previewDeviceID is the id of the sample device a preview without a device
// shows (ADR 0123). It names sample values, so the operator sees what a widget
// draws.
const previewDeviceID = "preview"

// previewDeviceStatus is the deterministic status of a preview that names no
// device (ADR 0123). It reads no state file, so every preview shows the same
// row: ok, 78 %, 4.05 V, not charging and a runtime of about 12 days.
func previewDeviceStatus(display string) plugin.DeviceStatus {
	percent, runtime := 78.0, int64(12*24*time.Hour/time.Second)
	return plugin.DeviceStatus{
		ID:           previewDeviceID,
		Display:      display,
		Status:       "ok",
		SeenS:        0,
		Power:        powerBattery,
		Battery:      &plugin.BatteryStatus{Percent: &percent, Volts: 4.05},
		RuntimeS:     runtime,
		RuntimeSpanS: runtime,
	}
}

// statusSnapshot collects the device status a page may show (ADR 0115). display
// names the display type the preview draws for. self names the device the page
// is rendered for; empty means a preview without a device, which gets the
// sample device of ADR 0123. selected lists the ids the page's widgets name.
// The state lock is held only while the values are read, so composing and
// drawing still happen outside it.
func (s *Server) statusSnapshot(cfg *Config, display, self string, selected []string, now time.Time) *plugin.ServerStatus {
	out := &plugin.ServerStatus{}
	byID := map[string]plugin.DeviceStatus{}
	s.mu.Lock()
	for i := range cfg.Devices {
		d := &cfg.Devices[i]
		var state deviceState
		if st := s.state[d.stateKey()]; st != nil {
			state = *st
		}
		byID[d.ID] = statusOfDevice(cfg, d, state, now)
	}
	s.mu.Unlock()
	if sd, ok := byID[self]; ok {
		out.CurrentDeviceStatus = &sd
	} else if self == "" {
		sd := previewDeviceStatus(display)
		out.CurrentDeviceStatus = &sd
	}
	for _, id := range selected {
		if id == "" {
			continue
		}
		sd, ok := byID[id]
		if !ok {
			sd = plugin.DeviceStatus{ID: id, Status: "unknown", SeenS: -1}
		}
		if out.SelectedDevicesStatus == nil {
			out.SelectedDevicesStatus = map[string]plugin.DeviceStatus{}
		}
		out.SelectedDevicesStatus[id] = sd
	}
	return out
}

// statusOfDevice turns one device and its state into the status a widget sees.
func statusOfDevice(cfg *Config, d *Device, st deviceState, now time.Time) plugin.DeviceStatus {
	sd := plugin.DeviceStatus{
		ID:       d.ID,
		Display:  d.Profile,
		Plan:     d.Plan,
		Hardware: st.Hardware,
		Firmware: st.Firmware,
		Status:   deviceStatus(*d, st, now),
		LastSeen: st.LastSeen,
		SeenS:    -1,
		Power:    cfg.power(d, st.ReportedPower),
		RSSI:     st.RSSI,
		Page:     st.LastPage,
		PageKind: st.LastPageKind,
		Error:    st.LastError,
	}
	if !st.LastSeen.IsZero() {
		seen := now.Sub(st.LastSeen)
		if seen < 0 {
			seen = 0
		}
		sd.SeenS = int64(seen / time.Second)
	}
	if st.BatteryVoltage != "" || st.BatteryPercent != nil || st.BatteryMAH != nil || st.BatteryMA != nil {
		b := &plugin.BatteryStatus{
			Percent:  copyFloat(st.BatteryPercent),
			MAH:      copyFloat(st.BatteryMAH),
			MA:       copyFloat(st.BatteryMA),
			Level:    st.BatteryLevel,
			Charging: st.BatteryCharging != nil && *st.BatteryCharging,
		}
		if v, ok := parseVoltage(st.BatteryVoltage); ok {
			b.Volts = v
		}
		if st.Charge != nil || (st.BatteryMA != nil && *st.BatteryMA > idleMA) {
			b.Charging = true
		}
		sd.Battery = b
	}
	if sd.Power != powerMains && sd.Battery != nil {
		e := estimateBattery(st)
		if e.Runtime > 0 {
			sd.RuntimeS = int64(e.Runtime / time.Second)
			sd.RuntimeSpanS = int64(e.Span / time.Second)
		}
	}
	return sd
}

// selectedDeviceIDs collects the ids a page asks for: the devices argument of
// device-status cells and the device argument of battery cells.
func selectedDeviceIDs(t *page.Template) []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range t.Cells {
		var ids []string
		switch c.Widget {
		case builtin.Name + "/device-status":
			ids = plugin.Values(c.Args).Strings("devices")
		case builtin.Name + "/battery":
			if id, ok := c.Args["device"].(string); ok {
				ids = []string{id}
			}
		}
		for _, id := range ids {
			if id != "" && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// selectedDeviceIDsFor is selectedDeviceIDs for one widget preview.
func selectedDeviceIDsFor(pluginName, widget string, args map[string]any) []string {
	t := &page.Template{Cells: []page.Cell{{Widget: pluginName + "/" + widget, Args: args}}}
	return selectedDeviceIDs(t)
}

// deviceScoped reports whether a page draws the device it is rendered for.
// Such a page is cached per device, not per display type, so two devices that
// share it cannot overwrite each other's status (ADR 0115).
func deviceScoped(t *page.Template) bool {
	for _, c := range t.Cells {
		switch c.Widget {
		case builtin.Name + "/device-status":
			named := false
			for _, id := range plugin.Values(c.Args).Strings("devices") {
				if id != "" {
					named = true
				}
			}
			if !named {
				return true
			}
		case builtin.Name + "/battery":
			if id, ok := c.Args["device"].(string); !ok || id == "" {
				return true
			}
		case builtin.Name + "/header":
			if plugin.Values(c.Args).Bool("battery", false) {
				return true
			}
		}
	}
	return false
}

// statusKey is the render cache key of a page: the page alone, or the page
// with the device when the page shows that device (ADR 0115).
func statusKey(profile, name string, t *page.Template, status *plugin.ServerStatus) string {
	key := profile + "/" + name
	if status != nil && status.CurrentDeviceStatus != nil && t != nil && deviceScoped(t) {
		key += "/" + status.CurrentDeviceStatus.ID
	}
	return key
}
