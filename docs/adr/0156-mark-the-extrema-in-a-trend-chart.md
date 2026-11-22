# 0156: Mark the extrema in a trend chart

- Status: Accepted, implemented
- Date: 2026-11-22
- Builds on: ADR 0151 (a shared design language for plugin widgets)
- Answers: issue #417

## Context

`DrawTrend` draws series, an axis, time ticks and a "now" mark. A reader sees
the shape of a curve but not its turning points. A glucose day that rises to
210 and falls again shows the peak in the curve, without its value. A
temperature chart does not name the highest and the lowest hour.

The plugins draw their own annotations today, or none. A shared rule would
give every chart the same dot and the same label, and a plugin would only
choose the points.

## Decision

We add a marker to `sdk/design`:

- `Mark` names one point of a trend: time, value, an optional `Range` for a
  series with its own scale, and the label.
- `Extrema` returns the lowest and the highest point of a series.
  `ExtremaMarks` turns a widget argument (`none`, `min`, `max`, `both`) into
  the matching marks, so a plugin passes its formatter and nothing else.
- `DrawMark` draws one dot with a white edge and the label beside it. A
  plugin with its own chart draws the same mark; `dexcom` does this inside
  its band-filled graph.
- `Trend.Marks` draws the marks after the series, so the dot stays above a
  line or a bar.

A label goes to the side with more room: below a point in the upper half,
above a point in the lower half. It carries a white box, so a bar underneath
does not hide it. A label drops when it does not fit the plot or would
overlap another label. The dot always stays. A mark is optional content and
never turns a drawn chart into `ErrTooSmall`.

The charts of `weather`, `fuel` and `dexcom` get the argument `mark`, default
`none`. The protocol does not change: this is an optional Go SDK package.

## Consequences

- One chart can name its turning points without a second drawing code, and
  `dexcom` shares the dot with the two `DrawTrend` charts.
- Every chart that wants a marker keeps the decision about the points. The
  component knows no data, no unit and no language.
- A marker can cover a small part of the curve near its point. The white box
  of the label hides more, so the plugin opts in per widget.
- A changed `sdk/design` raises the version of every plugin that imports it.
  ADR 0107 keeps the build check.
