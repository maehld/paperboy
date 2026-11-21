# 0147: Separate server, plugin and firmware releases

- Status: Accepted, implemented
- Date: 2026-11-21
- Builds on: ADR 0056 (release trains), ADR 0075 (a version per plugin), ADR 0089 (channels and prereleases), ADR 0101 (the Paperboy hint), ADR 0146 (three modules)
- Supersedes: the tag table and the separate-release paragraph of ADR 0056
- Answers: issue #368

## Context

One tag released everything. A `vX.Y.Z` tag built the Paperboy binaries, the plugin zips and the firmware bundle. The tags `plugins-v*` and `firmware-v*` existed in the workflow and in the documents, but no release ever used them: every release was a combined one.

That shape has two costs:

- A plugin fix and a firmware fix need a full server release, although their source is independent.
- The release version mixes three version lines. The server is one program with one version. The plugins are twelve programs, and the firmware is four images, each with its own version ([ADR 0075](0075-plugin-versions.md), [ADR 0043](0043-firmware-distribution.md)).

The download client already works per asset: it reads the newest release that carries the plugin zip or the firmware bundle, and then compares the versions inside the archive. The tag of a plugin or firmware release is therefore a label, not an installed version.

## Decision

### 1. One tag is one release

The repository releases the three deliverables separately. A tag names one train and produces exactly one GitHub release with only that train's assets.

| Train | Tag | Content |
|---|---|---|
| Server | `server-vX.Y.Z` | The server binaries; their version is `X.Y.Z` |
| Plugins | `plugins-YYYY.MM.DD` | The plugin zips; every program keeps its own `VERSION` |
| Firmware | `firmware-YYYY.MM.DD` | The firmware bundle; every image keeps its own `fw_version` |

Several tags may point to the same commit. A release therefore does not need a combined tag. The `vX.Y.Z` train ends; its releases stay as history.

### 2. A date labels a set that has no shared version

The plugin set and the firmware bundle have no semantic version of their own. A date says what the release contains without inventing one. The release notes list the versions inside, so an operator sees what a date means.

A second release of a train on the same day appends `.1`, `.2`, and so on: `plugins-2026.11.21.1`. The first release of the day has no suffix. The date uses dots, not hyphens. The prerelease detection strips the train prefix and then looks for `-rc` or `-beta`; a hyphen inside the date would mark every release as a prerelease.

A suffix marks a prerelease: `server-v3.31.0-rc.1`, `plugins-2026.11.21-rc.1`. The channels of [ADR 0089](0089-release-channels.md) keep their meaning.

### 3. A tag check guards the package

`scripts/check-release-tag.sh` rejects a tag that does not match its train before anything builds. The server tag must carry a SemVer version. The plugin and firmware tags must carry a valid date and an optional sequence and suffix.

### 4. The container image follows the server only

The image holds the Paperboy binary, so only a server release publishes it. A final release gets `X.Y.Z`, `X.Y` and `latest`; a prerelease gets only its full version. No `main` build and no plugin or firmware release publishes an image. A pull request builds the image without pushing it.

Every release sets `make_latest: false`. GitHub's "Latest" badge is repository-wide; with three trains no release should claim it. The Docker tag `latest` is independent of that badge and names the newest final server release.

## Consequences

- Installations keep working. The plugin and firmware client picks releases by their assets, so a dated release installs like a combined one. Old `vX.Y.Z` releases stay readable.
- The Paperboy hint reads `server-v*` ([ADR 0101](0101-a-hint-about-a-newer-paperboy.md)). An instance on an old combined release compares its version with the next server release as before.
- A release is no longer an atomic set of all three deliverables. An operator picks the newest release of each train. The download client already installs each deliverable on its own.
- The release order is the order of creation at GitHub, not the date in a tag. A later release is the newest even if its date is earlier.
- The download client reads at most 100 releases. Frequent releases of three trains shrink the window for old pins. Pagination stays a later task.
- The old combined release `v4.0.0` is older than the `3.29.0` line. A server release `server-v3.30.0` is higher than both for the hint; an instance on the old `v4.0.0` stays without a hint until the server line passes 4.0.0.
- The Docker image is no longer a by-product of every `main` build. `latest` means the newest final server release.
