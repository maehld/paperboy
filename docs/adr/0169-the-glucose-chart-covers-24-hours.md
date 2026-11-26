# 0169: The glucose chart covers 24 hours

- Status: Accepted, implemented
- Date: 2026-11-26
- Amends: the day window of [ADR 0099](0099-glucose-readings-from-the-dexcom-share-service.md)
- Answers: issue #462

## Context

The `graph` widget of the dexcom plugin showed the calendar day by default:
from midnight to now. The service keeps 24 hours, the plugin stores them, and
the `values` rows already summarize the rolling 24 hours. Only the chart cut
its window at midnight.

Right after midnight the calendar window is almost empty, although the stored
series still holds the readings of the previous day. The operator sees an
empty chart every night, and a reading just before midnight disappears from
the chart even though it is five minutes old.

## Decision

1. **The default window rolls.** `period: day` covers 24 hours up to the
   current time, not the calendar day. The key stays the same, so existing
   pages need no change.
2. **The title says what it shows.** The chart title is `24 Stunden` /
   `24 hours`, and the editor labels the `day` choice the same way.
3. **The hour marks follow the clock.** The marks sit at 00, 06, 12 and 18 of
   the window, because a rolling window crosses midnight and offsets from its
   start would no longer name clock hours.
4. **The hour variant stays.** `period: hour` keeps its last hour from the
   newest reading, `Letzte Stunde` / `Last hour`, and its five-minute points.
5. **The values follow the same words.** The value titles and the plugin
   documentation say `24 hours` instead of `the day`. Their behavior does
   not change: they already used the stored 24 hours.

## Consequences

- The chart has data at every time of day, including just after midnight.
- The chart no longer matches the calendar day. A comparison with "today"
  shifts by the time of the day, and a reading before midnight stays visible
  for the next 24 hours.
- The stored series and the network traffic do not change.
- A page that wants the calendar day would need a new period later.
