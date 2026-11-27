package main

import (
	"fmt"
	"strings"
)

// The keys of the state value (ADR 0127).
const (
	stateOK        = "ok"
	stateAttention = "attention"
	stateDegraded  = "degraded"
)

// stateKeys are the keys of the state value, in the order of the declaration.
var stateKeys = []string{stateOK, stateAttention, stateDegraded}

// siteInfo is the resolved site: the id for the documented integration API and
// the internalReference for the private controller path.
type siteInfo struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	InternalReference string `json:"internalReference"`
}

// device is one device of the documented integration API.
type device struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Model             string `json:"model"`
	State             string `json:"state"`
	FirmwareVersion   string `json:"firmwareVersion"`
	FirmwareUpdatable bool   `json:"firmwareUpdatable"`
	IPAddress         string `json:"ipAddress"`
}

// snapshot is the last controller answer the widgets and values show. It is
// copied under the plugin's lock and never changed afterwards.
type snapshot struct {
	loaded  bool
	site    siteInfo
	devices []device
	clients int
	wan     healthSubsystem
	www     healthSubsystem
}

// stateKey derives the state: degraded when the WAN or the internet status is
// not ok (both need wanEnabled) or a device is offline, attention when a device
// has firmware work, ok otherwise. Without data it is empty, so no plan
// condition matches (ADR 0127).
func (sn snapshot) stateKey(wanEnabled bool) string {
	if !sn.loaded {
		return ""
	}
	if wanEnabled && sn.wan.Status != "" && sn.wan.Status != "ok" {
		return stateDegraded
	}
	if sn.www.Status != "" && sn.www.Status != "ok" {
		return stateDegraded
	}
	if sn.offline() > 0 {
		return stateDegraded
	}
	if sn.updates() > 0 {
		return stateAttention
	}
	return stateOK
}

// WAN states of the connection alone (ADR 0177). The plugin reads them from
// the private health answer: the wan entry and the internet test.
const (
	wanOK      = "ok"
	wanWarning = "warning"
	wanError   = "error"
)

// wanStateKey derives the state of the connection: error when the controller
// or the internet test reports an error, warning for every other non-ok
// answer, ok when both are ok. It is empty without wan: true or without data,
// so no plan condition matches (ADR 0127).
func (sn snapshot) wanStateKey(wanEnabled bool) string {
	if !wanEnabled || !sn.loaded || sn.wan.Status == "" {
		return ""
	}
	if sn.wan.Status == wanError || sn.www.Status == wanError {
		return wanError
	}
	if sn.wan.Status != wanOK || (sn.www.Status != "" && sn.www.Status != wanOK) {
		return wanWarning
	}
	return wanOK
}

// offline counts the devices that count as down.
func (sn snapshot) offline() int {
	n := 0
	for _, d := range sn.devices {
		if d.offline() {
			n++
		}
	}
	return n
}

// offline reports whether a device counts as down: every state except ONLINE
// and the update transitions UPDATING and GETTING_READY, which report
// attention instead.
func (d device) offline() bool {
	switch d.State {
	case "ONLINE", "UPDATING", "GETTING_READY":
		return false
	}
	return true
}

// updates counts the devices with firmware work: an available update, an
// update that runs, or a device that comes back after one.
func (sn snapshot) updates() int {
	n := 0
	for _, d := range sn.devices {
		switch d.State {
		case "UPDATING", "GETTING_READY":
			n++
		case "ONLINE":
			if d.FirmwareUpdatable {
				n++
			}
		}
	}
	return n
}

// statusLine names the most important fact: the first offline device, then the
// number of firmware updates, then the WAN summary "<ISP> - <latency> ms",
// then nothing. t picks the German or the English text for the language.
func (sn snapshot) statusLine(wanEnabled bool, t func(de, en string) string) string {
	for _, d := range sn.devices {
		if d.offline() {
			return d.Name + " " + t("offline", "offline")
		}
	}
	if n := sn.updates(); n > 0 {
		if n == 1 {
			return t("1 Firmware-Update", "1 firmware update")
		}
		return t(fmt.Sprintf("%d Firmware-Updates", n), fmt.Sprintf("%d firmware updates", n))
	}
	if wanEnabled && sn.wan.ISPName != "" && sn.www.Latency != nil {
		return fmt.Sprintf("%s - %d ms", sn.wan.ISPName, *sn.www.Latency)
	}
	return ""
}

// speed answers the result of the last speed test in Mbit/s. It is nil when no
// test ever ran, so the widget shows nothing instead of a zero. down picks the
// download rate, otherwise the upload rate.
func (sn snapshot) speed(down bool) *float64 {
	if sn.www.SpeedtestLastRun == nil || *sn.www.SpeedtestLastRun == 0 {
		return nil
	}
	if down {
		return sn.www.XputDown
	}
	return sn.www.XputUp
}

// signature covers what the widgets and the values show of the snapshot, so a
// refresh that changes nothing does not redraw the pages.
func (sn snapshot) signature() string {
	if !sn.loaded {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%d|%s|%s|%s|", sn.site.ID, sn.clients,
		sn.wan.Status, sn.wan.ISPName, sn.www.Status)
	if sn.www.Latency != nil {
		fmt.Fprintf(&b, "%d", *sn.www.Latency)
	}
	b.WriteByte('|')
	for _, d := range sn.devices {
		fmt.Fprintf(&b, "%s=%s=%t;", d.Name, d.State, d.FirmwareUpdatable)
	}
	return b.String()
}
