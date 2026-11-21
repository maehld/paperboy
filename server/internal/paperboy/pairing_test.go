package paperboy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// pair performs what the operator does: read the code from the register answer
// and claim it. The next register then takes the key.
func pair(t *testing.T, s *Server, mac string) {
	t.Helper()
	rec := register(t, s, `{"mac":"`+mac+`"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("register before pairing %s: %d %s", mac, rec.Code, rec.Body)
	}
	code, _ := readChallenge(t, rec.Body.Bytes())
	if code == "" {
		t.Fatalf("no pairing code for %s: %d %s", mac, rec.Code, rec.Body)
	}
	// The claim is a UI route, so it needs the password when the instance has one.
	req := httptest.NewRequest(http.MethodPost, "/api/state/devices/"+mac+"/pair", strings.NewReader(`{"code":"`+code+`"}`))
	req.Header.Set("Content-Type", "application/json")
	if password := s.config().UI.Password; password != "" {
		req.SetBasicAuth("", password)
	}
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("pairing %s: %d %s", mac, rec.Code, rec.Body)
	}
}

func readChallenge(t *testing.T, body []byte) (code string, expires int) {
	t.Helper()
	var out struct {
		Error     string `json:"error"`
		Code      string `json:"code"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("challenge: %v (%s)", err, body)
	}
	return out.Code, out.ExpiresIn
}

// TestPairingFlow: a configured MAC gets a code instead of a key, the operator's
// code hands the key out once, and the next register asks again.
func TestPairingFlow(t *testing.T) {
	s, _ := testServer(t)
	const mac = "aa:bb:cc:dd:ee:01"
	rec := register(t, s, `{"mac":"`+mac+`","hardware":"sticky","firmware":"2.9.0"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("register without pairing: %d %s", rec.Code, rec.Body)
	}
	code, expires := readChallenge(t, rec.Body.Bytes())
	if len(code) != pairLength || expires != int(pairTTL.Seconds()) {
		t.Fatalf("challenge: %q %d (%s)", code, expires, rec.Body)
	}
	for _, r := range code {
		if !strings.ContainsRune(pairAlphabet, r) {
			t.Fatalf("code %q uses a character that is hard to read", code)
		}
	}
	// A second register while the code is valid shows the same code: the panel
	// must not paint a new one every minute.
	if again := register(t, s, `{"mac":"`+mac+`"}`); !strings.Contains(again.Body.String(), code) {
		t.Fatalf("the code changed within its lifetime: %s", again.Body)
	}
	// A wrong code is refused and counted.
	if rec := request(t, s, http.MethodPost, "/api/state/devices/"+mac+"/pair", map[string]string{"code": "ZZZZZZ"}, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong code: %d %s", rec.Code, rec.Body)
	}
	if rec := request(t, s, http.MethodPost, "/api/state/devices/"+mac+"/pair", map[string]string{"code": code}, nil); rec.Code != http.StatusOK {
		t.Fatalf("pairing: %d %s", rec.Code, rec.Body)
	}
	// The next register takes the key ...
	rec = register(t, s, `{"mac":"`+mac+`"}`)
	var reg struct{ Key string }
	if err := json.Unmarshal(rec.Body.Bytes(), &reg); err != nil || rec.Code != http.StatusOK || reg.Key == "" {
		t.Fatalf("register after pairing: %d %s", rec.Code, rec.Body)
	}
	// ... and only that one: the confirmation is consumed.
	rec = register(t, s, `{"mac":"`+mac+`"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("second register without pairing: %d %s", rec.Code, rec.Body)
	}
}

// TestPairingExpires: an old code is not accepted, and the next register shows
// a new one.
func TestPairingExpires(t *testing.T) {
	s, now := testServer(t)
	const mac = "aa:bb:cc:dd:ee:01"
	rec := register(t, s, `{"mac":"`+mac+`"}`)
	old, _ := readChallenge(t, rec.Body.Bytes())
	*now = now.Add(pairTTL + time.Minute)
	if rec := request(t, s, http.MethodPost, "/api/state/devices/"+mac+"/pair", map[string]string{"code": old}, nil); rec.Code != http.StatusGone {
		t.Fatalf("expired code: %d %s", rec.Code, rec.Body)
	}
	rec = register(t, s, `{"mac":"`+mac+`"}`)
	code, _ := readChallenge(t, rec.Body.Bytes())
	if code == old {
		t.Fatalf("the expired code %q came back", code)
	}
	if rec := request(t, s, http.MethodPost, "/api/state/devices/"+mac+"/pair", map[string]string{"code": code}, nil); rec.Code != http.StatusOK {
		t.Fatalf("new code: %d %s", rec.Code, rec.Body)
	}
}

// TestPairingTries: five wrong codes end the attempt; the display shows a new
// code afterwards.
func TestPairingTries(t *testing.T) {
	s, _ := testServer(t)
	const mac = "aa:bb:cc:dd:ee:01"
	register(t, s, `{"mac":"`+mac+`"}`)
	for i := 0; i < pairTries-1; i++ {
		if rec := request(t, s, http.MethodPost, "/api/state/devices/"+mac+"/pair", map[string]string{"code": "ZZZZZZ"}, nil); rec.Code != http.StatusForbidden {
			t.Fatalf("attempt %d: %d %s", i+1, rec.Code, rec.Body)
		}
	}
	if rec := request(t, s, http.MethodPost, "/api/state/devices/"+mac+"/pair", map[string]string{"code": "ZZZZZZ"}, nil); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("last attempt: %d %s", rec.Code, rec.Body)
	}
	// The next register issues a new code with fresh attempts.
	rec := register(t, s, `{"mac":"`+mac+`"}`)
	code, _ := readChallenge(t, rec.Body.Bytes())
	if rec := request(t, s, http.MethodPost, "/api/state/devices/"+mac+"/pair", map[string]string{"code": code}, nil); rec.Code != http.StatusOK {
		t.Fatalf("code after the limit: %d %s", rec.Code, rec.Body)
	}
}

// TestPairingNeedsConfiguredDevice: a code belongs to a device file; a state
// file of unclaimed hardware cannot be paired.
func TestPairingNeedsConfiguredDevice(t *testing.T) {
	s, _ := testServer(t)
	const mac = "aa:bb:cc:dd:ee:99"
	if rec := register(t, s, `{"mac":"`+mac+`"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("unknown device: %d %s", rec.Code, rec.Body)
	}
	if rec := request(t, s, http.MethodPost, "/api/state/devices/"+mac+"/pair", map[string]string{"code": "ABCDEF"}, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("pairing unclaimed hardware: %d %s", rec.Code, rec.Body)
	}
	if rec := request(t, s, http.MethodPost, "/api/state/devices/nope/pair", map[string]string{"code": "ABCDEF"}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid MAC: %d %s", rec.Code, rec.Body)
	}
}

// TestPairingInDeviceEditor: the device API names the pairing state, so the
// editor can offer the button and the code.
func TestPairingInDeviceEditor(t *testing.T) {
	cfg, _, err := loadConfigWith(t, "language: de\n", map[string]string{
		"test": "display_type: e1001\nmac: \"aa:bb:cc:dd:ee:01\"\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Date(2026, 11, 15, 12, 0, 0, 0, time.Local) }
	var out struct {
		Pairing pairingInfo `json:"pairing"`
	}
	request(t, s, http.MethodGet, "/api/devices/test", nil, &out)
	if out.Pairing.Paired || out.Pairing.Waiting || out.Pairing.MAC != "AA:BB:CC:DD:EE:01" {
		t.Fatalf("pairing before register: %+v", out.Pairing)
	}
	rec := register(t, s, `{"mac":"aa:bb:cc:dd:ee:01"}`)
	code, _ := readChallenge(t, rec.Body.Bytes())
	out.Pairing = pairingInfo{}
	request(t, s, http.MethodGet, "/api/devices/test", nil, &out)
	if out.Pairing.Paired || !out.Pairing.Waiting {
		t.Fatalf("pairing with a code on the panel: %+v", out.Pairing)
	}
	if rec := request(t, s, http.MethodPost, "/api/state/devices/AA:BB:CC:DD:EE:01/pair", map[string]string{"code": code}, nil); rec.Code != http.StatusOK {
		t.Fatalf("pairing: %d %s", rec.Code, rec.Body)
	}
	out.Pairing = pairingInfo{}
	request(t, s, http.MethodGet, "/api/devices/test", nil, &out)
	if !out.Pairing.Paired || out.Pairing.Waiting {
		t.Fatalf("pairing after the confirmation: %+v", out.Pairing)
	}

	// The confirmation is consumed by a register, which hands out the key.
	rec = register(t, s, `{"mac":"aa:bb:cc:dd:ee:01"}`)
	var reg struct{ Key string }
	if err := json.Unmarshal(rec.Body.Bytes(), &reg); err != nil || rec.Code != http.StatusOK || reg.Key == "" {
		t.Fatalf("register after the confirmation: %d %s", rec.Code, rec.Body)
	}

	// A later register without a key (the device lost it) gets a new code
	// while the old key is still in the state. The display must pair again,
	// and the editor must call it unpaired so the Pair button works (issue #322).
	rec = register(t, s, `{"mac":"aa:bb:cc:dd:ee:01"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("register with an old key and no pairing: %d %s", rec.Code, rec.Body)
	}
	code, _ = readChallenge(t, rec.Body.Bytes())
	out.Pairing = pairingInfo{}
	request(t, s, http.MethodGet, "/api/devices/test", nil, &out)
	if out.Pairing.Paired || !out.Pairing.Waiting {
		t.Fatalf("a waiting code must count as unpaired: %+v", out.Pairing)
	}
	if rec := request(t, s, http.MethodPost, "/api/state/devices/AA:BB:CC:DD:EE:01/pair", map[string]string{"code": code}, nil); rec.Code != http.StatusOK {
		t.Fatalf("pairing again: %d %s", rec.Code, rec.Body)
	}
}
