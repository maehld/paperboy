package main

import (
	"context"
	"errors"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseRSS2(t *testing.T) {
	entries, err := parseFeed(fixture(t, "rss2.xml"), "Example")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if entries[0].ID != "urn:2" || entries[1].ID != "urn:1" {
		t.Errorf("order: got %q, %q", entries[0].ID, entries[1].ID)
	}
	if entries[1].Title != "First & best" {
		t.Errorf("CDATA title: %q", entries[1].Title)
	}
	if want := "A summary with & an entity."; entries[1].Summary != want {
		t.Errorf("summary: %q, want %q", entries[1].Summary, want)
	}
	if want := "Plain HTML body"; entries[0].Summary != want {
		t.Errorf("html summary: %q, want %q", entries[0].Summary, want)
	}
	if !entries[2].Time.IsZero() {
		t.Errorf("entry without a date got %v", entries[2].Time)
	}
	if got := entries[1].Time.UTC().Format(time.RFC3339); got != "2026-11-10T07:30:00Z" {
		t.Errorf("pubDate: %s", got)
	}
	if entries[0].Source != "Example" {
		t.Errorf("source: %q", entries[0].Source)
	}
}

func TestParseAtom(t *testing.T) {
	entries, err := parseFeed(fixture(t, "atom.xml"), "Atom")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Title != "Content only" {
		t.Errorf("newest first: %q", entries[0].Title)
	}
	if entries[0].Summary != "The content" {
		t.Errorf("content falls back to summary: %q", entries[0].Summary)
	}
	if entries[0].Link != "https://example.org/a2" {
		t.Errorf("link: %q", entries[0].Link)
	}
	if entries[1].Title != "Atom title" || entries[1].Summary != "Short summary" {
		t.Errorf("html title/summary: %q / %q", entries[1].Title, entries[1].Summary)
	}
	if entries[1].Link != "https://example.org/a1" {
		t.Errorf("alternate link: %q", entries[1].Link)
	}
	if got := entries[1].Time.UTC().Format(time.RFC3339); got != "2026-11-10T10:00:00Z" {
		t.Errorf("published: %s", got)
	}
}

func TestParseRDF(t *testing.T) {
	entries, err := parseFeed(fixture(t, "rdf.xml"), "RDF")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Title != "RDF two" {
		t.Errorf("dc:date order: newest is %q", entries[0].Title)
	}
	if got := entries[0].Time.UTC().Format(time.RFC3339); got != "2026-11-10T12:00:00Z" {
		t.Errorf("dc:date: %s", got)
	}
}

func TestParseLatin1(t *testing.T) {
	entries, err := parseFeed(fixture(t, "latin1.xml"), "Latin")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if want := "Grüße aus Münster"; entries[0].Title != want {
		t.Errorf("title: %q, want %q", entries[0].Title, want)
	}
}

func TestParseFeedRejectsGarbage(t *testing.T) {
	if _, err := parseFeed([]byte("not xml"), "x"); err == nil {
		t.Fatal("want an error for a broken document")
	}
	if _, err := parseFeed([]byte(`<?xml version="1.0"?><rss><channel><title>t</title></channel></rss>`), "x"); err != nil {
		t.Fatalf("empty feed: %v", err)
	}
}

func TestParseFeeds(t *testing.T) {
	feeds, err := parseFeeds([]any{
		map[string]any{"name": "Alpha", "url": "https://alpha.example/feed"},
		"https://www.beta.example/rss",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 2 || feeds[0].Name != "Alpha" || feeds[1].Name != "beta.example" {
		t.Fatalf("feeds: %+v", feeds)
	}

	cases := []struct {
		name string
		in   any
	}{
		{"empty", []any{}},
		{"not a list", "https://example.org/feed"},
		{"missing url", []any{map[string]any{"name": "A"}}},
		{"bad scheme", []any{map[string]any{"name": "A", "url": "ftp://example.org/f"}}},
		{"unknown key", []any{map[string]any{"name": "A", "url": "https://example.org/f", "token": "x"}}},
		{"duplicate", []any{
			map[string]any{"name": "A", "url": "https://one.example/f"},
			map[string]any{"name": "A", "url": "https://two.example/f"},
		}},
	}
	for _, c := range cases {
		if _, err := parseFeeds(c.in); err == nil {
			t.Errorf("%s: want an error", c.name)
		}
	}
}

const rssOne = `<?xml version="1.0"?><rss version="2.0"><channel><title>t</title>
<item><title>One</title><link>https://example.org/1</link><guid>1</guid>
<pubDate>Mon, 10 Nov 2026 08:00:00 +0000</pubDate></item></channel></rss>`

const rssTwo = `<?xml version="1.0"?><rss version="2.0"><channel><title>t</title>
<item><title>One</title><link>https://example.org/1</link><guid>1</guid>
<pubDate>Mon, 10 Nov 2026 08:00:00 +0000</pubDate></item>
<item><title>Two</title><link>https://example.org/2</link><guid>2</guid>
<pubDate>Mon, 10 Nov 2026 09:00:00 +0000</pubDate></item></channel></rss>`

func TestFetchIsConditional(t *testing.T) {
	var bodies = []string{rssOne, rssTwo}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := min(calls, len(bodies)-1)
		etag := `"v` + string(rune('1'+i)) + `"`
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		io.WriteString(w, bodies[i])
	}))
	defer srv.Close()

	p := &rss{client: srv.Client()}
	fd := &feedData{spec: feedSpec{Name: "A", URL: srv.URL}}
	if err := p.fetch(context.Background(), fd); err != nil {
		t.Fatal(err)
	}
	if len(fd.entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(fd.entries))
	}

	calls = 1
	if err := p.fetch(context.Background(), fd); err != nil {
		t.Fatal(err)
	}
	if len(fd.entries) != 2 {
		t.Fatalf("changed feed: got %d entries, want 2", len(fd.entries))
	}

	// The same validators now answer 304, so the entries stay.
	if err := p.fetch(context.Background(), fd); err != nil {
		t.Fatal(err)
	}
	if len(fd.entries) != 2 {
		t.Fatalf("304 dropped entries: %d", len(fd.entries))
	}
}

func TestFetchKeepsEntriesOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, rssOne)
	}))
	p := &rss{client: srv.Client()}
	fd := &feedData{spec: feedSpec{Name: "A", URL: srv.URL}}
	if err := p.fetch(context.Background(), fd); err != nil {
		t.Fatal(err)
	}
	srv.Close() // the next fetch fails
	if err := p.fetch(context.Background(), fd); err == nil {
		t.Fatal("want an error after the server closed")
	}
	if len(fd.entries) != 1 {
		t.Fatalf("a failed fetch dropped the entries")
	}
}

func TestReloadReportsChanges(t *testing.T) {
	bodies := []string{rssOne, rssOne, rssTwo}
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := min(call, len(bodies)-1)
		io.WriteString(w, bodies[i])
	}))
	defer srv.Close()

	p := &rss{client: srv.Client(), refresh: time.Minute, feeds: []feedSpec{{Name: "A", URL: srv.URL}}}
	if changed, failed := p.reload(context.Background()); !changed || failed {
		t.Fatalf("first load: changed=%v failed=%v", changed, failed)
	}
	call = 1
	if changed, failed := p.reload(context.Background()); changed || failed {
		t.Fatalf("unchanged reload: changed=%v failed=%v", changed, failed)
	}
	call = 2
	if changed, _ := p.reload(context.Background()); !changed {
		t.Fatal("a new entry did not report a change")
	}
}

func TestSnapshotSelectsFeeds(t *testing.T) {
	data := []*feedData{
		{spec: feedSpec{Name: "A"}, entries: []entry{
			{Source: "A", Title: "old", Time: time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)},
		}},
		{spec: feedSpec{Name: "B"}, err: errors.New("boom")},
	}
	snap := buildSnapshot(data)

	all, err := snap.entries("all")
	if err != nil || len(all) != 1 {
		t.Fatalf("all: %v %v", all, err)
	}
	if got, err := snap.entries("A"); err != nil || len(got) != 1 {
		t.Fatalf("A: %v %v", got, err)
	}
	if _, err := snap.entries("B"); err == nil {
		t.Error("a failed feed without entries must report its error")
	}
	if _, err := snap.entries("nope"); err == nil {
		t.Error("an unknown feed must be an error")
	}
}

func testPlugin(t *testing.T) *rss {
	t.Helper()
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	entries := []entry{
		{Source: "A", Title: "Bundestag beschließt Reform des Klimageldes", Summary: "Die Entlastung soll ab Januar ausgezahlt werden.", ID: "1", Time: now.Add(-12 * time.Minute)},
		{Source: "A", Title: "Tarifrunde vertagt", ID: "2", Time: now.Add(-3 * time.Hour)},
		{Source: "B", Title: "Linux 6.14 freigegeben", ID: "3", Time: now.Add(-26 * time.Hour)},
	}
	return &rss{
		feeds:   []feedSpec{{Name: "A"}, {Name: "B"}},
		refresh: time.Minute,
		snap: snapshot{
			merged: sortEntries(entries),
			byFeed: map[string][]entry{"A": entries[:2], "B": entries[2:]},
			errs:   map[string]error{},
		},
	}
}

func renderWidget(t *testing.T, p *rss, w plugin.WidgetRequest, width, height int) *image.Paletted {
	t.Helper()
	req := &plugin.Request{
		Version: plugin.Version, Action: plugin.ActionRender,
		Now: time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC), Language: "de", Timezone: "Europe/Berlin",
		Levels: 4,
	}
	img := render.NewCanvas(width, height, 4)
	if err := p.Render(req, w, img); err != nil {
		t.Fatal(err)
	}
	return img
}

func ink(img *image.Paletted) int {
	white := uint8(len(img.Palette) - 1)
	n := 0
	for _, px := range img.Pix {
		if px != white {
			n++
		}
	}
	return n
}

func TestRenderFeed(t *testing.T) {
	p := testPlugin(t)
	img := renderWidget(t, p, plugin.WidgetRequest{
		Widget: "feed", Width: 800, Height: 240,
		Args:  map[string]any{"feed": "all", "summary": true},
		Style: plugin.Style{Padding: 8, Rule: 1, TextPx: 20},
	}, 800, 240)
	if ink(img) == 0 {
		t.Fatal("drew nothing")
	}

	// A two-level canvas must not lose its secondary text.
	req := &plugin.Request{Version: plugin.Version, Now: time.Now(), Language: "en"}
	mono := render.NewCanvas(400, 200, 2)
	if err := p.Render(req, plugin.WidgetRequest{
		Widget: "feed", Width: 400, Height: 200,
		Args:  map[string]any{"feed": "A", "summary": true},
		Style: plugin.Style{Padding: 6, Rule: 1, TextPx: 18},
	}, mono); err != nil {
		t.Fatal(err)
	}
	if ink(mono) == 0 {
		t.Fatal("mono: drew nothing")
	}
}

func TestRenderUnknownFeed(t *testing.T) {
	p := testPlugin(t)
	img := render.NewCanvas(400, 200, 4)
	err := p.Render(&plugin.Request{Version: plugin.Version, Language: "de"}, plugin.WidgetRequest{
		Widget: "feed", Width: 400, Height: 200,
		Args:  map[string]any{"feed": "nope"},
		Style: plugin.Style{Padding: 4, TextPx: 16},
	}, img)
	if err == nil || !strings.Contains(err.Error(), "unknown feed") {
		t.Fatalf("got %v", err)
	}
}

func TestRenderHeadline(t *testing.T) {
	p := testPlugin(t)
	img := renderWidget(t, p, plugin.WidgetRequest{
		Widget: "headline", Width: 800, Height: 320,
		Args:  map[string]any{"feed": "A"},
		Style: plugin.Style{Padding: 8, Rule: 1, TextPx: 20},
	}, 800, 320)
	if ink(img) == 0 {
		t.Fatal("drew nothing")
	}

	// Too small for the summary, but the title still has to appear.
	small := renderWidget(t, p, plugin.WidgetRequest{
		Widget: "headline", Width: 200, Height: 60,
		Style: plugin.Style{Padding: 4, TextPx: 16},
	}, 200, 60)
	if ink(small) == 0 {
		t.Fatal("small headline drew nothing")
	}
}

func TestTextFeed(t *testing.T) {
	p := testPlugin(t)
	req := &plugin.Request{
		Version: plugin.Version, Now: time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC), Language: "de",
	}
	lines, err := p.Text(req, plugin.WidgetRequest{
		Widget: "feed", Width: 20, Height: 4,
		Args: map[string]any{"feed": "all"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	for _, line := range lines {
		if len([]rune(line)) > 20 {
			t.Errorf("line longer than 20: %q", line)
		}
	}
	if !strings.HasPrefix(lines[0], "12m A:") {
		t.Errorf("first line: %q", lines[0])
	}
	if !strings.Contains(lines[1], "3h") {
		t.Errorf("second line: %q", lines[1])
	}
	if !strings.Contains(lines[2], "B:") {
		t.Errorf("mixed list should name the source: %q", lines[2])
	}
}

func TestTextHeadline(t *testing.T) {
	p := testPlugin(t)
	req := &plugin.Request{
		Version: plugin.Version, Now: time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC), Language: "en",
	}
	lines, err := p.Text(req, plugin.WidgetRequest{
		Widget: "headline", Width: 20, Height: 4,
		Args: map[string]any{"feed": "B"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "B ") {
		t.Fatalf("first line: %v", lines)
	}
	if len(lines) > 4 {
		t.Fatalf("too many lines: %v", lines)
	}
}

func TestRelativeTime(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, loc)
	cases := []struct {
		at   time.Time
		lang string
		want string
	}{
		{now.Add(-30 * time.Second), "de", "gerade eben"},
		{now.Add(-12 * time.Minute), "de", "vor 12 Min"},
		{now.Add(-12 * time.Minute), "en", "12 min ago"},
		{now.Add(-5 * time.Hour), "de", "vor 5 Std"},
		{time.Date(2026, 11, 10, 22, 0, 0, 0, loc), "de", "gestern"},
		{time.Date(2026, 11, 9, 10, 0, 0, 0, loc), "de", "Mo"},
		{time.Date(2026, 11, 9, 10, 0, 0, 0, loc), "en", "Mon"},
		{time.Date(2026, 10, 2, 10, 0, 0, 0, loc), "de", "02.10."},
		{time.Time{}, "de", ""},
	}
	for _, c := range cases {
		req := &plugin.Request{Language: c.lang, Timezone: "Europe/Berlin", Now: now}
		if got := relativeTime(c.at, now, req); got != c.want {
			t.Errorf("%v %s: got %q, want %q", c.at, c.lang, got, c.want)
		}
	}
}

func TestTimeModeRejectsUnknown(t *testing.T) {
	_, err := timeMode(plugin.WidgetRequest{Widget: "feed", Args: map[string]any{"time": "soon"}})
	if err == nil {
		t.Fatal("want an error")
	}
}

// A cell the padding eats up is reported for both widgets, so Paperboy draws
// its notice instead of an empty cell.
func TestTooSmallCell(t *testing.T) {
	p := testPlugin(t)
	req := &plugin.Request{Version: plugin.Version, Now: time.Now(), Language: "de", Levels: 2}
	style := plugin.Style{Padding: 8, Rule: 1, TextPx: 20}
	for _, name := range []string{"feed", "headline"} {
		err := p.Render(req, plugin.WidgetRequest{Widget: name, Width: 10, Height: 10, Style: style},
			render.NewCanvas(10, 10, 2))
		if !errors.Is(err, plugin.ErrTooSmall) {
			t.Errorf("%s in a 10x10 cell: error = %v, want ErrTooSmall", name, err)
		}
	}
}
