// Command github is a Paperboy plugin (ADR 0049, 0144) that shows the state of
// GitHub repositories: open issues and pull requests, failed workflow runs and
// the commit history. It reads the GitHub REST API in the background and keeps
// the last answer of every endpoint, so a page never waits for the network.
//
//	plugins:
//	  github:
//	    config:
//	      token: ${GITHUB_TOKEN}        # optional, raises the rate limit
//	      repositories: [maehld/paperboy]
//
//	pages/github.yaml:
//	  cells:
//	    - { at: [0, 0], span: [3, 3], widget: github/issues, args: { count: 8 } }
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/pkg/plugin"
)

const (
	defaultRefresh = 15 * time.Minute
	minRefresh     = 5 * time.Minute
	requestTimeout = 20 * time.Second
	defaultAPI     = "https://api.github.com"
	retryOnErrors  = 2 * time.Minute
	// issuesPerPage bounds the open issues in memory and the pulls that the
	// counts derive from. A larger list shows as "<first page>+".
	issuesPerPage = 100
	pullsPerPage  = 100
	// runHistory covers the latest run of every workflow on the default
	// branch, which is what the failing count needs.
	runHistory    = 50
	commitHistory = 30
	userAgent     = "paperboy-github (+https://github.com/maehld/paperboy)"
)

func main() {
	plugin.Serve(newPlugin())
}

func newPlugin() *github {
	return &github{client: &http.Client{Timeout: requestTimeout}}
}

// github is the plugin.
type github struct {
	plugin.Demand

	client  *http.Client
	api     string
	token   string
	refresh time.Duration
	repos   []repoSpec

	mu    sync.Mutex
	data  []*repoData // one per configured repository, in the same order
	ready chan struct{}
	snap  snapshot
}

// repoSpec is one configured repository.
type repoSpec struct {
	Name  string // the name the widgets select
	Owner string
	Repo  string
}

// Value is the owner/repo pair.
func (s repoSpec) Value() string { return s.Owner + "/" + s.Repo }

var configKeys = map[string]plugin.Arg{
	"repositories": {
		Type: "list", Required: true,
		Description: "repositories as owner/repo or {name, repo} entries; the name is what the widgets select",
	},
	"token": {
		Type:        "string",
		Description: "GitHub token; raises the rate limit and reads private repositories",
	},
	"refresh": {
		Type: "duration", Default: "15m",
		Description: "how often every repository is fetched, at least 5m",
	},
	"api": {
		Type: "string", Default: defaultAPI,
		Description: "API base URL, for GitHub Enterprise",
	},
}

// valueArgs are the arguments the values read (ADR 0120).
func (p *github) valueArgs() map[string]plugin.Arg {
	names := []any{"all"}
	for _, r := range p.repos {
		names = append(names, r.Name)
	}
	return map[string]plugin.Arg{
		"repo": {
			Type: "string", Default: "all", Enum: names,
			Description: "one configured repository by name, or all",
		},
	}
}

func (p *github) Describe() plugin.Description {
	valueArgs := p.valueArgs()
	image := []string{plugin.KindImage}
	countArg := plugin.Arg{Type: "int", Default: 0, Description: "most entries to show; 0 fills the cell"}
	branchArg := plugin.Arg{
		Type: "string", Default: "default",
		Description: "the repository's default branch, all branches, or a branch name",
	}
	issueArgs := p.valueArgs()
	issueArgs["count"] = countArg
	issueArgs["header"] = plugin.Arg{Type: "bool", Default: false, Description: "the repository's name above the list"}
	issueArgs["labels"] = plugin.Arg{Type: "list", Description: "only issues with one of these labels"}
	pullArgs := p.valueArgs()
	pullArgs["count"] = countArg
	pullArgs["header"] = plugin.Arg{Type: "bool", Default: false, Description: "the repository's name above the list"}
	runArgs := p.valueArgs()
	runArgs["count"] = countArg
	runArgs["branch"] = branchArg
	commitArgs := p.valueArgs()
	commitArgs["count"] = countArg
	return plugin.Description{
		Title:   "GitHub repositories",
		Version: version(),
		Config:  configKeys,
		Values: []plugin.ValueSpec{
			{Name: "open-issues", Title: "Open issues", Type: plugin.ValueNumber, Args: valueArgs},
			{Name: "open-pulls", Title: "Open pull requests", Type: plugin.ValueNumber, Args: valueArgs},
			{Name: "failed-runs", Title: "Failed workflow runs", Type: plugin.ValueNumber, Args: valueArgs},
			{Name: "last-commit", Title: "Last commit", Type: plugin.ValueText, Args: valueArgs},
			{Name: "last-commit-at", Title: "Last commit at", Type: plugin.ValueTimestamp, Args: valueArgs},
			{Name: "last-run", Title: "Last workflow run", Type: plugin.ValueText, Args: valueArgs},
			{Name: "last-run-at", Title: "Last workflow run at", Type: plugin.ValueTimestamp, Args: valueArgs},
			{Name: "state", Title: "Repository state", Type: plugin.ValueEnum, Enum: []string{"ok", "failing"}, Args: valueArgs},
		},
		Widgets: []plugin.WidgetInfo{
			{
				Widget: "repo", Title: "Counts and the last commit", Kinds: image,
				MinWidth: 140, MinHeight: 60,
				Sizes: []plugin.Span{{Columns: 3, Rows: 2}, {Columns: 3, Rows: 3}, {Columns: 6, Rows: 2}},
				Args:  p.valueArgs(),
			},
			{
				Widget: "issues", Title: "Open issues", Kinds: image,
				MinWidth: 160, MinHeight: 50,
				Sizes: []plugin.Span{{Columns: 3, Rows: 3}, {Columns: 3, Rows: 4}, {Columns: 3, Rows: 6}, {Columns: 6, Rows: 2}, {Columns: 6, Rows: 3}, {Columns: 6, Rows: 4}, {Columns: 6, Rows: 6}},
				Args:  issueArgs,
			},
			{
				Widget: "pulls", Title: "Open pull requests", Kinds: image,
				MinWidth: 160, MinHeight: 50,
				Sizes: []plugin.Span{{Columns: 3, Rows: 3}, {Columns: 3, Rows: 4}, {Columns: 3, Rows: 6}, {Columns: 6, Rows: 2}, {Columns: 6, Rows: 3}, {Columns: 6, Rows: 4}, {Columns: 6, Rows: 6}},
				Args:  pullArgs,
			},
			{
				Widget: "runs", Title: "Workflow runs", Kinds: image,
				MinWidth: 160, MinHeight: 50,
				Sizes: []plugin.Span{{Columns: 3, Rows: 3}, {Columns: 3, Rows: 4}, {Columns: 3, Rows: 6}, {Columns: 6, Rows: 2}, {Columns: 6, Rows: 3}, {Columns: 6, Rows: 4}, {Columns: 6, Rows: 6}},
				Args:  runArgs,
			},
			{
				Widget: "commits", Title: "Commit history", Kinds: image,
				MinWidth: 160, MinHeight: 50,
				Sizes: []plugin.Span{{Columns: 3, Rows: 3}, {Columns: 3, Rows: 4}, {Columns: 3, Rows: 6}, {Columns: 6, Rows: 2}, {Columns: 6, Rows: 3}, {Columns: 6, Rows: 4}, {Columns: 6, Rows: 6}},
				Args:  commitArgs,
			},
		},
	}
}

func (p *github) Configure(cfg plugin.Values, req *plugin.Request) error {
	if req.Version != 0 && req.Version < 9 {
		return fmt.Errorf("github needs Paperboy protocol 9 or newer; this Paperboy reports %d", req.Version)
	}
	if err := cfg.Check(configKeys); err != nil {
		return err
	}
	refresh, err := cfg.Duration("refresh", defaultRefresh)
	if err != nil {
		return err
	}
	if refresh < minRefresh {
		return fmt.Errorf("refresh must be at least %s", minRefresh)
	}
	api := strings.TrimRight(strings.TrimSpace(cfg.String("api", defaultAPI)), "/")
	u, err := url.Parse(api)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("api: %q is not an http(s) URL", api)
	}
	repos, err := parseRepositories(cfg["repositories"])
	if err != nil {
		return err
	}
	p.api, p.token, p.refresh, p.repos = api, cfg.String("token", ""), refresh, repos
	return nil
}

func parseRepositories(raw any) ([]repoSpec, error) {
	list, ok := raw.([]any)
	if !ok || len(list) == 0 {
		return nil, errors.New("repositories: expected a list of owner/repo entries")
	}
	var out []repoSpec
	seen := map[string]bool{}
	for i, item := range list {
		var spec repoSpec
		switch v := item.(type) {
		case string:
			owner, repo, err := splitSlug(v)
			if err != nil {
				return nil, fmt.Errorf("repositories[%d]: %w", i, err)
			}
			spec = repoSpec{Owner: owner, Repo: repo}
		case map[string]any:
			unknown := ""
			for key := range v {
				if key != "name" && key != "repo" {
					unknown = key
					break
				}
			}
			if unknown != "" {
				return nil, fmt.Errorf("repositories[%d]: unknown key %q", i, unknown)
			}
			name, _ := v["name"].(string)
			slug, _ := v["repo"].(string)
			owner, repo, err := splitSlug(slug)
			if err != nil {
				return nil, fmt.Errorf("repositories[%d]: %w", i, err)
			}
			spec = repoSpec{Name: strings.TrimSpace(name), Owner: owner, Repo: repo}
		default:
			return nil, fmt.Errorf("repositories[%d]: expected an owner/repo entry", i)
		}
		if spec.Name == "" {
			spec.Name = spec.Repo
		}
		if seen[spec.Name] {
			return nil, fmt.Errorf("repositories[%d]: the name %q is used twice", i, spec.Name)
		}
		seen[spec.Name] = true
		out = append(out, spec)
	}
	return out, nil
}

// splitSlug parses "owner/repo".
func splitSlug(s string) (string, string, error) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || !validName(parts[0]) || !validName(parts[1]) {
		return "", "", fmt.Errorf("%q is not an owner/repo pair", s)
	}
	return parts[0], parts[1], nil
}

func validName(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

// Start fetches every repository now and then after refresh (sooner after
// errors) and reports a change when a shown number, title or run moves.
func (p *github) Start(ctx context.Context, notify func(widgets ...string)) {
	p.mu.Lock()
	p.ready = make(chan struct{})
	ready := p.ready
	p.mu.Unlock()
	go func() {
		first := true
		var next time.Time
		for {
			if !next.IsZero() {
				if !p.Wait(ctx, 0) {
					return
				}
				if wait := time.Until(next); wait > 0 {
					select {
					case <-ctx.Done():
						return
					case <-time.After(wait):
					}
					continue
				}
			}
			changed, failed := p.reload(ctx)
			if changed && !first {
				notify()
			}
			if first {
				close(ready)
				first = false
			}
			wait := p.refresh
			if failed {
				wait = min(wait, retryOnErrors)
			}
			next = time.Now().Add(wait)
		}
	}()
}

// reload fetches all repositories in parallel and reports whether what the
// widgets show changed and whether one of the requests failed.
func (p *github) reload(ctx context.Context) (changed, failed bool) {
	p.mu.Lock()
	old := p.signature()
	prev := p.data
	p.mu.Unlock()

	next := make([]*repoData, len(p.repos))
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i, spec := range p.repos {
		var before *repoData
		if prev != nil && i < len(prev) {
			before = prev[i]
		}
		wg.Add(1)
		go func(i int, spec repoSpec, before *repoData) {
			defer wg.Done()
			fd := p.fetchRepo(ctx, spec, before)
			next[i] = fd
			if fd.err != nil {
				mu.Lock()
				failed = true
				mu.Unlock()
				fmt.Fprintf(os.Stderr, "github: %s: %v\n", spec.Name, fd.err)
			}
		}(i, spec, before)
	}
	wg.Wait()

	p.mu.Lock()
	p.data = next
	changed = old != p.signature()
	p.mu.Unlock()
	return changed, failed
}

// signature identifies what the widgets would show: the counts, the failing
// workflows and the newest entries of every repository.
func (p *github) signature() string {
	var b strings.Builder
	for _, d := range p.data {
		fmt.Fprintf(&b, "%s:%d/%d/%d/%d:%v|", d.spec.Name, d.openIssues, d.openPulls, d.failing, len(d.commits), d.err != nil)
		if len(d.issues) > 0 {
			fmt.Fprintf(&b, "%d:%s|", d.issues[0].Number, d.issues[0].Title)
		}
		if len(d.pulls) > 0 {
			fmt.Fprintf(&b, "%d:%s|", d.pulls[0].Number, d.pulls[0].Title)
		}
		if len(d.runs) > 0 {
			fmt.Fprintf(&b, "%d:%s|", d.runs[0].ID, d.runs[0].Conclusion)
		}
		if len(d.commits) > 0 {
			fmt.Fprintf(&b, "%s|", d.commits[0].SHA)
		}
	}
	return b.String()
}

func (p *github) Prepare(req *plugin.Request) error {
	p.mu.Lock()
	ready := p.ready
	p.mu.Unlock()
	if ready != nil {
		select {
		case <-ready:
		case <-time.After(requestTimeout):
			return errors.New("repositories are still loading")
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.snap = snapshot{repos: p.data}
	return nil
}

func (p *github) MaxAge(*plugin.Request) time.Duration { return p.refresh }
