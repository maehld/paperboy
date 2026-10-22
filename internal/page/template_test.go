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
	if got := tmpl.Blocks[1].Tiles.Items[0].Value; got != "solar/current/latest/values/pv_power" {
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

func TestParseShorthandsAndObjectForms(t *testing.T) {
	tmpl, err := Parse(strings.NewReader(`
title: Solar
notices: badge
rows: [1, 3, 3, 4, 2]
blocks:
  - name: kopf
    header:
      title: Solar
      align: left
      status: { time: solar.current.latest.time, label: Stand }
      page_indicator: true
      rule: 2
  - name: kacheln
    tiles:
      columns: 4
      divider: 2
      padding: 8
      items:
        - { label: PV, value: solar.current.latest.values.pv_power, format: power, detail: "{{ power .solar.current.latest.values.pv_power }}" }
  - name: detail
    columns:
      - weight: 26
        block:
          bar: { value: solar.current.latest.values.battery_soc, min: 0, max: 100, orientation: vertical, border: 5, cap: true, label: "{{ percent .solar.current.latest.values.battery_soc }}" }
      - weight: 74
        block:
          rows:
            rule: 1
            items:
              - { label: Leistung, value: solar.current.latest.values.load_power, format: power }
  - name: verlauf
    chart:
      points: solar.history.points
      time: true
      axis: true
      now: true
      y_max: auto
      series:
        - { key: pv_power, label: Solar, style: area, fill: shade }
        - load_power
  - name: text
    text: { value: "Hallo", align: left, size: 0.4 }
`))
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Notices != "badge" || tmpl.Blocks[0].Header.Status == nil || !tmpl.Blocks[0].Header.PageIndicator {
		t.Fatalf("header not parsed: %+v", tmpl.Blocks[0].Header)
	}
	if got := tmpl.Blocks[1].Tiles.Items[0].Detail; got != "{{ power .solar.current.latest.values.pv_power }}" {
		t.Fatalf("tile detail: %q", got)
	}
	if len(tmpl.Blocks[2].Columns.Items) != 2 || tmpl.Blocks[2].Columns.Items[0].Block.Bar == nil {
		t.Fatalf("columns not parsed: %+v", tmpl.Blocks[2].Columns)
	}
	if got := tmpl.Blocks[3].Chart.Series[1].Key; got != "load_power" {
		t.Fatalf("series shorthand: %q", got)
	}
	if tmpl.Blocks[3].Chart.YMax != 0 || !tmpl.Blocks[3].Chart.Time {
		t.Fatalf("chart options: %+v", tmpl.Blocks[3].Chart)
	}
	if tmpl.Blocks[4].Text.Value != "Hallo" {
		t.Fatalf("text object: %+v", tmpl.Blocks[4].Text)
	}
}

func TestParseNewValidationErrors(t *testing.T) {
	cases := map[string]string{
		"badge without status": `
notices: badge
rows: [1]
blocks:
  - header: { title: Solar }
`,
		"nested columns": `
rows: [1]
blocks:
  - columns:
      - weight: 1
        block:
          columns:
            - { weight: 1, block: { text: a } }
            - { weight: 1, block: { text: b } }
      - { weight: 1, block: { text: c } }
`,
		"one column": `
rows: [1]
blocks:
  - columns:
      - { weight: 1, block: { text: a } }
`,
		"bad weight": `
rows: [1]
blocks:
  - tiles:
      items:
        - { label: PV, value: solar.current, label_weight: heavy }
`,
		"bad chart style": `
rows: [1]
blocks:
  - chart:
      points: solar.history.points
      series: [{ key: pv_power, style: bars }]
`,
		"bad bar range": `
rows: [1]
blocks:
  - bar: { value: solar.current, min: 10, max: 5 }
`,
	}
	for name, yaml := range cases {
		if _, err := Parse(strings.NewReader(yaml)); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

func TestColumnsSequenceShorthand(t *testing.T) {
	tmpl, err := Parse(strings.NewReader(`
rows: [1]
blocks:
  - columns:
      - { weight: 30, block: { text: "L" } }
      - { weight: 70, block: { text: "R" } }
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(tmpl.Blocks[0].Columns.Items) != 2 {
		t.Fatalf("columns: %+v", tmpl.Blocks[0].Columns)
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
