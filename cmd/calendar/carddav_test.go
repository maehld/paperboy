package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

// fakeContacts mimics iCloud's CardDAV chain: well-known, principal,
// addressbook home and two addressbooks. The same contact appears in both, as
// iCloud returns "All Contacts" and a group.
type fakeContacts struct {
	server *httptest.Server
	books  map[string]string // path -> vCards
}

func newFakeContacts(t *testing.T) *fakeContacts {
	t.Helper()
	f := &fakeContacts{books: map[string]string{}}
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte("me@icloud.com:app-pass"))
	check := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != auth {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		switch {
		case r.Method == "PROPFIND" && r.URL.Path == "/.well-known/carddav":
			http.Redirect(w, r, "/", http.StatusMovedPermanently)
		case r.Method == "PROPFIND" && r.URL.Path == "/":
			w.WriteHeader(http.StatusMultiStatus)
			fmt.Fprint(w, `<?xml version="1.0"?><multistatus xmlns="DAV:"><response><href>/</href><propstat><prop><current-user-principal><href>/1234/principal/</href></current-user-principal></prop><status>HTTP/1.1 200 OK</status></propstat></response></multistatus>`)
		case r.Method == "PROPFIND" && r.URL.Path == "/1234/principal/":
			w.WriteHeader(http.StatusMultiStatus)
			fmt.Fprint(w, `<?xml version="1.0"?><multistatus xmlns="DAV:" xmlns:c="urn:ietf:params:xml:ns:carddav"><response><href>/1234/principal/</href><propstat><prop><c:addressbook-home-set><href>/1234/addressbooks/</href></c:addressbook-home-set></prop><status>HTTP/1.1 200 OK</status></propstat></response></multistatus>`)
		case r.Method == "PROPFIND" && r.URL.Path == "/1234/addressbooks/":
			if r.Header.Get("Depth") != "1" {
				t.Errorf("addressbooks depth %q", r.Header.Get("Depth"))
			}
			w.WriteHeader(http.StatusMultiStatus)
			fmt.Fprint(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:carddav">
<d:response><d:href>/1234/addressbooks/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
<d:response><d:href>/1234/addressbooks/all/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/><c:addressbook/></d:resourcetype><d:displayname>All Contacts</d:displayname></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
<d:response><d:href>/1234/addressbooks/friends/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/><c:addressbook/></d:resourcetype><d:displayname>Friends</d:displayname></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
<d:response><d:href>/1234/addressbooks/cal/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/><c1:calendar xmlns:c1="urn:ietf:params:xml:ns:caldav"/></d:resourcetype><d:displayname>Calendar</d:displayname></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
</d:multistatus>`)
		case r.Method == "REPORT":
			data, ok := f.books[r.URL.Path]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusMultiStatus)
			fmt.Fprintf(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:carddav"><d:response><d:href>%scard.vcf</d:href><d:propstat><d:prop><c:address-data>%s</c:address-data></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`,
				r.URL.Path, html.EscapeString(data))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func vcard(lines ...string) string {
	return "BEGIN:VCARD\r\nVERSION:3.0\r\n" + strings.Join(lines, "\r\n") + "\r\nEND:VCARD\r\n"
}

func TestCollectFromCardDAV(t *testing.T) {
	f := newFakeContacts(t)
	f.books["/1234/addressbooks/all/"] = vcard(
		"UID:anna", "FN:Anna Beispiel", "BDAY:1985-11-05",
	) + vcard(
		"UID:ben", "N:;Ben;;;", "BDAY:1990-12-03",
	) + vcard(
		"UID:bad", "FN:Bad Date", "BDAY:not-a-date",
	) + vcard(
		"UID:nameless", "BDAY:1980-01-02",
	) + vcard(
		"UID:nobday", "FN:No Birthday",
	)
	f.books["/1234/addressbooks/friends/"] = vcard(
		"UID:anna", "FN:Anna Beispiel", "BDAY:1985-11-05",
	) + vcard(
		"UID:noyear", "FN:No Year", "BDAY:--11-20",
	) + vcard(
		"UID:clara", "FN:Clara", "BDAY:1988-02-29",
	)

	o := options{carddav: f.server.URL, user: "me@icloud.com", password: "app-pass",
		days: 30, maxEvents: 100, language: "de", location: berlin}
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, berlin)
	b, warnings, err := collect(context.Background(), o, newClient(o.user, o.password), now)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if !reflect.DeepEqual(b.Calendars, []string{"Kontakte"}) {
		t.Fatalf("calendars %v", b.Calendars)
	}
	var got []string
	for _, e := range b.Events {
		got = append(got, e.Date+" "+e.Title)
		if !e.Birthday || e.Calendar != "Kontakte" {
			t.Errorf("not a CardDAV birthday: %+v", e)
		}
	}
	want := []string{"2026-11-05 Anna Beispiel", "2026-11-20 No Year", "2026-12-03 Ben"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events\n got %v\nwant %v", got, want)
	}
	// The year of birth survives for the age; a birthday without a year has
	// no age.
	anna := b.Events[0]
	if anna.First != "1985-11-04T23:00:00Z" {
		t.Errorf("first %q", anna.First)
	}
	if no := b.Events[1]; no.First != no.Start {
		t.Errorf("no-year first %q, start %q", no.First, no.Start)
	}
	if got := strings.Join(warnings, "\n"); !strings.Contains(got, `contact "Bad Date": BDAY`) ||
		!strings.Contains(got, "birthday without a name") {
		t.Errorf("warnings %q", got)
	}
	if strings.Contains(strings.Join(warnings, "\n"), "No Birthday") {
		t.Errorf("a contact without a birthday must stay quiet: %v", warnings)
	}
}

func TestCardDAVBirthdaysWidget(t *testing.T) {
	f := newFakeContacts(t)
	f.books["/1234/addressbooks/all/"] = vcard(
		"UID:anna", "FN:Anna Beispiel", "BDAY:1985-11-05",
	)
	f.books["/1234/addressbooks/friends/"] = ""
	o := options{carddav: f.server.URL, user: "me@icloud.com", password: "app-pass",
		days: 30, maxEvents: 100, language: "de", location: berlin}
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, berlin)
	b, _, err := collect(context.Background(), o, newClient(o.user, o.password), now)
	if err != nil {
		t.Fatal(err)
	}
	p := &calendarPlugin{loc: berlin, lang: "de", now: now, events: b.Events, calendars: b.Calendars}
	p.opts.days = 30
	// No birthday_calendars: the CardDAV flag alone marks the event.
	if got, want := birthdayText(t, p, map[string]any{"days": 30}, 4), []string{"Heute:", "Anna Beispiel (41)"}; !reflect.DeepEqual(got, want) {
		t.Errorf("birthdays = %q, want %q", got, want)
	}
}

func TestParseVCardsTolerant(t *testing.T) {
	cards, warnings := parseVCards([]byte("BEGIN:VCARD\r\nVERSION:3.0\r\nUID:x\r\nbroken-line\r\nFN:Anna\r\nBDAY:1990-01-02\r\nEND:VCARD\r\n"))
	if len(cards) != 1 || len(warnings) != 1 {
		t.Fatalf("cards %d, warnings %v", len(cards), warnings)
	}
	if p, ok := cards[0].prop("BDAY"); !ok || p.Value != "1990-01-02" {
		t.Fatalf("BDAY after the broken line: %+v", cards[0])
	}
}

func TestParseBDAY(t *testing.T) {
	cases := []struct {
		in     string
		params map[string]string
		year   int
		month  time.Month
		day    int
		ok     bool
	}{
		{"19850412", nil, 1985, time.April, 12, true},
		{"1985-04-12", nil, 1985, time.April, 12, true},
		{"1985-04-12T00:00:00Z", nil, 1985, time.April, 12, true},
		{"--04-12", nil, 0, time.April, 12, true},
		{"1604-04-12", map[string]string{"X-APPLE-OMIT-YEAR": "1604"}, 0, time.April, 12, true},
		// Some exports drop the parameter but keep Apple's placeholder year.
		{"1604-04-12", nil, 0, time.April, 12, true},
		{"--02-29", nil, 0, time.February, 29, true},
		{"1985-02-30", nil, 0, 0, 0, false},
		{"not-a-date", nil, 0, 0, 0, false},
		{"", nil, 0, 0, 0, false},
	}
	for _, c := range cases {
		year, month, day, err := parseBDAY(property{Value: c.in, Params: c.params})
		if (err == nil) != c.ok || (c.ok && (year != c.year || month != c.month || day != c.day)) {
			t.Errorf("%q: got %d %s %d, %v", c.in, year, month, day, err)
		}
	}
}

func TestBirthdayOccurrencesLeapDay(t *testing.T) {
	list := []contactBirthday{{name: "Lisa", year: 1988, month: time.February, day: 29}}
	for _, c := range []struct {
		year int
		want string
	}{
		{2027, "2027-02-28"},
		{2028, "2028-02-29"},
	} {
		from := time.Date(c.year, 2, 1, 0, 0, 0, 0, berlin)
		got := birthdayOccurrences(list, "Kontakte", from, from.AddDate(0, 1, 0), berlin)
		if len(got) != 1 || got[0].Start.Format("2006-01-02") != c.want {
			t.Errorf("%d: %+v", c.year, got)
		}
		if got[0].First.Format("2006-01-02") != "1988-02-29" {
			t.Errorf("%d: first %v", c.year, got[0].First)
		}
	}
}

func TestParseConfigCardDAV(t *testing.T) {
	cfg := plugin.Values{"carddav": "https://contacts.icloud.com", "user": "me"}
	if _, err := parseConfig(cfg, "de", berlin); err == nil || !strings.Contains(err.Error(), "password") {
		t.Fatalf("missing password: %v", err)
	}
	cfg["password"] = "pw"
	o, err := parseConfig(cfg, "de", berlin)
	if err != nil || o.carddav != "https://contacts.icloud.com" {
		t.Fatalf("%+v %v", o, err)
	}
	cfg["calendars"] = []any{"Privat"}
	if _, err := parseConfig(cfg, "de", berlin); err == nil || !strings.Contains(err.Error(), "calendars needs caldav") {
		t.Fatalf("calendars without caldav: %v", err)
	}
}
