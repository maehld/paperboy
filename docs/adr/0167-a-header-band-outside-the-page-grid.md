# 0167: A header band outside the page grid

- Status: Accepted, implemented
- Date: 2026-11-25
- Amends: [ADR 0049](0049-plugin-widgets.md) (pages are grids)
- Builds on: [ADR 0115](0115-device-status-in-a-widget.md) (the header shows this device), [ADR 0163](0163-the-readings-in-the-device-status-and-a-reading-widget.md) (a reading widget)
- Answers: issue #298

## Context

The old `paperboy/header` is a normal widget with a fixed cell span of `6 × 1`
or `4 × 1`. Every page places it in a cell. The header therefore ties the page
layout to its own height. A coarse grid gives the header a high row, and the
header cannot use a finer row than the content.

The header also has too little room for new parts. The clock, date and battery
compete for the right side. A room reading needs a place next to them
([ADR 0163](0163-the-readings-in-the-device-status-and-a-reading-widget.md)).

## Decision

1. **A page may reserve a header band above its grid.** The page file carries a
   `header` object. The band is part of the page, not of the display type. A
   page without the key keeps the full grid and the old widget.
2. **The band has a height in points.** The default is 24 pt. The display's
   `ppi` converts the height to pixels ([ADR 0050](0050-physical-scaling-by-display-ppi.md)).
   The grid divides only the area below the band.
3. **The band has three zones: `left`, `center` and `right`.** The zones decide
   the order of the parts. Each part is a normal widget with its own `args`.
   A `weight` of 1 to 6 divides the band width; the default is 1.
4. **A header part is a widget, not a new part vocabulary.** Any plugin widget
   can stand in the band. The built-in plugin adds `title` for the page title
   and the page indicator. `paperboy/clock` gains `format: auto`, which takes
   the longest date form that fits and adds the time. A room reading is
   `paperboy/reading`, and `paperboy/battery` stays available.
5. **`paperboy/header` stays for existing pages.** New pages use the band.
   A page may still place the old widget in a cell.
6. **A text page has no header band.** Its character grid keeps the rows. The
   built-in `paperboy/title` value stays the way a text page shows its title.
7. **The band takes part in device scoping and demand.** A `paperboy/reading`
   or `paperboy/battery` part without a `device` argument renders per device,
   like the same widget in a cell ([ADR 0115](0115-device-status-in-a-widget.md)).
   A plugin of a header part is warm like a plugin of a cell.
8. **The action mode keeps the band.** Cell rectangles start below the band, so
   taps and the action view use the new position. Header parts carry no
   action.

## Consequences

- A page author places a header once and can drag widgets into its three zones.
  The grid stays coarse.
- The band costs vertical space on every page that uses it.
- A variant file carries its own header. A page and its variants repeat the
  band, because a variant owns its layout.
- A header part can fail like a cell. It then shows its notice inside its zone.
- The editor shows the band above the grid and offers the three zones. The
  height and the part weights are page fields.
- A short band can drop the age of a reading. The operator can set `age: false`
  or use a taller band.
- Firmware and the plugin protocol do not change.
