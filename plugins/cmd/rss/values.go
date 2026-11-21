package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
)

// Values answers the declared values of a text page from the newest entries
// (ADR 0111). entries carries one line per entry.
func (p *rss) Values(req *plugin.Request) (map[string]plugin.Value, error) {
	if err := req.ValueArgs.Check(p.valueArgs()); err != nil {
		return nil, err
	}
	list, err := p.snap.entries(req.ValueArgs.String("feed", "all"))
	if err != nil {
		return nil, err
	}
	now := p.now(req)
	var first entry
	has := len(list) > 0
	if has {
		first = list[0]
	}
	out := make(map[string]plugin.Value, len(req.Values))
	for _, name := range req.Values {
		switch name {
		case "headline":
			if !has {
				out[name] = plugin.TextValue(req.T("Keine Meldungen", "No entries"))
				continue
			}
			out[name] = plugin.TextValue(first.Title)
		case "source":
			if !has {
				out[name] = plugin.TextValue("–")
				continue
			}
			out[name] = plugin.TextValue(first.Source)
		case "summary":
			if !has || first.Summary == "" {
				out[name] = plugin.TextValue("–")
				continue
			}
			out[name] = plugin.TextValue(first.Summary)
		case "published":
			if !has || first.Time.IsZero() {
				out[name] = plugin.TextValue("–")
				continue
			}
			out[name] = plugin.TimestampValue(first.Time)
		case "entries":
			out[name] = plugin.TextValue(p.entryLines(req, list, now))
		default:
			return nil, fmt.Errorf("%w %q", plugin.ErrUnknownValue, name)
		}
	}
	return out, nil
}

// entryLines writes one line per entry: the age, the feed name when several
// feeds are mixed, and the title.
func (p *rss) entryLines(req *plugin.Request, list []entry, now time.Time) string {
	source := len(p.feeds) > 1
	lines := make([]string, 0, len(list))
	for _, e := range list {
		line := e.Title
		if source && e.Source != "" {
			line = e.Source + ": " + line
		}
		if age := compactAge(e.Time, now, req); age != "" {
			line = age + " " + line
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
