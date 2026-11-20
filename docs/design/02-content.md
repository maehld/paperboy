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
| `cells` | Image pages: `at` (column, row), `span` (columns, rows), `widget`, `args`, `style` |
| `overflow` | Text pages: default `strip`, `wrap` or `scroll` ([text pages](#text-pages)) |
| `cells` | Text pages: boxes with `at`, `span`, `text` or `value`, `label`, `unit`, `scale`, `align` and `overflow` |

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

A box carries exactly one of `text` and `value`. `text` is literal text and may span lines; a literal text cell has no label, labels, unit, scale or args. A value cell names `<plugin>/<key>`, the key a plugin declares in `describe.values`. Paperboy asks every plugin once for all keys of the page and argument set. The value reads the box's `args`, the arguments the plugin declares for it ([ADR 0120](../adr/0120-arguments-for-values.md)). Two boxes with the same value and different arguments are two requests. Paperboy caches the answer like a widget and drops it on a `changed` event. The plugin answers data, never text. A text value may carry several lines, one per record. The box shows them like literal text; the label opens the first line and the unit closes the last.

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
| `align` | `left` (default), `right` or `decimal` |
| `overflow` | `strip`, `wrap` or `scroll`; default the page's `overflow` |

`overflow` on the page sets the default for every box; a cell overrides it.
- `strip` cuts after the box's width and height.
- `wrap` reflows text into the box width at word boundaries, then cuts at its height.
- `scroll` keeps the full content and moves it. A line wider than the box moves horizontally; more lines than the box move vertically. Both together move vertically by lines.

Boxes must stay inside the character grid and must not overlap; `at + span` is checked against the display in `Fits`. A box that is not filled stays blank, so a page can leave rows and columns free.

An `enum` value answers one key of its declaration ([ADR 0127](../adr/0127-plan-conditions-are-values.md)). A value cell may label its keys in `labels`, one text per key. Paperboy shows the label when it is not empty and the key otherwise. Only an enum value cell may carry labels. A plugin and a plan condition carry none, because the text belongs to the page and its language.

Paperboy formats every value itself: numbers follow the global `language` and the declared `precision`, durations and timestamps their compact forms, and directions their arrows. A number with a declared progression takes the largest step whose rounded absolute value is at least 1. Zero and small values stay in the base unit ([ADR 0122](../adr/0122-hide-and-progress-a-unit.md)). The cell's `scale` forces a step, and its `unit` overrides or hides the unit text. `RenderText` converts label, value and text to the charset before it measures, wraps or cuts. Lines of a multi-line value follow the cell's `align` and `overflow` like literal text. A stale value marks `!` in the last column of line 1. A missing value becomes a `? …` cell.

A scrolling box adds its full lines, its first window and its direction to the render result. The device answer carries them with the display type's `scroll.step` (default 300 ms) and a flag whether the scroll text changed. A firmware or the virtual display keeps its offset while the flag is false and restarts at the first window when it is true. ePaper pages reject `scroll` and `overflow` with a hint (ADR 0118).

The page editor places the same boxes on the board. Drag moves a box and its edges resize it. The inspector edits value or text, label, labels, unit, scale, align and overflow. It shows one label input per declared enum key. Text pages have no style in points.

## Which page a device gets

A plan is `plans/<name>.yaml` ([ADR 0025](../adr/0025-plaene-je-display.md), [0127](../adr/0127-plan-conditions-are-values.md)):

```yaml
rules:
  - name: tag
    when:
      from: "06:00"
      to: "22:00"
      days: [mo, tu, we, th, fr]
      conditions: [{ value: dexcom/state, is: [low, urgent] }]
    pages: [morning, work]
    on_refresh: cycle
    refresh: 30m
    alert: refresh
fallback: nacht
actions:
  - action: evcc/mode
    args: {loadpoint: 1, mode: pv}
```

Rules are tried in order and the first match wins:

- `from` is inclusive, `to` is exclusive. A window that crosses midnight belongs to its start day.
- `days` accepts German and English day names.
- `conditions` entries all match (AND). Each entry names a value `plugin/value` and `is`, one key or a list of keys. The keys within one entry are alternatives (OR). A missing value, a failing plugin and an unknown key do not match. A condition never creates a boundary: only time windows do ([ADR 0127](../adr/0127-plan-conditions-are-values.md)).
- The old condition string `plugin/condition` is rejected with a hint that shows the value form.
- `timezone` inside a plan is rejected; the global `timezone` applies ([ADR 0033](../adr/0033-global-language-and-timezone.md)). `when.data` of version 1 is rejected with a hint that names `when.conditions`.

A rule lists `pages`, `on_refresh` (what a timed refresh shows) and `refresh` (seconds until the next sync). `on_refresh` is `cycle` (the next page), `first` (the first page) or `current` (the same page), default `current`. A rule change always starts at the first page ([ADR 0130](../adr/0130-what-a-plan-refresh-does.md)). An edit of the page list keeps the device on its page ([ADR 0133](../adr/0133-the-page-cursor-holds-the-page.md)). `UntilNextBoundary` caps the refresh at the next rule change. `fallback` holds the pages of a device whose no rule matches.

`alert` asks the device to signal ([ADR 0131](../adr/0131-a-plan-rule-alerts-the-device.md)). `once` alerts in the sync that starts the rule. `refresh` alerts at the start and on every timed refresh, so a low glucose value reminds at every refresh. A rule without the key stays quiet. The device decides what an alert is: a beep, a blink or both.

A page of a rule that does not fit the device's display type is skipped. That is no file for the type, the other kind, or a text page whose boxes do not fit the character grid. The file the page names decides; a type that names no variant is served by the page file. The skip is logged once per display type, page and reason. No fitting page at all gives a notice page. A broken page is shown as a notice, not skipped. A render that panics is logged with its stack and answered with a notice page.

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
- Long presses inside the menu are ignored. Page changes pause while it is open.
- With the menu closed, a long `next` or `previous` jumps to the first page of the rule. The server decides this at 700 ms; the firmware only reports `hold_ms`.

Paperboy resolves the actions when the menu opens: one `describe` per plugin plus one `actions` request. It caches the answer in `menuState` in the device's state file. The entry keeps the phase (`list`, `confirm`, `running`, `result`), the selection by identity, the offset, the session and the confirmation hash.

An action that needs a confirmation shows `Home = ausführen · Next/Previous = abbrechen` and runs only when the device reports exactly the hash of that screen. The `Ja` and `Nein` cells are the touch targets. Running and result are a banner of an eighth of the display.

An action gets 5 seconds inside the sync. A slower one finishes on its own and a result wake brings the outcome.

Without input, the menu expires after `menu_show_duration` (default 30 seconds, at most 60). Paperboy asks the device for a one-shot wake at that deadline. Every button or touch restarts the deadline. A press that arrives while the device still reports the last served menu screen belongs to the menu, even past the deadline.

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

[ADR 0025](../adr/0025-plaene-je-display.md) (plans) · [0033](../adr/0033-global-language-and-timezone.md) (timezone) · [0044](../adr/0044-text-displays-on-the-device-api.md), [0045](../adr/0045-text-poll-interval-from-the-plan.md) (text displays) · [0049](../adr/0049-plugin-widgets.md) (widgets on a grid) · [0050](../adr/0050-physical-scaling-by-display-ppi.md) (points and ppi) · [0055](../adr/0055-allowed-arg-values-and-gallery-form.md) (argument values) · [0057](../adr/0057-plugin-actions-from-the-plan.md) (actions) · [0067](../adr/0067-buttons-and-the-menu.md), [0078](../adr/0078-the-menu-stays-open-while-it-is-used.md), [0082](../adr/0082-menu-tap-controls-in-a-strip.md) (the menu) · [0092](../adr/0092-scrolling-text-lines.md) (scrolling lines) · [0095](../adr/0095-the-logo-on-pages-and-devices.md) (logo) · [0127](../adr/0127-plan-conditions-are-values.md) (conditions are values) · [0110](../adr/0110-pages-serve-their-kind-and-name-variants.md) (page file and variants) · [0111](../adr/0111-text-pages-from-plugin-values.md) (text pages from values) · [0120](../adr/0120-arguments-for-values.md) (arguments for values) · [0121](../adr/0121-segmented-battery-gauge.md) (the battery gauge) · [0130](../adr/0130-what-a-plan-refresh-does.md) (what a refresh does) · [0131](../adr/0131-a-plan-rule-alerts-the-device.md) (a rule alerts)
