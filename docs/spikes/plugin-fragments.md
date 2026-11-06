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
  is a **long-running process**: Paperboy starts it on first use and keeps
  it. Requests go to stdin as JSON lines with an `id`, answers come back on
  stdout as JSON lines with the same `id`:
  - `{"id", "action": "render", "levels": 2|4, "language", "timezone", "now",
    "images": [{"image", "width", "height", "args"}]}` →
    `{"id", "render": {"images": [{"png": "<base64>"} | {"error": "…"}], "max_age": 900}}`
  - `{"id", "action": "describe"}` → `{"id", "description": …}` with images,
    title, size hints in grid spans (`columns` × `rows`), minimum pixel size
    and typed arguments.
  - Events without `id`: `{"event": "changed", "images": [...]}` marks the
    plugin's cached images as outdated, so the next render asks again.
  - The plugin ends when stdin closes. In Go, a plugin implements `Handler`
    and optionally `Starter` (background work with a `notify` callback) and
    `Ager` (`max_age`).
- **`plugin` block** in page templates (`internal/page/plugin.go`):
  `plugin: calendar/month` or `plugin: {name, image, args, padding}`; it
  works inside `columns`. Before drawing, the renderer walks the layout,
  collects every plugin cell with its exact pixel size and asks the source
  for all fragments at once, outside the render lock. Fragments are
  quantized to the panel palette (nearest gray, no dithering) and pasted
  1:1. A wrong size or a failing plugin becomes a hatched notice inside the
  cell; a stale fragment sets the page's stale badge or bar.
- **`internal/fragment`** – runs plugins (`<instance>/plugins`, configured
  under `plugins:` in `paperboy.yaml` with `command`, `timeout`, `env`; the
  environment also carries `PAPERBOY_LANGUAGE`/`PAPERBOY_TIMEZONE`). One
  process per plugin, started lazily, stopped with the server, restarted
  after an exit, killed and restarted when an answer exceeds `timeout`, and
  restarted when its configuration changes (reconciled by the reload loop).
  All missing images of one plugin go out in one request; images are cached
  per (plugin, image, args, size, levels, language, timezone) until
  `max_age` or a `changed` event; a failure serves the last image as stale
  and backs off for a minute.
- **`GET /api/plugins`** – the `describe` answers of all configured plugins,
  the input for a page editor.
- **Plugins:** `cmd/calendar -plugin` loads the calendars in the background
  every `-refresh` (default 15 min), keeps them in memory, reports
  `changed` when the events differ, and draws `agenda` (same logic as the
  `agenda` block) and `month` (a month grid with titles or event bars);
  `cmd/solardemo` stands in for SolarAI with `tiles` and `chart`. Example
  pages: `examples/pages/plugin-kalender.e1001.yaml`,
  `examples/pages/plugin-solar.e1001.yaml`, `examples/pages/plugin-mix.e1001.yaml`
  (cells from two plugins on one page), sample data
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
3. **Stitching works, also across plugins.** One request delivers several
   images of a plugin (`tiles` + `chart`), and one page mixes cells of
   several plugins (`plugin-mix.e1001.png`: calendar agenda next to solar
   tiles above the solar chart). The header stays a block, so mixed regions
   (title + status) remain possible.
4. **Cost.** A cold page with plugin images took ~0.1 s locally (process
   start + render); later renders ~0.03–0.07 s, mostly `Quantize` through
   `image.At` (can be made faster). Long-running plugins keep their data in
   memory, so a slow source (CalDAV, SolarAI over HTTP) never sits in the
   render path.
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

- **Cache bounds:** the spike cache never evicts.
- **SolarAI:** a SolarAI plugin is a long-running process that talks to
  SolarAI over HTTP internally. The data source knows nothing about
  Paperboy, and Paperboy knows only plugins; no HTTP transport for plugins
  (user decision 2026-11-06). Go's `plugin` package stays rejected (CGO,
  no Windows, identical toolchain, no isolation).
- **Background rendering:** a `changed` event only expires the cache;
  Paperboy could render the affected pages right away so device syncs
  never wait.
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
