[← Design documents](README.md) · [Paperboy](../../README.md)

# Delivery

One repository ships three deliverables – Paperboy, the plugins and the firmware – each with its own release train ([ADR 0056](../adr/0056-repository-layout-and-release-trains.md), [ADR 0147](../adr/0147-separate-server-plugin-and-firmware-releases.md)). One tag is one release.

## Release trains

| Tag | Release contains | Version |
|---|---|---|
| `server-vX.Y.Z` | The server binaries | The SemVer version of the server |
| `plugins-YYYY.MM.DD[.N]` | The plugin zips | A date; every plugin keeps its `VERSION` |
| `firmware-YYYY.MM.DD[.N]` | The firmware bundle | A date; every image keeps its `fw_version` |

Several tags may point to the same commit. A second release of a train on the same day appends `.1`, `.2`, and so on. A suffix (`server-v1.4.0-rc.1`, `plugins-2026.11.21-rc.1`) becomes a prerelease. Only the `latest` channel installs those; the `stable` channel skips them ([ADR 0089](../adr/0089-release-channels.md)). `scripts/check-release-tag.sh` checks the shape of the tag before the build. The `vX.Y.Z` train ends; its releases stay as history.

| Asset | Content |
|---|---|
| `paperboy-<os>-<arch>[.exe]` | The binary for Linux and Windows, amd64 and arm64 |
| `paperboy-plugins-<os>-<arch>.zip` | `plugins/<plugin>-v<version>` for the platform, plus the OpenMoji licences of the weather and evcc icons |
| `paperboy-firmware.zip` | One flat bundle with the image of every hardware type, unpacks to `firmware/` ([ADR 0059](../adr/0059-firmware-bundle.md), [0060](../adr/0060-download-from-releases.md), [0063](../adr/0063-constant-firmware-bundle-name.md)) |
| `SHA256SUMS` | The checksums that an instance verifies before it installs anything |

## Starting a release

A release starts from a tag or from the Actions UI ([ADR 0150](../adr/0150-start-a-release-from-the-actions-ui.md)). In the UI choose *Release* → *Run workflow*, the train, the version and the branch. An empty version is today in UTC for plugins and firmware. The workflow creates the tag with the release, so a failed build leaves no tag. A tag push behaves as before.

## Workflows

Only `release.yml` creates a GitHub release. The other workflows build for changes and pull requests and never run on a tag.

| Workflow | Job |
|---|---|
| *CI* | `gofmt`, `go vet`, tests and a cross-build on every change |
| *Plugins* | Builds both plugin zips for a change under `plugins/`, `sdk/` or the module files; fails a pull request that changed `plugins/cmd/<plugin>` without raising its `VERSION`, and one that changed `sdk/plugin` or `sdk/render` without raising every plugin's version ([ADR 0107](../adr/0107-plugin-builds-reach-installations.md)) |
| *Firmware* | Compiles only the hardware types whose files changed with the pinned ESPHome, and bundles them into one image archive; a release builds all four |
| *Docker* | Checks the container image on a pull request without pushing ([ADR 0148](../adr/0148-the-container-image-is-the-primary-server-artifact.md)) |
| *Release* | Starts from a tag or from the Actions UI, builds the server binaries, calls *Plugins* and *Firmware* for the train, builds and pushes the image for a server release, attaches the assets with `SHA256SUMS`, writes notes per train and sets the newest final server release as GitHub's latest |

A change to `common.yaml` builds `sticky`, `e1001` and `e1003`. A change to `text-common.yaml` builds `lcd2004`. A header change builds all four.

Two rules follow from the version stamping:

- The server's version comes from Go's VCS stamping, so a release build needs the full history (`fetch-depth: 0`). A build without a repository reports `dev`.
- The firmware images carry `fw_version` in their name. The release tag labels the bundle by date ([ADR 0147](../adr/0147-separate-server-plugin-and-firmware-releases.md)) and names no image version. The *Firmware* workflow compiles the same images in CI, so a tag ships what was tested.

The container image is the primary server artifact. A server release builds and pushes it before the release appears. A final release gets `X.Y.Z`, `X.Y` and `latest`; a prerelease keeps only its full version. No `main` build and no plugin or firmware release publishes an image. A pull request only builds the image to check it. GitHub's latest badge names the newest final server release; a prerelease and a plugin or firmware release do not take it.

## The download client

A `download` section enables the release checks ([ADR 0060](../adr/0060-download-from-releases.md), [0062](../adr/0062-plugin-updates-and-download-control.md)):

```yaml
download:
  source: https://github.com/maehld/paperboy
  token: ${GITHUB_TOKEN}
  interval: 24h
  plugin_channel: stable
  firmware_channel: latest
```

`download.go` runs at startup and then once per `interval` (default 24 hours, at least 1 hour). `POST /ui/download` starts a check at once.

One check builds one client per train – plugins, firmware and Paperboy – and then:

1. Reads the release list of the train. The list is cached with its ETag in `state/download/<train>/cache.json`, so an unchanged answer costs no request and no rate limit ([ADR 0090](../adr/0090-download-cache-and-inventory.md)).
2. Picks the newest release with a wanted asset. The tag prefix does not matter; the asset names the train. `latest` takes prereleases; `stable` skips them and also refuses a version with a suffix inside a release that is not flagged as a prerelease. A pin bypasses the channel.
3. Installs the program of a configured plugin that has none as `plugins/<plugin>-v<version>`, and a newer version next to an older downloaded one. An unversioned program and a pin stay.
4. Installs every image of the newest firmware bundle as `firmware/<hardware>-v<version>.bin` ([ADR 0061](../adr/0061-install-every-bundle-image.md)). A `firmware.version` pin restricts this to one version.
5. Verifies each archive against the release's `SHA256SUMS`. Unpacks it one level below `plugins/` or `firmware/`, with protection against `..`, absolute paths, links and oversized entries. An existing file is never overwritten.

The inventory is never touched automatically. A check keeps one archive per asset: the previous one is dropped after the next one was installed.

## The Paperboy version

`checkPaperboyRelease` names a newer Paperboy without installing it ([ADR 0101](../adr/0101-a-hint-about-a-newer-paperboy.md)). It reads the `server-vX.Y.Z` train and compares the newest release with the version that was stamped into the running binary.

- The channel follows the running version: a final version hears about finals, a prerelease also about prereleases.
- A development build and a source without a Paperboy release stay silent.
- The dashboard shows `2.14.0 is available` with the version as the link, but only for the public GitHub host.
- Both release train pages have a `Paperboy` row with the state of the comparison.
- The journal gets one line per version.

## Versions and names

`server/internal/semver` parses the versions in program and image names: `1.4`, `1.4.0`, `1.4.0-rc.1`, `1.4.0-beta.3`. Missing parts count as zero, so `1.4` equals `1.4.0`. A final version ranks above every suffix, numeric identifiers compare as numbers, the rest lexically. The package is called `semver` because `paperboy` already has a `version` variable for the stamped build version.

## Pruning

The maintenance page lists what *Preview* would delete, and the button deletes exactly that ([ADR 0090](../adr/0090-download-cache-and-inventory.md), [0154](../adr/0154-split-the-release-trains-and-add-maintenance.md)). It also removes the state of devices without a device file and the editor backups of deleted objects. `prune.go` keeps:

- the resolved program of every plugin,
- every version that a pin names,
- what a device reports or is offered as firmware,
- names without a version,
- the newest two versions per plugin and hardware type.

## Decisions

[ADR 0056](../adr/0056-repository-layout-and-release-trains.md) (release trains), [0147](../adr/0147-separate-server-plugin-and-firmware-releases.md) (separate releases), [0148](../adr/0148-the-container-image-is-the-primary-server-artifact.md) (the image is primary), [0150](../adr/0150-start-a-release-from-the-actions-ui.md) (releases from the UI) · [0059](../adr/0059-firmware-bundle.md), [0061](../adr/0061-install-every-bundle-image.md), [0063](../adr/0063-constant-firmware-bundle-name.md) (the firmware bundle) · [0060](../adr/0060-download-from-releases.md), [0062](../adr/0062-plugin-updates-and-download-control.md) (downloads and intervals) · [0075](../adr/0075-plugin-versions.md), [0107](../adr/0107-plugin-builds-reach-installations.md) (a version per plugin) · [0089](../adr/0089-release-channels.md) (channels and prereleases) · [0090](../adr/0090-download-cache-and-inventory.md) (cache and inventory) · [0101](../adr/0101-a-hint-about-a-newer-paperboy.md) (a hint about a newer Paperboy)
