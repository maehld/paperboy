// Command armory is a Paperboy plugin (ADR 0049) that shows a World of
// Warcraft character. It reads Blizzard's profile API with an application
// token and Raider.IO without a key, and offers the widgets "character",
// "mythic", "raid" and "gear" plus values for text pages.
//
//	plugins:
//	  armory:
//	    command: [armory]
//	    config:
//	      region: eu
//	      characters: [Thrall@frostwolf]
//	      client_id: "..."
//	      client_secret: "..."
package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"strings"
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
)

func main() {
	plugin.Serve(&armory{})
}

var configKeys = map[string]plugin.Arg{
	"region":        {Type: "string", Default: "eu", Description: "the game region", Enum: []any{"us", "eu", "kr", "tw"}},
	"characters":    {Type: "list", Description: "the characters, as Name@realm or a bare name"},
	"client_id":     {Type: "string", Description: "Blizzard client id; without it only Raider.IO answers"},
	"client_secret": {Type: "string", Description: "Blizzard client secret"},
	"namespace":     {Type: "string", Description: "the profile namespace; default profile-<region>"},
	"base":          {Type: "string", Default: defaultBase, Description: "Raider.IO base URL"},
	"api_base":      {Type: "string", Description: "Blizzard API base URL; default the region's host"},
	"oauth_base":    {Type: "string", Default: defaultOAuthBase, Description: "Battle.net OAuth base URL"},
	"refresh":       {Type: "duration", Default: "30m", Description: "how long a character's data stays fresh"},
}

// characterArg is the argument every widget and value takes.
func (a *armory) characterArg() plugin.Arg {
	arg := plugin.Arg{Type: "string", Description: "the character, as configured"}
	if len(a.chars) > 0 {
		names := make([]any, len(a.chars))
		for i, c := range a.chars {
			names[i] = c.String()
		}
		arg.Enum = names
	}
	return arg
}

func (a *armory) Describe() plugin.Description {
	char := a.characterArg()
	args := map[string]plugin.Arg{"character": char}
	gear := map[string]plugin.Arg{
		"character": char,
		"count":     {Type: "int", Default: 10, Description: "how many slots to list"},
	}
	return plugin.Description{
		Title:   "World of Warcraft character",
		Version: version(),
		Config:  configKeys,
		Widgets: []plugin.WidgetInfo{
			{Widget: "character", Title: "Character", Kinds: []string{plugin.KindImage}, MinWidth: 200, MinHeight: 160,
				Sizes: []plugin.Span{{Columns: 3, Rows: 8}, {Columns: 4, Rows: 8}, {Columns: 6, Rows: 8}, {Columns: 2, Rows: 4}},
				Args:  args},
			{Widget: "mythic", Title: "Mythic+", Kinds: []string{plugin.KindImage}, MinWidth: 140, MinHeight: 110,
				Sizes: []plugin.Span{{Columns: 2, Rows: 4}, {Columns: 3, Rows: 4}, {Columns: 2, Rows: 8}, {Columns: 4, Rows: 4}},
				Args:  args},
			{Widget: "raid", Title: "Raid progress", Kinds: []string{plugin.KindImage}, MinWidth: 140, MinHeight: 120,
				Sizes: []plugin.Span{{Columns: 2, Rows: 4}, {Columns: 3, Rows: 4}, {Columns: 2, Rows: 8}},
				Args:  args},
			{Widget: "gear", Title: "Equipment", Kinds: []string{plugin.KindImage}, MinWidth: 200, MinHeight: 160,
				Sizes: []plugin.Span{{Columns: 6, Rows: 8}, {Columns: 4, Rows: 8}, {Columns: 3, Rows: 8}},
				Args:  gear},
		},
		Values: []plugin.ValueSpec{
			{Name: "name", Title: "Character name", Type: plugin.ValueText, Args: args},
			{Name: "level", Title: "Level", Type: plugin.ValueNumber, Args: args},
			{Name: "class", Title: "Class", Type: plugin.ValueText, Args: args},
			{Name: "spec", Title: "Specialization", Type: plugin.ValueText, Args: args},
			{Name: "guild", Title: "Guild", Type: plugin.ValueText, Args: args},
			{Name: "item-level", Title: "Item level", Type: plugin.ValueNumber, Args: args},
			{Name: "achievements", Title: "Achievement points", Type: plugin.ValueNumber, Args: args},
			{Name: "mythic-score", Title: "Mythic+ score", Type: plugin.ValueNumber, Args: args},
			{Name: "raid-progress", Title: "Raid progress", Type: plugin.ValueText, Args: args},
			{Name: "last-login", Title: "Last login", Type: plugin.ValueTimestamp, Args: args},
		},
	}
}

func (a *armory) Configure(cfg plugin.Values, req *plugin.Request) error {
	if err := cfg.Check(configKeys); err != nil {
		return err
	}
	a.region = cfg.String("region", "eu")
	a.base = cfg.String("base", defaultBase)
	a.apiBase = cfg.String("api_base", "")
	a.oauth = cfg.String("oauth_base", defaultOAuthBase)
	a.clientID = cfg.String("client_id", "")
	a.secret = cfg.String("client_secret", "")
	a.ns = cfg.String("namespace", "")
	a.lang = req.Language
	d, err := cfg.Duration("refresh", 30*time.Minute)
	if err != nil {
		return err
	}
	if d < time.Minute {
		return errors.New("refresh must be at least 1m")
	}
	a.refresh = d

	a.chars = nil
	for _, s := range cfg.Strings("characters") {
		n, err := parseName(s)
		if err != nil {
			return err
		}
		a.chars = append(a.chars, n)
	}
	return nil
}

// resolve reads the character argument: a configured entry or "Name@realm".
func (a *armory) resolve(v plugin.Values) (name, error) {
	s := strings.TrimSpace(v.String("character", ""))
	if s == "" {
		if len(a.chars) == 0 {
			return name{}, errors.New("character: no character named and none configured")
		}
		return a.chars[0], nil
	}
	for _, c := range a.chars {
		if strings.EqualFold(c.String(), s) || strings.EqualFold(c.char, s) {
			return c, nil
		}
	}
	return parseName(s)
}

// Prepare loads every character of the request before its widgets draw.
func (a *armory) Prepare(req *plugin.Request) error {
	if req.Language != "" {
		a.lang = req.Language
	}
	want := map[string]name{}
	for _, w := range req.Widgets {
		if n, err := a.resolve(w.Arg()); err == nil {
			want[n.key()] = n
		}
	}
	if len(req.Values) > 0 {
		if n, err := a.resolve(req.ValueArgs); err == nil {
			want[n.key()] = n
		}
	}
	if len(want) == 0 {
		return nil
	}
	a.mu.Lock()
	if a.cur == nil {
		a.cur = map[string]*character{}
	}
	a.mu.Unlock()
	for key, n := range want {
		c := a.load(context.Background(), n)
		a.mu.Lock()
		a.cur[key] = c
		a.mu.Unlock()
	}
	return nil
}

// character returns the loaded character of a widget or value.
func (a *armory) character(v plugin.Values) (*character, error) {
	n, err := a.resolve(v)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	c := a.cur[n.key()]
	a.mu.Unlock()
	if c == nil {
		c = a.load(context.Background(), n)
	}
	if c.err != nil {
		return nil, fmt.Errorf("%s: %w", n.String(), c.err)
	}
	return c, nil
}

// Render draws one widget.
func (a *armory) Render(req *plugin.Request, w plugin.WidgetRequest, canvas *image.Paletted) error {
	c, err := a.character(w.Arg())
	if err != nil {
		return err
	}
	area := canvas.Rect.Inset(w.Style.Padding)
	switch w.Widget {
	case "character":
		if !drawCharacter(canvas, area, req, w, c) {
			return plugin.TooSmall(w)
		}
	case "mythic":
		if !drawMythic(canvas, area, req, w, c) {
			return plugin.TooSmall(w)
		}
	case "raid":
		if !drawRaid(canvas, area, req, w, c) {
			return plugin.TooSmall(w)
		}
	case "gear":
		if !drawGear(canvas, area, req, w, c) {
			return plugin.TooSmall(w)
		}
	default:
		return fmt.Errorf("unknown widget %q", w.Widget)
	}
	return nil
}
func (a *armory) MaxAge(*plugin.Request) time.Duration { return a.refresh }

// Values answers the declared values of a text page (ADR 0111, 0120).
func (a *armory) Values(req *plugin.Request) (map[string]plugin.Value, error) {
	c, err := a.character(req.ValueArgs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]plugin.Value, len(req.Values))
	for _, key := range req.Values {
		switch key {
		case "name":
			out[key] = plugin.TextValue(c.display())
		case "level":
			out[key] = plugin.NumberValue(float64(c.level))
		case "class":
			out[key] = plugin.TextValue(c.class)
		case "spec":
			out[key] = plugin.TextValue(c.spec)
		case "guild":
			out[key] = plugin.TextValue(c.guild)
		case "item-level":
			out[key] = plugin.NumberValue(c.itemLevel)
		case "achievements":
			out[key] = plugin.NumberValue(float64(c.achievements))
		case "mythic-score":
			out[key] = plugin.NumberValue(c.score)
		case "raid-progress":
			out[key] = plugin.TextValue(c.raidSummary)
		case "last-login":
			out[key] = plugin.TimestampValue(c.lastLogin)
		default:
			return nil, fmt.Errorf("%w %q", plugin.ErrUnknownValue, key)
		}
	}
	return out, nil
}
