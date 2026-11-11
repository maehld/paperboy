package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

// chargingActionArgs is the schema of the "charging" action. Describe publishes
// it and Invoke checks against it, so a plan and the run agree on the arguments.
var chargingActionArgs = map[string]plugin.Arg{
	"state": {Type: "string", Required: true, Description: "start or stop charging", Enum: []any{"start", "stop"}},
}

// climateActionArgs is the schema of the "climate" action. temperature is
// optional and defaults to the car's current target.
var climateActionArgs = map[string]plugin.Arg{
	"state":       {Type: "string", Required: true, Description: "start or stop air conditioning", Enum: []any{"start", "stop"}},
	"temperature": {Type: "float", Description: "target temperature in °C, 16 to 30 in 0.5 steps; default the current target"},
}

// defaultTemperature is used when the car reports no target and the plan gives
// none either.
const defaultTemperature = 22.0

// actionTitles names the actions in a menu. The device menu shows them as is;
// the vehicle is unique, so no vehicle name goes in front.
func actionTitle(req *plugin.Request, action, state string) string {
	switch action {
	case "charging":
		if state == "stop" {
			return req.T("Laden stoppen", "Stop charging")
		}
		return req.T("Laden starten", "Start charging")
	case "climate":
		if state == "stop" {
			return req.T("Klima stoppen", "Stop air conditioning")
		}
		return req.T("Klima starten", "Start air conditioning")
	}
	return action
}

// lastState returns the last state and the error of the last poll. The state
// stays usable for titles when a poll failed; the error still marks the
// actions as unavailable.
func (m *myskoda) lastState() (*vehicle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data, m.loadErr
}

// Actions reports one state per plan action (ADR 0057). The entries stay
// visible as unavailable when there is no data, the vehicle does not support
// the operation or the target state already holds.
func (m *myskoda) Actions(req *plugin.Request) ([]plugin.ActionState, error) {
	data, loadErr := m.lastState()
	out := make([]plugin.ActionState, 0, len(req.Actions))
	for _, call := range req.Actions {
		out = append(out, m.actionState(req, call, data, loadErr))
	}
	return out, nil
}

func (m *myskoda) actionState(req *plugin.Request, call plugin.ActionCall, data *vehicle, loadErr error) plugin.ActionState {
	var st plugin.ActionState
	switch call.Action {
	case "charging", "climate":
	case "":
		st.Disabled = "action missing"
		return st
	default:
		st.Disabled = fmt.Sprintf("unknown action %q", call.Action)
		return st
	}
	args := call.Arg()
	state := args.String("state", "")
	if state == "start" || state == "stop" {
		st.Title = actionTitle(req, call.Action, state)
	}
	schema := chargingActionArgs
	if call.Action == "climate" {
		schema = climateActionArgs
	}
	if err := args.Check(schema); err != nil {
		st.Disabled = err.Error()
		return st
	}

	// A temperature outside the range is a planning error, not a car state.
	if call.Action == "climate" {
		if _, set := args["temperature"]; set {
			t := args.Float("temperature", defaultTemperature)
			if msg := checkTemperature(t); msg != "" {
				st.Disabled = msg
				return st
			}
		}
	}

	switch {
	case data == nil && loadErr != nil:
		st.Disabled = loadErr.Error()
	case data == nil:
		st.Disabled = req.T("Fahrzeugdaten werden noch geladen", "vehicle data is still loading")
	case !data.hasOperation(operationName(call.Action, state)):
		st.Disabled = req.T("Das Fahrzeug unterstützt diesen Befehl nicht",
			"the vehicle does not support this operation")
	case call.Action == "charging":
		if reason := chargingDisabled(req, data, state); reason != "" {
			st.Disabled = reason
		}
	case call.Action == "climate":
		if reason := climateDisabled(req, data, state); reason != "" {
			st.Disabled = reason
		}
	}
	return st
}

// operationName maps an action and state to the name the API lists in
// "operations".
func operationName(action, state string) string {
	verb := "start"
	if state == "stop" {
		verb = "stop"
	}
	switch action {
	case "charging":
		return verb + "Charging"
	case "climate":
		return verb + "AirConditioning"
	}
	return ""
}

// chargingDisabled explains why a charging command cannot run now; an empty
// string means it can.
func chargingDisabled(req *plugin.Request, v *vehicle, state string) string {
	s := v.chargeStateValue()
	switch state {
	case "start":
		switch s {
		case "CHARGING":
			return req.T("Lädt bereits", "already charging")
		case "CONNECT_CABLE":
			return req.T("Kein Ladekabel angeschlossen", "charging cable is not connected")
		}
	case "stop":
		if s != "" && s != "CHARGING" {
			return req.T("Lädt nicht", "not charging")
		}
	}
	return ""
}

// climateDisabled explains why an air-conditioning command cannot run now.
func climateDisabled(req *plugin.Request, v *vehicle, state string) string {
	active := v.climateActive()
	switch state {
	case "start":
		if active {
			return req.T("Klimatisierung läuft bereits", "air conditioning is already running")
		}
	case "stop":
		if !active {
			return req.T("Klimatisierung ist aus", "air conditioning is off")
		}
	}
	return ""
}

// checkTemperature rejects a target outside the range the API accepts.
func checkTemperature(t float64) string {
	if t < 16 || t > 30 {
		return fmt.Sprintf("temperature: %.1f is outside 16 to 30 °C", t)
	}
	return ""
}

// Invoke runs one charging or air-conditioning command (ADR 0057). Actions set
// a target state, never a toggle, so a retried sync is safe.
func (m *myskoda) Invoke(req *plugin.Request, call plugin.ActionCall) (plugin.ActionOutcome, error) {
	var schema map[string]plugin.Arg
	var endpoint string
	switch call.Action {
	case "charging":
		schema, endpoint = chargingActionArgs, "charging"
	case "climate":
		schema, endpoint = climateActionArgs, "air-conditioning"
	default:
		return plugin.ActionOutcome{}, fmt.Errorf("unknown action %q", call.Action)
	}
	args := call.Arg()
	if err := args.Check(schema); err != nil {
		return plugin.ActionOutcome{Message: err.Error()}, nil
	}
	state := args.String("state", "")

	var body any
	if call.Action == "climate" && state == "start" {
		temp, msg := m.climateTemperature(req, args)
		if msg != "" {
			return plugin.ActionOutcome{Message: msg}, nil
		}
		body = map[string]any{"targetTemperature": map[string]any{
			"value": temp,
			"unit":  "CELSIUS",
		}}
	}

	message, ok := m.command(req, endpoint, state, body)
	out := plugin.ActionOutcome{OK: ok, Message: message}
	if !ok {
		return out, nil
	}
	out.Changed = true
	out.Message = actionTitle(req, call.Action, state)
	return out, nil
}

// climateTemperature picks the start temperature: the plan's value, else the
// car's current target, else a sensible default. The API wants °C, so a target
// reported in Fahrenheit is converted.
func (m *myskoda) climateTemperature(req *plugin.Request, args plugin.Values) (float64, string) {
	if _, set := args["temperature"]; set {
		t := args.Float("temperature", defaultTemperature)
		if msg := checkTemperature(t); msg != "" {
			return 0, msg
		}
		return roundHalf(t), ""
	}
	if data, _ := m.lastState(); data != nil && data.AirConditioning != nil {
		if t := data.AirConditioning.Target; t.Value != nil {
			v := *t.Value
			if t.Unit != nil && *t.Unit == "FAHRENHEIT" {
				v = (v - 32) * 5 / 9
			}
			if checkTemperature(v) == "" {
				return roundHalf(v), ""
			}
		}
	}
	return defaultTemperature, ""
}

// roundHalf rounds to the nearest 0.5 °C, the step the API accepts.
func roundHalf(t float64) float64 {
	return math.Round(t*2) / 2
}

// command posts one remote command and reloads the state afterwards, so the
// following render already shows the new one. The API answers a command with
// 202 and no body.
func (m *myskoda) command(req *plugin.Request, endpoint, state string, body any) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	u := m.base + "/api/v1/vehicles/" + url.PathEscape(m.vin) + "/" + endpoint + "/" + url.PathEscape(state)
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err.Error(), false
		}
		reader = strings.NewReader(string(data))
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u, reader)
	if err != nil {
		return err.Error(), false
	}
	httpReq.Header.Set("X-API-Key", m.apiKey)
	httpReq.Header.Set("Accept", "application/json")
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	resp, err := m.client.Do(httpReq)
	if err != nil {
		return req.T("Fahrzeug nicht erreichbar", "vehicle unreachable") + ": " + err.Error(), false
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return problem(resp.StatusCode, answer, m.vin).Error(), false
	}

	// Reload at once instead of waiting for the next poll. A failed reload
	// does not fail the command, which the car already accepted.
	if ans, err := m.fetch(ctx); err == nil && ans.vehicle != nil {
		m.mu.Lock()
		m.data, m.loadErr, m.fetched = ans.vehicle, nil, time.Now()
		if !ans.expires.IsZero() {
			m.expires = ans.expires
		}
		wait := m.refresh
		if ans.wait > wait {
			wait = ans.wait
		}
		m.next = time.Now().Add(wait)
		snapshot := m.snapshot()
		m.pending = &snapshot
		m.mu.Unlock()
	}
	return "", true
}

// chargeStateValue returns charging.status.state, or "" when it is unknown.
func (v *vehicle) chargeStateValue() string {
	if v == nil || v.Charging == nil || v.Charging.Status == nil || v.Charging.Status.State == nil {
		return ""
	}
	return *v.Charging.Status.State
}

// climateActive reports whether the air conditioning is currently running.
func (v *vehicle) climateActive() bool {
	if v == nil || v.AirConditioning == nil || v.AirConditioning.State == nil {
		return false
	}
	switch *v.AirConditioning.State {
	case "COOLING", "HEATING", "HEATING_AUXILIARY", "VENTILATION":
		return true
	}
	return false
}

// hasOperation reports whether the vehicle lists the operation. An answer
// without a list cannot tell, so it does not block the action.
func (v *vehicle) hasOperation(name string) bool {
	if v == nil || len(v.Operations) == 0 {
		return true
	}
	for _, o := range v.Operations {
		if o.Name == name {
			return true
		}
	}
	return false
}
