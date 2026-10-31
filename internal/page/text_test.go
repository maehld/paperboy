package page

import (
	"strings"
	"testing"
	"time"
)

func TestTextRender(t *testing.T) {
	now := time.Date(2026, 10, 21, 11, 12, 30, 0, time.UTC)
	doc := testDocument(t, &now)
	fillSolar(t, doc, now)
	tmpl, err := ParseText(strings.NewReader(`
watch:
  - { path: solar.current, stale_after: 1m }
lines:
  - "{{ pad 6 \"PV\" }}{{ right 14 (power .solar.current.latest.values.pv_power) }}"
  - "Akku {{ percent .solar.current.latest.values.battery_soc }} Größe"
  - "{{ center 20 \"Mitte\" }}"
`))
	if err != nil {
		t.Fatal(err)
	}
	d := TextDisplay{Columns: 20, Rows: 4, Charset: CharsetHD44780, Language: "de", Timezone: time.UTC}
	lines, err := tmpl.Render(doc, d, Context{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"PV           4,21 kW",
		"Akku 82 % Größe     ",
		"       Mitte        ",
		"                    ",
	}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("lines:\n%q\nwant\n%q", lines, want)
	}

	// A stale watched path marks the last column of the first line.
	now = now.Add(2 * time.Minute)
	lines, err = tmpl.Render(doc, d, Context{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(lines[0], "!") {
		t.Fatalf("stale mark missing: %q", lines[0])
	}
}

func TestTextRenderMissingAndCut(t *testing.T) {
	now := time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC)
	doc := testDocument(t, &now)
	tmpl, err := ParseText(strings.NewReader(`lines: ["Wert {{ power .x.y }} und ein viel zu langer Text"]`))
	if err != nil {
		t.Fatal(err)
	}
	lines, err := tmpl.Render(doc, TextDisplay{Columns: 16, Rows: 2, Charset: CharsetASCII}, Context{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if lines[0] != "Wert - und ein v" || lines[1] != strings.Repeat(" ", 16) {
		t.Fatalf("lines: %q", lines)
	}
	if _, err := tmpl.Render(doc, TextDisplay{Columns: 16, Rows: 0}, Context{Now: now}); err == nil {
		t.Fatal("zero rows accepted")
	}
}

func TestFitValue(t *testing.T) {
	de := formatter{comma: true, loc: time.UTC}
	cases := []struct {
		width int
		kind  string
		v     any
		want  string
	}{
		{8, "energy", 15.5, "15,5 kWh"},
		{7, "energy", 15.5, "15,5kWh"},
		{6, "energy", 15.5, "16 kWh"},
		{5, "energy", 15.5, "16kWh"},
		{4, "energy", 15.5, "15,5"},
		{3, "energy", 15.5, " 16"},
		{1, "energy", 15.5, "#"},
		{5, "percent", 100.0, "100 %"},
		{4, "percent", 100.0, "100%"},
		{3, "percent", 100.0, "100"},
		{6, "percent", 5.0, "   5 %"},
		{7, "power", 4210.0, "4,21 kW"},
		{6, "power", 4210.0, "4,21kW"},
		{5, "power", 4210.0, "4,2kW"},
		{3, "power", 435.0, "435"},
		{5, "power", nil, "    —"},
		{5, "time", "2026-10-21T14:20:00Z", "14:20"},
		{4, "time", "2026-10-21T14:20:00Z", "####"},
		{6, "duration", 12000.0, "######"},
		{7, "duration", 12000.0, "3h20min"},
		{0, "energy", 1.0, ""},
	}
	for _, c := range cases {
		if got := de.fitValue(c.width, c.kind, c.v); got != c.want {
			t.Errorf("fit %d %s %v = %q, want %q", c.width, c.kind, c.v, got, c.want)
		}
	}

	now := time.Now()
	doc := testDocument(t, &now)
	tmpl, err := ParseText(strings.NewReader(`lines: ["{{ fit 4 \"weird\" 1 }}"]`))
	if err == nil {
		_, err = tmpl.Render(doc, TextDisplay{Columns: 8, Rows: 1}, Context{Now: now})
	}
	if err == nil {
		t.Fatal("unknown format accepted")
	}
}

func TestFitAligned(t *testing.T) {
	de := formatter{comma: true, loc: time.UTC}
	cases := []struct {
		width, unit int
		kind        string
		v           any
		want        string
	}{
		{7, 3, "energy", 5.7, "5,7 kWh"},
		{7, 3, "energy", 15.5, " 16 kWh"},
		{7, 3, "percent", 0.0, "  0 %  "},
		{7, 3, "percent", 100.0, "100 %  "},
		{7, 3, "energy", 1234.0, "### kWh"},
		{7, 2, "power", 515.0, " 515 W "},
		{7, 2, "power", 4210.0, "4,21 kW"},
		{6, 2, "power", 4210.0, "4,2 kW"},
		{7, 3, "energy", nil, "  —    "},
		{7, 3, "time", "2026-10-21T14:20:00Z", "###    "},
		{3, 3, "energy", 1.0, "###"},
	}
	for _, c := range cases {
		if got := de.fitAligned(c.width, c.unit, c.kind, c.v); got != c.want {
			t.Errorf("fit %d %s %v %d = %q, want %q", c.width, c.kind, c.v, c.unit, got, c.want)
		}
	}
}

func TestTextParseErrors(t *testing.T) {
	for name, src := range map[string]string{
		"blocks":      "rows: [1]\nblocks: [{text: x}]\n",
		"no lines":    "title: x\n",
		"bad notices": "notices: bar\nlines: [x]\n",
		"bad func":    "lines: [\"{{ nope }}\"]\n",
		"bad watch":   "watch: [{path: a}]\nlines: [x]\n",
	} {
		if _, err := ParseText(strings.NewReader(src)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	now := time.Now()
	doc := testDocument(t, &now)
	tmpl, _ := ParseText(strings.NewReader("lines: [a, b, c]\n"))
	if _, err := tmpl.Render(doc, TextDisplay{Columns: 8, Rows: 2}, Context{Now: now}); err == nil {
		t.Fatal("too many lines accepted")
	}
}

func TestConvert(t *testing.T) {
	cases := []struct{ charset, in, want string }{
		{CharsetUTF8, "Größe 5 °C — €", "Größe 5 °C — €"},
		{CharsetASCII, "Größe 5 °C — €", "Groesse 5 oC - EUR"},
		{CharsetHD44780, "Größe Übel 5 °C — € ☃", "Größe Ubel 5 °C - EUR ?"},
	}
	for _, c := range cases {
		if got := Convert(c.charset, c.in); got != c.want {
			t.Errorf("%s: %q, want %q", c.charset, got, c.want)
		}
	}
}

func TestNoticeLines(t *testing.T) {
	got := NoticeLines(TextDisplay{Columns: 10, Rows: 3, Charset: CharsetASCII}, "Kein Plan", "Für dieses Gerät fehlt etwas")
	want := []string{"Kein Plan ", "Fuer      ", "dieses    "}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%q", got)
	}
}

// TestTextReferencePage renders the text reference page from the golden data.
func TestTextReferencePage(t *testing.T) {
	doc := goldenDocument(t)
	tmpl, err := LoadText("../../examples/pages", "solar-overview", "lcd2004")
	if err != nil {
		t.Fatal(err)
	}
	lines, err := tmpl.Render(doc, TextDisplay{Columns: 20, Rows: 4, Charset: CharsetHD44780, Language: "de", Timezone: time.UTC},
		Context{Now: fixedGoldenNow})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"PV   4,21 kW   14:20",
		"Akku 1,24 kW  82 %  ",
		"Netz 1,33 kW 1,2 kWh",
		"Heute       12,4 kWh",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("lines:\n%s", strings.Join(lines, "\n"))
	}
}
