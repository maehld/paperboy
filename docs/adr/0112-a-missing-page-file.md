# 0112: A missing page file is created by hand

- Status: Accepted
- Date: 2026-11-15
- Related: ADR 0110 (pages serve their kind and name their variants), ADR 0073 (edit creates and deletes)

## Context

ADR 0110 lets a page exist only as variants until someone writes its page file. The editor met that state with an *Assign* dialog on the pages row: it moved a variant onto a page, either as the page file or as an entry in the `variants` map, and the row carried a line of text ("No page file. Assign a variant to become it.").

That duplicates what the page editor does. The files already appear as chips under the page name. The *Variants* select is the one place that decides which file a display type gets. The dialog moved a file and wrote a second one: a second way to change the same map. The pages row also grew a line of text that repeated the chip state.

## Decision

1. **The assign dialog and `POST /api/pages/{name}/assign` go away.** No endpoint moves a page file.
2. **A page without a page file shows its name in red.** The row keeps kind, grid and title of its first variant. The page name carries the reason in its tooltip, and there is no second line of text.
3. **Its variants are not linked until the page file exists.** A variant without a page file is read-only in the UI. The list does not link it. A hand-written URL opens the editor with a note about the missing page file.
4. **The operator creates the page file with *New page*.** Then the *Variants* select picks the files. The variants stay where they are; nothing is moved or renamed.

## Alternatives

- **Keep the dialog.** It is a second path into the map, with its own rules for moving a file. It repeats what the row already shows.
- **Link the variants of a page without a page file.** The editor would open a file that cannot be used. The operator would fix the layout instead of the missing page.
- **Hide the page until its file exists.** Then the variants disappear from the list. Nothing tells the operator what waits for the page file.

## Consequences

- One endpoint, one dialog, its JavaScript and its tests go away.
- Two steps replace the dialog: create the page file, then choose the file per display type. Both happen in the editor.
- A variant without a page file stays visible as a chip in orange and keeps its parse error, so the operator sees what waits.
- `docs/design/04-operator-ui.md` and `docs/runtime.md` describe the red name and the read-only variants.
