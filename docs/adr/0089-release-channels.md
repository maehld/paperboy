# 0089: Release channels and prerelease versions

- Status: Accepted, implemented
- Date: 2026-11-12
- Builds on: ADR 0043 (firmware distribution), 0056 (release trains), 0060 (downloads), 0062 (plugin updates and download control), 0075 (a version per plugin)
- Amends: ADR 0060 §1 (the newest release is the newest of the chosen channel), ADR 0062 §1 (newer within that channel), ADR 0075 §2 (a program's name may carry a prerelease suffix)
- Answers: issue #162

## Context

A release check installs the newest release that carries the wanted asset, and `download.List` drops drafts and prereleases. There has always been exactly one channel: what has been released.

Two needs follow from that:

- a new plugin or firmware build has to be tried on the instance in front of the developer before every other instance gets it;
- the other instances should not receive a build that is not yet declared good, just because it was released.

Three shapes were considered:

- **mutable git tags** (`stable`, `latest`): they break the version stamping. Paperboy's version comes from the tag (ADR 0056) and the plugin and image names carry their versions, so a moving tag would either report "stable" as a version or contradict the file names.
- **a manifest asset** (`channels.json` with promote/demote, `min_paperboy`): the most powerful, and the only shape that can withdraw a release without publishing another one. It needs a stable pointer and a maintainer's discipline and stays an option for later (separate repositories, mirrors).
- **the prerelease flag**: GitHub already answers it, `download.List` already parses it, and setting it is one click after publishing. That is the channel.

A second question is what a prerelease is *called*. If the flag alone marks it, the rc delivery and the final delivery carry the same program name: `plugins/<plugin>-v1.5.0` would name two different artifacts, and there would be no way back from the final to the rc — no pin, no file to select. The version itself therefore carries the suffix.

## Decision

### 1. A version may carry a prerelease suffix

`1.4.0`, `1.4.0-rc.1`, `1.4.0-rc.2`, `1.4.0-beta.3`. `internal/semver` parses and compares them by SemVer precedence: the numeric parts first, then the suffix, where a version without one is the highest, numeric segments compare as numbers and the others lexically, and missing segments count as zero. The names keep their shape: `plugins/<plugin>-v<version>` and `firmware/<hardware>-v<version>.bin` (ADR 0075, 0043).

`versionParts`, `compareVersions` and `pinVersion` (all `[]int`) are replaced by that type, and `pluginVersionNewer` and the firmware store use it. A version that does not parse is logged; today `pluginVersionNewer` answers "not newer" for an unparsable candidate, so an rc-capable version would silently never install.

A pin matches exactly: `version: 1.4.0` does not select `1.4.0-rc.1`, and `version: 1.4.0-rc.1` selects the rc. `sameVersion` between the `fw_version` a device reports and an installed image keeps that exactness, so a device running `1.4.0` is not considered up to date against an image `1.4.0-rc.1`.

### 2. Two channels under `download`

```yaml
download:
  plugin_channel: stable     # stable | latest
  firmware_channel: stable   # stable | latest
```

Both default to `stable`, and either key may appear alone, like the section itself (ADR 0060). `stable` considers releases that are neither drafts nor flagged prereleases, `latest` considers every release. Belt and braces: a release that is not flagged but whose zip or bundle carries a version with a prerelease suffix is skipped by `stable` as well, so a forgotten flag cannot smuggle an rc into `stable`.

### 3. A channel decides eligibility, it does not downgrade and does not roll back

Inside the eligible releases nothing changes: the newest release carrying the asset wins, and a program is installed only when its version is higher (ADR 0062 §1). An instance that ran `latest` and returns to `stable` keeps the higher rc program. Rolling back is a pin (`version: 1.4.0`), which wins over the highest installed version and leaves the rc file in place.

A pin names a version, not a delivery: it bypasses the channel and may pick a version out of a prerelease release although the channel is `stable`. That is intentional — the pin is the operator's explicit instruction — and it keeps a channel switch from making an existing pin unsatisfiable ("no release carries v1.4.0").

`firmware: hold` on a device keeps its meaning (ADR 0043): it withholds the offer, not the download.

### 4. The release list keeps prereleases

`Release` gains `prerelease`; `List` no longer filters them out and asks for `per_page=100`, so a train with many prereleases cannot push the newest stable release out of the window. The channel filter lives at the two call sites, next to the configuration. The list is cached with its ETag (ADR 0060 §4), so switching a channel takes effect on the next check without an API round trip.

### 5. What the operator sees

- The releases page names the channel of each train and says so when the installed version is not one the channel would deliver ("1.4.0-rc.1 installed, not in channel stable"). Without that line the question "why does this not move?" ends in the log.
- A configured plugin that is missing from the newest eligible release but present in a prerelease is logged as such instead of "plugin not in the release zip".

### 6. A tag with a prerelease suffix is published as a prerelease

`release.yml` passes `--prerelease` when the tag carries the suffix, for every train (`v1.4.0-rc.1`, `plugins-v1.4.0-rc.1`, `firmware-v1.4.0-rc.1`). Nothing else changes: a release tag still only says what a release contains (ADR 0056), and the versions inside come from `cmd/<plugin>/VERSION` and `fw_version` (ADR 0075, 0043). Raising them stays mandatory, so a fixed rc produces `1.4.0-rc.2` or `1.4.1`, never a second `1.4.0`.

## Alternatives

- **Only the flag, no suffix in the version.** Cheaper — no version grammar change — but the same program name would name two artifacts and an rc could never be pinned again after the final exists. Rejected.
- **Other channel names** (`beta`, `edge`, `next`): `latest` already is the channel that takes everything, and a finer split is a pin per plugin. Rejected until someone needs a third.
- **A channel per plugin** in `paperboy.yaml`: the pin covers the case of one plugin on one instance. If it turns out to be needed, `channel` next to `version` is the obvious place. Deferred.
- **`channels.json` in the release.** See Context; deferred.

## Consequences

- During an rc cycle two files of one plugin coexist (`<plugin>-v1.4.0-rc.1`, `<plugin>-v1.4.0`). The inventory grows, and pruning it stays a manual action (ADR 0090).
- A device whose firmware reports `1.4.0` is never equal to an image `1.4.0-rc.1`. Intended, but it means the offer stays until the final is installed — and a withdrawn rc stays on the devices that already run it, because nothing downgrades.
- `latest` on firmware is a real risk next to a panel in the kitchen. The guards are the per-device `firmware: hold`, the battery threshold and the three-attempt limit (ADR 0043); an instance should choose `latest` there deliberately.
- Releases built before this decision carry numeric-only versions and parse as before. A pin written for them keeps working.
- `describe.version` carries the suffix, and the widget host compares it with the name of the resolved program (ADR 0075 §4), so both have to be raised together.
- Still open: a manifest with `min_paperboy` and promote/demote — without it, `latest` can hand a plugin to a Paperboy that is older than the plugin expects. The plugin protocol accepts newer versions additively (ADR 0081), so this only hurts when configuration keys or action fields change.
- Separate repositories per train (ADR 0056) would make the channel a per-repository question. The cache layout prepared for that is in ADR 0090.
