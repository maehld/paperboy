[← Design documents](README.md) · [Paperboy](../../README.md)

# Content

A page is a grid of cells on an image display and a layout of rows of values on a text display. Every cell shows one widget from one plugin, and every text cell shows one value a plugin declares. A plan decides which page a device gets and when.

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
| `grid` | Columns and rows of an image page. The reference grid is 6 × 8 cells. |
| `variants` | Display type to file label. The page file only. |
| `style.padding` | Space around the grid in points, default 4.5 |
| `style.rule` | Gap between cells in points, default 0.6 and at least one pixel |
| `style.text_size` | Base text size in points, 4–144, default 16, capped at 0.12 of the display height |
| `style.divider` | Lines that Paperboy draws between the cells |
| `notices` | `show` or `none`: whether a notice page may replace this page |
| `cells` | Image pages only: `at` (column, row), `span` (columns, rows), `widget`, `args`, `style` (overrides the page style), `scroll` |
| `rows` | Text pages only: lists of cells, each with `value`, `label`, `unit`, `width` and `align` ([text pages](#text-pages)) |

Parsing is strict and cells must not overlap. Sizes are in points since [ADR 0050](../adr/0050-physical-scaling-by-display-ppi.md). A fraction such as `0.06` is rejected with a hint: points are physical, and the display type's `ppi` converts them into pixels.

A widget declares its sizes as spans of the 6 × 8 reference grid, so an author places cells without knowing the panel.

## Image pages

A page has a pixel size from its display type. `RenderImage` divides that pixel area by the grid, which gives every cell its box, and calls the widget host once for the whole page.

For each cell, in this order:

1. The widget's PNG arrives at exactly the cell size, or the cell carries a notice.
2. Paperboy quantizes the PNG to the gray levels of the display type (2, 4 or 16) with the nearest level, without dithering. A plugin that needs dithering does it itself.
3. The image is pasted one to one. There is no scaling at this point: a widget that returns the wrong size gets a hatched notice with the reason.
4. A result that is older than its `max_age` adds the bar `Veraltet: …` at the bottom of the cell.

The built-in plugin draws text, lines and fills, so a page that is only text needs no image file. A page that shows its own device is cached per device; a page that only names other devices stays shared (ADR 0115).

## Text pages

A text page has no pixels. Its file says `kind: text` and carries `rows`: a list of lines, and every line a list of cells shown side by side ([ADR 0111](../adr/0111-text-pages-from-plugin-values.md)). The parser rejects `grid` and `cells` on a text page with a hint, because the character grid belongs to the display.

```yaml
title: Werte
kind: text
rows:
  - [{ value: paperboy/title, width: 20, align: left }]
  - [{ value: paperboy/date, label: Datum, width: 20, align: left }]
  - [{ value: weather/temperature, label: Temp, width: 10, align: decimal },
     { value: weather/humidity, unit: "%", width: 10, align: right }]
  - [{ value: evcc/pv, label: PV, width: 10, align: decimal, unit: kW }]
```

A cell names a `value` as `<plugin>/<key>`, the key a plugin declares in `describe.values`. Paperboy asks every plugin once for all keys of that page. It caches the answer like a widget, and a `changed` event drops it. The plugin answers data, never text: a number, a text, a duration in seconds, a timestamp or a direction. The shipped plugins declare their values in a later protocol version; until then a cell with such a key shows `? …`.

| Cell key | Meaning |
|---|---|
| `value` | `<plugin>/<key>` of the declared value |
| `label` | Text in front of the value, followed by one space |
| `unit` | Text behind the value, preceded by one space; default the unit the plugin declares for a number |
| `width` | Columns the cell takes, 1 to 80 |
| `align` | `left` (default), `right` or `decimal` |

Paperboy formats every value itself, so the same reading looks the same on every page:

- A number follows the global `language`: decimal comma for `de`, decimal point for `en`, and the `precision` the plugin declares. There is no grouping.
- A text stays as it is.
- A duration of seconds becomes `45s`, `12min`, `3h 20min` or `2d 4h`.
- A timestamp shows its time on the page's day. At midnight it shows its date (`02.01.2006`, English `Jan 2, 2006`). On another day it shows both (`14.11. 18:05`, English `Nov 14, 18:05`).
- A direction is a trend arrow: `^` up, `>` flat, `v` down, doubled for a strong move.

`left` starts the cell text at the first column. `right` ends it at the last. `decimal` puts the label at the left and the value against the unit, so decimal separators line up. The text is cut to the `width`; with `right` and `decimal` the end stays.

`RenderText` converts label, value and unit to the charset of the display before it measures or cuts: `utf8`, `ascii` or `hd44780`. The HD44780 keeps ä ö ü ß ° µ and turns Ä Ö Ü into A O U. A value the page asks for but no plugin declares becomes a wrapped `? …` cell, like a failing widget did. A stale value marks `!` in the last column of line 1.

A text page fits a text display when its rows fit the character grid. It has no more rows than the display, and every row is at most as wide. Rows the page does not fill stay blank. Other text sizes need a variant ([ADR 0110](../adr/0110-pages-serve-their-kind-and-name-variants.md)).

Lists (`for-each`), scrolling rows and the text editor arrive in later steps of ADR 0111. A page of values does not scroll yet.

## Which page a device gets

A plan is `plans/<name>.yaml` ([ADR 0025](../adr/0025-plaene-je-display.md), [0103](../adr/0103-plugin-conditions-in-plans.md)):

```yaml
rules:
  - from: "06:00"
    to: "22:00"
    days: [mo, tu, we, th, fr]
    conditions: [dexcom/low]
    pages: [morning, work]
    rotate: 30
    refresh: 300
fallback: [night]
actions:
  - action: evcc/mode
    args: {loadpoint: 1, mode: pv}
```

Rules are tried in order and the first match wins:

- `from` is inclusive, `to` is exclusive. A window that crosses midnight belongs to its start day.
- `days` accepts German and English day names.
- `conditions` must all be active (AND). A condition is `plugin/condition`, an answer of the plugin's `conditions` action. A missing or inactive condition does not match, and a condition never creates a boundary: only time windows do.
- `timezone` inside a plan is rejected; the global `timezone` applies ([ADR 0033](../adr/0033-global-language-and-timezone.md)). `when.data` of version 1 is rejected with a hint that names `when.conditions`.

A rule lists `pages`, `rotate` (seconds until the next page of the rule) and `refresh` (seconds until the next sync). `UntilNextBoundary` caps the refresh at the next rule change. `fallback` holds the pages of a device whose no rule matches.

A page of a rule that does not fit the device's display type is skipped. That is no file for the type, the other kind, or a text page whose rows do not fit the character grid. The file the page names decides; a type that names no variant is served by the page file. The skip is logged once per display type, page and reason. No fitting page at all gives a notice page. A broken page is shown as a notice, not skipped. A render that panics is logged with its stack and answered with a notice page.

A broken entry costs one display type, not the page ([ADR 0110](../adr/0110-pages-serve-their-kind-and-name-variants.md)). An entry for a type that no longer exists is unused: the file stays and Paperboy writes no log line. A variant file that no entry names is unused too.

Paperboy caches a parsed page by its modification time, so an edit applies with the next sync.

## Actions

A plan lists the actions that the device menu offers ([ADR 0057](../adr/0057-plugin-actions-from-the-plan.md)):

| Key | Meaning |
|---|---|
| `action` | `plugin/action` |
| `args` | Arguments of that action |
| `title` | Optional title in the menu; by default the action names itself |
| `id` | Optional plan-local identity |

The identity of an action is its `id`, or `plugin/action` when no `id` is set. As soon as the same action appears twice in a plan, every entry needs its own `id`. Exact duplicates of action and args are rejected. `actions` on a rule or on a page is rejected with a hint: actions belong to the plan.

Every rule carries the same action list, so the menu offers the same actions at any time of day.

## The device menu

Short `home` opens the menu when the device is not `actions: deny` and has a plan ([ADR 0057](../adr/0057-plugin-actions-from-the-plan.md), [0067](../adr/0067-buttons-and-the-menu.md), [0078](../adr/0078-the-menu-stays-open-while-it-is-used.md), [0082](../adr/0082-menu-tap-controls-in-a-strip.md)). Paperboy draws it over the served page and registers the result as a notice page, so the cached page stays untouched.

- An empty action list becomes a `Keine Aktionen` banner. A menu in which no action can run names the reason to run none. Both close after 10 seconds or on the next input.
- Otherwise three actions appear in a row, stacked in portrait, over the top half of the display.
- With `touch: true`, a smaller control strip sits beneath them: `<` and `>` while the list has more than one page, and `X`. The strip is drawn as controls, not as action cells.
- `next` and `previous` move the selection. `home` runs the selected action. A long `home` closes the menu and keeps the page. In the strip, `next` past the last action selects `X`, and `home` closes.
- Long presses inside the menu are ignored. `page_timeout` and page rotation pause while it is open.
- With the menu closed, a long `next` or `previous` jumps to the first page of the rule. The server decides this at 700 ms; the firmware only reports `hold_ms`.

Paperboy resolves the actions when the menu opens: one `describe` per plugin plus one `actions` request. It caches the answer in `menuState` in the device's state file. The entry keeps the phase (`list`, `confirm`, `running`, `result`), the selection by identity, the offset, the session and the confirmation hash.

An action that needs a confirmation shows `Home = ausführen · Next/Previous = abbrechen` and runs only when the device reports exactly the hash of that screen. The `Ja` and `Nein` cells are the touch targets. Running and result are a banner of an eighth of the display.

An action gets 5 seconds inside the sync. A slower one finishes on its own and a result wake brings the outcome.

Without input, the menu expires after `menu_show_duration` (default 30 seconds, at most 60). Paperboy asks the device for a one-shot wake at that deadline. Every button or touch restarts the deadline. A press that arrives while the device still reports the last served menu screen belongs to the menu, even past the deadline.

## Built-in widgets

The built-in plugin `paperboy` needs no data and is never cached ([ADR 0049](../adr/0049-plugin-widgets.md), [0095](../adr/0095-the-logo-on-pages-and-devices.md)):

| Widget | Draws |
|---|---|
| `header` | Title or page title, page indicator, clock, date, subtitle, alignment; the date takes the longest of four forms that fits half the cell, or the free text columns, and is dropped when none fits; `battery: true` adds the charge of this device at the right |
| `battery` | The battery of one device as an icon with percentage or voltage; without `device` it shows the device that renders the page |
| `device-status` | One row per device with battery, estimated runtime and the time since the last contact; `devices` selects ids, otherwise it shows the device that renders the page; the stalest row comes first |
| `text` | Free text, on text displays an overlong line untrimmed when the cell scrolls |
| `clock` | Time, date or both |
| `logo` | The Paperboy mark from its geometry; `wordmark` adds the lockup in Go Mono Bold, image displays only |

On a text page the built-in plugin provides four values. `title` is the page title. `time` and `now` are the current moment. `date` is midnight, so Paperboy writes the date.

## Decisions

[ADR 0025](../adr/0025-plaene-je-display.md) (plans) · [0033](../adr/0033-global-language-and-timezone.md) (timezone) · [0044](../adr/0044-text-displays-on-the-device-api.md), [0045](../adr/0045-text-poll-interval-from-the-plan.md) (text displays) · [0049](../adr/0049-plugin-widgets.md) (widgets on a grid) · [0050](../adr/0050-physical-scaling-by-display-ppi.md) (points and ppi) · [0055](../adr/0055-allowed-arg-values-and-gallery-form.md) (argument values) · [0057](../adr/0057-plugin-actions-from-the-plan.md) (actions) · [0067](../adr/0067-buttons-and-the-menu.md), [0078](../adr/0078-the-menu-stays-open-while-it-is-used.md), [0082](../adr/0082-menu-tap-controls-in-a-strip.md) (the menu) · [0092](../adr/0092-scrolling-text-lines.md) (scrolling lines) · [0095](../adr/0095-the-logo-on-pages-and-devices.md) (logo) · [0103](../adr/0103-plugin-conditions-in-plans.md) (conditions) · [0110](../adr/0110-pages-serve-their-kind-and-name-variants.md) (page file and variants) · [0111](../adr/0111-text-pages-from-plugin-values.md) (text pages from values)
