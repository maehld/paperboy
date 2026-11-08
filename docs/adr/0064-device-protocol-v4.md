# 0064: Device protocol v4 – event identities and explicit wake reasons

- Status: Proposed
- Date: 2026-11-07
- Builds on: ADR 0041 (firmware), ADR 0042 (device API), ADR 0051 (power source), ADR 0057 (action menu)
- Answers: issue #55

## Context

The action menu needs two properties the device API does not have:

- **No lost or double input.** `pb_sync_body` empties the event queue before the
  POST, so a failed request loses the presses and the device sleeps. A retry
  cannot be told apart from a new press, so a duplicated sync would run an
  action twice.
- **A menu that closes itself.** While the menu is open, the device must wake at
  the menu deadline even if the regular refresh is much later, and it must say
  *why* it woke so the server can close the menu and restore the page. The same
  is needed for an action that runs longer than the sync budget.

`shown` is also remembered when the download finishes, before the panel paints,
so a confirmation screen could be reported as painted although the panel still
shows the page.

The action menu is a new feature and old firmware is not supported (ADR 0057),
so the device API can change once instead of working around these limits.

## Decision

### 1. Event identities

- Every button, touch and hold event carries a monotonically increasing
  `id` per device. The firmware keeps the counter and the queue in RTC memory,
  so they survive deep sleep.
- The firmware sends the queue on every sync and **only drops it after HTTP
  200**. A failed sync keeps the queue and retries; a retry resends the same
  IDs.
- Paperboy stores the highest processed event ID per device
  (`last_event_id` in `state/devices.json`) and ignores events with an ID at or
  below it. The protocol is at-least-once; actions set target states, never
  toggles, so a repeated event is harmless beyond being ignored.
- An event without an ID (`id` missing or 0) is applied without deduplication,
  so a hand-written client and the existing tests keep working.
- A `powercycle` sync resets the stored ID: the RTC counter starts again at 1
  after a power loss, so the server forgets the old high-water mark. Pending
  events are lost with the RTC memory, which matches the menu reset on
  `powercycle`.

### 2. `shown` after the paint

`pb_remember` runs after the display update finished, not when the download
finished. A sync may only confirm an action when its `shown` hash equals the
confirmation screen that Paperboy served before, which now proves the panel
painted it.

### 3. Explicit one-shot wakes

- The sync answer may carry `wake: {reason, after_s}`. It names the reason of
  exactly the next wake and how long to sleep; the firmware stores both in RTC
  memory and reports `reason` in the next sync.
- Reasons are `timer` (default), `button`, `touch`, `powercycle`, `menu` and
  `result`.
  - `menu`: Paperboy wants to close the menu at its deadline, even when the
    regular `refresh_s` is later.
  - `result`: an action that did not finish within the sync budget has an
    outcome to show.
- Without `wake` the old rule applies: `refresh_s` with reason `timer`.
- `refresh_s` keeps its meaning for regular refreshes and stays above the
  floor of the power source; `wake.after_s` is a one-shot and may be shorter
  only where the server decides that one extra paint is worth it.

### 4. Version and compatibility

- The firmware reports `firmware` `0.6.0` for `sticky`, `e1001` and `lcd2004`;
  the device API stays path-compatible, so no URL changes.
- The server accepts events without IDs (see above) but no other v3
  behaviour; older firmware is not supported (ADR 0057).

## Consequences

- A press survives a failed sync and is applied exactly once.
- A confirmation cannot be triggered by a press that was made before the
  confirmation screen was painted.
- The menu closes at its deadline without waiting for the next refresh, and a
  slow action reports its result with one dedicated wake.
- The firmware queue lives in RTC memory, so a power cycle loses pending
  events; that matches the reset of the menu state on `powercycle`.
- The answer grows by one small object; the 1024-byte response buffer stays
  sufficient because the menu itself is an image, not JSON.
