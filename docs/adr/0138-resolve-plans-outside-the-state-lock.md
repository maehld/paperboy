# 0138: Resolve plans and pages outside the state lock

- Status: Accepted
- Datum: 2026-11-20

## Kontext

A device request holds the state lock while `choosePlanPage` runs. That
function loads the plan and checks every page of the selected rule with
`os.Stat`, `os.Open` and a YAML parse. A slow disk therefore blocks every other
device, and the documented rule says that no configuration or disk work happens
under the state lock ([ADR 0047](0047-config-reload.md),
[ADR 0127](0127-plan-conditions-are-values.md)).

## Entscheidung

Paperboy resolves the plan, the selected rule and the pages of that rule that
fit the display type before it takes the state lock. The result carries the
plan, the selection, the fitting pages and one reason per page that does not
fit.

Under the lock Paperboy applies the result to the device state: it compares the
rule, moves the cursor, applies the buttons and records the skip reasons for the
journal. It does not load a plan and does not read a page file there.

The same split covers the device menu. Its actions come from the selection that
the request resolved before the lock.

## Konsequenzen

A slow page file no longer blocks the other devices, and the state lock covers
only memory. A plan reload between the resolution and the render is invisible to
the request: it sees the configuration and the plan of its start, which is the
rule for the configuration snapshot ([ADR 0047](0047-config-reload.md)). The
next request picks up the new plan.

The resolution runs even when the menu consumes the input or the battery
warning replaces the page. That is one plan load and a few stats per wake. A
virtual display and a text poll take the same path.
