[← Design documents](README.md) · [Paperboy](../../README.md)

# Content

A page is a grid of cells. Every cell shows one widget from one plugin. A plan decides which page a device gets and when.

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
| `grid` | Columns and rows. The reference grid is 6 × 8 cells. |
| `variants` | Display type to file label. The page file only. |
| `style.padding` | Space around the grid in points, default 4.5 |
| `style.rule` | Gap between cells in points, default 0.6 and at least one pixel |
| `style.text_size` | Base text size in points, 4–144, default 16, capped at 0.12 of the display height |
| `style.divider` | Lines that Paperboy draws between the cells |
| `notices` | `show` or `none`: whether a notice page may replace this page |
| `cells` | `at` (column, row), `span` (columns, rows), `widget`, `args`, `style` (overrides the page style), `scroll` |

Parsing is strict and cells must not overlap. Sizes are in points since [ADR 0050](../adr/0050-physical-scaling-by-display-ppi.md). A fraction such as `0.06` is rejected with a hint: points are physical, and the display type's `ppi` converts them into pixels.

A widget declares its sizes as spans of the 6 × 8 reference grid, so an author places cells without knowing the panel.

## Image pages

A page has a pixel size from its display type. `RenderImage` divides that pixel area by the grid, which gives every cell its box, and calls the widget host once for the whole page.

For each cell, in this order:

1. The widget's PNG arrives at exactly the cell size, or the cell carries a notice.
2. Paperboy quantizes the PNG to the gray levels of the display type (2, 4 or 16) with the nearest level, without dithering. A plugin that needs dithering does it itself.
3. The image is pasted one to one. There is no scaling at this point: a widget that returns the wrong size gets a hatched notice with the reason.
4. A result that is older than its `max_age` adds the bar `Veraltet: …` at the bottom of the cell.

The built-in plugin draws text, lines and fills, so a page that is only text needs no image file.

## Text pages

A text page has no pixels. Its file says `kind: text`, and its grid must equal the character grid of the display type. The LCD2004 has 20 × 4, and any text type has at most 80 × 24 ([ADR 0044](../adr/0044-text-displays-on-the-device-api.md)). A text page file serves only the text types with its character grid; other text types need a variant.

`RenderText` asks the same cells, but in characters. Each widget answers lines. Paperboy converts them to the charset of the display and then cuts or pads every line to its cell width. The charsets are `utf8`, `ascii` and `hd44780`; the last keeps ä ö ü ß ° µ and turns Ä Ö Ü into A O U. A widget that fails contributes wrapped `? …` lines instead of breaking the page. A stale result marks `!` in the last column of line 1.

A full-width cell with `scroll: true` passes the flag to its widget. The widget may then answer a line wider than the cell ([ADR 0092](../adr/0092-scrolling-text-lines.md)). Paperboy keeps the first window in `Lines` and the full text in `Scroll`, and hashes both, so a changed tail changes the page. The firmware moves such a row by one character every 300 ms.

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

A page of a rule that does not fit the device's display type is skipped. That is no file for the type, the other kind, or a text page with the wrong grid. The file the page names decides; a type that names no variant is served by the page file. The skip is logged once per display type, page and reason. No fitting page at all gives a notice page. A broken page is shown as a notice, not skipped. A render that panics is logged with its stack and answered with a notice page.

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
| `header` | Title or page title, page indicator, clock, date, subtitle, alignment; the date takes the longest of four forms that fits half the cell, or the free text columns, and is dropped when none fits |
| `text` | Free text, on text displays an overlong line untrimmed when the cell scrolls |
| `clock` | Time, date or both |
| `logo` | The Paperboy mark from its geometry; `wordmark` adds the lockup in Go Mono Bold, image displays only |

## Decisions

[ADR 0025](../adr/0025-plaene-je-display.md) (plans) · [0033](../adr/0033-global-language-and-timezone.md) (timezone) · [0044](../adr/0044-text-displays-on-the-device-api.md), [0045](../adr/0045-text-poll-interval-from-the-plan.md) (text displays) · [0049](../adr/0049-plugin-widgets.md) (widgets on a grid) · [0050](../adr/0050-physical-scaling-by-display-ppi.md) (points and ppi) · [0055](../adr/0055-allowed-arg-values-and-gallery-form.md) (argument values) · [0057](../adr/0057-plugin-actions-from-the-plan.md) (actions) · [0067](../adr/0067-buttons-and-the-menu.md), [0078](../adr/0078-the-menu-stays-open-while-it-is-used.md), [0082](../adr/0082-menu-tap-controls-in-a-strip.md) (the menu) · [0092](../adr/0092-scrolling-text-lines.md) (scrolling lines) · [0095](../adr/0095-the-logo-on-pages-and-devices.md) (logo) · [0103](../adr/0103-plugin-conditions-in-plans.md) (conditions) · [0110](../adr/0110-pages-serve-their-kind-and-name-variants.md) (page file and variants)
