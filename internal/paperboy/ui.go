package paperboy

import (
	_ "embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/maehld/paperboy/internal/reading"
)

// dashboardTemplate is the operator overview. It is read-only and rendered on
// the server; the script it links only re-fetches the page and morphs the
// region that changed (ADR 0040, 0106).
//
//go:embed ui/index.html
var dashboardTemplate string

var dashboardTmpl = template.Must(template.New("dashboard").Parse(dashboardTemplate))

// uiCSS is the stylesheet every operator page links, so the header, the action
// bar and the buttons cannot drift apart between the pages (ADR 0076).
//
//go:embed ui/ui.css
var uiCSS []byte

// handleUICSS serves the shared stylesheet.
func (s *Server) handleUICSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(uiCSS)
}

// uiHtmx is the only third-party code the operator UI ships (ADR 0106). The
// pages that update themselves poll their own URL and let htmx morph only the
// region that changed, so an opened details block, the focus and the loaded
// previews survive a tick. Version, source and licence sit next to the file in
// ui/vendor/.
//
//go:embed ui/vendor/htmx-4.0.1.min.js
var uiHtmx []byte

// handleUIHtmx serves the vendored script the self-updating pages link.
func (s *Server) handleUIHtmx(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(uiHtmx)
}

// The logo the pages carry: the lockup in the header and the small cut as the
// icon, both copies of docs/logo/ so the binary needs no extra files (a test
// keeps the copies identical to the design files).
//
//go:embed ui/logo.svg
var uiLogo []byte

//go:embed ui/favicon.svg
var uiFavicon []byte

// uiFont is the vendored pixel font of the text previews (ADR 0116). The
// licence and the source sit next to the file in ui/fonts/.
//
//go:embed ui/fonts/dotgothic16-latin.woff2
var uiFont []byte

// handleUIFont serves the pixel font of the text previews behind the password.
func (s *Server) handleUIFont(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "font/woff2")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(uiFont)
}

// handleUILogo serves the lockup the header of every page shows.
func (s *Server) handleUILogo(w http.ResponseWriter, r *http.Request) {
	serveSVG(w, uiLogo)
}

// handleUIFavicon serves the icon the pages link.
func (s *Server) handleUIFavicon(w http.ResponseWriter, r *http.Request) {
	serveSVG(w, uiFavicon)
}

func serveSVG(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}

// dashboardDevice is one card of the overview. It carries display-ready texts
// so the template holds no formatting logic.
type dashboardDevice struct {
	ID            string
	Status        string // ok, late, never or unregistered
	Type          string // display type: the operator's label for the page layout
	Hardware      string // hardware the device reports; empty until it registered
	Plan          string // plan name, "no plan" when empty
	Rule          string // active plan rule, empty when unknown
	Content       string // page name, "notice", "battery warning" or empty
	ImageURL      string // last served image, else the current page's preview; empty for a placeholder
	Waiting       string // placeholder text when there is neither an image nor text
	Power         string // charge state with the charge or voltage, e.g. "Charging · 50 %" (ADR 0051)
	Battery       string // raw fuel gauge reading for the details, e.g. "3.83 V · 50 % · +568 mA"
	LastCharge    string // last finished charging episode, e.g. "2 h ago · 41 → 50 %" (issue #205)
	Runtime       string // estimated battery runtime, e.g. "~12 d" or "still collecting" (issue #207, #236)
	RuntimeNote   string // "estimated" or "early estimate", empty while collecting
	RuntimeHint   string // tooltip on the Runtime row: the data behind it
	Readings      string // measured values of the room, e.g. "21.5 °C · 45 %" (ADR 0134)
	ReadingsKeys  string // the catalog names behind them, as the tooltip
	ReadingsSince string // age of the oldest shown value, empty while the last sync brought it
	Width         int
	Height        int
	Text          []string // text displays: the last served lines
	Refresh       string   // "every 10 min · next in 4 min", empty when unknown
	SeenAgo       string   // last contact, "never" when unknown
	Address       string   // source address of the last contact, empty when unknown (ADR 0083)
	ServedAgo     string   // age of the last served page
	Firmware      string   // running version, available version and update status
	Menu          string   // open menu phase and selected action (ADR 0057)
	Actions       string   // "denied", empty when the device may use actions
	Problem       string   // why the device cannot sync, empty when it can (ADR 0079)
	Error         string   // last render or plan error
	VirtualURL    string   // the plan as a virtual display, empty without a plan (ADR 0091)
}

type dashboardPage struct {
	Version   string // build label: version, commit and commit date
	GoVersion string
	Devices   []dashboardDevice
	Downloads *dashboardDownloads // hint about the release checks (ADR 0060), nil without a download section
}

// dashboardDownloads is the hint above the device grid: the details live on the
// downloads page, so a running check no longer reloads every card.
type dashboardDownloads struct {
	Running   bool   // a check is running right now
	Error     string // last error, empty when the check succeeded
	Available string // a newer Paperboy release, "2.14.0 is available" (ADR 0101)
	URL       string // its release page, empty when the source has none
}

// handleRoot sends the bare root to the dashboard so that the instance needs
// no extra path in the browser.
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/ui", http.StatusFound)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	cfg := s.config()
	profiles := cfg.Profiles
	devices := map[string]*Device{}
	for i := range cfg.Devices {
		devices[cfg.Devices[i].ID] = &cfg.Devices[i]
	}
	cards := []dashboardDevice{}
	for _, d := range s.deviceInfos() {
		image, waiting := imageText(d, profiles[d.Profile].text())
		runtimeValue, runtimeNote, runtimeHint := runtimeCard(d.Power, d.deviceState)
		readingsValue, readingsKeys, readingsSince := readingsText(d.deviceState, now)
		cards = append(cards, dashboardDevice{
			ID:            d.ID,
			Status:        d.Status,
			Type:          d.Profile,
			Hardware:      d.Hardware,
			Plan:          planText(d.Plan),
			Rule:          d.Rule,
			Content:       contentText(d.deviceState),
			ImageURL:      image,
			Waiting:       waiting,
			Power:         powerText(d.Power, d.deviceState),
			Battery:       batteryText(d.deviceState),
			LastCharge:    chargeText(d.deviceState, now),
			Runtime:       runtimeValue,
			RuntimeNote:   runtimeNote,
			RuntimeHint:   runtimeHint,
			Readings:      readingsValue,
			ReadingsKeys:  readingsKeys,
			ReadingsSince: readingsSince,
			Width:         profiles[d.Profile].Width,
			Height:        profiles[d.Profile].Height,
			Text:          d.LastText,
			Refresh:       refreshText(d.deviceState, now, profiles[d.Profile].text()),
			SeenAgo:       agoText(d.LastSeen, now),
			Address:       d.ClientIP,
			ServedAgo:     agoText(d.LastPageTime, now),
			Firmware:      firmwareText(d),
			Menu:          menuText(d.deviceState),
			Actions:       actionsText(devices[d.ID]),
			Problem:       d.Problem,
			Error:         d.LastError,
			VirtualURL:    virtualURL(d.Plan, d.Profile),
		})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	loc := cfg.Timezone
	if loc == nil {
		loc = time.UTC
	}
	page := dashboardPage{
		Version:   versionText(build, loc),
		GoVersion: build.Go,
		Devices:   cards,
		Downloads: s.downloadHint(cfg),
	}
	if err := dashboardTmpl.Execute(w, page); err != nil {
		slog.Error("dashboard rendering failed", "error", err)
	}
}

// handleDownloadCheck starts a release check in the background and returns to
// the downloads page at once; a second request while one runs changes nothing
// (ADR 0062). It is the page's only action, so it needs no form token.
func (s *Server) handleDownloadCheck(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	if cfg.Download != nil && s.download.begin() {
		go func() {
			s.finishDownloadCheck(cfg, s.downloadOnce(s.ctx, cfg))
		}()
	}
	http.Redirect(w, r, "/ui/downloads", http.StatusSeeOther)
}

// downloadHint is the dashboard's line about the release checks; nil without a
// download section. Details are on the downloads page.
func (s *Server) downloadHint(cfg *Config) *dashboardDownloads {
	if cfg.Download == nil {
		return nil
	}
	running, _, _, lastErr, _, _, update := s.download.view()
	hint := &dashboardDownloads{Running: running, Error: lastErr}
	if update.Version != "" {
		hint.Available, hint.URL = paperboyUpdateText(update)
	}
	return hint
}

// downloadTime prints a time of the day, with the date when it is not today.
func downloadTime(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return "never"
	}
	now := time.Now().In(loc)
	if t.In(loc).Format("2006-01-02") == now.Format("2006-01-02") {
		return t.In(loc).Format("15:04")
	}
	return t.In(loc).Format("2006-01-02 15:04")
}

// imageText picks what the card shows for an image display. Rendered pages
// live in memory only, so after a restart the last served image is gone; a
// page then shows through its live preview, which names the device so its
// status widgets show this device (ADR 0123). Anything else is a placeholder.
func imageText(d deviceInfo, text bool) (image, waiting string) {
	switch {
	case text && len(d.LastText) > 0:
		return "", ""
	case d.LastPageURL != "":
		return pageImageURL(d.LastPageURL, d.LastPageHash), ""
	case !text && d.LastPageKind == "page" && d.LastPage != "":
		q := url.Values{"device": {d.ID}}
		return "/api/preview/" + url.PathEscape(d.Profile) + "/" + url.PathEscape(d.LastPage) + "?" + q.Encode(), ""
	case d.LastPageHash != "" || d.LastPageKind != "":
		return "", "No image yet – waiting for the next sync"
	}
	return "", "Nothing served yet"
}

// pageImageURL names the served image together with the hash of its pixels. The
// dashboard polls itself, and a browser caches what keeps its URL: only a new
// page changes the hash, so a tick does not download the picture again (ADR
// 0106). Without a hash the route stays as it is.
func pageImageURL(imageURL, hash string) string {
	if imageURL == "" || hash == "" {
		return imageURL
	}
	return imageURL + "?h=" + hash
}

// powerState names what the device does with its battery (ADR 0051). The fuel
// gauge current is the only reading that tells charging from connected, and it
// also catches a device that drains although it reports mains; without a gauge
// the reported power source decides.
func powerState(power string, st deviceState) string {
	if st.BatteryMA != nil {
		switch {
		case *st.BatteryMA > idleMA:
			return "Charging"
		case *st.BatteryMA < -idleMA:
			return "Discharging"
		}
	}
	if power == powerMains {
		return "Connected"
	}
	return "Discharging"
}

// powerText is the card's power row: the state and either the charge or, when
// the hardware has no percentage, the voltage. The raw reading sits in the
// card's details so the row stays quiet.
func powerText(power string, st deviceState) string {
	text := powerState(power, st)
	switch {
	case st.BatteryPercent != nil:
		text += fmt.Sprintf(" · %.0f %%", *st.BatteryPercent)
	case st.BatteryVoltage != "":
		text += " · " + st.BatteryVoltage + " V"
	}
	switch st.BatteryLevel {
	case batteryWarn:
		text += " · low"
	case batteryCritical:
		text += " · critical"
	}
	return text
}

// currentText prints a gauge current with its sign; an idle gauge reads 0 mA
// rather than +0.
func currentText(ma float64) string {
	if ma > -0.5 && ma < 0.5 {
		return "0 mA"
	}
	return fmt.Sprintf("%+.0f mA", ma)
}

func planText(plan string) string {
	if plan == "" {
		return "no plan"
	}
	return plan
}

// menuText names the open menu and its selection for the dashboard.
func menuText(st deviceState) string {
	if !st.Menu.Open {
		return ""
	}
	if a := st.Menu.find(st.Menu.Selected); a != nil {
		return st.Menu.Phase + " · " + a.Title
	}
	return st.Menu.Phase
}

// actionsText marks a device that may not use the device menu (ADR 0057).
func actionsText(d *Device) string {
	if d != nil && d.Actions == actionsDeny {
		return "denied"
	}
	return ""
}

// contentText names what the device is showing in plain words.
func contentText(st deviceState) string {
	switch {
	case st.LastPageKind == "page" && st.LastPage != "":
		return st.LastPage
	case st.LastPageKind == "notice":
		return "notice"
	case st.LastPageKind == "battery":
		return "battery warning"
	}
	return ""
}

// agoText renders the distance to now; the empty time means "never".
func agoText(t, now time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	}
	return fmt.Sprintf("%d d ago", int(d.Hours()/24))
}

// intervalText turns a regular interval in seconds into "every 10 min".
func intervalText(seconds int) string {
	if seconds <= 0 {
		return ""
	}
	return "every " + waitText(time.Duration(seconds)*time.Second)
}

// waitText renders a positive duration with its two largest units, e.g. "40 s",
// "4 min" or "1 h 5 min". It rounds to the second below a minute and to the
// minute above it, so a dashboard readout does not overflow.
func waitText(d time.Duration) string {
	if d < time.Minute {
		seconds := int(d.Round(time.Second) / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		return fmt.Sprintf("%d s", seconds)
	}
	d = d.Round(time.Minute)
	if d < time.Hour {
		return fmt.Sprintf("%d min", int(d/time.Minute))
	}
	if d%time.Hour == 0 {
		return fmt.Sprintf("%d h", int(d/time.Hour))
	}
	return fmt.Sprintf("%d h %d min", int(d/time.Hour), int(d/time.Minute)%60)
}

// refreshText shows the regular interval of a device and the time to its next
// sync. The reported refresh is the sleep until that sync, not the interval
// (ADR 0129), so it changes from cycle to cycle and must not read as "every".
// A text display polls instead of sleeping, so it names only its poll.
func refreshText(st deviceState, now time.Time, polling bool) string {
	if polling {
		if st.LastRefresh > 0 {
			return "polling " + intervalText(st.LastRefresh)
		}
		return ""
	}
	interval := intervalText(st.LastInterval)
	next := ""
	if st.LastRefresh > 0 && !st.LastPageTime.IsZero() {
		left := st.LastPageTime.Add(time.Duration(st.LastRefresh) * time.Second).Sub(now)
		if left <= 0 {
			next = "due now"
		} else {
			next = "next in " + waitText(left)
		}
	}
	switch {
	case interval != "" && next != "":
		return interval + " · " + next
	case interval != "":
		return interval
	}
	return next
}

// firmwareText shows the running version and, if another image is available,
// the target and the update status (ADR 0043).
func firmwareText(d deviceInfo) string {
	running := d.Firmware
	if running == "" {
		running = "unknown"
	}
	switch d.FirmwareStatus {
	case fwNone:
		if d.Firmware == "" {
			return ""
		}
		return running
	case fwCurrent:
		return running + " · current"
	case fwFailed:
		return fmt.Sprintf("%s → %s · failed %d times", running, d.FirmwareAvailable, d.FirmwareFailures)
	}
	return running + " → " + d.FirmwareAvailable + " · " + d.FirmwareStatus
}

// batteryText is the raw fuel gauge reading for the card's details: voltage,
// charge and current.
func batteryText(st deviceState) string {
	if st.BatteryVoltage == "" {
		return ""
	}
	text := st.BatteryVoltage + " V"
	if st.BatteryPercent != nil {
		text += fmt.Sprintf(" · %.0f %%", *st.BatteryPercent)
	}
	if st.BatteryMA != nil {
		text += " · " + currentText(*st.BatteryMA)
	}
	return text
}

// readingsText writes the measured values of a device for the card's details:
// "21.5 °C · 45 %", the catalog names behind them for the tooltip and, when the
// last sync brought no value, the age of the oldest one (ADR 0134). The catalog
// fixes the order, so the row reads the same on every device.
func readingsText(st deviceState, now time.Time) (text, keys, since string) {
	var parts, names []string
	var oldest time.Time
	for _, spec := range reading.Catalog {
		r, ok := st.Readings[spec.Name]
		if !ok {
			continue
		}
		parts = append(parts, spec.Format(r.Value))
		names = append(names, spec.Name)
		if oldest.IsZero() || r.At.Before(oldest) {
			oldest = r.At
		}
	}
	if len(parts) == 0 {
		return "", "", ""
	}
	text, keys = strings.Join(parts, " · "), strings.Join(names, ", ")
	// A reading older than the last contact came from an earlier sync: the value
	// still stands, but the operator has to see that it is stale.
	if !oldest.IsZero() && oldest.Before(st.LastSeen) {
		since = "since " + agoText(oldest, now)
	}
	return text, keys, since
}

// runtimeCard builds the Runtime row of a card: the value, the muted note next
// to it and the tooltip that names the data behind it (issue #207, #236). The
// row stays hidden on mains and on hardware without a battery reading, and it
// says "still collecting" until the series shows a measurable discharge.
func runtimeCard(power string, st deviceState) (value, note, hint string) {
	if power == powerMains || st.BatteryVoltage == "" {
		return "", "", ""
	}
	e := estimateBattery(st)
	switch {
	case e.Samples < 2:
		return "still collecting", "", "waiting for the second sample"
	case e.Runtime > 0:
		value = runtimeText(e.Runtime)
		if e.Span >= settledAfter {
			note = "estimated"
		} else {
			note = "early estimate"
		}
		return value, note, dataSpanText(e.Span) + " of data"
	case e.Wait > 0:
		return "still collecting", "", dataSpanText(e.Span) + " of data · estimate in " + runtimeText(e.Wait)
	}
	return "still collecting", "", dataSpanText(e.Span) + " of data · no discharge yet"
}

// chargeText is the details line for the last finished charging episode: when
// it ended and what it did (issue #205). Only what the hardware reported is
// shown, so a gauge without a percentage shows the voltage instead.
func chargeText(st deviceState, now time.Time) string {
	episode := st.LastCharge
	if episode == nil || episode.End.IsZero() {
		return ""
	}
	text := agoText(episode.End, now)
	if from, to := episode.From, episode.To; from != nil && to != nil {
		switch {
		case from.Percent != nil && to.Percent != nil:
			text += fmt.Sprintf(" · %.0f → %.0f %%", *from.Percent, *to.Percent)
		case from.Volts != "" && to.Volts != "":
			text += " · " + from.Volts + " → " + to.Volts + " V"
		}
	}
	return text
}
