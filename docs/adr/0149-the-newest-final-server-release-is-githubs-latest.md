# 0149: The newest final server release is GitHub's latest

- Status: Accepted, implemented
- Date: 2026-11-21
- Builds on: ADR 0147 (separate releases), ADR 0148 (the image is primary)
- Amends: the badge rule of ADR 0148
- Answers: issue #373

## Context

GitHub keeps one release as its latest. ADR 0148 sets `make_latest: false` on every release, so the badge stayed on the last combined release, `v3.29.0`. A badge on an old release misleads: the server is the main product, and the newest server release is the state of the project.

## Decision

The newest final server release is GitHub's latest. `release.yml` sets `make_latest` to true for a final `server-v*` release. A prerelease cannot be latest and sets false. A plugin or firmware release sets false, so it never takes the badge from the server.

The badge is repository-wide: it names the newest final server release, not the newest release of any train.

## Consequences

- The badge moves with every final server release.
- A plugin or firmware release created later leaves the badge on the server.
- A later backport of an older server version takes the badge, because GitHub orders by creation. Release server versions in order.
- `server-v3.30.0` carries the badge from a manual move; the next final server release moves it on.
