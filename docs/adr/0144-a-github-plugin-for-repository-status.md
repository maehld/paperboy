# 0144: A GitHub plugin for repository status

- Status: Accepted, implemented
- Date: 2026-11-21
- Builds on: ADR 0049 (plugin widgets), 0075 (plugin versions), 0111 (values for text pages), 0120 (arguments for values), 0126 (schedule event), 0127 (plan conditions are values)
- Answers: issue #359

## Context

An operator keeps repositories on GitHub. A display at the desk should show
open issues, open pull requests, failed workflow runs and the commit history.

GitHub offers a REST API. Public repositories need no account. The
unauthenticated limit is 60 requests per hour and address. A personal access
token raises the limit and reads private repositories. The API answers
conditional requests with an `ETag`, so an unchanged resource costs no body.

The plugin protocol has values for text pages (ADR 0111) and a schedule event
for demand (ADR 0126). One fetch serves every widget of a page. Paperboy's
state is for data that a restart cannot rebuild cheaply (ADR 0081). Every
GitHub answer is one request away, so the plugin keeps no state.

## Decision

1. One plugin `github` reads the GitHub REST API v3. It adds no Go dependency.
2. The config lists one or more `owner/repo` pairs or `{name, repo}` entries.
   The widgets select one repository by name or all of them.
3. The token is optional. The config carries it, and `${GITHUB_TOKEN}` keeps
   the secret out of `paperboy.yaml`. The plugin sends the token as a bearer
   token and never stores it.
4. One fetch makes five requests per repository: the repository, the open
   issues, the open pull requests, the latest workflow runs and the commits of
   the default branch. Every request is conditional. A `304` keeps the last
   answer. A failed request keeps its part, and the plugin retries within two
   minutes.
5. The plugin fetches on demand through the schedule event and never more often
   than `refresh`. The default is 15m, the minimum 5m.
6. A workflow counts as failed when its latest completed run on the default
   branch ended with `failure`, `timed_out`, `startup_failure` or
   `action_required`. A cancelled or skipped run does not count. The plugin
   reads the latest 50 runs of all branches.
7. The widgets are `repo`, `issues`, `pulls`, `runs` and `commits`. Values for
   text pages are the counts, the newest commit, the newest run and an enum
   `state` for plan conditions.
8. The first version has no actions. A write from the device menu needs a token
   with write access, and the menu is not the place to start a build by
   accident. A later ADR can add a rerun action.

## Consequences

- A display shows the repository state without a browser. One plugin instance
  covers several repositories.
- Without a token the plugin lasts for three repositories at the default
  refresh. The plugin documentation asks for a token.
- A count above 100 shows as `100+`, because the plugin keeps the first page
  only.
- The `state` value describes the default branch. A failing run on a feature
  branch appears in the `runs` widget only.
- The plugin keeps no state. A restart costs one fetch, and every page shows
  live data.
