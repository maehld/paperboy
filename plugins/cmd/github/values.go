package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/maehld/paperboy/sdk/plugin"
)

// Values answers the declared values of a text page (ADR 0111).
func (p *github) Values(req *plugin.Request) (map[string]plugin.Value, error) {
	if err := req.ValueArgs.Check(p.valueArgs()); err != nil {
		return nil, err
	}
	list, err := p.snap.selectRepo(req.ValueArgs.String("repo", "all"))
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

	issues, _, pulls, _, failing := aggregate(list)
	commit, commitRepo := newestCommit(list)
	run, runRepo, hasRun := newestRun(list)
	out := make(map[string]plugin.Value, len(req.Values))
	for _, name := range req.Values {
		switch name {
		case "open-issues":
			out[name] = plugin.NumberValue(float64(issues))
		case "open-pulls":
			out[name] = plugin.NumberValue(float64(pulls))
		case "failed-runs":
			out[name] = plugin.NumberValue(float64(failing))
		case "last-commit":
			if commit.SHA == "" {
				out[name] = plugin.TextValue("–")
				continue
			}
			text := shortSHA(commit.SHA) + " " + commit.Message
			if len(list) > 1 && commitRepo != nil {
				text = commitRepo.spec.Name + ": " + text
			}
			out[name] = plugin.TextValue(text)
		case "last-commit-at":
			if commit.SHA == "" {
				out[name] = plugin.TextValue("–")
				continue
			}
			out[name] = plugin.TimestampValue(commit.Date)
		case "last-run":
			if !hasRun {
				out[name] = plugin.TextValue("–")
				continue
			}
			text := strings.Join([]string{run.Name, statusText(run, req)}, " – ")
			if len(list) > 1 && runRepo != nil {
				text = runRepo.spec.Name + ": " + text
			}
			out[name] = plugin.TextValue(text)
		case "last-run-at":
			if !hasRun {
				out[name] = plugin.TextValue("–")
				continue
			}
			out[name] = plugin.TimestampValue(run.Created)
		case "state":
			if failing > 0 {
				out[name] = plugin.EnumValue("failing")
			} else {
				out[name] = plugin.EnumValue("ok")
			}
		default:
			return nil, fmt.Errorf("%w %q", plugin.ErrUnknownValue, name)
		}
	}
	return out, nil
}

// newestRun returns the newest run of the selected repositories, preferring
// each repository's default branch.
func newestRun(list []*repoData) (run, *repoData, bool) {
	var best run
	var from *repoData
	for _, d := range list {
		r, ok := latestRun(d)
		if !ok {
			continue
		}
		if from == nil || r.Created.After(best.Created) {
			best, from = r, d
		}
	}
	return best, from, from != nil
}

func latestRun(d *repoData) (run, bool) {
	if len(d.runs) == 0 {
		return run{}, false
	}
	return d.runs[0], true
}
