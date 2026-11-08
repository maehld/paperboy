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
	pix  []byte          // composed palette indexes; an identical composition reuses data and hash
	rect image.Rectangle // bounds of pix
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

// renderPage composes pages/<name>.<profile>.yaml (or pages/<name>.yaml) from
// plugin widgets.
// A panicking template or font problem is reported as an error instead of
// tearing down the request, so the device can be served a notice page. cfg is
// the configuration snapshot of the request; the render path never reloads the
// configuration itself, so a device sees one configuration from start to end.
func (s *Server) renderPage(cfg *Config, profile, name string, now time.Time, pos pagePosition, banners ...string) (rp *renderedPage, err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("page rendering panicked", "profile", profile, "page", name, "panic", r, "stack", string(debug.Stack()))
			rp, err = nil, fmt.Errorf("page rendering panicked: %v", r)
		}
	}()
	start := time.Now()
	p, ok := cfg.Profiles[profile]
	if !ok {
		return nil, fmt.Errorf("unknown display type %q", profile)
	}
	tmpl, err := s.pages.get(cfg.Pages, name, profile)
	if err != nil {
		return nil, err
	}
	img, err := tmpl.RenderImage(s.display(cfg, p), page.Context{Now: now, Page: pos.index + 1, Pages: pos.count,
		Banners: banners, Widgets: s.widgetSource()})
	if err != nil {
		return nil, err
	}
	if took := time.Since(start); took > 2*time.Second {
		slog.Warn("page rendering is slow", "profile", profile, "page", name, "took", took)
	}
	// Short intervals compose the same page again and again (ADR 0051): when
	// the pixels match the last render, its PNG and hash are reused instead of
	// encoding again, so the device sees an unchanged hash and does not redraw.
	s.mu.Lock()
	prev := s.rendered[profile+"/"+name]
	s.mu.Unlock()
	if prev != nil && prev.rect == img.Rect && bytes.Equal(prev.pix, img.Pix) {
		return prev, nil
	}
	data := encodePNG(img)
	return &renderedPage{data: data, hash: hashBytes(data), pix: img.Pix, rect: img.Rect}, nil
}

// display describes a display type for the page package.
func (s *Server) display(cfg *Config, p Profile) page.Display {
	return page.Display{Width: p.Width, Height: p.Height, Gray: p.Gray, Text: p.text(), Charset: p.Charset, PPI: p.PPI,
		Language: cfg.Language, Timezone: cfg.Timezone}
}

// hashLines is the content hash of a text page.
func hashLines(lines []string) string {
	return hashBytes([]byte(strings.Join(lines, "\n")))
}

// renderText renders pages/<name>.<profile>.yaml for a text display type
// (ADR 0044). Like renderPage it turns a panic into an error.
func (s *Server) renderText(cfg *Config, profile, name string, now time.Time, pos pagePosition) (lines []string, err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("text page rendering panicked", "profile", profile, "page", name, "panic", r, "stack", string(debug.Stack()))
			lines, err = nil, fmt.Errorf("page rendering panicked: %v", r)
		}
	}()
	p, ok := cfg.Profiles[profile]
	if !ok || !p.text() {
		return nil, fmt.Errorf("unknown text display type %q", profile)
	}
	tmpl, err := s.pages.get(cfg.Pages, name, profile)
	if err != nil {
		return nil, err
	}
	return tmpl.RenderText(s.display(cfg, p), page.Context{Now: now, Page: pos.index + 1, Pages: pos.count,
		Widgets: s.widgetSource()})
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
func (s *Server) hint(cfg *Config, profile, title, text string) servedPage {
	p, ok := cfg.Profiles[profile]
	if !ok { // never expected: the device's display type was validated
		p = Profile{Width: 400, Height: 300}
	}
	if p.text() {
		lines := page.NoticeLines(s.display(cfg, p), title, text)
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
	actions []plan.PlanAction // the plan's actions for the device menu (ADR 0057)
}

// choosePlanPage evaluates the device plan and advances the rotation state.
// Callers hold s.mu and pass the configuration snapshot of the request: no
// config access happens under the state lock (ADR 0047). Rendering happens
// later in renderPlan. paused freezes paging while the device menu is open
// (ADR 0057): the page stays and rotation waits.
func (s *Server) choosePlanPage(cfg *Config, d *Device, st *deviceState, now time.Time, firstContact bool, buttons []string, pageTimeout time.Duration, paused, quiet bool) planChoice {
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
	pages, skipped := s.fittingPages(cfg, d.Profile, sel.Pages)
	if len(pages) == 0 {
		title, text := cfg.texts().noFittingPage(sel.Rule, d.Profile)
		return planChoice{title: title, text: shorten(text + " " + joinReasons(skipped)),
			err: fmt.Errorf("rule %s has no page for display type %s: %s", sel.Rule, d.Profile, joinReasons(skipped))}
	}
	n := len(pages)
	if switched := st.Rule != sel.Rule; switched {
		// A new rule starts at its first page, the previous position is dropped.
		st.Rule, st.Page, st.LastInput = sel.Rule, 0, now
	} else if paused {
		// The menu is modal: no paging, no rotation, no page timeout.
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
	floor := cfg.minRefresh(d, st.ReportedPower)
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
	c := planChoice{page: page, pages: n, name: pages[page], refresh: refresh, actions: sel.Actions}
	if st.BatteryLevel == batteryWarn {
		c.banners = append(c.banners, cfg.texts().lowBattery(voltageKey(st.BatteryVoltage)))
	}
	return c
}

// renderPlan draws the chosen page or notice outside s.mu. A broken template
// becomes a notice page so that a device is never left without an answer.
// quiet suppresses the error log for the text poll, which repeats every
// second; the next sync logs it.
func (s *Server) renderPlan(cfg *Config, d *Device, c planChoice, now time.Time, quiet bool) servedPage {
	if c.name == "" {
		sp := s.hint(cfg, d.Profile, c.title, c.text)
		sp.page, sp.pages = c.page, c.pages
		return sp
	}
	if cfg.Profiles[d.Profile].text() {
		lines, err := s.renderText(cfg, d.Profile, c.name, now, pagePosition{index: c.page, count: c.pages})
		if err != nil {
			return s.pageFailed(cfg, d, c, err, quiet)
		}
		return servedPage{lines: lines, hash: hashLines(lines), name: c.name, page: c.page, pages: c.pages, refresh: c.refresh}
	}
	rp, err := s.renderPage(cfg, d.Profile, c.name, now, pagePosition{index: c.page, count: c.pages}, c.banners...)
	if err != nil {
		return s.pageFailed(cfg, d, c, err, quiet)
	}
	sp := s.storePage(d.Profile, c.name, rp)
	sp.name, sp.page, sp.pages, sp.refresh = c.name, c.page, c.pages, c.refresh
	return sp
}

// pageFailed turns a render error into a notice page; its log line says so
// instead of naming a page that was not drawn.
func (s *Server) pageFailed(cfg *Config, d *Device, c planChoice, err error, quiet bool) servedPage {
	if !quiet {
		slog.Error("page rendering failed", "device", d.ID, "page", c.name, "error", err)
	}
	sp := s.hint(cfg, d.Profile, cfg.texts().pageError(c.name), shorten(err.Error()))
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
