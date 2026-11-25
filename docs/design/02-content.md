[← Design documents](README.md) · [Paperboy](../../README.md)

# Content

A page is a grid of cells on an image display and a character grid of boxes on a text display. Every cell shows one widget from one plugin, and every text cell shows one value a plugin declares. A plan decides which page a device gets and when.

## Pages

A page is one or more files under `pages/` ([ADR 0110](../adr/0110-pages-serve-their-kind-and-name-variants.md)). The page file `pages/<page>.yaml` carries the layout for its kind and serves every configured display type of that kind. Its `variants` map names the exceptions: the key is a display type, the value is the label of a file that serves it.

```yaml
title: Morning
kind: image
grid: [6, 8]
variants:
  sticky: hochkant
  lcd2004: lcd
style:
  padding: 4.5
  text_size: 16
  divider: true
notices: show
cells:
  - at: [0, 0]
    span: [6, 2]
    widget: paperboy/header
  - at: [0, 2]
    span: [6, 4]
    widget: weather/current
```

`sticky: hochkant` means that `pages/<page>.hochkant.yaml` serves the sticky. Several types may name one label, and a type appears once.

A variant file carries `kind`, `grid`, `style`, `notices` and `cells`. It inherits the title and its display types from the page file. It carries neither `title` nor `variants`, and the parser rejects both with a hint (ADR 0110).

| Key | Meaning |
|---|---|
| `title` | Name on the dashboard, in the plan preview and for the header widget. The page file only. |
| `kind` | `image` or `text`: required, the kind of the display types the file serves |
| `grid` | Columns and rows of an image page, 1 to 80 each. The display type suggests one, 6 × 8 by default ([ADR 0128](../adr/0128-a-default-grid-per-display-type.md)). |
| `variants` | Display type to file label. The page file only. |
| `style.padding` | Space around the grid in points, default 4.5 |
| `style.rule` | Gap between cells in points, default 0.6 and at least one pixel |
| `style.text_size` | Base text size in points, 4–144, default 16, capped at 0.12 of the display height |
| `style.divider` | Lines that Paperboy draws between the cells |
| `notices` | `show` or `none`: whether a notice page may replace this page |
| `cells` | Image pages: `at` (column, row), `span` (columns, rows), `widget`, `args`, `action`, `style` |
| `overflow` | Text pages: default `strip`, `wrap` or `scroll` ([text pages](#text-pages)) |
| `cells` | Text pages: boxes with `at`, `span`, `text` or `value`, `label`, `unit`, `scale`, `flow`, `align` and `overflow` |

Parsing is strict: an unknown key or a second YAML document is an error. Cells must not overlap. Sizes are in points since [ADR 0050](../adr/0050-physical-scaling-by-display-ppi.md). A fraction such as `0.06` is rejected with a hint: points are physical, and the display type's `ppi` converts them into pixels.

A widget declares its sizes as spans of the 6 × 8 reference grid, so an author places cells without knowing the panel. That reference grid is a fraction vocabulary, not a density. A span of 3 × 4 means half the width and half the height, and the shipped plugins use halves and quarters.

The page's own grid divides the pixels. It is independent of the reference grid. The editor maps a declared span onto it. Half the width then stays half the width on a page with 8 × 6 or 10 × 6 cells. A display type names the grid a new page starts with ([ADR 0128](../adr/0128-a-default-grid-per-display-type.md)). The page file keeps its own grid afterwards.

## Image pages

A page has a pixel size from its display type. `RenderImage` divides that pixel area by the grid, which gives every cell its box, and calls the widget host once for the whole page.

For each cell, in this order:

1. The widget's PNG arrives at exactly the cell size, or the cell carries a notice.
2. Paperboy quantizes the PNG to the gray levels of the display type (2, 4 or 16) with the nearest level, without dithering. A plugin that needs dithering does it itself.
3. The image is pasted one to one. There is no scaling at this point: a widget that returns the wrong size gets a hatched notice with the reason.
4. A result that is older than its `max_age` adds the bar `Veraltet: …` at the bottom of the cell.

The built-in plugin draws text, lines and fills, so a page that is only text needs no image file. A page that shows its own device is cached per device; a page that only names other devices stays shared (ADR 0115).

## Widget actions

An image cell may name one action of its plugin: `action: mode` (ADR 0162).
The page carries no arguments, titles or confirmations; the plugin owns them.

```yaml
- at: [3, 5]
  span: [3, 3]
  widget: evcc/loadpoint
  args: { index: 0 }
  action: mode
```

A short `home` opens the action mode when `actions` is not `deny` and the page
has an actionable cell (ADR 0163). Paperboy frames every actionable cell and
replaces it with the plugin's options: one tile per value of the action's one
required choice argument. A disabled option is hatched. A cell that cannot
hold its tiles says `Cell too small`.

The server resolves the options without an argument on the page. The
widget's arguments fill the action arguments they name (`index`). The
plugin's `describe` and `actions` answers name the choices, their titles and
their state. A plugin that is down keeps its cell with the reason.

A tap on a tile runs that call in the sync. The device gets the normal page
with the new state and the mode closes. A failed call shows a black banner
`Action failed: <reason>` (`Aktion fehlgeschlagen: …`); the next sync serves
the plain page again. A tap outside, a stale `shown` hash or
20 seconds without input close the mode and serve the plain page. The server
asks for a one-shot wake of reason `action` at the deadline.

A device without a digitiser operates the mode with the keys. The first
runnable option is selected and drawn inverted; a disabled option is skipped.
`next` and `previous` move the selection in reading order, wrap and skip
disabled options. A short `home` runs the selected option. A long `home`
closes the mode. Every move restarts the 20 seconds. Without a runnable option
the mode shows the reason tiles and a short `home` only closes it.

A device with a digitiser keeps the touch view: the tiles have no selection.
A tap runs the option under the finger. `next` or `previous` closes the mode.
A second short `home` asks the plugin again. `next` and `previous` page only
while the mode is closed.

## Text pages

A text page has no pixels. Its file says `kind: text` and carries `cells`: boxes at a character column and row, with a span in columns and rows ([ADR 0118](../adr/0118-text-boxes-and-overflow.md)). The display type supplies the character grid; the parser rejects `grid` on a text page.

```yaml
title: Werte
kind: text
overflow: strip
cells:
  - { at: [0, 0], span: [20, 1], text: "Familie" }
  - { at: [0, 1], span: [12, 2], value: weather/forecast, overflow: wrap }
  - { at: [14, 1], span: [6, 2], value: paperboy/now, align: right }
  - { at: [0, 3], span: [12, 1], value: paperboy/long, overflow: scroll }
```

A box carries exactly one of `text` and `value`. `text` is literal text and may span lines; a literal text cell has no label, labels, unit, scale, flow or args. A value cell names `<plugin>/<key>`, the key a plugin declares in `describe.values`. Paperboy asks every plugin once for all keys of the page and argument set. The value reads the box's `args`, the arguments the plugin declares for it ([ADR 0120](../adr/0120-arguments-for-values.md)). Two boxes with the same value and different arguments are two requests. Paperboy caches the answer like a widget and drops it on a `changed` event. The plugin answers data, never text. A text value may carry several lines, one per record. The box shows them like literal text; the label opens the first line and the unit closes the last.

| Cell key | Meaning |
|---|---|
| `at` | `[column, row]` of the top-left corner |
| `span` | `[columns, rows]` the box takes; default `[1, 1]` |
| `text` | literal text; multi-line strings keep their line breaks |
| `value` | `<plugin>/<key>` of the declared value |
| `args` | value cells only: the arguments of the declared value |
| `label` | value cells only: text in front of the value |
| `labels` | enum value cells only: display text per enum key |
| `unit` | value cells only: text behind the value; `none` hides it; default the declared unit |
| `scale` | value cells only: `auto` (default) or a declared unit step, e.g. `W` |
| `flow` | number value cells only: `arrow`, or `out` and `in` with the words of the page (issue #399) |
| `align` | `left` (default), `right` or `decimal` |
| `overflow` | `strip`, `wrap` or `scroll`; default the page's `overflow` |

`overflow` on the page sets the default for every box; a cell overrides it.
- `strip` cuts after the box's width and height.
- `wrap` reflows text into the box width at word boundaries, then cuts at its height.
- `scroll` keeps the full content and moves it. A line wider than the box moves horizontally; more lines than the box move vertically. Both together move vertically by lines.

Boxes must stay inside the character grid and must not overlap; `at + span` is checked against the display in `Fits`. A box that is not filled stays blank, so a page can leave rows and columns free.

An `enum` value answers one key of its declaration ([ADR 0127](../adr/0127-plan-conditions-are-values.md)). A value cell may label its keys in `labels`, one text per key. Paperboy shows the label when it is not empty and the key otherwise. Only an enum value cell may carry labels. A plugin and a plan condition carry none, because the text belongs to the page and its language.

Paperboy formats every value itself: numbers follow the global `language` and the declared `precision`, durations and timestamps their compact forms, and trends their arrows. A number with a declared progression takes the largest step whose rounded absolute value is at least 1. Zero and small values stay in the base unit ([ADR 0122](../adr/0122-hide-and-progress-a-unit.md)). The cell's `scale` forces a step, and its `unit` overrides or hides the unit text. A number cell may mark its direction with `flow`. `arrow` draws `←` for a positive value and `→` for a negative one. The words `out` and `in` replace the arrows. Positive leaves the entity, negative enters it. A value that rounds to zero gets no marker (issue #399). On an HD44780 display the arrows `←`, `→`, `↑`, `↓` and a `✓` come from CGRAM. `ascii` falls back to `<`, `>`, `^`, `v` and `OK` ([ADR 0155](../adr/0155-cgram-glyphs-on-hd44780-text-displays.md)). `RenderText` converts label, value and text to the charset before it measures, wraps or cuts. Lines of a multi-line value follow the cell's `align` and `overflow` like literal text. A stale value marks `!` in the last column of line 1. A missing value becomes a `? …` cell.

A scrolling box adds its full lines, its first window and its direction to the render result. The device answer carries them with the display type's `scroll.step` (default 300 ms) and a flag whether the scroll text changed. A firmware or the virtual display keeps its offset while the flag is false and restarts at the first window when it is true. ePaper pages reject `scroll` and `overflow` with a hint (ADR 0118).

The page editor places the same boxes on the board. Drag moves a box and its edges resize it. The inspector edits value or text, label, labels, unit, scale, flow, align and overflow. It shows one label input per declared enum key. Text pages have no style in points.

## Which page a device gets

A plan is `plans/<name>.yaml` ([ADR 0158](../adr/0158-plans-are-pages-refresh-windows-and-triggers.md)). It has an ordered page list, a refresh schedule and ordered triggers.

```yaml
pages:
  - energie
  - prusa
  - wetter
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
    page: prusa
```

`pages` is the rotation, in order. An entry is a page name or `{page: <name>, hidden: true}`. A hidden page is not in the rotation; only a trigger shows it. At least one page is visible.

`on_refresh` decides what a timed refresh shows: `cycle` (the next page), `first` (the first page) or `current` (the same page), default `current`. A button press and a sync without a timer do not page on their own. A page that leaves the list is resolved by the mode. `cycle` takes the next page at its declared position, `first` the first page and `current` the page a trigger displaced. An edit keeps the device on its page by name ([ADR 0133](../adr/0133-the-page-cursor-holds-the-page.md)).

`refresh` is the interval outside its windows. `default` falls back to the interval of the device. A window has `from`, `to` and `interval`. `from` is inclusive and `to` exclusive; a later `to` than `from` crosses midnight. Windows must not overlap. The next edge caps the sleep, so the device changes its interval on time.

`triggers` are tried in order and the first match wins. A trigger has `when` conditions, a `page` that must be in `pages`, and an optional `alert`. The page of the first matching trigger that fits the display type is delivered on every sync without button input. A button sync pages the extended list: the visible pages plus the page of the active trigger. A short home keeps the served page and the trigger decision. A trigger re-asserts at the next sync without input. A trigger whose page does not fit is skipped, and the next matching trigger may apply.

A condition tests a value:

- An enum value of a plugin answers one of the keys in `is`. A missing value, a failing plugin and an unknown key do not match.
- `weekday` is an enum of the plan with the keys `mon … sun`.
- `time` takes a window of the day in `from` and `to`.
- `date` takes a range of local days in `from` and `to`. Without `to` it is the single day of `from`.

Every condition of a trigger must match (AND). The keys within `is` are alternatives (OR). `weekday`, `time` and `date` are built into the plan and evaluated from the local moment, so they need no plugin. The plan's names are bare; `paperboy/time` and `paperboy/date` stay values of a text page. A trigger needs at least one condition.

A trigger lifts the hidden flag of its page while it holds. Its page keeps the declared place in the extended list. When the trigger ends, the page is hidden again and the cursor is resolved by `on_refresh`. A visible trigger page stays where it is.

`alert` asks the device to signal ([ADR 0131](../adr/0131-a-plan-rule-alerts-the-device.md)). `once` alerts in the sync that starts the trigger. `refresh` alerts at the start and on every sync without button input while it holds, so a low glucose value reminds each time. A trigger without the key stays quiet. The device decides what an alert is: a beep, a blink or both.

A page that does not fit the device's display type is skipped. That is no file for the type, the other kind, or a text page whose boxes do not fit the character grid. The file the page names decides; a type that names no variant is served by the page file. The skip is logged once per display type, page and reason. No fitting page at all gives a notice page. A broken page is shown as a notice, not skipped. A render that panics is logged with its stack and answered with a notice page.

A broken entry costs one display type, not the page ([ADR 0110](../adr/0110-pages-serve-their-kind-and-name-variants.md)). An entry for a type that no longer exists is unused: the file stays and Paperboy writes no log line. A variant file that no entry names is unused too.

The pages of a plan are static, so every page and the values of every trigger are warm. A trigger page is not cold. Paperboy caches a parsed page by its modification time, so an edit applies with the next sync.

## Built-in widgets

The built-in plugin `paperboy` needs no data and is never cached ([ADR 0049](../adr/0049-plugin-widgets.md), [0095](../adr/0095-the-logo-on-pages-and-devices.md)):

| Widget | Draws |
|---|---|
| `header` | Title or page title, page indicator, clock, date, subtitle, alignment; the date takes the longest of four forms that fits half the cell, or the free text columns, and is dropped when none fits; `battery: true` adds the gauge of this device at the right, as high as a capital letter of the date, without a number (a text display shows the percentage instead) |
| `battery` | The battery of one device as a gauge with percentage or voltage; without `device` it shows the device that renders the page; `2x2` puts the value below the gauge, the horizontal spans beside it ([ADR 0121](../adr/0121-segmented-battery-gauge.md)) |
| `device-status` | One row per device with battery, estimated runtime and the time since the last contact; `devices` selects ids, otherwise it shows the device that renders the page; the stalest row comes first |
| `text` | Free text, on text displays an overlong line untrimmed when the cell scrolls |
| `clock` | Time, date or both |
| `logo` | The Paperboy mark from its geometry; `wordmark` adds the lockup in Go Mono Bold, image displays only |

On a text page the built-in plugin provides four values. `title` is the page title. `time` and `now` are the current moment. `date` is midnight, so Paperboy writes the date.

## Decisions

[ADR 0158](../adr/0158-plans-are-pages-refresh-windows-and-triggers.md) (plans, refresh windows, triggers) · [0033](../adr/0033-global-language-and-timezone.md) (timezone) · [0044](../adr/0044-text-displays-on-the-device-api.md), [0045](../adr/0045-text-poll-interval-from-the-plan.md) (text displays) · [0049](../adr/0049-plugin-widgets.md) (widgets on a grid) · [0050](../adr/0050-physical-scaling-by-display-ppi.md) (points and ppi) · [0055](../adr/0055-allowed-arg-values-and-gallery-form.md) (argument values) · [0161](../adr/0161-home-syncs-and-the-menu-goes.md) (home and the menu), [0162](../adr/0162-widget-actions-and-the-plugins-choices.md) (widget actions), [0163](../adr/0163-the-action-mode-works-with-the-keys.md) (the key-operated action mode) · [0092](../adr/0092-scrolling-text-lines.md) (scrolling lines) · [0095](../adr/0095-the-logo-on-pages-and-devices.md) (logo) · [0127](../adr/0127-plan-conditions-are-values.md) (conditions are values) · [0110](../adr/0110-pages-serve-their-kind-and-name-variants.md) (page file and variants) · [0111](../adr/0111-text-pages-from-plugin-values.md) (text pages from values) · [0120](../adr/0120-arguments-for-values.md) (arguments for values) · [0121](../adr/0121-segmented-battery-gauge.md) (the battery gauge) · [0130](../adr/0130-what-a-plan-refresh-does.md) (what a refresh does), [0133](../adr/0133-the-page-cursor-holds-the-page.md) (the page cursor) · [0131](../adr/0131-a-plan-rule-alerts-the-device.md) (a trigger alerts)
