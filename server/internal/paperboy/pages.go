package paperboy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/server/internal/page"
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

// get returns the file that serves a display type: the variant that the page
// names, else pages/<name>.yaml (ADR 0110). It parses a file only when it
// changed. A page without a file for the type is page.ErrNotFound.
func (l *pageLoader) get(dir, name, display string) (*page.Template, error) {
	path, err := page.PagePath(dir, name)
	if err != nil {
		return nil, err
	}
	t, err := l.file(path, false)
	if err != nil {
		return nil, err
	}
	label, ok := t.Variants[display]
	if !ok {
		return t, nil
	}
	path, err = page.VariantPath(dir, name, label)
	if err != nil {
		return nil, err
	}
	v, err := l.file(path, true)
	if errors.Is(err, page.ErrNotFound) {
		return nil, fmt.Errorf("%w: the page names the variant %q for display type %q, but %s is missing",
			page.ErrNotFound, label, display, filepath.Base(path))
	}
	if err != nil {
		return nil, err
	}
	out := *v // the cached template keeps its own title
	out.Title = t.Title
	return &out, nil
}

// file reads one page file and keeps it until it changes. variant selects the
// parser: a variant carries no title and no variants (ADR 0110).
func (l *pageLoader) file(path string, variant bool) (*page.Template, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", page.ErrNotFound, path)
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
	var t *page.Template
	if variant {
		t, err = page.ParseVariant(f)
	} else {
		t, err = page.Parse(f)
	}
	f.Close()
	if err != nil {
		err = fmt.Errorf("%s: %w", path, err)
	}
	l.mu.Lock()
	l.entries[path] = &pageEntry{mod: info.ModTime(), size: info.Size(), t: t, err: err}
	l.mu.Unlock()
	return t, err
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

// fits reports why a page cannot be shown on a display type: the page has no
// file for that type (page.ErrNotFound), or the file is of the other kind or
// with a grid that is not the text display's. A broken page fits, so that its
// error is shown as a notice instead of being skipped silently.
func (s *Server) fits(cfg *Config, profile, name string) error {
	p, ok := cfg.Profiles[profile]
	if !ok {
		return fmt.Errorf("unknown display type %q", profile)
	}
	t, err := s.pages.get(cfg.Pages, name, profile)
	if errors.Is(err, page.ErrNotFound) {
		return fmt.Errorf("no page for this display type: %w", err)
	}
	if err != nil {
		return nil
	}
	return t.Fits(s.display(cfg, p))
}

// joinReasons keeps a notice short.
func joinReasons(r []string) string {
	return strings.Join(r, "; ")
}
