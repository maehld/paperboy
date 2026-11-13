package paperboy

import (
	_ "embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

// dashboardTemplate is the operator overview. It is read-only and rendered on
// the server, so the page needs no JavaScript and no build step (ADR 0040).
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

// The logo the pages carry: the lockup in the header and the small cut as the
// icon, both copies of docs/logo/ so the binary needs no extra files (a test
// keeps the copies identical to the design files).
//
//go:embed ui/logo.svg
var uiLogo []byte

//go:embed ui/favicon.svg
var uiFavicon []byte

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
	ID         string
	Status     string // ok, late, never or unregistered
	Type       string // display type: the operator's label for the page layout
	Hardware   string // hardware the device reports; empty until it registered
	Plan       string // plan name, "no plan" when empty
	Rule       string // active plan rule, empty when unknown
	Content    string // page name, "notice", "battery warning" or empty
	ImageURL   string // last served image, else the current page's preview; empty for a placeholder
	Waiting    string // placeholder text when there is neither an image nor text
	Power      string // charge state with the charge or voltage, e.g. "Charging · 50 %" (ADR 0051)
	Battery    string // raw fuel gauge reading for the details, e.g. "3.83 V · 50 % · +568 mA"
	LastCharge string // last finished charging episode, e.g. "2 h ago · 41 → 50 %" (issue #205)
	Width      int
	Height     int
	Text       []string // text displays: the last served lines
	Refresh    string   // "every 10 min", empty when unknown
	SeenAgo    string   // last contact, "never" when unknown
	Address    string   // source address of the last contact, empty when unknown (ADR 0083)
	ServedAgo  string   // age of the last served page
	Firmware   string   // running version, available version and update status
	Menu       string   // open menu phase and selected action (ADR 0057)
	Actions    string   // "denied", empty when the device may use actions
	Problem    string   // why the device cannot sync, empty when it can (ADR 0079)
	Error      string   // last render or plan error
	VirtualURL string   // the plan as a virtual display, empty without a plan (ADR 0091)
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
	Running bool   // a check is running right now
	Error   string // last error, empty when the check succeeded
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
		cards = append(cards, dashboardDevice{
			ID:         d.ID,
			Status:     d.Status,
			Type:       d.Profile,
			Hardware:   d.Hardware,
			Plan:       planText(d.Plan),
			Rule:       d.Rule,
			Content:    contentText(d.deviceState),
			ImageURL:   image,
			Waiting:    waiting,
			Power:      powerText(d.Power, d.deviceState),
			Battery:    batteryText(d.deviceState),
			LastCharge: chargeText(d.deviceState, now),
			Width:      profiles[d.Profile].Width,
			Height:     profiles[d.Profile].Height,
			Text:       d.LastText,
			Refresh:    refreshText(d.LastRefresh),
			SeenAgo:    agoText(d.LastSeen, now),
			Address:    d.ClientIP,
			ServedAgo:  agoText(d.LastPageTime, now),
			Firmware:   firmwareText(d),
			Menu:       menuText(d.deviceState),
			Actions:    actionsText(devices[d.ID]),
			Problem:    d.Problem,
			Error:      d.LastError,
			VirtualURL: virtualURL(d.Plan, d.Profile),
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
	running, _, _, lastErr, _, _ := s.download.view()
	return &dashboardDownloads{Running: running, Error: lastErr}
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
// page then shows through its live preview, anything else as a placeholder.
func imageText(d deviceInfo, text bool) (image, waiting string) {
	switch {
	case text && len(d.LastText) > 0:
		return "", ""
	case d.LastPageURL != "":
		return d.LastPageURL, ""
	case !text && d.LastPageKind == "page" && d.LastPage != "":
		return "/api/preview/" + url.PathEscape(d.Profile) + "/" + url.PathEscape(d.LastPage), ""
	case d.LastPageHash != "" || d.LastPageKind != "":
		return "", "No image yet – waiting for the next sync"
	}
	return "", "Nothing served yet"
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

// refreshText turns a reported refresh in seconds into "every 10 min".
func refreshText(seconds int) string {
	if seconds <= 0 {
		return ""
	}
	if seconds >= 3600 {
		if hours := seconds / 3600; seconds%3600 == 0 {
			return fmt.Sprintf("every %d h", hours)
		}
	}
	return fmt.Sprintf("every %d min", (seconds+30)/60)
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
