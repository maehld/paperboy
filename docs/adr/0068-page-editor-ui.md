# 0068: A page editor in the web UI

- Status: Accepted, implemented
- Date: 2026-11-08
- Builds on: ADR 0066 (page and plan API), ADR 0049 (pages from plugin widgets), ADR 0055 (arg schema), ADR 0040 (dashboard)

## Context

ADR 0066 gave the service a page and plan API: pages can be listed, read,
written and previewed as JSON, and the API validates, protects files with a
hash and keeps the last ten versions. Building a page still means editing YAML
on the instance host. The widget gallery (ADR 0055) shows what plugins offer,
but it cannot assemble a page.

## Decision

- Two server-rendered views, like the dashboard (ADR 0040), so the service keeps
  no static file directory:
  - `/ui/editor` lists the files under `pages/` grouped by page name, with the
    shared page and the variants of a page next to each other, their kind,
    grid, title and parse error, and a form for a new page.
  - `/ui/editor/page?name=<name>&display=<type>` edits one page for one display
    type. A shared page is opened for the display type chosen in the overview, a
    variant for its own; a variant of a display type that is no longer
    configured stays listed but cannot be opened, because a save would be
    rejected.
- The editor itself is one embedded `ui/editor.js`: plain JavaScript without a
  framework and without a build step, served at `/ui/editor.js` from the binary.
  It talks only to the API: the model from `GET /api/pages/{name}`, the palette
  from `GET /api/plugins` (filtered to the widgets that support the display's
  kind), the draft from `POST /api/preview/{display}` and the save through
  `PUT /api/pages/{name}?display=<file>`. The browser parses no YAML and knows no
  file paths.
- The page shell carries its starting state as JSON (name, display type, kind,
  size, default grid), so a page without a file needs no API of its own: it
  starts as a new page with the kind and grid of the display type
  (`&new=1`, and without a display type as the shared page).
- Placement is a click on a widget and a click on a free cell, or a drag of the
  widget onto a cell; the first declared size of the widget is used where it
  fits. A selected cell has span, arguments and style (points), and it can be
  deleted. The UI rejects an overlap and a placement outside the grid before the
  API does; the grid only shrinks when every cell still fits.
- The grid of a text page is the character grid of its display and is fixed;
  image pages start on the 6 × 8 reference grid of the widget sizes.
- Arguments are rendered from the declared schema (type, default, required,
  `enum`) and written as values of that type, so a bool that differs from its
  default is written out. Values the schema does not constrain are passed as
  they are typed; the plugin answers what it does not accept, in the preview cell.
- The file that is written is the file the API reported: a shared page stays the
  shared page. A variant is written only on purpose ("Save as <type> variant"),
  which starts from an empty hash. Otherwise the hash that was read protects the
  save: a `409` keeps the disk version and asks for a reload instead of
  overwriting a hand edit.
- A file that does not parse is listed with its error and opens read-only: the
  API takes a model, not text, so repairing it still means editing the YAML.
- Navigation: an *Editor* link on `/ui`, `/ui/plan` and `/ui/plugins`.
- Out of scope: the plan editor and authentication (separate issues).

## Consequences

- A page can be built and changed in the browser; the files stay the single
  source of truth and hand edits keep working. A save writes the model, so it
  reformats the file and drops comments, exactly like any write through the API
  (ADR 0066); the backups under `state/editor/` remain the undo.
- The palette needs the plugins to answer `describe`; a failing plugin shows its
  error instead of widgets, the rest of the page still edits.
- The editor depends on the API only, so the plan editor can reuse the shape and
  the dashboard keeps working without JavaScript.
- Everything under `/ui` stays unauthenticated (ADR 0040, 0066): the editor is
  as open as the dashboard and belongs in a trusted network.
