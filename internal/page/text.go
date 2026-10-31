package page

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/maehld/paperboy/internal/data"
)

// Character sets of text displays (ADR 0044).
const (
	CharsetUTF8    = "utf8"
	CharsetASCII   = "ascii"
	CharsetHD44780 = "hd44780"
)

// ValidCharset reports whether s names a supported character set; empty means utf8.
func ValidCharset(s string) bool {
	switch s {
	case "", CharsetUTF8, CharsetASCII, CharsetHD44780:
		return true
	}
	return false
}

// TextTemplate is a page for a text display (ADR 0027, ADR 0044): one Go text
// template per line instead of blocks.
type TextTemplate struct {
	Title   string   `yaml:"title"`
	Watch   []Watch  `yaml:"watch"`
	Notices string   `yaml:"notices"` // mark (default) or none
	Lines   []string `yaml:"lines"`
}

// TextDisplay describes the character grid a text page is rendered for.
type TextDisplay struct {
	Columns  int
	Rows     int
	Charset  string
	Language string
	Timezone *time.Location
}

// ParseText reads a text template and validates it.
func ParseText(r io.Reader) (*TextTemplate, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var t TextTemplate
	if err := dec.Decode(&t); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("empty template")
		}
		return nil, fmt.Errorf("%w (text display types use lines instead of rows and blocks)", err)
	}
	if err := t.validate(); err != nil {
		return nil, err
	}
	return &t, nil
}

// LoadText reads pages/<name>.<display>.yaml for a text display type.
func LoadText(dir, name, display string) (*TextTemplate, error) {
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
	t, err := ParseText(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return t, nil
}

func (t *TextTemplate) validate() error {
	if len(t.Lines) == 0 {
		return fmt.Errorf("lines missing")
	}
	switch t.Notices {
	case "":
		t.Notices = "mark"
	case "mark", "none":
	default:
		return fmt.Errorf("notices %q: expected mark or none", t.Notices)
	}
	for i, l := range t.Lines {
		if _, err := template.New("line").Funcs((&renderer{}).textFuncs()).Parse(l); err != nil {
			return fmt.Errorf("line %d: %w", i+1, err)
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

// Render expands the lines from the data document and fits them to the grid:
// converted to the character set, cut to the column count and padded with
// spaces. More lines than rows is an error.
func (t *TextTemplate) Render(doc *data.Document, d TextDisplay, c Context) ([]string, error) {
	if d.Columns < 1 || d.Rows < 1 {
		return nil, fmt.Errorf("invalid text display size %dx%d", d.Columns, d.Rows)
	}
	if len(t.Lines) > d.Rows {
		return nil, fmt.Errorf("%d lines but the display has %d rows", len(t.Lines), d.Rows)
	}
	raw, _ := doc.Get("")
	root, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("data document is not an object")
	}
	disp := Display{Language: d.Language, Timezone: d.Timezone}
	language, loc := disp.language(), disp.location()
	if c.Page <= 0 {
		c.Page = 1
	}
	if c.Pages <= 0 {
		c.Pages = 1
	}
	r := &renderer{watch: t.Watch, doc: doc, root: root, f: formatter{comma: language != "en", loc: loc},
		now: c.Now.In(loc), language: language, page: c.Page, pages: c.Pages}
	out := make([]string, d.Rows)
	for i, l := range t.Lines {
		out[i] = r.expandLine(l)
		if r.err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, r.err)
		}
	}
	for i := range out {
		out[i] = fit(Convert(d.Charset, out[i]), d.Columns)
	}
	if t.Notices == "mark" && watchStale(t.Watch, doc) {
		line := []rune(out[0])
		line[len(line)-1] = '!'
		out[0] = string(line)
	}
	return out, nil
}

// expandLine runs a line template without trimming, so alignment survives.
func (r *renderer) expandLine(s string) string {
	if !strings.Contains(s, "{{") {
		return s
	}
	tmpl, err := template.New("line").Option("missingkey=zero").Funcs(r.textFuncs()).Parse(s)
	if err != nil {
		r.fail(err)
		return ""
	}
	ensureBranches(r.root, tmpl.Tree)
	var b strings.Builder
	if err := tmpl.Execute(&b, r.root); err != nil {
		r.fail(err)
		return ""
	}
	return strings.ReplaceAll(b.String(), "<no value>", "—")
}

// textFuncs adds the fixed-width helpers of ADR 0027 to the template helpers.
func (r *renderer) textFuncs() template.FuncMap {
	m := r.funcs()
	m["pad"] = func(n int, v any) string { return align(n, fmt.Sprint(v), 'l') }
	m["right"] = func(n int, v any) string { return align(n, fmt.Sprint(v), 'r') }
	m["center"] = func(n int, v any) string { return align(n, fmt.Sprint(v), 'c') }
	m["fit"] = func(n int, kind string, v any, unitWidth ...int) (string, error) {
		if !formats[kind] {
			return "", fmt.Errorf("fit: unknown format %q", kind)
		}
		switch {
		case len(unitWidth) == 0:
			return r.f.fitValue(n, kind, v), nil
		case len(unitWidth) > 1 || unitWidth[0] < 0:
			return "", fmt.Errorf("fit: expected one unit width >= 0")
		}
		return r.f.fitAligned(n, unitWidth[0], kind, v), nil
	}
	return m
}

// align cuts or fills s to exactly n characters.
func align(n int, s string, mode byte) string {
	if n < 0 {
		n = 0
	}
	r := []rune(s)
	if len(r) >= n {
		if mode == 'r' {
			return string(r[len(r)-n:])
		}
		return string(r[:n])
	}
	gap := n - len(r)
	switch mode {
	case 'r':
		return strings.Repeat(" ", gap) + s
	case 'c':
		return strings.Repeat(" ", gap/2) + s + strings.Repeat(" ", gap-gap/2)
	}
	return s + strings.Repeat(" ", gap)
}

// fit replaces control characters and cuts or pads a line to n columns.
func fit(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	return align(n, s, 'l')
}

func watchStale(watch []Watch, doc *data.Document) bool {
	for _, w := range watch {
		age, ok := doc.Age(w.Path)
		if ok && age > time.Duration(w.StaleAfter) {
			return true
		}
	}
	return false
}

// hd44780Kept are the characters of the A00 ROM the firmware maps itself.
var hd44780Kept = map[rune]bool{'ä': true, 'ö': true, 'ü': true, 'ß': true, '°': true, 'µ': true}

var transliteration = map[rune]string{
	'ä': "ae", 'ö': "oe", 'ü': "ue", 'Ä': "Ae", 'Ö': "Oe", 'Ü': "Ue", 'ß': "ss",
	'°': "o", 'µ': "u", '€': "EUR", '—': "-", '–': "-", '−': "-", '…': "...",
	'„': "\"", '“': "\"", '”': "\"", '‚': "'", '‘': "'", '’': "'",
	'\u00a0': " ", '\u202f': " ", '\u2009': " ", '·': ".", '×': "x",
	'²': "2", '³': "3", '½': "1/2", 'é': "e", 'è': "e", 'á': "a", 'à': "a",
}

// hd44780Upper maps capitals without a ROM glyph to their base letter, so a
// column keeps its width.
var hd44780Upper = map[rune]string{'Ä': "A", 'Ö': "O", 'Ü': "U"}

// Convert maps s to the character set of a text display (ADR 0044).
func Convert(charset, s string) string {
	if charset == "" || charset == CharsetUTF8 {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < utf8.RuneSelf:
			b.WriteRune(r)
		case charset == CharsetHD44780 && hd44780Kept[r]:
			b.WriteRune(r)
		case charset == CharsetHD44780 && hd44780Upper[r] != "":
			b.WriteString(hd44780Upper[r])
		case transliteration[r] != "":
			b.WriteString(transliteration[r])
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}

// NoticeLines word-wraps a notice title and text onto the grid.
func NoticeLines(d TextDisplay, title, text string) []string {
	var lines []string
	for _, part := range []string{title, text} {
		lines = append(lines, wrap(Convert(d.Charset, part), d.Columns)...)
	}
	if len(lines) > d.Rows {
		lines = lines[:d.Rows]
	}
	out := make([]string, d.Rows)
	for i := range out {
		if i < len(lines) {
			out[i] = lines[i]
		}
		out[i] = fit(out[i], d.Columns)
	}
	return out
}

func wrap(s string, n int) []string {
	var lines []string
	cur := ""
	for _, w := range strings.Fields(s) {
		for len([]rune(w)) > n {
			if cur != "" {
				lines, cur = append(lines, cur), ""
			}
			r := []rune(w)
			lines, w = append(lines, string(r[:n])), string(r[n:])
		}
		switch {
		case cur == "":
			cur = w
		case len([]rune(cur))+1+len([]rune(w)) <= n:
			cur += " " + w
		default:
			lines, cur = append(lines, cur), w
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}
