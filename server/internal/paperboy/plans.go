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
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/server/internal/page"
	"github.com/maehld/paperboy/server/internal/plan"
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
	page     int              // page index within the plan pages, for logging
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

// pagePosition is the 0-based page index and page count of the plan pages. The
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
// notice pages contain a page of the instance, so
// a device may only fetch those of its own type (ADR 0097).
type hintPage struct {
	data    []byte
	profile string
	at      time.Time // when the notice was drawn (ADR 0104)
	used    time.Time // last download or notice use, for the LRU eviction (ADR 0139)
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
	now := s.now()
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
// the plan, so reloading the configuration also moves the plan windows (ADR 0047).
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
		slog.Info("plan loaded", "plan", name, "pages", len(p.Pages))
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
	alert    bool // let the device alert with this answer (ADR 0131)
}

// planResolution is a device plan evaluated before the state lock: the plan,
// the trigger that matches the values and which pages fit the display type. A
// slow page read therefore never blocks the other devices (ADR 0138).
type planResolution struct {
	plan    *plan.Plan
	err     error
	trigger string // page of the first matching trigger that fits; "" = none
	alert   plan.AlertMode
	fit     map[string]string // declared page → reason it does not fit ("" = fits)
	noted   map[string]string // pages worth a skip line: visible pages and trigger targets
	skipped []string          // visible pages that do not fit, "page: reason"
}

// resolvePlan loads the plan and checks every declared page against the
// display type. It runs outside the state lock; a missing plan or a failing
// page is part of the answer, not an error of the caller.
func (s *Server) resolvePlan(cfg *Config, d *Device, now time.Time, values plan.Values) planResolution {
	r := planResolution{fit: map[string]string{}, noted: map[string]string{}}
	if d.Plan == "" {
		return r
	}
	r.plan, r.err = s.plans.get(cfg.Plans, d.Plan, cfg.Timezone)
	if r.plan == nil {
		return r
	}
	for _, pg := range r.plan.Pages {
		if err := s.fits(cfg, d.Profile, pg.Name); err != nil {
			r.fit[pg.Name] = err.Error()
		}
		if !pg.Hidden {
			r.noted[pg.Name] = r.fit[pg.Name]
			if r.fit[pg.Name] != "" {
				r.skipped = append(r.skipped, pg.Name+": "+r.fit[pg.Name])
			}
		}
	}
	for _, tr := range r.plan.Triggers {
		r.noted[tr.Page] = r.fit[tr.Page]
	}
	for _, tr := range r.plan.Matching(now, values) {
		if r.fit[tr.Page] != "" {
			continue // the page does not fit; the next matching trigger may
		}
		r.trigger, r.alert = tr.Page, tr.Alert
		break
	}
	return r
}

// effective returns the page order a device sees, without the pages that do
// not fit its display type.
func (r planResolution) effective(trigger string) []string {
	out := make([]string, 0, len(r.plan.Pages))
	for _, name := range r.plan.Effective(trigger) {
		if r.fit[name] == "" {
			out = append(out, name)
		}
	}
	return out
}

// resolve puts a cursor on a page of the list. A page that left the list is
// resolved by the refresh mode (ADR 0158): current takes the page before the
// trigger, cycle the next page at the declared position, first the first page.
func (r planResolution) resolve(name string, pages []string, mode plan.RefreshMode, before string) string {
	if len(pages) == 0 {
		return ""
	}
	if slices.Contains(pages, name) {
		return name
	}
	switch mode {
	case plan.RefreshCurrent:
		if before != "" && slices.Contains(pages, before) {
			return before
		}
	case plan.RefreshCycle:
		if next := r.nextAfter(name, pages); next != "" {
			return next
		}
	}
	return pages[0]
}

// nextAfter finds the next page of the list after name in the plan's declared
// order, wrapping once. A name the plan no longer lists has no next page.
func (r planResolution) nextAfter(name string, pages []string) string {
	start := -1
	for i, pg := range r.plan.Pages {
		if pg.Name == name {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	for k := 1; k <= len(r.plan.Pages); k++ {
		candidate := r.plan.Pages[(start+k)%len(r.plan.Pages)].Name
		if slices.Contains(pages, candidate) {
			return candidate
		}
	}
	return ""
}

// announceSkippedLocked updates the skip notes and logs a page that stopped
// fitting once per display type and page. Callers hold s.mu.
func (s *Server) announceSkippedLocked(d *Device, r planResolution) {
	for name, reason := range r.noted {
		key := d.Profile + "/" + name
		if reason == "" {
			delete(s.skipNotes, key)
			continue
		}
		if s.skipNotes[key] != reason {
			s.skipNotes[key] = reason
			slog.Info("page skipped for display type", "display_type", d.Profile, "page", name, "reason", reason)
		}
	}
}

// choosePlanPage applies a resolved plan under the state lock and moves the
// page cursor. Callers hold s.mu and pass the configuration snapshot of the
// request: plan loading and page reads happen before the lock (ADR 0138).
// Rendering happens later in renderPlan. buttonInput marks a wake with button
// input, which keeps the page and the trigger decision so that a trigger
// waits for the next sync (ADR 0161). timed marks a timed refresh, which is
// when a trigger with alert: refresh alerts again (ADR 0131). Button input
// keeps the trigger decision; next and previous page the extended list
// (ADR 0158).
func (s *Server) choosePlanPage(res planResolution, cfg *Config, d *Device, st *deviceState, now time.Time, firstContact, timed bool, buttons []string, buttonInput, paused, quiet bool) planChoice {
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
	base := res.effective("")
	if len(base) == 0 {
		title, text := cfg.texts().noFittingPage(d.Profile)
		return planChoice{title: title, text: shorten(text + " " + joinReasons(res.skipped)),
			err: fmt.Errorf("plan %s has no page for display type %s: %s", d.Plan, d.Profile, joinReasons(res.skipped))}
	}
	mode := p.Mode()
	prev := st.Trigger
	// A trigger decides on every sync without button input. A button sync
	// keeps the last decision; next and previous page the extended list
	// (ADR 0158).
	trigger := res.trigger
	if buttonInput {
		trigger = prev
	}
	if trigger != "" && res.fit[trigger] != "" {
		trigger = ""
	}
	// The action mode is modal: its page and its trigger decision stay
	// (ADR 0162).
	listTrigger := trigger
	if paused {
		listTrigger = prev
	}
	pages := res.effective(listTrigger)
	missing := !slices.Contains(pages, st.PageName)
	// The cursor holds the page, not its index: a plan edit follows the name
	// (ADR 0133), and a page that left the list is resolved by the mode.
	st.PageName = res.resolve(st.PageName, pages, mode, st.PageBeforeTrigger)
	alert := false
	if !paused {
		if trigger != "" && trigger != prev && slices.Contains(base, st.PageName) {
			// The page this trigger displaces, for on_refresh: current.
			st.PageBeforeTrigger = st.PageName
		}
		switch {
		case len(buttons) > 0:
			// Button roles apply in order (ADR 0042): presses made while the
			// panel was painting arrive together and only the final page is
			// rendered. A button also pages away from a trigger page.
			for _, role := range buttons {
				switch role {
				case buttonNext:
					st.PageName = nextPage(pages, st.PageName)
				case buttonPrevious:
					st.PageName = prevPage(pages, st.PageName)
				case buttonHome:
					st.PageName = pages[0]
				}
			}
		case buttonInput:
			// A home press keeps the served page, also while a trigger is
			// active (ADR 0161).
		case trigger != "":
			st.PageName = trigger
		case timed && !firstContact && !missing:
			// Only a timed refresh pages on its own (ADR 0130); a touch or a
			// retried event never does. A page that just left the list was
			// resolved above; advancing again would skip one more page.
			switch mode {
			case plan.RefreshCycle:
				st.PageName = nextPage(pages, st.PageName)
			case plan.RefreshFirst:
				st.PageName = pages[0]
			}
		}
		st.Trigger = trigger
		if trigger == "" && prev != "" {
			// The trigger ended and hid its page again; the cursor was
			// resolved above, so its memory can go.
			st.PageBeforeTrigger = ""
		}
		if !buttonInput {
			switch res.alert {
			case plan.AlertOnce:
				alert = trigger != "" && trigger != prev
			case plan.AlertRefresh:
				alert = trigger != "" && (trigger != prev || timed)
			}
		}
	}
	index := slices.Index(pages, st.PageName)
	if index < 0 {
		index = 0
		st.PageName = pages[0]
	}
	st.Page = index
	floor := cfg.minRefresh(d, st.ReportedPower)
	interval := p.Interval(now)
	if interval == 0 {
		interval = d.refresh()
	}
	interval = max(interval, floor)
	refresh := interval
	// Wake up at the next window edge instead of sleeping through it.
	if until := p.UntilNextChange(now); until > 0 && until < refresh {
		refresh = max(floor, until)
	}
	c := planChoice{page: index, pages: len(pages), name: st.PageName, refresh: refresh, interval: interval,
		alert: alert}
	if st.BatteryLevel == batteryWarn {
		c.banners = append(c.banners, cfg.texts().lowBattery(voltageKey(st.BatteryVoltage)))
	}
	return c
}

// nextPage and prevPage walk the effective list, wrapping at its ends.
func nextPage(pages []string, name string) string {
	if len(pages) == 0 {
		return ""
	}
	i := slices.Index(pages, name)
	if i < 0 {
		return pages[0]
	}
	return pages[(i+1)%len(pages)]
}

func prevPage(pages []string, name string) string {
	if len(pages) == 0 {
		return ""
	}
	i := slices.Index(pages, name)
	if i < 0 {
		return pages[0]
	}
	return pages[(i-1+len(pages))%len(pages)]
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
