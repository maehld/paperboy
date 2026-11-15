# 0118: Text boxes and overflow

- Status: Accepted
- Date: 2026-11-15
- Related: ADR 0044 (text displays), ADR 0045 (text poll interval), ADR 0085 (moving and resizing widgets), ADR 0111 (text pages from plugin values), ADR 0117 (text cell columns)

## Context

ADR 0117 gave every text cell its column, but a row was still a list of cells that had to keep their order. A cell could not own a rectangle: a tall value or a wrapped paragraph needed several rows, and a label next to a two-line block needed a second box. The next step is the image page model in characters: a box at a column and row with a width and height.

Text content is not one shape. A clock is one short line. A message is a paragraph that should wrap. A station name is a single line that is wider than the display. A scrolling marquee needs the full text and a window. The page must say which of those it wants, per box, with a useful default.

## Decision

1. **A text page is a character grid of boxes.** `kind: text` carries `cells` with `at` and `span` in character columns and rows, like an image page in grid cells. There is no page `grid`: the display type supplies the columns and rows. The parser rejects overlaps and boxes outside the largest text grid; `Fits` uses the display type.
2. **A box carries literal text or a value.** Exactly one of `text` and `value` is set. A literal text cell has no label or unit; a value cell keeps `label`, `unit` and `align`. YAML multi-line strings work, so a label can be a block.
3. **Overflow is strip, wrap or scroll.** The page sets the default with `overflow`; a cell overrides it.
   - `strip` cuts the content after the box's width and height.
   - `wrap` reflows text into the box width at word boundaries, then cuts at its height.
   - `scroll` keeps the full content and moves it: horizontally when a line is wider than the box, vertically when there are more lines than the box. Both together scroll vertically by lines first.
4. **The renderer returns the window and the full scroll text.** A scrolling box carries the full lines, the first window and a direction. The text answer exposes them to the firmware and the virtual display. A legacy per-row `scroll` field remains for a horizontal single-row box.
5. **The display type sets the marquee step.** `scroll: {step: 300ms}` on a text display, default 300 ms. The sync answer carries the step with each scrolling box.
6. **The sync applies at once and keeps the offset.** `scroll_changed` says whether the full scroll text changed. A firmware or the virtual display keeps its offset when the flag is false and starts at the first window when it is true. A new page or changed text resets it.
7. **ePaper rejects scroll.** A `scroll` key on an image page is a parse error with a hint; an `overflow` on an image page is rejected too. Only the firmware of a text display animates.

```yaml
kind: text
overflow: strip
cells:
  - { at: [0, 0], span: [20, 1], text: "Familie" }
  - { at: [0, 1], span: [12, 2], value: weather/forecast, overflow: wrap }
  - { at: [14, 1], span: [6, 2], value: paperboy/now, align: right }
  - { at: [0, 3], span: [12, 1], value: paperboy/long, overflow: scroll }
```

## Alternatives

- **Keep rows of concatenated cells.** A tall box and a box beside it cannot share lines; a gap needs an empty cell with a width.
- **One overflow policy per page.** A page mixes a title, a clock and a paragraph; one policy fits at most one of them.
- **Wrap everything.** A clock should not wrap and a station name should scroll, not disappear after two lines.
- **Let the plugin return lines for its box.** That is the drawing model that ADR 0111 removed; the page owns the box and the language.
- **Animate in Paperboy only.** Paperboy composes on demand; the display polls and paints. The full text and the window belong to the answer.

## Consequences

- A text page and an image page share the box model; the editor can use the same drag and resize interaction.
- Existing hand-written text pages migrate from `rows` to `cells` once; the flow of ADR 0117 is replaced by an explicit `at` and `span`.
- The renderer must produce a window and may produce several full lines per box; the framing and memory limits need tests.
- The firmware of a text display needs a scroll offset per box, a timer from `step_ms` and the `scroll_changed` flag. This ADR does not change the firmware.
- The display type editor gains one optional field; an image display rejects it.
- The virtual display animates the same fields, so an operator can check a scrolling page without hardware.
