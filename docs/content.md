[← Paperboy](../README.md)

# Pages and plans

## Pages

A page is a grid of cells, and every cell shows one widget (ADR 0049): `pages/<page>.<display type>.yaml`, or `pages/<page>.yaml` for all display types without their own variant.

```yaml
title: Übersicht
grid: [6, 8]                 # columns, rows
style: { padding: 4.5, rule: 0.6, text_size: 16, divider: 0.6 }   # points
notices: show                # show (default) or none
cells:
  - { at: [0, 0], span: [6, 1], widget: paperboy/header }
  - { at: [0, 1], span: [3, 3], widget: weather/current, args: { clothing: true } }
  - { at: [3, 1], span: [3, 3], widget: evcc/site }
  - { at: [0, 4], span: [3, 4], widget: calendar/agenda, args: { lines: 7 }, style: { padding: 2 } }
  - { at: [3, 4], span: [3, 4], widget: weather/days, args: { days: 3 } }
```

- `widget` is `<plugin>/<widget>`. Paperboy passes `args` to the widget unchanged. `at` is the top-left cell (column, row, from 0), and `span` is the size in cells (default `[1, 1]`). Cells must stay inside the grid and must not overlap. Empty cells stay white.
- On an image display the grid divides the pixels. Paperboy asks every plugin for its widgets in exactly the pixel size of the cell and the gray levels of the panel. It quantizes the PNG to the palette (nearest gray, no dithering) and pastes it 1:1, so Paperboy never scales a widget. The PNG has 1 bit or 2 bits (4 gray levels), depending on the display type. A plugin that answers with another size gets a notice in its cell.
- `style` is in points (1 pt = 1/72 inch, ADR 0050). Paperboy passes it to every widget and converts it to pixels with the `ppi` of the display type. `padding` is the inner margin (default 4.5 pt ≈ 1.6 mm). `rule` is the separator width (default 0.6 pt ≈ 0.2 mm, never thinner than 1 px). `text_size` is the base text height (4–144 pt, default 16 pt, at most 0.12 of the display height), so widgets of different plugins match. `divider` (points, at least 1 px, default 0 = none) draws lines between the cells; Paperboy draws them, not the widgets. The `style` of a cell overrides the page style.
- The **built-in plugin** `paperboy` needs no configuration and provides `paperboy/header`, `paperboy/text`, `paperboy/clock` and `paperboy/logo` (see [docs/plugins/paperboy.md](plugins/paperboy.md)).
- `kind: image` (default) or `kind: text` says which displays a page is for. A plan may serve image and text displays alike. When a device pages through a rule, Paperboy skips pages that do not fit its display type. A page does not fit when it has no variant and no shared page or when it has the other kind. It also does not fit when its text grid is not the character grid of the display. Paperboy logs each skip once per display type and page. When no page of the rule fits, the device gets a notice (*No matching page*). Paperboy does not skip a broken page; it shows its error as a notice.
- **Stale widgets**: when a plugin fails, Paperboy keeps showing its last widgets and marks the page. An image display gets a bar (*Outdated: calendar/agenda*, German *Veraltet: …*), a text display gets `!` in the last column of the first line. `notices: none` turns both off. A widget that never rendered becomes a hatched notice inside its cell with the reason. A plugin that cannot draw in its cell at all answers `cell too small: <width>x<height>` instead of leaving the cell empty, so a cell that is too small for its widget shows that notice.
- Preview without a device: `GET /api/preview/<display type>/<page>` (PNG, or `text/plain` for text displays; an optional `?at=YYYY-MM-DDTHH:MM` renders the page as at that time in the configured timezone). `GET /api/profiles` lists the display types.
- The page editor at `/ui/editor` builds a page from the same model (see [Web UI](runtime.md#web-ui)).

### Text pages

A page for a text display (display type `kind: text`, ADR 0044) says `kind: text`. Its grid is the character grid and must match the display size, and cells are measured in characters:

```yaml
kind: text
grid: [20, 4]
cells:
  - { at: [0, 0], span: [20, 1], widget: paperboy/header }
  - { at: [0, 1], span: [20, 1], widget: weather/current }
  - { at: [0, 2], span: [20, 2], widget: calendar/agenda }
```

Plugins answer with lines for the cell. Paperboy converts them to the `charset` of the display type, cuts them to the cell width and pads them. `ascii` transliterates (ä → ae, ° → o, € → EUR). `hd44780` keeps `ä ö ü ß ° µ` for the ROM mapping of the firmware, writes `Ä Ö Ü` as `A O U` (the ROM has no capital umlauts, and the width must stay) and transliterates the rest. Set `charset: hd44780` for HD44780 displays; with the default `utf8` the firmware has to guess. A widget without a text variant shows its error in the cell. A cell that spans the whole display width may set `scroll: true` (ADR 0092): the widget then gets `scroll` in its request and may answer a line that is wider than the cell, Paperboy shows the first columns, and firmware 2.2.0 for text displays moves the rest as a marquee. Without the flag a long line is cut; `examples/pages/hinweise.lcd2004.yaml` shows the scrolling line.

The pages in `examples/pages` (`uebersicht`, `kalender`, `wetter`, `laden` and the 20 × 4 variants `uebersicht.lcd2004` and `laden.lcd2004`) use the shipped plugins. `examples/plans/standard.yaml` pages through them.

## Plans

Which pages a device shows is decided by a plan, `<name>.yaml` in `plans/`. A device points to it with `plan: wohnzimmer` in its `devices/<id>.yaml`, and several devices may share a plan. A plan only names pages, so it works for devices of different display types. Create and edit a plan in the browser at `/ui/editor` (ADR 0069, 0072): the *Plans* tab creates one, `/ui/editor/plan?name=<plan>` edits it, and the device page assigns it (ADR 0070). When you delete a plan, a copy stays under `state/editor/`; the confirmation names the devices that still use it.

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
- Times use the global `timezone` from the configuration (ADR 0033). Plans have no timezone of their own, and Paperboy rejects a `timezone` key.
- `pages` is the paging order within the rule. `rotate: true` moves on at every timed refresh; otherwise only a button changes the page. Buttons page within the active rule only, a long press returns to its first page, and after `page_timeout` the device shows the first page again unless the rule rotates.
- `refresh` replaces the device interval. Paperboy caps the reported interval to the next rule boundary (but never below the floor of the power source of the device), so a sleeping device wakes up for a rule change.
- `fallback` names the rule used when nothing matches. Without a match and without a fallback, the device gets a notice page.
- A rule needs a name (a-z, 0-9, `-`) and at least one page. Rule names must be unique, and a rule must not name the same page twice.
- Paperboy reloads plans on change without a restart; a broken file keeps the last valid version. A missing or broken page also leads to a notice page, so devices never see a server error.
- Plans serve devices on the Paperboy firmware.

### Conditions

A rule can depend on **conditions** that plugins declare, such as "the glucose reading is low" (ADR 0103). The condition names are `plugin/condition`; a plugin declares them in its `describe` answer, and the plugin configuration holds the thresholds, so the page switch and the widget's colouring always agree.

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
- `when.data` from ADR 0025 is rejected with a hint, because the shared data document no longer exists.

Dexcom offers `urgent`, `low` (including the urgent range) and `high`, following its configured thresholds and inactive while the reading is older than 15 minutes; see [the dexcom page](plugins/dexcom.md) for an example plan.

### Actions

A plan may offer **actions** that plugins provide (ADR 0057): things a user starts from the device menu, such as switching the evcc charge mode. Actions belong to the plan, not to a page, so every rule and page of a plan offers the same list.

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
- Paperboy rejects exact duplicates of `action` and `args`. An unknown plugin, action or argument disables the entry with a reason instead of breaking the plan. Paperboy refuses a plugin that reports another protocol version as a whole (ADR 0058); it does not use the plugin without its actions.
- Paperboy rejects an `actions` key on a rule or a page with a hint.

#### The device menu

A short `home` always opens the menu. It also opens when the plan offers no actions; then it shows a *No actions* (*Keine Aktionen*) banner instead of the action grid. A device with `actions: deny` has no menu. A text display, a device with a critical battery and a device without a plan have no menu either. Paperboy draws the menu as an overlay over the top half of the current page. It has one row with `X`, up to three actions and `<`/`>`, stacked in the same order on portrait displays. The page below stays visible, and the menu pauses rotation and `page_timeout`.

- `next`/`previous` move the selection and wrap. `<`/`>` page through the list in steps of three.
- `home` runs the selected action. If the plugin asks for a confirmation (`confirm` in its `describe`), Paperboy shows a confirmation screen first. It runs the action only after the device reports that screen as painted (`shown` hash): the first `home` opens the menu or the confirmation, it never runs an action blind.
- A menu without a runnable action, and `home` on a disabled action, show the reason of the plugin as a banner.
- A long `home` closes the menu and keeps the page. On a device with `touch: true` the menu draws a control strip under the actions: `X` closes it, `<`/`>` page the list, and `next` past the last action selects the `X`. On a button-only device the strip is not drawn at all (ADR 0082). A tap on a confirming action opens its confirmation first; a tap never opens the menu.
- With the menu closed, a long `next` or `previous` goes to the first page of the active rule. Paperboy ignores long presses inside the menu, so the direction buttons only move the selection there.
- Unavailable actions stay visible, hatched, with the reason of the plugin. A plugin that is down keeps its actions visible as unavailable instead of hiding them.
- The menu closes itself after `menu_show_duration` (default 30 s, at most 60 s). It asks for a one-shot wake at that deadline (`wake` in the sync answer, ADR 0064), so it closes without waiting for the next regular refresh. The empty banner closes after ten seconds at the latest; any input dismisses it.
- An action gets about five seconds inside the sync. If it is not done by then, a thin banner at the top says *Running …* (*Wird ausgeführt …*), and Paperboy schedules one result wake (`reason: result`) for the outcome. The outcome is a banner too (*Done: …* or *Failed: …*, German *Erledigt: …* or *Fehlgeschlagen: …*) without cells or a close box. The next input or the next regular sync clears it.
- With `actions: deny`, `home` goes to page 1, and the dashboard marks the device as denied.
- Plugin actions set target states (e.g. `mode: now`), never toggles, so a repeated sync is safe.
