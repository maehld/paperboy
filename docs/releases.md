[← Paperboy](../README.md)

# Releases and downloads

## Releases

GitHub releases are the distribution point (ADR 0056). Each tag names a tested combination of the three deliverables:

| Tag | Contains |
|---|---|
| `vX.Y.Z` | Paperboy binaries, plugin zips and the firmware bundle |
| `plugins-vX.Y.Z` | plugin zips only |
| `firmware-vX.Y.Z` | firmware bundle only (`X.Y.Z` is the highest `fw_version` in the bundle) |

The tag names the release, not what is inside. Every plugin and every firmware image carries its own version, and that version changes only with a change (ADR 0043, 0075).

| Asset | Use |
|---|---|
| `paperboy-linux-amd64`, `paperboy-linux-arm64`, `paperboy-windows-amd64.exe` | the Paperboy binary for that platform |
| `paperboy-plugins-<os>-<arch>.zip` | unpack in the instance directory: creates `plugins/` with `<plugin>-v<version>` of every shipped plugin |
| `paperboy-firmware.zip` | unpack in the instance directory: creates `firmware/` with `<hardware>-v<version>.bin` of every hardware type (ADR 0043, 0059, 0060, 0063) |
| `SHA256SUMS` | checksums of all assets of the release |

The container image is published separately as `ghcr.io/maehld/paperboy` (`X.Y.Z`, `X.Y` and `latest`). To create a release, push a tag: `git tag v2.1.0 && git push origin v2.1.0`. The *Release* workflow builds all assets and attaches them to one release. A tag with a suffix (`v1.4.0-rc.1`, `plugins-v1.4.0-rc.1`) becomes a prerelease; only the `latest` channel installs it (ADR 0089, see [Downloads](#downloads)).

## Downloads

A `download` section makes Paperboy fetch plugin programs and firmware images from the releases (ADR 0060). Paperboy verifies every file against the `SHA256SUMS` of the release.

```yaml
download:
  source: https://github.com/maehld/paperboy  # optional, this is the default
  token: ${GITHUB_TOKEN}                     # optional: rate limits, private forks
  interval: 24h                              # optional: between checks, at least 1h
  plugin_channel: stable                     # optional: stable (default) or latest (ADR 0089)
  firmware_channel: stable                   # optional: stable or latest, per train
```

A bare `download:` key is enough. It uses the official source and checks every 24 hours. Without the section, the checks stay off.

- A check runs at startup and then after `interval` (default 24 h) in the background. The *Downloads* page (`/ui/downloads`) has a **Check now** button that starts a check at once. An instance without internet keeps working.
- **Plugins**: a configured plugin without a program gets the newest one as `plugins/<plugin>-v<version>` (`weather-v1.4.0`, `weather-v1.4.0.exe` on Windows). Paperboy updates a downloaded program when the plugin zip has a higher version of that plugin. An unchanged plugin stays as it is, however many releases pass (ADR 0075). The old file stays, and only that plugin restarts. A `version` pin names a plugin version (`v1.4.0`) and never moves.
- **Firmware**: every image of the bundle lands in `firmware/<hardware>-v<version>.bin` (ADR 0061). A `firmware.version` pin selects the release with exactly that image; this is also how you pin a rollback. Paperboy offers an update only with `firmware.update: auto`.
- **Channels** (ADR 0089): `stable` (default) installs only releases that GitHub does not mark as a prerelease. It also refuses a version with a suffix inside an unflagged release. `latest` installs prereleases too. A version may carry a suffix (`weather-v1.4.0-rc.1`, `e1001-v1.4.0-rc.1.bin`), so an rc and the final are two files, and a pin can name either. A channel never downgrades: after a switch from `latest` to `stable`, the newer program stays; use a pin for a rollback. A pin bypasses the channel and can select an rc from a prerelease.
- A program **without** a version in `plugins/` (`weather`) always wins. It is your build. Paperboy neither replaces nor pins it. Download such a plugin by hand, or delete the program to give the plugin back to the releases.
- Programs and images are never overwritten, so they pile up. The *Downloads* page shows the size of both inventories and a **Preview cleanup**. The preview lists what a cleanup would delete. It keeps the version in use and every version a pin names. It also keeps the firmware a device reports or is offered, files without a version, and the newest two versions of every plugin and hardware type (ADR 0090). If you delete a newer plugin file, Paperboy rolls back to the older one for the rest of the run. A `version` pin keeps the older one across restarts, and a firmware pin keeps the older image on offer.
- Paperboy cleans the download cache under `state/download/`. It keeps one archive per train and asset, and drops the previous one after the next is verified and installed. The learned entry lists stay, so a pin costs no download.
- The *Downloads* page shows the source, the interval, the last check and the running check. It shows the program and version behind every plugin, with the time each program and image arrived. It also shows the images in `firmware/`, the ignored files, the channel of each train and notes when a running version is not one the channel would deliver. The dashboard warns only when the last check failed.
