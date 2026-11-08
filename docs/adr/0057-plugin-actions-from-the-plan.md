# 0057: Plugin actions from the plan and the action menu

- Status: Proposed
- Date: 2026-11-07
- Builds on: ADR 0025 (plans), ADR 0031 (English), ADR 0042 (device protocol), ADR 0049 (plugins), ADR 0050 (plugin protocol version 3), ADR 0051 (power source), ADR 0056 (release trains), ADR 0058 (protocol version check)
- Answers: issue #30
- Relates to: issue #54 (touch input), issue #55 (device protocol v4)

## Context

The Sticky and the E1001 have three buttons; the `home` button only jumps
back to page 1. Plugins can already draw widgets, but there is no way to let a
user trigger something from the device: start charging, refresh a source,
switch a mode.

The first design put actions on pages. That forces the operator to navigate to
the page that hosts the right action, and a page change loses the menu. This
ADR therefore moves actions to the plan: every page of a plan offers the same
actions, independent of which page is currently shown.

The menu is rendered by Paperboy like a notice, not by the firmware. This keeps
localization, wrapping, availability and the look of the panel in one place;
the firmware only reports button and touch events and paints the image it gets.

## Decision

### 1. Actions belong to the plan

A plan gains a top-level `actions` list. Rules and pages do not have actions;
`actions` under a rule or a page is rejected with a hint.

```yaml
actions:
  - action: evcc/mode
    args: { loadpoint: 0, mode: now }
    id: charge-now
    title: "Charge now"
  - action: evcc/mode
    args: { loadpoint: 1, mode: now }
    id: charge-now-2
  - paperboy/refresh
```

- `action` is `plugin/action`; `args` are the action arguments; `title`
  overrides the plugin's title.
- The list is plan-level, so every rule and every page of a plan shares it and
  all devices using the plan see the same list.
- Validation is strict. Unknown keys, unknown plugins, actions or arguments
  disable the entry with a reason; they do not break the plan. Exact duplicates
  of `action` + `args` are rejected.

### 2. Identity and IDs

- `id` is optional and plan-local. It is mandatory for **every** entry of an
  action as soon as the same `plugin/action` appears more than once in the
  plan, because the action reference alone is no longer unique.
- IDs are unique within the plan and use the same character set as other names
  (`a-z`, `0-9`, `-`).
- The selection identity is `id` when set, otherwise `plugin/action`. The
  concrete execution still uses `action` + `args`.
- Reordering the list or changing `args`/`title` keeps the identity. A reload
  that removes the selected identity closes the menu with a notice.

### 3. Plugin protocol v4

The plugin protocol moves to version 4. ADR 0058 makes the version a hard
check: Paperboy refuses any plugin whose answer names another version, so a
version 3 plugin fails visibly (its cells show the error) instead of running
without actions. Plugins ship with Paperboy in the same release (ADR 0056), so
the remedy is the current plugin zip; the error names the expected version.

- `describe.actions` lists `name`, `title`, `args` (the same schema as widget
  args) and `confirm` (a bool and an optional text). The plugin decides whether
  an action needs confirmation.
- A new `actions` request receives the concrete instances (`action`, `args`,
  `device`, `page`, `language`, `timezone`, `now`) and answers per instance
  with title, availability, reason and confirmation. Paperboy batches per
  plugin and caches the answer by `max_age`; it asks again when the menu opens.
- A new `action` request executes one instance and answers
  `{ok, message, changed}`. `changed` invalidates the widget cache before the
  next render.
- Plugins must report action-relevant state changes with `changed`. evcc's
  current 50 W threshold is not enough for mode and availability.
- The protocol is documented in `docs/plugins/protocol.md`, not only in the Go
  package and the ADR log.

### 4. Menu and buttons

- Short `home` opens the menu when the plan has actions and the device allows
  them; otherwise it keeps today's behaviour (page 1).
- `next` / `previous` move the selection and wrap.
- `home` is **yes**: execute the selected action or open its confirmation.
  On the confirmation screen every other button is **no** and returns to the
  list with the selection unchanged.
- Long `home` closes the menu and goes to page 1 ("go to start"). `X` is a
  fixed menu control that closes the menu without forcing page 1; it is not a
  plan action.
- The menu is modal while open: rotation and `page_timeout` pause. If the page
  or rule changes from outside, the page is re-rendered and the overlay stays.
- `menu_show_duration` defaults to 30 s and is configurable in `defaults` and per
  device; the maximum is 60 s. Expiry wakes the device once, closes the menu and
  restores the page. The wake still respects the floor of the device's power
  source, so a battery device may keep the menu for up to a minute.
- Critical battery, a missing or broken plan and notice pages do not open the
  menu. `powercycle` closes it. Menu state, selection, confirmation, session and
  the last result are persisted in `state/devices.json`.
- The first short `home` only opens the menu. It never runs an action in the
  same wake, because the plugin state of the actions is resolved after the
  button that opens the menu.

### 5. Overlay

- The menu is a server-rendered overlay over the current page, anchored at the
  top where the buttons are. It covers the top half of the display; the page
  stays visible below.
- Landscape: full width, one row. Portrait: full width, stacked in depth.
- Always three actions plus `X`, `<` and `>`. `<`/`>` scroll by three actions
  and disappear when the list has at most three entries.
- Selection is inverted; unavailable actions are hatched and show the plugin's
  reason; long titles are wrapped to two lines and ellipsized. The arrows are
  drawn whenever the list has more than three entries.
- The confirmation uses the top-half overlay: the action title, "Home = yes,
  any other = no" and, on touch, `Ja`/`Nein`.
- Running and result are not screens but a **banner**: a strip of an eighth of
  the display at the top, outlined while the action runs and filled with the
  outcome ("done: …" or "failed: …"). No cells, no close box; any input or the
  next regular sync clears it. The page stays visible almost in full.
- Page rendering and overlay drawing stay separate. The menu state is part of
  the served image hash, and the page PNG cache is not overwritten by a menu,
  confirmation or result image.
- Touch uses the same cells: the server derives their rectangles from the
  phase and the display size, so a tap applies the same transition as the
  matching button. A tap never opens the menu. The Sticky firmware debounces
  the continuous touch stream into one event per tap (ADR 0060).

### 6. Confirmation and execution

- The confirmation is a separate screen. A confirmation is executed only if
  `req.Shown` equals the confirmation screen's hash **after the panel painted
  it**, and the event belongs to the current menu session. Otherwise the
  confirmation screen is served again. A coalesced `home, next, home` batch
  therefore cannot execute an action without the user seeing the confirmation.
- Every event has a monotonic ID (device protocol v4); the menu session records
  the smallest event ID it accepts. A queued event from before the confirmation
  screen cannot confirm it.
- The confirming sync executes the action with a short budget (about 5 s), not
  the full plugin timeout. If the action is not done by then, the answer shows
  "running" and schedules exactly one result wake. The result is stored in
  state and shown as an overlay; the next regular sync picks up the final
  result if it is still running.
- Actions set target states, never toggles, so retries are safe. Availability
  and arguments are validated again when the action is executed.
- A plugin that is down keeps its actions visible as unavailable with a reason;
  it does not silently hide them.
- The server keeps menu transitions under the state lock but fetches action
  metadata and renders the overlay outside it, like page rendering today.

### 7. Protection

- `devices.<id>.actions: allow|deny` controls whether a device may open the
  menu and execute actions. The default is `allow`.
- `deny` means: short `home` does not open the menu, touch input is ignored and
  the server rejects an execution even if an old menu state still exists. The
  check is server-side and appears in the dashboard and the log.
- A registered device with `allow` may execute every action of its plan.
  Confirmation is the only additional safeguard. A per-action allowlist is out
  of scope for v1; the optional action IDs make it possible later.

### 8. Device protocol and firmware

- Old firmware is not supported. The device protocol is redesigned in issue
  #55: event IDs with an RTC retry queue, `shown` after paint,
  `reason: menu`/`reason: result`, explicit one-shot wakes, and a sync answer
  that stays below the firmware's 1024-byte response buffer.
- Touch on the Sticky is handled in issue #54: tap instead of continuous
  touch, hit-testing against the overlay, and no menu opening by tap.
- The overlay is anchored at the top because both current devices have their
  buttons there. A future display type with buttons elsewhere adds an explicit
  `buttons: top|bottom|left|right` field; v1 does not need it.

### 9. State

`state/devices.json` gains a menu object per device:

- open/closed and phase (`list`, `confirm`, `running`, `result`)
- selected identity
- menu session and the smallest accepted event ID
- confirmation hash and `opened_at`
- last action and result

The state is cleared on `powercycle` and when the plan changes so that the
selected identity no longer exists.

## Out of scope

- `goto` and page-navigation actions.
- Text displays: no input path today.
- OpenDisplay delivery.
- A per-action allowlist.
- Drawing the menu in the firmware.
- Dynamic confirmation text per state in v1.

## Consequences

- Actions are written once per plan and work from every page; the operator no
  longer has to know which page hosts an action.
- The plugin protocol changes to version 4; all shipped plugins and third-party
  plugins must be rebuilt. The action menu needs new firmware (#55), so old
  firmware stays unsupported.
- The menu needs a renderer, a state machine, persistence and tests on the
  server, plus touch work (#54). The firmware change is bounded to events,
  wake reasons and `shown`, but it touches every device.
- The action list is visible to every device on the plan; `actions: deny` is
  the only per-device switch in v1.
- There is no migration: page-level actions never shipped, and plans without
  `actions` keep working unchanged.
