# 0128: A default grid per display type

- Status: Accepted, implemented
- Date: 2026-11-17
- Amends: ADR 0049 (reference grid), ADR 0110 (pages serve their kind)
- Answers: issue #294

## Context

A page is a grid of cells ([ADR 0049](0049-plugin-widgets.md)). The reference grid is 6 × 8, and a plugin declares its sizes as spans of it. Both were the same number, so the editor started every image page at 6 × 8 and previewed widgets on it.

The reference grid is a fraction vocabulary, not a density. The shipped plugins declare 3 × 4 (half width, half height), 6 × 4 (full width, half height) and 6 × 8 (the page), and no plugin names 8 or 10 columns.

On a landscape panel that vocabulary gives flat cells: 2.2:1 on 800 × 480 and 1.78:1 on 1872 × 1404. Square cells need 10 × 6 and 8 × 6 there. An operator could already write another grid into a page file, but new pages started wrong, the palette placed declared spans as if the page were 6 × 8, and the Playground previewed a size no cell had.

## Decision

1. `displays/<name>.yaml` gains `grid: [columns, rows]` for `kind: image`: 1 to 80 per axis, default 6 × 8. A text type rejects it, like `rotation` ([ADR 0102](0102-rotate-pages-onto-the-panel.md)).
2. The grid belongs to the page. The display type only supplies the default for a new page and a new variant. A page file keeps its own grid, and a change to the display type never rewrites a page.
3. The reference grid stays 6 × 8, and the plugin protocol stays unchanged. The editor maps a declared span onto the page grid when it places a cell: half the width stays half the width on 8 × 6 and on 10 × 6.
4. The display type editor suggests the coarsest grid whose cells are within 3 % of square, with even columns and rows so half cells stay exact. 4:3 becomes 8 × 6, 5:3 becomes 10 × 6, 3:4 becomes 6 × 10. The operator may override it, and the editor then names the cell size and the deviation. The page editor has a lock that keeps the page's grid ratio: a change to one axis derives the other from the ratio it was switched on with, rounded up. It keeps the layout an operator chose, not the display's shape.
5. The Playground previews a real cell. A link from a page carries the page's grid, and the widget endpoint divides by the display type's grid unless the request names one.
6. A page is not bound to an aspect ratio. A page file still serves every display type of its kind ([ADR 0110](0110-pages-serve-their-kind-and-name-variants.md)); widgets see pixels and adapt.

## Alternatives

- **Cells are always square.** Square cells and "half width, half height" conflict on the common ratios. On 4 × 3 the common span 3 × 4 has no integer equivalent, and on 16:9 no even square grid exists below 32 × 18. The rule also contradicts its own override: a grid that an operator may change is a suggestion.
- **Derive the grid from `size` and `ppi` without configuration.** That is a good suggestion, and the editor uses the same arithmetic. As a silent default it removes the operator's choice between square and coarse cells, and it surprises an instance on upgrade.
- **`grid: auto` on the page.** The layout would depend on the display type, spans would have to scale at compose time, and one page could not serve two types. [ADR 0110](0110-pages-serve-their-kind-and-name-variants.md) would no longer hold.

## Consequences

- Existing instances and pages do not change. A display type without `grid` keeps 6 × 8.
- A page keeps its layout when a display type names another grid. Only new pages and variants start there.
- The palette shows the span it will place, and the Playground shows the cell size of the page, so the editor and the device agree.
- A coarse cell can push a widget below its minimum size; it then answers `ErrTooSmall` and Paperboy draws the notice, as before.
- A page shared by a 4:3 and a 5:3 panel still gets cells of different shapes. A variant per display type remains the tool for that, not a new rule.
