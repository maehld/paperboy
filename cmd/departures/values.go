package main

import (
	"fmt"
	"strings"

	"github.com/maehld/paperboy/pkg/plugin"
)

// Values answers the declared values of a text page for the first configured
// stop (ADR 0111). departures carries one line per departure.
func (p *departures) Values(req *plugin.Request) (map[string]plugin.Value, error) {
	v, err := p.selected(plugin.WidgetRequest{Widget: "departures"})
	if err != nil {
		return nil, err
	}
	out := make(map[string]plugin.Value, len(req.Values))
	for _, name := range req.Values {
		switch name {
		case "stop":
			out[name] = plugin.TextValue(v.name)
		case "departures":
			out[name] = plugin.TextValue(p.valueLines(v))
		case "next":
			if len(v.list) == 0 {
				out[name] = plugin.TextValue("–")
				continue
			}
			d := v.list[0]
			out[name] = plugin.TextValue(strings.TrimSpace(d.Line + " " + v.destination(d, "auto", "after")))
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

// valueLines writes one line per departure: line, destination, optional
// delay and the minutes until it leaves.
func (p *departures) valueLines(v *stopView) string {
	lines := make([]string, 0, len(v.list))
	for _, d := range v.list {
		right := p.timeIn(d, p.req.Now, "auto", true)
		if dl := delayText(d); dl != "" && !d.Cancelled {
			right = dl + " " + right
		}
		lines = append(lines, strings.TrimSpace(d.Line+" "+v.destination(d, "auto", "after")+" "+right))
	}
	return strings.Join(lines, "\n")
}
