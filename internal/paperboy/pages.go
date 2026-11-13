package paperboy

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/internal/page"
)

// pageEntry caches one page file; mod and size detect changes on disk.
type pageEntry struct {
	mod  time.Time
	size int64
	t    *page.Template
	err  error
}

// pageLoader reads pages and keeps them until their file changes, so the plan
// can check every page of a rule against the display type on each request.
type pageLoader struct {
	mu      sync.Mutex
	entries map[string]*pageEntry
}

func newPageLoader() *pageLoader {
	return &pageLoader{entries: map[string]*pageEntry{}}
}

// get returns pages/<name>.<display>.yaml or pages/<name>.yaml like page.Load,
// parsing a file only when it changed. A missing page is page.ErrNotFound.
func (l *pageLoader) get(dir, name, display string) (*page.Template, error) {
	paths, err := page.Paths(dir, name, display)
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		l.mu.Lock()
		e := l.entries[path]
		l.mu.Unlock()
		if e != nil && e.mod.Equal(info.ModTime()) && e.size == info.Size() {
			return e.t, e.err
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		t, err := page.Parse(f)
		f.Close()
		if err != nil {
			err = fmt.Errorf("%s: %w", path, err)
		}
		l.mu.Lock()
		l.entries[path] = &pageEntry{mod: info.ModTime(), size: info.Size(), t: t, err: err}
		l.mu.Unlock()
		return t, err
	}
	return nil, fmt.Errorf("%w: %s.%s.yaml or %s.yaml in %s", page.ErrNotFound, name, display, name, dir)
}

// stats reports what the loader keeps: the number of cached page files and
// their size on disk (ADR 0104).
func (l *pageLoader) stats() (files int, bytes int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.entries {
		files++
		bytes += e.size
	}
	return files, bytes
}

// fits reports why a page cannot be shown on a display type: it is missing
// for that type, or it is a page of the other kind or with a grid that is not
// the text display's. A broken page fits, so that its error is shown as a
// notice instead of being skipped silently.
func (s *Server) fits(cfg *Config, profile, name string) error {
	p, ok := cfg.Profiles[profile]
	if !ok {
		return fmt.Errorf("unknown display type %q", profile)
	}
	t, err := s.pages.get(cfg.Pages, name, profile)
	if errors.Is(err, page.ErrNotFound) {
		return errors.New("no page for this display type")
	}
	if err != nil {
		return nil
	}
	return t.Fits(s.display(cfg, p))
}

// fittingPages keeps the pages of a plan rule that fit the device's display
// type. A skipped page is logged once per display type and page, and again
// only when the reason changes. Callers hold s.mu.
func (s *Server) fittingPages(cfg *Config, profile string, names []string) ([]string, []string) {
	var out, reasons []string
	for _, name := range names {
		key := profile + "/" + name
		err := s.fits(cfg, profile, name)
		if err == nil {
			out = append(out, name)
			delete(s.skipNotes, key)
			continue
		}
		reasons = append(reasons, name+": "+err.Error())
		if s.skipNotes[key] != err.Error() {
			s.skipNotes[key] = err.Error()
			slog.Info("page skipped for display type", "display_type", profile, "page", name, "reason", err)
		}
	}
	return out, reasons
}

// joinReasons keeps a notice short.
func joinReasons(r []string) string {
	return strings.Join(r, "; ")
}
