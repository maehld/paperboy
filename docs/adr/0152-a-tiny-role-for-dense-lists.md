# 0152: A tiny role for dense lists

- Status: Accepted, implemented
- Date: 2026-11-22
- Extends: ADR 0151 (shared design language)
- Answers: issue #383

## Context

ADR 0151 gave the plugins one type scale with `Micro` as the smallest role at
0.70 of the page's `text_px`. The first migrations showed that the list
plugins need a smaller line. `github`, `rss` and `departures` draw a title and
a metadata line per row. Their metadata was 0.45 to 0.48 of `text_px`, and a
row keeps two or three lines. With `Micro` at 0.70 a row grows by about 20 %,
so a list shows fewer items.

The metadata is not a footnote beside a value. It is the second line of a
dense row, and its small size is the point. Dropping it would lose the age,
the author or the feed name.

## Decision

1. `design.Scale` gains `Tiny` at 0.55 of `text_px`. It sits below `Micro`
   and above `render.MinSize`.
2. `Tiny` is for the second line of a dense list, such as an age, an author or
   a feed name. It is not for a tile label, a chart annotation or a value.
3. `Micro` keeps its role: axis ticks, footnotes and the third line of a
   compact chart.
4. A list that uses `Tiny` keeps one title size for all rows, as `rss` already
   does. The list drops the metadata line before it shrinks the title.
5. The gallery and `docs/design/07-design-language.md` show the new role.

## Consequences

- List plugins keep their density and share one metadata size.
- The scale has six roles. A plugin chooses a role; it does not invent a
  factor.
- `Tiny` is close to the smallest hinting size. On a small display it falls
  back to `render.MinSize` like every other role.
- The change is additive. A plugin that does not use `Tiny` keeps its current
  drawing.
