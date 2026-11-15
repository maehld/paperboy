# 0117: Text cells name their column

- Status: Accepted
- Date: 2026-11-15
- Related: ADR 0111 (text pages from plugin values), ADR 0085 (moving and resizing widgets), ADR 0074 (the editor board)

## Context

ADR 0111 made a text row a list of cells that are concatenated from the left. A row therefore has no space between its cells and no space before its first cell. The editor could only reorder a row by changing the list order. Shrinking the only cell and dragging it to the right changed nothing, because the model had no column to put it at.

An image page already names the position of a cell: `at` is the column and row of its top-left corner (ADR 0085). The image editor moves a box to another `at` and leaves the freed grid cells empty.

## Decision

1. **A text cell carries `at`.** It is the 0-based character column of the cell inside its row. `at` sits next to `value`, `label`, `unit`, `width` and `align`.
2. **A missing `at` flows.** Without `at`, a cell starts where the previous cell ends. A hand-written page can therefore stay terse, and every existing text page keeps its layout without a migration. An empty row stays an empty line.
3. **Columns are explicit when the editor writes.** The editor resolves the flow of every loaded cell to a column and writes that `at` back on the next save. Only a page that the editor saves gains the explicit keys.
4. **Cells keep the order of their columns.** `at` must not be negative, must not overlap the cell before it, and `at + width` must fit the display. The parser checks this with the maximum of 80 columns; `Fits` checks it with the columns of the display type. The renderer writes the cell at its column and leaves the gaps as spaces.
5. **The editor places freely.** A drag sets `at` from the pointer, so a cell can leave space on its left. A drop on an occupied range is refused, or moved to the next free column and shown there by the drop marker. The width handle stops at the next cell's column.
6. **The image page model does not change.** Only `TextCell` gains `at`; the image `Cell` keeps its own `at` pair.

```yaml
rows:
  - [{ value: paperboy/title, at: 6, width: 8, align: left }]
  - [{ value: paperboy/date, width: 10 }, { value: paperboy/time, width: 8, align: right }]
```

The second row flows: `date` starts at 0, `time` at 10. The first row starts at column 6; the six columns before it stay blank.

## Alternatives

- **Keep the list and move cells by list order.** A cell can never leave a gap on its left, which is exactly the defect this ADR fixes.
- **Give every row a fixed set of columns.** Columns would be page-wide and every value would need a column, which is a table, not a row of values.
- **Use `at` for every cell in every stored page.** Hand-written pages would become noisy, and existing pages would need a migration.
- **Pad the gaps with empty cells.** An empty cell still has a width and a value field; `at` is the one fact a gap needs.

## Consequences

- The renderer and the editor can show and create gaps. A row with a gap looks like the LCD shows it.
- Existing text pages keep their layout: their cells flow from column 0, exactly as before. The editor shows them with resolved columns and adds `at` when it saves.
- Two cells may not overlap. A drop on an occupied range needs a free column or a change of another cell's width.
- The editor's width handle and `At` field must agree about the next cell; both use the sorted row.
- The page schema gains one key. The plugin protocol and the image page model do not change.
