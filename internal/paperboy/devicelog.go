package paperboy

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// deviceLogEntry is one firmware log entry delivered with a sync (ADR 0042).
// The firmware may buffer entries it could not deliver and send them with a
// later sync, so an entry can be hours old.
type deviceLogEntry struct {
	Time    int64  `json:"time"` // Unix seconds on the device clock; 0 = unknown
	ID      int    `json:"id"`   // optional sequence number, used to drop repeated deliveries
	Level   string `json:"level"`
	Message string `json:"message"`
	Source  string `json:"source"` // firmware component or file
}

// key identifies an entry across repeated deliveries. Entries without a time
// and an ID cannot be told apart and are always logged.
func (e deviceLogEntry) key(device string) (string, bool) {
	if e.Time == 0 && e.ID == 0 {
		return "", false
	}
	return fmt.Sprintf("%s|%d|%d|%s", device, e.ID, e.Time, e.Message), true
}

// slogLevel maps the firmware level names onto the slog levels.
func (e deviceLogEntry) slogLevel() slog.Level {
	switch strings.ToLower(e.Level) {
	case "debug", "verbose":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error", "critical":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// logDevice writes one line per delivered entry, so firmware messages stay
// readable and filterable by level and source.
func (s *Server) logDevice(device string, entries []deviceLogEntry) {
	now := s.now()
	for _, entry := range entries {
		// A sync that failed is repeated with the same entries.
		if key, ok := entry.key(device); ok && !s.logs.first(key, now) {
			continue
		}
		attrs := []any{"device", device, "message", entry.Message}
		if entry.Source != "" {
			attrs = append(attrs, "source", entry.Source)
		}
		// Times come from the device clock: a positive distance means the entry
		// waited in the device buffer, a negative one a clock ahead of ours.
		if entry.Time > 0 {
			switch age := now.Sub(time.Unix(entry.Time, 0)); {
			case age >= time.Minute:
				attrs = append(attrs, "age", age.Round(time.Minute))
			case age <= -time.Minute:
				attrs = append(attrs, "skew", (-age).Round(time.Minute))
			}
		}
		slog.Log(context.Background(), entry.slogLevel(), "display log", attrs...)
	}
}

// logSeen remembers delivered entries so a repeated delivery is logged only once.
type logSeen struct {
	mu    sync.Mutex
	seen  map[string]time.Time
	limit int
	ttl   time.Duration
}

func newLogSeen() *logSeen {
	return &logSeen{seen: map[string]time.Time{}, limit: 1024, ttl: 24 * time.Hour}
}

// first reports whether key is new and remembers it for the next calls.
func (l *logSeen) first(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, at := range l.seen {
		if now.Sub(at) > l.ttl {
			delete(l.seen, k)
		}
	}
	if _, ok := l.seen[key]; ok {
		return false
	}
	// A full bookkeeping must not swallow messages.
	if len(l.seen) >= l.limit {
		return true
	}
	l.seen[key] = now
	return true
}
