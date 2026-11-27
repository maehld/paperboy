# 0174: The glucose profile and the tight range

- Status: Accepted, implemented
- Date: 2026-11-27
- Builds on: [ADR 0099](0099-glucose-readings-from-the-dexcom-share-service.md) (glucose readings from the Dexcom Share service), [ADR 0169](0169-the-glucose-chart-covers-24-hours.md) (the 24-hour chart), [ADR 0175](0175-keep-five-weeks-of-glucose-readings.md) (five weeks of stored readings), [ADR 0081](0081-plugin-state-over-the-protocol.md) (plugin state), [ADR 0151](0151-a-shared-design-language-for-plugin-widgets.md) (the design language)
- Answers: issue #490

## Context

The dexcom plugin showed a day: the newest reading, the day curve and numbers
of the day. A CGM report that a physician reads is the ambulatory glucose
profile (AGP): the readings of at least 14 days folded onto one 24-hour grid,
with the median, the interquartile range and the 5th to 95th percentile.

The plugin used 80 to 280 mg/dL as its target band, the alert range of the G7
app. The clinical consensus uses two ranges instead:

- time in range (TIR), 70 to 180 mg/dL or 3.9 to 10.0 mmol/L, the target range;
- time in tight range (TITR), 70 to 140 mg/dL or 3.9 to 7.8 mmol/L, a narrower
  range proposed in 2023 for tight control.

The Share service answers at most 24 hours. Aggregating several days is
therefore the plugin's task, and the profile only exists while the plugin
keeps the readings.

## Decision

1. **The dexcom plugin draws the AGP as its own widget.** The `agp` widget
   folds the stored readings onto 48 half-hour slots. Each slot draws the
   median, the 25th to 75th percentile band and the 5th to 95th percentile
   band. The target range and the tight range are reference lines.
2. **The profile covers 14 days by default**, the AGP standard. The `days`
   argument selects 1 to 30 days out of the stored history of 35 days.
3. **A slot draws its median from the first day and its bands from three
   days.** The median needs no deviation, so one day already shows the curve
   of that day. A shorter history draws no invented band. Slots are drawn one
   by one, so a gap stays a gap. The title counts the days with readings out
   of the window, so 10 of 14 days read `10/14 Tage`.
4. **The widget shows no summary of its own.** Average, TIR, TITR, CV and GMI
   are values of the plugin, so the `values` widget and a text page show the
   same numbers without a second layout. Those values stay on 24 hours, like
   the day widgets. A figure over the profile window needs a value of its own
   or a window argument, and this change adds neither.
5. **A second widget draws the time in ranges as the report's stacked bar.**
   The `ranges` widget counts the readings in very low, low, target, high and
   very high and draws one segment per range with a legend of range and share.
   A wide cell lies the bar down, a tall cell stands it up.
6. **The defaults move to the consensus**: `low` and `high` become 70 and 180
   mg/dL or 3.9 and 10.0 mmol/L. The new `tight_low` and `tight_high` defaults
   are 70 and 140 mg/dL or 3.9 and 7.8 mmol/L. A tight range the configuration
   does not name follows the target range: its lower edge is `low`, its upper
   edge is at most `high`. `very_high` (default 250 mg/dL or 13.9 mmol/L)
   caps the high range of the bar.
7. **The plugin exposes `tight`, `cv` and `gmi` as values** and as rows of the
   `values` widget, so a text page carries the same numbers as the chart.

## Consequences

- Installations that never set `low` and `high` see a narrower target range.
  The glucose widget colours differently and `in-band` counts a different
  share. An explicit configuration keeps its numbers.
- A tight range outside the target range fails the configuration check. The
  fallback keeps the default configuration valid.
- The bar has five ranges only with an `urgent` low and a `very_high` above
  `high`. A value of 0 merges the range into the next one, so a configuration
  without them keeps four segments.
- The bands are only meaningful after several days. The curve starts with one
  day. The plugin draws the history of [ADR 0175](0175-keep-five-weeks-of-glucose-readings.md)
  and asks the service for nothing older than 24 hours.
- The chart is drawn by the plugin, not by `sdk/design`. Percentile bands are
  glucose-specific and no other widget needs them.
- The bands use the palette grays on a four- and sixteen-tone panel and row
  patterns on a one-bit panel. The median gets a white edge on a one-bit
  panel, so it stays apart from the rows of the inner band.
