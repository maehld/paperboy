package main

// This file turns the feature list of the API into the snapshot the widgets
// draw. A feature a device does not have stays nil, and the widgets leave it
// out instead of inventing a value.

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// curve is the heating curve of one circuit: slope and shift, as the API
// reports them. The curve itself is computed.
type curve struct {
	Slope float64 `json:"slope"`
	Shift float64 `json:"shift"`
}

// circuit is the state of one heating circuit.
type circuit struct {
	Index   int      `json:"index"`
	Name    string   `json:"name,omitempty"`
	Room    *float64 `json:"room,omitempty"`
	Supply  *float64 `json:"supply,omitempty"`
	Target  *float64 `json:"target,omitempty"`
	Program string   `json:"program,omitempty"`
	Mode    string   `json:"mode,omitempty"`
	Curve   *curve   `json:"curve,omitempty"`
	TempMin *float64 `json:"temp_min,omitempty"`
	TempMax *float64 `json:"temp_max,omitempty"`
}

// dhw is the domestic hot water.
type dhw struct {
	Temp     *float64 `json:"temp,omitempty"`
	Target   *float64 `json:"target,omitempty"`
	Charging bool     `json:"charging,omitempty"`
	OneTime  bool     `json:"one_time_charge,omitempty"`
	Mode     string   `json:"mode,omitempty"`
}

// burner is a gas, oil or pellet burner.
type burner struct {
	Active     bool     `json:"active"`
	Modulation *float64 `json:"modulation,omitempty"`
	Hours      *float64 `json:"hours,omitempty"`
	Starts     *float64 `json:"starts,omitempty"`
}

// compressor is the compressor of a heat pump.
type compressor struct {
	Active bool     `json:"active"`
	Hours  *float64 `json:"hours,omitempty"`
	Starts *float64 `json:"starts,omitempty"`
}

// message is one entry of the device messages, an error or a notice.
type message struct {
	Code        string    `json:"code,omitempty"`
	Description string    `json:"description,omitempty"`
	Priority    string    `json:"priority,omitempty"`
	At          time.Time `json:"at,omitempty"`
}

// snapshot is the state of the heating system at one moment.
type snapshot struct {
	Updated    time.Time   `json:"updated,omitempty"` // newest feature timestamp
	Outside    *float64    `json:"outside,omitempty"`
	Boiler     *float64    `json:"boiler,omitempty"`
	Return     *float64    `json:"return,omitempty"`
	Circuits   []circuit   `json:"circuits,omitempty"`
	DHW        *dhw        `json:"dhw,omitempty"`
	Burner     *burner     `json:"burner,omitempty"`
	Compressor *compressor `json:"compressor,omitempty"`
	Errors     []message   `json:"errors,omitempty"`
}

// feature is one entry of the features answer.
type feature struct {
	Feature    string              `json:"feature"`
	DeviceID   string              `json:"deviceId"`
	Timestamp  string              `json:"timestamp"`
	IsReady    *bool               `json:"isReady"`
	Properties map[string]property `json:"properties"`
}

// property is one property of a feature. The value is decoded on demand,
// because it is a number, a string, a boolean or a list.
type property struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
	Unit  string          `json:"unit"`
}

func (p property) number() (float64, bool) {
	var n json.Number
	if json.Unmarshal(p.Value, &n) != nil {
		return 0, false
	}
	f, err := n.Float64()
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

func (p property) text() (string, bool) {
	var s string
	if json.Unmarshal(p.Value, &s) != nil {
		return "", false
	}
	return s, true
}

func (p property) boolean() (bool, bool) {
	var b bool
	if json.Unmarshal(p.Value, &b) != nil {
		return false, false
	}
	return b, true
}

// num returns a number property of the feature.
func (f feature) num(name string) *float64 {
	p, ok := f.Properties[name]
	if !ok {
		return nil
	}
	if v, ok := p.number(); ok {
		return &v
	}
	return nil
}

// text returns a text property of the feature.
func (f feature) text(name string) string {
	if p, ok := f.Properties[name]; ok {
		if v, ok := p.text(); ok {
			return v
		}
	}
	return ""
}

// active reports the "active" property; a missing one is false.
func (f feature) active() bool {
	if p, ok := f.Properties["active"]; ok {
		if v, ok := p.boolean(); ok {
			return v
		}
	}
	return false
}

// newest returns the feature's timestamp.
func (f feature) newest() time.Time {
	t, err := time.Parse(time.RFC3339, f.Timestamp)
	if err != nil {
		return time.Time{}
	}
	return t
}

// snapshotFrom builds the snapshot from the features of one device. A
// developer mode flag or a stale entry is skipped, because it is not data.
func snapshotFrom(features []feature, deviceID string) *snapshot {
	s := &snapshot{}
	byName := make(map[string]feature, len(features))
	for _, f := range features {
		if deviceID != "" && f.DeviceID != "" && f.DeviceID != deviceID {
			continue
		}
		if t := f.newest(); t.After(s.Updated) {
			s.Updated = t
		}
		if name, ok := strings.CutPrefix(f.Feature, "heating."); ok {
			byName[name] = f
		} else if f.Feature == "device.messages.errors.raw" {
			s.Errors = readMessages(f)
		}
	}

	get := func(name string) *feature {
		if f, ok := byName[name]; ok {
			return &f
		}
		return nil
	}
	temp := func(name string) *float64 {
		if f := get(name); f != nil {
			return f.num("value")
		}
		return nil
	}

	s.Outside = temp("sensors.temperature.outside")
	s.Boiler = temp("boiler.sensors.temperature.main")
	s.Return = temp("sensors.temperature.return")

	// The DHW sensor is named hotWaterStorage by older and dhwCylinder by
	// newer devices; the widgets show whichever exists.
	dhwTemp := temp("dhw.sensors.temperature.hotWaterStorage")
	if dhwTemp == nil {
		dhwTemp = temp("dhw.sensors.temperature.dhwCylinder")
	}
	dhwTarget := temp("dhw.temperature.main")
	if s.DHW = readDHW(get); s.DHW == nil && (dhwTemp != nil || dhwTarget != nil) {
		s.DHW = &dhw{}
	}
	if s.DHW != nil {
		s.DHW.Temp, s.DHW.Target = dhwTemp, dhwTarget
	}

	if f := get("burners.0"); f != nil {
		b := &burner{Active: f.active()}
		if m := get("burners.0.modulation"); m != nil {
			b.Modulation = m.num("value")
		}
		if st := get("burners.0.statistics"); st != nil {
			b.Hours, b.Starts = st.num("hours"), st.num("starts")
		}
		s.Burner = b
	}
	if f := get("compressors.0"); f != nil {
		c := &compressor{Active: f.active()}
		if st := get("compressors.0.statistics"); st != nil {
			c.Hours, c.Starts = st.num("hours"), st.num("starts")
		}
		s.Compressor = c
	}

	s.Circuits = readCircuits(byName)
	return s
}

// readDHW reads the charging state and the mode, nil when the device has no
// domestic hot water.
func readDHW(get func(string) *feature) *dhw {
	charging := get("dhw.charging")
	mode := get("dhw.operating.modes.active")
	oneTime := get("dhw.oneTimeCharge")
	if charging == nil && mode == nil && oneTime == nil {
		return nil
	}
	d := &dhw{}
	if charging != nil {
		d.Charging = charging.active()
	}
	if mode != nil {
		d.Mode = mode.text("value")
	}
	if oneTime != nil {
		d.OneTime = oneTime.active()
	}
	return d
}

// circuitIndex returns the index of a feature name such as
// "circuits.2.operating.programs.active".
func circuitIndex(name string) (int, bool) {
	rest, ok := strings.CutPrefix(name, "circuits.")
	if !ok {
		return 0, false
	}
	head, _, ok := strings.Cut(rest, ".")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(head)
	if err != nil {
		return 0, false
	}
	return n, true
}

// readCircuits collects one circuit per index that the device reports.
func readCircuits(byName map[string]feature) []circuit {
	indexes := map[int]bool{}
	for name := range byName {
		if n, ok := circuitIndex(name); ok {
			indexes[n] = true
		}
	}
	out := make([]circuit, 0, len(indexes))
	for n := range indexes {
		prefix := "circuits." + strconv.Itoa(n) + "."
		c := circuit{Index: n}
		if f, ok := byName[prefix+"name"]; ok {
			c.Name = f.text("name")
		}
		if f, ok := byName[prefix+"sensors.temperature.room"]; ok {
			c.Room = f.num("value")
		}
		if f, ok := byName[prefix+"sensors.temperature.supply"]; ok {
			c.Supply = f.num("value")
		}
		if f, ok := byName[prefix+"temperature"]; ok {
			c.Target = f.num("value")
		}
		if f, ok := byName[prefix+"operating.programs.active"]; ok {
			c.Program = f.text("value")
		}
		if f, ok := byName[prefix+"operating.modes.active"]; ok {
			c.Mode = f.text("value")
		}
		if f, ok := byName[prefix+"heating.curve"]; ok {
			slope, shift := f.num("slope"), f.num("shift")
			if slope != nil && shift != nil {
				c.Curve = &curve{Slope: *slope, Shift: *shift}
			}
		}
		if f, ok := byName[prefix+"temperature.levels"]; ok {
			c.TempMin, c.TempMax = f.num("min"), f.num("max")
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}

// readMessages reads the entries of the device messages.
func readMessages(f feature) []message {
	p, ok := f.Properties["entries"]
	if !ok {
		return nil
	}
	var entries []struct {
		Code        string `json:"errorCode"`
		Description string `json:"errorDescription"`
		Priority    string `json:"priority"`
		Timestamp   string `json:"timestamp"`
	}
	if json.Unmarshal(p.Value, &entries) != nil {
		return nil
	}
	out := make([]message, 0, len(entries))
	for _, e := range entries {
		m := message{Code: e.Code, Description: e.Description, Priority: e.Priority}
		if t, err := time.Parse(time.RFC3339, e.Timestamp); err == nil {
			m.At = t
		}
		out = append(out, m)
	}
	return out
}
