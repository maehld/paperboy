package main

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
)

// snapshot is what one request draws from. The repository entries are
// immutable once they are published, so the request can keep the pointers.
type snapshot struct {
	repos []*repoData
}

// selectRepo returns one configured repository by name, or all of them.
func (s snapshot) selectRepo(name string) ([]*repoData, error) {
	if name == "" || name == "all" {
		return s.repos, nil
	}
	for _, d := range s.repos {
		if d.spec.Name == name {
			return []*repoData{d}, nil
		}
	}
	return nil, fmt.Errorf("unknown repository %q", name)
}

// Render draws one widget.
func (p *github) Render(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	switch w.Widget {
	case "repo":
		return p.renderRepo(req, w, img)
	case "issues":
		return p.renderIssues(req, w, img)
	case "pulls":
		return p.renderPulls(req, w, img)
	case "runs":
		return p.renderRuns(req, w, img)
	case "commits":
		return p.renderCommits(req, w, img)
	}
	return fmt.Errorf("%w %q", plugin.ErrUnknownWidget, w.Widget)
}

// dataFor resolves the widget's repo arg and fails when no repository has
// anything to draw.
func (p *github) dataFor(w plugin.WidgetRequest) ([]*repoData, error) {
	list, err := p.snap.selectRepo(w.Arg().String("repo", "all"))
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, errors.New("no repositories configured")
	}
	var errs []error
	usable := false
	for _, d := range list {
		if !d.empty() {
			usable = true
			continue
		}
		if d.err != nil {
			errs = append(errs, d.err)
		}
	}
	if !usable && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return list, nil
}

func (p *github) renderRepo(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	list, err := p.dataFor(w)
	if err != nil {
		return err
	}
	inner := img.Rect.Inset(w.Style.Padding)
	base := max(req.TextPx(w), render.MinSize)
	if inner.Dx() < 100 || inner.Dy() < int(base*4) {
		return plugin.TooSmall(w)
	}
	rule := max(w.Style.Rule, 1)
	muted := mutedColor(img)
	issues, issuesCapped, pulls, pullsCapped, failing := aggregate(list)

	titleSize := max(base*0.6, render.MinSize)
	y := inner.Min.Y
	render.Text(img, repoTitle(list, req), inner.Min.X, y+int(titleSize), render.Style{Size: titleSize, Bold: true, Shrink: true}, inner.Dx(), render.Black, render.Left)
	y += int(titleSize * 1.5)
	render.HLine(img, inner.Min.X, y, inner.Max.X, rule)
	y += rule + int(base*0.25)

	footer := 0
	if inner.Max.Y-y >= int(base*3.2) {
		footer = int(base * 1.7)
	}
	labelSize := max(base*0.45, 9)
	room := inner.Max.Y - footer - y - int(labelSize*1.15)
	valueSize := min(base*2.2, float64(room)*0.55)
	valueSize = max(valueSize, base)
	baseline := y + int(valueSize)

	colW := inner.Dx() / 3
	values := []string{countText(issues, issuesCapped), countText(pulls, pullsCapped), strconv.Itoa(failing)}
	labels := []string{req.T("Issues", "Issues"), req.T("PRs", "PRs"), req.T("Fehler", "Failed")}
	for i := range values {
		if colW < 24 {
			break
		}
		x := inner.Min.X + i*colW + colW/2
		render.Text(img, values[i], x, baseline, render.Style{Size: valueSize, Bold: true, Shrink: true}, colW-6, render.Black, render.Center)
		render.Text(img, labels[i], x, baseline+int(labelSize*1.35), render.Style{Size: labelSize, Shrink: true}, colW-4, muted, render.Center)
	}
	if footer > 0 {
		drawRepoFooter(req, img, inner, base, rule, footer, list, muted)
	}
	return nil
}

// drawRepoFooter shows the newest commit of the selected repositories.
func drawRepoFooter(req *plugin.Request, img *image.Paletted, inner image.Rectangle, base float64, rule, height int, list []*repoData, muted color.Color) {
	c, from := newestCommit(list)
	y := inner.Max.Y - height
	render.HLine(img, inner.Min.X, y, inner.Max.X, rule)
	y += rule + int(base*0.2)
	if c.SHA == "" {
		return
	}
	title := c.Message
	if len(list) > 1 && from != nil {
		title = from.spec.Name + ": " + title
	}
	titleSize := max(base*0.5, render.MinSize)
	render.Text(img, render.Ellipsize(title, titleSize, true, inner.Dx()), inner.Min.X, y+int(titleSize), render.Style{Size: titleSize, Bold: true}, inner.Dx(), render.Black, render.Left)
	metaSize := max(base*0.4, 9)
	meta := strings.Join(compact([]string{shortSHA(c.SHA), c.Author, relativeTime(c.Date, now(req), req)}), "  ·  ")
	render.Text(img, render.Ellipsize(meta, metaSize, false, inner.Dx()), inner.Min.X, y+int(titleSize)+int(metaSize*1.2), render.Style{Size: metaSize}, inner.Dx(), muted, render.Left)
}

func repoTitle(list []*repoData, req *plugin.Request) string {
	if len(list) == 1 {
		return list[0].spec.Value()
	}
	return fmt.Sprintf(req.T("%d Repositories", "%d repositories"), len(list))
}

func (p *github) renderIssues(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	list, err := p.dataFor(w)
	if err != nil {
		return err
	}
	labels := w.Arg().Strings("labels")
	type ref struct {
		*repoData
		issue issue
	}
	var refs []ref
	for _, d := range list {
		for _, it := range d.issues {
			if hasLabel(it, labels) {
				refs = append(refs, ref{d, it})
			}
		}
	}
	sort.SliceStable(refs, func(i, j int) bool { return refs[i].issue.Updated.After(refs[j].issue.Updated) })
	items := make([]listItem, 0, len(refs))
	for _, r := range refs {
		items = append(items, listItem{
			Title: fmt.Sprintf("#%d  %s", r.issue.Number, r.issue.Title),
			Meta:  issueMeta(r.repoData, r.issue, req, len(list) > 1),
		})
	}
	return renderList(req, w, img, listHeader(w, list, req), req.T("Keine offenen Issues", "No open issues"), items)
}

func (p *github) renderPulls(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	list, err := p.dataFor(w)
	if err != nil {
		return err
	}
	type ref struct {
		*repoData
		issue issue
	}
	var refs []ref
	for _, d := range list {
		for _, it := range d.pulls {
			refs = append(refs, ref{d, it})
		}
	}
	sort.SliceStable(refs, func(i, j int) bool { return refs[i].issue.Updated.After(refs[j].issue.Updated) })
	items := make([]listItem, 0, len(refs))
	for _, r := range refs {
		items = append(items, listItem{
			Title: fmt.Sprintf("#%d  %s", r.issue.Number, r.issue.Title),
			Meta:  issueMeta(r.repoData, r.issue, req, len(list) > 1),
		})
	}
	return renderList(req, w, img, listHeader(w, list, req), req.T("Keine offenen Pull Requests", "No open pull requests"), items)
}

func (p *github) renderRuns(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	list, err := p.dataFor(w)
	if err != nil {
		return err
	}
	mode := w.Arg().String("branch", "default")
	type ref struct {
		*repoData
		run run
	}
	var refs []ref
	for _, d := range list {
		want := resolveBranch(d, mode)
		for _, r := range d.runs {
			if want != "all" && r.Branch != want {
				continue
			}
			refs = append(refs, ref{d, r})
		}
	}
	sort.SliceStable(refs, func(i, j int) bool { return refs[i].run.Created.After(refs[j].run.Created) })
	items := make([]listItem, 0, len(refs))
	for _, r := range refs {
		meta := compact([]string{repoPrefix(r.repoData, len(list) > 1), r.run.Branch, r.run.Event, relativeTime(r.run.Created, now(req), req)})
		items = append(items, listItem{
			Title:  r.run.Name,
			Meta:   strings.Join(meta, "  ·  "),
			Status: statusText(r.run, req),
			Alarm:  r.run.failed(),
		})
	}
	return renderList(req, w, img, "", req.T("Keine Workflow-Läufe", "No workflow runs"), items)
}

func (p *github) renderCommits(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	list, err := p.dataFor(w)
	if err != nil {
		return err
	}
	type ref struct {
		*repoData
		commit commit
	}
	var refs []ref
	for _, d := range list {
		for _, c := range d.commits {
			refs = append(refs, ref{d, c})
		}
	}
	sort.SliceStable(refs, func(i, j int) bool { return refs[i].commit.Date.After(refs[j].commit.Date) })
	items := make([]listItem, 0, len(refs))
	for _, r := range refs {
		meta := compact([]string{repoPrefix(r.repoData, len(list) > 1), shortSHA(r.commit.SHA), r.commit.Author, relativeTime(r.commit.Date, now(req), req)})
		items = append(items, listItem{
			Title: r.commit.Message,
			Meta:  strings.Join(meta, "  ·  "),
		})
	}
	return renderList(req, w, img, "", req.T("Keine Commits", "No commits"), items)
}

// listItem is one row of a list widget.
type listItem struct {
	Title  string
	Meta   string
	Status string // right-aligned, e.g. the result of a workflow run
	Alarm  bool   // draw the status in bold
}

// renderList draws one title line and one meta line per item. The rows share
// the cell, so a taller cell shows more items.
func renderList(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted, header, empty string, items []listItem) error {
	inner := img.Rect.Inset(w.Style.Padding)
	base := max(req.TextPx(w), render.MinSize)
	if inner.Dx() < 60 || inner.Dy() < int(base*1.5) {
		return plugin.TooSmall(w)
	}
	rule := max(w.Style.Rule, 1)
	muted := mutedColor(img)

	if header != "" {
		size := max(base*0.55, render.MinSize)
		render.Text(img, header, inner.Min.X, inner.Min.Y+int(size), render.Style{Size: size, Bold: true, Shrink: true}, inner.Dx(), render.Black, render.Left)
		inner.Min.Y += int(size * 1.6)
		render.HLine(img, inner.Min.X, inner.Min.Y, inner.Max.X, rule)
		inner.Min.Y += rule + int(base*0.2)
	}
	if len(items) == 0 {
		render.Text(img, empty, inner.Min.X, inner.Min.Y+int(base), render.Style{Size: base, Shrink: true}, inner.Dx(), render.Black, render.Left)
		return nil
	}

	metaSize := max(base*0.45, 9)
	metaStep := int(metaSize * 1.45)
	titleH := int(base * 0.95)
	showMeta := true
	row := titleH + metaStep
	rows := inner.Dy() / row
	if rows < 1 {
		showMeta = false
		row = titleH
		rows = inner.Dy() / row
	}
	if limit := w.Arg().Int("count", 0); limit > 0 {
		rows = min(rows, limit)
	}
	rows = min(rows, len(items))
	if rows < 1 {
		return plugin.TooSmall(w)
	}
	row = inner.Dy() / rows

	y := inner.Min.Y
	for i := 0; i < rows; i++ {
		item := items[i]
		if i > 0 {
			render.HLine(img, inner.Min.X, y, inner.Max.X, rule)
		}
		content := titleH
		if showMeta {
			content += metaStep
		}
		baseline := y + max((row-content)/2, 0) + titleH
		statusW := 0
		if item.Status != "" {
			statusW = render.Measure(item.Status, metaSize, true) + int(base*0.5)
			render.Text(img, item.Status, inner.Max.X, baseline, render.Style{Size: metaSize, Bold: item.Alarm}, statusW, render.Black, render.Right)
		}
		width := inner.Dx() - statusW
		render.Text(img, render.Ellipsize(item.Title, base, true, width), inner.Min.X, baseline, render.Style{Size: base, Bold: true}, width, render.Black, render.Left)
		if showMeta && item.Meta != "" {
			baseline += metaStep
			render.Text(img, render.Ellipsize(item.Meta, metaSize, false, inner.Dx()), inner.Min.X, baseline, render.Style{Size: metaSize}, inner.Dx(), muted, render.Left)
		}
		y += row
	}
	return nil
}

func listHeader(w plugin.WidgetRequest, list []*repoData, req *plugin.Request) string {
	if !w.Arg().Bool("header", false) {
		return ""
	}
	return repoTitle(list, req)
}

// resolveBranch turns the widget's branch arg into the branch to show. An
// unknown default branch falls back to all branches.
func resolveBranch(d *repoData, mode string) string {
	switch mode {
	case "", "default":
		if d.info.DefaultBranch == "" {
			return "all"
		}
		return d.info.DefaultBranch
	case "all":
		return "all"
	}
	return mode
}

func repoPrefix(d *repoData, withRepo bool) string {
	if !withRepo {
		return ""
	}
	return d.spec.Name
}

func hasLabel(it issue, want []string) bool {
	if len(want) == 0 {
		return true
	}
	for _, w := range want {
		for _, have := range it.Labels {
			if strings.EqualFold(strings.TrimSpace(w), have) {
				return true
			}
		}
	}
	return false
}

func issueMeta(d *repoData, it issue, req *plugin.Request, withRepo bool) string {
	parts := []string{repoPrefix(d, withRepo), it.Author}
	if it.Draft {
		parts = append(parts, req.T("Entwurf", "draft"))
	}
	for _, l := range it.Labels {
		if len(parts) >= 4 {
			break
		}
		parts = append(parts, l)
	}
	parts = append(parts, relativeTime(it.Updated, now(req), req))
	return strings.Join(compact(parts), "  ·  ")
}

// statusText names the result of a workflow run.
func statusText(r run, req *plugin.Request) string {
	if r.Status != "completed" {
		switch r.Status {
		case "queued", "requested", "waiting", "pending":
			return req.T("wartet", "queued")
		case "in_progress":
			return req.T("läuft", "running")
		}
		return r.Status
	}
	switch r.Conclusion {
	case "success":
		return req.T("ok", "ok")
	case "failure":
		return req.T("Fehler", "failed")
	case "cancelled":
		return req.T("Abbruch", "cancelled")
	case "timed_out":
		return req.T("Timeout", "timed out")
	case "startup_failure":
		return req.T("Startfehler", "startup failed")
	case "action_required":
		return req.T("Aktion nötig", "action required")
	case "skipped":
		return req.T("übersprungen", "skipped")
	case "neutral":
		return req.T("neutral", "neutral")
	}
	return r.Conclusion
}

// compact drops empty strings from a metadata line.
func compact(parts []string) []string {
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// now is the request's time, or the clock when a caller leaves it empty.
func now(req *plugin.Request) time.Time {
	if req.Now.IsZero() {
		return time.Now()
	}
	return req.Now
}

// relativeTime says how old something is: minutes, hours, "yesterday", a
// weekday within the last week, otherwise the date.
func relativeTime(t, n time.Time, req *plugin.Request) string {
	if t.IsZero() {
		return ""
	}
	loc := req.Location()
	n = n.In(loc)
	d := n.Sub(t.In(loc))
	if d < 0 {
		d = 0
	}
	days := calendarDays(t.In(loc), n)
	switch {
	case d < time.Minute:
		return req.T("gerade eben", "just now")
	case d < time.Hour:
		return fmt.Sprintf(req.T("vor %d Min", "%d min ago"), int(d.Minutes()))
	case days == 0:
		return fmt.Sprintf(req.T("vor %d Std", "%d h ago"), int(d.Hours()))
	case days == 1:
		return req.T("gestern", "yesterday")
	case days < 7:
		return weekday(t.In(loc).Weekday(), req.Lang())
	}
	return dateText(t.In(loc), n, req.Lang())
}

func calendarDays(t, n time.Time) int {
	a := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	b := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, n.Location())
	return int(b.Sub(a).Hours() / 24)
}

func weekday(d time.Weekday, lang string) string {
	names := weekdays[lang]
	if names == nil {
		names = weekdays["en"]
	}
	return names[int(d)]
}

var weekdays = map[string][]string{
	"de": {"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"},
	"en": {"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
}

func dateText(t, n time.Time, lang string) string {
	if lang == "de" {
		if t.Year() == n.Year() {
			return t.Format("02.01.")
		}
		return t.Format("02.01.06")
	}
	if t.Year() == n.Year() {
		return t.Format("Jan 2")
	}
	return t.Format("Jan 2, 2006")
}

// mutedColor keeps secondary text readable on 2-level panels, where a light
// gray would turn white.
func mutedColor(img *image.Paletted) color.Color {
	if render.Levels(img.Palette) >= 4 {
		return render.DarkGray
	}
	return render.Black
}

// countText marks a count that the first page could not confirm.
func countText(n int, capped bool) string {
	if capped {
		return fmt.Sprintf("%d+", n)
	}
	return strconv.Itoa(n)
}

// aggregate adds the counts of the selected repositories.
func aggregate(list []*repoData) (issues int, issuesCapped bool, pulls int, pullsCapped bool, failing int) {
	for _, d := range list {
		issues += d.openIssues
		pulls += d.openPulls
		failing += d.failing
		issuesCapped = issuesCapped || d.issuesCapped
		pullsCapped = pullsCapped || d.pullsCapped
	}
	return
}

// newestCommit returns the newest commit of the selected repositories.
func newestCommit(list []*repoData) (commit, *repoData) {
	var best commit
	var from *repoData
	for _, d := range list {
		if len(d.commits) == 0 {
			continue
		}
		if from == nil || d.commits[0].Date.After(best.Date) {
			best, from = d.commits[0], d
		}
	}
	return best, from
}
