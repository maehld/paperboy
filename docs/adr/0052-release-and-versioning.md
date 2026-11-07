# 0052: Release and versioning of Paperboy, plugins and firmware

- Status: Proposed
- Date: 2026-11-07
- Builds on: ADR 0041 (ESPHome firmware), ADR 0043 (firmware distribution), ADR 0049 (plugin widgets)
- Issue: #12

## Context

The repository ships three deliverables:

- **Paperboy**, the service (`cmd/paperboy`), as binaries built by `scripts/build.ps1` and as a container image. The *Docker* workflow already pushes `ghcr.io/maehld/paperboy` on `main` (`main`, `latest`, `sha-…`) and on `v*.*.*` tags (`X.Y.Z`, `X.Y`). The binary has no version of its own; only `buildvcs` stamps the commit.
- **Plugins** (`cmd/weather`, `cmd/calendar`, `cmd/evcc`), zipped per platform by `scripts/package-plugins.sh`; the *Plugins* workflow attaches the zips to a GitHub release on `v*.*.*` tags.
- **Firmware** (`firmware/esphome`), built locally with `build.ps1` (ESPHome is pinned, compiling takes minutes and needs no hardware); every hardware file has its own `fw_version` substitution (`sticky` 0.4.0, `e1001` 0.4.1, `lcd2004` 0.2.1). `build.ps1 -Instance` drops `<hardware>-<version>.bin` into the instance's `firmware/`, where Paperboy offers the highest version per hardware (ADR 0043).

The public Go packages `pkg/plugin` and `pkg/render` live in the single module `github.com/maehld/paperboy`. Every request already carries `version: 2` (`plugin.Version`), but neither side checks it, and plugins do not report a version.

There are no tags and no releases yet. Commits on `main` are squash merges whose subject ends with `[#<issue>]`.

## Decision

### 1. One SemVer version and one tag for Paperboy, plugins and `pkg/`

Paperboy, the shipped plugins and the Go module share **one version and one tag `vX.Y.Z`** (SemVer 2.0). The plugins are built from the same commit, use the same `pkg/` code and are tested together; separate versions would only add a compatibility matrix nobody maintains.

- **Major:** incompatible change for operators or plugin authors: `paperboy.yaml`, page or plan format, a new plugin protocol version (section 2), the device API (ADR 0042) or a breaking change in `pkg/`.
- **Minor:** new features (widgets, options, endpoints), backwards compatible.
- **Patch:** fixes only.
- Pre-releases as `vX.Y.Z-rc.N`; they are marked as pre-release on GitHub and do not move `latest`.
- The first tag is **`v2.0.0`**, matching "Paperboy v2" (ADR 0049).

Because the module path has no `/v2` suffix, Go's semantic import versioning would reject a `v2.0.0` tag for `go get` (it resolves as `+incompatible` only without a `go.mod`). Two options; see *Open decisions*:

- **a)** Start at `v2.0.0` and change the module path to `github.com/maehld/paperboy/v2` (all imports change once; plugin authors import `github.com/maehld/paperboy/v2/pkg/plugin`).
- **b)** Start at `v0.x` (e.g. `v0.2.0`): no compatibility promise for `pkg/` yet, the module path stays, and "v2" remains the architecture name only.

Recommendation: **b)** until the plugin protocol has outside users; `v1.0.0` then makes the compatibility promise for `pkg/` explicit.

### 2. Plugin protocol version

The protocol version (`plugin.Version`, currently 2) is **independent of the release version** and changes only with incompatible protocol changes. Compatible additions (new optional request fields, new optional answer fields) keep the number; both sides ignore unknown fields, as JSON decoding already does.

- Paperboy keeps sending `version` with every request.
- The answer to `configure` and `describe` gains an optional `protocol` field (the plugin's `plugin.Version`) and an optional `version` field (the plugin's release version, informational). `pkg/plugin.Serve` fills both automatically.
- Paperboy compares `protocol` on `configure`: missing or equal is accepted (plugins written before this ADR answer without it); a different number fails all cells of that plugin with "plugin speaks protocol N, Paperboy speaks M" and is reported in the log, `GET /api/plugins` and the widget gallery. There is no negotiation of several versions: one number on each side, a mismatch is a clear error, and since plugins and Paperboy ship together the shipped plugins never mismatch.
- `pkg/plugin.Handle` answers a request whose `version` differs with an error instead of guessing.

### 3. Embedding versions

- Go binaries get the version through `-ldflags "-X github.com/maehld/paperboy/internal/version.Version=vX.Y.Z"`. A small package `internal/version` holds `Version` (default `dev`) plus commit and date from `runtime/debug.ReadBuildInfo` (`buildvcs`), and exposes `String()`. For plugins the value is passed to `pkg/plugin` through an exported variable `plugin.ProgramVersion` set by the same `-X` flag (pkg/ cannot import internal/).
- The version comes from `git describe --tags --always --dirty` in `scripts/build.ps1`, `scripts/package-plugins.sh` and the Dockerfile (build argument `VERSION`, set by the workflow from the tag).
- Shown in: `paperboy -version` (and the plugins' `-version`), the startup log line, `GET /api/version` (`{version, commit, date, protocol}`), the dashboard footer, and per plugin in `GET /api/plugins` and the widget gallery.
- **Firmware:** the source of truth stays the `fw_version` substitution in each hardware file; firmware versions are **independent per hardware** and not tied to the Paperboy version, because devices update over the air one image at a time (ADR 0043) and most Paperboy releases do not touch the firmware. The firmware already reports its version in `register` and `sync`, and the dashboard shows it.

### 4. Firmware tags and releases

Firmware is released with its own tag **`firmware/<hardware>/vX.Y.Z`** (e.g. `firmware/sticky/v0.4.1`), only when `fw_version` changes. A firmware workflow on such a tag builds that hardware with the pinned ESPHome in CI and attaches `<hardware>-<version>.bin` (the OTA image, ready to drop into `firmware/<hardware>/`) and the factory image for USB flashing. A check in CI fails when the tag version and the `fw_version` in the file differ. Paperboy releases do not contain firmware.

### 5. Release contents

A GitHub release for `vX.Y.Z` contains:

| Asset | Content |
|---|---|
| `paperboy-X.Y.Z-<os>-<arch>.zip` (`.tar.gz` for Linux) | Paperboy binary, `paperboy.example.yaml`, README excerpt, LICENSE |
| `paperboy-plugins-X.Y.Z-<os>-<arch>.zip` | as today, now with the version in the name |
| `SHA256SUMS` | SHA-256 of every asset (`sha256sum` format) |

Platforms: `linux/amd64`, `linux/arm64`, `windows/amd64` (as `build.ps1`). `odtest` is a developer tool and not released.

Container image: `ghcr.io/maehld/paperboy` (already in place) with tags `X.Y.Z`, `X.Y`, `X` and `latest` for releases, built for `linux/amd64` and `linux/arm64`; `main` keeps `main` and `sha-…`. `latest` moves to the latest release instead of `main`. No Docker Hub.

Signing (cosign, GitHub artifact attestations) is not part of this ADR; `SHA256SUMS` is enough for now.

### 6. Changelog

Release notes are generated by GitHub's automatic release notes (`generate_release_notes`), configured by `.github/release.yml` with categories from the issue labels (`enhancement` → Features, `bug` → Fixes, `documentation` → Documentation, rest → Other). Since every PR is squash-merged and its subject ends with `[#<issue>]`, the PR list is the changelog; no `CHANGELOG.md` is maintained in the repository. The maintainer may edit the draft notes before publishing.

### 7. Automation and manual steps

One workflow `release.yml` replaces the release step of `plugins.yml`:

1. **Manual:** decide the version, create and push the tag `vX.Y.Z` on `main` (`git tag -a vX.Y.Z -m vX.Y.Z && git push origin vX.Y.Z`).
2. **Automatic** on the tag: `go vet` and `go test ./...`, build binaries and plugin zips for all platforms with the version embedded, `SHA256SUMS`, create a **draft** release with generated notes and all assets; the Docker workflow pushes the image with the version tags.
3. **Manual:** review the draft notes and publish the release.

`plugins.yml` and `docker.yml` keep building on `main` and pull requests as a check. Firmware releases follow the same pattern with their own tag (section 4).

## Consequences

- One number tells operators which Paperboy and plugins belong together; plugin authors see protocol mismatches as a clear error instead of broken cells.
- `pkg/` gets an explicit (if, with option b, still pre-1.0) versioning story.
- Firmware keeps its own cadence per hardware, matching OTA distribution.
- More CI: a release workflow and a firmware workflow with ESPHome (cached pip install, a few minutes per hardware).
- Releasing stays a deliberate act: tag and publish are manual.

## Open decisions

1. Go module path: `v0.x` without path change (recommended) or `v2.0.0` with `/v2` module path.
2. Firmware built in CI on `firmware/<hardware>/v…` tags, or kept local with `build.ps1` and attached by hand.
3. Container `latest` = latest release (proposed) or `main` (current).
4. Add `linux/arm64` to the container image (proposed) and `linux/arm64` to the plugin zips.
5. Protocol mismatch strictness: reject a different `protocol` (proposed) or only warn.
