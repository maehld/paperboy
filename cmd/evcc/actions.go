package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

// actionArgs is the schema of the "mode" action. Describe publishes it and
// Invoke checks against it, so a plan and the run agree on the arguments.
var actionArgs = map[string]plugin.Arg{
	"loadpoint": {Type: "int", Required: true, Description: "loadpoint, counted from 0"},
	"mode":      {Type: "string", Required: true, Description: "evcc charge mode", Enum: []any{"off", "minpv", "pv", "now"}},
}

// actionModeLabels names the modes in an action title. The widgets use the
// shorter labels of modes; an action reads like "Wohnmobil: jetzt laden".
var actionModeLabels = map[string][2]string{
	"off":   {"aus", "off"},
	"minpv": {"Min+PV", "Min+Solar"},
	"pv":    {"nur PV", "PV only"},
	"now":   {"jetzt laden", "charge now"},
}

func actionModeLabel(mode string, lang string) string {
	labels, ok := actionModeLabels[mode]
	if !ok {
		return mode
	}
	if lang == "de" {
		return labels[0]
	}
	return labels[1]
}

// loadpointName is the title of a loadpoint for an action. It prefers the
// loadpoint title because that stays put while a vehicle comes and goes.
func loadpointName(lp loadpoint, index int, lang string) string {
	switch {
	case lp.Title != "":
		return lp.Title
	case lp.VehicleTitle != "":
		return lp.VehicleTitle
	case lang == "de":
		return fmt.Sprintf("Ladepunkt %d", index)
	}
	return fmt.Sprintf("Loadpoint %d", index)
}

// snapshot returns the last state and the error of the last poll. The state
// stays usable for titles when a poll failed; the error still marks the
// actions as unavailable.
func (e *evcc) snapshot() (*state, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.data, e.loadErr
}

// Actions reports one state per plan action (ADR 0057). The entries stay
// visible as unavailable when evcc is down, the loadpoint does not exist or
// the mode is already active.
func (e *evcc) Actions(req *plugin.Request) ([]plugin.ActionState, error) {
	data, loadErr := e.snapshot()
	out := make([]plugin.ActionState, 0, len(req.Actions))
	for _, call := range req.Actions {
		out = append(out, actionState(req, call, data, loadErr))
	}
	return out, nil
}

func actionState(req *plugin.Request, call plugin.ActionCall, data *state, loadErr error) plugin.ActionState {
	var st plugin.ActionState
	if call.Action != "mode" {
		st.Disabled = fmt.Sprintf("unknown action %q", call.Action)
		return st
	}
	args := call.Arg()
	index, mode := args.Int("loadpoint", 0), args.String("mode", "")
	if data != nil && index >= 0 && index < len(data.Loadpoints) {
		st.Title = loadpointName(data.Loadpoints[index], index, req.Lang()) + ": " + actionModeLabel(mode, req.Lang())
	}
	if err := args.Check(actionArgs); err != nil {
		st.Disabled = err.Error()
		return st
	}
	switch {
	case loadErr != nil:
		st.Disabled = req.T("evcc nicht erreichbar", "evcc unreachable") + ": " + loadErr.Error()
	case data == nil:
		st.Disabled = req.T("evcc liefert noch keine Daten", "evcc has no data yet")
	case index < 0 || index >= len(data.Loadpoints):
		st.Disabled = fmt.Sprintf(req.T("Ladepunkt %d: evcc hat %d", "loadpoint %d: evcc has %d"),
			index, len(data.Loadpoints))
	case data.Loadpoints[index].Mode == mode:
		st.Disabled = fmt.Sprintf(req.T("Modus %s ist bereits aktiv", "mode %s is already active"),
			actionModeLabel(mode, req.Lang()))
	}
	return st
}

// Invoke switches one charge mode through evcc's REST API (ADR 0057). Actions
// set a target state, never a toggle, so a retried sync is safe.
func (e *evcc) Invoke(req *plugin.Request, call plugin.ActionCall) (plugin.ActionOutcome, error) {
	if call.Action != "mode" {
		return plugin.ActionOutcome{}, fmt.Errorf("unknown action %q", call.Action)
	}
	args := call.Arg()
	if err := args.Check(actionArgs); err != nil {
		return plugin.ActionOutcome{Message: err.Error()}, nil
	}
	index, mode := args.Int("loadpoint", 0), args.String("mode", "")
	message, ok := e.setMode(req, index, mode)
	out := plugin.ActionOutcome{OK: ok, Message: message}
	if !ok {
		return out, nil
	}
	out.Changed = true
	if data, _ := e.snapshot(); data != nil && index >= 0 && index < len(data.Loadpoints) {
		out.Message = loadpointName(data.Loadpoints[index], index, req.Lang()) + ": " + actionModeLabel(mode, req.Lang())
	}
	return out, nil
}

// setMode posts the new mode and reads the state again instead of waiting for
// the next poll, so the following render already shows the new mode.
func (e *evcc) setMode(req *plugin.Request, index int, mode string) (string, bool) {
	target := fmt.Sprintf("%s/api/loadpoints/%d/mode/%s", e.base, index, url.PathEscape(mode))
	httpReq, err := http.NewRequest(http.MethodPost, target, nil)
	if err != nil {
		return err.Error(), false
	}
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return req.T("evcc nicht erreichbar", "evcc unreachable") + ": " + err.Error(), false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		detail := strings.TrimSpace(string(body))
		if detail == "" {
			return fmt.Sprintf("HTTP %d", resp.StatusCode), false
		}
		return fmt.Sprintf("HTTP %d: %s", resp.StatusCode, detail), false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := e.fetch(ctx)
	e.mu.Lock()
	if err != nil {
		e.loadErr = err
	} else {
		e.data, e.at, e.loadErr = s, time.Now(), nil
	}
	e.mu.Unlock()
	return "", true
}
