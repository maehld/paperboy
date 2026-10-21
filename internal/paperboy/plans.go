package paperboy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/internal/page"
	"github.com/maehld/paperboy/internal/plan"
)

// hintRefresh is reported for pages that only announce a configuration problem,
// so the device comes back after the problem was fixed.
const hintRefresh = 15 * time.Minute

// servedPage is the page an answer points the device to.
type servedPage struct {
	filename string        // cache name reported as "filename"
	path     string        // image path below the server root
	refresh  time.Duration // sleep time to report
	page     int           // page index for logging and state
}

// renderedPage is a page template rendered for one profile and page.
type renderedPage struct {
	data []byte
	hash string
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:6])
}

// renderPage renders pages/<name>.<profile>.yaml from the data document.
func (s *Server) renderPage(profile, name string, now time.Time, banners ...string) (*renderedPage, error) {
	p, ok := s.cfg.Profiles[profile]
	if !ok {
		return nil, fmt.Errorf("unknown profile %q", profile)
	}
	tmpl, err := page.Load(s.cfg.Pages, name, profile)
	if err != nil {
		return nil, err
	}
	img, err := tmpl.Render(s.doc, page.Display{Width: p.Width, Height: p.Height, Gray: p.Gray}, now, banners...)
	if err != nil {
		return nil, err
	}
	data := encodePNG(img)
	return &renderedPage{data: data, hash: hashBytes(data)}, nil
}

// hint registers a notice page and returns its image path. Callers hold s.mu.
func (s *Server) hint(profile, title, text string) servedPage {
	data := encodePNG(placeholder(s.cfg.Profiles[profile], title, text))
	key := hashBytes(data)
	s.hints[key] = data
	return servedPage{filename: "hints/" + key, path: "/hints/" + key + ".png", refresh: hintRefresh}
}

// planEntry caches one plan file; mod and size detect changes on disk.
type planEntry struct {
	mod  time.Time
	size int64
	p    *plan.Plan
	err  error
}

// planLoader reloads plans when their file changes. A broken file keeps the
// last valid version active (ADR 0025).
type planLoader struct {
	mu      sync.Mutex
	entries map[string]*planEntry
}

func newPlanLoader() *planLoader {
	return &planLoader{entries: map[string]*planEntry{}}
}

// get returns the current plan and the error of the last load attempt. p is
// nil when the plan was never valid; err may be set even when p is usable.
func (l *planLoader) get(dir, name string) (*plan.Plan, error) {
	path := filepath.Join(dir, name+".yaml")
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if e := l.entries[name]; e != nil && e.mod.Equal(info.ModTime()) && e.size == info.Size() {
		return e.p, e.err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p, parseErr := plan.Parse(bytes.NewReader(data))
	entry := &planEntry{mod: info.ModTime(), size: info.Size(), p: p, err: parseErr}
	if parseErr != nil {
		slog.Error("plan invalid, keeping the last valid version", "plan", name, "error", parseErr)
		if old := l.entries[name]; old != nil && old.p != nil {
			entry.p, entry.err = old.p, parseErr
		}
	} else {
		slog.Info("plan loaded", "plan", name, "rules", len(p.Rules))
	}
	l.entries[name] = entry
	return entry.p, entry.err
}

// uploadPage is the pre-plan behaviour: pages of the profile, paging by button,
// back to page 0 after the timeout. Callers hold s.mu.
func (s *Server) uploadPage(d *Device, st *deviceState, now time.Time, firstContact, special bool, pageTimeout time.Duration) servedPage {
	pages := s.images[d.Profile]
	st.Rule = ""
	switch {
	case special: // double click: back to the overview
		st.Page, st.LastInput = 0, now
	case buttonWake(st.WakeReason) && !firstContact:
		st.Page, st.LastInput = st.Page+1, now
	case st.Page != 0 && now.Sub(st.LastInput) >= pageTimeout:
		st.Page = 0
	}
	if len(pages) == 0 || st.Page >= len(pages) || st.Page < 0 {
		st.Page = 0
	}
	page := st.Page
	refresh := d.refresh(now)
	if page != 0 {
		refresh = min(refresh, max(minRefresh, pageTimeout-now.Sub(st.LastInput)+5*time.Second))
	}
	key := d.Profile + "-setup"
	if len(pages) > 0 {
		img := pages[page]
		key = fmt.Sprintf("%s-%d-%s", d.Profile, page, img.hash)
		if now.Sub(img.uploaded) > time.Duration(s.cfg.StaleAfter) {
			key += "-stale"
		}
		if st.BatteryLevel == batteryWarn {
			key += "-low" + voltageKey(st.BatteryVoltage)
		}
	}
	return servedPage{filename: key, path: "/images/" + key + ".png", refresh: refresh, page: page}
}

// planPage evaluates the device plan and renders the page it selects. Callers
// hold s.mu.
func (s *Server) planPage(d *Device, st *deviceState, now time.Time, firstContact, special bool, pageTimeout time.Duration) servedPage {
	p, err := s.plans.get(s.cfg.Plans, d.Plan)
	if p == nil {
		slog.Error("plan unavailable", "device", d.Name, "plan", d.Plan, "error", err)
		return s.hint(d.Profile, "Plan fehlt", shorten(fmt.Sprintf("%s: %v", d.Plan, err)))
	}
	if err != nil {
		slog.Warn("plan invalid, using the last valid version", "device", d.Name, "plan", d.Plan, "error", err)
	}
	sel, ok := p.Select(now)
	if !ok {
		return s.hint(d.Profile, "Keine Seite festgelegt", "Für diese Situation passt keine Regel und kein fallback.")
	}
	n := len(sel.Pages)
	if switched := st.Rule != sel.Rule; switched {
		// A new rule starts at its first page, the previous position is dropped.
		st.Rule, st.Page, st.LastInput = sel.Rule, 0, now
	} else {
		switch {
		case special: // hold: first page of the rule
			st.Page, st.LastInput = 0, now
		case buttonWake(st.WakeReason) && !firstContact:
			st.Page, st.LastInput = (st.Page+1)%n, now
		case sel.Rotate && !buttonWake(st.WakeReason) && !firstContact:
			st.Page = (st.Page + 1) % n
		case !sel.Rotate && st.Page != 0 && now.Sub(st.LastInput) >= pageTimeout:
			st.Page = 0
		}
	}
	if st.Page < 0 || st.Page >= n {
		st.Page = 0
	}
	page := st.Page
	name := sel.Pages[page]
	refresh := time.Duration(sel.Refresh)
	if refresh == 0 {
		refresh = d.refresh(now)
	}
	refresh = max(refresh, minRefresh)
	// Wake up for the next rule change instead of sleeping through it.
	if until := p.UntilNextBoundary(now); until > 0 && until < refresh {
		refresh = max(minRefresh, until)
	}
	if !sel.Rotate && page != 0 {
		refresh = min(refresh, max(minRefresh, pageTimeout-now.Sub(st.LastInput)+5*time.Second))
	}
	var banners []string
	if st.BatteryLevel == batteryWarn {
		banners = append(banners, lowBatteryText(voltageKey(st.BatteryVoltage)))
	}
	rp, err := s.renderPage(d.Profile, name, now, banners...)
	if err != nil {
		slog.Error("page rendering failed", "device", d.Name, "page", name, "error", err)
		hint := s.hint(d.Profile, "Seite "+name, shorten(err.Error()))
		hint.refresh, hint.page = hintRefresh, page
		return hint
	}
	s.rendered[d.Profile+"/"+name] = rp
	filename := fmt.Sprintf("pages/%s/%s/%s", d.Profile, name, rp.hash)
	return servedPage{filename: filename, path: "/" + filename + ".png", refresh: refresh, page: page}
}

// shorten keeps notice texts on one short line.
func shorten(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > 90 {
		s = string(r[:90]) + "…"
	}
	return s
}
