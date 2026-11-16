# 0121: A segmented gauge for the battery

- Status: Accepted
- Date: 2026-11-16
- Related: ADR 0020 (battery warning), ADR 0050 (physical scaling), ADR 0115 (device status in a widget), issue #275

## Context

ADR 0115 gave Paperboy the widgets `battery` and `device-status` and a `battery` switch on the header. The `battery` widget drew a small icon whose size followed the page's text size. A wide or tall cell only added white space around the same drawing. A narrow cell dropped the value, and then stretched the icon past the padding.

The gauge itself was hard to read. The charge was one continuous bar with a white line to the outline, so 84 % and 100 % differed by a thin strip. The charging mark cut a band out of the fill and drew a chevron, which reads as `<`. An empty battery and a device without a reading looked the same. The warning level of the device state stayed unused.

ADR 0115 point 5 asks for "an icon and percentage" in the header. The percentage repeats a value that the page shows elsewhere. ADRs are not rewritten, so this decision needs its own record.

## Decision

1. **The gauge has segments.** The body holds four or five segments, each filled or empty, and an outline of one or two pixels. A step is wider than one pixel, so 84 % and 100 % differ at a glance. The terminal is half the icon height and one eighth of its width.
2. **The gauge follows the cell.** The padded cell height sizes the icon, not the page's text size. The value takes its size from the icon, so both read as one block. The icon never grows past the padded height, and a lone icon keeps its normal shape instead of stretching to the cell width. ADR 0050 keeps its rule: sizes are physical, and the cell supplies the height.
3. **The value wins.** When the cell is too narrow for both, the value shrinks first. The gauge is dropped only when the value cannot shrink any more. A number alone says more than a battery symbol alone.
4. **The charging mark is a bolt over the fill.** Paperboy inverts the pixels of a bolt-shaped polygon. The mark is white on the charge and black on the empty part, and it keeps the fill around it.
5. **A missing reading is marked.** A gauge without a percentage carries a diagonal slash inside the outline, so it does not look like 0 %.
6. **The warning level thickens the outline.** `warn` adds one pixel to the outline, `critical` adds two. The level is visible without a number and without a new argument.
7. **The header draws the gauge only.** On an image display, `battery: true` draws the gauge at the far right and no percentage or voltage. The gauge sits on the text baseline, so the gauge and the date read as one line. The date and the clock move left by the gauge width and the gap only. A text display has no gauge and keeps the number (`84 %`), because the flag would otherwise show nothing. This supersedes ADR 0115 point 5.
8. **`paperboy/battery` declares a square span.** `2x2` gives the gauge above a large value. `1x1`, `2x1` and `3x1` keep the inline form beside the value. A cell that is wider than three times its height is inline.

## Alternatives

- **One bar with a stronger outline:** easier to draw, but 84 % and 100 % still differ by a thin strip only.
- **Inverted digits inside the gauge:** dropped. The header gauge is about 16 pixels high on a page with 16 pt text, and the charge area is smaller still.
- **A stricter level mark, an exclamation beside the value:** it needs room that a narrow cell does not have, and it repeats the battery warning page of ADR 0020. The outline carries the level everywhere.
- **A simpler gauge for the header:** the same drawing keeps one implementation and one reading. The header gauge is small but shows the segments.
- **Keep the value in the header:** the page already shows a number for its device, so the header repeated it.

## Consequences

- A `1x1` or `2x2` cell now draws a gauge that fills the cell. On a page of text-sized widgets the gauge becomes the loudest element; the author chooses the span.
- The gauge reads in steps of about 20 %. The number beside it keeps the exact value.
- A device without a battery reading now shows a marked gauge instead of a bare outline; a page with `label: none` shows the missing reading too.
- The header's right side is narrower, so the date keeps more of its long forms.
- The gauge and the level mark are drawing rules of the built-in plugin. A later external plugin repeats them or gets a shared helper.
