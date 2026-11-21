package paperboy

import (
	"crypto/rand"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Pairing (ADR 0109): a MAC address alone does not buy the device key. The
// register draws a short code on the panel, the operator types it into the
// device editor, and only the next register takes the key. Nothing travels from
// the panel to the browser: the code is proof that the person at the editor can
// see the display. There is no second path; every register needs a pairing.

const (
	// pairAlphabet leaves out the characters that are easy to confuse on a
	// panel (I, O, 0, 1); 32 characters fit a byte without bias.
	pairAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	pairLength   = 6
	pairTTL      = 10 * time.Minute
	pairTries    = 5
)

type pairChallenge struct {
	Error     string `json:"error"`
	Code      string `json:"code"`
	ExpiresIn int    `json:"expires_in"` // seconds
}

// newPairCode returns a code the panel can show in one line.
func newPairCode() (string, error) {
	b := make([]byte, pairLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = pairAlphabet[int(b[i])%len(pairAlphabet)]
	}
	return string(b), nil
}

// pairChallengeLocked decides what a register gets: either nothing (the
// operator paired this display) or a code to show. The code stays the same
// while it is valid, so a display that asks every minute does not paint a new
// one. fresh names a new code, so the caller logs once per challenge and not
// once per poll.
func (s *Server) pairChallengeLocked(st *deviceState, now time.Time) (code string, challenge, fresh bool) {
	if st.PairAuthorized {
		return "", false, false
	}
	if st.PairCode != "" && now.Before(st.PairExpiry) && st.PairTries < pairTries {
		return st.PairCode, true, false
	}
	code, err := newPairCode()
	if err != nil { // never expected: crypto/rand
		slog.Error("pairing code generation failed", "error", err)
		return "", false, false
	}
	st.PairCode = code
	st.PairExpiry = now.Add(pairTTL)
	st.PairTries = 0
	return code, true, true
}

// usePairingLocked consumes the operator's authorization: the register that
// follows a pairing takes the key, and only that one.
func usePairingLocked(st *deviceState, now time.Time) {
	st.PairAuthorized = false
	st.PairedAt = now
	st.PairCode = ""
	st.PairExpiry = time.Time{}
	st.PairTries = 0
}

// pairClaim is the body of POST /api/state/devices/{mac}/pair: the code the
// panel showed.
type pairClaim struct {
	Code string `json:"code"`
}

// handlePairDevice accepts the code of one display. It is a UI route, so the
// operator UI password guards it already; the attempt limit is for the code.
func (s *Server) handlePairDevice(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	key, err := deviceStateKey(r.PathValue("mac"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid MAC address")
		return
	}
	var body pairClaim
	if err := decodeBody(w, r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "%v", err)
		return
	}
	code := strings.ToUpper(strings.TrimSpace(body.Code))
	if code == "" {
		writeAPIError(w, http.StatusBadRequest, "code missing")
		return
	}
	mac := formatStateKey(key)
	if d := cfg.device(mac); d == nil {
		writeAPIError(w, http.StatusNotFound, "no device file uses %s", mac)
		return
	}
	now := s.now()
	s.mu.Lock()
	st := s.state[key]
	if st == nil || st.PairCode == "" {
		s.mu.Unlock()
		writeAPIError(w, http.StatusNotFound, "no pairing code for %s: let the display register, then enter the code it shows", mac)
		return
	}
	if now.After(st.PairExpiry) || st.PairTries >= pairTries {
		st.PairCode, st.PairExpiry, st.PairTries = "", time.Time{}, 0
		data := s.stateSnapshotLocked(st)
		s.mu.Unlock()
		s.saveState(key, data)
		writeAPIError(w, http.StatusGone, "the code expired: wait for the next one on the display, then enter it")
		return
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(st.PairCode)) != 1 {
		st.PairTries++
		left := pairTries - st.PairTries
		data := s.stateSnapshotLocked(st)
		s.mu.Unlock()
		s.saveState(key, data)
		slog.Warn("pairing code rejected", "mac", mac, "attempt", st.PairTries)
		if left <= 0 {
			writeAPIError(w, http.StatusTooManyRequests, "wrong code too often: wait for a new code on the display")
			return
		}
		writeAPIError(w, http.StatusForbidden, "wrong code, %d attempts left", left)
		return
	}
	st.PairAuthorized = true
	st.PairedAt = now
	st.PairCode = ""
	st.PairExpiry = time.Time{}
	st.PairTries = 0
	data := s.stateSnapshotLocked(st)
	s.mu.Unlock()
	s.saveState(key, data)
	slog.Info("display paired; its next register takes the key", "mac", mac)
	writeJSON(w, http.StatusOK, pairedAnswer{Paired: true})
}

// pairedAnswer confirms a pairing to the device editor.
type pairedAnswer struct {
	Paired bool `json:"paired"`
}

// pairingInfo tells the device editor what to show: whether the display may
// take a key, and whether a code is waiting on its panel.
type pairingInfo struct {
	MAC     string `json:"mac"`
	Paired  bool   `json:"paired"`
	Waiting bool   `json:"waiting"` // a code was issued and is still valid
}

// pairingOf answers the pairing state of a MAC. A waiting code means the
// display cannot register, also when an old key is still in the state, so it
// is not paired in that moment.
func (s *Server) pairingOf(mac string) pairingInfo {
	key, err := deviceStateKey(mac)
	if err != nil {
		return pairingInfo{}
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state[key]
	info := pairingInfo{MAC: formatStateKey(key)}
	if st != nil {
		info.Waiting = st.PairCode != "" && now.Before(st.PairExpiry)
		info.Paired = (st.APIKey != "" || st.PairAuthorized) && !info.Waiting
	}
	return info
}
