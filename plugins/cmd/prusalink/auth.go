package main

// HTTP Digest authentication as the PrusaLink API documents it (RFC 7616,
// algorithm MD5 or SHA-256, qop auth). The printer uses the user maker and the
// API key as the password. Go's standard client has no Digest support, so the
// plugin builds the Authorization header itself and keeps the challenge.

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"strings"
	"sync"
)

// digestAuth keeps the last challenge of one printer.
type digestAuth struct {
	mu        sync.Mutex
	challenge *challenge
	nc        uint32
}

// challenge is the part of WWW-Authenticate the plugin answers.
type challenge struct {
	realm     string
	nonce     string
	opaque    string
	algorithm string
	qop       string
}

// pending returns the cached challenge, or nil when the printer has not asked
// for Digest yet.
func (a *digestAuth) pending() *challenge {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.challenge
}

// remember replaces the challenge and restarts the nonce count.
func (a *digestAuth) remember(c *challenge) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.challenge = c
	a.nc = 0
}

// header builds the Authorization answer for one request.
func (a *digestAuth) header(c *challenge, method, uri, user, pass string) (string, error) {
	hash, err := hashFunc(c.algorithm)
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	a.nc++
	nc := a.nc
	a.mu.Unlock()

	ha1 := hashHex(hash, user+":"+c.realm+":"+pass)
	ha2 := hashHex(hash, method+":"+uri)
	cnonce := randomHex(8)
	var response string
	if c.qop != "" {
		response = hashHex(hash, strings.Join([]string{
			ha1, c.nonce, fmt.Sprintf("%08x", nc), cnonce, c.qop, ha2,
		}, ":"))
	} else {
		response = hashHex(hash, ha1+":"+c.nonce+":"+ha2)
	}

	parts := []string{
		`username="` + user + `"`,
		`realm="` + c.realm + `"`,
		`nonce="` + c.nonce + `"`,
		`uri="` + uri + `"`,
		`response="` + response + `"`,
		"algorithm=" + algorithmName(c.algorithm),
	}
	if c.opaque != "" {
		parts = append(parts, `opaque="`+c.opaque+`"`)
	}
	if c.qop != "" {
		parts = append(parts,
			"qop="+c.qop,
			fmt.Sprintf("nc=%08x", nc),
			`cnonce="`+cnonce+`"`,
		)
	}
	return "Digest " + strings.Join(parts, ", "), nil
}

// hashFunc selects the hash of the challenge. MD5 is the default.
func hashFunc(algorithm string) (func() hash.Hash, error) {
	switch strings.ToUpper(strings.TrimSpace(algorithm)) {
	case "", "MD5":
		return md5.New, nil
	case "SHA-256":
		return sha256.New, nil
	default:
		return nil, fmt.Errorf("unsupported Digest algorithm %q", algorithm)
	}
}

func algorithmName(algorithm string) string {
	if strings.TrimSpace(algorithm) == "" {
		return "MD5"
	}
	return algorithm
}

func hashHex(newHash func() hash.Hash, s string) string {
	h := newHash()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// The cnonce only has to differ per request; a fixed one still works.
		return strings.Repeat("0", 2*n)
	}
	return hex.EncodeToString(b)
}

// parseChallenge reads the Digest parameters of a WWW-Authenticate header.
func parseChallenge(header string) (*challenge, error) {
	const prefix = "digest "
	header = strings.TrimSpace(header)
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return nil, errors.New("the printer asks for no Digest authentication")
	}
	params := authParams(header[len(prefix):])
	c := &challenge{
		realm:     params["realm"],
		nonce:     params["nonce"],
		opaque:    params["opaque"],
		algorithm: params["algorithm"],
	}
	if c.nonce == "" {
		return nil, errors.New("the Digest challenge names no nonce")
	}
	if offered := params["qop"]; offered != "" {
		for _, q := range strings.Split(offered, ",") {
			if strings.EqualFold(strings.TrimSpace(q), "auth") {
				c.qop = "auth"
				break
			}
		}
		if c.qop == "" {
			return nil, fmt.Errorf("the Digest challenge offers no auth qop: %q", offered)
		}
	}
	if _, err := hashFunc(c.algorithm); err != nil {
		return nil, err
	}
	return c, nil
}

// authParams splits `key=value, key="quoted, value"` pairs.
func authParams(s string) map[string]string {
	out := map[string]string{}
	for len(s) > 0 {
		s = strings.TrimLeft(s, " \t,")
		i := strings.IndexByte(s, '=')
		if i < 0 {
			break
		}
		key := strings.ToLower(strings.TrimSpace(s[:i]))
		s = s[i+1:]
		var value string
		if strings.HasPrefix(s, `"`) {
			var b strings.Builder
			j := 1
			for j < len(s) {
				if s[j] == '\\' && j+1 < len(s) {
					b.WriteByte(s[j+1])
					j += 2
					continue
				}
				if s[j] == '"' {
					j++
					break
				}
				b.WriteByte(s[j])
				j++
			}
			value = b.String()
			s = s[j:]
		} else {
			j := strings.IndexByte(s, ',')
			if j < 0 {
				j = len(s)
			}
			value = strings.TrimSpace(s[:j])
			s = s[j:]
		}
		if key != "" {
			out[key] = value
		}
	}
	return out
}
