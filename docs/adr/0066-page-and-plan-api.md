# 0066: A page and plan API for an editor

- Status: Accepted
- Date: 2026-11-08
- Builds on: ADR 0037 (instance directory), ADR 0049 (pages from plugin widgets), ADR 0054 (plan preview), ADR 0055 (arg values), ADR 0057 (actions)

## Context

ADR 0049, 0054 and 0055 already describe the editor they are meant for:
`GET /api/plugins` provides widgets, arguments, allowed values and sizes,
`/api/preview/{display}/{page}` renders one saved page, and `/ui/plan` shows a
device's plan. What is missing is everything between the browser and the
files: there is no way to list pages and plans, to read one as a model or to
write one. Building a page or a plan today means editing YAML on the instance
host by hand.

A UI needs an API that does not force it to know file paths, to parse YAML in
the browser or to invent its own validation. The API also has to protect the
files: they are the only copy of the work.

## Decision

- The files under `pages/` and `plans/` stay the single source of truth. No
  database, no shadow copy. Hand edits keep working and are picked up by the
  existing mtime-based loaders.
- The model of the file is the API. `page.Template` and `plan.Plan` carry JSON
  tags next to their YAML tags; `plan.Clock`, `plan.Day` and `plan.Duration`
  marshal to the forms operators write (`"06:00"`, `"mon"`, `"5m"`). `page.Encode`
  and `plan.Encode` write a file with a stable, readable layout (one cell or
  rule per line, flow style for coordinates and windows) and validate before
  writing, so the API never stores something the runtime would reject.
- Read and write endpoints:
  - `GET /api/pages` lists the page files: name, display variant (`""` for
    `pages/<name>.yaml`), title, kind, grid, file hash and parse error.
  - `GET /api/pages/{name}?display=<type>` returns the file that display type
    would use — the variant first, the shared page second, reported with the
    display it belongs to — including the YAML text, the model and the hash.
    A broken file still answers, so its text can be read and repaired.
  - `PUT /api/pages/{name}?display=<type>` takes `{"hash","template"}`, writes
    the file atomically and answers with the stored state. A hash that no
    longer matches the file on disk is `409`; an empty hash creates a file or
    overwrites one deliberately.
  - `GET /api/plans`, `GET /api/plans/{name}` and `PUT /api/plans/{name}` do
    the same for plans; time windows are validated in the global timezone.
  - `POST /api/preview/{display}` renders `{"template":…}` without writing a
    file, so an editor can show a draft. Response: PNG for image displays,
    text lines for text displays, like `/api/preview`.
- Before every overwrite the previous content is copied to
  `state/editor/<pages|plans>/<name>/<unix-nano>.yaml`; at most ten versions
  per file are kept.
- Writes take no state lock, are atomic (`writeAtomic`) and are not
  authenticated, like the rest of `/ui` (ADR 0040). Page and plan names must
  pass `page.ValidName` (`a-z`, `0-9`, `-`), which also rules out path
  traversal.
- This ADR covers the API only. The editor UI on top of it is a separate step.

## Consequences

- An editor can list, read, validate and save pages and plans without knowing
  file paths and without a YAML parser in the browser.
- YAML comments are lost when a file is saved through the API, because the
  model is written back. The version ring in `state/editor/` softens that;
  whoever cares about comments keeps editing by hand.
- Defaults are written normalised (as in the example pages), so a save may
  reformat a hand-written file without changing its meaning.
- The endpoints are open to anyone who can reach the service. That matches the
  dashboard and the download check and assumes a trusted LAN; authentication
  is a separate decision before the service is exposed beyond it.
