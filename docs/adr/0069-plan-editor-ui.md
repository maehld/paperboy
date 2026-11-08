# 0069: A plan editor in the web UI

- Status: Accepted, implemented
- Date: 2026-11-08
- Builds on: ADR 0066 (page and plan API), ADR 0025 (plans), ADR 0057 (actions), ADR 0068 (page editor)

## Context

ADR 0066 gives the service read and write access to plans, and `/ui/plan?device=`
previews one device's plan. Building or changing a plan still means editing
`plans/<name>.yaml` by hand: rules with time windows, the page order, `rotate`,
`refresh`, the fallback and the actions the device menu offers.

## Decision

- `GET /ui/editor` lists the plan files next to the pages: name, number of
  rules, fallback and parse error. A broken plan is listed with its error and
  cannot be opened, because the API takes a model, not text.
- `GET /ui/editor/plan?name=<name>` opens one plan, built like the page editor
  (ADR 0068): a server-rendered template plus one embedded `ui/plan-editor.js`
  without a framework or build step, served at `/ui/editor/plan.js`. It works
  only through the API: the model from `GET /api/plans/{name}`, the action
  schema from `GET /api/plugins`, and the save through `PUT /api/plans/{name}`
  with the hash that was read; a `409` keeps the disk version and asks for a
  reload.
- The shell carries what the API does not have: the devices that use the plan
  with their display types, and every page file with the reason it is skipped on
  those display types, from the same check the device path uses (`fits`). So the
  editor warns about a page that the plan's own devices would never show, and
  links `/ui/plan?device=<id>` for each of them.
- Rules are edited in their order: name, `from`/`to` as times of day, days as
  checkboxes (none means every day), the page order (up, down, remove, add from
  the pages that are not listed yet), `rotate` and `refresh`.
- A rule that can never match is marked: an earlier rule that matches at any
  time, or an earlier rule with the same time window, wins for good. Only this
  clear case is decided in the browser; everything subtler stays with the
  operator and the plan preview (ADR 0054).
- `fallback` is chosen from the rule names, and renaming the fallback rule keeps
  the fallback.
- Actions come from the `describe` answers of the plugins
  (`plugin.Description.Actions`): title, arguments from the same schema as
  widgets, and whether the action asks for a confirmation. The plan-local `id`
  rule of ADR 0057 is checked before the API does: an action that appears more
  than once needs an id on every entry, ids are unique and an action with the
  same arguments appears once.
- The editor does not create plans: a plan file needs a rule with a page, and the
  files stay the operator's.
- Out of scope: assigning a plan to a device, which means writing
  `paperboy.yaml` (separate issue).

## Consequences

- Plans can be built and changed in the browser; the files stay the single
  source of truth, and a save reformats them and drops comments like any write
  through the API (ADR 0066).
- The warnings are hints, not a validator: the API stays the authority and
  answers what it rejects (a page without a rule, an action without an id, a
  refresh that is not a duration).
- The editor needs the plugin descriptions for its actions; a failing plugin only
  leaves its actions out, the rest of the plan still edits.
- Everything under `/ui` stays unauthenticated (ADR 0040, 0066, 0068): the
  editor is as open as the dashboard and belongs in a trusted network.
