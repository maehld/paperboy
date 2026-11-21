package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// repoData is one repository as last loaded. A failed request keeps the data
// it had, so one broken endpoint does not empty the page.
type repoData struct {
	spec repoSpec

	info    repoResponse
	hasInfo bool

	issues       []issue
	issuesCapped bool
	pulls        []issue
	pullsCapped  bool
	runs         []run
	commits      []commit
	commitBranch string

	openIssues int
	openPulls  int
	failing    int

	etagInfo    string
	etagIssues  string
	etagPulls   string
	etagRuns    string
	etagCommits string

	err error
}

// issue is an open issue or pull request, reduced to what the widgets show.
type issue struct {
	Number  int
	Title   string
	Author  string
	Labels  []string
	Draft   bool
	Created time.Time
	Updated time.Time
}

// commit is one commit on a branch.
type commit struct {
	SHA     string
	Message string
	Author  string
	Date    time.Time
}

// run is one workflow run.
type run struct {
	ID         int64
	WorkflowID int64
	Name       string
	Branch     string
	Event      string
	Status     string
	Conclusion string
	Created    time.Time
}

// failed reports whether the run ended in a failure the operator cares about.
// A cancelled or skipped run is not a failure.
func (r run) failed() bool {
	if r.Status != "completed" {
		return false
	}
	switch r.Conclusion {
	case "failure", "timed_out", "startup_failure", "action_required":
		return true
	}
	return false
}

// empty reports whether the repository has no data to draw yet.
func (d *repoData) empty() bool {
	return !d.hasInfo && len(d.issues) == 0 && len(d.pulls) == 0 && len(d.runs) == 0 && len(d.commits) == 0
}

// finish derives the counts from the lists and the repository answer. The
// repository's open-issues_count holds issues and pull requests together, so
// the open issues are what remains after the pull requests.
func (d *repoData) finish() {
	d.openPulls = len(d.pulls)
	if !d.pullsCapped && d.hasInfo && d.info.OpenIssuesCount >= len(d.pulls) {
		d.openIssues = d.info.OpenIssuesCount - len(d.pulls)
		d.issuesCapped = false
	} else {
		d.openIssues = len(d.issues)
	}
	d.failing = failedWorkflows(d.runs, d.info.DefaultBranch)
}

// failedWorkflows counts the workflows whose latest completed run on branch
// failed. The API returns runs newest first, so the first completed run of a
// workflow is its latest completed one.
func failedWorkflows(runs []run, branch string) int {
	seen := map[int64]bool{}
	n := 0
	for _, r := range runs {
		if branch != "" && r.Branch != branch {
			continue
		}
		if seen[r.WorkflowID] || r.Status != "completed" {
			continue
		}
		seen[r.WorkflowID] = true
		if r.failed() {
			n++
		}
	}
	return n
}

// fetchRepo loads every endpoint of one repository. A request that answers
// 304 leaves its part as it was.
func (p *github) fetchRepo(ctx context.Context, spec repoSpec, old *repoData) *repoData {
	fd := &repoData{spec: spec}
	if old != nil {
		copied := *old
		fd = &copied
	}
	fd.err = nil

	var errs []error
	record := func(err error) {
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", spec.Name, err))
		}
	}
	base := "/repos/" + url.PathEscape(spec.Owner) + "/" + url.PathEscape(spec.Repo)

	var info repoResponse
	if modified, _, err := p.get(ctx, base, &fd.etagInfo, &info); err != nil {
		record(err)
	} else if modified {
		fd.info, fd.hasInfo = info, true
	}

	var issues []issueResponse
	path := fmt.Sprintf("%s/issues?state=open&per_page=%d&sort=updated", base, issuesPerPage)
	modified, next, err := p.get(ctx, path, &fd.etagIssues, &issues)
	if err != nil {
		record(err)
	} else if modified {
		fd.issues = convertIssues(issues)
		fd.issuesCapped = next
	}

	var pulls []issueResponse
	path = fmt.Sprintf("%s/pulls?state=open&per_page=%d&sort=updated", base, pullsPerPage)
	modified, next, err = p.get(ctx, path, &fd.etagPulls, &pulls)
	if err != nil {
		record(err)
	} else if modified {
		fd.pulls = convertPulls(pulls)
		fd.pullsCapped = next
	}

	var runs runsResponse
	path = fmt.Sprintf("%s/actions/runs?per_page=%d", base, runHistory)
	modified, _, err = p.get(ctx, path, &fd.etagRuns, &runs)
	if err != nil {
		record(err)
	} else if modified {
		fd.runs = convertRuns(runs.WorkflowRuns)
	}

	branch := fd.info.DefaultBranch
	if branch != fd.commitBranch {
		// The old commits belong to another branch and its ETag to another URL.
		fd.etagCommits, fd.commitBranch = "", branch
	}
	path = fmt.Sprintf("%s/commits?per_page=%d", base, commitHistory)
	if branch != "" {
		path += "&sha=" + url.QueryEscape(branch)
	}
	var commits []commitResponse
	modified, _, err = p.get(ctx, path, &fd.etagCommits, &commits)
	if err != nil {
		record(err)
	} else if modified {
		fd.commits = convertCommits(commits)
	}

	fd.err = errors.Join(errs...)
	fd.finish()
	return fd
}

// get makes one conditional request. It reports whether the answer carried a
// body (false on 304) and whether a list has a next page.
func (p *github) get(ctx context.Context, path string, etag *string, out any) (modified, next bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.api+path, nil)
	if err != nil {
		return false, false, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
	if *etag != "" {
		req.Header.Set("If-None-Match", *etag)
	}
	client := p.client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, false, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotModified:
		return false, false, nil
	case resp.StatusCode != http.StatusOK:
		return false, false, apiError(resp)
	}
	if tag := resp.Header.Get("ETag"); tag != "" {
		*etag = tag
	}
	next = strings.Contains(resp.Header.Get("Link"), `rel="next"`)
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(out); err != nil {
		return false, false, fmt.Errorf("answer: %w", err)
	}
	return true, next, nil
}

// apiError turns an error answer into one line, with the rate limit reset when
// GitHub names it.
func apiError(resp *http.Response) error {
	var body struct {
		Message string `json:"message"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body)
	msg := body.Message
	if msg == "" {
		msg = resp.Status
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			msg += " (rate limit exhausted"
			if reset := resp.Header.Get("X-RateLimit-Reset"); reset != "" {
				var unix int64
				if _, err := fmt.Sscan(reset, &unix); err == nil {
					msg += ", resets at " + time.Unix(unix, 0).Format("15:04")
				}
			}
			msg += ")"
		}
	}
	return fmt.Errorf("%s: %s", resp.Status, msg)
}

type user struct {
	Login string `json:"login"`
}

type label struct {
	Name string `json:"name"`
}

type repoResponse struct {
	FullName        string `json:"full_name"`
	DefaultBranch   string `json:"default_branch"`
	OpenIssuesCount int    `json:"open-issues_count"`
}

type issueResponse struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	User        *user     `json:"user"`
	Labels      []label   `json:"labels"`
	PullRequest *struct{} `json:"pull_request"`
	Draft       bool      `json:"draft"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type runsResponse struct {
	WorkflowRuns []runResponse `json:"workflow_runs"`
}

type runResponse struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	WorkflowID int64     `json:"workflow_id"`
	HeadBranch string    `json:"head_branch"`
	Event      string    `json:"event"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	CreatedAt  time.Time `json:"created_at"`
}

type commitResponse struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Name string    `json:"name"`
			Date time.Time `json:"date"`
		} `json:"author"`
	} `json:"commit"`
	Author *user `json:"author"`
}

func convertIssues(in []issueResponse) []issue {
	out := make([]issue, 0, len(in))
	for _, r := range in {
		if r.PullRequest != nil {
			continue // the issues endpoint also lists pull requests
		}
		out = append(out, issue{
			Number: r.Number, Title: r.Title, Author: login(r.User),
			Labels: labelNames(r.Labels), Created: r.CreatedAt, Updated: r.UpdatedAt,
		})
	}
	return out
}

func convertPulls(in []issueResponse) []issue {
	out := make([]issue, 0, len(in))
	for _, r := range in {
		out = append(out, issue{
			Number: r.Number, Title: r.Title, Author: login(r.User),
			Labels: labelNames(r.Labels), Draft: r.Draft,
			Created: r.CreatedAt, Updated: r.UpdatedAt,
		})
	}
	return out
}

func convertRuns(in []runResponse) []run {
	out := make([]run, 0, len(in))
	for _, r := range in {
		out = append(out, run{
			ID: r.ID, WorkflowID: r.WorkflowID, Name: r.Name, Branch: r.HeadBranch,
			Event: r.Event, Status: r.Status, Conclusion: r.Conclusion, Created: r.CreatedAt,
		})
	}
	return out
}

func convertCommits(in []commitResponse) []commit {
	out := make([]commit, 0, len(in))
	for _, r := range in {
		author := login(r.Author)
		if author == "" {
			author = r.Commit.Author.Name
		}
		out = append(out, commit{
			SHA: r.SHA, Message: firstLine(r.Commit.Message), Author: author,
			Date: r.Commit.Author.Date,
		})
	}
	return out
}

func login(u *user) string {
	if u == nil {
		return ""
	}
	return u.Login
}

func labelNames(labels []label) []string {
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		if l.Name != "" {
			out = append(out, l.Name)
		}
	}
	return out
}

// firstLine reduces a commit message to its subject.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}
