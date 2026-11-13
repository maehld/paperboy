// The Share API client of the dexcom plugin.

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// shareRegions are the hosts of the Share service. Which one answers does not
// follow the country the sensor sits in: a German account may live on the US
// host, so region is a required configuration value instead of a guess.
var shareRegions = map[string]string{
	"eu": "https://share2.dexcom.com/ShareWebServices/Services",
	"us": "https://shareous1.dexcom.com/ShareWebServices/Services",
}

// errRefused means the service did not accept the credentials. The one-step
// login answers the all-zero session instead of an error, the lookups answer
// null.
var errRefused = errors.New("the credentials were refused")

// shareClient speaks the parts of the Share service this plugin needs.
type shareClient struct {
	base  string
	appID string
	http  *http.Client
}

// reading is one glucose value of the service.
type reading struct {
	at    time.Time
	value float64
	trend string
}

// authenticate exchanges the account name for the account ID. The ID is what
// the login uses, and it does not change, so the plugin stores it.
func (c *shareClient) authenticate(accountName, password string) (string, error) {
	body, err := json.Marshal(map[string]string{
		"accountName":   accountName,
		"password":      password,
		"applicationId": c.appID,
	})
	if err != nil {
		return "", err
	}
	return c.session("/General/AuthenticatePublisherAccount", body)
}

// login answers the session ID for an account ID.
func (c *shareClient) login(accountID, password string) (string, error) {
	body, err := json.Marshal(map[string]string{
		"accountId":     accountID,
		"password":      password,
		"applicationId": c.appID,
	})
	if err != nil {
		return "", err
	}
	return c.session("/General/LoginPublisherAccountById", body)
}

// readings reads the values of the last minutes. The service keeps one day.
func (c *shareClient) readings(session string, minutes, maxCount int) ([]reading, error) {
	query := url.Values{}
	query.Set("sessionId", session)
	query.Set("minutes", strconv.Itoa(minutes))
	query.Set("maxCount", strconv.Itoa(maxCount))
	// The service takes the parameters in the query and an empty body.
	body, err := c.post("/Publisher/ReadPublisherLatestGlucoseValues", query, nil)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		// A session without values answers 200 with an empty body.
		return nil, nil
	}
	return parseReadings(body)
}

// session runs one of the login calls and reads the JSON string it answers.
func (c *shareClient) session(path string, body []byte) (string, error) {
	ans, err := c.post(path, nil, body)
	if err != nil {
		return "", err
	}
	trimmed := strings.TrimSpace(string(ans))
	if trimmed == "" || trimmed == "null" || trimmed == `"null"` {
		return "", errRefused
	}
	var s string
	if err := json.Unmarshal(ans, &s); err != nil {
		return "", fmt.Errorf("unexpected answer %q", truncate(trimmed, 80))
	}
	if s == "" || isZeroGUID(s) {
		return "", errRefused
	}
	return s, nil
}

func (c *shareClient) post(path string, query url.Values, body []byte) ([]byte, error) {
	target := c.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(http.MethodPost, target, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	ans, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, fmt.Errorf("%s", faultText(ans, res.StatusCode))
	}
	return ans, nil
}

// faultText names the reason of a failed call. The service answers a JSON
// object with Code and Message; anything else is cut.
func faultText(body []byte, status int) string {
	var f struct {
		Code    string `json:"Code"`
		Message string `json:"Message"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(body), &f); err == nil && f.Code != "" {
		if f.Message != "" {
			return f.Code + ": " + f.Message
		}
		return f.Code
	}
	text := strings.Trim(strings.TrimSpace(string(body)), `"`)
	if text == "" {
		return fmt.Sprintf("HTTP %d", status)
	}
	return fmt.Sprintf("HTTP %d: %s", status, truncate(text, 200))
}

// isSessionGone reports whether the service forgot the session, which costs
// one new login.
func isSessionGone(err error) bool {
	return err != nil && strings.Contains(err.Error(), "SessionIdNotFound")
}

// parseReadings reads the answer of the readings call: a JSON array, or the
// same array inside a JSON string as some deployments send it.
func parseReadings(body []byte) ([]reading, error) {
	var list []storedReading
	if err := json.Unmarshal(body, &list); err != nil {
		var s string
		if err2 := json.Unmarshal(body, &s); err2 != nil {
			return nil, fmt.Errorf("not a reading list: %w", err)
		}
		if err := json.Unmarshal([]byte(s), &list); err != nil {
			return nil, fmt.Errorf("not a reading list: %w", err)
		}
	}
	out := make([]reading, 0, len(list))
	for _, r := range list {
		at, ok := parseDexDate(r.WT)
		if !ok {
			if at, ok = parseDexDate(r.ST); !ok {
				continue
			}
		}
		out = append(out, reading{at: at, value: r.Value, trend: r.Trend})
	}
	return out, nil
}

// storedReading is one reading in the service's format and in the plugin's
// state. The timestamp is "/Date(<milliseconds>)/", with an optional zone.
type storedReading struct {
	WT    string  `json:"WT"`
	ST    string  `json:"ST"`
	Value float64 `json:"Value"`
	Trend string  `json:"Trend"`
}

// parseDexDate reads "/Date(1700000000000)/", with and without a zone suffix,
// and a plain RFC 3339 timestamp.
func parseDexDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	open := strings.Index(s, "Date(")
	closing := strings.LastIndex(s, ")")
	if open < 0 || closing < open {
		return time.Time{}, false
	}
	inner := s[open+len("Date(") : closing]
	zone := time.UTC
	if i := strings.IndexAny(inner, "+-"); i > 0 {
		sign := 1
		if inner[i] == '-' {
			sign = -1
		}
		z := inner[i+1:]
		if len(z) == 4 {
			var h, m int
			if _, err := fmt.Sscanf(z, "%2d%2d", &h, &m); err == nil {
				zone = time.FixedZone("", sign*(h*3600+m*60))
			}
		}
		inner = inner[:i]
	}
	var ms int64
	if _, err := fmt.Sscanf(inner, "%d", &ms); err != nil {
		return time.Time{}, false
	}
	return time.UnixMilli(ms).In(zone), true
}

// isZeroGUID reports the all-zero GUID the login answers for refused
// credentials.
func isZeroGUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if r != '-' {
				return false
			}
		case r != '0':
			return false
		}
	}
	return true
}

// looksLikeGUID reports whether s has the shape of a GUID. The application ID
// is one, and an account may be given as one.
func looksLikeGUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
			continue
		}
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
