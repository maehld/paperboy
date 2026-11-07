# 0059: One firmware bundle per release

- Status: Accepted
- Date: 2026-11-07
- Builds on: ADR 0043 (firmware distribution), ADR 0056 (release trains)
- Amends: ADR 0056 §4 (assets)

## Context

ADR 0056 attaches the firmware to a release as one zip and one loose `.bin` per
hardware type. With `sticky`, `e1001` and `lcd2004` that is six assets per
release, three of them zips that each contain a single directory. A user has to
pick the right three of them, and the paperboy asset names carry the hardware
while the plugin zip right next to it carries only the platform.

The firmware versions have also grown apart by accident: `0.5.0` for Sticky and
E1001, `0.3.0` for the text display, although all three share the same cycle in
`common.yaml`/`text-common.yaml`. Numbers that follow the hardware history say
nothing about the state of the firmware.

## Decision

### 1. One bundle

A release carries exactly one firmware asset, built like the plugin zip:

```
paperboy-firmware-<version>.zip
  firmware/sticky/sticky-1.0.0.bin
  firmware/e1001/e1001-1.0.0.bin
  firmware/lcd2004/lcd2004-1.0.0.bin
```

The zip root is `firmware/`, so unpacking it in the instance directory produces
the layout Paperboy reads (ADR 0043) without a second step. The per-hardware
zips and the loose `.bin` assets disappear; whoever needs a single image unzips
the bundle.

`firmware.yml` keeps compiling per hardware in a matrix and uploads the images
as internal artifacts (`firmware-image-<hardware>`). A second job collects them
and writes the bundle; `release.yml` loads only `paperboy-*` artifacts, so the
images never become release assets.

### 2. Two versions

- **The bundle is named after the release.** `v2.1.0` produces
  `paperboy-firmware-2.1.0.zip`; a `firmware-v*` tag keeps the convention from
  ADR 0056, who names the bundle after the highest `fw_version` in it.
- **The images keep their own `fw_version`** and their own file name, because
  Paperboy derives the offered version from the file name (ADR 0043) and a
  device reports what it runs. The two versions may differ, and they say
  different things: the tag names a tested combination of deliverables, the
  image name names the firmware build.
- All three hardware types start at `1.0.0` so that the shared cycle is the
  number, not the age of a board. They stay in step: a change to a shared file
  raises the version for all three types.

## Consequences

- One file to download and unpack, matching the plugin zips; the release page
  shrinks to the Paperboy binaries, two plugin zips, one firmware bundle and
  `SHA256SUMS`.
- Unchanged images keep their version, so a release that does not touch the
  firmware does not make devices flash: the offer compares the running
  `fw_version` with the highest image, not with the tag.
- A change to a single hardware type republishes all three images in the
  bundle, since they share one file. The rebuild is cheap compared to the
  matrix that compiles them anyway.
- The version of an image no longer says how old the board port is; for
  `lcd2004` this hides that it is the youngest port. `fw_version` is now a
  statement about the firmware as a whole, and a hardware-specific fix raises
  the version of all three images at once.
- Still open: an image cannot be downloaded alone without unzipping. If a
  download feature in Paperboy needs single images, the bundle is still the
  release asset and such a feature unzips in place.
