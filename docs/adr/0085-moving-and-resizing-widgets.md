# 0085: Moving and resizing widgets with the pointer

- Status: Accepted
- Date: 2026-11-11

## Context

The page editor placed widgets by clicking or by an HTML drag-and-drop from the
palette and moved a placed box by dropping it on another cell. The drop cell
became the top-left corner, so a box picked up in the middle jumped by half its
size, and the size could only be changed through the two number fields in the
inspector.

## Decision

Placed boxes are moved and resized with pointer events instead of HTML
drag-and-drop. Pressing the body of a box picks it up at the grabbed point, which
stays under the pointer while the box follows cell by cell; the position is
committed on release, so one preview runs per drag. Three handles on the selected
box - right edge, bottom edge and corner - resize it the same way, one cell at a
time.

A drag stops at the edge of the board and at a neighbouring box instead of
jumping over it: a position or size that would overlap another box is not
applied, and the last free one stays. The number fields in the inspector keep
working for exact values. Dragging a widget out of the palette stays an HTML
drag-and-drop, because it starts outside the board; the cell under the pointer
becomes the new box's top-left corner.

## Consequences

Moving and resizing feel direct and need no numbers, and a picked-up box no
longer jumps. The board owns pointer events on touch screens (`touch-action:
none`), so a drag on a box does not scroll the page; the columns around it still
scroll. The script keeps two drag paths - pointer for boxes, HTML drag-and-drop
for the palette - which is less code than carrying pointer capture across two
panels. A box cannot be pushed past a neighbour; the operator moves the
neighbour first. The handles only exist while the box is selected and the *Cells*
switch hides them with the rest of the overlay.
