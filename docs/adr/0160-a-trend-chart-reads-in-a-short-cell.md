# 0160: A trend chart reads in a short cell

- Status: Accepted, implemented
- Date: 2026-11-23
- Builds on: ADR 0151 (a shared design language for plugin widgets)
- Extends: ADR 0156 (mark the extrema in a trend chart)
- Answers: issue #422

## Context

A Sticky cell of `span: [6, 2]` is 800 × 120 px at 236 ppi. Its page carries
`text_px` 52, so the `Detail` title is 44 px and the `Micro` axis is 37 px.
Title row, axis row and padding leave a plot of about 30 px. The heading
`Temperatur (°C)` takes more room than the curve.

ADR 0156 puts a mark label below a point in the upper half. The highest
temperature therefore carries the label under its peak, on the curve, and the
white box of the label hides the bars below.

## Decision

1. `DrawTrend` caps the title row at 0.16 of the cell height and the axis
   labels at 0.7 of that title. A tall cell keeps the type scale. `Legend` and
   `Current` follow the title, the marks follow the axis labels. The roles of
   the type scale do not change.
2. A mark label goes to the nearest free place in the plot. "Free" means the
   drawn series leave the place white. The search prefers the side away from
   the curve: above a point in the upper half, below a point in the lower
   half. A free place on the other side beats a place on the series. A leader
   line joins the label to the dot, so the line grows when the curve needs the
   room. The white box stays as a fallback when no place is free.
3. The dot always stays, and a label without a free place still goes to the
   nearest rectangle. `plugin.ErrTooSmall` never comes from a mark.

`weather/hours` gets the argument `legend` (default true). The three fill
entries cost the width of the title row in a short cell.

## Consequences

- A chart keeps a plot in a short cell. Its heading follows the cell, not the
  page, while the other widgets keep the page-wide type scale.
- A mark label can sit farther from its dot; the leader names the dot. A label
  hides the series only when every place is inked.
- The leader is a thin line and can cross the curve. That is the price of a
  free label.
- `sdk/design` is optional code. Every plugin that imports it raises its
  version (ADR 0107): calendar, countdown, departures, dexcom, evcc, fuel,
  github, myskoda, prusalink, rss and weather.
