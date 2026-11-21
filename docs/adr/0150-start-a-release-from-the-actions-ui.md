# 0150: Start a release from the Actions UI

- Status: Accepted, implemented
- Date: 2026-11-21
- Builds on: ADR 0147 (separate releases), ADR 0148 (the image is primary), ADR 0149 (the latest badge)
- Answers: issue #380

## Context

A release starts when a tag is pushed. That works from a terminal, but not from the Actions UI. The maintainer cannot start a release in the browser, and a tag pushed by a workflow with `GITHUB_TOKEN` does not trigger another workflow: GitHub suppresses that to stop recursion.

## Decision

`release.yml` gets a `workflow_dispatch` with two inputs:

| Input | Value |
|---|---|
| `train` | `server`, `plugins` or `firmware` |
| `version` | `X.Y.Z` for the server, `YYYY.MM.DD[.N]` for plugins and firmware; empty means today in UTC |

The branch is the one selected in the UI. A `prepare` job computes and checks the tag and outputs `tag`, `train`, `version`, `minor` and `prerelease`. The server, Docker, plugin and firmware jobs use these outputs instead of `github.ref_name`.

The release step sets `tag_name` and `target_commitish: github.sha`. GitHub therefore creates the tag together with the release. The tag exists only after a successful build. A failed build leaves no tag and no release.

A tag push keeps its path. The `prepare` job reads the tag instead of the inputs, and the release step uses the existing tag.

`concurrency: release` runs one release at a time, so two dispatches cannot create the same tag.

## Consequences

- The maintainer starts a release in the browser.
- The server binary gets its version from the input. Go's VCS stamping still names the commit of the selected branch.
- A dispatch on an older commit can hit the token limit for tag creation. Release the current branch head.
- The date for plugins and firmware is UTC when the version is empty. A second release on the same day needs `.1`.
- Tag pushes behave as before.
