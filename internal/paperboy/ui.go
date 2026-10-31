package paperboy

import (
	_ "embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
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
	ID        string
	Status    string // ok, late, never or unregistered
	Plan      string // plan name, "no plan" when empty
	Rule      string // active plan rule, empty when unknown
	Content   string // page name, "notice", "battery warning" or empty
	ImageURL  string // last served image, empty before the first request
	Width     int
	Height    int
	Refresh   string // "every 10 min", empty when unknown
	SeenAgo   string // last contact, "never" when unknown
	ServedAgo string // age of the last served page
	Battery   string // voltage and warning level, empty when unknown
	Firmware  string // running version, available version and update status
	Error     string // last render or plan error
}

type dashboardPage struct {
	Now            string
	Version        string // build label: version, commit and commit date
	GoVersion      string
	Devices        []dashboardDevice
	FirmwareIssues []string // ignored files in the firmware directory
}

// handleRoot sends the bare root to the dashboard so that the instance needs
// no extra path in the browser.
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/ui", http.StatusFound)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	profiles := s.cfg.Profiles
	cards := []dashboardDevice{}
	for _, d := range s.deviceInfos() {
		cards = append(cards, dashboardDevice{
			ID:        d.ID,
			Status:    d.Status,
			Plan:      planText(d.Plan),
			Rule:      d.Rule,
			Content:   contentText(d.deviceState),
			ImageURL:  d.LastPageURL,
			Width:     profiles[d.Profile].Width,
			Height:    profiles[d.Profile].Height,
			Refresh:   refreshText(d.LastRefresh),
			SeenAgo:   agoText(d.LastSeen, now),
			ServedAgo: agoText(d.LastPageTime, now),
			Battery:   batteryText(d.deviceState),
			Firmware:  firmwareText(d),
			Error:     d.LastError,
		})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	loc := s.cfg.Timezone
	if loc == nil {
		loc = time.UTC
	}
	page := dashboardPage{
		Now:            now.In(loc).Format("15:04:05"),
		Version:        versionText(build, loc),
		GoVersion:      build.Go,
		Devices:        cards,
		FirmwareIssues: s.firmware.invalidNames(),
	}
	if err := dashboardTmpl.Execute(w, page); err != nil {
		slog.Error("dashboard rendering failed", "error", err)
	}
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
