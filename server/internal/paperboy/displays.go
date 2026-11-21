package paperboy

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/maehld/paperboy/server/internal/page"
	"github.com/maehld/paperboy/server/internal/strictyaml"
)

// Display types live in files of their own (ADR 0071): displays/<name>.yaml,
// one file per display type. The file name is the stable name that page
// variants and device files use; a file that does not parse aborts the load,
// like an invalid paperboy.yaml.

// displayFile mirrors displays/<name>.yaml with what the operator wrote: an
// empty field keeps the default, so the editor writes back only what is set.
// The model is also the JSON model of the display type API.
type displayFile struct {
	Kind       string         `yaml:"kind" json:"kind"`
	Size       [2]int         `yaml:"size" json:"size"`
	GrayLevels int            `yaml:"gray_levels" json:"gray_levels,omitempty"`
	PPI        float64        `yaml:"ppi" json:"ppi,omitempty"`
	Charset    string         `yaml:"charset" json:"charset,omitempty"`
	MinRefresh Duration       `yaml:"min_refresh" json:"min_refresh,omitempty"`
	Power      string         `yaml:"power" json:"power,omitempty"`
	Rotation   int            `yaml:"rotation" json:"rotation,omitempty"`           // page → panel, ADR 0102
	Scroll     *displayScroll `yaml:"scroll,omitempty" json:"scroll,omitempty"`     // text displays, ADR 0118
	Grid       *[2]int        `yaml:"grid,omitempty" json:"grid,omitempty"`         // default grid of a new page, ADR 0128
	WidthMM    *int           `yaml:"width_mm,omitempty" json:"width_mm,omitempty"` // removed by ADR 0050, rejected with a hint
}

// displayScroll is the marquee timing of a text display (ADR 0118).
type displayScroll struct {
	Step Duration `yaml:"step,omitempty" json:"step,omitempty"`
}

// parseDisplayFile reads one display type file strictly: an unknown key or a
// second document is an error, like in pages, plans and devices.
func parseDisplayFile(data []byte) (displayFile, error) {
	var f displayFile
	if err := strictyaml.Decode(data, &f); err != nil {
		if errors.Is(err, io.EOF) {
			return f, errors.New("empty display type file")
		}
		return f, err
	}
	return f, nil
}

// check rejects what only the file can carry, before it becomes a Profile.
func (f displayFile) check(name string) error {
	if f.WidthMM != nil {
		return fmt.Errorf("display type %s: width_mm was replaced by ppi (ADR 0050): replace it with ppi = pixels × 25.4 / mm of the same side", name)
	}
	if f.GrayLevels != 0 && f.GrayLevels != 1 && f.GrayLevels != 4 && f.GrayLevels != 16 {
		return fmt.Errorf("display type %s: gray_levels must be 1, 4 or 16", name)
	}
	if !validRotation(f.Rotation) {
		return fmt.Errorf("display type %s: rotation must be 0, 90, 180 or 270", name)
	}
	if f.Scroll != nil {
		if f.Kind != kindText {
			return fmt.Errorf("display type %s: scroll only applies to kind text", name)
		}
		if f.Scroll.Step < 0 {
			return fmt.Errorf("display type %s: scroll.step must not be negative", name)
		}
	}
	if f.Grid != nil {
		if f.Kind == kindText {
			return fmt.Errorf("display type %s: grid only applies to kind image; a text page uses the character grid", name)
		}
		if f.Grid[0] < 1 || f.Grid[1] < 1 || f.Grid[0] > maxGrid || f.Grid[1] > maxGrid {
			return fmt.Errorf("display type %s: grid [%d, %d]: expected [columns, rows] between 1 and %d",
				name, f.Grid[0], f.Grid[1], maxGrid)
		}
	}
	return nil
}

// profile builds the runtime display type; validateProfile fills the defaults.
func (f displayFile) profile() Profile {
	levels := 2
	if f.GrayLevels > 2 {
		levels = f.GrayLevels
	}
	step := time.Duration(0)
	if f.Kind == kindText {
		step = DefaultScrollStep
		if f.Scroll != nil && f.Scroll.Step > 0 {
			step = time.Duration(f.Scroll.Step)
		}
	}
	p := Profile{
		Kind:       f.Kind,
		Charset:    f.Charset,
		Width:      f.Size[0],
		Height:     f.Size[1],
		Levels:     levels,
		PPI:        f.PPI,
		MinRefresh: f.MinRefresh,
		Power:      f.Power,
		Rotation:   f.Rotation,
		ScrollStep: step,
	}
	p.Grid = f.Grid
	return p
}

// displayFileNames lists the display type files of a directory; a missing
// directory simply has none.
func displayFileNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// displayName strips the file suffix; the file name is the display type.
func displayName(file string) string {
	name, _ := strings.CutSuffix(file, ".yaml")
	return name
}

// loadDisplays reads displays/<name>.yaml of an instance directory (ADR 0071).
// A file that does not parse or validate aborts the load; the caller keeps its
// last valid configuration (ADR 0047).
func loadDisplays(dir string) (map[string]Profile, error) {
	names, err := displayFileNames(dir)
	if err != nil {
		return nil, err
	}
	displays := make(map[string]Profile, len(names))
	for _, file := range names {
		path := filepath.Join(dir, file)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		name := displayName(file)
		f, err := parseDisplayFile(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if err := f.check(name); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		p := f.profile()
		if err := validateProfile(name, &p); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		displays[name] = p
	}
	return displays, nil
}

// encodeDisplayFile writes a display type file the way parseDisplayFile reads
// it: a stable, hand-readable layout with only the values that are set.
func encodeDisplayFile(f *displayFile) ([]byte, error) {
	root := mapping()
	set := func(key string, value *yaml.Node) {
		root.Content = append(root.Content, plainScalar(key), value)
	}
	kind := f.Kind
	if kind == "" {
		kind = kindImage
	}
	set("kind", plainScalar(kind))
	size := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle,
		Content: []*yaml.Node{intScalar(f.Size[0]), intScalar(f.Size[1])}}
	set("size", size)
	if f.GrayLevels != 0 {
		set("gray_levels", intScalar(f.GrayLevels))
	}
	if f.PPI != 0 {
		set("ppi", floatScalar(f.PPI))
	}
	if f.Charset != "" {
		set("charset", plainScalar(f.Charset))
	}
	if f.MinRefresh != 0 {
		set("min_refresh", durationValue(time.Duration(f.MinRefresh)))
	}
	if f.Power != "" {
		set("power", plainScalar(f.Power))
	}
	if f.Rotation != 0 {
		set("rotation", intScalar(f.Rotation))
	}
	if f.Grid != nil {
		grid := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle,
			Content: []*yaml.Node{intScalar(f.Grid[0]), intScalar(f.Grid[1])}}
		set("grid", grid)
	}
	return marshalFile(root)
}

func intScalar(v int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(v)}
}

func floatScalar(v float64) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: strconv.FormatFloat(v, 'f', -1, 64)}
}

// editorDisplayFile is one display type file in the overview of the editor.
type editorDisplayFile struct {
	Name     string  `json:"name"`
	Kind     string  `json:"kind,omitempty"`
	Size     [2]int  `json:"size"`
	Gray     int     `json:"gray_levels,omitempty"`
	PPI      float64 `json:"ppi,omitempty"`
	Charset  string  `json:"charset,omitempty"`
	Power    string  `json:"power,omitempty"`
	Rotation int     `json:"rotation,omitempty"`
	Hash     string  `json:"hash"`
	Error    string  `json:"error,omitempty"`
}

// displayFileList reads the display type files of an instance directory for
// the editor; a file that does not parse or validate still appears, with its
// error.
func displayFileList(dir string) ([]editorDisplayFile, error) {
	names, err := displayFileNames(dir)
	if err != nil {
		return nil, err
	}
	list := make([]editorDisplayFile, 0, len(names))
	for _, file := range names {
		data, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			continue
		}
		name := displayName(file)
		f := editorDisplayFile{Name: name, Hash: fileHash(data)}
		display, err := parseDisplayFile(data)
		if err != nil {
			f.Error = shortError(err)
			list = append(list, f)
			continue
		}
		f.Kind, f.Size, f.Gray, f.PPI, f.Charset, f.Power, f.Rotation =
			display.Kind, display.Size, display.GrayLevels, display.PPI, display.Charset, display.Power, display.Rotation
		if err := display.check(name); err != nil {
			f.Error = shortError(err)
		} else {
			p := display.profile()
			if err := validateProfile(name, &p); err != nil {
				f.Error = shortError(err)
			}
		}
		list = append(list, f)
	}
	return list, nil
}

// displayFilePath resolves displays/<name>.yaml; the name is validated because
// it becomes a file name.
func displayFilePath(dir, name string) (string, error) {
	if !page.ValidName(name) {
		return "", fmt.Errorf("display type %q: only a-z, 0-9 and -", name)
	}
	return filepath.Join(dir, name+".yaml"), nil
}

// displaysStamp summarizes the display type files of a directory, so the
// configuration loader notices a change there even when paperboy.yaml did not
// change (ADR 0047, 0071).
func displaysStamp(dir string) string {
	names, err := displayFileNames(dir)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, name := range names {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "%s:%d:%d;", name, info.Size(), info.ModTime().UnixNano())
	}
	return b.String()
}
