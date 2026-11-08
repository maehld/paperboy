# 0075: A version per plugin

- Status: Accepted
- Date: 2026-11-08
- Builds on: ADR 0043 (firmware distribution), 0049 (plugin widgets), 0056 (release trains), 0058 (protocol check), 0060 (downloads), 0062 (plugin updates)
- Amends: ADR 0060 §2 (a pin names a plugin version, and the program's name carries the plugin's own version), ADR 0062 §1 (newer means newer by plugin version)
- Answers: issue #115

## Context

A firmware image carries its own version: `fw_version` in `firmware/esphome/<hardware>.yaml`, raised only when that hardware's firmware changed, and the image inside the bundle is named after it (ADR 0043, 0059). The plugins had no version of their own.

Two problems followed from that:

- `download.go` derived the installed name from the release tag (`<plugin>-v<release version>`). A release that only touched one plugin therefore installed a new file for every plugin, so `weather-v2.3.0` named a program that had not changed since `weather-v2.1.0`; the version said nothing about the program.
- `scripts/package-plugins.sh` built `plugins/<plugin>` without a version. Unpacked by hand, such a program wins over every download (ADR 0060 §2) and is never updated, and a difference between two builds of one plugin was invisible.

ADR 0060 already admitted the weakness: a plugin version is only a file name, and nothing verifies it.

## Decision

### 1. `cmd/<plugin>/VERSION` is the version, raised by hand when the plugin changes

```
cmd/weather/VERSION      1.4.0
```

The file is embedded into the program (`//go:embed`), so a local `go build` and `go run` carry it too. Raising it is the same manual step as `fw_version`: whoever changes a plugin raises its version in the same commit. Starting value for every shipped plugin is `1.0.0`.

### 2. The program is named after the plugin's version

`scripts/package-plugins.sh` reads `cmd/<plugin>/VERSION` and builds `plugins/<plugin>-v<version>[.exe]`. A plugin that did not change keeps its name across releases, so unpacking a newer zip over `plugins/` leaves it alone and adds only the changed programs under new names.

### 3. A download compares versions per plugin

- `download.Client.PluginZip` lists the entries of a release's plugin zip and answers plugin → version; the entry names are cached per release tag in `state/download/cache.json`, like the firmware bundle (ADR 0060 §4).
- Missing plugins are installed with the version the newest zip names them with.
- A downloaded program is replaced only when the zip ships a **higher** version of that plugin. An unchanged plugin in a newer release installs nothing.
- A `version:` pin names a plugin version (`v1.4.0`), no longer a release tag: Paperboy walks the releases down until a plugin zip carries exactly that version. The pinned file is `plugins/<plugin>-v1.4.0`.
- Everything else keeps ADR 0060 §2 and ADR 0062 §1: an unversioned program wins and is left alone, nothing is overwritten or deleted, a deleted version is not reinstalled in the same run, and a new file restarts exactly that plugin.

### 4. `describe` reports the version

`plugin.Description` gains `version`. All shipped plugins fill it from their `VERSION` file. The widget host compares it with the version in the file name of the resolved program and logs one warning per plugin when they differ — the check ADR 0060 §Consequences was missing. An optional field, so the protocol version stays 4.

### 5. The release tag no longer names a plugin version

`vX.Y.Z` and `plugins-vX.Y.Z` still select what a release contains (ADR 0056); a `plugins-v*` release is a build of the current plugin state. It says nothing about the version of any plugin inside.

### 6. A pull request has to raise the version of a changed plugin

`scripts/check-plugin-versions.sh` compares `cmd/<plugin>` against the base branch. A changed plugin with an unchanged `VERSION` fails the *Plugins* workflow; shared code under `pkg/`, `go.mod` or `go.sum` only warns, because which plugins need a new version is the maintainer's decision.

## Consequences

- Only a changed plugin gets a new program in a release. A `plugins-v*` release with one raised version contains one new file next to five unchanged ones.
- **Instances that unpacked an older zip keep an unversioned `plugins/<plugin>`.** That file still wins (ADR 0060 §2) and is never updated; it has to be deleted once so that the download takes over. The README says so, and the release notes should repeat it.
- Releases built before this decision carry unversioned programs in their zip. A download finds no version to install there and logs "plugin not in the release zip" once per plugin; the first release built with versioned names fixes that.
- A local build from `scripts/build.ps1` stays unversioned on purpose: the operator's build wins and a download leaves it alone.
- A change to `pkg/plugin` or `pkg/render` changes every plugin binary. The check only warns; raising the version of every affected plugin stays a manual decision.
- Pins written before this decision name a release tag (`v2.1.0` with no plugin version `2.1.0` behind it) and now fail with "no release carries v…". The fix is one line in `paperboy.yaml`.
- The firmware keeps its own scheme; nothing changes for `firmware/`.
- Still open: pruning old plugin versions, and a version reported by the built-in `paperboy` plugin (it runs in process and has no program name).
