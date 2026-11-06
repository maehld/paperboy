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

// Template is one page.
type Template struct {
	Title   string `yaml:"title"`
	Kind    string `yaml:"kind"`    // image (default) or text: the displays the page is for
	Grid    [2]int `yaml:"grid"`    // columns, rows
	Style   Style  `yaml:"style"`   // look passed to every widget
	Notices string `yaml:"notices"` // show (default) or none: stale widgets
	Cells   []Cell `yaml:"cells"`
}

// Style is the page's look. Image displays use pixels; text displays ignore
// it.
type Style struct {
	Padding  *int    `yaml:"padding"`   // inner margin of a widget, px; default 8
	Rule     int     `yaml:"rule"`      // separator width a widget draws, px; default 1
	TextSize float64 `yaml:"text_size"` // base text height, fraction of the page height; default 0.06
	Divider  int     `yaml:"divider"`   // lines Paperboy draws between cells, px; default 0
}

// Cell places one widget on the grid.
type Cell struct {
	At     [2]int         `yaml:"at"`     // column, row of the top-left cell
	Span   [2]int         `yaml:"span"`   // columns, rows; default 1x1
	Widget string         `yaml:"widget"` // plugin/widget
	Args   map[string]any `yaml:"args"`
	Style  *Style         `yaml:"style"` // overrides the page style for this widget
}

// Plugin and widget name of the cell.
func (c Cell) Plugin() string { p, _, _ := strings.Cut(c.Widget, "/"); return p }

// Name is the widget name without the plugin.
func (c Cell) Name() string { _, w, _ := strings.Cut(c.Widget, "/"); return w }

const maxGrid = 80

// Parse reads a template and validates it.
func Parse(r io.Reader) (*Template, error) {
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
	return &t, nil
}

// Kinds of pages and displays.
const (
	KindImage = "image"
	KindText  = "text"
)

// Paths lists the files a page may come from for a display type, the variant
// first: pages/<name>.<display>.yaml, then pages/<name>.yaml.
func Paths(dir, name, display string) ([]string, error) {
	if !ValidName(name) || !ValidName(display) {
		return nil, fmt.Errorf("%w: invalid page %q for display %q", ErrNotFound, name, display)
	}
	return []string{filepath.Join(dir, name+"."+display+".yaml"), filepath.Join(dir, name+".yaml")}, nil
}

// Load reads pages/<name>.<display>.yaml, or pages/<name>.yaml when the page
// has no variant for the display type. A missing page is ErrNotFound.
func Load(dir, name, display string) (*Template, error) {
	paths, err := Paths(dir, name, display)
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		f, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		t, err := Parse(f)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return t, nil
	}
	return nil, fmt.Errorf("%w: %s.%s.yaml or %s.yaml in %s", ErrNotFound, name, display, name, dir)
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
	cols, rows := t.Grid[0], t.Grid[1]
	if cols < 1 || rows < 1 || cols > maxGrid || rows > maxGrid {
		return fmt.Errorf("grid [%d, %d]: expected [columns, rows] between 1 and %d", cols, rows, maxGrid)
	}
	switch t.Kind {
	case "":
		t.Kind = KindImage
	case KindImage, KindText:
	default:
		return fmt.Errorf("kind %q: expected image or text", t.Kind)
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

// Fits reports why the page cannot be shown on a display: a text page on an
// image display or the other way round, or a text page whose grid is not the
// display's character grid. nil means it fits.
func (t *Template) Fits(d Display) error {
	switch {
	case d.Text && t.Kind != KindText:
		return fmt.Errorf("page is for image displays (kind: %s)", t.Kind)
	case !d.Text && t.Kind == KindText:
		return fmt.Errorf("page is for text displays (kind: text)")
	case d.Text && (t.Grid[0] != d.Width || t.Grid[1] != d.Height):
		return fmt.Errorf("grid [%d, %d] does not match the text display (%d columns, %d rows)",
			t.Grid[0], t.Grid[1], d.Width, d.Height)
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
	if s.TextSize < 0 || s.TextSize > 0.5 {
		return fmt.Errorf("text_size %g: expected a fraction of the page height, e.g. 0.06", s.TextSize)
	}
	return nil
}

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
