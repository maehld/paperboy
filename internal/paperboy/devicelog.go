package paperboy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// deviceLogEntry is one entry of the TRMNL log payload. The firmware buffers
// logs it could not upload and delivers them later in batches, so one request
// can carry entries from hours ago.
type deviceLogEntry struct {
	CreatedAt      int64   `json:"created_at"`
	ID             int     `json:"id"`
	Message        string  `json:"message"`
	SourcePath     string  `json:"source_path"`
	SourceLine     int     `json:"source_line"`
	Level          string  `json:"level"`
	WiFiStatus     string  `json:"wifi_status"`
	BatteryVoltage float64 `json:"battery_voltage"`
	Retry          int     `json:"retry"`
}

// source names the firmware file and line that logged the entry.
func (e deviceLogEntry) source() string {
	switch {
	case e.SourcePath == "":
		return ""
	case e.SourceLine == 0:
		return e.SourcePath
	}
	return fmt.Sprintf("%s:%d", e.SourcePath, e.SourceLine)
}

// key identifies an entry across retried deliveries of the same batch.
func (e deviceLogEntry) key(device string) string {
	return fmt.Sprintf("%s|%d|%d|%s", device, e.ID, e.CreatedAt, e.Message)
}

// slogLevel maps the firmware level names onto the slog levels.
func (e deviceLogEntry) slogLevel() slog.Level {
	switch strings.ToLower(e.Level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error", "critical":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// logDevice writes one line per delivered entry instead of dumping the whole
// batch, so firmware noise stays readable and filterable by level and source.
// A body that is not a log batch is kept as is.
func (s *Server) logDevice(device string, body []byte) {
	var batch struct {
		Logs []deviceLogEntry `json:"logs"`
	}
	if err := json.Unmarshal(body, &batch); err != nil || len(batch.Logs) == 0 {
		slog.Warn("display log", "device", device, "log", string(body))
		return
	}
	now := s.now()
	for _, entry := range batch.Logs {
		// A failed upload is retried with the same entries.
		if !s.logs.first(entry.key(device), now) {
			continue
		}
		attrs := []any{"device", device, "message", entry.Message}
		if src := entry.source(); src != "" {
			attrs = append(attrs, "source", src)
		}
		if entry.WiFiStatus != "" && entry.WiFiStatus != "connected" {
			attrs = append(attrs, "wifi", entry.WiFiStatus)
		}
		if entry.Retry > 1 {
			attrs = append(attrs, "retry", entry.Retry)
		}
		// Times come from the device clock: a positive distance means the entry
		// waited in the device buffer, a negative one a clock ahead of ours.
		if entry.CreatedAt > 0 {
			switch age := now.Sub(time.Unix(entry.CreatedAt, 0)); {
			case age >= time.Minute:
				attrs = append(attrs, "age", age.Round(time.Minute))
			case age <= -time.Minute:
				attrs = append(attrs, "skew", (-age).Round(time.Minute))
			}
		}
		slog.Log(context.Background(), entry.slogLevel(), "display log", attrs...)
	}
}

// logSeen remembers delivered entries so a retried batch is logged only once.
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
