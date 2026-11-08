# 0060: Downloading plugins and firmware from releases

- Status: Accepted
- Date: 2026-11-08
- Builds on: ADR 0026 (configuration), 0037 (instance directory), 0040 (dashboard), 0043 (firmware distribution), 0047 (config reload), 0049 (plugin widgets), 0055 (widget gallery), 0056 (release trains), 0058 (protocol check), 0059 (firmware bundle)
- Amends: ADR 0049 (a plugin's program is no longer configured with `command`); ADR 0037 and 0043 (Paperboy writes into `plugins/` and `firmware/`); ADR 0043 §1 and 0059 §1 (flat firmware directory, version with `v` in the image name)
- Answers: issue #64

## Context

Updating an instance is handwork: download `paperboy-plugins-<os>-<arch>.zip` and `paperboy-firmware-<version>.zip` from the release page, unpack both in the instance directory, restart Paperboy. ADR 0056 §4 and §6 already noted that a download feature needs nothing but the release API and these names: the names carry the platform and the hardware, `SHA256SUMS` carries the checksums, and the tags separate the trains.

Paperboy knows which plugins are configured, which hardware types its devices use, which versions already lie in the instance and what version it runs itself. Two properties of the current layout make the feature small: the release archives unpack into exactly the directories Paperboy reads, and a plugin is a separate long-running process that can be restarted on its own (ADR 0049).

Two things are in the way. A plugin program is configured with `command` (ADR 0049), so its name says nothing about its version and nothing tells Paperboy when a new one arrived. And `plugins/` and `firmware/` belong to the operator: Paperboy reads them and never writes them (ADR 0037, 0043).

## Decision

### 1. Opt in with two keys

```yaml
download:
  source: https://github.com/maehld/paperboy   # optional, default the official repository
  token: ${GITHUB_TOKEN}                       # optional: rate limits, private forks
```

The section switches the feature on. Without it no request leaves the machine and existing instances stay as they are. `source` names the release origin (a fork, a mirror), `token` raises the rate limit or opens a private repository; `${VAR}` is expanded as everywhere else (ADR 0026). There is no separate switch per deliverable: the section is the operator's decision that this instance is fed from releases.

Versions are pinned where the deliverable is configured, not in `download`:

```yaml
plugins:
  weather:
    version: v2.1.0
    config: { lat: 51.96, lon: 7.63 }
  calendar:

firmware:
  update: auto
  version: v1.0.0
```

A pin is exact and written as the version appears in the file name (§2, §3). Without one the newest release wins.

### 2. The program is named like the plugin

`plugins: weather:` means the program `weather`. `command` disappears from the configuration: one name per plugin, no argv, no alias. A downloaded program is named `<plugin>-v<version>` — `weather-v2.1.0`, `weather-v2.1.0.exe` on Windows — with the version being the release tag without the train prefix (`v2.1.0` and `plugins-v2.2.0` give `v2.1.0` and `v2.2.0`).

Lookup for the plugin `weather`:

1. `plugins/weather` **without a version always wins**: a program built or unpacked by hand, shown as `dev`. A pin next to it is ignored with one log line.
2. a pin selects exactly `plugins/weather-v<pin>`, downloaded when missing;
3. otherwise the highest `plugins/weather-v*`;
4. otherwise `weather` on `PATH`.

A plugin that resolves through 1 or 4 is not downloaded at all. Only programs the plugin zip ships under a configured name are considered, so custom plugins stay untouched. The built-in `paperboy` needs no program.

The unversioned file is thereby the operator's statement "this is my build, leave it alone"; deleting it hands the plugin back to the release.

### 3. The firmware directory is flat too

`plugins/` and `firmware/` get the same shape: one directory, files named `<thing>-v<version>`.

```
firmware/
  sticky-v1.0.0.bin
  e1001-v1.0.0.bin
  lcd2004-v1.0.0.bin
```

- The hardware was already part of the file name (ADR 0043), so `firmware/<hardware>/` repeated it. A missing subdirectory also means an operator drops an image into one place instead of remembering which folder it belongs to.
- The version keeps the `v` of the tag, like the plugin programs. The image itself reports `fw_version` without it, so Paperboy strips the `v` for comparison and keeps the plain number in the offer (`{"version": "1.0.0", …}`), because that is what a device reports.
- The bundle (ADR 0059) unpacks the same way, `firmware/sticky-v1.0.0.bin` and so on, so unpacking it in the instance directory still needs no second step. The bundle name keeps the release version, the image names keep `fw_version`.
- Images are served at `/firmware/<file>`. The sync offer carries that path and the firmware downloads whatever it is handed, so devices need no change.
- The reader also accepts the old `firmware/<hardware>/<hardware>-<version>.bin` layout and logs one hint; on the same version the flat file wins. An instance keeps working after the update without a migration step.
- A firmware pin names the version as it appears in the file name (`v1.0.0`) or as the device reports it (`1.0.0`); both are normalized.

### 4. Which release

- **Latest** is the newest release that carries the asset a deliverable needs: `paperboy-plugins-<os>-<arch>.zip` for `runtime.GOOS`/`GOARCH`, `paperboy-firmware-<version>.zip` for the firmware. `plugins-v*` and `firmware-v*` releases compete with full `v*` releases (ADR 0056).
- A **pinned plugin** needs no listing: the asset URL follows from the tag, `<source>/releases/download/<tag>/paperboy-plugins-<os>-<arch>.zip`.
- A **pinned firmware version** is the image version inside a bundle, so Paperboy walks the releases down until a bundle contains `firmware/<hardware>-v<version>.bin` for the configured hardware types. The image versions of a bundle follow from its file names; they are read once per release and remembered in `state/download.json`.
- The release list comes from `https://api.github.com/repos/<owner>/<repo>/releases` and is cached with its ETag in `state/download.json`, so a check that finds nothing new costs one conditional request a day. `Authorization: Bearer <token>` is sent when configured.

### 5. Installing

- Download to a temporary file, compare its SHA-256 with the release's `SHA256SUMS`, unzip into a temporary directory below `plugins/` or `firmware/` (rejecting `..`, absolute paths, symlinks and oversized entries), then move the single files to their names.
- Only the images of hardware types that appear in `display_types` are installed; the bundle carries all of them and the rest stays in the zip.
- No existing file is overwritten: a new version is a new name. A running plugin keeps its program (Windows locks it anyway), so a download can complete while a page renders.
- Nothing is deleted. Paperboy cannot tell a downloaded file from one placed by hand, and old images are the rollback path of ADR 0043; plugin versions behave the same.

### 6. When it runs, what reacts

- Once at startup, in the background and without blocking the listener, and then once a day.
- A new plugin file changes the resolved command, so the host's spec differs and exactly that plugin restarts with an empty cache (ADR 0049). A pin changed in a configuration reload (ADR 0047) does the same.
- Firmware needs nothing beyond the offer that already exists in the sync (ADR 0043): the image is there, the next sync offers it when `firmware.update` is `auto` and the device may update.
- Failures log one line and wait for the next daily check; a broken release does not take the instance down.

### 7. Pinning is the rollback switch

- A plugin pin uses exactly that program; other versions stay on disk unused.
- A firmware pin makes the offer use the images of that version even when a higher image lies in `firmware/`. A device on a newer image is offered the older one, which ADR 0043 already treats as a deliberate rollback.

### 8. Visibility

- The dashboard (ADR 0040) gets a *Downloads* row: source, last successful check, the version in use per plugin and per hardware type, and the last error.
- `GET /api/plugins` and the widget gallery (ADR 0055) name the program and its version behind each plugin.
- One log line per check that changes something, one per failure.

## Consequences

- Updating an instance becomes "restart Paperboy and let it fetch": plugins arrive for the next render, firmware images are there for the next sync, and a device flashes what `firmware/` offers.
- **Paperboy writes into `plugins/` and `firmware/`.** ADR 0037 and 0043 said it never creates or writes there; that changes for these two directories only. Pages, plans, configuration and the operator's own builds stay untouched.
- **`command` disappears.** A plugin has to be a program named like the plugin; wrappers such as `command: [python, thing.py]`, aliases and programs with another name are no longer configurable. A leftover `command:` is rejected with a hint, like the other v1 leftovers.
- **The firmware layout changes.** `firmware/<hardware>/<hardware>-<version>.bin` becomes `firmware/<hardware>-v<version>.bin`, the bundle and `build.ps1 -Instance` write it that way, images are served at `/firmware/<file>`, and the offer path follows. The old layout is still read, so nothing breaks on an update; the dashboard can point out leftovers. ADR 0043 §1 and 0059 §1 keep their history and are amended here, not rewritten.
- A plugin version is only a file name. Nothing verifies that `weather-v2.1.0` is version 2.1.0 — the firmware has the same weakness (ADR 0043), though there the device itself reports `fw_version` after the flash and a wrong file name shows as a failed offer.
- `latest` can pair a newer plugin with an older Paperboy. ADR 0058's protocol check turns that into one clear log line instead of odd widget errors, and a pin avoids the question.
- Checksums prove an intact transfer, not who built the file; authenticity rests on TLS and the LAN, as for the firmware MD5 (ADR 0043).
- Disk usage grows with every downloaded version in directories the operator owns, and nothing prunes it.
- New work: the `download` section, `version` on plugins and firmware, the versioned lookup, a download package (release lookup, asset selection, checksum, safe unzip, install), the daily loop, the dashboard row, the removal of `command` from configuration, examples and documentation, and the flat firmware directory in reader, handler, `firmware.yml` and `build.ps1`.
- Still open: a schedule in the configuration, pruning old versions, a plugin protocol version, and updating the Paperboy binary itself.
