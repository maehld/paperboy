package paperboy

import (
	_ "embed"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/maehld/paperboy/server/internal/plan"
)

// planViewTemplate shows a device's plan with a preview of every page for the
// device's display type (ADR 0054). Like the dashboard it is read-only and
// rendered on the server.
//
//go:embed ui/plan.html
var planViewTemplate string

var planViewTmpl = pageTemplate("plan", planViewTemplate, nil)

// atLayout is the format of the time selection, as sent by a datetime-local input.
const atLayout = "2006-01-02T15:04"

type planViewPage struct {
	Name      string
	Hidden    bool
	ImageURL  string   // image displays: the page preview
	Width     int      // preview size in pixels
	Height    int      //
	Text      []string // text displays: the rendered lines
	Skipped   string   // reason the device skips the page for its display type
	Error     string   // page file or render error; the device shows a notice
	Triggered bool     // a trigger shows this page now
}

// planViewTrigger is one trigger of the plan and whether it matches the values
// the plugins answer now (ADR 0127). Triggers carry no labels, so the view
// names the key.
type planViewTrigger struct {
	Number     int
	Page       string
	Alert      string // once or refresh; empty when the trigger stays quiet
	Active     bool   // the trigger the device follows now
	Matches    bool   // every condition answers one of its keys
	Conditions []planViewCondition
	PageNote   string // the page does not fit the display type
}

type planViewCondition struct {
	Kind    string // value, time or dates
	Value   string // value: plugin/value
	Is      string // value: the accepted keys, comma-separated
	Current string // value: the key it answers now; empty when it has none
	Time    string // time: the recurring window
	Dates   string // dates: the calendar days
	Active  bool
}

type planView struct {
	Version    string
	GoVersion  string
	Nav        []navItem // the rail, with the active section (ADR 0152)
	Breadcrumb []crumb   // the path to this page
	FileHint   string    // the file path in the title line
	Device     string
	Profile    string
	Plan       string
	VirtualURL string // the same plan as a virtual display (ADR 0091)
	At         string // selected time as atLayout
	AtText     string // selected time for reading
	Now        bool   // the selected time is now
	Interval   string // refresh interval at the selected time
	Next       string // next window edge and the interval behind it
	Trigger    string // page the device would show because of a trigger
	Triggered  bool
	Error      string // plan missing, broken or not configured
	Pages      []planViewPage
	Triggers   []planViewTrigger
}

// handlePlanView previews the plan of one device: its pages, the effective
// refresh interval at the selected time and every trigger with its current
// value. It never touches the device state, so the page cursor and the state
// file stay as they are; rendering happens without the state lock.
func (s *Server) handlePlanView(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	id := r.URL.Query().Get("device")
	var dev *Device
	for i := range cfg.Devices {
		if cfg.Devices[i].ID == id {
			dev = &cfg.Devices[i]
			break
		}
	}
	if dev == nil {
		http.Error(w, fmt.Sprintf("unknown device %q", id), http.StatusNotFound)
		return
	}
	loc := cfg.Timezone
	if loc == nil {
		loc = time.Local
	}
	now := s.now().In(loc)
	at, isNow := now, true
	if v := r.URL.Query().Get("at"); v != "" {
		t, err := time.ParseInLocation(atLayout, v, loc)
		if err != nil {
			http.Error(w, fmt.Sprintf("at: expected %s, got %q", atLayout, v), http.StatusBadRequest)
			return
		}
		at, isNow = t, false
	}
	view := planView{Device: dev.ID, Profile: dev.Profile, Plan: dev.Plan, At: at.Format(atLayout),
		AtText: at.Format("Mon 2006-01-02 15:04"), Now: isNow}
	view.Nav = navFor("Devices")
	view.Breadcrumb = crumbPath(
		crumb{Label: "Devices", URL: "/ui/devices"},
		crumb{Label: dev.ID, URL: objectURL("/ui/devices", dev.ID, nil)},
		crumb{Label: "Plan"},
	)
	view.VirtualURL = virtualURL(dev.Plan, dev.Profile)
	view.Version, view.GoVersion = editorVersion(cfg)
	if dev.Problem != "" {
		view.Error = "The device cannot sync: " + dev.Problem + "."
		s.writePlanView(w, view)
		return
	}
	if dev.Plan == "" {
		view.Error = "This device has no plan; it shows a notice page."
		s.writePlanView(w, view)
		return
	}
	p, err := s.plans.get(cfg.Plans, dev.Plan, cfg.Timezone)
	if err != nil {
		if p == nil {
			view.Error = shortError(err)
			s.writePlanView(w, view)
			return
		}
		view.Error = "Plan invalid, the last valid version is shown: " + shortError(err)
	}
	// Trigger values are live; the selected time only reads the refresh
	// windows (ADR 0158).
	values := s.evaluatePlanValues(r.Context(), cfg, p, dev.Profile, dev)
	res := s.resolvePlan(cfg, dev, at, values)
	view.Trigger, view.Triggered = res.trigger, res.trigger != ""
	view.Interval = intervalText(int(atInterval(dev, p, at) / time.Second))
	if until := p.UntilNextChange(at); until > 0 {
		next := at.Add(until)
		view.Next = next.Format("Mon 15:04")
		var parts []string
		if d := p.Interval(next); d > 0 && d != p.Interval(at) {
			parts = append(parts, intervalText(int(d/time.Second)))
		}
		for _, tr := range p.Matching(next, values) {
			if tr.Page != res.trigger {
				parts = append(parts, tr.Page)
				break
			}
		}
		if len(parts) > 0 {
			view.Next += " → " + strings.Join(parts, " · ")
		}
	}
	text := cfg.Profiles[dev.Profile].text()
	for i, tr := range p.Triggers {
		vt := planViewTrigger{Number: i + 1, Page: tr.Page, Alert: string(tr.Alert),
			Active: tr.Page == res.trigger, PageNote: res.fit[tr.Page]}
		for _, m := range p.Matching(at, values) {
			if m.Page == tr.Page {
				vt.Matches = true
			}
		}
		for _, cond := range tr.When {
			vc := planViewCondition{Kind: string(cond.Kind()), Active: cond.Matches(at, values)}
			switch cond.Kind() {
			case plan.ConditionValue:
				vc.Value, vc.Is, vc.Current = cond.Value, strings.Join(cond.Is, ", "), values[cond.Value]
				if cond.Value == plan.BuiltinWeekday {
					vc.Current = plan.WeekdayKey(at)
				}
			case plan.ConditionTime:
				vc.Time = windowText(cond)
			case plan.ConditionDates:
				vc.Dates = datesText(cond)
			}
			vt.Conditions = append(vt.Conditions, vc)
		}
		view.Triggers = append(view.Triggers, vt)
	}
	for _, pg := range p.Pages {
		vp := s.planViewPage(cfg, dev.ID, dev.Profile, pg.Name, text, at)
		vp.Hidden = pg.Hidden
		vp.Triggered = pg.Name == res.trigger
		view.Pages = append(view.Pages, vp)
	}
	s.writePlanView(w, view)
}

// atInterval is the refresh interval the plan yields at a moment; 0 means the
// device's own refresh, which the view names as well.
func atInterval(d *Device, p *plan.Plan, at time.Time) time.Duration {
	if interval := p.Interval(at); interval > 0 {
		return interval
	}
	return d.refresh()
}

// planViewPage resolves one page the way the device would: skipped when it does
// not fit the display type, a notice when it is broken. id names the device, so
// a preview shows its status widgets (ADR 0123).
func (s *Server) planViewPage(cfg *Config, id, profile, name string, text bool, at time.Time) planViewPage {
	vp := planViewPage{Name: name}
	if err := s.fits(cfg, profile, name); err != nil {
		vp.Skipped = err.Error()
		return vp
	}
	if _, err := s.pages.get(cfg.Pages, name, profile); err != nil {
		vp.Error = shortError(err)
		return vp
	}
	if text {
		text, err := s.renderText(cfg, profile, name, at, pagePosition{index: 0, count: 1}, id, nil)
		if err != nil {
			vp.Error = shortError(err)
			return vp
		}
		vp.Text = text.Lines
		return vp
	}
	p := cfg.Profiles[profile]
	q := url.Values{"at": {at.Format(atLayout)}, "device": {id}}
	vp.ImageURL = "/api/preview/" + url.PathEscape(profile) + "/" + url.PathEscape(name) + "?" + q.Encode()
	vp.Width, vp.Height = p.Width, p.Height
	return vp
}

func (s *Server) writePlanView(w http.ResponseWriter, view planView) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := planViewTmpl.Execute(w, view); err != nil {
		slog.Error("plan view rendering failed", "error", err)
	}
}

// windowText writes a window of the day for reading: "07:00–09:00".
func windowText(c plan.Condition) string {
	if c.From == nil || c.To == nil {
		return ""
	}
	return clockText(*c.From) + "–" + clockText(*c.To)
}

// datesText writes a range of days for reading: "2026-12-24" or
// "2026-12-24 – 2026-12-26".
func datesText(c plan.Condition) string {
	if c.DateFrom == nil {
		return ""
	}
	if c.DateTo == nil {
		return string(*c.DateFrom)
	}
	return string(*c.DateFrom) + " – " + string(*c.DateTo)
}

func clockText(c plan.Clock) string {
	return fmt.Sprintf("%02d:%02d", int(c)/60, int(c)%60)
}
