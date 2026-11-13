package paperboy

import (
	"crypto/sha256"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// requireUI guards the operator UI and its API with the password from
// paperboy.yaml (ADR 0096). Without a configured password the UI stays open
// like before, so an existing instance keeps working; the CSRF check below runs
// either way, because Basic credentials are attached to cross-site form posts.
func (s *Server) requireUI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if crossSite(r) {
			slog.Info("UI request refused: cross-site", "path", r.URL.Path, "remote", clientIP(r), "origin", r.Header.Get("Origin"))
			http.Error(w, "cross-site request refused (ADR 0096)", http.StatusForbidden)
			return
		}
		if password := s.config().UI.Password; password != "" && !authorized(r, password) {
			slog.Info("UI request without valid credentials", "path", r.URL.Path, "remote", clientIP(r))
			w.Header().Set("WWW-Authenticate", `Basic realm="Paperboy", charset="UTF-8"`)
			http.Error(w, "the operator UI needs the password from ui.password in paperboy.yaml (ADR 0096)", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authorized reports whether the request carries the UI password as HTTP Basic
// credentials. The user name is ignored: Paperboy has one operator.
func authorized(r *http.Request, password string) bool {
	_, given, ok := r.BasicAuth()
	if !ok {
		return false
	}
	// Hashing first makes the comparison independent of the length; the
	// plaintext never has to compare equal byte by byte.
	want, got := sha256.Sum256([]byte(password)), sha256.Sum256([]byte(given))
	return subtle.ConstantTimeCompare(want[:], got[:]) == 1
}

// crossSite reports a state-changing request that a browser sends on behalf of
// another site. A form post does not need a preflight, and with the UI password
// in the browser's cache it would even carry the credentials, so every write
// must come from Paperboy's own pages (ADR 0096). Requests without an Origin
// header come from scripts and tools and are allowed.
func crossSite(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return true
	}
	return !strings.EqualFold(u.Host, r.Host)
}
