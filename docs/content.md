[← Paperboy](../README.md)

# Pages and plans

## Pages

A page is a grid of cells, and every cell shows one widget ([ADR 0049](adr/0049-plugin-widgets.md)). The page file `pages/<page>.yaml` carries the layout and serves every display type of its kind. Its `variants` map names the exceptions ([ADR 0110](adr/0110-pages-serve-their-kind-and-name-variants.md)): `sticky: hochkant` means that `pages/<page>.hochkant.yaml` serves the sticky display type. The variant file carries the layout without the title and without `variants`.

```yaml
title: Übersicht
kind: image
grid: [6, 8]                 # columns, rows
variants: { sticky: hochkant }
style: { padding: 4.5, rule: 0.6, text_size: 16, divider: 0.6 }   # points
notices: show                # show (default) or none
cells:
  - { at: [0, 0], span: [6, 1], widget: paperboy/header }
  - { at: [0, 1], span: [3, 3], widget: weather/current, args: { clothing: true } }
  - { at: [3, 1], span: [3, 3], widget: evcc/site }
  - { at: [0, 4], span: [3, 4], widget: calendar/agenda, args: { lines: 7 }, style: { padding: 2 } }
  - { at: [3, 4], span: [3, 4], widget: weather/days, args: { days: 3 } }
```

```yaml
# pages/uebersicht.hochkant.yaml: the same screen on the portrait panel
kind: image
grid: [6, 8]
cells:
  - { at: [0, 0], span: [6, 1], widget: paperboy/header }
```

- `widget` is `<plugin>/<widget>`. Paperboy passes `args` to the widget unchanged. `at` is the top-left cell (column, row, from 0), and `span` is the size in cells (default `[1, 1]`). Cells must stay inside the grid and must not overlap. Empty cells stay white.
- On an image display the grid divides the pixels. Paperboy asks every plugin for its widgets in exactly the pixel size of the cell and the gray levels of the panel. It quantizes the PNG to the palette (nearest gray, no dithering) and pastes it 1:1, so Paperboy never scales a widget. The PNG has 1, 2 or 4 bits (2, 4 or 16 gray levels), depending on the display type. A plugin that answers with another size gets a notice in its cell.
- `style` is in points (1 pt = 1/72 inch, [ADR 0050](adr/0050-physical-scaling-by-display-ppi.md)). Paperboy passes it to every widget and converts it to pixels with the `ppi` of the display type. `padding` is the inner margin (default 4.5 pt ≈ 1.6 mm). `rule` is the separator width (default 0.6 pt ≈ 0.2 mm, never thinner than 1 px). `text_size` is the base text height (4–144 pt, default 16 pt, at most 0.12 of the display height), so widgets of different plugins match. `divider` (points, at least 1 px, default 0 = none) draws lines between the cells; Paperboy draws them, not the widgets. The `style` of a cell overrides the page style.
- The **built-in plugin** `paperboy` needs no configuration and provides `paperboy/header`, `paperboy/text`, `paperboy/clock` and `paperboy/logo` (see [docs/plugins/paperboy.md](plugins/paperboy.md)).
- `kind: image` or `kind: text` is required and says which displays a file is for. A plan may serve image and text displays alike. When a device pages through a rule, Paperboy skips pages that do not fit its display type. A page does not fit when no file serves the type or when it has the other kind. It also does not fit when its text rows are more or wider than the character grid of the display. Paperboy logs each skip once per display type and page. When no page of the rule fits, the device gets a notice (*No matching page*). Paperboy does not skip a broken page; it shows its error as a notice.
- **Stale widgets**: when a plugin fails, Paperboy keeps showing its last widgets and marks the page. An image display gets a bar (*Outdated: calendar/agenda*, German *Veraltet: …*), a text display gets `!` in the last column of the first line. `notices: none` turns both off. A widget that never rendered becomes a hatched notice inside its cell with the reason. A plugin that cannot draw in its cell at all answers `cell too small: <width>x<height>` instead of leaving the cell empty, so a cell that is too small for its widget shows that notice.
- Preview without a device: `GET /api/preview/<display type>/<page>` (PNG, or `text/plain` for text displays; an optional `?at=YYYY-MM-DDTHH:MM` renders the page as at that time in the configured timezone). `GET /api/profiles` lists the display types.
- The page editor at `/ui/editor` builds a page from the same model (see [Web UI](runtime.md#web-ui)).

### Text pages

A page for a text display (display type `kind: text`, [ADR 0044](adr/0044-text-displays-on-the-device-api.md)) says `kind: text` and carries `rows` ([ADR 0111](adr/0111-text-pages-from-plugin-values.md)). Every row is a list of cells, and every cell shows one value a plugin declares:

```yaml
title: Werte
kind: text
rows:
  - [{ value: paperboy/title, width: 20, align: left }]
  - [{ value: paperboy/date, label: Datum, width: 20, align: left }]
  - [{ value: paperboy/time, label: Zeit, width: 10, align: right },
     { value: paperboy/now, width: 10, align: decimal }]
  - [{ value: paperboy/now, label: Jetzt, width: 20, align: decimal }]
```

- `value` is `<plugin>/<key>`, a value the plugin declares in its `describe` answer. Paperboy asks every plugin once for all keys of the page and caches the answer until its `max_age` or a `changed` event.
- `label` and `unit` are text in front of and behind the value. A number uses the unit the plugin declares when the cell names none.
- `width` is the number of characters the cell takes. `align` is `left` (default), `right` or `decimal`; `decimal` keeps the decimal separators of cells with the same unit in one column.
- Paperboy formats the value itself. A number takes the `precision` of the plugin and the language's decimal separator (comma for `de`, point for `en`). A duration becomes `45s`, `12min`, `3h 20min` or `2d 4h`. A timestamp shows its time today, its date at midnight and date and time on another day. A direction becomes `^`, `>` or `v`.
- `grid` and `cells` are rejected on a text page with a hint: the character grid belongs to the display.
- Paperboy converts label, value and unit to the `charset` of the display type before it cuts or pads them. `ascii` transliterates (ä → ae, ° → o, € → EUR). `hd44780` keeps `ä ö ü ß ° µ` for the ROM mapping of the firmware, writes `Ä Ö Ü` as `A O U` (the ROM has no capital umlauts, and the width must stay) and transliterates the rest. Set `charset: hd44780` for HD44780 displays; with the default `utf8` the firmware has to guess.
- A value no plugin declares shows `? …` in its cell. A stale value marks `!` in the last column of the first line; `notices: none` turns that off.
- A text page fits a display when its rows fit the character grid: at most as many rows as the display has, and every row at most as wide as the display. Other sizes need a variant.
- The page editor shows only the selected cell in the right column and puts the rows in a compact panel under the palette: add, move and remove rows, select a value chip, or delete a cell. Per cell you pick the value from the declared values and set `label`, `unit`, `width` and `align`. The palette lists the values grouped by plugin: click a value and then a slot, or drag it onto a row. On the board, drag a cell to reorder it and drag its right edge to change its width. A row without cells stays a blank line. Lists (`for-each`) and scrolling rows follow in later steps of ADR 0111.

The built-in plugin declares `paperboy/title`, `paperboy/time`, `paperboy/date` and `paperboy/now`, so a text page has data without another plugin. The shipped plugins declare values in a later protocol version; until then the 20 × 4 examples in `examples/pages` (`uebersicht.lcd2004`, `laden.lcd2004` and the others) use the built-in values. `uebersicht.yaml` and `laden.yaml` name their text variant in `variants`, and `examples/plans/standard.yaml` pages through the pages.

## Plans

Which pages a device shows is decided by a plan, `<name>.yaml` in `plans/`. A device points to it with `plan: wohnzimmer` in its `devices/<id>.yaml`, and several devices may share a plan. A plan only names pages, so it works for devices of different display types. Create and edit a plan in the browser at `/ui/editor` ([ADR 0069](adr/0069-plan-editor-ui.md), [0072](adr/0072-pages-as-qoi.md)): the *Plans* tab creates one, `/ui/editor/plan?name=<plan>` edits it, and the device page assigns it ([ADR 0070](adr/0070-devices-in-their-own-files.md)). When you delete a plan, a copy stays under `state/editor/`; the confirmation names the devices that still use it.

```yaml
rules:
  - name: morgen
    when: { from: "07:00", to: "09:00", days: [mo, di, mi, do, fr] }
    pages: [kalender, uebersicht]
  - name: tag
    when: { from: "09:00", to: "19:00" }
    pages: [uebersicht, wetter, energie]
    rotate: true
    refresh: 5m
  - name: nacht
    when: { from: "19:00", to: "07:00" }
    pages: [uebersicht]
    refresh: 30m
fallback: nacht
```

- The first matching rule wins. A `when` without `from`/`to` always matches. `from` is inclusive, `to` exclusive. `to < from` wraps over midnight and then belongs to the day the window starts. Days accept German and English short names.
- Times use the global `timezone` from the configuration ([ADR 0033](adr/0033-global-language-and-timezone.md)). Plans have no timezone of their own, and Paperboy rejects a `timezone` key.
- `pages` is the paging order within the rule. `rotate: true` moves on at every timed refresh; otherwise only a button changes the page. Buttons page within the active rule only, a long press returns to its first page, and after `page_timeout` the device shows the first page again unless the rule rotates.
- `refresh` replaces the device interval. Paperboy caps the reported interval to the next rule boundary (but never below the floor of the power source of the device), so a sleeping device wakes up for a rule change.
- `fallback` names the rule used when nothing matches. Without a match and without a fallback, the device gets a notice page.
- A rule needs a name (a-z, 0-9, `-`) and at least one page. Rule names must be unique, and a rule must not name the same page twice.
- Paperboy reloads plans on change without a restart; a broken file keeps the last valid version. A missing or broken page also leads to a notice page, so devices never see a server error.
- Plans serve devices on the Paperboy firmware.

### Conditions

A rule can depend on **conditions** that plugins declare, such as "the glucose reading is low" ([ADR 0103](adr/0103-plugin-conditions-in-plans.md)). The condition names are `plugin/condition`; a plugin declares them in its `describe` answer, and the plugin configuration holds the thresholds, so the page switch and the widget's colouring always agree.

```yaml
rules:
  - name: unterzucker
    when: { conditions: [dexcom/low] }
    pages: [zucker]
  - name: tag
    when: { from: "07:00", to: "22:00" }
    pages: [uebersicht]
fallback: tag
```

- Conditions combine with `from`/`to`/`days` as AND, and the first matching rule wins. A rule with only conditions matches whenever they are active.
- A plugin that is not configured, a name it does not declare, a failing answer and a stale value all count as **inactive**, so missing data never raises an alarm page; the plan falls through to its next rule.
- Paperboy asks the plugins named in a plan before it selects the page and caches the answer briefly. A change reaches a device at its next wake, so the `refresh` of the rule that shows the normal page sets how quickly.
- The plan editor offers the declared conditions as check boxes per rule, and the plan view shows each condition as active or inactive (`if dexcom/low`). The plan view evaluates conditions at the current moment, even when you preview another time.
- `when.data` from [ADR 0025](adr/0025-plaene-je-display.md) is rejected with a hint, because the shared data document no longer exists.

Dexcom offers `urgent`, `low` (including the urgent range) and `high`, following its configured thresholds and inactive while the reading is older than 15 minutes; see [the dexcom page](plugins/dexcom.md) for an example plan.

### Actions

A plan may offer **actions** that plugins provide ([ADR 0057](adr/0057-plugin-actions-from-the-plan.md)): things a user starts from the device menu, such as switching the evcc charge mode. Actions belong to the plan, not to a page, so every rule and page of a plan offers the same list.

```yaml
actions:
  - action: evcc/mode
    args: { loadpoint: 0, mode: now }
    id: charge-now
    title: "Charge now"          # optional; otherwise the title of the plugin
  - action: evcc/mode
    args: { loadpoint: 1, mode: now }
    id: charge-now-2             # required: evcc/mode appears twice
```

- `action` is `plugin/action`. `args` are the arguments of the action from the `describe` answer of the plugin. An action without arguments can be a single string instead of a map.
- `id` is optional and plan-local. It becomes mandatory for **every** entry of an action as soon as the same `action` appears more than once, and IDs must be unique. The menu keeps its selection by `id`, or by `plugin/action` when there is no `id`, so reordering the list or changing `args`/`title` does not change the identity.
- Paperboy rejects exact duplicates of `action` and `args`. An unknown plugin, action or argument disables the entry with a reason instead of breaking the plan. Paperboy refuses a plugin that reports another protocol version as a whole ([ADR 0058](adr/0058-plugin-protocol-version-check.md)); it does not use the plugin without its actions.
- Paperboy rejects an `actions` key on a rule or a page with a hint.

#### The device menu

A short `home` always opens the menu. It also opens when the plan offers no actions; then it shows a *No actions* (*Keine Aktionen*) banner instead of the action grid. A device with `actions: deny` has no menu. A text display, a device with a critical battery and a device without a plan have no menu either. Paperboy draws the menu as an overlay over the top half of the current page. It has one row with `X`, up to three actions and `<`/`>`, stacked in the same order on portrait displays. The page below stays visible, and the menu pauses rotation and `page_timeout`.

- `next`/`previous` move the selection and wrap. `<`/`>` page through the list in steps of three.
- `home` runs the selected action. If the plugin asks for a confirmation (`confirm` in its `describe`), Paperboy shows a confirmation screen first. It runs the action only after the device reports that screen as painted (`shown` hash): the first `home` opens the menu or the confirmation, it never runs an action blind.
- A menu without a runnable action, and `home` on a disabled action, show the reason of the plugin as a banner.
- A long `home` closes the menu and keeps the page. On a device with `touch: true` the menu draws a control strip under the actions: `X` closes it, `<`/`>` page the list, and `next` past the last action selects the `X`. On a button-only device the strip is not drawn at all ([ADR 0082](adr/0082-menu-tap-controls-in-a-strip.md)). A tap on a confirming action opens its confirmation first; a tap never opens the menu.
- With the menu closed, a long `next` or `previous` goes to the first page of the active rule. Paperboy ignores long presses inside the menu, so the direction buttons only move the selection there.
- Unavailable actions stay visible, hatched, with the reason of the plugin. A plugin that is down keeps its actions visible as unavailable instead of hiding them.
- The menu closes itself after `menu_show_duration` (default 30 s, at most 60 s). It asks for a one-shot wake at that deadline (`wake` in the sync answer, [ADR 0064](adr/0064-device-protocol-v4.md)), so it closes without waiting for the next regular refresh. The empty banner closes after ten seconds at the latest; any input dismisses it.
- An action gets about five seconds inside the sync. If it is not done by then, a thin banner at the top says *Running …* (*Wird ausgeführt …*), and Paperboy schedules one result wake (`reason: result`) for the outcome. The outcome is a banner too (*Done: …* or *Failed: …*, German *Erledigt: …* or *Fehlgeschlagen: …*) without cells or a close box. The next input or the next regular sync clears it.
- With `actions: deny`, `home` goes to page 1, and the dashboard marks the device as denied.
- Plugin actions set target states (e.g. `mode: now`), never toggles, so a repeated sync is safe.
