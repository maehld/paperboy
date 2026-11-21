package paperboy

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// configPollInterval is how often the background loop re-checks paperboy.yaml
// and reconfigures the plugins. Handlers also check on every request, so a
// change becomes visible there immediately; the loop keeps the plugins in line
// even when no device wakes up.
const configPollInterval = 2 * time.Second

// configEntry caches one paperboy.yaml; mod and size detect changes on disk.
type configEntry struct {
	mod  time.Time
	size int64
}

// configLoader holds the active configuration and reloads it from
// paperboy.yaml when the file changes, using the same stat check as the plan
// loader (ADR 0025): a broken file keeps the last valid configuration active
// and is logged, a missing file leaves the current configuration alone. On a
// degraded start (no usable paperboy.yaml yet) the server runs on the initial
// configuration and applies the file as soon as it becomes usable, even when
// its mod time did not change (ADR 0047).
type configLoader struct {
	dir string

	mu       sync.Mutex
	entry    configEntry
	displays string // stamp of the display type files (ADR 0071)
	devices  string // stamp of the device files (ADR 0070)
	cfg      *Config
	lastErr  string // message of the last load failure; the same failure logs once
	missing  bool   // the last stat failed; the problem is logged once
}

func newConfigLoader(dir string, cfg *Config) *configLoader {
	l := &configLoader{dir: dir, cfg: cfg,
		displays: displaysStamp(filepath.Join(dir, displaysDirName)),
		devices:  devicesStamp(filepath.Join(dir, devicesDirName))}
	if info, err := os.Stat(filepath.Join(dir, configFileName)); err == nil {
		l.entry = configEntry{mod: info.ModTime(), size: info.Size()}
	} else {
		l.missing = true
		// main logs the startup failure; New also runs in tests without a file.
		slog.Debug("paperboy.yaml unreadable, starting with the given configuration",
			"path", filepath.Join(dir, configFileName), "error", err)
	}
	return l
}

// get returns the current configuration and reloads it from disk when the
// file changed. A reload that fails keeps the last valid configuration; a
// missing file keeps the current one.
func (l *configLoader) get() *Config {
	l.mu.Lock()
	defer l.mu.Unlock()
	path := filepath.Join(l.dir, configFileName)
	info, err := os.Stat(path)
	if err != nil {
		if !l.missing {
			l.missing = true
			slog.Error("paperboy.yaml unreadable, keeping the current configuration",
				"path", path, "error", err)
		}
		return l.cfg
	}
	l.missing = false
	changed := l.entry.size != info.Size() || !l.entry.mod.Equal(info.ModTime())
	// Display types and devices live in their own files (ADR 0070, 0071), so a
	// change there has to reload the configuration as well.
	if stamp := displaysStamp(filepath.Join(l.dir, displaysDirName)); stamp != l.displays {
		changed = true
		l.displays = stamp
	}
	if stamp := devicesStamp(filepath.Join(l.dir, devicesDirName)); stamp != l.devices {
		changed = true
		l.devices = stamp
	}
	// A changed file reloads at once. A degraded start retries the file on
	// every access even when it did not change, so a repaired file or a fixed
	// permission applies without touching it; the same failure is logged once
	// (ADR 0047).
	if !changed && !l.cfg.degraded {
		return l.cfg
	}
	l.entry = configEntry{mod: info.ModTime(), size: info.Size()}
	cfg, loadErr := LoadConfig(l.dir)
	if loadErr != nil {
		if msg := loadErr.Error(); msg != l.lastErr {
			l.lastErr = msg
			slog.Error("paperboy.yaml invalid, keeping the last valid configuration",
				"path", path, "error", loadErr)
		}
		return l.cfg
	}
	l.lastErr = ""
	slog.Info("configuration loaded", "display_types", len(cfg.Profiles),
		"devices", len(cfg.Devices), "plugins", len(cfg.Plugins))
	l.cfg = cfg
	return l.cfg
}

// DegradedConfig returns a minimal configuration for a startup without a
// usable paperboy.yaml: the server serves the profiles and the dashboard with
// no display types and no devices, and applies the real configuration as soon
// as the file becomes usable (ADR 0047).
func DegradedConfig(dir string) *Config {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return &Config{Dir: dir, Profiles: map[string]Profile{}, degraded: true}
}

// configLoop re-reads paperboy.yaml every configPollInterval and hands the
// plugin section to the widget host, so plugins follow the configuration
// even when no device wakes up (ADR 0047, 0049).
func (s *Server) configLoop(ctx context.Context) {
	s.configurePlugins(s.config())
	ticker := time.NewTicker(configPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		s.configurePlugins(s.config())
	}
}
