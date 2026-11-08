// Command pokedex is a Paperboy plugin (ADR 0049) that shows a Pokémon from
// PokéAPI (https://pokeapi.co, no API key): either a fixed one or a random
// one that changes every `rotate` interval. The widget "card" draws the
// artwork, name, number, types, size, base stats and a flavor text; it has a
// text variant for character displays.
//
// The artwork is downloaded at runtime and quantized to the display's gray
// levels. It is never stored on disk and never part of the repository.
//
//	plugins:
//	  pokedex:
//	    command: [pokedex]
//	    config: { mode: random, rotate: 24h, generations: [1, 2, 3] }
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/png" // PokéAPI artwork is PNG
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

func main() {
	plugin.Serve(newPokedex(&http.Client{Timeout: 20 * time.Second}))
}

var configKeys = map[string]plugin.Arg{
	"pokemon":     {Type: "string", Description: "name or national dex number of the Pokémon (mode fixed)"},
	"mode":        {Type: "string", Default: "fixed with pokemon, else random", Description: "fixed or random", Enum: []any{"fixed", "random"}},
	"rotate":      {Type: "duration", Default: "24h", Description: "how often a random Pokémon changes, at least 1h"},
	"generations": {Type: "list", Description: "only draw random Pokémon of these generations (1 to 9)", Enum: []any{1, 2, 3, 4, 5, 6, 7, 8, 9}},
	"base":        {Type: "string", Default: defaultBase, Description: "PokéAPI base URL"},
}

const defaultBase = "https://pokeapi.co/api/v2"

// generations are the national dex ranges [first, last] per generation.
var generations = [...][2]int{
	{1, 151}, {152, 251}, {252, 386}, {387, 493}, {494, 649},
	{650, 721}, {722, 809}, {810, 905}, {906, 1025},
}

// minGap is the pause between two requests to PokéAPI.
const minGap = 250 * time.Millisecond

// mon is everything the card shows about one Pokémon, in all languages the
// plugin supports.
type mon struct {
	id        int
	name      string
	height    int // decimetres
	weight    int // hectograms
	stats     [6]int
	types     []string
	typeNames map[string]map[string]string // type -> language -> name
	names     map[string]string            // language -> species name
	flavor    map[string]string            // language -> newest flavor text
	art       image.Image
}

// statOrder is the order of the base stats on the card.
var statOrder = [6]string{"hp", "attack", "defense", "special-attack", "special-defense", "speed"}

type pokedex struct {
	client *http.Client
	now    func() time.Time
	gap    time.Duration

	base       string
	fixed      string // name or number; empty = random
	rotate     time.Duration
	offset     int64 // seconds east of UTC, aligns rotation to local midnight
	candidates []int

	netMu    sync.Mutex // serializes requests to PokéAPI
	lastCall time.Time

	mu    sync.Mutex
	mons  map[string]*mon
	types map[string]map[string]string
	cur   *mon
	req   *plugin.Request
	lang  string
}

func newPokedex(c *http.Client) *pokedex {
	return &pokedex{client: c, now: time.Now, gap: minGap, mons: map[string]*mon{}, types: map[string]map[string]string{}}
}

func (p *pokedex) Describe() plugin.Description {
	both := []string{plugin.KindImage, plugin.KindText}
	return plugin.Description{
		Title:   "Pokédex",
		Version: version(),
		Config:  configKeys,
		Widgets: []plugin.WidgetInfo{
			{Widget: "card", Title: "Pokémon card", Kinds: both, MinWidth: 240, MinHeight: 240,
				Sizes: []plugin.Span{{Columns: 3, Rows: 4}, {Columns: 6, Rows: 4}, {Columns: 6, Rows: 8}}},
		},
	}
}

func (p *pokedex) Configure(cfg plugin.Values, req *plugin.Request) error {
	if err := cfg.Check(configKeys); err != nil {
		return err
	}
	base := strings.TrimRight(cfg.String("base", defaultBase), "/")
	if u, err := url.Parse(base); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("base: expected http(s)://host/path")
	}
	fixed := strings.ToLower(strings.TrimSpace(cfg.String("pokemon", "")))
	mode := cfg.String("mode", "")
	if mode == "" {
		mode = "random"
		if fixed != "" {
			mode = "fixed"
		}
	}
	switch mode {
	case "fixed":
		if fixed == "" {
			return errors.New("mode fixed needs pokemon")
		}
		if strings.ContainsAny(fixed, "/?# ") {
			return fmt.Errorf("pokemon %q: expected a name or a number", fixed)
		}
	case "random":
		fixed = ""
	default:
		return fmt.Errorf("mode %q: expected fixed or random", mode)
	}
	rotate, err := cfg.Duration("rotate", 24*time.Hour)
	if err != nil {
		return err
	}
	if rotate < time.Hour {
		return errors.New("rotate must be at least 1h")
	}
	gens, err := generationList(cfg["generations"])
	if err != nil {
		return err
	}
	var cands []int
	if len(gens) > 0 {
		for _, n := range gens {
			for id := generations[n-1][0]; id <= generations[n-1][1]; id++ {
				cands = append(cands, id)
			}
		}
	} else {
		for id := 1; id <= generations[len(generations)-1][1]; id++ {
			cands = append(cands, id)
		}
	}
	p.base, p.fixed, p.rotate, p.candidates = base, fixed, rotate, cands
	_, off := p.now().In(req.Location()).Zone()
	p.offset = int64(off)
	return nil
}

// slot numbers the rotation interval that contains t.
func (p *pokedex) slot(t time.Time) int64 {
	return (t.Unix() + p.offset) / int64(p.rotate.Seconds())
}

// nextRotation is the start of the interval after the one containing t.
func (p *pokedex) nextRotation(t time.Time) time.Time {
	return time.Unix((p.slot(t)+1)*int64(p.rotate.Seconds())-p.offset, 0)
}

// key names the Pokémon that is current at t: the configured one or the
// random pick of the rotation interval. The pick only depends on the interval
// number, so a restart keeps it.
func (p *pokedex) key(t time.Time) string {
	if p.fixed != "" {
		return p.fixed
	}
	r := rand.New(rand.NewPCG(uint64(p.slot(t)), 0x706f6b65))
	return strconv.Itoa(p.candidates[r.IntN(len(p.candidates))])
}

// Start loads the current Pokémon ahead of the first request and reports a
// change whenever a random Pokémon rotates.
func (p *pokedex) Start(ctx context.Context, notify func(widgets ...string)) {
	go func() {
		first := true
		for {
			t := p.now()
			if _, err := p.load(ctx, p.key(t)); err != nil {
				if ctx.Err() != nil {
					return
				}
				fmt.Fprintln(os.Stderr, "pokedex:", err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Minute):
				}
				continue
			}
			if !first {
				notify()
			}
			first = false
			if p.fixed != "" {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Until(p.nextRotation(t)) + time.Second):
			}
		}
	}()
}

func (p *pokedex) Prepare(req *plugin.Request) error {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	m, err := p.load(ctx, p.key(p.now()))
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.cur, p.req, p.lang = m, req, req.Lang()
	p.mu.Unlock()
	return nil
}

// MaxAge: the card only changes when the Pokémon rotates, which is also
// reported as an event.
func (p *pokedex) MaxAge(*plugin.Request) time.Duration { return time.Hour }

// get fetches one URL, at most one request at a time and with a pause
// between requests.
func (p *pokedex) get(ctx context.Context, u string) ([]byte, error) {
	p.netMu.Lock()
	defer p.netMu.Unlock()
	if wait := p.gap - time.Since(p.lastCall); wait > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	defer func() { p.lastCall = time.Now() }()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "paperboy-pokedex (+https://github.com/maehld/paperboy)")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, u)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

func (p *pokedex) getJSON(ctx context.Context, path string, v any) error {
	body, err := p.get(ctx, p.base+path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("invalid answer for %s: %w", path, err)
	}
	return nil
}

type apiNames []struct {
	Name     string `json:"name"`
	Language struct {
		Name string `json:"name"`
	} `json:"language"`
}

func (n apiNames) byLanguage() map[string]string {
	out := map[string]string{}
	for _, e := range n {
		out[e.Language.Name] = e.Name
	}
	return out
}

// load returns the Pokémon for a name or number, from the cache or PokéAPI.
// Everything it needs (species, type names, artwork) is cached with it, so a
// Pokémon costs a handful of requests once.
func (p *pokedex) load(ctx context.Context, key string) (*mon, error) {
	p.mu.Lock()
	m := p.mons[key]
	p.mu.Unlock()
	if m != nil {
		return m, nil
	}
	var pk struct {
		ID      int `json:"id"`
		Name    string
		Height  int
		Weight  int
		Species struct {
			Name string `json:"name"`
		} `json:"species"`
		Types []struct {
			Slot int `json:"slot"`
			Type struct {
				Name string `json:"name"`
			} `json:"type"`
		} `json:"types"`
		Stats []struct {
			Base int `json:"base_stat"`
			Stat struct {
				Name string `json:"name"`
			} `json:"stat"`
		} `json:"stats"`
		Sprites struct {
			Front string `json:"front_default"`
			Other struct {
				Artwork struct {
					Front string `json:"front_default"`
				} `json:"official-artwork"`
			} `json:"other"`
		} `json:"sprites"`
	}
	if err := p.getJSON(ctx, "/pokemon/"+url.PathEscape(key), &pk); err != nil {
		return nil, fmt.Errorf("pokemon %s: %w", key, err)
	}
	m = &mon{id: pk.ID, name: pk.Name, height: pk.Height, weight: pk.Weight, typeNames: map[string]map[string]string{}}
	for i, want := range statOrder {
		for _, s := range pk.Stats {
			if s.Stat.Name == want {
				m.stats[i] = s.Base
			}
		}
	}
	for _, t := range pk.Types { // API lists slot 1 first
		m.types = append(m.types, t.Type.Name)
	}
	species := pk.Species.Name
	if species == "" {
		species = strconv.Itoa(pk.ID)
	}
	var sp struct {
		Names   apiNames `json:"names"`
		Flavors []struct {
			Text     string `json:"flavor_text"`
			Language struct {
				Name string `json:"name"`
			} `json:"language"`
		} `json:"flavor_text_entries"`
	}
	if err := p.getJSON(ctx, "/pokemon-species/"+url.PathEscape(species), &sp); err != nil {
		return nil, fmt.Errorf("species %s: %w", species, err)
	}
	m.names = sp.Names.byLanguage()
	m.flavor = map[string]string{}
	for _, f := range sp.Flavors { // oldest first: the newest entry wins
		m.flavor[f.Language.Name] = cleanFlavor(f.Text)
	}
	for _, t := range m.types {
		names, err := p.typeNames(ctx, t)
		if err != nil {
			return nil, err
		}
		m.typeNames[t] = names
	}
	art := pk.Sprites.Other.Artwork.Front
	if art == "" {
		art = pk.Sprites.Front
	}
	if art != "" {
		body, err := p.get(ctx, art)
		if err == nil {
			m.art, _, err = image.Decode(strings.NewReader(string(body)))
		}
		if err != nil { // the card still works without a picture
			fmt.Fprintf(os.Stderr, "pokedex: artwork of %s: %v\n", pk.Name, err)
			m.art = nil
		}
	}
	p.mu.Lock()
	if len(p.mons) >= 8 { // only the current few are of interest
		p.mons = map[string]*mon{}
	}
	p.mons[key] = m
	p.mu.Unlock()
	return m, nil
}

func (p *pokedex) typeNames(ctx context.Context, name string) (map[string]string, error) {
	p.mu.Lock()
	names, ok := p.types[name]
	p.mu.Unlock()
	if ok {
		return names, nil
	}
	var t struct {
		Names apiNames `json:"names"`
	}
	if err := p.getJSON(ctx, "/type/"+url.PathEscape(name), &t); err != nil {
		return nil, fmt.Errorf("type %s: %w", name, err)
	}
	names = t.Names.byLanguage()
	p.mu.Lock()
	p.types[name] = names
	p.mu.Unlock()
	return names, nil
}

// cleanFlavor turns the hard-wrapped flavor text of the games into one line.
func cleanFlavor(s string) string {
	s = strings.NewReplacer("\u00ad\n", "", "\u00ad", "", "\n", " ", "\f", " ", "\r", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// title upper-cases the first letter of an API slug such as "mr-mime".
func title(s string) string {
	s = strings.ReplaceAll(s, "-", " ")
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 'a' - 'A'
	}
	return string(r)
}

func (m *mon) displayName(lang string) string {
	if n := m.names[lang]; n != "" {
		return n
	}
	if n := m.names["en"]; n != "" {
		return n
	}
	return title(m.name)
}

func (m *mon) typeName(t, lang string) string {
	if n := m.typeNames[t][lang]; n != "" {
		return n
	}
	if n := m.typeNames[t]["en"]; n != "" {
		return n
	}
	return title(t)
}

func (m *mon) flavorText(lang string) string {
	if f := m.flavor[lang]; f != "" {
		return f
	}
	return m.flavor["en"]
}

// generationList reads a list of generation numbers (or one number); YAML
// numbers arrive as float64.
func generationList(v any) ([]int, error) {
	var items []any
	switch x := v.(type) {
	case nil:
		return nil, nil
	case []any:
		items = x
	default:
		items = []any{x}
	}
	var out []int
	for _, it := range items {
		n := 0
		switch x := it.(type) {
		case float64:
			n = int(x)
		case int:
			n = x
		case string:
			n, _ = strconv.Atoi(strings.TrimSpace(x))
		}
		if n < 1 || n > len(generations) {
			return nil, fmt.Errorf("generations: %v is not a generation from 1 to %d", it, len(generations))
		}
		out = append(out, n)
	}
	return out, nil
}
