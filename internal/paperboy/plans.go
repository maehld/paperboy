package paperboy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	"github.com/maehld/paperboy/pkg/plugin"
)

// hintRefresh is reported for pages that only announce a configuration problem,
// so the device comes back after the problem was fixed.
const hintRefresh = 15 * time.Minute

// servedPage is the page an answer points the device to.
type servedPage struct {
	path     string           // image path below the server root
	refresh  time.Duration    // sleep time to report, after the plan boundary and timeout caps
	interval time.Duration    // the regular interval before those caps, for the alignment (ADR 0129)
	name     string           // page or template name for logging; empty for a notice
	page     int              // page index within the plan rule, for logging
	pages    int              // pages in the rule, for logging
	hash     string           // content hash of the PNG or the lines; the device repaints when it changes
	lines    []string         // text displays: the lines to show (ADR 0044)
	scrolls  []page.ScrollBox // text displays: boxes that scroll (ADR 0118)
	err      error            // render error that turned the page into a notice
}

// renderedPage is a page template rendered for one profile and page.
type renderedPage struct {
	data []byte
	hash string
	key  string          // render cache key, per device on device-scoped pages (ADR 0115)
	pix  []byte          // composed palette indexes; an identical composition reuses data and hash
	rect image.Rectangle // bounds of pix
	at   time.Time       // when the image was encoded; the runtime page ages the cache by it (ADR 0104)
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
func (s *Server) renderPage(cfg *Config, profile, name string, now time.Time, pos pagePosition, self string, banners ...string) (rp *renderedPage, err error) {
	defer s.stats.Start("render.page")()
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
	status := s.statusSnapshot(cfg, profile, self, selectedDeviceIDs(tmpl), now)
	img, err := tmpl.RenderImage(s.display(cfg, p), page.Context{Now: now, Page: pos.index + 1, Pages: pos.count,
		Banners: banners, Widgets: s.widgetSource(), ServerStatus: status})
	if err != nil {
		return nil, err
	}
	if took := time.Since(start); took > 2*time.Second {
		slog.Warn("page rendering is slow", "profile", profile, "page", name, "took", took)
	}
	// Short intervals compose the same page again and again (ADR 0051): when
	// the pixels match the last render, its PNG and hash are reused instead of
	// encoding again, so the device sees an unchanged hash and does not redraw.
	key := statusKey(profile, name, tmpl, status)
	s.mu.Lock()
	prev := s.rendered[key]
	s.mu.Unlock()
	if prev != nil && prev.rect == img.Rect && bytes.Equal(prev.pix, img.Pix) {
		return prev, nil
	}
	encodeDone := s.stats.Start("render.encode")
	data := encodePNG(img)
	encodeDone()
	return &renderedPage{data: data, hash: hashBytes(data), key: key, pix: img.Pix, rect: img.Rect, at: time.Now()}, nil
}

// deviceInfo names a device for a plugin request (ADR 0111).
func pluginDevice(d *Device) *plugin.DeviceInfo {
	return &plugin.DeviceInfo{ID: d.ID}
}

// display describes a display type for the page package.
func (s *Server) display(cfg *Config, p Profile) page.Display {
	return page.Display{Width: p.Width, Height: p.Height, Levels: p.Levels, Text: p.text(), Charset: p.Charset, PPI: p.PPI,
		Language: cfg.Language, Timezone: cfg.Timezone}
}

// hashText is the content hash of a text page. The full text of scrolling
// boxes is part of it, so a change behind the visible window still repaints the
// display (ADR 0118).
func hashText(lines []string, scrolls []page.ScrollBox) string {
	data := strings.Join(lines, "\n")
	if len(scrolls) > 0 {
		raw, _ := json.Marshal(scrolls)
		data += "\x00" + string(raw)
	}
	return hashBytes([]byte(data))
}

// legacyScroll fills the old per-row scroll list for horizontal, single-row
// boxes, so an older firmware keeps working until it reads scrolls (ADR 0118).
func legacyScroll(scrolls []page.ScrollBox, height int) []string {
	if len(scrolls) == 0 {
		return nil
	}
	out := make([]string, height)
	used := false
	for _, box := range scrolls {
		if box.Direction != page.ScrollHorizontal || box.Span[1] != 1 || len(box.Lines) == 0 {
			continue
		}
		row := box.At[1]
		if row >= 0 && row < height {
			out[row] = box.Lines[0]
			used = true
		}
	}
	if !used {
		return nil
	}
	return out
}

// renderText renders pages/<name>.<profile>.yaml for a text display type
// (ADR 0044). The page lays out plugin values (ADR 0111); device names the
// display a device render is for, nil for a preview. self is the device the
// built-in status widget may show (ADR 0115). Like renderPage it turns a
// panic into an error.
func (s *Server) renderText(cfg *Config, profile, name string, now time.Time, pos pagePosition, self string, device *plugin.DeviceInfo) (text page.Text, err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("text page rendering panicked", "profile", profile, "page", name, "panic", r, "stack", string(debug.Stack()))
			text, err = page.Text{}, fmt.Errorf("page rendering panicked: %v", r)
		}
	}()
	p, ok := cfg.Profiles[profile]
	if !ok || !p.text() {
		return page.Text{}, fmt.Errorf("unknown text display type %q", profile)
	}
	tmpl, err := s.pages.get(cfg.Pages, name, profile)
	if err != nil {
		return page.Text{}, err
	}
	status := s.statusSnapshot(cfg, profile, self, selectedDeviceIDs(tmpl), now)
	return tmpl.RenderText(s.display(cfg, p), page.Context{Now: now, Page: pos.index + 1, Pages: pos.count,
		Values: s.valueSource(), Device: device, ServerStatus: status})
}

// hintPage is a generated notice page and the display type it was drawn for:
// notice pages contain a page of the instance (the menu is drawn over one), so
// a device may only fetch those of its own type (ADR 0097).
type hintPage struct {
	data    []byte
	profile string
	at      time.Time // when the notice was drawn (ADR 0104)
	used    time.Time // last download or menu use, for the LRU eviction (ADR 0139)
}

// maxHints bounds the generated notice pages in memory (ADR 0139). A page that
// falls out is drawn again on the next request; a device with an old URL gets
// 404 and keeps its screen until the next sync.
const maxHints = 500

// notice registers a generated page and returns where it is served. Only the
// registration needs the lock; drawing happens before.
func (s *Server) notice(profile string, img *image.Paletted) servedPage {
	data := encodePNG(img)
	key := hashBytes(data)
	now := time.Now()
	s.mu.Lock()
	s.hints[key] = hintPage{data: data, profile: profile, at: now, used: now}
	s.pruneHintsLocked()
	s.mu.Unlock()
	return servedPage{path: "/hints/" + key + ".png", hash: key}
}

// pruneHintsLocked drops the least recently used notice pages above the cap.
// Callers hold s.mu.
func (s *Server) pruneHintsLocked() {
	for len(s.hints) > maxHints {
		oldest, oldestAt := "", time.Time{}
		for key, hp := range s.hints {
			at := hp.used
			if at.IsZero() {
				at = hp.at
			}
			if oldest == "" || at.Before(oldestAt) {
				oldest, oldestAt = key, at
			}
		}
		delete(s.hints, oldest)
	}
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
		return servedPage{lines: lines, hash: hashText(lines, nil), refresh: time.Minute}
	}
	sp := s.notice(profile, placeholder(p, title, text))
	sp.refresh = hintRefresh
	return sp
}

// storePage caches a rendered page for /pages/{profile}/{page}/{file} and
// returns where the device can fetch it.
func (s *Server) storePage(profile, name string, rp *renderedPage) servedPage {
	key := rp.key
	if key == "" {
		key = profile + "/" + name
	}
	s.mu.Lock()
	if prev := s.rendered[key]; prev != nil && prev.hash != rp.hash {
		s.forgetRenderedLocked(key, prev.hash)
	}
	s.rendered[key] = rp
	s.renderedHash[rp.hash] = rp
	s.pruneRenderedLocked(time.Now())
	s.mu.Unlock()
	return servedPage{path: fmt.Sprintf("/pages/%s/%s/%s.png", profile, name, rp.hash), hash: rp.hash}
}

// maxRendered and renderedTTL bound the rendered page cache (ADR 0139). The
// cache is memory only; a dropped entry is composed again on the next request.
const (
	maxRendered = 256
	renderedTTL = 24 * time.Hour
)

// pruneRenderedLocked drops stale renders and the oldest entries above the
// cap. Callers hold s.mu.
func (s *Server) pruneRenderedLocked(now time.Time) {
	for key, rp := range s.rendered {
		if !rp.at.IsZero() && now.Sub(rp.at) > renderedTTL {
			s.forgetRenderedLocked(key, rp.hash)
			delete(s.rendered, key)
		}
	}
	for len(s.rendered) > maxRendered {
		oldest, oldestAt := "", time.Time{}
		for key, rp := range s.rendered {
			if oldest == "" || rp.at.Before(oldestAt) {
				oldest, oldestAt = key, rp.at
			}
		}
		s.forgetRenderedLocked(oldest, s.rendered[oldest].hash)
		delete(s.rendered, oldest)
	}
}

// forgetRenderedLocked drops an old hash unless another cache key still names
// it; callers hold s.mu.
func (s *Server) forgetRenderedLocked(key, hash string) {
	for k, rp := range s.rendered {
		if k != key && rp.hash == hash {
			return
		}
	}
	delete(s.renderedHash, hash)
}

// renderedPageLocked finds the render behind a content hash and falls back to
// the last render of the page; callers hold s.mu.
func (s *Server) renderedPageLocked(profile, page, hash string) *renderedPage {
	if rp := s.renderedHash[hash]; rp != nil {
		return rp
	}
	if rp := s.rendered[profile+"/"+page]; rp != nil && rp.hash == hash {
		return rp
	}
	return nil
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

// stats reports what the plan loader keeps: the number of cached plan files
// and their size on disk (ADR 0104).
func (l *planLoader) stats() (files int, bytes int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.entries {
		files++
		bytes += e.size
	}
	return files, bytes
}

// planChoice is the page a plan picked for one device request, resolved without
// rendering: either a page template or a notice to show.
type planChoice struct {
	page     int
	pages    int
	name     string        // page template; empty when only a notice is shown
	title    string        // notice title
	text     string        // notice text
	err      error         // reason for the notice, shown as the device's last error
	refresh  time.Duration // sleep time to report, after the plan boundary and timeout caps
	interval time.Duration // the regular interval before those caps, for the alignment (ADR 0129)
	banners  []string
	alert    bool              // let the device alert with this answer (ADR 0131)
	actions  []plan.PlanAction // the plan's actions for the device menu (ADR 0057)
}

// planResolution is a device plan evaluated before the state lock: the plan,
// the rule it selects and the pages of that rule that fit the display type. A
// slow page read therefore never blocks the other devices (ADR 0138).
type planResolution struct {
	plan      *plan.Plan
	err       error
	selection plan.Selection
	selected  bool
	pages     []string  // pages of the selected rule that fit, in rule order
	skipped   []string  // "page: reason" for the pages that do not fit
	fitNotes  []pageFit // one entry per page of the selected rule
}

// pageFit is one page of the selected rule and why it does not fit; an empty
// reason means it fits.
type pageFit struct {
	page   string
	reason string
}

// resolvePlan loads the plan, selects its rule and keeps the pages that fit the
// device's display type. It runs outside the state lock; a missing plan or a
// failing page is part of the answer, not an error of the caller.
func (s *Server) resolvePlan(cfg *Config, d *Device, now time.Time, values plan.Values) planResolution {
	r := planResolution{}
	if d.Plan == "" {
		return r
	}
	r.plan, r.err = s.plans.get(cfg.Plans, d.Plan, cfg.Timezone)
	if r.plan == nil {
		return r
	}
	sel, ok := r.plan.Select(now, values)
	if !ok {
		return r
	}
	r.selection, r.selected = sel, true
	for _, name := range sel.Pages {
		reason := ""
		if err := s.fits(cfg, d.Profile, name); err == nil {
			r.pages = append(r.pages, name)
		} else {
			reason = err.Error()
			r.skipped = append(r.skipped, name+": "+reason)
		}
		r.fitNotes = append(r.fitNotes, pageFit{page: name, reason: reason})
	}
	return r
}

// announceSkippedLocked updates the skip notes and logs a page that stopped
// fitting once per display type and page. Callers hold s.mu.
func (s *Server) announceSkippedLocked(d *Device, r planResolution) {
	for _, f := range r.fitNotes {
		key := d.Profile + "/" + f.page
		if f.reason == "" {
			delete(s.skipNotes, key)
			continue
		}
		if s.skipNotes[key] != f.reason {
			s.skipNotes[key] = f.reason
			slog.Info("page skipped for display type", "display_type", d.Profile, "page", f.page, "reason", f.reason)
		}
	}
}

// choosePlanPage applies a resolved plan under the state lock and advances the
// page cursor. Callers hold s.mu and pass the configuration snapshot of the
// request: plan loading and page reads happen before the lock (ADR 0138).
// Rendering happens later in renderPlan. paused freezes paging while the
// device menu is open (ADR 0057): the page stays and a timed change waits.
// timed marks a timed refresh, which is when a rule with alert: refresh alerts
// again (ADR 0131). A rule change always starts at the first page (ADR 0130).
func (s *Server) choosePlanPage(res planResolution, cfg *Config, d *Device, st *deviceState, now time.Time, firstContact, timed bool, buttons []string, paused, quiet bool) planChoice {
	p, err := res.plan, res.err
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
	s.announceSkippedLocked(d, res)
	if !res.selected {
		title, text := cfg.texts().noSelection()
		return planChoice{title: title, text: text}
	}
	sel, pages, skipped := res.selection, res.pages, res.skipped
	if len(pages) == 0 {
		title, text := cfg.texts().noFittingPage(sel.Rule, d.Profile)
		return planChoice{title: title, text: shorten(text + " " + joinReasons(skipped)),
			err: fmt.Errorf("rule %s has no page for display type %s: %s", sel.Rule, d.Profile, joinReasons(skipped))}
	}
	n := len(pages)
	ruleChanged := st.Rule != sel.Rule
	if ruleChanged {
		// A new rule always starts at its first page, also with current.
		st.Rule, st.Page = sel.Rule, 0
	} else {
		if st.PageName != "" && (st.Page < 0 || st.Page >= n || pages[st.Page] != st.PageName) {
			// The cursor holds the page, not its index: a plan edit that adds,
			// removes or reorders pages keeps the device on its page, and a
			// page the rule lost starts the rule at its first page (ADR 0133).
			st.Page = pageIndex(pages, st.PageName)
		}
		// The menu is modal: no paging and no automatic page change.
		if !paused {
			// Button roles apply in order (ADR 0042): presses made while the
			// panel was painting arrive together and only the final page is
			// rendered.
			for _, role := range buttons {
				switch role {
				case buttonNext:
					st.Page = (st.Page + 1) % n
				case buttonPrevious:
					st.Page = ((st.Page-1)%n + n) % n
				case buttonHome:
					st.Page = 0
				}
			}
			if len(buttons) == 0 && !firstContact {
				switch sel.OnRefresh {
				case plan.RefreshCycle:
					st.Page = (st.Page + 1) % n
				case plan.RefreshFirst:
					st.Page = 0
				}
			}
		}
	}
	if st.Page < 0 || st.Page >= n {
		st.Page = 0
	}
	page := st.Page
	st.PageName = pages[page]
	floor := cfg.minRefresh(d, st.ReportedPower)
	interval := time.Duration(sel.Refresh)
	if interval == 0 {
		interval = d.refresh()
	}
	interval = max(interval, floor)
	refresh := interval
	// Wake up for the next rule change instead of sleeping through it.
	if until := p.UntilNextBoundary(now); until > 0 && until < refresh {
		refresh = max(floor, until)
	}
	alert := false
	switch sel.Alert {
	case plan.AlertOnce:
		alert = ruleChanged
	case plan.AlertRefresh:
		alert = ruleChanged || timed
	}
	c := planChoice{page: page, pages: n, name: pages[page], refresh: refresh, interval: interval, actions: sel.Actions,
		alert: alert}
	if st.BatteryLevel == batteryWarn {
		c.banners = append(c.banners, cfg.texts().lowBattery(voltageKey(st.BatteryVoltage)))
	}
	return c
}

// pageIndex follows a page name through the pages of a rule. A page that the
// rule no longer lists starts the rule at its first page; a duplicate name is
// the first entry, which the plan editor already warns about.
func pageIndex(pages []string, name string) int {
	for i, p := range pages {
		if p == name {
			return i
		}
	}
	return 0
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
		text, err := s.renderText(cfg, d.Profile, c.name, now, pagePosition{index: c.page, count: c.pages}, d.ID, pluginDevice(d))
		if err != nil {
			return s.pageFailed(cfg, d, c, err, quiet)
		}
		return servedPage{lines: text.Lines, scrolls: text.Scrolls, hash: hashText(text.Lines, text.Scrolls),
			name: c.name, page: c.page, pages: c.pages, refresh: c.refresh, interval: c.interval}
	}
	rp, err := s.renderPage(cfg, d.Profile, c.name, now, pagePosition{index: c.page, count: c.pages}, d.ID, c.banners...)
	if err != nil {
		return s.pageFailed(cfg, d, c, err, quiet)
	}
	sp := s.storePage(d.Profile, c.name, rp)
	sp.name, sp.page, sp.pages, sp.refresh, sp.interval = c.name, c.page, c.pages, c.refresh, c.interval
	return sp
}

// pageFailed turns a render error into a notice page; its log line says so
// instead of naming a page that was not drawn.
func (s *Server) pageFailed(cfg *Config, d *Device, c planChoice, err error, quiet bool) servedPage {
	if !quiet {
		slog.Error("page rendering failed", "device", d.ID, "page", c.name, "error", err)
	}
	sp := s.hint(cfg, d.Profile, cfg.texts().pageError(c.name), shorten(err.Error()))
	sp.page, sp.pages, sp.interval = c.page, c.pages, c.interval
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
