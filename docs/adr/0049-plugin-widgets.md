# 0049: Paperboy v2 – pages composed from plugin widgets

- Status: Accepted
- Date: 2026-11-06
- Supersedes: ADR 0024 (data document and YAML layouts), ADR 0032 (data providers with named regions), ADR 0034 (layout primitives and typography), ADR 0035 (duration format), ADR 0036 (exec providers), ADR 0038 (icons from the instance directory), ADR 0044 (text templates; the device API part stays), ADR 0046 (width-aware value formatting); amends ADR 0037 (instance layout) and ADR 0048 (calendar provider and agenda block)

## Context

Paperboy v1 renders every page itself: providers push JSON into a shared data
document (ADR 0024, 0032) or run as exec providers (ADR 0036), and page
templates turn the data into pixels with a growing set of blocks, formats and
typography options (ADR 0034, 0035, 0038, 0046, 0048). Every new kind of
content – a month grid, a weather pictogram, a SolarAI chart – needs a new
block in Paperboy and a new data contract with its provider.

ADR 0032 rejected image fragments from providers because fragments rendered
once and scaled for other display types lost their quality. The spike in
`docs/spikes/plugin-fragments.md` showed that this is a scaling problem, not a
fragment problem: a fragment rendered on request in exactly the size and
palette of its cell is pixel-identical to a block, as long as both use the same
drawing code.

## Decision

1. **Plugins provide widgets.** A plugin is a long-running program in the
   instance's `plugins/` directory (or on `PATH`), declared under `plugins` in
   `paperboy.yaml`. It offers named widgets, e.g. `calendar/agenda` or
   `weather/days`. Paperboy starts it on first use and keeps it running; it
   restarts a plugin that exits, hangs (`timeout`) or whose configuration
   changes.
2. **Protocol** (`pkg/plugin`): JSON lines over stdin/stdout with request ids.
   Actions: `configure` (the plugin's `config` section, sent once after the
   start; an error stops the plugin), `describe` (widgets with kinds, size
   hints in grid spans, minimum pixel size, typed arguments, config keys),
   `render` (PNG in exactly the requested pixel size and number of gray
   levels) and `text` (lines for a size in characters). Events without an id:
   `changed` marks widgets as outdated. Requests carry the global language and
   timezone, the page (title, number, count, display size) and per widget its
   arguments and style (padding, rule width, base text size).
3. **Configuration from `paperboy.yaml`.** `plugins.<name>` has `command`
   (argv), `timeout`, `env` and `config`. String values in `config` expand
   `${VAR}` from Paperboy's environment (an unset variable is a configuration
   error, `$$` is a literal dollar sign), so secrets stay out of the file.
   Plugins validate their own keys and reject unknown ones.
4. **Pages are grids.** `pages/<page>.<display>.yaml`, falling back to
   `pages/<page>.yaml`, has `title`, `grid: [columns, rows]`, `style`
   (`padding`, `rule`, `text_size`, `divider`), `notices: show|none` and
   `cells` with `at`, `span`, `widget`, `args` and an optional `style`
   override. Cells must not overlap. On an image display the grid divides the
   pixels; on a text display the grid is the character grid and must match the
   display, e.g. `[20, 4]`. Paperboy never scales: it asks for every widget in
   the size of its cell, quantizes the PNG to the panel's palette (nearest
   gray, no dithering) and pastes it; text lines are converted to the display's
   charset, cut and padded.
5. **Shared drawing code.** `pkg/render` holds the fonts, palettes, text,
   lines, fills, `Quantize` and icon masks Paperboy uses. Go plugins draw with
   it, so widgets of different plugins match.
6. **Built-in plugin `paperboy`** runs in the Paperboy process and offers the
   widgets without a data source: `header` (title, page indicator, clock),
   `text` and `clock`, for image and text displays. The name is reserved.
7. **Cache and background refresh.** Results are cached per plugin, widget,
   arguments, size, palette or text, style, page, language and timezone until
   the plugin's `max_age` (default 5 minutes). A `changed` event or an
   upcoming expiry renders widgets that were used in the last hour again in the
   background, so a device sync does not wait for a plugin. A failing plugin
   serves its last results as stale (a notice bar on image displays, `!` on
   text displays) and is not asked again for a minute; a widget without any
   result becomes a notice inside its cell. Results unused for an hour are
   dropped, and the cache is bounded.
8. **Removed:** the data API (`/api/data`, `internal/data`, `state/data.json`),
   exec providers (`providers`, `internal/provider`, `providers/`), the
   `server` section with the upload token, the row/block page model with its
   formats, `watch` and text-line templates, the `icon` block and the `icons/`
   directory. `server` and `providers` in `paperboy.yaml` are rejected with a
   hint. The preview (`GET /api/preview/{display}/{page}`) and `GET
   /api/plugins` need no token, like the dashboard.
9. **Shipped plugins:** `calendar` (CalDAV and iCalendar, ADR 0048; widgets
   `agenda` and `month`), `weather` (Open-Meteo; widgets `current`, `days`,
   `hours`, OpenMoji pictograms embedded) and `solardemo` (synthetic solar
   values; a stand-in until a SolarAI plugin exists). A SolarAI plugin talks
   to SolarAI over HTTP itself; SolarAI knows nothing about Paperboy, and
   Paperboy knows only plugins. Plugins have no HTTP transport; Go's `plugin`
   package stays rejected (CGO, no Windows, identical toolchain, no
   isolation).

## Consequences

- New content needs a plugin, not a change to Paperboy. Paperboy shrinks to
  devices, plans, composition and delivery.
- `/api/plugins` describes everything a page editor needs; the grid model maps
  directly onto the spans plugins declare.
- Existing installations break: pages, `providers` and the upload token must
  be migrated by hand. There is no migration code. SolarAI pages need a
  SolarAI plugin.
- Look and feel depend on plugins using `pkg/render` and the style they
  receive; a plugin that draws with other tools will look different.
- `pkg/render` and `pkg/plugin` are public API now and need care when they
  change.
- Open: an editor, a SolarAI plugin, data conditions in plans (ADR 0025), and
  OpenDisplay delivery.
