# github plugin

The state of GitHub repositories as widgets: open issues, open pull requests,
failed workflow runs and the commit history. The plugin reads the
[GitHub REST API](https://docs.github.com/en/rest) in the background. Public
repositories need no account; a token raises the rate limit and reads private
repositories.

## Configuration

| Key | Description |
|---|---|
| `repositories` | Required: a list of `owner/repo` entries or `{name, repo}` entries. The name is what the widgets select; without `name` the repository part is the name |
| `token` | Optional: a GitHub token. It raises the rate limit and reads private repositories |
| `refresh` | How often every repository is fetched, default `15m`, at least `5m` |
| `api` | API base URL, default `https://api.github.com`; set it for GitHub Enterprise |

**Demand:** Paperboy names the next moment a display needs the data and the
plugin fetches then, never more often than `refresh`
([ADR 0126](../adr/0126-a-schedule-event-for-plugin-demand.md)). A page that no
display shows costs no request. The plugin requires Paperboy protocol 9.

```yaml
plugins:
  github:
    config:
      token: ${GITHUB_TOKEN}
      refresh: 15m
      repositories:
        - maehld/paperboy
        - { name: solarai, repo: example/solarai }
```

The token is a classic personal access token with the scope `repo` for private
repositories and `public_repo` for public ones. A fine-grained token needs read
access to the contents, the issues, the pull requests and the actions.
`${GITHUB_TOKEN}` keeps the secret out of `paperboy.yaml`.

## Data source

One fetch makes five requests per repository. They cover the repository, its
open issues, its open pull requests, its workflow runs and the commits of the
default branch. Every request uses `ETag` and `If-None-Match`, a 20 s timeout and a
2 MB cap. A `304` keeps the last answer. A failed request keeps the data it
had, and the plugin retries within two minutes. A repository that never loaded
shows the error in the cell.

GitHub allows 60 requests per hour without a token. One repository at the
default refresh costs 20 requests per hour, so a token is necessary for more
than one repository or a shorter refresh. A `304` on an authenticated request
does not count against the limit.

The repository answer counts open issues and pull requests together. The plugin
subtracts the open pull requests, so the issue count is exact. A list with more
than 100 entries shows its first page and a `+`, for example `100+`.

A workflow counts as failed when its latest completed run on the default branch
ended with `failure`, `timed_out`, `startup_failure` or `action_required`. A
cancelled or skipped run does not count. The plugin reads the latest 50 runs of
all branches.

## Widget `repo`

The open issues, the open pull requests, the failed workflows and the newest
commit.

| Arg | Description |
|---|---|
| `repo` | A configured name, or `all` (default) for the sum and the newest commit of every repository |

## Widget `issues`

The open issues, newest first.

| Arg | Description |
|---|---|
| `repo` | A configured name, or `all` (default) |
| `count` | Most issues, `0` (default) fills the cell |
| `header` | The repository's name above the list |
| `labels` | Only issues with one of these labels |

Every row shows the number, the title, the author, up to two labels and the
age. The list fills the cell, so a taller cell shows more issues.

## Widget `pulls`

The open pull requests, newest first. Drafts carry the word `draft` in their
row.

| Arg | Description |
|---|---|
| `repo` | A configured name, or `all` (default) |
| `count` | Most pull requests, `0` (default) fills the cell |
| `header` | The repository's name above the list |

## Widget `runs`

The latest workflow runs. A failure shows in bold.

| Arg | Description |
|---|---|
| `repo` | A configured name, or `all` (default) |
| `count` | Most runs, `0` (default) fills the cell |
| `branch` | `default` (default) for the repository's default branch, `all`, or a branch name |

The plugin keeps the latest 50 runs of all branches. A widget that names a
long unused branch may show no run.

## Widget `commits`

The commit history of the default branch.

| Arg | Description |
|---|---|
| `repo` | A configured name, or `all` (default) |
| `count` | Most commits, `0` (default) fills the cell |

Every row shows the subject, the short SHA, the author and the age.

## Values

A text page reads these values
([ADR 0111](../adr/0111-text-pages-from-plugin-values.md),
[0120](../adr/0120-arguments-for-values.md)). The argument `repo` picks one
configured repository by name, or all repositories (default `all`):

| Value | Type | Meaning |
|---|---|---|
| `open-issues` | number | open issues |
| `open-pulls` | number | open pull requests |
| `failed-runs` | number | workflows whose latest run on the default branch failed |
| `last-commit` | text | short SHA and subject of the newest commit |
| `last-commit-at` | timestamp | date of the newest commit |
| `last-run` | text | workflow name and result of the newest run |
| `last-run-at` | timestamp | date of the newest run |
| `state` | enum | `ok` without a failed workflow, `failing` otherwise |

The enum `state` fits a plan condition
([ADR 0127](../adr/0127-plan-conditions-are-values.md)): show the repository
page only while the state is `failing`.

See `examples/pages/github.yaml` for a page with all five widgets and
`examples/pages/github.lcd2004.yaml` for a text display.
