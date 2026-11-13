package paperboy

import (
	"log/slog"
	"net/http"
	"strings"
)

// requirePageKey guards a page or notice download (ADR 0097): the request has
// to carry the key of a configured display, and that display has to be of the
// type the page belongs to. Pages carry the instance's content, so they are
// never served to the network at large.
func (s *Server) requirePageKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := s.config()
		device := s.deviceFor(r, cfg)
		if device == nil {
			s.notePageKey(r.URL.Path, "no valid device key")
			http.Error(w, "page downloads need the key of a configured display (ADR 0097)", http.StatusUnauthorized)
			return
		}
		if want := s.pageProfile(r); want != device.Profile {
			s.notePageKey(r.URL.Path, "display type "+device.Profile+" asked for type "+want)
			http.Error(w, "this page belongs to another display type", http.StatusForbidden)
			return
		}
		s.clearPageKeyNote(r.URL.Path)
		next(w, r)
	}
}

// deviceFor finds the device that owns the request's key, or nil (ADR 0042).
func (s *Server) deviceFor(r *http.Request, cfg *Config) *Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	device, _ := s.deviceByKeyLocked(r, cfg)
	return device
}

// pageProfile names the display type a download belongs to: a rendered page
// carries it in the path, a notice page only knows it from its registration.
func (s *Server) pageProfile(r *http.Request) string {
	if profile := r.PathValue("profile"); profile != "" {
		return profile
	}
	key, _ := strings.CutSuffix(r.PathValue("file"), ".png")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hints[key].profile
}

// notePageKey logs a refused download once per path and reason, so a display
// with old firmware shows up in the journal without a line on every wake.
func (s *Server) notePageKey(path, reason string) {
	s.mu.Lock()
	if s.pageKeyNotes == nil {
		s.pageKeyNotes = map[string]string{}
	}
	if s.pageKeyNotes[path] == reason {
		s.mu.Unlock()
		return
	}
	s.pageKeyNotes[path] = reason
	s.mu.Unlock()
	slog.Info("page download refused", "path", path, "reason", reason)
}

// clearPageKeyNote lets the next refusal of the same path log again.
func (s *Server) clearPageKeyNote(path string) {
	s.mu.Lock()
	delete(s.pageKeyNotes, path)
	s.mu.Unlock()
}
