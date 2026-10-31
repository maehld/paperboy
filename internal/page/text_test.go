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
