# 0054: Plan preview per device in the web UI

- Status: Accepted
- Date: 2026-11-07
- Builds on: ADR 0025 (plans), ADR 0040 (dashboard), ADR 0049 (pages from plugin widgets)

## Context

Issue #23 asks for a way to see a device's plan in the web UI: which rules it
has, which rule applies now or at another time, and how each page looks on the
device's display. Until now the dashboard only showed the last served page,
and checking a plan meant waiting for the device or calling
`/api/preview/{display}/{page}` page by page.

## Decision

- New read-only view `GET /ui/plan?device=<id>[&at=YYYY-MM-DDTHH:MM]`, linked
  from each device card on `/ui`. It is a server-rendered Go template like the
  dashboard, without JavaScript and without a token.
- The view lists every rule of the device's plan with its time window, days,
  `rotate` and `refresh`, marks the fallback, and highlights the rule that
  applies at the selected time (default: now), noting when it applies only as
  the fallback. It names the next rule change (`UntilNextBoundary`) and the
  rule that follows. `at` is read in the global timezone (ADR 0033).
- Each page is resolved the way the device would: pages that do not fit the
  display type (missing, other kind, wrong text grid) show the skip reason,
  pages whose file is broken show the error. Fitting image pages are embedded
  as `<img>` pointing to `/api/preview/{display}/{page}?at=…`, so the browser
  loads them in parallel and a slow plugin does not block the view; text pages
  are rendered inline as lines.
- `/api/preview` accepts the same optional `at`, so clocks and headers in the
  preview show the selected time. Plugin data stays current: widgets cannot
  render the past or future.
- The view never touches the device state: no page cursor change, no write to
  `state/devices.json`, no use of the state lock while rendering. Previews do
  not replace the pages stored for devices.
- `plan.Plan.Matches` exposes whether a rule's window matches, so the view can
  tell a matching rule from the fallback.

## Consequences

- Operators can check plans and page fitting without a device.
- Previews render through the plugins and may be slow on first use; they show
  the stale bar like the device would.
- Previews always show page 1 of 1 (no page indicator position), and the
  battery warning bar is not shown.
