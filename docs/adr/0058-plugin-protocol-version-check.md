# 0058: Check the plugin protocol version at runtime

- Status: Accepted
- Date: 2026-11-07
- Builds on: ADR 0049 (plugins), ADR 0050 (protocol version 3), ADR 0055 (additive fields keep the version)
- Implements: the prerequisite of ADR 0056

## Context

Paperboy sends `Request.Version` (currently 3) with every request, but nothing
compared it with the protocol a plugin actually speaks. A plugin answered
whatever it could and a mismatch showed up as garbage in a cell, a wrong size
or a missing line, not as a clear error. Inside one repository `go test ./...`
catches this; a plugin zip from a release, a plugin left over from an update or
a third-party plugin does not get that check (issue #57).

## Decision

- `plugin.Message` gets an optional `version`. The Go helper fills it in
  `Handle` for every answer, including error answers and the answers to an
  unparsable request, and in the `changed` event of `ServeIO`. A plugin built
  against `pkg/plugin` reports the version it was compiled with; nothing has to
  be set by hand.
- `Message.CheckVersion` compares it with `plugin.Version`. Paperboy calls it
  in `internal/widget` on every answer, so a mismatch fails the request as a
  whole: the plugin's cells show the error, the plugin is dropped, and the
  host's usual backoff keeps it from being restarted in a loop. The first
  mismatch per process is logged as `plugin protocol mismatch` with the plugin
  name.
- A message **without** a version is refused, not accepted with a warning. It
  comes from a plugin that was not built with the current `pkg/plugin`, which
  is exactly the case that must not pass silently: the error reads "plugin
  reports no protocol version; rebuild it against protocol version 3". Every
  shipped plugin uses the helper, so this only hits stale binaries and
  plugins written by hand in another language.
- The field is additive, so `Version` stays 3 (same reasoning as ADR 0055). An
  older Paperboy ignores the field; an older plugin omits it and is refused by
  a newer Paperboy, which is the intended outcome of a protocol change.

## Consequences

- Updating Paperboy can make an old plugin fail visibly, with the version in
  the message and one log line, instead of drawing something wrong.
- A protocol bump therefore invalidates every older plugin at once; the host
  does not degrade them. There is no "older plugins keep rendering widgets"
  mode. Plugins ship in the same release as Paperboy (ADR 0056), so the remedy
  is to unpack the current plugin zip, and the error names the expected
  version. This applies to protocol version 4 of the action menu as well: a
  version 3 plugin is refused, not silently used without its actions.
- The plugin gallery (`/ui/plugins`) and the device API surface the error as
  the cell notice, like any other plugin failure.
- Plugin authors in other languages have to print `"version": 3` in every
  answer; the error message names the expected version.
- Independent plugin releases (`plugins-v*`, ADR 0056) become safe: a release
  built against a different protocol is rejected instead of half-working.
- Still open: a protocol version in `describe` alone would let Paperboy check a
  plugin before using it, but `configure` already passes through the same
  check, so no separate handshake is needed.
