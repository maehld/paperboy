package main

import (
	"fmt"

	"github.com/maehld/paperboy/sdk/plugin"
)

// Values answers the declared values of a text page from the last snapshot
// (ADR 0111). A value the device did not report becomes a dash.
func (v *vicare) Values(req *plugin.Request) (map[string]plugin.Value, error) {
	dash := plugin.TextValue("–")
	number := func(f *float64) plugin.Value {
		if f == nil {
			return dash
		}
		return plugin.NumberValue(*f)
	}
	out := make(map[string]plugin.Value, len(req.Values))
	for _, name := range req.Values {
		c := v.circuit(0)
		switch name {
		case "outside":
			out[name] = number(v.cur.Outside)
		case "room":
			if c == nil {
				out[name] = dash
				continue
			}
			out[name] = number(c.Room)
		case "flow":
			if c == nil {
				out[name] = dash
				continue
			}
			out[name] = number(c.Supply)
		case "flow_target":
			if c == nil {
				out[name] = dash
				continue
			}
			out[name] = number(c.Target)
		case "dhw":
			if v.cur.DHW == nil {
				out[name] = dash
				continue
			}
			out[name] = number(v.cur.DHW.Temp)
		case "dhw_target":
			if v.cur.DHW == nil {
				out[name] = dash
				continue
			}
			out[name] = number(v.cur.DHW.Target)
		case "burner":
			if s := v.burnerState(); s != "" {
				out[name] = plugin.TextValue(s)
				continue
			}
			out[name] = dash
		case "program":
			if c == nil || c.Program == "" {
				out[name] = dash
				continue
			}
			out[name] = plugin.TextValue(v.programName(c.Program))
		case "state":
			out[name] = plugin.EnumValue(v.stateKey())
		case "curve_slope":
			if c == nil || c.Curve == nil {
				out[name] = dash
				continue
			}
			out[name] = plugin.NumberValue(c.Curve.Slope)
		case "curve_shift":
			if c == nil || c.Curve == nil {
				out[name] = dash
				continue
			}
			out[name] = plugin.NumberValue(c.Curve.Shift)
		case "model":
			if v.curModel == "" {
				out[name] = dash
				continue
			}
			out[name] = plugin.TextValue(v.curModel)
		case "updated":
			if v.cur.Updated.IsZero() {
				out[name] = dash
				continue
			}
			out[name] = plugin.TimestampValue(v.cur.Updated)
		default:
			return nil, fmt.Errorf("%w %q", plugin.ErrUnknownValue, name)
		}
	}
	return out, nil
}
