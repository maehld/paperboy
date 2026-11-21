package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// feedSpec is one configured feed.
type feedSpec struct {
	Name string
	URL  string
}

// entry is one feed entry, already reduced to what the widgets show.
type entry struct {
	Source  string
	Title   string
	Summary string
	Link    string
	ID      string
	Time    time.Time
}

// key identifies an entry within a feed; entries without GUID or link fall
// back to their title.
func (e entry) key() string {
	if e.ID != "" {
		return e.ID
	}
	if e.Link != "" {
		return e.Link
	}
	if e.Title != "" {
		return e.Title
	}
	return e.Summary
}

// feedData is one feed as last loaded. entries survive a failed fetch.
type feedData struct {
	spec     feedSpec
	entries  []entry
	err      error
	etag     string
	modified string
}

// fetch loads one feed with a conditional GET. A 304 keeps the entries; any
// error leaves them untouched.
func (p *rss) fetch(ctx context.Context, fd *feedData) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fd.spec.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml;q=0.9, text/xml;q=0.8, */*;q=0.5")
	if fd.etag != "" {
		req.Header.Set("If-None-Match", fd.etag)
	}
	if fd.modified != "" {
		req.Header.Set("If-Modified-Since", fd.modified)
	}
	client := p.client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotModified:
		return nil
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFeedBytes+1))
	if err != nil {
		return err
	}
	if len(body) > maxFeedBytes {
		return fmt.Errorf("response larger than %d bytes", maxFeedBytes)
	}
	entries, err := parseFeed(body, fd.spec.Name)
	if err != nil {
		return err
	}
	fd.entries, fd.err = entries, nil
	fd.etag = resp.Header.Get("ETag")
	fd.modified = resp.Header.Get("Last-Modified")
	return nil
}

// --- parsing ----------------------------------------------------------------

type xmlDoc struct {
	XMLName xml.Name
	Channel struct {
		Title string    `xml:"title"`
		Items []xmlItem `xml:"item"`
	} `xml:"channel"`
	// RSS 1.0 (RDF) puts the items next to the channel.
	Items []xmlItem `xml:"item"`
	// Atom.
	FeedTitle string     `xml:"title"`
	Entries   []xmlEntry `xml:"entry"`
}

type xmlItem struct {
	Title   string `xml:"title"`
	Link    string `xml:"link"`
	Desc    string `xml:"description"`
	Content string `xml:"encoded"`
	PubDate string `xml:"pubDate"`
	Date    string `xml:"date"` // dc:date
	GUID    string `xml:"guid"`
}

type xmlEntry struct {
	Title     string    `xml:"title"`
	Summary   string    `xml:"summary"`
	Content   string    `xml:"content"`
	ID        string    `xml:"id"`
	Published string    `xml:"published"`
	Updated   string    `xml:"updated"`
	Links     []xmlLink `xml:"link"`
}

type xmlLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}

var tagRE = regexp.MustCompile(`(?s)<[^>]*>`)

// parseFeed turns an RSS 2.0, RSS 1.0 or Atom document into entries, newest
// first. Entries keep their feed order when they have no date.
func parseFeed(body []byte, source string) ([]entry, error) {
	d := xml.NewDecoder(bytes.NewReader(body))
	d.Strict = false
	d.Entity = xml.HTMLEntity
	d.CharsetReader = charsetReader
	var doc xmlDoc
	if err := d.Decode(&doc); err != nil {
		return nil, fmt.Errorf("invalid feed: %w", err)
	}

	var out []entry
	add := func(title, summary, link, id, date string) {
		out = append(out, entry{
			Source:  source,
			Title:   clean(title),
			Summary: truncate(clean(summary), 400),
			Link:    strings.TrimSpace(link),
			ID:      strings.TrimSpace(id),
			Time:    parseTime(date),
		})
	}
	for _, it := range append(doc.Channel.Items, doc.Items...) {
		date := firstNonEmpty(it.PubDate, it.Date)
		add(it.Title, firstNonEmpty(it.Desc, it.Content), it.Link, it.GUID, date)
	}
	for _, e := range doc.Entries {
		add(e.Title, firstNonEmpty(e.Summary, e.Content), atomLink(e.Links), e.ID, firstNonEmpty(e.Published, e.Updated))
	}
	if len(out) == 0 {
		if strings.TrimSpace(doc.Channel.Title+doc.FeedTitle) == "" {
			return nil, errors.New("no feed: neither channel nor Atom entries found")
		}
		return nil, nil
	}

	// Titles that end up empty still identify the entry by its link, but an
	// entry without any text is noise (e.g. an Atom feed with only binaries).
	kept := out[:0]
	for _, e := range out {
		if e.Title != "" || e.Summary != "" {
			kept = append(kept, e)
		}
	}
	out = sortEntries(dedupe(kept))
	if len(out) > maxEntries {
		out = out[:maxEntries]
	}
	return out, nil
}

func atomLink(links []xmlLink) string {
	for _, l := range links {
		if l.Rel == "" || l.Rel == "alternate" {
			if l.Href != "" {
				return l.Href
			}
		}
	}
	for _, l := range links {
		if l.Href != "" {
			return l.Href
		}
	}
	return ""
}

// sortEntries orders by date, newest first; entries without a date keep their
// order at the end.
func sortEntries(in []entry) []entry {
	out := append([]entry(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Time, out[j].Time
		if a.IsZero() != b.IsZero() {
			return b.IsZero()
		}
		return a.After(b)
	})
	return out
}

// dedupe keeps the first occurrence of every GUID (or link) in a sorted list.
func dedupe(in []entry) []entry {
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, e := range in {
		k := e.key()
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, e)
	}
	return out
}

// mergedEntries mixes the feeds by date, newest first. Ties keep the order of
// the configured feeds.
func mergedEntries(data []*feedData) []entry {
	var out []entry
	for _, fd := range data {
		out = append(out, fd.entries...)
	}
	return sortEntries(out)
}

// clean turns a feed's HTML fragment into one line of plain text.
func clean(s string) string {
	s = strings.ReplaceAll(s, "\u00a0", " ")
	// RSS descriptions are escaped HTML, Atom content often plain HTML; two
	// passes cover both.
	for range 2 {
		s = html.UnescapeString(s)
		s = tagRE.ReplaceAllString(s, " ")
	}
	return strings.Join(strings.Fields(s), " ")
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	cut := max
	for cut > 0 && r[cut] != ' ' {
		cut--
	}
	if cut < max/2 {
		cut = max
	}
	return strings.TrimRight(string(r[:cut]), " ,;:") + "…"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// dateLayouts are the formats feeds use in practice: RFC 1123 and 822 with
// and without a zone name, RFC 3339 and a few plain variants.
var dateLayouts = []string{
	time.RFC1123Z,
	time.RFC1123,
	time.RFC822Z,
	time.RFC822,
	time.RFC3339Nano,
	time.RFC3339,
	"Mon, 2 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04:05 MST",
	"2 Jan 2006 15:04:05 -0700",
	"2006-01-02T15:04:05Z0700",
	"2006-01-02 15:04:05 -0700",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// --- charsets ---------------------------------------------------------------

// charsetReader converts the charsets that old feeds still declare. XML
// defaults to UTF-8, so the reader only runs for an explicit encoding.
func charsetReader(charset string, input io.Reader) (io.Reader, error) {
	body, err := io.ReadAll(input)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "utf-8", "utf8", "us-ascii", "ascii":
		return bytes.NewReader(body), nil
	case "iso-8859-1", "iso8859-1", "latin1", "latin-1":
		return bytes.NewReader(decodeLatin1(body, nil)), nil
	case "windows-1252", "cp1252":
		return bytes.NewReader(decodeLatin1(body, windows1252)), nil
	}
	return nil, fmt.Errorf("unsupported charset %q", charset)
}

// windows1252 maps the bytes that differ from ISO-8859-1.
var windows1252 = map[byte]rune{
	0x80: '€', 0x82: '‚', 0x83: 'ƒ', 0x84: '„', 0x85: '…', 0x86: '†', 0x87: '‡',
	0x88: 'ˆ', 0x89: '‰', 0x8a: 'Š', 0x8b: '‹', 0x8c: 'Œ', 0x8e: 'Ž',
	0x91: '‘', 0x92: '’', 0x93: '“', 0x94: '”', 0x95: '•', 0x96: '–', 0x97: '—',
	0x98: '˜', 0x99: '™', 0x9a: 'š', 0x9b: '›', 0x9c: 'œ', 0x9e: 'ž', 0x9f: 'Ÿ',
}

func decodeLatin1(in []byte, special map[byte]rune) []byte {
	out := make([]byte, 0, len(in)+len(in)/2)
	for _, c := range in {
		switch {
		case c < 0x80:
			out = append(out, c)
		case special != nil:
			if r, ok := special[c]; ok {
				out = utf8.AppendRune(out, r)
				continue
			}
			out = utf8.AppendRune(out, rune(c))
		default:
			out = utf8.AppendRune(out, rune(c))
		}
	}
	return out
}
