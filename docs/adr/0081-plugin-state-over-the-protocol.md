# 0081: Plugin state and blobs over the protocol

- Status: Accepted, implemented
- Date: 2026-11-11
- Builds on: ADR 0049 (plugins), ADR 0065 (a state directory per plugin), ADR 0057 (plugin actions)
- Supersedes: ADR 0065

## Context

ADR 0065 handed every plugin a writable directory, `state/plugins/<plugin>/`, and promised that Paperboy would never touch what is inside. That made plugin state invisible and unconstrained: myskoda wrote a JSON file and a PNG, and nothing stopped a plugin from filling the disk or from writing a format that a later version of itself could no longer read. Paperboy could not back the files up meaningfully, could not show or clean them, and could not promise that a plugin survives a restart without a directory.

The plugin protocol already carries everything else between Paperboy and a plugin as JSON lines on stdin and stdout. State is small (a timestamp, a cache of the last answer); images and other files are the exception.

## Decision

- **The state is a field of the protocol, not a file the plugin writes.** Protocol version 5 adds `state` to every request (the stored JSON object, absent when there is none) and to every answer and event (absent = unchanged, `null` = forget it). It must be a JSON object of at most 256 KiB.
- **Blobs are the exception that stays base64.** `describe` declares them by name (`"blobs": ["render.png"]`); `configure` carries the stored ones, so a restart has them before the first request; every answer and event may write changed ones. Each blob is at most 1 MiB, a plugin keeps at most four (4 MiB). Names use `a-z 0-9 . _ -`, start with a letter or digit and are no longer than 64 characters, so they are safe file names. Undeclared, invalid or oversized blobs are logged and ignored, and they never fail the widget.
- **Paperboy owns the files.**
  - `state/plugins/<id>/state.json` — the plugin's state object, mode `0600`.
  - `state/plugins/<id>/blobs/<name>` — its declared blobs, mode `0600`.

  Both are written atomically after the answer that carried them, outside any request. Nothing is deleted automatically: a file a plugin no longer declares stays until the operator removes it. Deleting `state/plugins/<id>/` resets the plugin, exactly like forgetting device state (ADR 0080).
- **No writable directory any more.** `PAPERBOY_STATE_DIR` and `pkg/plugin.StateDir()` are gone, so a plugin has no path it may write to and the state is always JSON (plus declared blobs). Paperboy stores state and blobs from any line the plugin writes, so a state-only change can be flushed with a line that carries nothing else, e.g. `{"state": {…}}`, without touching the widget cache.
- **Go plugins keep their code shape.** The handler reads `Request.State` and `Request.Blobs` in `Configure`, and implements `State() json.RawMessage` and `Blobs() map[string][]byte` (`pkg/plugin.Stater`, `Blober`). `Handle` and `ServeIO` attach both to every answer and event, so no plugin marshals the envelope itself. `Blobs` returns only the files that changed and clears its marks.
- **myskoda is the first user**: its vehicle state stays the same JSON, and the render image becomes the `render.png` blob instead of a file it writes itself.

## Alternatives

- **Keep the writable directory and document its format.** Cheapest, but the state stays unreadable for Paperboy, unbounded and impossible to back up with confidence; every plugin invents its own layout.
- **A read-only path plus a protocol write.** The plugin could read its files without a round trip, but Paperboy would still not know the format, and a cache could be re-read and rewritten behind its back.
- **Blobs as paths rather than base64.** Would need a writable directory again and would let a plugin point at arbitrary files.
- **Sending blobs with every request.** Simpler rule, but a 300 KiB image would ride along with every widget request; the state alone is small enough for that.

## Consequences

- Every plugin needs a rebuild: protocol 5 refuses the old one with the usual mismatch error (ADR 0058). None are third-party yet, and the shipped ones are built from this repository.
- A restart after a configuration change hands state and blobs back through `configure`; the request line is the largest the protocol carries, which is why blobs are capped at 1 MiB.
- The state is now a file an operator can read, back up and delete, and the blobs sit next to it under a name that says what they are.
