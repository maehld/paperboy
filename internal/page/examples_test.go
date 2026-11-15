package page

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExamplePagesParse keeps the shipped examples in the model of ADR 0110:
// a page file parses as a page, a variant file as a variant, and every entry of
// a page's variants map names a file that exists.
func TestExamplePagesParse(t *testing.T) {
	files, err := filepath.Glob("../../examples/pages/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no example pages found")
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimSuffix(filepath.Base(file), ".yaml")
		if strings.Contains(name, ".") {
			if _, err := ParseVariant(strings.NewReader(string(data))); err != nil {
				t.Errorf("%s does not read as a variant: %v", file, err)
			}
			continue
		}
		tmpl, err := Parse(strings.NewReader(string(data)))
		if err != nil {
			t.Errorf("%s does not read as a page: %v", file, err)
			continue
		}
		for display, label := range tmpl.Variants {
			variant := filepath.Join(filepath.Dir(file), name+"."+label+".yaml")
			if _, err := os.Stat(variant); err != nil {
				t.Errorf("%s sends %s to %s: %v", file, display, label, err)
			}
		}
	}
}
