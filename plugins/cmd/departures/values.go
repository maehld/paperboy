package main

import (
	"fmt"
	"strings"

	"github.com/maehld/paperboy/sdk/plugin"
)

// Values answers the declared values of a text page (ADR 0111). The page picks
// the stop and the format with the value arguments; without them the values
// describe the first configured stop (ADR 0120). departures carries one line
// per departure.
func (p *departures) Values(req *plugin.Request) (map[string]plugin.Value, error) {
	if err := req.ValueArgs.Check(p.valueArgs()); err != nil {
		return nil, err
	}
	w := plugin.WidgetRequest{Widget: "departures", Args: req.ValueArgs}
	v, err := p.selected(w)
	if err != nil {
		return nil, err
	}
	mode, err := timeMode(w)
	if err != nil {
		return nil, err
	}
	locality, err := localityMode(w)
	if err != nil {
		return nil, err
	}
	order, err := localityOrder(w)
	if err != nil {
		return nil, err
	}
	out := make(map[string]plugin.Value, len(req.Values))
	for _, name := range req.Values {
		switch name {
		case "stop":
			out[name] = plugin.TextValue(v.name)
		case "departures":
			out[name] = plugin.TextValue(p.valueLines(v, mode, locality, order))
		case "next":
			if len(v.list) == 0 {
				out[name] = plugin.TextValue("–")
				continue
			}
			d := v.list[0]
			out[name] = plugin.TextValue(strings.TrimSpace(d.Line + " " + v.destination(d, locality, order)))
		case "next-at":
			if len(v.list) == 0 {
				out[name] = plugin.TextValue("–")
				continue
			}
			out[name] = plugin.TimestampValue(v.list[0].when())
		case "next-in":
			if len(v.list) == 0 {
				out[name] = plugin.TextValue("–")
				continue
			}
			out[name] = plugin.DurationValue(max(v.list[0].when().Sub(p.req.Now), 0))
		default:
			return nil, fmt.Errorf("%w %q", plugin.ErrUnknownValue, name)
		}
	}
	return out, nil
}

// valueLines writes one line per departure: line, destination and the time
// with the page's time mode.
func (p *departures) valueLines(v *stopView, mode, locality, order string) string {
	lines := make([]string, 0, len(v.list))
	for _, d := range v.list {
		right := p.timeIn(d, p.req.Now, mode, true)
		if dl := delayText(d); dl != "" && !d.Cancelled {
			right = dl + " " + right
		}
		lines = append(lines, strings.TrimSpace(d.Line+" "+v.destination(d, locality, order)+" "+right))
	}
	return strings.Join(lines, "\n")
}
