# 0151: A shared design language for plugin widgets

- Status: Accepted, implemented
- Date: 2026-11-22
- Builds on: ADR 0049 (plugin widgets), 0050 (physical scaling), 0107 (plugin builds), 0121 (battery gauge), 0122 (units)
- Answers: issue #383

## Context

`sdk/render` gives plugins the fonts, the gray palettes and the drawing
primitives. That is enough for a plugin to paint anything, and no plugin is
forced to use more. The freedom costs consistency: every plugin composes its
own tiles, hero elements, charts and meters, and every plugin derives its own
font sizes from `text_px`.

The differences are measurable. A tile label is `0.8 × text_px` in `evcc` and
`0.9 × text_px` in `fuel`. A tile value is `1.6 × text_px` in `evcc`,
`1.25 × text_px` in `countdown` and `2.8 × text_px` in `fuel`. A chart title is
`0.6 × text_px` in `fuel` and `0.9 × text_px` in `dexcom`. Neighbouring widgets
therefore look like they came from different applications.

The shapes repeat too. `evcc` has `tile` and `drawTiles`, `myskoda` has `fact`
and `drawFacts`, and `github`, `rss` and `prusalink` build similar rows by hand.
Hero layouts live in `fuel`, `weather`, `countdown` and `dexcom`. Charts live
in `fuel`, `weather`, `evcc` and `dexcom`. Five plugins draw a fraction meter.
Three plugins define the same `mutedColor`, and six define the same `num` with
a decimal comma.

ADR 0121 names the consequence for the battery gauge: a later external plugin
repeats the drawing or gets a shared helper. The same holds for every other
repeated shape.

## Decision

We add `sdk/design`, a small drawing layer on top of `sdk/render`. It owns a
page-wide type scale, the spacing that follows it and the recurring shapes. It
does not own the layout of a page: every component draws into a rectangle and
reports whether it drew. A plugin can use one component and paint the rest
itself.

### Type scale

| Role | Factor of `text_px` | Use |
|---|---|---|
| `Micro` | 0.70 | axis ticks, footnotes, "since 14:20" |
| `Detail` | 0.85 | details, tile label |
| `Body` | 1.00 | list rows, normal text |
| `Title` | 1.25 | widget title, tile value |
| `Hero` | 2.40–2.80 | the one large value |

The roles follow the page's base text size, not the cell height. Only `Hero`
grows with the cell. A label therefore has the same size in every tile of a
page. A block that does not fit drops optional lines before it shrinks its
type. A hero drops its details, then the caption. A tile drops its detail
line. Text never falls below 80 % of its role while optional content remains.

### Components

1. `Tiles` draws a grid of label, value, unit, detail and an optional trend
   arrow. It adds thin dividers between the cells.
2. `DrawHero` draws a label, a large value with a smaller unit, a caption, up
   to three details, an optional emphasized note, an optional icon and an
   optional meter.
3. `DrawTrend` draws line, area, step and bar series over time. It has an
   optional value axis, time ticks, a "now" mark, a title with a current value
   and a legend. A series can carry its own range, so precipitation
   probability and temperature share one chart. `Sparkline` is the axis-free
   variant.
4. `DrawMeter` draws an outlined fraction bar and marks a missing reading.
   `Arrow` draws a trend arrow, `Muted` returns the secondary ink and `Notice`
   draws a centered empty state.

The package imports `sdk/render` and not `sdk/plugin`, so it knows no widget
request and no protocol. The plugin maps a component that returns false to
`plugin.ErrTooSmall`.

The protocol does not change. This is a Go SDK package. A plugin in another
language keeps its own drawing.

### Review

A visual change needs pictures. `scripts/design-preview.sh` renders a gallery
of every component in four gray levels and in one bit. The gallery is
`docs/images/design/components.png` and its one-bit variant. The changed
documentation screenshots in `docs/images/plugins/` show the migrated widgets
before and after in the pull request.

## Consequences

- A page looks more uniform: labels, details, values and chart annotations
  share one scale across plugins.
- Plugins that adopt the package lose local drawing code. `evcc`, `fuel`,
  `weather` and `countdown` adopt it first. The other plugins follow in
  separate pull requests.
- The package is optional. A calendar month, a Pokédex card or a comic keeps
  its own drawing where the components do not fit.
- A component cannot know the data behind a value. It receives strings and
  numbers, so the plugin keeps formatting, language and units.
- The chart API covers the line, area, step and bar series that the shipped
  plugins need. A new chart form needs a new decision.
- The battery gauge of ADR 0121 stays in the built-in plugin for now. It can
  move to `sdk/design` when a second plugin needs it.
- A changed `sdk/design` raises the version of every plugin that imports it.
  ADR 0107 keeps the build check.
- A larger cell no longer enlarges every text. The author chooses a larger
  widget for a larger reading, not a larger font factor.
