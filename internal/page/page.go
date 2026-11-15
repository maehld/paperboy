// Package page parses page templates and composes them from widgets
// (ADR 0049).
//
// A page is a grid of cells. Every cell shows one widget of a plugin, e.g.
// "calendar/agenda". On an image display the grid divides the pixels; on a
// text display the grid is the character grid of the display, e.g. 20x4.
// Paperboy asks the plugins for every widget in exactly the size of its cell
// and pastes the results; it never scales them.
package page

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ErrNotFound reports a page template that does not exist for a display type.
var ErrNotFound = errors.New("page not found")

// Template is one page. The JSON tags carry the same model to the editor API
// (ADR 0066).
type Template struct {
	Title string `yaml:"title" json:"title"`
	Kind  string `yaml:"kind" json:"kind"` // image or text: the displays the page serves
	// Variants maps a display type to the label of the file that serves it:
	// "sticky: hochkant" means pages/<name>.hochkant.yaml (ADR 0110). Only a
	// page file carries variants; a variant carries the layout.
	Variants map[string]string `yaml:"variants" json:"variants,omitempty"`
	Grid     [2]int            `yaml:"grid" json:"grid"`                 // columns, rows; image pages only
	Style    Style             `yaml:"style" json:"style"`               // look passed to every widget
	Notices  string            `yaml:"notices" json:"notices,omitempty"` // show (default) or none: stale widgets
	Cells    []Cell            `yaml:"cells" json:"cells,omitempty"`     // image pages only
	// Rows are the lines of a text page; each row is a list of cells with a
	// value, a width and an alignment (ADR 0111). Text pages have no grid.
	Rows [][]TextCell `yaml:"rows" json:"rows,omitempty"`
	// Actions belong to the plan, not to a page. The field only exists to
	// reject the key with a hint instead of "field not found".
	Actions []any `yaml:"actions" json:"-"`
}

// Style is the page's look in points (1/72 inch, ADR 0050); Paperboy
// converts it with the display's PPI. Text displays ignore it.
type Style struct {
	Padding  *float64 `yaml:"padding" json:"padding,omitempty"`     // inner margin of a widget, pt; default 4.5 (1.6 mm)
	Rule     float64  `yaml:"rule" json:"rule,omitempty"`           // separator width a widget draws, pt; default 0.6 (0.2 mm), at least 1 px
	TextSize float64  `yaml:"text_size" json:"text_size,omitempty"` // base text height, pt; default 16, capped at 0.12 of the display height
	Divider  float64  `yaml:"divider" json:"divider,omitempty"`     // lines Paperboy draws between cells, pt; default 0 (none), at least 1 px
}

// Cell places one widget on the grid.
type Cell struct {
	At     [2]int         `yaml:"at" json:"at"`                 // column, row of the top-left cell
	Span   [2]int         `yaml:"span" json:"span"`             // columns, rows; default 1x1
	Widget string         `yaml:"widget" json:"widget"`         // plugin/widget
	Args   map[string]any `yaml:"args" json:"args,omitempty"`   // widget arguments
	Style  *Style         `yaml:"style" json:"style,omitempty"` // overrides the page style for this widget
	// Scroll lets a text widget return lines wider than the cell and shows
	// them as a marquee (ADR 0092); the cell has to span the display's width.
	Scroll bool `yaml:"scroll" json:"scroll,omitempty"`
}

// Plugin and widget name of the cell.
func (c Cell) Plugin() string { p, _, _ := strings.Cut(c.Widget, "/"); return p }

// Name is the widget name without the plugin.
func (c Cell) Name() string { _, w, _ := strings.Cut(c.Widget, "/"); return w }

// Alignments of a text cell (ADR 0111).
const (
	AlignLeft    = "left"
	AlignRight   = "right"
	AlignDecimal = "decimal"
)

// TextCell is one cell of a text page: a value in a run of characters. Width
// and alignment belong to the page, not to the plugin.
type TextCell struct {
	// Value is the declared value as <plugin>/<key>.
	Value string `yaml:"value" json:"value"`
	// Label sits in front of the value, Unit behind it with one space each.
	Label string `yaml:"label" json:"label,omitempty"`
	Unit  string `yaml:"unit" json:"unit,omitempty"`
	// Width is the number of characters the cell takes.
	Width int `yaml:"width" json:"width"`
	// Align is left, right or decimal; empty means left.
	Align string `yaml:"align" json:"align,omitempty"`
}

// Plugin and value name of the cell.
func (c TextCell) Plugin() string { p, _, _ := strings.Cut(c.Value, "/"); return p }

// Key is the value name without the plugin.
func (c TextCell) Key() string { _, k, _ := strings.Cut(c.Value, "/"); return k }

const maxGrid = 80

// Parse reads a page file and validates it.
func Parse(r io.Reader) (*Template, error) { return parse(r, false) }

// ParseVariant reads the file of a variant and validates it. A variant carries
// the layout, not the title and not the variants of its page (ADR 0110).
func ParseVariant(r io.Reader) (*Template, error) { return parse(r, true) }

func parse(r io.Reader, variant bool) (*Template, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var t Template
	if err := dec.Decode(&t); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("empty template")
		}
		return nil, err
	}
	if err := t.validate(); err != nil {
		return nil, err
	}
	if variant {
		if t.Title != "" {
			return nil, fmt.Errorf("title belongs to the page, not to its variant")
		}
		if len(t.Variants) > 0 {
			return nil, fmt.Errorf("a variant cannot carry variants")
		}
	}
	return &t, nil
}

// Kinds of pages and displays.
const (
	KindImage = "image"
	KindText  = "text"
)

// PagePath is the file of a page: pages/<name>.yaml (ADR 0110).
func PagePath(dir, name string) (string, error) {
	if !ValidName(name) {
		return "", fmt.Errorf("%w: invalid page name %q", ErrNotFound, name)
	}
	return filepath.Join(dir, name+".yaml"), nil
}

// VariantPath is the file of a variant: pages/<name>.<label>.yaml.
func VariantPath(dir, name, label string) (string, error) {
	if !ValidName(name) || !ValidName(label) {
		return "", fmt.Errorf("%w: invalid variant %q of page %q", ErrNotFound, label, name)
	}
	return filepath.Join(dir, name+"."+label+".yaml"), nil
}

// LoadFor reads the file that serves a display type: the variant that the page
// names, else the page file itself. A page without a file for the type is
// ErrNotFound, and the message names the reason (ADR 0110).
func LoadFor(dir, name, display string) (*Template, error) {
	p, err := PagePath(dir, name)
	if err != nil {
		return nil, err
	}
	t, err := readTemplate(p, false)
	if err != nil {
		return nil, err
	}
	label, ok := t.Variants[display]
	if !ok {
		return t, nil
	}
	vp, err := VariantPath(dir, name, label)
	if err != nil {
		return nil, err
	}
	v, err := readTemplate(vp, true)
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("%w: page %s names the variant %q for display type %q, but %s is missing",
			ErrNotFound, name, label, display, filepath.Base(vp))
	}
	if err != nil {
		return nil, err
	}
	v.Title = t.Title
	return v, nil
}

// readTemplate opens one page file and parses it as a page or as a variant.
func readTemplate(path string, variant bool) (*Template, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, path)
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var t *Template
	if variant {
		t, err = ParseVariant(f)
	} else {
		t, err = Parse(f)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return t, nil
}

// ValidName reports whether s can be used as a page, display, plugin or
// widget name.
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
	if len(t.Actions) > 0 {
		return fmt.Errorf("actions belong to the plan, not to a page (ADR 0057)")
	}
	switch t.Kind {
	case "":
		return fmt.Errorf("kind is required: image or text")
	case KindImage, KindText:
	default:
		return fmt.Errorf("kind %q: expected image or text", t.Kind)
	}
	for display, label := range t.Variants {
		if !ValidName(display) {
			return fmt.Errorf("variants: display type %q: only a-z, 0-9 and -", display)
		}
		if !ValidName(label) {
			return fmt.Errorf("variants: %s: label %q: only a-z, 0-9 and -", display, label)
		}
	}
	switch t.Notices {
	case "":
		t.Notices = "show"
	case "show", "none":
	default:
		return fmt.Errorf("notices %q: expected show or none", t.Notices)
	}
	if err := t.Style.validate(); err != nil {
		return fmt.Errorf("style: %w", err)
	}
	if t.Kind == KindText {
		return t.validateRows()
	}
	return t.validateGrid()
}

// validateGrid checks the layout of an image page: a grid of widget cells.
func (t *Template) validateGrid() error {
	if len(t.Rows) > 0 {
		return fmt.Errorf("rows are for text pages (kind: text); an image page uses grid and cells")
	}
	cols, rows := t.Grid[0], t.Grid[1]
	if cols < 1 || rows < 1 || cols > maxGrid || rows > maxGrid {
		return fmt.Errorf("grid [%d, %d]: expected [columns, rows] between 1 and %d", cols, rows, maxGrid)
	}
	if len(t.Cells) == 0 {
		return fmt.Errorf("cells missing")
	}
	taken := make([]int, cols*rows) // 1-based cell index per grid square
	for i := range t.Cells {
		c := &t.Cells[i]
		if c.Span == [2]int{} {
			c.Span = [2]int{1, 1}
		}
		where := fmt.Sprintf("cell %d (%s)", i+1, c.Widget)
		plugin, widget, ok := strings.Cut(c.Widget, "/")
		if !ok || !ValidName(plugin) || !ValidName(widget) {
			return fmt.Errorf("%s: widget %q: expected plugin/widget with a-z, 0-9 and -", where, c.Widget)
		}
		if c.At[0] < 0 || c.At[1] < 0 || c.Span[0] < 1 || c.Span[1] < 1 ||
			c.At[0]+c.Span[0] > cols || c.At[1]+c.Span[1] > rows {
			return fmt.Errorf("%s: at %v span %v leaves the %dx%d grid", where, c.At, c.Span, cols, rows)
		}
		if c.Style != nil {
			if err := c.Style.validate(); err != nil {
				return fmt.Errorf("%s: style: %w", where, err)
			}
		}
		if c.Scroll {
			return fmt.Errorf("%s: scroll is only for text pages (kind: text)", where)
		}
		for y := c.At[1]; y < c.At[1]+c.Span[1]; y++ {
			for x := c.At[0]; x < c.At[0]+c.Span[0]; x++ {
				if other := taken[y*cols+x]; other != 0 {
					return fmt.Errorf("%s overlaps cell %d at [%d, %d]", where, other, x, y)
				}
				taken[y*cols+x] = i + 1
			}
		}
	}
	return nil
}

// validateRows checks the layout of a text page: rows of value cells, no
// pixel grid and no widget cells (ADR 0111).
func (t *Template) validateRows() error {
	if t.Grid != [2]int{} {
		return fmt.Errorf("a text page has no grid: the character grid is the display's; use rows (ADR 0111)")
	}
	if len(t.Cells) > 0 {
		return fmt.Errorf("cells are for image pages; a text page uses rows of values (ADR 0111)")
	}
	if len(t.Rows) == 0 {
		return fmt.Errorf("rows missing: a text page needs at least one row")
	}
	if len(t.Rows) > maxGrid {
		return fmt.Errorf("rows: at most %d rows", maxGrid)
	}
	for y, row := range t.Rows {
		if len(row) == 0 {
			return fmt.Errorf("row %d: no cells", y+1)
		}
		width := 0
		for i := range row {
			c := &row[i]
			where := fmt.Sprintf("row %d, cell %d (%s)", y+1, i+1, c.Value)
			plugin, key, ok := strings.Cut(c.Value, "/")
			if !ok || !ValidName(plugin) || !ValidName(key) {
				return fmt.Errorf("%s: value %q: expected plugin/value with a-z, 0-9 and -", where, c.Value)
			}
			if c.Width < 1 || c.Width > maxGrid {
				return fmt.Errorf("%s: width %d: expected 1 to %d characters", where, c.Width, maxGrid)
			}
			switch c.Align {
			case "":
				c.Align = AlignLeft
			case AlignLeft, AlignRight, AlignDecimal:
			default:
				return fmt.Errorf("%s: align %q: expected left, right or decimal", where, c.Align)
			}
			width += c.Width
		}
		if width > maxGrid {
			return fmt.Errorf("row %d: %d characters wide, at most %d", y+1, width, maxGrid)
		}
	}
	return nil
}

// Fits reports why the page cannot be shown on a display: a text page on an
// image display or the other way round, an image page whose grid is not the
// display's pixel grid (the grid divides pixels, so any grid fits), or a text
// page whose rows do not fit the character grid. nil means it fits.
func (t *Template) Fits(d Display) error {
	switch {
	case d.Text && t.Kind != KindText:
		return fmt.Errorf("page is for image displays (kind: %s)", t.Kind)
	case !d.Text && t.Kind == KindText:
		return fmt.Errorf("page is for text displays (kind: text)")
	case d.Text:
		if len(t.Rows) > d.Height {
			return fmt.Errorf("%d rows do not fit the text display (%d rows)", len(t.Rows), d.Height)
		}
		for y, row := range t.Rows {
			width := 0
			for _, c := range row {
				width += c.Width
			}
			if width > d.Width {
				return fmt.Errorf("row %d is %d characters wide, the text display has %d columns", y+1, width, d.Width)
			}
		}
	}
	for i, c := range t.Cells {
		if c.Scroll && c.Span[0] != d.Width {
			return fmt.Errorf("cell %d (%s): scroll needs the full display width of %d columns, got %d",
				i+1, c.Widget, d.Width, c.Span[0])
		}
	}
	return nil
}

func (s *Style) validate() error {
	if s.Padding != nil && *s.Padding < 0 {
		return fmt.Errorf("padding must not be negative")
	}
	if s.Rule < 0 || s.Divider < 0 {
		return fmt.Errorf("rule and divider must not be negative")
	}
	if s.TextSize != 0 && s.TextSize < MinTextPt {
		return fmt.Errorf("text_size %g: text_size is in points since ADR 0050 (e.g. 16), not a fraction of the height", s.TextSize)
	}
	if s.TextSize > MaxTextPt {
		return fmt.Errorf("text_size %g: at most %g pt", s.TextSize, MaxTextPt)
	}
	return nil
}

// Limits of text_size in points.
const (
	MinTextPt = 4.0
	MaxTextPt = 144.0
)

// merged returns the page style with a cell's overrides applied.
func (s Style) merged(o *Style) Style {
	if o == nil {
		return s
	}
	if o.Padding != nil {
		s.Padding = o.Padding
	}
	if o.Rule != 0 {
		s.Rule = o.Rule
	}
	if o.TextSize != 0 {
		s.TextSize = o.TextSize
	}
	return s
}
