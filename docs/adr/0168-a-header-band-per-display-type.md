# 0168: A header band per display type

- Status: Accepted, implemented
- Date: 2026-11-25
- Supersedes: the three zones of [ADR 0167](0167-a-header-band-outside-the-page-grid.md)
- Answers: issue #455

## Context

ADR 0167 put the header in the page. Every page repeated its band, and every
variant repeated it again. The band is mostly chrome: title, clock, date,
battery and room readings. The title is the only page-specific part, and
`paperboy/title` already reads the page title.

The band also depends on the panel: its height and its usable width. That is
the display type, not the page.

A relative width made small parts grow with the band. A battery then took a
share of the width instead of the width it needs. The widgets are static, so a
fixed width is enough; one part fills the remaining width.

## Decision

1. **The header belongs to the display type.** `displays/<name>.yaml` carries
   `header.height` and `header.parts`. Every page of that type uses it.
2. **A page only hides it.** `header: false` hides the band; an absent key
   shows it. A page does not carry header parts.
3. **A part either fills or has a fixed width.** `fill: true` shares the space
   that the fixed parts leave. `width` is in points, like the height and the
   page style. The order of `parts` is the visual order.
4. **The old zone keys and the weight are rejected.** `header.left`,
   `header.center`, `header.right` and `weight` answer a hint for `parts`,
   `fill` and `width`.
5. **The display type editor owns the band.** It lists one row per part: a
   select with the image widgets grouped by plugin, the `width` or `fill`, and
   the declared argument fields of the selected part. A server-rendered
   preview shows the band. The page editor draws the band as part of the
   preview image, not as a cell, and offers the *Show header* switch for the
   page.
6. **The band follows the display type in the renderer.** Device scoping,
   demand and the action rectangles read the band of the profile. A page that
   hides it takes the full display.

## Consequences

- One band per panel instead of one per page and variant.
- The title stays page-specific; the battery and the readings stay
  device-specific because `server_status` is filled per device.
- A page that hides the band gives its space to the grid.
- A page cannot carry a different header on its own. A page-specific header
  would be a later override.
- A fixed part keeps its physical width on every page of the type. The fill
  part absorbs the panel width.
- The argument fields are shared with the page editor, so a widget's arguments
  look the same in both editors.
- The preview composes the same parts as the device path. It uses the saved
  panel size, ppi and gray levels, because the editor previews a draft header,
  not a draft display type.
- The display type editor edits the band as a list, not on the board. The page
  editor needs no header write and no header lock.
- Firmware and the plugin protocol do not change.
