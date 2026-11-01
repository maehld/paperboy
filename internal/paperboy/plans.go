package paperboy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
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
	path    string        // image path below the server root
	refresh time.Duration // sleep time to report
	name    string        // page or template name for logging; empty for a notice
	page    int           // page index within the plan rule, for logging
	pages   int           // pages in the rule, for logging
	hash    string        // content hash of the PNG or the lines; the device repaints when it changes
	lines   []string      // text displays: the lines to show (ADR 0044)
	err     error         // render error that turned the page into a notice
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

// pagePosition is the 0-based page index and page count of a plan rule. The
// preview uses a single page so that no indicator is appended.
type pagePosition struct {
	index int
	count int
}

// renderPage renders pages/<name>.<profile>.yaml from the data document.
// A panicking template or font problem is reported as an error instead of
// tearing down the request, so the device can be served a notice page.
func (s *Server) renderPage(profile, name string, now time.Time, pos pagePosition, banners ...string) (rp *renderedPage, err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("page rendering panicked", "profile", profile, "page", name, "panic", r, "stack", string(debug.Stack()))
			rp, err = nil, fmt.Errorf("page rendering panicked: %v", r)
		}
	}()
	start := time.Now()
	cfg := s.config()
	p, ok := cfg.Profiles[profile]
	if !ok {
		return nil, fmt.Errorf("unknown display type %q", profile)
	}
	tmpl, err := page.Load(cfg.Pages, name, profile)
	if err != nil {
		return nil, err
	}
	img, err := tmpl.RenderContext(s.doc, page.Display{
		Width: p.Width, Height: p.Height, Gray: p.Gray, Language: cfg.Language, Timezone: cfg.Timezone,
	}, page.Context{Now: now, Page: pos.index + 1, Pages: pos.count, Banners: banners})
	if err != nil {
		return nil, err
	}
	if took := time.Since(start); took > 2*time.Second {
		slog.Warn("page rendering is slow", "profile", profile, "page", name, "took", took)
	}
	data := encodePNG(img)
	return &renderedPage{data: data, hash: hashBytes(data)}, nil
}

// textDisplay describes a text display type for the page package.
func (s *Server) textDisplay(p Profile) page.TextDisplay {
	cfg := s.config()
	return page.TextDisplay{Columns: p.Width, Rows: p.Height, Charset: p.Charset,
		Language: cfg.Language, Timezone: cfg.Timezone}
}

// hashLines is the content hash of a text page.
func hashLines(lines []string) string {
	return hashBytes([]byte(strings.Join(lines, "\n")))
}

// renderText renders pages/<name>.<profile>.yaml for a text display type
// (ADR 0044). Like renderPage it turns a panic into an error.
func (s *Server) renderText(profile, name string, now time.Time, pos pagePosition) (lines []string, err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("text page rendering panicked", "profile", profile, "page", name, "panic", r, "stack", string(debug.Stack()))
			lines, err = nil, fmt.Errorf("page rendering panicked: %v", r)
		}
	}()
	cfg := s.config()
	p, ok := cfg.Profiles[profile]
	if !ok || !p.text() {
		return nil, fmt.Errorf("unknown text display type %q", profile)
	}
	tmpl, err := page.LoadText(cfg.Pages, name, profile)
	if err != nil {
		return nil, err
	}
	return tmpl.Render(s.doc, s.textDisplay(p), page.Context{Now: now, Page: pos.index + 1, Pages: pos.count})
}

// notice registers a generated page and returns where it is served. Only the
// registration needs the lock; drawing happens before.
func (s *Server) notice(profile string, img *image.Paletted) servedPage {
	data := encodePNG(img)
	key := hashBytes(data)
	s.mu.Lock()
	s.hints[key] = data
	s.mu.Unlock()
	return servedPage{path: "/hints/" + key + ".png", hash: key}
}

// hint registers a notice page with a title and a line of text. It draws the
// page before touching the server state and may be called without the lock.
func (s *Server) hint(profile, title, text string) servedPage {
	cfg := s.config()
	p, ok := cfg.Profiles[profile]
	if !ok { // never expected: the device's display type was validated
		p = Profile{Width: 400, Height: 300}
	}
	if p.text() {
		lines := page.NoticeLines(s.textDisplay(p), title, text)
		// Text displays come back sooner: their sync is cheap and they never sleep.
		return servedPage{lines: lines, hash: hashLines(lines), refresh: time.Minute}
	}
	sp := s.notice(profile, placeholder(p, title, text))
	sp.refresh = hintRefresh
	return sp
}

// storePage caches a rendered page for /pages/{profile}/{page}/{file} and
// returns where the device can fetch it.
func (s *Server) storePage(profile, name string, rp *renderedPage) servedPage {
	s.mu.Lock()
	s.rendered[profile+"/"+name] = rp
	s.mu.Unlock()
	return servedPage{path: fmt.Sprintf("/pages/%s/%s/%s.png", profile, name, rp.hash), hash: rp.hash}
}

// planEntry caches one plan file; mod and size detect changes on disk. The
// timezone belongs to the cache key too: a changed global timezone re-parses
// the plan even when the file itself did not change (ADR 0047).
type planEntry struct {
	mod  time.Time
	size int64
	loc  string // timezone name the plan was parsed with
	p    *plan.Plan
	err  error
}

// locName names a timezone for the plan cache key; the system local timezone
// gets a stable name that does not depend on the process location.
func locName(loc *time.Location) string {
	if loc == nil {
		return "local"
	}
	return loc.String()
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
// nil when the plan was never valid; err may be set even when p is usable. Time
// windows use loc, the global timezone (ADR 0033); a changed timezone re-parses
// the plan, so reloading the configuration also moves the plan rules (ADR 0047).
func (l *planLoader) get(dir, name string, loc *time.Location) (*plan.Plan, error) {
	path := filepath.Join(dir, name+".yaml")
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	zone := locName(loc)
	if e := l.entries[name]; e != nil && e.mod.Equal(info.ModTime()) && e.size == info.Size() && e.loc == zone {
		return e.p, e.err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p, parseErr := plan.Parse(bytes.NewReader(data), loc)
	entry := &planEntry{mod: info.ModTime(), size: info.Size(), loc: zone, p: p, err: parseErr}
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

// planChoice is the page a plan picked for one device request, resolved without
// rendering: either a page template or a notice to show.
type planChoice struct {
	page    int
	pages   int
	name    string        // page template; empty when only a notice is shown
	title   string        // notice title
	text    string        // notice text
	err     error         // reason for the notice, shown as the device's last error
	refresh time.Duration // sleep time to report
	banners []string
}

// choosePlanPage evaluates the device plan and advances the rotation state.
// Callers hold s.mu; rendering happens later in renderPlan.
func (s *Server) choosePlanPage(d *Device, st *deviceState, now time.Time, firstContact bool, buttons []string, pageTimeout time.Duration, quiet bool) planChoice {
	cfg := s.config()
	p, err := s.plans.get(cfg.Plans, d.Plan, cfg.Timezone)
	if p == nil {
		if !quiet {
			slog.Error("plan unavailable", "device", d.ID, "plan", d.Plan, "error", err)
		}
		title, text := cfg.texts().planMissing(), shorten(fmt.Sprintf("%s: %v", d.Plan, err))
		if err == nil {
			err = fmt.Errorf("plan %s missing", d.Plan)
		}
		return planChoice{title: title, text: text, err: err}
	}
	if err != nil && !quiet {
		slog.Warn("plan invalid, using the last valid version", "device", d.ID, "plan", d.Plan, "error", err)
	}
	sel, ok := p.Select(now)
	if !ok {
		title, text := cfg.texts().noSelection()
		return planChoice{title: title, text: text}
	}
	n := len(sel.Pages)
	if switched := st.Rule != sel.Rule; switched {
		// A new rule starts at its first page, the previous position is dropped.
		st.Rule, st.Page, st.LastInput = sel.Rule, 0, now
	} else {
		// Button roles apply in order (ADR 0042): presses made while the panel
		// was painting arrive together and only the final page is rendered.
		for _, role := range buttons {
			switch role {
			case buttonNext:
				st.Page = (st.Page + 1) % n
			case buttonPrevious:
				st.Page = ((st.Page-1)%n + n) % n
			case buttonHome:
				st.Page = 0
			}
			st.LastInput = now
		}
		switch {
		case len(buttons) > 0:
		case sel.Rotate && !firstContact:
			st.Page = (st.Page + 1) % n
		case !sel.Rotate && st.Page != 0 && now.Sub(st.LastInput) >= pageTimeout:
			st.Page = 0
		}
	}
	if st.Page < 0 || st.Page >= n {
		st.Page = 0
	}
	page := st.Page
	floor := cfg.minRefresh(d.Profile)
	refresh := time.Duration(sel.Refresh)
	if refresh == 0 {
		refresh = d.refresh()
	}
	refresh = max(refresh, floor)
	// Wake up for the next rule change instead of sleeping through it.
	if until := p.UntilNextBoundary(now); until > 0 && until < refresh {
		refresh = max(floor, until)
	}
	if !sel.Rotate && page != 0 {
		refresh = min(refresh, max(floor, pageTimeout-now.Sub(st.LastInput)+5*time.Second))
	}
	c := planChoice{page: page, pages: n, name: sel.Pages[page], refresh: refresh}
	if st.BatteryLevel == batteryWarn {
		c.banners = append(c.banners, cfg.texts().lowBattery(voltageKey(st.BatteryVoltage)))
	}
	return c
}

// renderPlan draws the chosen page or notice outside s.mu. A broken template
// becomes a notice page so that a device is never left without an answer.
// quiet suppresses the error log for the text poll, which repeats every
// second; the next sync logs it.
func (s *Server) renderPlan(d *Device, c planChoice, now time.Time, quiet bool) servedPage {
	if c.name == "" {
		sp := s.hint(d.Profile, c.title, c.text)
		sp.page, sp.pages = c.page, c.pages
		return sp
	}
	cfg := s.config()
	if cfg.Profiles[d.Profile].text() {
		lines, err := s.renderText(d.Profile, c.name, now, pagePosition{index: c.page, count: c.pages})
		if err != nil {
			return s.pageFailed(d, c, err, quiet)
		}
		return servedPage{lines: lines, hash: hashLines(lines), name: c.name, page: c.page, pages: c.pages, refresh: c.refresh}
	}
	rp, err := s.renderPage(d.Profile, c.name, now, pagePosition{index: c.page, count: c.pages}, c.banners...)
	if err != nil {
		return s.pageFailed(d, c, err, quiet)
	}
	sp := s.storePage(d.Profile, c.name, rp)
	sp.name, sp.page, sp.pages, sp.refresh = c.name, c.page, c.pages, c.refresh
	return sp
}

// pageFailed turns a render error into a notice page; its log line says so
// instead of naming a page that was not drawn.
func (s *Server) pageFailed(d *Device, c planChoice, err error, quiet bool) servedPage {
	if !quiet {
		slog.Error("page rendering failed", "device", d.ID, "page", c.name, "error", err)
	}
	sp := s.hint(d.Profile, s.config().texts().pageError(c.name), shorten(err.Error()))
	sp.page, sp.pages = c.page, c.pages
	sp.err = err
	return sp
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

// shortError keeps a rendered error readable in the devices API and the state
// file: long enough for the file name and the YAML message, short enough to
// stay on one line.
func shortError(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200]) + "…"
	}
	return s
}
