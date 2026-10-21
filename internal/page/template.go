// Package page parses page templates and renders them into paletted images.
//
// A page is a skeleton of named regions with relative heights; every region
// holds one block (header, tiles, rows, chart or text). Values are read from
// the shared data document (ADR 0024), the region names are the slots that
// providers fill (ADR 0032).
package page

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// ErrNotFound reports a page template that does not exist for a display type.
var ErrNotFound = errors.New("page not found")

// Duration accepts duration strings such as "5m" in YAML.
type Duration time.Duration

func (d *Duration) UnmarshalYAML(v *yaml.Node) error {
	var s string
	if err := v.Decode(&s); err != nil {
		return fmt.Errorf("expected duration as string, e.g. \"5m\"")
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

// Template is one page for one display type.
type Template struct {
	Title  string  `yaml:"title"`
	Watch  []Watch `yaml:"watch"`
	Rows   []int   `yaml:"rows"`   // relative heights, one per block
	Blocks []Block `yaml:"blocks"` // one block per row
}

// Watch declares a data path the page depends on and when it counts as stale.
type Watch struct {
	Path       string   `yaml:"path"`
	StaleAfter Duration `yaml:"stale_after"`
	Label      string   `yaml:"label"`
}

// Block is the content of one region; exactly one field is set.
type Block struct {
	Name   string  `yaml:"name"`
	Header *Header `yaml:"header"`
	Tiles  []Tile  `yaml:"tiles"`
	Rows   []Tile  `yaml:"rows"`
	Chart  *Chart  `yaml:"chart"`
	Text   *string `yaml:"text"`
}

// Header shows a title, an optional subtitle and an optional time.
type Header struct {
	Title    string `yaml:"title"`    // text template
	Subtitle string `yaml:"subtitle"` // text template
	Time     string `yaml:"time"`     // path to an RFC 3339 timestamp
}

// Tile is one label/value pair, used by both tiles and rows.
type Tile struct {
	Label  string   `yaml:"label"`  // text template
	Value  string   `yaml:"value"`  // path into the data document
	Format string   `yaml:"format"` // power, energy, percent, temp, time, abs
	Sign   []string `yaml:"sign"`   // [negative, positive] suffix words
}

// Chart draws one or more series from a list of points.
type Chart struct {
	Points string   `yaml:"points"` // path to [{values: {...}}]
	Series []string `yaml:"series"` // keys inside "values"
	Unit   string   `yaml:"unit"`
}

var formats = map[string]bool{"": true, "power": true, "energy": true, "percent": true, "temp": true, "time": true, "abs": true}

// Parse reads a template and validates it.
func Parse(r io.Reader) (*Template, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	var t Template
	if err := dec.Decode(&t); err != nil {
		return nil, err
	}
	if err := t.validate(); err != nil {
		return nil, err
	}
	return &t, nil
}

// Load reads pages/<name>.<display>.yaml from dir. A missing file is reported
// as ErrNotFound, a broken one as a parse error.
func Load(dir, name, display string) (*Template, error) {
	if !ValidName(name) || !ValidName(display) {
		return nil, fmt.Errorf("%w: invalid page %q for display %q", ErrNotFound, name, display)
	}
	path := filepath.Join(dir, name+"."+display+".yaml")
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, path)
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	t, err := Parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return t, nil
}

// ValidName reports whether s can be used as a page, region or display name.
func ValidName(s string) bool {
	if s == "" || len(s) > 40 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

func (t *Template) validate() error {
	if len(t.Rows) == 0 {
		return fmt.Errorf("rows missing")
	}
	if len(t.Rows) != len(t.Blocks) {
		return fmt.Errorf("%d rows but %d blocks", len(t.Rows), len(t.Blocks))
	}
	if len(t.Rows) > 12 {
		return fmt.Errorf("%d rows: at most 12 are supported", len(t.Rows))
	}
	for i, w := range t.Rows {
		if w < 1 {
			return fmt.Errorf("row %d: weight must be positive", i)
		}
	}
	seen := map[string]bool{}
	for i := range t.Blocks {
		b := &t.Blocks[i]
		if b.Name == "" {
			b.Name = fmt.Sprintf("row%d", i)
		}
		if !ValidName(b.Name) {
			return fmt.Errorf("block name %q: only a-z, 0-9 and -", b.Name)
		}
		if seen[b.Name] {
			return fmt.Errorf("duplicate block name %q", b.Name)
		}
		seen[b.Name] = true
		if err := b.validate(); err != nil {
			return fmt.Errorf("block %s: %w", b.Name, err)
		}
	}
	for i := range t.Watch {
		w := &t.Watch[i]
		path, err := normalizePath(w.Path)
		if err != nil {
			return fmt.Errorf("watch: %w", err)
		}
		w.Path = path
		if w.StaleAfter <= 0 {
			return fmt.Errorf("watch %s: stale_after must be positive", path)
		}
	}
	return nil
}

func (b *Block) validate() error {
	kinds := 0
	for _, set := range []bool{b.Header != nil, len(b.Tiles) > 0, len(b.Rows) > 0, b.Chart != nil, b.Text != nil} {
		if set {
			kinds++
		}
	}
	if kinds != 1 {
		return fmt.Errorf("exactly one of header, tiles, rows, chart or text is required")
	}
	for _, tiles := range [][]Tile{b.Tiles, b.Rows} {
		for i := range tiles {
			if err := tiles[i].validate(); err != nil {
				return err
			}
		}
	}
	if b.Chart != nil {
		path, err := normalizePath(b.Chart.Points)
		if err != nil {
			return fmt.Errorf("chart: %w", err)
		}
		b.Chart.Points = path
		if len(b.Chart.Series) == 0 {
			return fmt.Errorf("chart: series missing")
		}
	}
	if b.Header != nil && b.Header.Time != "" {
		path, err := normalizePath(b.Header.Time)
		if err != nil {
			return fmt.Errorf("header: %w", err)
		}
		b.Header.Time = path
	}
	if b.Text != nil && strings.TrimSpace(*b.Text) == "" {
		return fmt.Errorf("text is empty")
	}
	return nil
}

func (t *Tile) validate() error {
	if !formats[t.Format] {
		return fmt.Errorf("unknown format %q", t.Format)
	}
	path, err := normalizePath(t.Value)
	if err != nil {
		return err
	}
	t.Value = path
	if len(t.Sign) != 0 && len(t.Sign) != 2 {
		return fmt.Errorf("sign needs two words (negative, positive)")
	}
	return nil
}

// normalizePath accepts "solar.current" and "solar/current" and returns the
// slash form used by the data document.
func normalizePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, ".", "/")
	p = strings.Trim(p, "/")
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	for _, seg := range strings.Split(p, "/") {
		if !dataSegment(seg) {
			return "", fmt.Errorf("invalid path segment %q", seg)
		}
	}
	return p, nil
}

func dataSegment(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
