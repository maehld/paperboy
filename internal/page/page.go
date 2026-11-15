// Package page parses page templates and composes them from widgets
// (ADR 0049) and plugin values (ADR 0111).
//
// An image page is a grid of cells. Every cell shows one widget of a plugin,
// e.g. "calendar/agenda". The grid divides the pixels; Paperboy asks the
// plugins for every widget in exactly the size of its cell and pastes the
// results; it never scales them.
//
// A text page is a layout of rows of values. Every cell names a value a plugin
// declares, and Paperboy formats it, aligns it and maps it to the character
// set of the display.
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
	// Overflow is the default of a text page: strip, wrap or scroll. A cell
	// may override it (ADR 0117).
	Overflow string `yaml:"overflow" json:"overflow,omitempty"`
	// Cells are boxes of an image page in grid cells, or boxes of a text page
	// in character columns and rows (ADR 0111, 0117).
	Cells []Cell `yaml:"cells" json:"cells,omitempty"`
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

// Cell places one widget on an image page or one box of text on a text page.
type Cell struct {
	At     [2]int         `yaml:"at" json:"at"`     // column, row of the top-left cell
	Span   [2]int         `yaml:"span" json:"span"` // columns, rows; default 1x1
	Widget string         `yaml:"widget,omitempty" json:"widget,omitempty"`
	Args   map[string]any `yaml:"args,omitempty" json:"args,omitempty"`
	Style  *Style         `yaml:"style,omitempty" json:"style,omitempty"`
	// Text is literal text of a text page; Value is a declared value. Exactly
	// one of them is set (ADR 0117). Multi-line text keeps its line breaks.
	Text  string `yaml:"text,omitempty" json:"text,omitempty"`
	Value string `yaml:"value,omitempty" json:"value,omitempty"`
	Label string `yaml:"label,omitempty" json:"label,omitempty"`
	Unit  string `yaml:"unit,omitempty" json:"unit,omitempty"`
	// Align is left, right or decimal; empty means left.
	Align string `yaml:"align,omitempty" json:"align,omitempty"`
	// Overflow overrides the page's strip, wrap or scroll.
	Overflow string `yaml:"overflow,omitempty" json:"overflow,omitempty"`
	// Scroll only exists to reject the old image key with a hint.
	Scroll bool `yaml:"scroll,omitempty" json:"-"`
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

// Overflow policies of a text cell (ADR 0117).
const (
	OverflowStrip  = "strip"
	OverflowWrap   = "wrap"
	OverflowScroll = "scroll"
)

// Plugin and value name of a value cell.
func (c Cell) ValuePlugin() string { p, _, _ := strings.Cut(c.Value, "/"); return p }

// ValueKey is the value name without the plugin.
func (c Cell) ValueKey() string { _, k, _ := strings.Cut(c.Value, "/"); return k }

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
		return t.validateText()
	}
	return t.validateGrid()
}

// validateGrid checks the layout of an image page: a grid of widget cells.
func (t *Template) validateGrid() error {
	if t.Overflow != "" {
		return fmt.Errorf("overflow is for text pages (kind: text); ePaper cannot animate")
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
		if c.Scroll {
			return fmt.Errorf("%s: scroll is for text pages; ePaper cannot animate (ADR 0117)", where)
		}
		if c.Text != "" || c.Value != "" || c.Label != "" || c.Unit != "" || c.Align != "" || c.Overflow != "" {
			return fmt.Errorf("%s: text and value are for text pages (kind: text); use widget", where)
		}
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

// Limits of a text page: the character grid of a display type (ADR 0044).
const (
	maxTextColumns = 80
	maxTextRows    = 24
)

// validateText checks the boxes of a text page. It does not know the display
// type, so it uses the largest character grid; Fits checks the real one.
func (t *Template) validateText() error {
	if t.Grid != [2]int{} {
		return fmt.Errorf("a text page has no grid: the character grid is the display's; use cells (ADR 0117)")
	}
	switch t.Overflow {
	case "":
		t.Overflow = OverflowStrip
	case OverflowStrip, OverflowWrap, OverflowScroll:
	default:
		return fmt.Errorf("overflow %q: expected strip, wrap or scroll", t.Overflow)
	}
	if len(t.Cells) == 0 {
		return fmt.Errorf("cells missing")
	}
	taken := make([]int, maxTextColumns*maxTextRows)
	for i := range t.Cells {
		c := &t.Cells[i]
		if c.Span == [2]int{} {
			c.Span = [2]int{1, 1}
		}
		where := fmt.Sprintf("cell %d", i+1)
		if c.Widget != "" || len(c.Args) > 0 || c.Style != nil {
			return fmt.Errorf("%s: widget, args and style are for image pages (kind: image)", where)
		}
		if c.Scroll {
			return fmt.Errorf("%s: use overflow: scroll instead of scroll (ADR 0117)", where)
		}
		if c.At[0] < 0 || c.At[1] < 0 || c.Span[0] < 1 || c.Span[1] < 1 ||
			c.At[0]+c.Span[0] > maxTextColumns || c.At[1]+c.Span[1] > maxTextRows {
			return fmt.Errorf("%s: at %v span %v leaves the %dx%d character grid", where, c.At, c.Span, maxTextColumns, maxTextRows)
		}
		if (c.Text == "") == (c.Value == "") {
			return fmt.Errorf("%s: exactly one of text and value is required", where)
		}
		if c.Value != "" {
			plugin, key, ok := strings.Cut(c.Value, "/")
			if !ok || !ValidName(plugin) || !ValidName(key) {
				return fmt.Errorf("%s: value %q: expected plugin/value with a-z, 0-9 and -", where, c.Value)
			}
		} else if c.Label != "" || c.Unit != "" {
			return fmt.Errorf("%s: a literal text cell has no label or unit", where)
		}
		switch c.Align {
		case "":
			c.Align = AlignLeft
		case AlignLeft, AlignRight, AlignDecimal:
		default:
			return fmt.Errorf("%s: align %q: expected left, right or decimal", where, c.Align)
		}
		switch c.Overflow {
		case "":
			c.Overflow = t.Overflow
		case OverflowStrip, OverflowWrap, OverflowScroll:
		default:
			return fmt.Errorf("%s: overflow %q: expected strip, wrap or scroll", where, c.Overflow)
		}
		for y := c.At[1]; y < c.At[1]+c.Span[1]; y++ {
			for x := c.At[0]; x < c.At[0]+c.Span[0]; x++ {
				if other := taken[y*maxTextColumns+x]; other != 0 {
					return fmt.Errorf("%s overlaps cell %d at [%d, %d]", where, other, x, y)
				}
				taken[y*maxTextColumns+x] = i + 1
			}
		}
	}
	return nil
}

// Fits reports why the page cannot be shown on a display: a text page on an
// image display or the other way round, or a text box outside the character
// grid. nil means it fits.
func (t *Template) Fits(d Display) error {
	switch {
	case d.Text && t.Kind != KindText:
		return fmt.Errorf("page is for image displays (kind: %s)", t.Kind)
	case !d.Text && t.Kind == KindText:
		return fmt.Errorf("page is for text displays (kind: text)")
	case d.Text:
		if len(t.Cells) > d.Width*d.Height {
			return fmt.Errorf("%d cells do not fit the text display", len(t.Cells))
		}
		taken := make([]int, d.Width*d.Height)
		for i, c := range t.Cells {
			if c.At[0] < 0 || c.At[1] < 0 || c.Span[0] < 1 || c.Span[1] < 1 ||
				c.At[0]+c.Span[0] > d.Width || c.At[1]+c.Span[1] > d.Height {
				return fmt.Errorf("cell %d: at %v span %v leaves the %dx%d character grid", i+1, c.At, c.Span, d.Width, d.Height)
			}
			for y := c.At[1]; y < c.At[1]+c.Span[1]; y++ {
				for x := c.At[0]; x < c.At[0]+c.Span[0]; x++ {
					if other := taken[y*d.Width+x]; other != 0 {
						return fmt.Errorf("cell %d overlaps cell %d at [%d, %d]", i+1, other, x, y)
					}
					taken[y*d.Width+x] = i + 1
				}
			}
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
