# 0107: Plugin builds reach installations

- Status: Accepted
- Date: 2026-11-15
- Related: ADR 0075 (plugin versions), ADR 0081 (the protocol grows additively), ADR 0105 (sixteen gray levels), issue #243

## Context

ADR 0105 added `levels: 16` to the `render` action and kept the protocol version, because the field already existed and a new value looked additive. Every plugin binary, however, validates the values it knows: a plugin built before ADR 0105 answers `levels 16: expected 2 or 4` and its cells show that notice. The downloader installs a plugin program only when a release names a higher version, so a rebuilt plugin with an unchanged version never reaches an installation.

Release v3.19.0 raised `calendar` (1.6.1), `evcc` (1.4.1) and `dexcom` (1.2.1), but not `weather`, `countdown`, `departures`, `myskoda`, `pokedex`, `rss` and `xkcd`. Their installed binaries stayed the old ones, and exactly those plugins failed on the first 16-level display. The version rule only covered changes under `cmd/<plugin>`; the *Plugins* workflow merely warned for `pkg/`, although `pkg/plugin` and `pkg/render` are compiled into every plugin.

## Decision

1. **A change to `pkg/plugin` or `pkg/render` raises every plugin version.** Both packages are linked into every plugin binary, so `scripts/check-plugin-versions.sh` fails a pull request that touches one of them without raising each `cmd/<plugin>/VERSION`. Other shared code under `pkg/`, `go.mod` and `go.sum` keep a warning, because which plugins they affect is a judgement.
2. **The protocol accepts any tone count from 1 to 256.** A `render` request no longer has to name 2, 4 or 16: `render.NewCanvas` falls back to the next supported palette (2, 4 or 16) and Paperboy quantizes the answer to the display's palette. A future display type with another tone count must not fail an installed plugin; it draws with the nearest palette instead.
3. **A pin stays the operator's.** A `version:` pin in `paperboy.yaml` keeps the pinned program; after a release raises a version, the pin has to be raised by hand to receive the new build.

## Alternatives

- **Replace an installed program whose version is unchanged when the release bytes differ:** a version has to name fixed bytes; the installed name would silently change its meaning, and the downloader would need per-file hashes from inside the zip.
- **Raise the minimum protocol version:** would refuse every older plugin on every display, although only one value of one field is unknown.
- **Ask `describe` for the supported tone counts:** another field and a negotiation for something that clamping already answers.
- **Keep the strict `levels` check and rely on version bumps alone:** an installation that misses the rebuilds still fails, and every future value would repeat the incident.

## Consequences

- Shared plugin code reaches installations with the next release, and CI refuses a pull request that would leave them behind.
- The seven plugins that missed the v3.19.0 rebuild get a patch version, so the next release installs them for 16-level displays.
- An installed plugin binary from before ADR 0105 still answers its error until it is replaced; the tolerant check protects future builds, not the installed ones.
- A pinned plugin version keeps its old program until the operator raises the pin.
