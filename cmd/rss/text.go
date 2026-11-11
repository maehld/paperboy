package main

import (
	"fmt"
	"strings"

	"github.com/maehld/paperboy/pkg/plugin"
)

// Text fills a character display: one line per entry for the feed list, the
// headline with its source line and wrapped title.
func (p *rss) Text(req *plugin.Request, w plugin.WidgetRequest) ([]string, error) {
	switch w.Widget {
	case "feed":
		return p.textFeed(req, w)
	case "headline":
		return p.textHeadline(req, w)
	}
	return nil, fmt.Errorf("%w %q for text displays", plugin.ErrUnknownWidget, w.Widget)
}

func (p *rss) textFeed(req *plugin.Request, w plugin.WidgetRequest) ([]string, error) {
	name := w.Arg().String("feed", "all")
	list, err := p.snap.entries(name)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return []string{req.T("Keine Meldungen", "No entries")}, nil
	}
	mode, err := timeMode(w)
	if err != nil {
		return nil, err
	}
	if limit := w.Arg().Int("items", 0); limit > 0 && limit < len(list) {
		list = list[:limit]
	}
	if len(list) > w.Height {
		list = list[:max(w.Height, 1)]
	}
	now := p.now(req)
	source := p.sourcesOn(w, name)
	lines := make([]string, 0, len(list))
	for _, e := range list {
		title := e.Title
		if source && e.Source != "" {
			title = e.Source + ": " + title
		}
		var age string
		switch mode {
		case "none":
			lines = append(lines, fitText(title, w.Width))
			continue
		case "clock":
			age = e.Time.In(req.Location()).Format("15:04")
		default:
			age = compactAge(e.Time, now, req)
		}
		room := w.Width - len([]rune(age)) - 1
		if room < 1 {
			room = w.Width
		}
		lines = append(lines, plugin.Columns(age, fitText(title, room), w.Width))
	}
	return lines, nil
}

func (p *rss) textHeadline(req *plugin.Request, w plugin.WidgetRequest) ([]string, error) {
	name := w.Arg().String("feed", "all")
	list, err := p.snap.entries(name)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return []string{req.T("Keine Meldungen", "No entries")}, nil
	}
	if w.Height < 1 {
		return nil, nil
	}
	e := list[0]
	mode, err := timeMode(w)
	if err != nil {
		return nil, err
	}
	first := strings.ToUpper(e.Source)
	if mode != "none" {
		if age := compactAge(e.Time, p.now(req), req); age != "" {
			first += " " + age
		}
	}
	lines := []string{fitText(first, w.Width)}
	lines = append(lines, wrapText(e.Title, w.Width)...)
	if w.Arg().Bool("summary", true) && len(lines) < w.Height && e.Summary != "" {
		lines = append(lines, fitText(e.Summary, w.Width))
	}
	if len(lines) > w.Height {
		lines = lines[:w.Height]
	}
	return lines, nil
}

// fitText cuts s to width characters, with an ellipsis when it does not fit.
func fitText(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width < 2 {
		return string(r[:max(width, 0)])
	}
	return strings.TrimRight(string(r[:width-1]), " ") + "…"
}

// wrapText breaks s at spaces into lines of at most width characters.
func wrapText(s string, width int) []string {
	if width < 1 {
		return nil
	}
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		for len([]rune(word)) > width {
			if line != "" {
				lines, line = append(lines, line), ""
			}
			r := []rune(word)
			lines = append(lines, string(r[:width]))
			word = string(r[width:])
		}
		switch {
		case line == "":
			line = word
		case len([]rune(line))+1+len([]rune(word)) <= width:
			line += " " + word
		default:
			lines, line = append(lines, line), word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
