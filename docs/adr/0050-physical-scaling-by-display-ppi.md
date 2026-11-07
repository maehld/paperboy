# 0050: Scale widget layout by display PPI

- Status: Proposed
- Date: 2026-11-07
- Amends: ADR 0049 (page style and plugin protocol), ADR 0026 (display types: `width_mm` replaced by `ppi`)

## Context

Since v2 (ADR 0049) every size on a page is derived from pixels: the page
style defaults to `padding: 8`, `rule: 1` and `text_size: 0.06` of the display
height, and plugins size their texts with `Request.TextPx`, which is
`text_size × page height`. Two panels with the same resolution therefore get
the same pixels, whatever their physical size:

| Display type | Pixels | Active width | PPI | Base text today |
|---|---|---|---|---|
| `e1001` (7.5″) | 800 × 480 | 163 mm | ≈ 125 | 28.8 px ≈ 5.9 mm |
| `sticky` | 800 × 480 | 86 mm | ≈ 236 | 28.8 px ≈ 3.1 mm |

Text on small high-density panels is physically tiny, large low-density panels
get oversized text. `display_types` accepts a `width_mm` (ADR 0026) that
nothing uses; it is also orientation-dependent: a portrait display type
(480 × 800) would need the short side, which is easy to get wrong.

## Decision

### `ppi` on display types

- Every image display type has a mandatory key `ppi` (pixels per inch, a
  positive number, e.g. `124.7`). It is independent of the orientation.
  `ppi` is rejected for `kind: text`.
- `width_mm` is removed. A configuration that still contains it is rejected
  with the hint `width_mm was replaced by ppi (ADR 0050)`, like the v1
  sections `server` and `providers`. A missing `ppi` on an image display type
  is an error. Migration is by hand (README): `ppi = pixels × 25.4 / mm` of
  the same side.
- Example configuration: `e1001` `ppi: 125` (800 px / 163 mm active area),
  `sticky` `ppi: 236` (800 px / 86 mm).
- Paperboy has no fallback PPI: every image page is composed with the PPI of
  its display type.

### Page style in points

All size values of the page and cell `style` are physical, in one unit,
points (1 pt = 1/72 in):

| Style key | Meaning | Default |
|---|---|---|
| `text_size` | base text height in pt | 16 pt, capped at 0.12 × display height |
| `padding` | inner margin of a widget in pt | 4.5 pt (≈ 1.6 mm) |
| `rule` | width of separator lines in pt | 0.6 pt (≈ 0.2 mm), never thinner than 1 px |

Paperboy converts them with the display's PPI (`px = pt × ppi / 72`, rounded,
padding at least 0, rule at least 1 px). On the E1001 this is almost today's
look (27.7 px text, 8 px padding, 1 px rule); the Sticky gets about twice the
pixels. Old fractional values fail parsing: a `text_size` below 4 is rejected
with `text_size is in points since ADR 0050 (e.g. 16), not a fraction of the
height`. `padding` and `rule` accept decimals; pages that set pixel values
there keep parsing but now mean points, which the README migration notes
call out. Text displays ignore the style as today.

### Plugin protocol (version stays 2)

The protocol keeps sending pixels, so plugins do not convert anything they
already use:

- `PageInfo.ppi` (`float64`): pixel density of the display. Always set for
  image displays, omitted for text displays.
- `Style.padding` and `Style.rule` stay pixels (now resolved from points).
- `Style.text_px` (`float64`, new): the resolved base text height in pixels.
  `Style.text_size` (fraction of the height) is removed from the protocol;
  its meaning no longer exists in the page style.
- `Request.TextPx(w)` returns `text_px`.
- New helpers `Request.PtPx(pt)` and `Request.MMPx(mm)` convert physical sizes
  with `page.ppi`. Only for a new plugin talking to an old Paperboy that does
  not send `ppi` they fall back to 125 PPI (and `TextPx` to 0.06 × height
  when `text_px` is missing); this emergency default is documented in
  `pkg/plugin` and never used by Paperboy itself.

### Reference grid and widget sizes

The 6×8 reference grid and the `sizes` a plugin declares stay unchanged: they
describe proportions of a page, not physical sizes. A plugin that cannot fit
into a physically small cell shows less, as today.

### Built-in and bundled plugins

`paperboy`, `weather`, `calendar` and `evcc` take the base text from
`TextPx`, padding and rule from the style (as today) and replace hard-coded
pixel minimums (e.g. the 10 px label minimum) with `PtPx`.

### Other places

- The widget gallery (`/ui/plugins`, `/api/widget/...`) uses the `ppi` of the
  selected display type; requests with plain `width`/`height` need a `ppi`
  query parameter.
- The widget cache key contains the style and the page info, and therefore
  the PPI.

## Consequences

- Every existing configuration needs a manual edit: `width_mm` → `ppi`, `ppi`
  added to image display types, and page styles that set `text_size`,
  `padding` or `rule` rewritten in points.
- Pages on dense panels (the Sticky) get larger text and fewer lines per
  widget; this is the point of the change.
- Plugins built against the old helper keep drawing with `TextPx`'s fallback
  until they are rebuilt; the bundled plugins are rebuilt with Paperboy.
- Rejected: deriving the PPI from `width_mm` (orientation trap), a fallback
  PPI in Paperboy (silently wrong sizes), a separate `text_pt` key next to
  fractional `text_size` (two units), physical widget `sizes`, and scaling
  whole pages (blurry with 2 or 4 gray levels).
