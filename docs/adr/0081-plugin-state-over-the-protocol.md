# 0081: Plugin state and blobs over the protocol

- Status: Accepted, implemented
- Date: 2026-11-11
- Builds on: ADR 0049 (plugins), ADR 0065 (a state directory per plugin), ADR 0057 (plugin actions)
- Supersedes: ADR 0065
- Amends: ADR 0058 (a plugin is refused only below the minimum version; a newer one is accepted)

## Context

ADR 0065 handed every plugin a writable directory, `state/plugins/<plugin>/`, and promised that Paperboy would never touch what is inside. That made plugin state invisible and unconstrained: myskoda wrote a JSON file and a PNG, and nothing stopped a plugin from filling the disk or from writing a format that a later version of itself could no longer read. Paperboy could not back the files up meaningfully, could not show or clean them, and could not promise that a plugin survives a restart without a directory.

The plugin protocol already carries everything else between Paperboy and a plugin as JSON lines on stdin and stdout. State is small (a timestamp, a cache of the last answer); images and other files are the exception.


## Decision

- **State is a request of its own.** Protocol version 5 adds the action `state`, sent once right after a successful `configure`. Its request carries the stored JSON object (`state`, absent when there is none) and the stored blobs; its answer may return new ones. Every later answer and event may carry `state` and `blobs` as well. The state must be a JSON object of at most 256 KiB.
- **Why a separate request.** `configure` keeps its shape, the state and the blobs do not ride along with every widget request, and the load happens once with everything the plugin needs. It also leaves `configure` alone for the next protocol version.
- **Blobs are the exception that stays base64.** `describe` declares them by name (`"blobs": ["render.png"]`); the `state` request carries the stored ones, so a restart has them before the first widget request; every answer and event may write changed ones. Each blob is at most 1 MiB, a plugin keeps at most four (4 MiB). Names use `a-z 0-9 . _ -`, start with a letter or digit and are no longer than 64 characters, so they are safe file names. Undeclared, invalid or oversized blobs are logged and ignored, and they never fail the widget.
- **Paperboy owns the files.**
  - `state/plugins/<id>/state.json` — the plugin's state object, mode `0600`.
  - `state/plugins/<id>/blobs/<name>` — its declared blobs, mode `0600`.

  Both are written atomically after the line that carried them, outside any request. Nothing is deleted automatically: a file a plugin no longer declares stays until the operator removes it. Deleting `state/plugins/<id>/` resets the plugin, exactly like forgetting device state (ADR 0080). Paperboy stores state and blobs from any line the plugin writes, so a state-only change can be flushed with a line that carries nothing else, e.g. `{"state": {…}}`, without touching the widget cache.
- **Version 5 is the cut, and from here on the version gates the features.** Protocol 4, `PAPERBOY_STATE_DIR` and `pkg/plugin.StateDir()` are gone. Paperboy requires at least version 5 and accepts newer versions, because the protocol only grows additively: new fields are ignored by the side that does not know them, existing fields keep their meaning, and Paperboy gates an optional feature on the version a plugin reports in its answers. A plugin does not need to announce capabilities beyond that: the version says what it has.
- **Go plugins keep their code shape.** A plugin with state implements `LoadState(state, blobs)` and `State()` (`pkg/plugin.Stater`), one with files also `Blobs() map[string][]byte` (`Blober`). `Handle` and `ServeIO` call them and attach the result to every answer and event, so no plugin marshals the envelope itself. `Blobs` returns only the files that changed and clears its marks.
- **myskoda is the first user**: its vehicle state stays the same JSON, and the render image becomes the `render.png` blob instead of a file it writes itself.

## Alternatives

- **Keep the writable directory and document its format.** Cheapest, but the state stays unreadable for Paperboy, unbounded and impossible to back up with confidence; every plugin invents its own layout.
- **A read-only path plus a protocol write.** The plugin could read its files without a round trip, but Paperboy would still not know the format, and a cache could be re-read and rewritten behind its back.
- **Blobs as paths rather than base64.** Would need a writable directory again and would let a plugin point at arbitrary files.
- **State on every request instead of one `state` request.** Saves one round trip at the start, but the state would travel with every widget request and `configure` would have to carry the blobs.
- **Keep protocol 4 running.** Rejected: the old writable directory is what this ADR removes, and two state mechanisms in one directory would be a trap. The protocol is young and every plugin ships with Paperboy.

## Consequences

- Every plugin has to be rebuilt; a protocol 4 one is refused with a clear error (ADR 0058).
- A plugin starts with one more request than before.
- A restart after a configuration change hands state and blobs back through the `state` request; that request line is the largest the protocol carries, which is why blobs are capped at 1 MiB.
- The state is now a file an operator can read, back up and delete, and the blobs sit next to it under a name that says what they are.
- A newer plugin runs on an older Paperboy only as far as it stays additive; a plugin that needs a request the host does not send will fail, and that is a reason for a new protocol version rather than for capability lists.
