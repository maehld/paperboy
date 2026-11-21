package main

import (
	"fmt"
	"strings"

	"github.com/maehld/paperboy/sdk/plugin"
)

// Values answers the declared values of a text page (ADR 0111).
func (p *prusalink) Values(req *plugin.Request) (map[string]plugin.Value, error) {
	p.mu.Lock()
	snap := p.cur
	p.mu.Unlock()
	if !snap.loaded && snap.err != nil {
		return nil, snap.err
	}

	dash := plugin.TextValue("–")
	out := make(map[string]plugin.Value, len(req.Values))
	for _, name := range req.Values {
		switch name {
		case "state":
			key := strings.ToLower(snap.state())
			if !contains(valueKeys, key) {
				out[name] = plugin.Value{}
				continue
			}
			out[name] = plugin.EnumValue(key)
		case "progress":
			if v, ok := snap.progress(); ok {
				out[name] = plugin.NumberValue(v)
			} else {
				out[name] = dash
			}
		case "time-remaining":
			if d, ok := snap.remaining(); ok {
				out[name] = plugin.DurationValue(d)
			} else {
				out[name] = dash
			}
		case "time-printing":
			if d, ok := snap.printingTime(); ok {
				out[name] = plugin.DurationValue(d)
			} else {
				out[name] = dash
			}
		case "nozzle":
			if v := snap.status.Printer.TempNozzle; v != nil {
				out[name] = plugin.NumberValue(*v)
			} else {
				out[name] = dash
			}
		case "bed":
			if v := snap.status.Printer.TempBed; v != nil {
				out[name] = plugin.NumberValue(*v)
			} else {
				out[name] = dash
			}
		case "job":
			if jobName := snap.jobName(); jobName != "" {
				out[name] = plugin.TextValue(jobName)
			} else {
				out[name] = dash
			}
		default:
			return nil, fmt.Errorf("%w %q", plugin.ErrUnknownValue, name)
		}
	}
	return out, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
