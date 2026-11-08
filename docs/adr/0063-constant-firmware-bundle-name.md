# 0063: A constant name for the firmware bundle

- Status: Accepted
- Date: 2026-11-08
- Builds on: ADR 0056 (release trains), 0059 (firmware bundle), 0060 (downloads)
- Amends: ADR 0059 §1 and §2 (bundle name), ADR 0056 §4 (asset table), ADR 0060 §4 (asset selection)
- Answers: issue #75

## Context

The release assets used two naming schemes. The Paperboy binaries and the plugin
zips carry no version in their name; the firmware bundle did:
`paperboy-firmware-<version>.zip`. The version already sits on the release page
and in the tag, and the images inside keep their own `fw_version` (ADR 0059
§2). The only reason for the versioned name was ADR 0059 §1, and it made the
download code derive the name from the tag (`bundleName`).

## Decision

The bundle is named `paperboy-firmware.zip`, constant for every release, like
the other assets. The tag names the version; the images keep `fw_version`.

- `firmware.yml` stages and uploads the constant name; the `version` input that
  only chose the zip name disappears, and `release.yml` calls the workflow
  without it.
- `internal/download` matches `paperboy-firmware.zip` instead of a prefix, so no
  tag is involved in finding the bundle. A `firmware.version` pin still walks
  the releases and inspects the images inside their bundles (ADR 0060 §4).
- `firmware-v*` releases keep working: the bundle has the same name and the tag
  carries the version. Releases published before this decision keep their
  versioned bundle name; they remain history and are not rewritten.

## Consequences

- The asset table is uniform: binaries and zips without a version, `SHA256SUMS`
  last. The download code no longer parses a name back into a version.
- A pinned firmware version against a release older than this decision finds no
  bundle, because that release still uses the versioned name.
- ADR 0059 §1 and §2 and ADR 0060 §4 keep their history; this decision replaces
  their bundle-name sentences.
