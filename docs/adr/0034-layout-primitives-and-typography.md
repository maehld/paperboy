# 0034: Layout primitives and typography in page templates

- Status: Draft (discussion)
- Date: 2026-10-21
- Extends: ADR 0024 (data document, page templates), ADR 0032 (data providers and regions)
- Related: ADR 0013 (TrueType font), ADR 0026 (YAML configuration), ADR 0033 (global language and timezone)

## Context

Page templates (`internal/page`) render pages from the data document, and plans select them per device. The result works, but it looks noticeably poorer than the four pages SolarAI rendered itself (`solarai/internal/epaper`): overview (detailed and compact), battery, daily balance and daily history. Those pages are the visual reference for this ADR.

The rendering primitives are almost the same in both implementations: TrueType text, horizontal and vertical rules, lines, a mid-tone fill and the same font family. What differs is that SolarAI hard-codes composition and typography in Go, while our template vocabulary exposes only one row of one block:

| Aspect | SolarAI | Paperboy today |
|---|---|---|
| Typeface | regular and bold | bold only (`render.go` loads `gobold`) |
| Sizes | per element (label 0.16, value 0.30, detail 0.14 of the region height) | fixed factors, every text shrunk by `drawFitted` |
| Alignment | title left, status right, labels left, values right, footer centered | fixed per block (tiles left, list right, text centered) |
| Tiles | four columns or 2x2, three lines per tile | always two columns, label and value only |
| Columns | battery icon 26 %, table 74 %; four tiles side by side | vertical bands only, every block full width |
| Separators | 2 px rules between tiles and rows, under the header | one 1 px line under the header |
| Chart | time axis 0-24 h, PV as shaded area, load as 3 px line, axes, hour ticks, kW scale, "now" marker | x is the sample index, 1 px lines, every second series dotted, no axes or fill |
| Mid tone | 170 on 4-gray panels, checkerboard on 1-bit panels | no fill primitive |
| Battery | rectangle with border, cap and fill level | not expressible (ADR 0024 lists `bar` as open) |
| Stale state | inverted badge in the header | full-width bar at the bottom that covers the chart |
| Page indicator | "1/4" appended to the title | no page or page count in the render context |

Constraints carried over from earlier ADRs:

- Layouts stay declarative and strictly validated (ADR 0024); no HTML, CSS, SVG or headless browser, no image fragments or plugins (ADR 0032).
- Paperboy owns fonts, palettes, quantization, hashes, staleness and caching. Providers only push JSON.
- One template per page and display type; `width_mm` is not a layout switch, compact and detailed layouts are separate files.
- No new dependency: `goregular` is part of `golang.org/x/image`, which is already used (ADR 0013).
- Everything in the software is English (ADR 0031); texts that belong to a page remain template content (ADR 0033).

## Evaluation

Most differences are not missing drawing code but missing **style knobs**: weight, size, alignment, padding, rules and series styles. Three are structural and account for the pages we cannot express at all:

1. **Horizontal composition.** The battery page splits into icon and table, the overview puts four tiles next to each other. Vertical weights alone cannot express this.
2. **Chart semantics.** Plotting samples by index cannot reproduce a day chart. Without a time axis, area fill and axes the history page is a different chart.
3. **Vector primitives.** The battery icon needs a bordered rectangle with a cap and a fill level. A generic `bar` block covers it without icons or images.

Two data gaps belong to the provider side and are not template features: history points carry no `time`, and `solar/derived` (consumption, autarky, self-consumption, battery remaining) is missing (ADR 0024, section 5).

Porting SolarAI's pages with today's vocabulary would need either several nearly identical templates or a fork of the renderer. Adding options is the smaller change and keeps the operator in control.

## Decision

We extend the template vocabulary with optional style fields and three block types. Existing templates stay valid; new fields are optional and list shorthands remain accepted.

### 1. Typography

- `weight: regular|bold` on titles, labels, values, details, status and chart annotations. Defaults follow the reference: titles and values bold, labels, details, subtitles, status and annotations regular.
- `size` is a fraction of the block height (e.g. `0.30`). It replaces the hard-coded factors and makes typography a template decision.
- `shrink: true|false` controls whether the renderer reduces a font until the text fits. Default `true` keeps current behavior; templates that need consistent sizes set `false`.
- `align: left|center|right` per text or block. Defaults keep today's behavior (tiles left, list values right, text centered).
- Both fonts are loaded once (`goregular`, `gobold`) and faces are cached, as SolarAI does.

### 2. Box model and rules

- `padding: <px>` is the inner distance of a block to its region edges; default `4`. It replaces the current mixed heuristics (`cw/16`, `dx/40`, ...).
- `rule: <px>` draws a separator at the bottom edge of a block, `divider: <px>` between items of a block. `0` disables it. Units are pixels because templates are written per display type.
- Rules are solid black at the given thickness; the renderer keeps the existing palette.

### 3. Horizontal composition: `columns`

A new block type splits its region into weighted columns. Every column holds exactly one block. Nested `columns` are rejected: the language stays a row/column skeleton, not a layout engine.

```yaml
- name: akku
  columns:
    - weight: 26
      block:
        bar: { value: solar/current/latest/values/battery_soc, min: 0, max: 100,
               orientation: vertical, border: 5, cap: true, fill: shade,
               label: '{{ percent .solar.current.latest.values.battery_soc }}' }
    - weight: 74
      block:
        rows: { rule: 1, padding: 4, items: [...] }
```

### 4. Tiles and rows

`tiles` and `rows` accept an object form in addition to the list shorthand:

```yaml
tiles:
  columns: 4          # default 2
  divider: 2          # vertical separator between cells, optional
  padding: 8
  items:
    - label: Solar
      value: solar/current/latest/values/pv_power
      format: power
      detail: ''      # optional third line, text template
```

- A tile gains an optional `detail` (third line, e.g. "charging 1.2 kW" or "no flow") and per-element `label_weight`, `value_weight`, `label_size`, `value_size`, `align`, `shrink`.
- `sign` stays as a shorthand for a suffix on the value.
- `rows` gains `rule` between items and the same per-item style fields.
- The detail line is a text template, so presentation thresholds stay in the template. Comparison helpers `below` and `above` coerce numbers (`{{ if below .solar.current.latest.values.battery_power -10 }}discharging{{ else if above ... }}charging{{ else }}idle{{ end }}`).

### 5. Chart

```yaml
chart:
  points: solar/history/points
  title: 'Today: Solar (area) - House (line)'
  unit: W
  time: true          # x from point.time, local midnight to midnight
  y_max: auto         # or a number
  axis: true
  ticks: 6h
  now: true
  legend: true
  series:
    - { key: pv_power,   label: Solar, style: area, fill: shade }
    - { key: load_power, label: House, style: line, weight: 3 }
```

- `time: true` plots points by their RFC 3339 `time` in the configured timezone from local midnight to midnight. Points without a valid time are skipped and leave a gap.
- `style: area` fills down to the baseline with `fill`; `style: line` draws a line of `weight` pixels. Series accept the current string shorthand (line, 1 px).
- `axis` draws the left and bottom rules, `ticks` labels the time axis, `now` marks the current time with a dashed vertical line, `y_max: auto` rounds the maximum up to a readable value and labels it with `unit`.
- `title` and `legend` are optional; both are rendered with the regular weight.

### 6. Bar and fills

A `bar` block draws a value as a filled bar or as a battery symbol:

```yaml
bar:
  value: solar/current/latest/values/battery_soc
  min: 0
  max: 100
  orientation: vertical
  border: 5           # px, 0 = no frame
  cap: true           # battery nub, vertical only
  fill: shade
  label: '{{ percent .solar.current.latest.values.battery_soc }}'
  label_size: 0.13
  label_weight: bold
```

`fill` is shared by chart series and bars: `shade` (mid tone), `hatch` (diagonal hatching), `solid` and `none`. `shade` renders 170 on 4-gray panels and a checkerboard on 1-bit panels, exactly as SolarAI does, so one template remains usable for both panel kinds.

### 7. Header, status and notices

```yaml
- name: kopf
  header:
    title: Solar - Energy flow
    align: left
    status: { time: solar/current/latest/time, label: Stand }
    page_indicator: true
    rule: 2
```

- The title is drawn at `align` instead of always centered; the status area reserves its own width, so title and time no longer collide.
- `status.time` is a data path; the renderer formats it as `15:04` in the configured timezone. `status.label` is a text template.
- `page_indicator: true` appends `" - <page>/<pages>"` to the title when the plan has more than one page. The render values come from the plan, not from the data document.
- A template-level `notices: bar|badge|none` decides how stale watched paths are surfaced. `bar` keeps today's bottom bars, `badge` inverts the header status area and appends the built-in "outdated" text (requires a header with `status`), `none` hides the notice.

### 8. Template context

Text templates keep the data document as their root and gain functions:

- `now` (current time), `page` and `pages` (1-based index and count), `stale "path"` (age of a watched path against its `stale_after`).
- `below` and `above` compare a value with a number and coerce JSON numbers.
- The `time` helper also accepts `time.Time`, so `{{ time now }}` works.
- Functions do not collide with data branches: `{{ now }}` calls the function, `{{ .now }}` reads a data key.

### 9. Compatibility and validation

- New fields are optional; the existing `paperboy-data/pages/*.yaml` files remain valid. Templates with the old list forms are parsed into the new object forms.
- Unknown keys are still rejected, weights must be positive, `columns` needs at least two children with exactly one block each, nested `columns` are rejected, and `notices: badge` requires a header with `status`.
- `stale` requires the path to appear in `watch`; an unknown path is a template error.
- The four existing pages will look different after stage 1 because defaults move from "everything bold and shrunk" to the reference typography. This is intended and checked with the preview endpoint.

### 10. Data contract (provider side)

The following additions are required in the data document, not in the template language:

- `solar/history/points[]` carries `time` as RFC 3339.
- `solar/derived` carries the daily balance (`consumption`, `autarky`, `self_consumption`, `approximate`) and the battery remaining time, as already sketched in ADR 0024, section 5.
- `solar/current/latest/time` remains the timestamp shown in the header.

Templates do not gain arithmetic or business calculations. Only presentation thresholds (idle/charging, feed-in/consumption) live in the page.

### 11. Non-goals

- No free pixel positioning, no HTML/CSS/SVG, no image fragments or icons.
- No recursive layout engine; `columns` is one level deep.
- No `width_mm` branching inside templates; compact and detailed layouts remain separate files per display type.
- No multi-language template files; page texts stay in the installation language (ADR 0033).
- No new block types beyond `columns` and `bar` for the four reference pages. A `shape` block or more chart modes need a new ADR that names a page they are required for.

## Consequences

- Page templates can reproduce SolarAI's four pages; the renderer gains options instead of forked layouts, and providers stay pixel-free.
- The language grows from five blocks to seven and from no style to a small box model. This is the risk ADR 0024 already names; mitigation is a closed vocabulary, strict validation, golden-image tests and the preview endpoint as the feedback loop.
- Typography becomes explicit: templates carry sizes and weights, which is more verbose but consistent across tiles. Defaults keep old templates rendering, at the cost of a one-time visual rework.
- The chart depends on point timestamps and therefore on a provider change. Until then `time: true` charts show gaps or no data.
- Rendering work grows: multiple faces, fills, hatching and axis layout. All remain pure Go with the existing `x/image` dependency; the 1-bit checkerboard must be deterministic and hash-stable.
- Staleness can move into the header, which removes the bottom bar that currently covers the chart; the bar stays the default for pages without a header.
- The implementation is split into four steps: (1) typography, box model, rules, header status, `columns`, tile detail, 1.5-2 days; (2) chart with time axis, area fill and axes, 1-1.5 days plus provider timestamps; (3) `bar` and fills, 1 day; (4) porting the four reference pages and golden-image tests, 0.5-1 day.

## Open questions

1. Is `size` as a fraction of the block height sufficient, or do templates also need absolute pixel sizes for elements whose height varies with content?
2. Should `shrink` default to `true` (safe, current behavior) or `false` (predictable typography, clipping risk)?
3. Is one column level enough? It covers the battery and overview pages, but a weather page with a forecast grid may want a second level.
4. Does `bar` cover every vector primitive the reference pages need, or is a minimal `shape` block (rect, line) unavoidable later?
5. Should stale state always be derived from the newest watched path, or should the header status name its own watch entry?
6. Is the explicit `page_indicator` better than appending the page number automatically whenever a plan has several pages?
7. Do chart series need labels and a legend, or is a chart `title` enough for the reference pages?
