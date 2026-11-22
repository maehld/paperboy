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
	Name     string
	ImageURL string   // image displays: the page preview
	Width    int      // preview size in pixels
	Height   int      //
	Text     []string // text displays: the rendered lines
	Skipped  string   // reason the device skips the page for its display type
	Error    string   // page file or render error; the device shows a notice
}

type planViewRule struct {
	Name       string
	When       string
	OnRefresh  string
	Refresh    string
	Alert      string // once or refresh; empty when the rule stays quiet
	Active     bool   // the rule the device shows at the selected time
	Fallback   bool   // the plan's fallback rule
	Conditions []planViewCondition
	Pages      []planViewPage
}

// planViewCondition is one value condition of a rule and the key it answers
// right now (ADR 0127). Plan conditions carry no labels; the view shows the
// key.
type planViewCondition struct {
	Value   string // plugin/value
	Is      string // the accepted keys, comma-separated
	Current string // the key the value answers now; empty when it has none
	Active  bool
}

type planView struct {
	Version    string
	GoVersion  string
	Nav        []navItem // the rail, with the active section (ADR 0150)
	Breadcrumb []crumb   // the path to this page
	Device     string
	Profile    string
	Plan       string
	VirtualURL string // the same plan as a virtual display (ADR 0091)
	At         string // selected time as atLayout
	AtText     string // selected time for reading
	Now        bool   // the selected time is now
	Active     string // active rule, empty when none applies
	ByFallback bool   // the active rule applies as the fallback only
	Next       string // next rule change, empty when the plan has no time windows
	Error      string // plan missing, broken or not configured
	Conditions bool   // a rule uses value conditions (ADR 0127)
	Rules      []planViewRule
}

// handlePlanView previews the plan of one device: its rules, the active one at
// the selected time and every page as the device would draw it. It never
// touches the device state, so the page cursor and the state file stay as
// they are; rendering happens without the state lock.
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
		crumb{Label: "Devices", URL: "/ui"},
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
	// Conditions are live; the selected time only moves the time windows
	// (ADR 0127).
	values := s.evaluatePlanValues(r.Context(), cfg, p, dev.Profile, dev)
	for _, rule := range p.Rules {
		if len(rule.When.Conditions) > 0 {
			view.Conditions = true
			break
		}
	}
	sel, ok := p.Select(at, values)
	if ok {
		view.Active = sel.Rule
		view.ByFallback = true
		for i := range p.Rules {
			if p.Rules[i].Name == sel.Rule && p.Matches(i, at, values) {
				view.ByFallback = false
			}
		}
	}
	if until := p.UntilNextBoundary(at); until > 0 {
		next := at.Add(until)
		view.Next = next.Format("Mon 15:04")
		if nsel, ok := p.Select(next, values); ok {
			view.Next += " → " + nsel.Rule
		}
	}
	text := cfg.Profiles[dev.Profile].text()
	for _, rule := range p.Rules {
		vr := planViewRule{Name: rule.Name, When: whenText(rule.When), OnRefresh: string(rule.Mode()),
			Alert: string(rule.Alert), Active: rule.Name == view.Active, Fallback: rule.Name == p.Fallback}
		for _, cond := range rule.When.Conditions {
			current, has := values[cond.Value]
			active := false
			if has {
				for _, key := range cond.Is {
					if key == current {
						active = true
						break
					}
				}
			}
			vr.Conditions = append(vr.Conditions, planViewCondition{Value: cond.Value,
				Is: strings.Join(cond.Is, ", "), Current: current, Active: active})
		}
		if rule.Refresh > 0 {
			vr.Refresh = intervalText(int(time.Duration(rule.Refresh) / time.Second))
		}
		for _, name := range rule.Pages {
			vr.Pages = append(vr.Pages, s.planViewPage(cfg, dev.ID, dev.Profile, name, text, at))
		}
		view.Rules = append(view.Rules, vr)
	}
	s.writePlanView(w, view)
}

// planViewPage resolves one page of a rule the way the device would: skipped
// when it does not fit the display type, a notice when it is broken. id names
// the device, so a preview shows its status widgets (ADR 0123).
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

// whenText describes a time window, e.g. "07:00–09:00 · mon, tue".
func whenText(w plan.When) string {
	var parts []string
	if w.From != nil && w.To != nil {
		parts = append(parts, clockText(*w.From)+"–"+clockText(*w.To))
	}
	if len(w.Days) > 0 {
		days := make([]string, len(w.Days))
		for i, d := range w.Days {
			days[i] = strings.ToLower(time.Weekday(d).String()[:3])
		}
		parts = append(parts, strings.Join(days, ", "))
	}
	if len(w.Conditions) > 0 {
		parts = append(parts, "if "+conditionsText(w.Conditions))
	}
	if len(parts) == 0 {
		return "always"
	}
	return strings.Join(parts, " · ")
}

// conditionsText writes value conditions for reading: "dexcom/state is low or
// urgent", several conditions joined by "and".
func conditionsText(conds []plan.Condition) string {
	parts := make([]string, 0, len(conds))
	for _, cond := range conds {
		parts = append(parts, cond.Value+" is "+strings.Join(cond.Is, " or "))
	}
	return strings.Join(parts, " and ")
}

func clockText(c plan.Clock) string {
	return fmt.Sprintf("%02d:%02d", int(c)/60, int(c)%60)
}
