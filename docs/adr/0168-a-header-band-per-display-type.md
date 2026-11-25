# 0168: A header band per display type

- Status: Accepted, implemented
- Date: 2026-11-25
- Supersedes: the three zones and the free part list of [ADR 0167](0167-a-header-band-outside-the-page-grid.md)
- Answers: issue #455

## Context

ADR 0167 put the header in the page. Every page repeated its band, and every
variant repeated it again. The band is mostly chrome: title, clock, date,
battery and room readings. The title is the only page-specific part, and
`paperboy/title` already reads the page title.

The band also depends on the panel: its height and its usable width. That is
the display type, not the page.

A free list of widget parts made the band flexible but the editor heavy: the
operator picked widgets, widths, fill and arguments for chrome that is always
the same. A fixed band with switches keeps the display type file short and the
editor to a few choices.

## Decision

1. **The header belongs to the display type.** `displays/<name>.yaml` carries
   `header.height` and the switches `temperature`, `humidity`, `clock` and
   `battery`. Every page of that type uses it.
2. **The band is fixed chrome.** The left group is the page title and its page
   indicator. The right group is right-aligned and holds the temperature, the
   humidity, the clock and the battery, in that order from left to right. A
   pipe separates the temperature, the humidity and the clock. The
   page indicator follows the title after one space.
3. **Each part is a switch.** `page_indicator`, `temperature`, `humidity` and
   `battery` are booleans; `clock` is `time`, `date`, `datetime` or `none`,
   default `time`. The title is always on.
4. **A page only hides it.** `header: false` hides the band; an absent key
   shows it. A page does not carry header settings.
5. **The parts share one text size.** Paperboy composes the built-in widgets
   `paperboy/title`, `paperboy/reading`, `paperboy/clock` and
   `paperboy/battery` with widths in points relative to the band height. The
   title, the readings and the clock use the same size; only the title is
   bold. The battery gauge stays a little smaller than the text and shows no
   percentage. The title fills the rest, so the right group keeps its
   physical size.
6. **Every image display type has a band.** The display type editor offers the
   height and the five switches and a server-rendered preview. The preview
   shows page 1 of 2, so the page indicator is visible. There is no add or
   remove: a page hides the band, the type always has one. The page editor
   draws the band as part of the preview image, not as a cell, and offers the
   *Show header* switch for the page.
7. **The old keys and widgets are gone.** `parts`, `left`, `center`, `right`
   and `weight` answer a hint for the switches. The `paperboy/header` and
   `paperboy/title` widgets are removed; the band draws the title itself as
   one internal widget. The text value `paperboy/title` stays for text pages.
8. **The band follows the display type in the renderer.** Device scoping,
   demand and the action rectangles read the fixed parts of the profile. A
   page that hides it takes the full display.

## Consequences

- One band per panel instead of one per page and variant.
- The title stays page-specific; the battery and the readings stay
  device-specific because `server_status` is filled per device.
- A page that hides the band gives its space to the grid.
- A page cannot carry a different header on its own. A page-specific header
  would be a later override.
- The operator cannot place arbitrary widgets in the band. A new fixed part
  needs a change to the band and to its editor.
- Pages that used the old `paperboy/header` cell lose it; the band replaces
  that row. The example pages move their content up by one grid row.
- Every image display type has a band. To show no band on a panel, every page
  of the type writes `header: false`; there is no remove button.
- A reading part stays in place when the device has no such reading; the
  widget shows its notice there. The operator enables a reading only for
  devices that measure it.
- The argument fields of a cell stay in the page editor. The display type
  editor needs no widget schema for the band.
- The preview renders the same fixed parts as the device path. It uses the
  saved panel size, ppi and gray levels, because the editor previews a draft
  header, not a draft display type.
- Firmware and the plugin protocol do not change.
