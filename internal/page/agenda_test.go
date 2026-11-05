package page

import (
	"image"
	"reflect"
	"strings"
	"testing"
	"time"
)

func agendaRenderer(now time.Time, language string) *renderer {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	return &renderer{t: &Template{}, root: map[string]any{}, now: now,
		f: formatter{comma: language == "de", loc: berlin}, language: language}
}

func agendaEvt(start, end string, allDay bool, title string) map[string]any {
	return map[string]any{"start": start, "end": end, "all_day": allDay, "title": title, "location": "Ort"}
}

func lineTexts(lines []agendaLine) []string {
	var out []string
	for _, l := range lines {
		if l.heading {
			out = append(out, "# "+l.text)
		} else {
			out = append(out, strings.TrimSpace(l.when+" "+l.text))
		}
	}
	return out
}

func TestAgendaLines(t *testing.T) {
	// 2026-11-05 10:00 in Berlin, a Thursday.
	r := agendaRenderer(time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC), "de")
	list := []any{
		agendaEvt("2026-11-05T13:00:00Z", "2026-11-05T14:00:00Z", false, "Später"),
		agendaEvt("2026-11-05T07:00:00Z", "2026-11-05T08:00:00Z", false, "Vorbei"),
		agendaEvt("2026-11-04T19:00:00Z", "2026-11-05T11:00:00Z", false, "Nachtschicht"),
		agendaEvt("2026-11-04T23:00:00Z", "2026-11-05T23:00:00Z", true, "Feiertag"),
		agendaEvt("2026-11-06T07:00:00Z", "2026-11-06T08:00:00Z", false, "Morgens"),
		agendaEvt("2026-11-09T07:00:00Z", "2026-11-09T08:00:00Z", false, "Montag"),
		"kein Objekt",
		map[string]any{"title": "ohne Start"},
	}
	events := r.agendaEvents(list)

	lines, hidden := r.agendaLines(events, &Agenda{Lines: 10})
	want := []string{"# Heute", "ganztägig Feiertag", "bis 12:00 Nachtschicht", "14:00 Später",
		"# Morgen", "08:00 Morgens", "# Mo, 9.11.", "08:00 Montag"}
	if !reflect.DeepEqual(lineTexts(lines), want) || hidden != 0 {
		t.Fatalf("got %q hidden %d", lineTexts(lines), hidden)
	}

	// Five lines: the heading of Monday would dangle and is dropped as well.
	lines, hidden = r.agendaLines(events, &Agenda{Lines: 7})
	if got := lineTexts(lines); len(got) != 6 || got[5] != "08:00 Morgens" || hidden != 1 {
		t.Fatalf("overflow: %q hidden %d", got, hidden)
	}
	lines, hidden = r.agendaLines(events, &Agenda{Lines: 4})
	if got := lineTexts(lines); len(got) != 3 || hidden != 3 {
		t.Fatalf("overflow: %q hidden %d", got, hidden)
	}

	off := false
	lines, _ = r.agendaLines(events, &Agenda{Lines: 10, Headings: &off, Location: true})
	want = []string{"ganztägig Feiertag · Ort", "bis 12:00 Nachtschicht · Ort", "14:00 Später · Ort",
		"Fr 08:00 Morgens · Ort", "Mo 08:00 Montag · Ort"}
	if !reflect.DeepEqual(lineTexts(lines), want) {
		t.Fatalf("without headings: %q", lineTexts(lines))
	}

	en := agendaRenderer(time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC), "en")
	lines, _ = en.agendaLines(en.agendaEvents(list), &Agenda{Lines: 10})
	if got := lineTexts(lines); got[0] != "# Today" || got[1] != "all day Feiertag" || got[6] != "# Mon, Nov 9" {
		t.Fatalf("english: %q", got)
	}
}

func TestAgendaParseAndValidate(t *testing.T) {
	tmpl, err := Parse(strings.NewReader("rows: [1]\nblocks:\n  - agenda: calendar.events\n"))
	if err != nil {
		t.Fatal(err)
	}
	a := tmpl.Blocks[0].Agenda
	if a == nil || a.Events != "calendar/events" || a.Lines != 6 {
		t.Fatalf("shorthand: %+v", a)
	}
	for _, bad := range []string{
		"rows: [1]\nblocks:\n  - agenda: {}\n",
		"rows: [1]\nblocks:\n  - agenda: { events: x, lines: 40 }\n",
		"rows: [1]\nblocks:\n  - agenda: { events: x, rule: -1 }\n",
		"rows: [1]\nblocks:\n  - agenda: { events: x, colour: red }\n",
	} {
		if _, err := Parse(strings.NewReader(bad)); err == nil {
			t.Errorf("expected an error for %q", bad)
		}
	}
}

func TestAgendaRendersEmptyAndMissing(t *testing.T) {
	ink := func(r *renderer, b Block) int {
		img := newCanvas(400, 200, false)
		r.block(img, image.Rect(0, 0, 400, 200), b)
		n := 0
		for _, p := range img.Pix {
			if p == 0 {
				n++
			}
		}
		return n
	}
	block := Block{Agenda: &Agenda{Events: "calendar/events", Lines: 5}}
	r := agendaRenderer(time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC), "de")
	if ink(r, block) == 0 {
		t.Fatal("missing list must draw a dash")
	}
	r.root = map[string]any{"calendar": map[string]any{"events": []any{}}}
	if ink(r, block) == 0 || r.err != nil {
		t.Fatalf("empty list must draw the empty text: %v", r.err)
	}
}
