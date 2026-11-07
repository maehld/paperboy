# 0050: Scale widget layout by display PPI

- Status: Proposed
- Date: 2026-11-07
- Amends: ADR 0049 (page style and plugin protocol), ADR 0026 (`width_mm` of display types)

## Context

Since v2 (ADR 0049) every size on a page is derived from pixels: the page
style defaults to `padding: 8`, `rule: 1` and `text_size: 0.06` of the display
height, and plugins size their texts with `Request.TextPx`, which is
`text_size × page height`. Two panels with the same resolution therefore get
the same pixels, whatever their physical size:

| Display type | Pixels | `width_mm` | PPI | Base text today |
|---|---|---|---|---|
| `e1001` (7.5″) | 800 × 480 | 163 | ≈ 125 | 28.8 px ≈ 5.9 mm |
| `sticky` | 800 × 480 | 86 | ≈ 236 | 28.8 px ≈ 3.1 mm |

Text on small high-density panels is physically tiny, large low-density panels
get oversized text. `display_types` already accepts and validates `width_mm`
(ADR 0026), but nothing uses it since ADR 0034 decided against branching on it.

## Decision

### Physical size of a display type

- The physical size stays `width_mm` on image display types (positive integer,
  rejected for `kind: text` as today). No new field: Paperboy derives
  `ppi = width × 25.4 / width_mm`. An explicit `ppi` key is not added, so
  there is a single source of truth.
- Without `width_mm` the PPI is unknown (0) and everything behaves exactly as
  today (fallback, see below).

### Page style defaults in physical units

When the PPI is known, Paperboy resolves the defaults of the page style from
physical sizes instead of pixels:

| Style key | Default without PPI (unchanged) | Default with PPI |
|---|---|---|
| `text_size` | 0.06 × height | 16 pt (≈ 5.6 mm), capped at 0.12 × height |
| `padding` | 8 px | 1.6 mm, at least 1 px |
| `rule` | 1 px | 0.2 mm, rounded, at least 1 px |

The values are chosen so that the E1001 keeps (almost) its current look
(16 pt at 125 PPI ≈ 27.7 px, 1.6 mm ≈ 7.9 px, 0.2 mm ≈ 1 px); the Sticky gets
about twice the pixels. The cap keeps a page usable on tiny panels.

A page or cell can set the base text size physically with the new style key
`text_pt` (points, 4–72). `text_pt` and `text_size` are mutually exclusive in
one style block; a cell override of either one replaces the inherited one.
Explicit `text_size`, `padding` and `rule` keep their pixel meaning and win
over the physical defaults, so existing pages that set them do not change.

### Plugin protocol (additive, version stays 2)

- `PageInfo.ppi` (`float64`, omitted when unknown): pixel density of the
  display. Text displays never get it.
- `Style.text_px` (`float64`): the resolved base text height in pixels.
  Paperboy always fills it for image displays, whether it came from
  `text_size`, `text_pt` or a default. `Style.text_size` stays and keeps its
  meaning (fraction of the height, filled when known) for old plugins.
- `Request.TextPx(w)` returns `text_px` when set and falls back to the old
  formula otherwise, so plugins built against the old helper keep working.
- New helper `Request.PtPx(pt)` / `Request.MMPx(mm)` convert physical sizes
  to pixels; without a PPI they assume 125 PPI (the E1001), which reproduces
  today's pixel sizes. Plugins use it for minimum sizes that are hard-coded
  pixels today (e.g. the 10 px label minimum in weather and evcc).
- Plugins that ignore the new fields keep working; the change only grows the
  JSON.

### Reference grid and widget sizes

The 6×8 reference grid and the `sizes` a plugin declares stay unchanged: they
describe proportions of a page, not physical sizes. A plugin that cannot fit
into a physically small cell shows less (as today with small cells); it does
not need a separate size list per density.

### Built-in and bundled plugins

`paperboy`, `weather`, `calendar` and `evcc` take the base text from
`TextPx`, padding and rule from the style (they already do) and replace
hard-coded pixel minimums with `PtPx`. Icons are scaled relative to the base
text, so they follow automatically.

### Other places

- The widget gallery (`/ui/plugins`, `/api/widget/...`) uses the PPI of the
  selected display type; pixel-only sizes (`width`/`height`) have no PPI
  unless an optional `ppi` query parameter is given.
- The widget cache keys already contain the style and therefore the resolved
  pixel sizes; the PPI is added to the key as part of the page info.

## Consequences

- Pages on display types with `width_mm` change their look after the update
  (mostly the Sticky: larger text, fewer lines per widget). This is the point
  of the change, but it is visible; operators can pin the old look with an
  explicit `text_size: 0.06`, `padding: 8`, `rule: 1`.
- No migration is needed: without `width_mm` nothing changes, and the protocol
  change is additive.
- Rejected: a separate `ppi` or `diagonal` key (two sources of truth),
  physical widget `sizes` (the plugin would have to know the panel when
  describing itself), and scaling whole pages (blurry with 2 or 4 gray levels).
