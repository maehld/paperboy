# 0056: One repository with separate release trains

- Status: Accepted
- Date: 2026-11-07
- Builds on: ADR 0028 (own repository), ADR 0031 (English), ADR 0041 (firmware), ADR 0042 (device protocol), ADR 0043 (firmware distribution), ADR 0049 (plugins), ADR 0050 (protocol version 3)
- Answers: issue #12

## Context

The repository carries three deliverables that are distributed separately:

| Deliverable | Shared code | Speaks | Toolchain | Changes |
|---|---|---|---|---|
| Paperboy (`cmd/paperboy`, `internal/`) | `pkg/plugin`, `pkg/render` | plugin protocol, device API | Go | daily |
| Plugins (`cmd/calendar`, `weather`, `evcc`, `pokedex`, `xkcd`, `departures`) | `pkg/plugin`, `pkg/render` | plugin protocol | Go | often together with the protocol |
| Firmware (`firmware/esphome`) | none | device API (ADR 0042) | ESPHome, pinned | rarely, and not yet flashed |

Only the firmware has a boundary of its own: it shares no code, has its own
toolchain and its own hardware concerns. Plugins and Paperboy share the public
packages and the protocol, and every protocol change touches both in one
commit; today the protocols are checked by `go test ./...`, not at runtime.

Distribution was uneven: the *Plugins* workflow attached its zips to a release
on any `v*.*.*` tag (and created the release as a side effect), the *Docker*
workflow published only an image, Paperboy had no binary artifacts, and the
firmware was built locally with `build.ps1`. Nothing named the version of a
plugin zip or a firmware image in the artifact itself, and there were no
checksums.

## Decision

### 1. Keep one repository

Paperboy, the plugins and the firmware stay in `github.com/maehld/paperboy`,
one Go module, one ADR log, one issue tracker. The cost of splitting now is
higher than the gain:

- A plugin split means publishing `pkg/plugin` and `pkg/render` as versioned
  modules (submodule tags `pkg/plugin/vX.Y.Z` or a separate SDK repository).
  Protocol changes then need a module release plus pull requests in two
  repositories, while the protocol (version 3) is still moving.
- The firmware has never run on real hardware (ADR 0041); splitting it now
  would freeze an interface that is still being tested. After the rollback
  test with a deliberately broken image, the device API is the next boundary
  to move out — the firmware is the only deliverable without shared code.
- Predictable, tested combinations are worth more to the few installations
  than independent cadences: `v2.1.0` names a Paperboy, a plugin set and
  firmware images that were built and tested together.

A later split follows these rules:

- **Firmware** may become `paperboy-firmware` once it has been flashed and the
  device API has survived a round of device iterations. Its releases then only
  attach firmware assets, and Paperboy consumes them unchanged (§4).
- **Plugins** move to an SDK repository (only `pkg/plugin` and `pkg/render`) if
  third-party plugins appear; the shipped plugins stay in this repository. Do
  not move the shipped plugins into a second repository while they share code
  with Paperboy.

A split needs a runtime protocol check first (§6).

### 2. Separate release trains

Three tag prefixes, all with SemVer numbers:

| Tag | Release train | Assets |
|---|---|---|
| `v2.1.0` | Paperboy, default | Paperboy binaries, plugin zips, firmware images |
| `plugins-v2.1.0` | plugins only | plugin zips |
| `firmware-v0.5.0` | firmware only | firmware images |

- `vX.Y.Z` is the release that contains everything at that commit; use it
  unless only plugins or only firmware changed.
- A `plugins-v*` or `firmware-v*` release carries only its own assets, so a
  plugin fix does not need a Paperboy release.
- Plugin versions follow the Paperboy numbering around them, so a `v2.1.0`
  release and a `plugins-v2.1.0` release denote the same plugin source state.
- `firmware-vX.Y.Z` numbers the bundle, not every image: `X.Y.Z` is the
  highest `fw_version` in it. Each image keeps its own version from
  `<hardware>.yaml` in the file name, and the assets name it (§4).
- Go only reads `vX.Y.Z` as a module version; the other prefixes are ignored
  by the toolchain, which is why they need no `pkg/` changes.

### 3. Workflows per deliverable

| Workflow | Runs on | Does |
|---|---|---|
| `ci.yml` | every push to `main`, every pull request | `gofmt`, `go vet ./...`, `go test ./...`, cross-build all three targets |
| `plugins.yml` | every push to `main`, every pull request, dispatch | builds the two plugin zips as artifacts; callable (`workflow_call`) |
| `firmware.yml` | pushes and pull requests touching `firmware/esphome/`, dispatch | compiles `sticky`, `e1001` and `lcd2004` with the pinned ESPHome version as artifacts; callable |
| `docker.yml` | push to `main`, tags | publishes the container image to `ghcr.io/maehld/paperboy` |
| `release.yml` | tags `v*`, `plugins-v*`, `firmware-v*` | builds the Paperboy binaries, calls the plugin and firmware workflows, collects all artifacts into one GitHub release |

Rules that follow from this:

- Tags never trigger the artifact workflows themselves: `release.yml` is the
  only place that creates a release, so notes and assets cannot race.
- Only the firmware workflow filters paths; its compilation is by far the most
  expensive step. The Go checks deliberately run on every change, so a
  required status check is never skipped.
- GitHub release notes are generated from the merged pull requests for `v*`
  tags; `plugins-v*` and `firmware-v*` releases get a short body that explains
  the assets.
- `docker.yml` does not build for changes below `firmware/` or in Markdown.

### 4. Assets

One release holds files with predictable names; the tag carries the version,
the file name carries the hardware or platform. A later download feature
(Paperboy fetching plugins or firmware itself) only needs the release API and
these names.

| Asset | Content |
|---|---|
| `paperboy-linux-amd64`, `paperboy-linux-arm64`, `paperboy-windows-amd64.exe` | the Paperboy binary per platform |
| `paperboy-plugins-linux-amd64.zip`, `paperboy-plugins-windows-amd64.zip` | `plugins/` with the six plugins, unpacked in the instance directory (ADR 0049) |
| `paperboy-firmware-<hardware>-<version>.zip` | `<hardware>/<hardware>-<version>.bin`: unpacked in the instance directory it creates `firmware/<hardware>/` (ADR 0043) |
| `paperboy-firmware-<hardware>-<version>.bin` | the same OTA image alone, for consumers that do not unzip |
| `SHA256SUMS` | checksums of all assets of the release |

The container image stays `ghcr.io/maehld/paperboy` with `X.Y.Z`, `X.Y` and
`latest` tags; it is not attached to the release.

### 5. Version sources

- Paperboy's version comes from Go's VCS stamping (`internal/paperboy/buildinfo.go`),
  as before. Release builds therefore check out with full history and tags
  (`fetch-depth: 0`); a shallow clone yields a pseudo-version and loses the
  version name.
- Plugin zips and firmware images carry no separate version: the release tag
  is their version.
- Firmware images are named after `fw_version` from `<hardware>.yaml`, which
  stays the single source of truth for what a device reports (ADR 0042).

### 6. Open prerequisites

- A runtime check of the plugin protocol version was still missing when this
  decision was written; ADR 0057 adds it and is the prerequisite for any
  repository split.
- A manifest per release that lists assets, protocol version and firmware
  versions is unnecessary while names and tags carry the information; add it
  together with the download feature.

## Consequences

- One tag releases a tested set, three prefixes allow small releases; the
  release page becomes the distribution point for everything except the
  container image.
- The firmware is now built in CI as well, so a broken ESPHome configuration
  fails on a pull request instead of on a developer's Windows machine. It
  keeps the pinned ESPHome version and caches PlatformIO packages.
- `plugins.yml` no longer creates releases on its own; the *Plugins* workflow
  comment and the README link to `release.yml` instead.
- Splitting the firmware later no longer changes what users download: the
  asset names stay, only the repository that produces them changes.
- Checksums cover transfer errors; firmware authenticity still rests on the
  LAN, as before (ADR 0043).
