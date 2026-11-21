package main

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
)

// fixture serves one repository. Every endpoint answers 304 when the request
// carries its ETag.
type fixture struct {
	mu       sync.Mutex
	requests int
	auth     []string
}

func newFixture(t *testing.T, now time.Time) (*httptest.Server, *fixture) {
	t.Helper()
	f := &fixture{}
	commit := commitResponse{SHA: "abcdef1234567890"}
	commit.Commit.Message = "Add the plugin\n\nA longer body."
	commit.Commit.Author.Name = "Carol"
	commit.Commit.Author.Date = now.Add(-5 * time.Minute)
	commit.Author = &user{Login: "carol"}
	handlers := map[string]any{
		"/repos/acme/widget": repoResponse{FullName: "acme/widget", DefaultBranch: "main", OpenIssuesCount: 3},
		"/repos/acme/widget/issues": []issueResponse{
			{Number: 1, Title: "Fix the header", User: &user{Login: "alice"}, Labels: []label{{Name: "bug"}}, CreatedAt: now.Add(-48 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour)},
			{Number: 7, Title: "A pull request in the issue list", User: &user{Login: "bob"}, PullRequest: &struct{}{}},
		},
		"/repos/acme/widget/pulls": []issueResponse{
			{Number: 9, Title: "Add the plugin", User: &user{Login: "carol"}, Draft: true, CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now.Add(-time.Hour)},
		},
		"/repos/acme/widget/actions/runs": runsResponse{WorkflowRuns: []runResponse{
			{ID: 4, WorkflowID: 1, Name: "CI", HeadBranch: "main", Event: "push", Status: "completed", Conclusion: "failure", CreatedAt: now},
			{ID: 3, WorkflowID: 2, Name: "Docs", HeadBranch: "feature", Event: "push", Status: "completed", Conclusion: "failure", CreatedAt: now.Add(-10 * time.Minute)},
			{ID: 2, WorkflowID: 1, Name: "CI", HeadBranch: "main", Event: "push", Status: "completed", Conclusion: "success", CreatedAt: now.Add(-time.Hour)},
			{ID: 1, WorkflowID: 2, Name: "Docs", HeadBranch: "main", Event: "schedule", Status: "completed", Conclusion: "success", CreatedAt: now.Add(-2 * time.Hour)},
		}},
		"/repos/acme/widget/commits": []commitResponse{commit},
	}
	mux := http.NewServeMux()
	for path, body := range handlers {
		path, body := path, body
		etag := `"` + path + `"`
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			f.requests++
			f.auth = append(f.auth, r.Header.Get("Authorization"))
			f.mu.Unlock()
			if r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", etag)
			_ = json.NewEncoder(w).Encode(body)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, f
}

func configured(t *testing.T, api string, now time.Time) *github {
	t.Helper()
	p := newPlugin()
	cfg := plugin.Values{
		"repositories": []any{"acme/widget"},
		"token":        "secret",
		"refresh":      "5m",
		"api":          api,
	}
	if err := p.Configure(cfg, &plugin.Request{Version: plugin.Version}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	changed, failed := p.reload(context.Background())
	_ = changed
	if failed {
		t.Fatalf("reload failed: %v", p.data[0].err)
	}
	return p
}

func TestFetch(t *testing.T) {
	now := time.Now()
	srv, _ := newFixture(t, now)
	p := configured(t, srv.URL, now)

	d := p.data[0]
	if d.openIssues != 2 {
		t.Errorf("open issues = %d, want 2", d.openIssues)
	}
	if d.openPulls != 1 {
		t.Errorf("open pulls = %d, want 1", d.openPulls)
	}
	if d.failing != 1 {
		t.Errorf("failing workflows = %d, want 1", d.failing)
	}
	if len(d.commits) != 1 || d.commits[0].Message != "Add the plugin" {
		t.Errorf("commits = %+v", d.commits)
	}
	if d.commits[0].Author != "carol" {
		t.Errorf("commit author = %q, want carol", d.commits[0].Author)
	}
}

func TestPrepareAndRender(t *testing.T) {
	now := time.Now()
	srv, _ := newFixture(t, now)
	p := configured(t, srv.URL, now)
	req := &plugin.Request{Version: plugin.Version, Now: now, Language: "de"}
	if err := p.Prepare(req); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	for _, name := range []string{"repo", "issues", "pulls", "runs", "commits"} {
		w := plugin.WidgetRequest{
			Widget: name, Width: 400, Height: 160,
			Style: plugin.Style{Padding: 4, Rule: 1, TextPx: 16},
			Args:  map[string]any{"count": 2},
		}
		img := render.NewCanvas(w.Width, w.Height, 4)
		if err := p.Render(req, w, img); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !hasInk(img) {
			t.Errorf("%s: empty image", name)
		}
	}
}

func TestRenderTooSmall(t *testing.T) {
	now := time.Now()
	srv, _ := newFixture(t, now)
	p := configured(t, srv.URL, now)
	req := &plugin.Request{Version: plugin.Version, Now: now}
	if err := p.Prepare(req); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	w := plugin.WidgetRequest{Widget: "repo", Width: 40, Height: 20, Style: plugin.Style{TextPx: 16}}
	if err := p.Render(req, w, render.NewCanvas(w.Width, w.Height, 4)); !errors.Is(err, plugin.ErrTooSmall) {
		t.Errorf("err = %v, want ErrTooSmall", err)
	}
	w.Widget = "unknown"
	if err := p.Render(req, w, render.NewCanvas(w.Width, w.Height, 4)); !errors.Is(err, plugin.ErrUnknownWidget) {
		t.Errorf("err = %v, want ErrUnknownWidget", err)
	}
}

func TestValues(t *testing.T) {
	now := time.Now()
	srv, _ := newFixture(t, now)
	p := configured(t, srv.URL, now)
	req := &plugin.Request{
		Version: plugin.Version, Now: now, Language: "en",
		ValueArgs: plugin.Values{"repo": "widget"},
		Values:    []string{"open-issues", "open-pulls", "failed-runs", "last-commit", "last-commit-at", "last-run", "last-run-at", "state"},
	}
	if err := p.Prepare(req); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	values, err := p.Values(req)
	if err != nil {
		t.Fatalf("values: %v", err)
	}
	for name, want := range map[string]float64{"open-issues": 2, "open-pulls": 1, "failed-runs": 1} {
		v, ok := values[name]
		if !ok || v.Number == nil || *v.Number != want {
			t.Errorf("%s = %+v, want %v", name, v, want)
		}
	}
	if v := values["last-commit"]; v.Text == nil || *v.Text != "abcdef1 Add the plugin" {
		t.Errorf("last-commit = %+v", v)
	}
	if v := values["last-run"]; v.Text == nil || *v.Text != "CI – failed" {
		t.Errorf("last-run = %+v", v)
	}
	if v := values["state"]; v.Enum == nil || *v.Enum != "failing" {
		t.Errorf("state = %+v", v)
	}
	if v := values["last-commit-at"]; v.Timestamp == nil {
		t.Errorf("last-commit-at = %+v", v)
	}
}

func TestETagKeepsData(t *testing.T) {
	now := time.Now()
	srv, f := newFixture(t, now)
	p := configured(t, srv.URL, now)
	first := f.requests
	changed, failed := p.reload(context.Background())
	if changed || failed {
		t.Fatalf("second reload: changed=%v failed=%v", changed, failed)
	}
	if f.requests-first != 5 {
		t.Errorf("requests = %d, want 5", f.requests-first)
	}
	if d := p.data[0]; d.openIssues != 2 || d.openPulls != 1 || d.failing != 1 {
		t.Errorf("counts changed after 304: %+v", d)
	}
	for _, auth := range f.auth {
		if auth != "Bearer secret" {
			t.Fatalf("Authorization = %q", auth)
		}
	}
}

func TestParseRepositories(t *testing.T) {
	repos, err := parseRepositories([]any{"acme/widget", map[string]any{"name": "other", "repo": "acme/other"}})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(repos) != 2 || repos[0].Name != "widget" || repos[1].Name != "other" {
		t.Errorf("repos = %+v", repos)
	}
	for _, raw := range []any{
		[]any{"no-slash"},
		[]any{"acme/widget", "acme/widget"},
		[]any{"acme/widget", map[string]any{"repo": "acme/widget"}},
		[]any{"acme/widget", map[string]any{"repo": "acme/widget", "extra": true}},
	} {
		if _, err := parseRepositories(raw); err == nil {
			t.Errorf("parseRepositories(%v) accepted an invalid list", raw)
		}
	}
}

func TestConfigure(t *testing.T) {
	p := newPlugin()
	valid := plugin.Values{"repositories": []any{"acme/widget"}}
	if err := p.Configure(valid, &plugin.Request{Version: plugin.Version}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if p.refresh != defaultRefresh || p.api != defaultAPI {
		t.Errorf("defaults: refresh=%v api=%q", p.refresh, p.api)
	}
	for name, cfg := range map[string]plugin.Values{
		"short refresh": {"repositories": []any{"acme/widget"}, "refresh": "1m"},
		"bad api":       {"repositories": []any{"acme/widget"}, "api": "ftp://example.com"},
		"unknown key":   {"repositories": []any{"acme/widget"}, "tokn": "x"},
	} {
		if err := p.Configure(cfg, &plugin.Request{Version: plugin.Version}); err == nil {
			t.Errorf("%s: configure accepted it", name)
		}
	}
}

func TestFailedWorkflows(t *testing.T) {
	runs := []run{
		{WorkflowID: 6, Branch: "main", Status: "in_progress"},
		{WorkflowID: 1, Branch: "main", Status: "completed", Conclusion: "failure"},
		{WorkflowID: 1, Branch: "main", Status: "completed", Conclusion: "failure"},
		{WorkflowID: 2, Branch: "main", Status: "completed", Conclusion: "cancelled"},
		{WorkflowID: 3, Branch: "feature", Status: "completed", Conclusion: "failure"},
		{WorkflowID: 4, Branch: "main", Status: "in_progress"},
		{WorkflowID: 6, Branch: "main", Status: "completed", Conclusion: "failure"},
	}
	if n := failedWorkflows(runs, "main"); n != 2 {
		t.Errorf("failedWorkflows = %d, want 2", n)
	}
}

func TestAPIRateLimitError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1700000000")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	}))
	defer srv.Close()
	p := newPlugin()
	cfg := plugin.Values{"repositories": []any{"acme/widget"}, "api": srv.URL}
	if err := p.Configure(cfg, &plugin.Request{Version: plugin.Version}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	p.reload(context.Background())
	if err := p.data[0].err; err == nil || !strings.Contains(err.Error(), "rate limit exhausted") {
		t.Errorf("err = %v", err)
	}
}

func hasInk(img *image.Paletted) bool {
	white := uint8(len(img.Palette) - 1)
	for _, px := range img.Pix {
		if px != white {
			return true
		}
	}
	return false
}
