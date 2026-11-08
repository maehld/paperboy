# 0073: Creating plans and deleting files in the editor

- Status: Accepted, implemented
- Date: 2026-11-08
- Builds on: ADR 0025 (plans), ADR 0066 (page and plan API), ADR 0068 (page editor), ADR 0069 (plan editor), ADR 0070 (devices in files of their own), ADR 0071 (display types in files of their own)
- Replaces: the part of ADR 0071 that leaves deleting to the operator

## Context

After ADR 0068–0071 every object type is a file in the instance directory and
has an editor: pages, plans, devices and display types. Pages, devices and
display types can be created from their tab, because the API writes a missing
file on the first save. A plan is different: `/ui/editor/plan` answers `404`
for a missing file, so a plan still has to be written by hand before the editor
opens it.

Deleting is missing everywhere. ADR 0071 named it out of scope ("Deleting is
manual too, like pages, plans and devices"), and the README repeats that for
display types. Removing a file by hand means finding it in the instance
directory, and the last version is only kept while the file still exists —
an accidental `rm` loses it.

## Decision

- **A plan is created in the plan editor.** The *Plans* tab gets a *New plan*
  form (name, `a-z 0-9 -`), which opens `/ui/editor/plan?name=<name>&new=1` with
  an empty model: no rules, no fallback, no actions. The editor says that a plan
  needs at least one rule with a page, and the existing
  `PUT /api/plans/{name}` writes the file on the first save. Without `new=1` a
  missing plan stays a `404`, so a typo never silently opens an empty plan.
- **Deleting is an API operation** in the shape of the save endpoints (ADR 0066):
  - `DELETE /api/pages/{name}?display=<type>`
  - `DELETE /api/plans/{name}`
  - `DELETE /api/devices/{id}`
  - `DELETE /api/displays/{name}`

  The optional `hash` query parameter works like the hash of a save: a file that
  changed since the editor read it answers `409` and stays. A missing file is a
  `404`. The answer is `{"deleted": "<path relative to the instance directory>"}`.
- **A page delete names exactly one file.** Unlike `GET`/`PUT /api/pages`, which
  fall back to the shared page for a display type without a variant, `DELETE`
  never resolves a variant to `pages/<name>.yaml`: the row or the open editor
  names the file, and that file is removed.
- **A copy stays behind.** Before the file is removed it goes through the same
  backup as an overwrite: `state/editor/<kind>/<label>/<timestamp>.yaml`, pruned
  to the last ten versions. There is no trash and no undo button in the UI; a
  deleted file is restored by hand from that copy.
- **References do not block a delete.** The confirmation names what still
  points at the file — page variants and devices of a display type, devices of
  a plan, plans whose rules name a page — and then the operator decides. Two
  reasons: a file that does not parse must be removable at all (the editor can
  list it, not repair it), and missing references are already tolerated by the
  runtime (a device without a plan gets a notice page, a plan rule skips a page
  that does not fit, a device pointing at a missing display type fails the load
  with a named file). A hard check in the API would have to model every
  reference and would still leave the broken-file case open.
- **Where the references come from:** `editorPlanFile` carries the page names of
  its rules, so one pass over the plans answers the page warning and the
  overview; the display type editor already collects its page variants and
  devices (`displayReferences`) and now also hands them to the script; the plan
  editor has its device list; the device warning is static.
- **The editor offers the delete**: one button per row in the four tabs of
  `/ui/editor` and one in the toolbar of the four editors, hidden for a new
  object. The confirmation comes from the page itself (`confirm()`), the call is
  the `DELETE` above, and afterwards the overview of the same object type is
  shown. Unsaved changes are discarded, which the confirmation says.
- Renaming stays a manual step over the references, as ADR 0071 decided.
- Out of scope: renaming, an undo action in the UI, deleting plugin programs or
  firmware images, authentication of the editor.

## Consequences

- All four object types can be created and removed in the browser; hand work
  stays for renaming and for restoring a file from its backup.
- A deleted display type leaves page variants and device files behind. The
  overview lists them with their error and the devices stop loading until they
  are fixed — which the confirmation named before the delete.
- `state/editor/` grows by one file per deletion and is pruned like the save
  backups.
- `GET`/`PUT` and `DELETE` behave differently for pages with a display type and
  no variant: `GET` answers the shared page and `PUT` would write a variant,
  while `DELETE` only removes the file it is given.
