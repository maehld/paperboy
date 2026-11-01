package paperboy

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/maehld/paperboy/internal/data"
	"github.com/maehld/paperboy/internal/provider"
)

// configPollInterval is how often the background loop re-checks paperboy.yaml
// and reconciles the exec providers. Handlers also check on every request, so
// a change becomes visible there immediately; the loop keeps providers and the
// data API in line even when no device wakes up.
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
// configuration and applies the file as soon as it becomes valid (ADR 0047).
type configLoader struct {
	dir string

	mu      sync.Mutex
	entry   configEntry
	cfg     *Config
	missing bool // the last stat failed; the problem is logged once
}

func newConfigLoader(dir string, cfg *Config) *configLoader {
	l := &configLoader{dir: dir, cfg: cfg}
	if info, err := os.Stat(filepath.Join(dir, configFileName)); err == nil {
		l.entry = configEntry{mod: info.ModTime(), size: info.Size()}
	} else {
		l.missing = true
		slog.Error("paperboy.yaml unreadable, starting with the given configuration",
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
	if l.entry.size == info.Size() && l.entry.mod.Equal(info.ModTime()) {
		return l.cfg
	}
	l.entry = configEntry{mod: info.ModTime(), size: info.Size()}
	cfg, loadErr := LoadConfig(l.dir)
	if loadErr != nil {
		slog.Error("paperboy.yaml invalid, keeping the last valid configuration",
			"path", path, "error", loadErr)
		return l.cfg
	}
	slog.Info("configuration loaded", "display_types", len(cfg.Profiles),
		"devices", len(cfg.Devices), "providers", len(cfg.Providers))
	l.cfg = cfg
	return l.cfg
}

// DegradedConfig returns a minimal configuration for a startup without a
// usable paperboy.yaml: the server serves the data API, profiles and the
// dashboard with no display types and no devices, and applies the real
// configuration as soon as the file becomes valid (ADR 0047).
func DegradedConfig(dir string) *Config {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return &Config{Dir: dir, Profiles: map[string]Profile{}}
}

// providerRun is one running exec provider and the spec it was started with.
type providerRun struct {
	cancel context.CancelFunc
	spec   provider.Spec
}

// providerManager starts, stops and restarts the exec providers whenever the
// configuration changes (ADR 0036, 0047). It is reconciled from the
// configuration loop only, never from a request handler.
type providerManager struct {
	mu   sync.Mutex
	runs map[string]*providerRun
}

func newProviderManager() *providerManager {
	return &providerManager{runs: map[string]*providerRun{}}
}

// sync brings the running providers in line with cfg: removed providers are
// stopped, new ones started, and providers whose spec changed are restarted.
// The document and the clock are shared; the providers write into the same
// data document as the data API.
func (m *providerManager) sync(ctx context.Context, cfg *Config, doc *data.Document, now func() time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	wanted := make(map[string]provider.Spec, len(cfg.Providers))
	for _, p := range cfg.Providers {
		wanted[p.Name] = providerSpec(cfg, p)
	}
	for name, run := range m.runs {
		if _, keep := wanted[name]; !keep || !reflect.DeepEqual(run.spec, wanted[name]) {
			run.cancel()
			delete(m.runs, name)
			slog.Info("provider stopped", "provider", name)
		}
	}
	for _, p := range cfg.Providers {
		if _, running := m.runs[p.Name]; running {
			continue
		}
		spec := wanted[p.Name]
		runCtx, cancel := context.WithCancel(ctx)
		runner := &provider.Runner{Spec: spec, Sink: doc, Now: now}
		m.runs[p.Name] = &providerRun{cancel: cancel, spec: spec}
		go runner.Run(runCtx)
	}
}

// providerSpec builds one provider.Spec from the configuration, with the
// global language and timezone fixed into the environment (ADR 0036).
func providerSpec(cfg *Config, p Provider) provider.Spec {
	tz := "UTC"
	if cfg.Timezone != nil {
		tz = cfg.Timezone.String()
	}
	env := map[string]string{
		"PAPERBOY_LANGUAGE": cfg.Language,
		"PAPERBOY_TIMEZONE": tz,
	}
	for k, v := range p.Env {
		env[k] = v
	}
	return provider.Spec{
		Name:     p.Name,
		Command:  p.Command,
		Path:     p.Path,
		Interval: time.Duration(p.Interval),
		Timeout:  time.Duration(p.Timeout),
		Env:      env,
		Dir:      cfg.ProvidersDir,
	}
}

// configLoop reloads the configuration on the poll interval and reconciles
// the exec providers, so provider changes (start/stop/interval) follow the
// configuration without a restart. Handlers reload on their own when they
// access the configuration, so endpoints react to a change immediately.
func (s *Server) configLoop(ctx context.Context) {
	cfg := s.config()
	s.providers.sync(ctx, cfg, s.doc, s.now)
	ticker := time.NewTicker(configPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		cfg := s.config()
		s.providers.sync(ctx, cfg, s.doc, s.now)
	}
}
