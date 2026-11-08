# 0074: The editor board shows the page preview

- Status: Accepted, implemented
- Date: 2026-11-08
- Builds on: ADR 0068 (page editor), ADR 0066 (page and plan API), ADR 0055 (arg schema and playground)
- Amends: ADR 0068 (the board and the preview panel)

## Context

The page editor (ADR 0068) draws two pictures of the same page: the board, a
CSS grid of blue cell boxes with the widget names, and below it the preview
rendered by the server. Both live in the middle column and scroll together. On
a tall display the board already needs most of the height, so the preview pushes
the column into a scroll; on a wide, short screen the board loses height to the
preview. The operator also has to map the boxes and the rendered page onto each
other by eye, because the two sit apart.

## Decision

- **The board is the rendered page with the boxes on top.** The preview the
  script already fetches from `POST /api/preview/{display}` becomes the board's
  background (`background-size: 100% 100%`); the cells stay as a translucent
  overlay. One panel replaces the board and the separate preview panel.
- **Text displays fill the character slots.** The text answer is one line per
  row and one character per column, so the script writes a character into the
  slot it belongs to. The character grid is the overlay and needs no font
  metrics; the LCD block stays only in the playground.
- **The boxes are a switch.** A *Cells* checkbox in the board header hides the
  tint, the borders and the labels, so the plain page can be judged. It is on by
  default, remembered per browser (localStorage) and turned back on while a
  widget is being placed, because placement needs visible targets.
- **Only the background changes.** A preview answer sets the board background or
  the slot text, never rebuilds the grid, so the cells and the selection survive
  the answer.
- **A hint while rendering.** The board shows "rendering…" from the change until
  the answer arrives, including the 350 ms debounce.
- **The playground keeps its preview panel.** It has no board; its one widget is
  rendered in its cell size (ADR 0055).
- Errors and the empty page take the place of the picture: the board goes blank
  and the message shows underneath.

## Consequences

- The middle column is the board alone and no longer scrolls the editor for a
  portrait display; the page and its cells are seen at once.
- While a new preview is on its way, the board can briefly show the previous
  page under a changed grid. The answer replaces it, and no second request or a
  layout pass is needed.
- With the boxes hidden, a click still selects a cell but the selection is not
  visible; the operator turns the switch back on for placing.
- A preview that fails leaves an empty board and its error, not the last picture.
