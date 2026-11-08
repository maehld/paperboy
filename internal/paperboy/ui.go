package paperboy

import (
	_ "embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"time"
)

// dashboardTemplate is the operator overview. It is read-only and rendered on
// the server, so the page needs no JavaScript and no build step (ADR 0040).
//
//go:embed ui/index.html
var dashboardTemplate string

var dashboardTmpl = template.Must(template.New("dashboard").Parse(dashboardTemplate))

// dashboardDevice is one card of the overview. It carries display-ready texts
// so the template holds no formatting logic.
type dashboardDevice struct {
	ID          string
	Status      string // ok, late, never or unregistered
	Type        string // display type: the operator's label for the page layout
	Hardware    string // hardware the device reports; empty until it registered
	Plan        string // plan name, "no plan" when empty
	Rule        string // active plan rule, empty when unknown
	Content     string // page name, "notice", "battery warning" or empty
	ImageURL    string // last served image, else the current page's preview; empty for a placeholder
	Waiting     string // placeholder text when there is neither an image nor text
	Power       string // effective power source with the battery reading (ADR 0051)
	PowerOrigin string // configured, reported or fallback
	Width       int
	Height      int
	Text        []string // text displays: the last served lines
	Refresh     string   // "every 10 min", empty when unknown
	SeenAgo     string   // last contact, "never" when unknown
	ServedAgo   string   // age of the last served page
	Firmware    string   // running version, available version and update status
	Error       string   // last render or plan error
}

type dashboardPage struct {
	Now            string
	Version        string // build label: version, commit and commit date
	GoVersion      string
	Devices        []dashboardDevice
	FirmwareIssues []string            // ignored files in the firmware directory
	Downloads      *dashboardDownloads // release checks (ADR 0060), nil without a download section
}

// dashboardDownloads is the release card: source, checks and what came from it.
type dashboardDownloads struct {
	Source    string
	Running   bool     // a check is running right now
	LastCheck string   // time of the last check, "never" before the first one
	LastOK    string   // time of the last successful check, empty when unknown
	Error     string   // last error, empty when the check succeeded
	Plugins   []string // "weather v2.1.0"
	Firmware  []string // "sticky 1.0.0"
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
			ID:          d.ID,
			Status:      d.Status,
			Type:        d.Profile,
			Hardware:    d.Hardware,
			Plan:        planText(d.Plan),
			Rule:        d.Rule,
			Content:     contentText(d.deviceState),
			ImageURL:    image,
			Waiting:     waiting,
			Power:       powerSource(d.Power, d.deviceState),
			PowerOrigin: powerOrigin(cfg, devices[d.ID], d.ReportedPower),
			Width:       profiles[d.Profile].Width,
			Height:      profiles[d.Profile].Height,
			Text:        d.LastText,
			Refresh:     refreshText(d.LastRefresh),
			SeenAgo:     agoText(d.LastSeen, now),
			ServedAgo:   agoText(d.LastPageTime, now),
			Firmware:    firmwareText(d),
			Error:       d.LastError,
		})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	loc := cfg.Timezone
	if loc == nil {
		loc = time.UTC
	}
	page := dashboardPage{
		Now:            now.In(loc).Format("15:04:05"),
		Version:        versionText(build, loc),
		GoVersion:      build.Go,
		Devices:        cards,
		FirmwareIssues: s.firmware.invalidNames(),
		Downloads:      s.downloadCard(cfg, loc),
	}
	if err := dashboardTmpl.Execute(w, page); err != nil {
		slog.Error("dashboard rendering failed", "error", err)
	}
}

// handleDownloadCheck starts a release check in the background and returns the
// dashboard at once; a second request while one runs changes nothing (ADR 0062).
// It is the dashboard's only action, so it needs no form token.
func (s *Server) handleDownloadCheck(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	if cfg.Download != nil && s.download.begin() {
		go func() {
			s.finishDownloadCheck(cfg, s.downloadOnce(s.ctx, cfg))
		}()
	}
	http.Redirect(w, r, "/ui", http.StatusSeeOther)
}

// downloadCard summarizes the release checks for the dashboard; nil without a
// download section.
func (s *Server) downloadCard(cfg *Config, loc *time.Location) *dashboardDownloads {
	if cfg.Download == nil {
		return nil
	}
	running, lastCheck, lastOK, lastErr, plugins, firmware := s.download.view()
	card := &dashboardDownloads{Source: cfg.Download.Source, Error: lastErr,
		Running:   running,
		LastCheck: downloadTime(lastCheck, loc), LastOK: downloadTime(lastOK, loc)}
	for _, name := range sortedStringKeys(plugins) {
		card.Plugins = append(card.Plugins, name+" "+plugins[name])
	}
	for _, hw := range sortedStringKeys(firmware) {
		card.Firmware = append(card.Firmware, hw+" "+firmware[hw])
	}
	return card
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

func sortedStringKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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

// powerOrigin names where the effective power source comes from (ADR 0051): set
// on the device, set on the display type, reported by the firmware or the
// battery fallback of auto.
func powerOrigin(cfg *Config, d *Device, reported string) string {
	if d == nil {
		return ""
	}
	fixed := func(v string) bool { return v == powerBattery || v == powerMains }
	switch {
	case fixed(d.Power):
		return "set on device"
	case d.Power == "" && fixed(cfg.Profiles[d.Profile].Power):
		return "set on display type"
	case reported != "":
		return "reported by firmware"
	}
	return "fallback"
}

// powerSource joins the effective power source with the battery reading, e.g.
// "Battery · 3.79 V · 71 %" or just "Mains".
func powerSource(power string, st deviceState) string {
	text := "Battery"
	if power == powerMains {
		text = "Mains"
	}
	if b := batteryText(st); b != "" {
		text += " · " + b
	}
	return text
}

func planText(plan string) string {
	if plan == "" {
		return "no plan"
	}
	return plan
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

// batteryText shows the voltage, the fuel gauge charge if known and, when a
// threshold was crossed, its level.
func batteryText(st deviceState) string {
	if st.BatteryVoltage == "" {
		return ""
	}
	text := st.BatteryVoltage + " V"
	if st.BatteryPercent != nil {
		text += fmt.Sprintf(" · %.0f %%", *st.BatteryPercent)
	}
	switch st.BatteryLevel {
	case batteryWarn:
		return text + " · low"
	case batteryCritical:
		return text + " · critical"
	}
	return text
}
