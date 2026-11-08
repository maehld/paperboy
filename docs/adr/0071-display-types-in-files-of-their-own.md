# 0071: Display types in files of their own

- Status: Accepted, implemented
- Date: 2026-11-08
- Builds on: ADR 0037 (instance directory), ADR 0047 (configuration reload), ADR 0050 (ppi), ADR 0051 (power source), ADR 0066 (page and plan API), ADR 0070 (devices in files of their own)

## Context

Display types were the last object that existed only as a section of
`paperboy.yaml`. After ADR 0068–0070 the editor covers pages, plans and devices;
ADR 0070 left the display types out on purpose ("a later step; devices reference
them"). Creating a new hardware type still means editing the hand-written main
configuration, and the display selector of the page editor, the display type
list of the device editor and the widget gallery only offer what was configured
there.

Unlike a page or a device, a display type is an identifier that other files
carry: it is the suffix of every `pages/<page>.<name>.yaml` variant and the
`display_type:` value of every device file.

## Decision

- Display types move to `displays/<name>.yaml` in the instance directory
  (ADR 0037 layout), one file per type. The file name is the stable name
  (a-z, 0-9, `-`); the file holds the keys the map entry held before: `kind`,
  `size`, `gray_levels`, `ppi`, `charset`, `min_refresh`, `power`.
- The files are parsed as strictly as pages, plans and devices
  (`displays.go`): an unknown key, a second document, a broken value or an
  invalid name aborts the load and names the file. A leftover `display_types:`
  section in `paperboy.yaml` aborts the start with a hint that points at
  `displays/`, like the v1 sections and `devices:`.
- The per-type checks move from `Config.validate` into one function,
  `validateProfile`, which the loader and the API both use, so a written file is
  as valid as a loaded one. At least one display type is still required; a
  configuration without any is not usable and keeps the last valid version
  active (ADR 0047).
- Reload: the loader watches the `displays/` directory with the same stamp as
  `devices/` (name, size and mod time per `*.yaml`), so adding, changing or
  removing a display type applies without a restart; a broken file keeps the
  last valid configuration and logs once.
- API in the shape of ADR 0066/0070: `GET /api/displays` (list with parse and
  validation errors), `GET /api/displays/{name}` (model, raw YAML, file hash,
  parse error), `PUT /api/displays/{name}` (model and the hash that was read,
  `409` when the file changed, creates a missing file, keeps the old version
  under `state/editor/displays/<name>/`). `GET /api/profiles` stays the runtime
  list the editor and the gallery use.
- Editor: `/ui/editor` gets a fourth sub-tab *Displays* with the files, their
  kind, size, pixel density or charset, gray levels, power and errors, and the
  form for a new type; `/ui/editor/display?name=<name>[&new=1]` edits one with
  kind-dependent fields, the physical size computed from `ppi`, and the page
  variants and devices that reference it. Save and conflict handling work like
  the other editors.
- **Renaming stays manual and is not an editor operation.** The name is the
  suffix of page files and a value in device files, and it is no more a label
  than a page name or a device ID; a rename in the browser would be the first
  write that touches several files at once and would need an ordered
  transaction with recovery. Renaming means moving `displays/<name>.yaml`,
  renaming the `pages/<page>.<name>.yaml` variants and rewriting the
  `display_type:` of the devices together; the README describes it.
- Deleting is manual too, like pages, plans and devices; the references in the
  display editor make the consequences visible before a file is removed (a
  device pointing at a missing type fails the load, a page variant of a missing
  type is listed with its error by the overview).
- Migration is by hand, like the v1 to v2 migration and ADR 0070: split the
  `display_types:` section into one file per entry and remove the section
  (README). There is no migration code.
- Out of scope: renaming and deleting through the API, plugins, automatic
  migration, authentication of the editor.

## Consequences

- A new display type is created in the browser and appears at once in the page
  editor, the device editor, the widget gallery and `/api/profiles`; a change
  rewrites one small file and never loses the comments of `paperboy.yaml`.
- `paperboy.yaml` keeps only the global settings (`language`, `timezone`,
  `plugins`, `download`, `defaults`, `firmware`, `battery`).
- A new instance needs one file per display type before it starts; the example
  instance ships `displays/` next to pages, plans and devices.
- Changing `ppi` silently changes the typography of every page of that type,
  and switching `kind` or `size` invalidates its page variants; the editor
  shows the computed physical size and the dependent pages, so that is an
  informed action.
- The display type files stay the authority; the editor is as open as the rest
  of `/ui` and belongs in a trusted network.
