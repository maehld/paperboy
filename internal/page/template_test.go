package page

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const validTemplate = `
title: Solar
watch:
  - { path: solar.current, stale_after: 5m, label: Solar }
rows: [1, 2, 2]
blocks:
  - name: kopf
    header:
      title: Solar
      time: solar.current.latest.time
  - name: kacheln
    tiles:
      - { label: PV, value: solar.current.latest.values.pv_power, format: power }
      - { label: Netz, value: solar.current.latest.values.grid_power, format: power, sign: [Einspeisung, Bezug] }
  - name: verlauf
    chart:
      points: solar.history.points
      series: [pv_power, load_power]
      unit: kW
`

func TestParseValid(t *testing.T) {
	tmpl, err := Parse(strings.NewReader(validTemplate))
	if err != nil {
		t.Fatal(err)
	}
	if len(tmpl.Rows) != 3 || len(tmpl.Blocks) != 3 {
		t.Fatalf("rows %d, blocks %d", len(tmpl.Rows), len(tmpl.Blocks))
	}
	// Dotted paths are normalized to the slash form of the data document.
	if got := tmpl.Blocks[1].Tiles[0].Value; got != "solar/current/latest/values/pv_power" {
		t.Fatalf("value path: %q", got)
	}
	if got := tmpl.Watch[0].Path; got != "solar/current" {
		t.Fatalf("watch path: %q", got)
	}
	if time.Duration(tmpl.Watch[0].StaleAfter) != 5*time.Minute {
		t.Fatalf("stale_after: %v", tmpl.Watch[0].StaleAfter)
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"unknown field": `
rows: [1]
blocks:
  - text: "x"
    fontsize: 12
`,
		"rows missing": `
blocks:
  - text: "x"
`,
		"count mismatch": `
rows: [1, 1]
blocks:
  - text: "x"
`,
		"two blocks in one region": `
rows: [1]
blocks:
  - text: "x"
    header: { title: Solar }
`,
		"unknown format": `
rows: [1]
blocks:
  - tiles:
      - { label: PV, value: solar.current, format: watts }
`,
		"bad sign": `
rows: [1]
blocks:
  - tiles:
      - { label: PV, value: solar.current, format: power, sign: [nur-eins] }
`,
		"bad path": `
rows: [1]
blocks:
  - tiles:
      - { label: PV, value: "Solar.Current", format: power }
`,
		"watch without stale_after": `
watch:
  - { path: solar/current }
rows: [1]
blocks:
  - text: "x"
`,
		"locale removed": `
locale: de
rows: [1]
blocks:
  - text: "x"
`,
	}
	for name, yaml := range cases {
		if _, err := Parse(strings.NewReader(yaml)); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "solar-overview.e1001.yaml")
	if err := os.WriteFile(path, []byte(validTemplate), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, "solar-overview", "e1001"); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, "solar-overview", "sticky"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing display: %v", err)
	}
	if _, err := Load(dir, "../etc/passwd", "e1001"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("path traversal: %v", err)
	}

	broken := filepath.Join(dir, "broken.e1001.yaml")
	if err := os.WriteFile(broken, []byte("rows: [1]\nblocks:\n  - text: x\n    nope: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, "broken", "e1001"); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("broken template must be a parse error: %v", err)
	}
}

func TestValidName(t *testing.T) {
	for _, name := range []string{"a", "solar-overview", "row0"} {
		if !ValidName(name) {
			t.Fatalf("%q must be valid", name)
		}
	}
	for _, name := range []string{"", "Solar", "a/b", "../x", strings.Repeat("a", 41)} {
		if ValidName(name) {
			t.Fatalf("%q must be invalid", name)
		}
	}
}
