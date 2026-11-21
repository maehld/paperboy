# 0145: A period and a balance in the evcc plugin

- Status: Accepted, implemented
- Date: 2026-11-21
- Builds on: ADR 0049 (plugin widgets), 0075 (plugin versions), 0126 (a schedule event for plugin demand)
- Answers: issue #362

## Context

The evcc `flow` widget showed the current power as a diagram. A display should
also show the energy of a day or a week. The plugin already reads the day
buckets of `/api/history/energy` for the `history` widget and knows the same
categories: PV, grid import and export, battery charge and discharge, home and
the loadpoints.

The diagram also drew a dashed link for every idle node. That made the picture
busy and hid the one statement that matters: a link exists only while power
flows.

## Decision

1. The `flow` widget takes a `period`. `now` keeps the current power from
   `/api/state` in W. The other periods (`today` to `last_year`) sum the same
   day buckets and categories as the `history` widget and show kWh.
2. The flow draws a link only while power or energy flows. An idle node keeps
   its card and value but has no link.
3. A new `balance` widget shows the split as a T-account. Sources stand on the
   left, consumers on the right. The rows are fixed, a zero is a gray dash, and
   each side sums its values. The widget shows the current power only.
4. The flow uses line icons instead of the OpenMoji pictograms, so all icons
   share one stroke width. The plugin no longer carries the OpenMoji assets.
5. The protocol does not change; both widgets are image widgets of the plugin.

## Consequences

- A period flow is as current as `history_refresh` (15 minutes by default).
- evcc measures each category on its own. The totals of the balance can differ
  a little; the flow drops the remainder the way `splitFlows` already did.
- An evcc without the energy database keeps the current flow. A period flow
  shows the error of the history in its cell.
