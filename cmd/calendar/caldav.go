package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxResponse bounds one CalDAV or iCalendar response.
const maxResponse = 16 << 20

// client talks to a CalDAV server or fetches iCalendar files with optional
// Basic authentication. Redirects are followed by hand: Go's client turns a
// PROPFIND into a GET on 301/302 and drops the credentials on a host change,
// while iCloud moves every account from caldav.icloud.com to its own
// pNN-caldav.icloud.com host.
type client struct {
	http     *http.Client
	user     string
	password string
}

func newClient(user, password string) *client {
	return &client{
		http: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		user:     user,
		password: password,
	}
}

// do sends one request and follows up to five redirects with the same method,
// body and credentials. It returns the final URL with the body.
func (c *client) do(ctx context.Context, method string, u *url.URL, depth string, body []byte) (*url.URL, []byte, int, error) {
	for range 6 {
		req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
		if err != nil {
			return nil, nil, 0, err
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/xml; charset=utf-8")
		}
		if depth != "" {
			req.Header.Set("Depth", depth)
		}
		if c.user != "" || c.password != "" {
			req.SetBasicAuth(c.user, c.password)
		}
		req.Header.Set("User-Agent", "paperboy-calendar")
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, nil, 0, err
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
		resp.Body.Close()
		if err != nil {
			return nil, nil, 0, err
		}
		if len(data) > maxResponse {
			return nil, nil, 0, fmt.Errorf("%s: response exceeds %d bytes", redact(u), maxResponse)
		}
		switch resp.StatusCode {
		case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
			http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
			loc, err := u.Parse(resp.Header.Get("Location"))
			if err != nil || resp.Header.Get("Location") == "" {
				return nil, nil, 0, fmt.Errorf("%s: redirect without a valid location", redact(u))
			}
			u = loc
			continue
		}
		return u, data, resp.StatusCode, nil
	}
	return nil, nil, 0, fmt.Errorf("%s: too many redirects", redact(u))
}

// redact removes credentials and the query from a URL for messages; secret
// calendar links often carry their token there.
func redact(u *url.URL) string {
	c := *u
	c.User = nil
	c.RawQuery = ""
	return c.String()
}

func statusError(u *url.URL, status int) error {
	switch status {
	case http.StatusUnauthorized:
		return fmt.Errorf("%s: authentication failed (HTTP 401); iCloud needs an app-specific password", redact(u))
	case http.StatusForbidden:
		return fmt.Errorf("%s: access denied (HTTP 403)", redact(u))
	}
	return fmt.Errorf("%s: HTTP %d", redact(u), status)
}

// fetchICS downloads an iCalendar file; webcal:// becomes https://.
func (c *client) fetchICS(ctx context.Context, raw string) ([]byte, error) {
	if strings.HasPrefix(strings.ToLower(raw), "webcal://") {
		raw = "https://" + raw[len("webcal://"):]
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("invalid calendar URL")
	}
	final, data, status, err := c.do(ctx, http.MethodGet, u, "", nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, statusError(final, status)
	}
	return data, nil
}

// ---- WebDAV XML ----

type multistatus struct {
	Responses []davResponse `xml:"DAV: response"`
}

type davResponse struct {
	Href      string     `xml:"DAV: href"`
	Propstats []propstat `xml:"DAV: propstat"`
}

type propstat struct {
	Status string  `xml:"DAV: status"`
	Prop   davProp `xml:"DAV: prop"`
}

type davProp struct {
	Principal    hrefProp `xml:"DAV: current-user-principal"`
	Home         hrefProp `xml:"urn:ietf:params:xml:ns:caldav calendar-home-set"`
	DisplayName  string   `xml:"DAV: displayname"`
	ResourceType struct {
		Calendar *struct{} `xml:"urn:ietf:params:xml:ns:caldav calendar"`
	} `xml:"DAV: resourcetype"`
	Components *struct {
		Comps []struct {
			Name string `xml:"name,attr"`
		} `xml:"urn:ietf:params:xml:ns:caldav comp"`
	} `xml:"urn:ietf:params:xml:ns:caldav supported-calendar-component-set"`
	CalendarData string `xml:"urn:ietf:params:xml:ns:caldav calendar-data"`
}

type hrefProp struct {
	Href string `xml:"DAV: href"`
}

// ok reports whether a propstat carries found properties (HTTP 200).
func (p propstat) ok() bool {
	f := strings.Fields(p.Status)
	return len(f) >= 2 && f[1] == "200"
}

// props merges the successful propstats of a response.
func (r davResponse) props() []davProp {
	var out []davProp
	for _, p := range r.Propstats {
		if p.ok() {
			out = append(out, p.Prop)
		}
	}
	return out
}

func (c *client) propfind(ctx context.Context, u *url.URL, depth, body string) (*url.URL, *multistatus, error) {
	final, data, status, err := c.do(ctx, "PROPFIND", u, depth, []byte(body))
	if err != nil {
		return nil, nil, err
	}
	if status != http.StatusMultiStatus {
		return nil, nil, statusError(final, status)
	}
	var ms multistatus
	if err := xml.Unmarshal(data, &ms); err != nil {
		return nil, nil, fmt.Errorf("%s: invalid WebDAV response: %w", redact(final), err)
	}
	return final, &ms, nil
}

const (
	principalQuery = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:"><d:prop><d:current-user-principal/></d:prop></d:propfind>`
	homeQuery = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><c:calendar-home-set/></d:prop></d:propfind>`
	collectionsQuery = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><d:resourcetype/><d:displayname/><c:supported-calendar-component-set/></d:prop></d:propfind>`
)

// calendarRef is one calendar collection found on the server.
type calendarRef struct {
	Name string
	URL  *url.URL
}

// discover finds the calendars of the account: current-user-principal, then
// calendar-home-set, then the calendar collections below it (RFC 4791, RFC
// 5397). A URL that already points at a home or a calendar works as well,
// because missing properties fall back to the URL itself.
func (c *client) discover(ctx context.Context, base *url.URL) ([]calendarRef, error) {
	start := base
	if base.Path == "" || base.Path == "/" {
		wk, _ := base.Parse("/.well-known/caldav")
		if final, ms, err := c.propfind(ctx, wk, "0", principalQuery); err == nil && hrefOf(ms, func(p davProp) string { return p.Principal.Href }) != "" {
			start = final
		}
	}
	final, ms, err := c.propfind(ctx, start, "0", principalQuery)
	if err != nil {
		return nil, err
	}
	principal := final
	if href := hrefOf(ms, func(p davProp) string { return p.Principal.Href }); href != "" {
		if principal, err = final.Parse(href); err != nil {
			return nil, fmt.Errorf("invalid principal URL %q", href)
		}
	}

	final, ms, err = c.propfind(ctx, principal, "0", homeQuery)
	if err != nil {
		return nil, err
	}
	home := final
	if href := hrefOf(ms, func(p davProp) string { return p.Home.Href }); href != "" {
		if home, err = final.Parse(href); err != nil {
			return nil, fmt.Errorf("invalid calendar home URL %q", href)
		}
	}

	final, ms, err = c.propfind(ctx, home, "1", collectionsQuery)
	if err != nil {
		return nil, err
	}
	var out []calendarRef
	for _, r := range ms.Responses {
		var name string
		calendar, events := false, true
		for _, p := range r.props() {
			if p.ResourceType.Calendar != nil {
				calendar = true
			}
			if p.DisplayName != "" {
				name = strings.TrimSpace(p.DisplayName)
			}
			if p.Components != nil && len(p.Components.Comps) > 0 {
				events = false
				for _, comp := range p.Components.Comps {
					if strings.EqualFold(comp.Name, "VEVENT") {
						events = true
					}
				}
			}
		}
		if !calendar || !events {
			continue
		}
		u, err := final.Parse(strings.TrimSpace(r.Href))
		if err != nil {
			continue
		}
		if name == "" {
			name = lastSegment(u.Path)
		}
		out = append(out, calendarRef{Name: name, URL: u})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no calendars with events found", redact(home))
	}
	return out, nil
}

func hrefOf(ms *multistatus, pick func(davProp) string) string {
	for _, r := range ms.Responses {
		for _, p := range r.props() {
			if h := strings.TrimSpace(pick(p)); h != "" {
				return h
			}
		}
	}
	return ""
}

func lastSegment(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		p = p[i+1:]
	}
	if s, err := url.PathUnescape(p); err == nil {
		return s
	}
	return p
}

// events asks one calendar for the events that touch [from, to) with a
// calendar-query REPORT (RFC 4791 7.8). The server returns whole objects,
// recurring masters included; the expansion happens here, because server-side
// expansion is optional and differs between servers.
func (c *client) events(ctx context.Context, cal calendarRef, from, to time.Time) ([]byte, error) {
	body := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:prop><c:calendar-data/></d:prop>
  <c:filter><c:comp-filter name="VCALENDAR"><c:comp-filter name="VEVENT">
    <c:time-range start="%s" end="%s"/>
  </c:comp-filter></c:comp-filter></c:filter>
</c:calendar-query>`, from.UTC().Format("20060102T150405Z"), to.UTC().Format("20060102T150405Z"))
	final, data, status, err := c.do(ctx, "REPORT", cal.URL, "1", []byte(body))
	if err != nil {
		return nil, err
	}
	if status != http.StatusMultiStatus {
		return nil, statusError(final, status)
	}
	var ms multistatus
	if err := xml.Unmarshal(data, &ms); err != nil {
		return nil, fmt.Errorf("%s: invalid WebDAV response: %w", redact(final), err)
	}
	var out bytes.Buffer
	for _, r := range ms.Responses {
		for _, p := range r.props() {
			if strings.TrimSpace(p.CalendarData) != "" {
				out.WriteString(strings.TrimSpace(p.CalendarData))
				out.WriteString("\r\n")
			}
		}
	}
	return out.Bytes(), nil
}
