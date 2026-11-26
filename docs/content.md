[← Paperboy](../README.md)

# Pages and plans

## Pages

A page is a grid of cells, and every cell shows one widget ([ADR 0049](adr/0049-plugin-widgets.md)). An image display type may carry a header band above the grid; a page hides it with `header: false` ([ADR 0167](adr/0167-a-header-band-outside-the-page-grid.md), [0168](adr/0168-a-header-band-per-display-type.md)). The page file `pages/<page>.yaml` carries the layout and serves every display type of its kind. Its `variants` map names the exceptions ([ADR 0110](adr/0110-pages-serve-their-kind-and-name-variants.md)): `sticky: hochkant` means that `pages/<page>.hochkant.yaml` serves the sticky display type. The variant file carries the layout without the title and without `variants`.

```yaml
title: Übersicht
kind: image
grid: [6, 7]                 # columns, rows below the header band
variants: { sticky: hochkant }
style: { padding: 4.5, rule: 0.6, text_size: 16, divider: 0.6 }   # points
notices: show                # show (default) or none
cells:
  - { at: [0, 0], span: [3, 3], widget: weather/current }
  - { at: [3, 0], span: [3, 3], widget: evcc/site }
  - { at: [0, 3], span: [3, 4], widget: calendar/agenda, args: { lines: 7 }, style: { padding: 2 } }
  - { at: [3, 3], span: [3, 4], widget: weather/days, args: { days: 3 } }
```

```yaml
# pages/uebersicht.hochkant.yaml: the same screen on the portrait panel
kind: image
grid: [6, 7]
cells:
  - { at: [0, 0], span: [3, 3], widget: weather/current }
```

- `widget` is `<plugin>/<widget>`. Paperboy passes `args` to the widget unchanged. `at` is the top-left cell (column, row, from 0), and `span` is the size in cells (default `[1, 1]`). `action` names one action of the widget's plugin (see [Actions](#actions)). Cells must stay inside the grid and must not overlap. Empty cells stay white.
- `grid` is `[columns, rows]`, 1 to 80 each. A plugin offers its sizes in halves and quarters of a 6 × 8 reference grid, and the editor maps them onto the page's grid. The display type names the grid a new page starts with, for example `8 × 6` on a 4:3 panel and `10 × 6` on 5:3 ([ADR 0128](adr/0128-a-default-grid-per-display-type.md)). A page keeps its own grid, so a change to the display type never rewrites a page.
- `header: false` hides the header band of the display type. Without the key the page shows it. A page carries no header settings; the band lives in `displays/<name>.yaml` ([ADR 0168](adr/0168-a-header-band-per-display-type.md)). The title stays page-specific because the band reads the page title.
- On an image display the grid divides the pixels below the header band. Paperboy asks every plugin for its widgets in exactly the pixel size of the cell and the gray levels of the panel. It quantizes the PNG to the palette (nearest gray, no dithering) and pastes it 1:1, so Paperboy never scales a widget. The PNG has 1, 2 or 4 bits (2, 4 or 16 gray levels), depending on the display type. A plugin that answers with another size gets a notice in its cell.
- `style` is in points (1 pt = 1/72 inch, [ADR 0050](adr/0050-physical-scaling-by-display-ppi.md)). Paperboy passes it to every widget and converts it to pixels with the `ppi` of the display type. `padding` is the inner margin (default 4.5 pt ≈ 1.6 mm). `rule` is the separator width (default 0.6 pt ≈ 0.2 mm, never thinner than 1 px). `text_size` is the base text height (4–144 pt, default 16 pt, at most 0.12 of the display height), so widgets of different plugins match. `divider` (points, at least 1 px, default 0 = none) draws lines between the cells; Paperboy draws them, not the widgets. The `style` of a cell overrides the page style.
- The **built-in plugin** `paperboy` needs no configuration and provides `paperboy/text`, `paperboy/clock`, `paperboy/logo`, `paperboy/battery`, `paperboy/device-status` and `paperboy/reading` (see [docs/plugins/paperboy.md](plugins/paperboy.md)).
- `kind: image` or `kind: text` is required and says which displays a file is for. A plan may serve image and text displays alike. When a device pages through the pages of a plan, Paperboy skips pages that do not fit its display type. A page does not fit when no file serves the type or when it has the other kind. It also does not fit when a text box leaves the character grid of the display. Paperboy logs each skip once per display type and page. When no page of the plan fits, the device gets a notice (*No matching page*). Paperboy does not skip a broken page; it shows its error as a notice.
- **Stale widgets**: when a plugin fails, Paperboy keeps showing its last widgets and marks the page. An image display gets a bar (*Outdated: calendar/agenda*, German *Veraltet: …*), a text display gets `!` in the last column of the first line. `notices: none` turns both off. A widget that never rendered becomes a hatched notice inside its cell with the reason. A plugin that cannot draw in its cell at all answers `cell too small: <width>x<height>` instead of leaving the cell empty, so a cell that is too small for its widget shows that notice.
- Preview without a device: `GET /api/preview/<display type>/<page>` (PNG, or `text/plain` for text displays; an optional `?at=YYYY-MM-DDTHH:MM` renders the page as at that time in the configured timezone). `GET /api/profiles` lists the display types.
- The page editor at `/ui/pages` builds a page from the same model (see [Web UI](runtime.md#web-ui)).

### Actions

An image cell may name one action of its plugin with `action` and give the action its own `args` ([ADR 0164](adr/0164-a-widget-action-carries-its-own-arguments.md)). The plugin decides the choices and marks the current one ([ADR 0165](adr/0165-a-plugin-describes-its-action-options.md)). The page names the action and its arguments separately from the widget's args. The scalar `action: mode` stays a shorthand for an action without arguments.

```yaml
- { at: [3, 5], span: [3, 3], widget: evcc/loadpoint, args: { index: 0 }, action: { name: mode, args: { index: 0 } } }
```

A required enum left empty is the choice: the device offers its values as tiles. A set value fixes the call, and the action becomes one tile. An optional argument with a default may stay absent; the plugin applies the default. The plugin labels the tiles in the configured language and marks the current one. A missing required argument keeps the cell with the reason.

On a device whose actions are not denied and a page with an action, a short `home` shows the options in the cell: one tile per choice. A plugin from protocol 10 marks the current choice active; an older plugin reports it as unavailable. A disabled choice is hatched. A tap runs a runnable option, and the page returns with the new state; a tap on the active or a disabled option does nothing. A failed call shows a black banner *Action failed: …* (*Aktion fehlgeschlagen: …*) until the next sync.

Without a digitiser the keys operate the view ([ADR 0163](adr/0163-the-action-mode-works-with-the-keys.md)). The first runnable choice is selected and drawn inverted; the active choice is skipped. `next` and `previous` move the selection with wrap; a short `home` runs it and a long `home` closes the view. On a touch device the active choice is drawn inverted. A tap outside, 20 seconds without input or `next`/`previous` closes the view. A device with `actions: deny` never shows it.

A cell that is too small for the tiles says *Cell too small* (*Feld zu klein*); enlarge it in the page editor.

### Text pages

A page for a text display ([ADR 0044](adr/0044-text-displays-on-the-device-api.md)) says `kind: text` and carries `cells`: boxes at a character column and row ([ADR 0118](adr/0118-text-boxes-and-overflow.md)). The display type supplies the character grid.

```yaml
title: Werte
kind: text
overflow: strip
cells:
  - { at: [0, 0], span: [20, 1], text: "Familie" }
  - { at: [0, 1], span: [12, 2], value: paperboy/block, overflow: wrap }
  - { at: [14, 1], span: [6, 2], value: paperboy/now, align: right }
  - { at: [0, 3], span: [12, 1], value: paperboy/long, overflow: scroll }
```

- A box carries exactly one of `text` and `value`. `text` is literal text, may span lines and has no label or unit. A value cell names `<plugin>/<key>`.
- `at` and `span` are `[column, row]` and `[columns, rows]`. Boxes must stay inside the grid and must not overlap. Unused lines and columns stay blank.
- Page `overflow` is `strip`, `wrap` or `scroll`; a cell overrides it. `strip` cuts the content, `wrap` reflows at word boundaries and cuts at the box height, and `scroll` moves a wider line horizontally or extra lines vertically.
- A value cell can set `label`, `unit` and `align` (`left`, `right` or `decimal`), and `scale` for a value with a unit progression. A number cell can set `flow: arrow` for `←` and `→`, or name the words `out` and `in`. On `ascii` these show as `<` and `>`. A literal text cell has no label or unit.
- Paperboy formats values with the global language and the declared precision. It converts label, value and text to the display charset. A missing value shows `? …`; a stale value marks `!` in the last column of line 1. A text value may carry several lines, one per record; the box shows them line by line.
- A text page fits a display when every box fits the character grid. Other text sizes need a variant.
- The editor places and resizes boxes on the board. It hides the style in points, has no rows panel, and edits text or value, label, the unit choice and scale, flow, align and overflow in the right inspector. The stale switch is *Stale values*.
- A scrolling box gives the device its full text, its first window, a direction, the display type's `scroll.step` (default 300 ms) and a flag whether the scroll text changed. The virtual display and the firmware animate it.

The built-in plugin declares `paperboy/title`, `paperboy/time`, `paperboy/date`, `paperboy/now`, `paperboy/long`, `paperboy/block`, `paperboy/temperature` and `paperboy/humidity`. Every shipped plugin declares the values of its own data, from `weather/temperature` to `evcc/pv` and `departures/departures`; the `*.lcd2004` examples use them.

## Plans

Which pages a device shows is decided by a plan, `<name>.yaml` in `plans/`. A device points to it with `plan: wohnzimmer` in its `devices/<id>.yaml`, and several devices may share a plan. A plan only names pages, so it works for devices of different display types. Create and edit a plan in the browser at `/ui/plans` ([ADR 0069](adr/0069-plan-editor-ui.md), [0072](adr/0072-pages-as-qoi.md)): the list creates one, `/ui/plans/<plan>` edits it, and the device page assigns it ([ADR 0070](adr/0070-devices-in-their-own-files.md)). When you delete a plan, a copy stays under `state/editor/`; the confirmation names the devices that still use it.

```yaml
pages:
  - uebersicht
  - wetter
  - energie
  - { page: zucker, hidden: true }

on_refresh: cycle
refresh:
  default: 1h
  windows:
    - { from: "07:00", to: "22:00", interval: 10m }

triggers:
  - when: [{ value: github/state, is: [failing] }]
    page: zucker
    alert: refresh
  - when:
      - { value: weekday, is: [mon, tue, wed, thu, fri] }
      - { value: time, from: "06:00", to: "09:00" }
    page: wetter

```

- `pages` is the rotation, in order. A hidden page is not in it; only a trigger shows it. At least one page stays visible.
- `on_refresh` decides what a timed refresh shows: `cycle` moves to the next page, `first` shows the first page, `current` keeps the page (the default). Buttons page the visible pages plus the page of the active trigger; a long `previous` goes to the first page and a long `next` to the last ([ADR 0170](adr/0170-a-long-page-key-jumps-to-the-end-of-the-list.md)). A page that leaves the list is resolved by this mode ([ADR 0130](adr/0130-what-a-plan-refresh-does.md), [ADR 0133](adr/0133-the-page-cursor-holds-the-page.md)).
- `refresh` is the interval outside its windows; `default` falls back to the device interval. `from` is inclusive and `to` exclusive; a later `to` than `from` crosses midnight. Windows must not overlap. Paperboy caps the interval at the next edge, so the device changes its cadence on time.
- Times and days use the global `timezone` of the configuration ([ADR 0033](adr/0033-global-language-and-timezone.md)). A plan has no timezone of its own.
- A plan needs at least one page, and each page name appears once. A hidden page is still a page of the plan.

### Triggers

A trigger brings a page to the front while its conditions hold. Triggers are tried in order and the first match wins. The first matching trigger whose page fits the display type is delivered on every sync without button input. `next` or `previous` pages instead, and the next sync without input brings the page back. A short `home` keeps the served page. A trigger whose page does not fit is skipped, and the next matching trigger may apply ([ADR 0158](adr/0158-plans-are-pages-refresh-windows-and-triggers.md)).

- The page must be in `pages`. A hidden page is fine: the trigger lifts it while it holds and hides it again afterwards.
- `alert` lets the device signal: `once` when the trigger starts, `refresh` at the start and on every sync without input while it holds, or absent to stay quiet ([ADR 0131](adr/0131-a-plan-rule-alerts-the-device.md)). The device decides what an alert is: the Sticky beeps, the E1001 and E1003 beep and blink, the LCD2004 blinks.
- Every condition must match (AND). The keys within `is` are alternatives (OR). A missing value, a failing plugin and an unknown key do not match, so missing data never shows a warning page.

A condition tests one value:

- an enum of a plugin, for example `dexcom/state` with `is: [low, urgent]`;
- `weekday` with `is: [mon, …]`;
- `time` with `from` and `to`, a window of the day;
- `date` with `from` and `to`, a range of local days. Without `to` it is one day.

`weekday`, `time` and `date` are built into the plan and need no plugin. The plan editor offers all values in one select and shows the keys, the window or the range behind it. The plan view shows the key each value answers now, the current weekday, the window and the range, even when you preview another time. The names `paperboy/time` and `paperboy/date` on a text page are separate: they show the time and the date.

```yaml
triggers:
  - when: [{ value: dexcom/state, is: [low, urgent] }]
    page: zucker
    alert: refresh
  - when: [{ value: date, from: "2026-12-24", to: "2026-12-26" }]
    page: weihnachten
```

Dexcom declares the value `state` with the keys `normal`, `low`, `high` and `urgent`, following its configured thresholds. It answers no key while the reading is older than 15 minutes; see [the dexcom page](plugins/dexcom.md) for an example trigger and a labelled text cell.

The old rule format (`rules`, `fallback`, `when.conditions: [dexcom/low]`) is rejected with a hint. Rewrite the plan by hand; Paperboy does not migrate v1 content.

- Paperboy reloads plans on change without a restart; a broken file keeps the last valid version. A missing or broken page also leads to a notice page, so devices never see a server error.
- Plans serve devices on the Paperboy firmware.
