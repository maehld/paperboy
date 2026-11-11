# 0079: Renaming pages, plans, devices and display types

- Status: Accepted, implemented
- Date: 2026-11-11
- Builds on: ADR 0025 (plans), ADR 0066 (page and plan API), ADR 0068 (page editor), ADR 0069 (plan editor), ADR 0070 (devices in files of their own), ADR 0071 (display types in files of their own), ADR 0073 (creating and deleting in the editor)
- Amends: ADR 0070 and ADR 0071, which made a device whose display type is missing fail the whole load and left renaming to the operator; ADR 0073, which repeated that renaming stays manual

## Context

Every object of an instance is a file whose name is its identity: `pages/<name>[.<display type>].yaml`, `plans/<name>.yaml`, `devices/<id>.yaml` and `displays/<name>.yaml` (ADR 0068–0071). Creating and deleting already work in the editor (ADR 0073). Renaming was still handwork, and ADR 0071 argued it stays that way because a rename touches several files at once.

Handwork has two costs. Moving `displays/<name>.yaml` by hand leaves page variants and device files pointing at the old name; ADR 0070 made such a device fail the whole configuration, so one `mv` took every device down and the server refused to start until the operator found every file. And moving a page means finding all its variants and moving them together, which is easy to get half right.

A rename in the browser must not become a hidden rewrite of the configuration: names appear in plan rules, device files and page file names, and rewriting them silently would change files the operator did not open. The operator wants to see every dependent and decide each change.

## Decision

- **A rename moves files and nothing else.** The four endpoints follow the save and delete shape (ADR 0066, 0073):
  - `POST /api/pages/{name}/rename?hash=<set hash>`
  - `POST /api/plans/{name}/rename?hash=<file hash>`
  - `POST /api/devices/{id}/rename?hash=<file hash>`
  - `POST /api/displays/{name}/rename?hash=<file hash>`

  The body is `{"name": "<new name>"}`. The answer is
  `{"name": "<new name>", "files": ["<path relative to the instance directory>", …]}`.
  A name that is not `a-z 0-9 -` is a `400`, a missing file a `404`, a target
  that already exists a `409`, and a hash that no longer matches the file a
  `409` as well.
- **A page rename moves the whole page.** The shared page and every variant
  move as one set, and the set is guarded by one hash over all its files
  (`GET /api/pages/{name}` answers it as `set_hash`). If one move fails, the
  ones already done are undone, so a page never ends up half renamed. A new
  name that already has any page file is refused instead of merging two file
  sets.
- **A device rename moves its state too.** `state/devices.json` is keyed by the
  device ID, so the entry moves with the file: page cursor, battery reading,
  firmware notes and the menu survive. A state entry that already exists under
  the new ID is refused (`409`), because a deleted device keeps its state so a
  recreated file picks it up. The firmware registers by MAC, so a display does
  not notice the rename.
- **A display type rename moves only `displays/<name>.yaml`.** The page
  variants named after the old type and the device files that point at it keep
  the old name and are the operator's work.
- **The server stays usable while references dangle.** A device file that
  names a display type that is not configured no longer fails the load (ADR
  0070): the device is loaded with its defaults, marked with the reason in
  `state/devices.json`'s runtime view, and the rest of the configuration loads.
  Sync and the text poll answer `503` with the reason, never `401`, so the
  firmware keeps its key; register still works. The dashboard, `GET
  /api/devices`, the devices tab and the plan preview name the reason, and the
  device editor keeps the unknown value visible as `<name> (no such display
  type)` instead of dropping it. The save API stays strict, so the editor
  writes only a device whose display type exists.
- **An orphaned page variant stays editable.** A variant of a display type that
  is not configured is listed in the overview and opens through a configured
  display type of the same kind (`/ui/editor/page?name=<name>&display=<type>&source=<old type>`).
  The editor shows it read-only and offers *Save as &lt;type&gt; variant*; a
  save then writes `pages/<name>.<type>.yaml`, and the old file stays until the
  operator deletes it.
- **No reference is rewritten and no extra backup is taken.** The confirmation
  names what keeps the old name — the plans that name a page, the devices that
  use a plan, the page variants and devices of a display type — and the
  operator updates them in the editor. Renaming back is the undo; the versions
  under `state/editor/<kind>/<old name>/` stay as they are.
- **The editor offers the rename where it offers delete:** one button per row
  of the four tabs of `/ui/editor` and one in the toolbar of the four editors,
  hidden for a new object. The confirmation comes from the server, so both
  paths say the same. Unsaved changes are discarded, which the confirmation
  says. After the rename the overview is reloaded, or the editor opens the
  object under its new name.

## Consequences

- Renaming a display type no longer takes the instance down, and an instance
  after a rename is visibly incomplete in the browser rather than broken on
  the command line.
- A page rename is one transaction over its files; a failure leaves the old
  page as it was.
- Dangling references stay visible until they are fixed: a plan that names a
  page that does not exist gets a notice page, a device whose display type is
  missing answers `503`.
- The handwork that remains is the reference update the confirmation names,
  plus the option to save an orphaned variant under the new display type.
- `state/editor/` does not learn a rename backup. An accidental rename is
  undone by renaming back or by restoring a file from its delete backup.
