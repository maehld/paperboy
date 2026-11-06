# Spike: plugin fragments

- Branch: `spike/plugin-fragments` (not for `main` as is)
- Date: 2026-11-06
- Revisits: ADR 0032 (image fragments rejected as the default model)

## Question

Can plugins render image fragments for regions of a page instead of providers
pushing data that page templates turn into pixels? ADR 0032 rejected that
because fragments were rendered once for one display type and scaled for the
others, which broke strokes and text.

## What the spike builds

- **`pkg/render`** – the drawing primitives of the page renderer, moved out
  of `internal/page` unchanged (Go fonts with full hinting, palettes with 2
  or 4 gray levels, text with shrink and ellipsis, lines, fills, `Quantize`).
  `internal/page/draw.go` now delegates to it; all golden images are
  unchanged.
- **`pkg/plugin`** – the protocol and a Go helper (`plugin.Serve`). A plugin
  is a program that reads one JSON request on stdin and prints one JSON
  answer on stdout:
  - `{"action": "render", "levels": 2|4, "language", "timezone", "now",
    "images": [{"image", "width", "height", "args"}]}` →
    `{"images": [{"png": "<base64>"} | {"error": "…"}], "max_age": 600}`
  - `{"action": "describe"}` → images with title, size hints in grid spans
    (`columns` × `rows`), minimum pixel size and typed arguments.
- **`plugin` block** in page templates (`internal/page/plugin.go`):
  `plugin: calendar/month` or `plugin: {name, image, args, padding}`; it
  works inside `columns`. Before drawing, the renderer walks the layout,
  collects every plugin cell with its exact pixel size and asks the source
  for all fragments at once, outside the render lock. Fragments are
  quantized to the panel palette (nearest gray, no dithering) and pasted
  1:1. A wrong size or a failing plugin becomes a hatched notice inside the
  cell; a stale fragment sets the page's stale badge or bar.
- **`internal/fragment`** – runs plugins (`<instance>/plugins`, configured
  under `plugins:` in `paperboy.yaml` with `command`, `timeout`, `env`),
  batches all missing images of one plugin into one run, caches per
  (plugin, image, args, size, levels, language, timezone) for `max_age`,
  serves the last image as stale when a run fails and backs off for a
  minute.
- **`GET /api/plugins`** – the `describe` answers of all configured plugins,
  the input for a page editor.
- **Plugins:** `cmd/calendar -plugin` draws `agenda` (same logic as the
  `agenda` block) and `month` (a month grid with titles or event bars);
  `cmd/solardemo` stands in for SolarAI with `tiles` and `chart`. Example
  pages: `examples/pages/plugin-kalender.e1001.yaml`,
  `examples/pages/plugin-solar.e1001.yaml`, sample data
  `examples/plugins/demo.ics`.

## Findings

1. **Image quality is solved.** Rendered in the exact cell size and palette,
   a fragment is indistinguishable from a block: the plugin agenda and the
   `agenda` block produce the same glyphs (compare
   `plugin-kalender.e1001.png` with `kalender.e1001.png`). The ADR 0032
   problem was scaling, and on-demand rendering removes it. 1-bit panels
   get the same checkerboard and hatch fills as blocks
   (`plugin-solar.mono.png`); a 400×300 panel gets its own layout, e.g. bars
   instead of titles in the month grid (`plugin-kalender.klein.png`).
2. **Consistency depends on `pkg/render`.** With the shared package, fonts,
   hinting and fills match. Margins, rule widths and size ratios are still
   the plugin's choice; the solar tiles of the demo look like the `tiles`
   block only because they copy its ratios. A plugin that draws with
   another library (Cairo, a browser, SVG) will look different.
3. **Stitching works.** One run delivers several images (`tiles` + `chart`),
   which a page arranges with rows and columns. The header stays a block,
   so mixed regions (title + status) remain possible.
4. **Cost.** A cold page with two plugin images took ~0.1 s locally
   (process start + render); cached ~0.07 s, mostly `Quantize` through
   `image.At` (can be made faster). The calendar plugin fetches its data in
   every run; with CalDAV that is seconds, so `max_age` and a background
   refresh matter (see open points).
5. **Data for text displays** does not come out of this model. A plugin can
   still print data (the calendar binary is both provider and plugin), or
   the protocol gains a `text` action that returns lines for a size in
   characters.

## SVG as a result format

Possible, but not a good default for ePaper:

- Go has no SVG rasterizer in the standard library or `x/image`; it needs a
  new dependency (e.g. `oksvg` + `rasterx`, with partial SVG support) or an
  external tool (`resvg`, a browser).
- Text in SVG is laid out by the rasterizer with its own font and without
  hinting; at 10–14 px on a 1-bit panel that is exactly the blurry,
  thresholded text the ADR 0032 spike saw.
- Anti-aliased edges must be thresholded to 2 or 4 levels; thin lines
  vanish or thicken.
- Benefit: a size-independent fragment, so one render per image instead of
  one per size. With on-demand rendering and the cache that benefit is
  small.

Recommendation: PNG in exact size stays the contract. SVG could be accepted
later as an optional format for graphics without small text (icons, simple
charts), rasterized by Paperboy.

## Plugin description and an editor

`describe` already lists images with grid spans (`1×2`, `3×1`, …), minimum
pixel sizes and typed arguments, so an editor can offer a palette of
widgets. For a real editor the page model needs a **grid**: today a page is
rows with relative heights and one level of weighted columns. A grid page
(`grid: [columns, rows]`, cells with `at` and `span`) maps directly onto the
spans plugins declare. That is a separate decision.

## Open points before an ADR

- **Background refresh:** render fragments ahead of `max_age` for the cells
  that plans will show, so device syncs never wait for a plugin.
- **Cache bounds:** the spike cache never evicts.
- **Long-running plugins:** a process start per run is fine for Go
  binaries; plugins with a slow start (Python, data fetches) want a
  persistent process or HTTP. SolarAI could also serve the protocol over
  HTTP instead of exec.
- **Style hand-over:** pass `padding`, rule width and base font size in the
  request so plugins follow the page theme.
- **Hashes and refresh:** a page changes when a fragment changes; plugins
  should avoid redrawing clocks they do not need, or displays wake to the
  same content with a new hash.
- **Text displays:** a `text` action, or keep providers for them.
- **Data API:** stays for text displays and simple values until a text
  action exists; whether providers remain is decided with the ADR.

## Verdict

Feasible. On-demand fragments in exact size fix the quality issue that led
to ADR 0032, and the shared `pkg/render` keeps them consistent with blocks.
Next steps: a grid page model, background refresh, and an ADR that
supersedes ADR 0032 point 3.
