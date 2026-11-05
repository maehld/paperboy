// Package page parses page templates and renders them into paletted images.
//
// A page is a skeleton of rows with relative heights; every row holds one block
// (header, tiles, rows, chart, text, columns or bar). Values are read from the
// shared data document (ADR 0024), the block names are the slots that providers
// fill (ADR 0032), and the optional style fields follow ADR 0034.
package page

import (
	"bytes"
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
	Title   string  `yaml:"title"`
	Watch   []Watch `yaml:"watch"`
	Notices string  `yaml:"notices"` // bar (default), badge or none
	Rows    []int   `yaml:"rows"`    // relative heights, one per block
	Blocks  []Block `yaml:"blocks"`  // one block per row

	pages string // pages directory the template was loaded from; icons live next to it
}

// Watch declares a data path the page depends on and when it counts as stale.
type Watch struct {
	Path       string   `yaml:"path"`
	StaleAfter Duration `yaml:"stale_after"`
	Label      string   `yaml:"label"`
}

// Block is the content of one region; exactly one content field is set.
type Block struct {
	Name    string   `yaml:"name"`
	Rule    int      `yaml:"rule"`    // separator at the bottom edge, px; 0 = none
	Padding *int     `yaml:"padding"` // inner distance to the region edges, px
	Header  *Header  `yaml:"header"`
	Tiles   *Tiles   `yaml:"tiles"`
	Rows    *Rows    `yaml:"rows"`
	Chart   *Chart   `yaml:"chart"`
	Text    *Text    `yaml:"text"`
	Columns *Columns `yaml:"columns"`
	Bar     *Bar     `yaml:"bar"`
	Icon    *Icon    `yaml:"icon"`
	Agenda  *Agenda  `yaml:"agenda"`
}

// Header shows a title, an optional subtitle and a status area.
type Header struct {
	Title          string        `yaml:"title"`    // text template
	Subtitle       string        `yaml:"subtitle"` // text template
	Time           string        `yaml:"time"`     // legacy shortcut for status.time
	Status         *HeaderStatus `yaml:"status"`
	Align          string        `yaml:"align"` // left, center (default) or right
	PageIndicator  bool          `yaml:"page_indicator"`
	Rule           int           `yaml:"rule"` // separator below the header, px; 0 = default 1
	Padding        *int          `yaml:"padding"`
	TitleSize      float64       `yaml:"title_size"`
	SubtitleSize   float64       `yaml:"subtitle_size"`
	StatusSize     float64       `yaml:"status_size"`
	TitleWeight    string        `yaml:"title_weight"`
	SubtitleWeight string        `yaml:"subtitle_weight"`
	StatusWeight   string        `yaml:"status_weight"`
	Shrink         *bool         `yaml:"shrink"`
}

// HeaderStatus is the right-aligned area of a header.
type HeaderStatus struct {
	Time  string `yaml:"time"`  // path to an RFC 3339 timestamp
	Label string `yaml:"label"` // text template, e.g. "Stand"
}

// Tiles lays out label/value pairs in a grid. In YAML the block also accepts
// the list shorthand: tiles: [{label: ..., value: ...}].
type Tiles struct {
	Columns int    `yaml:"columns"` // default 2
	Divider int    `yaml:"divider"` // separator between cells, px; 0 = none
	Padding *int   `yaml:"padding"`
	Align   string `yaml:"align"` // alignment inside a cell; default left
	Shrink  *bool  `yaml:"shrink"`
	Items   []Tile `yaml:"items"`
}

// Rows lays out label/value pairs as an evenly spaced table. In YAML the block
// also accepts the list shorthand: rows: [{label: ..., value: ...}].
type Rows struct {
	Rule    int    `yaml:"rule"` // separator between items, px; 0 = none
	Padding *int   `yaml:"padding"`
	Align   string `yaml:"align"` // alignment of the values; default right
	Shrink  *bool  `yaml:"shrink"`
	Items   []Tile `yaml:"items"`
}

// Text is a free text block. In YAML the block also accepts a scalar:
// text: "Hello".
type Text struct {
	Value   string  `yaml:"value"`
	Align   string  `yaml:"align"` // default center
	Size    float64 `yaml:"size"`  // fraction of the line height
	Weight  string  `yaml:"weight"`
	Shrink  *bool   `yaml:"shrink"`
	Padding *int    `yaml:"padding"`
}

// Icon draws one PNG pictogram from the instance's icons directory (ADR 0038).
// In YAML the block also accepts a scalar: icon: sun.png.
type Icon struct {
	Source      string  `yaml:"source"` // text template, relative to <instance>/icons
	Align       string  `yaml:"align"`  // left, center (default) or right
	Size        float64 `yaml:"size"`   // fraction of the region height, default 0.85
	Label       string  `yaml:"label"`  // optional caption below the icon, text template
	LabelSize   float64 `yaml:"label_size"`
	LabelWeight string  `yaml:"label_weight"`
	Padding     *int    `yaml:"padding"`
}

// Agenda lists upcoming events with day headings (ADR 0048). In YAML the block
// also accepts a scalar: agenda: calendar/events.
type Agenda struct {
	Events   string  `yaml:"events"`   // path to [{start, end, all_day, title, location}]
	Lines    int     `yaml:"lines"`    // number of text lines; default 6
	Headings *bool   `yaml:"headings"` // day headings; default true
	Location bool    `yaml:"location"` // append the location to the title
	Empty    string  `yaml:"empty"`    // text template for an empty list
	Size     float64 `yaml:"size"`     // fraction of the line height, default 0.6
	Rule     int     `yaml:"rule"`     // separator above each later day heading, px
	Padding  *int    `yaml:"padding"`
}

// Tile is one label/value pair, used by both tiles and rows.
type Tile struct {
	Label        string   `yaml:"label"`  // text template
	Detail       string   `yaml:"detail"` // optional third line, text template
	Value        string   `yaml:"value"`  // path into the data document
	Format       string   `yaml:"format"` // power, energy, percent, ratio, temp, time, abs, duration
	Sign         []string `yaml:"sign"`   // [negative, positive] suffix words
	Align        string   `yaml:"align"`
	LabelSize    float64  `yaml:"label_size"`
	ValueSize    float64  `yaml:"value_size"`
	DetailSize   float64  `yaml:"detail_size"`
	LabelWeight  string   `yaml:"label_weight"`
	ValueWeight  string   `yaml:"value_weight"`
	DetailWeight string   `yaml:"detail_weight"`
	Shrink       *bool    `yaml:"shrink"`
}

// Chart draws one or more series from a list of points.
type Chart struct {
	Points string   `yaml:"points"` // path to [{time: ..., values: {...}}]
	Title  string   `yaml:"title"`  // text template
	Unit   string   `yaml:"unit"`
	Time   bool     `yaml:"time"`  // x from point.time, local midnight to midnight
	YMax   YMax     `yaml:"y_max"` // auto (0) or a number
	Axis   bool     `yaml:"axis"`  // draw axes and labels
	Ticks  Duration `yaml:"ticks"` // time axis step; default 6h
	Now    bool     `yaml:"now"`   // mark the current time
	Legend bool     `yaml:"legend"`
	Series []Series `yaml:"series"`
}

// YMax accepts a number or the word "auto"; zero means auto.
type YMax float64

func (y *YMax) UnmarshalYAML(v *yaml.Node) error {
	if v.Kind == yaml.ScalarNode && v.Value == "auto" {
		*y = 0
		return nil
	}
	var n float64
	if err := v.Decode(&n); err != nil {
		return fmt.Errorf("y_max: expected a number or \"auto\"")
	}
	*y = YMax(n)
	return nil
}

// Series is one chart line. In YAML the list also accepts a plain key:
// series: [pv_power].
type Series struct {
	Key    string `yaml:"key"`
	Label  string `yaml:"label"`  // text template
	Style  string `yaml:"style"`  // line (default) or area
	Fill   string `yaml:"fill"`   // shade (default for areas), hatch, solid, none
	Weight int    `yaml:"weight"` // line thickness in px; default 1
}

// Columns splits a region into weighted columns with one block each.
type Columns struct {
	Divider int      `yaml:"divider"` // separator between columns, px
	Items   []Column `yaml:"items"`
}

// Column is one weighted column of a columns block.
type Column struct {
	Weight int   `yaml:"weight"`
	Block  Block `yaml:"block"`
}

// Bar draws a value as a filled bar or a battery symbol.
type Bar struct {
	Value       string  `yaml:"value"`
	Min         float64 `yaml:"min"`         // default 0
	Max         float64 `yaml:"max"`         // default 100
	Orientation string  `yaml:"orientation"` // vertical (default) or horizontal
	Border      int     `yaml:"border"`      // frame in px; 0 = none
	Cap         bool    `yaml:"cap"`         // battery nub, vertical only
	Fill        string  `yaml:"fill"`        // shade (default), hatch, solid, none
	Label       string  `yaml:"label"`       // text template
	LabelSize   float64 `yaml:"label_size"`
	LabelWeight string  `yaml:"label_weight"`
	Align       string  `yaml:"align"`
}

var (
	formats      = map[string]bool{"": true, "power": true, "energy": true, "percent": true, "ratio": true, "temp": true, "time": true, "abs": true, "duration": true}
	weights      = map[string]bool{"": true, "regular": true, "bold": true}
	aligns       = map[string]bool{"": true, "left": true, "center": true, "right": true}
	fills        = map[string]bool{"": true, "shade": true, "hatch": true, "solid": true, "none": true}
	styles       = map[string]bool{"": true, "line": true, "area": true}
	notices      = map[string]bool{"": true, "bar": true, "badge": true, "none": true}
	orientations = map[string]bool{"": true, "vertical": true, "horizontal": true}
)

// Parse reads a template and validates it. The list shorthands of ADR 0024 are
// normalized to the object forms before the strict decode.
func Parse(r io.Reader) (*Template, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return nil, fmt.Errorf("empty template")
	}
	if err := normalizeDoc(&doc); err != nil {
		return nil, err
	}
	normalized, err := yaml.Marshal(&doc)
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(normalized))
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
	t.pages = dir
	return t, nil
}

// iconsDir returns the icons directory of the instance. Pages and icons are
// siblings in the instance directory (ADR 0037, ADR 0038).
func (t *Template) iconsDir() string {
	if t.pages == "" {
		return "icons"
	}
	return filepath.Join(filepath.Dir(filepath.Clean(t.pages)), "icons")
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

// ---- shorthand normalization ----

func normalizeDoc(n *yaml.Node) error {
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	if n.Kind != yaml.MappingNode {
		return nil
	}
	blocks := mappingValue(n, "blocks")
	if blocks == nil || blocks.Kind != yaml.SequenceNode {
		return nil
	}
	for _, b := range blocks.Content {
		if err := normalizeBlock(b); err != nil {
			return err
		}
	}
	return nil
}

func normalizeBlock(b *yaml.Node) error {
	if b.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: block must be a mapping", b.Line)
	}
	for i := 0; i+1 < len(b.Content); i += 2 {
		key, val := b.Content[i], b.Content[i+1]
		switch key.Value {
		case "tiles", "rows":
			if val.Kind == yaml.SequenceNode {
				b.Content[i+1] = newMapping(newKey("items"), val)
			}
		case "text":
			if val.Kind == yaml.ScalarNode {
				b.Content[i+1] = newMapping(newKey("value"), val)
			}
		case "icon":
			if val.Kind == yaml.ScalarNode {
				b.Content[i+1] = newMapping(newKey("source"), val)
			}
		case "agenda":
			if val.Kind == yaml.ScalarNode {
				b.Content[i+1] = newMapping(newKey("events"), val)
			}
		case "chart":
			normalizeChart(val)
		case "columns":
			if err := normalizeColumns(val); err != nil {
				return err
			}
		}
	}
	return nil
}

func normalizeChart(c *yaml.Node) {
	if c == nil || c.Kind != yaml.MappingNode {
		return
	}
	series := mappingValue(c, "series")
	if series == nil || series.Kind != yaml.SequenceNode {
		return
	}
	for i, s := range series.Content {
		if s.Kind == yaml.ScalarNode {
			series.Content[i] = newMapping(newKey("key"), s)
		}
	}
}

func normalizeColumns(c *yaml.Node) error {
	var items *yaml.Node
	if c.Kind == yaml.SequenceNode {
		original := *c
		items = &original
		*c = *newMapping(newKey("items"), items)
	} else {
		items = mappingValue(c, "items")
	}
	if items == nil || items.Kind != yaml.SequenceNode {
		return nil
	}
	for _, col := range items.Content {
		if col.Kind != yaml.MappingNode {
			continue
		}
		if block := mappingValue(col, "block"); block != nil {
			if err := normalizeBlock(block); err != nil {
				return err
			}
		}
	}
	return nil
}

func mappingValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func newKey(name string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}
}

func newMapping(pairs ...*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: pairs}
}

// ---- validation ----

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
	if t.Notices == "" {
		t.Notices = "bar"
	}
	if !notices[t.Notices] {
		return fmt.Errorf("notices %q: expected bar, badge or none", t.Notices)
	}
	seen := map[string]bool{}
	hasStatus := false
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
		if b.Header != nil && (b.Header.Status != nil || b.Header.Time != "") {
			hasStatus = true
		}
	}
	if t.Notices == "badge" && !hasStatus {
		return fmt.Errorf("notices badge requires a header with status")
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
	return b.validateNested(false)
}

func (b *Block) validateNested(nested bool) error {
	if b.Rule < 0 {
		return fmt.Errorf("rule must not be negative")
	}
	if b.Padding != nil && *b.Padding < 0 {
		return fmt.Errorf("padding must not be negative")
	}
	kinds := 0
	for _, set := range []bool{b.Header != nil, b.Tiles != nil, b.Rows != nil, b.Chart != nil,
		b.Text != nil, b.Columns != nil, b.Bar != nil, b.Icon != nil, b.Agenda != nil} {
		if set {
			kinds++
		}
	}
	if kinds != 1 {
		return fmt.Errorf("exactly one of header, tiles, rows, chart, text, columns, bar, icon or agenda is required")
	}
	if nested && b.Columns != nil {
		return fmt.Errorf("nested columns are not supported")
	}
	if b.Header != nil {
		if err := b.Header.validate(); err != nil {
			return fmt.Errorf("header: %w", err)
		}
	}
	if b.Tiles != nil {
		if err := b.Tiles.validate(); err != nil {
			return fmt.Errorf("tiles: %w", err)
		}
	}
	if b.Rows != nil {
		if err := b.Rows.validate(); err != nil {
			return fmt.Errorf("rows: %w", err)
		}
	}
	if b.Chart != nil {
		if err := b.Chart.validate(); err != nil {
			return fmt.Errorf("chart: %w", err)
		}
	}
	if b.Text != nil {
		if err := b.Text.validate(); err != nil {
			return fmt.Errorf("text: %w", err)
		}
	}
	if b.Columns != nil {
		if err := b.Columns.validate(); err != nil {
			return fmt.Errorf("columns: %w", err)
		}
	}
	if b.Bar != nil {
		if err := b.Bar.validate(); err != nil {
			return fmt.Errorf("bar: %w", err)
		}
	}
	if b.Icon != nil {
		if err := b.Icon.validate(); err != nil {
			return fmt.Errorf("icon: %w", err)
		}
	}
	if b.Agenda != nil {
		if err := b.Agenda.validate(); err != nil {
			return fmt.Errorf("agenda: %w", err)
		}
	}
	return nil
}

func (a *Agenda) validate() error {
	path, err := normalizePath(a.Events)
	if err != nil {
		return fmt.Errorf("events: %w", err)
	}
	a.Events = path
	if a.Lines == 0 {
		a.Lines = 6
	}
	if a.Lines < 1 || a.Lines > 30 {
		return fmt.Errorf("lines %d: expected 1 to 30", a.Lines)
	}
	if err := validateSize(a.Size); err != nil {
		return fmt.Errorf("size: %w", err)
	}
	if a.Rule < 0 {
		return fmt.Errorf("rule must not be negative")
	}
	if a.Padding != nil && *a.Padding < 0 {
		return fmt.Errorf("padding must not be negative")
	}
	return nil
}

func (h *Header) validate() error {
	if err := validateStyle("header", h.Align, h.TitleWeight, h.TitleSize); err != nil {
		return err
	}
	if err := validateWeight(h.SubtitleWeight); err != nil {
		return fmt.Errorf("subtitle_weight: %w", err)
	}
	if err := validateWeight(h.StatusWeight); err != nil {
		return fmt.Errorf("status_weight: %w", err)
	}
	if err := validateSize(h.SubtitleSize); err != nil {
		return fmt.Errorf("subtitle_size: %w", err)
	}
	if err := validateSize(h.StatusSize); err != nil {
		return fmt.Errorf("status_size: %w", err)
	}
	if h.Rule < 0 {
		return fmt.Errorf("rule must not be negative")
	}
	if h.Padding != nil && *h.Padding < 0 {
		return fmt.Errorf("padding must not be negative")
	}
	if h.Status != nil {
		if h.Status.Time == "" && strings.TrimSpace(h.Status.Label) == "" {
			return fmt.Errorf("status needs time or label")
		}
		if h.Status.Time != "" {
			path, err := normalizePath(h.Status.Time)
			if err != nil {
				return fmt.Errorf("status.time: %w", err)
			}
			h.Status.Time = path
		}
	}
	if h.Time != "" {
		path, err := normalizePath(h.Time)
		if err != nil {
			return fmt.Errorf("time: %w", err)
		}
		h.Time = path
	}
	return nil
}

func (t *Tiles) validate() error {
	if len(t.Items) == 0 {
		return fmt.Errorf("items missing")
	}
	if t.Columns == 0 {
		t.Columns = 2
	}
	if t.Columns < 1 || t.Columns > 4 {
		return fmt.Errorf("columns %d: expected 1 to 4", t.Columns)
	}
	if t.Divider < 0 {
		return fmt.Errorf("divider must not be negative")
	}
	if t.Padding != nil && *t.Padding < 0 {
		return fmt.Errorf("padding must not be negative")
	}
	if err := validateAlign(t.Align); err != nil {
		return err
	}
	for i := range t.Items {
		if err := t.Items[i].validate(); err != nil {
			return fmt.Errorf("item %d: %w", i, err)
		}
	}
	return nil
}

func (rs *Rows) validate() error {
	if len(rs.Items) == 0 {
		return fmt.Errorf("items missing")
	}
	if rs.Rule < 0 {
		return fmt.Errorf("rule must not be negative")
	}
	if rs.Padding != nil && *rs.Padding < 0 {
		return fmt.Errorf("padding must not be negative")
	}
	if err := validateAlign(rs.Align); err != nil {
		return err
	}
	for i := range rs.Items {
		if err := rs.Items[i].validate(); err != nil {
			return fmt.Errorf("item %d: %w", i, err)
		}
	}
	return nil
}

func (tx *Text) validate() error {
	if strings.TrimSpace(tx.Value) == "" {
		return fmt.Errorf("value is empty")
	}
	if err := validateStyle("text", tx.Align, tx.Weight, tx.Size); err != nil {
		return err
	}
	if tx.Padding != nil && *tx.Padding < 0 {
		return fmt.Errorf("padding must not be negative")
	}
	return nil
}

func (t *Tile) validate() error {
	if !formats[t.Format] {
		return fmt.Errorf("unknown format %q", t.Format)
	}
	if t.Value != "" {
		path, err := normalizePath(t.Value)
		if err != nil {
			return err
		}
		t.Value = path
	}
	if len(t.Sign) != 0 && len(t.Sign) != 2 {
		return fmt.Errorf("sign needs two words (negative, positive)")
	}
	if err := validateAlign(t.Align); err != nil {
		return err
	}
	for name, w := range map[string]string{"label": t.LabelWeight, "value": t.ValueWeight, "detail": t.DetailWeight} {
		if err := validateWeight(w); err != nil {
			return fmt.Errorf("%s_weight: %w", name, err)
		}
	}
	for name, size := range map[string]float64{"label": t.LabelSize, "value": t.ValueSize, "detail": t.DetailSize} {
		if err := validateSize(size); err != nil {
			return fmt.Errorf("%s_size: %w", name, err)
		}
	}
	return nil
}

func (c *Chart) validate() error {
	if c.Points == "" {
		return fmt.Errorf("points missing")
	}
	path, err := normalizePath(c.Points)
	if err != nil {
		return err
	}
	c.Points = path
	if len(c.Series) == 0 {
		return fmt.Errorf("series missing")
	}
	if c.Ticks < 0 {
		return fmt.Errorf("ticks must not be negative")
	}
	if c.YMax < 0 {
		return fmt.Errorf("y_max must not be negative")
	}
	for i := range c.Series {
		s := &c.Series[i]
		if s.Key == "" {
			return fmt.Errorf("series %d: key missing", i)
		}
		if !styles[s.Style] {
			return fmt.Errorf("series %s: style %q: expected line or area", s.Key, s.Style)
		}
		if !fills[s.Fill] {
			return fmt.Errorf("series %s: fill %q: expected shade, hatch, solid or none", s.Key, s.Fill)
		}
		if s.Weight < 0 || s.Weight > 10 {
			return fmt.Errorf("series %s: weight must be between 0 and 10", s.Key)
		}
	}
	return nil
}

func (c *Columns) validate() error {
	if len(c.Items) < 2 {
		return fmt.Errorf("at least two columns are required")
	}
	if c.Divider < 0 {
		return fmt.Errorf("divider must not be negative")
	}
	for i := range c.Items {
		col := &c.Items[i]
		if col.Weight < 1 {
			return fmt.Errorf("column %d: weight must be positive", i)
		}
		if err := col.Block.validateNested(true); err != nil {
			return fmt.Errorf("column %d: %w", i, err)
		}
	}
	return nil
}

func (b *Bar) validate() error {
	if b.Value == "" {
		return fmt.Errorf("value missing")
	}
	path, err := normalizePath(b.Value)
	if err != nil {
		return err
	}
	b.Value = path
	if b.Max == 0 {
		b.Max = 100
	}
	if b.Max <= b.Min {
		return fmt.Errorf("max must be above min")
	}
	if !orientations[b.Orientation] {
		return fmt.Errorf("orientation %q: expected vertical or horizontal", b.Orientation)
	}
	if b.Border < 0 {
		return fmt.Errorf("border must not be negative")
	}
	if !fills[b.Fill] {
		return fmt.Errorf("fill %q: expected shade, hatch, solid or none", b.Fill)
	}
	if err := validateAlign(b.Align); err != nil {
		return err
	}
	if err := validateWeight(b.LabelWeight); err != nil {
		return fmt.Errorf("label_weight: %w", err)
	}
	if err := validateSize(b.LabelSize); err != nil {
		return fmt.Errorf("label_size: %w", err)
	}
	return nil
}

func validateStyle(owner, align, weight string, size float64) error {
	if err := validateAlign(align); err != nil {
		return err
	}
	if err := validateWeight(weight); err != nil {
		return err
	}
	if err := validateSize(size); err != nil {
		return err
	}
	return nil
}

func validateAlign(a string) error {
	if !aligns[a] {
		return fmt.Errorf("align %q: expected left, center or right", a)
	}
	return nil
}

func validateWeight(w string) error {
	if !weights[w] {
		return fmt.Errorf("weight %q: expected regular or bold", w)
	}
	return nil
}

func validateSize(size float64) error {
	if size < 0 || size > 1.5 {
		return fmt.Errorf("size %g: expected a fraction between 0 and 1.5", size)
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
