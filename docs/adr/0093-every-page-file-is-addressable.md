# 0093: Every page file is addressable

- Status: Accepted, implemented
- Date: 2026-11-12
- Builds on: ADR 0066 (page API), 0068 (page editor), 0073 (delete in the editor), 0079 (renaming and orphaned variants), 0084 (the overview links, the editors act)
- Answers: issue #181

## Context

The pages tab of `/ui/editor` groups the files of a page and resolves one of them for the selected display type: the variant of that type, else the shared page. The chips in the variants column were resolution links too, not file links, so the shared page became unreachable as soon as the selected display type had a variant. The list compensated with a Delete link on exactly that chip, which read like a defect: the one file an operator wanted to edit offered only to delete it.

A page can legitimately exist only as variants: `page.Load` tries the variant and then the shared page, and a plan rule skips a page that neither provides; nothing requires a shared file. For mixed kinds a shared page is not even possible, because `Fits` requires the same kind and, for text, the exact character grid. The overview still reported that state as a red error.

## Decision

### 1. The row resolves, the chips are files

The page column shows the page name only; the file path moves into the chip tooltip and into the error line. The variants column lists every existing file, the shared page first, with the resolved file in bold. The row (the page name and a click on the row) keeps opening the resolved file, like the other object tabs (ADR 0084).

Every chip is a link that opens exactly its file:

- `shared=1` names the shared page, `file=<display type>` names a variant.
- `display=<display type>` stays the rendering context: kind, pixels, ppi and the grid a new page starts with.
- `source=<display type>` from ADR 0079 remains the old name of an exact variant and keeps working.
- The row and the chips open the shared page for the selected display type when its kind fits, else for a configured type of its kind.

### 2. A broken file opens, an unopenable one keeps its Delete

A file that does not parse still opens the editor read-only (ADR 0068) instead of being unlinked, so its text can be repaired and it can be deleted or renamed there. Only a file that no display type can render — a variant of a display type that is gone, whose kind has no configured type, or a shared page whose kind has none — keeps a Delete in the list. Otherwise Delete lives in the editor (ADR 0073, 0084).

### 3. The editor toggles between the two files of a display type

`GET /ui/editor/page` shows `Edit <type> variant` next to the actions when the shared page is open and that variant exists, and `Edit shared` when a variant is open and the shared page exists. Creating a variant stays the explicit `Save as <type> variant` and is hidden once the variant exists, so a save cannot overwrite it by accident. The shared file stays open across the editor's display type switch; a variant is per type, so switching resolves.

A page with variants only is a normal state: the missing-file line becomes a neutral note instead of an error.

### 4. The action bar names the page, not the file

The bar carries the page name and the actions; the file is not described there. The overview and the toggle say which file is open.

## Consequences

- No file is stranded: the list never offers a Delete for something it can open.
- The shared page stays editable while a variant exists, and branching remains a deliberate `Save as <type> variant`.
- Mixed kinds work where the list and the toggle link a file: a shared page opens for a display type of its own kind, and the toggle from a text variant to a shared image page carries the image type. The editor's display type switch still crosses kinds for a shared file; whether the page fits a device is decided by `Fits` at runtime (ADR 0049).
- The display type switch keeps the shared file and resolves a variant for the new type; the shared page is also reachable through the toggle and the list.
- Still open: the page API itself keeps resolving (`GET /api/pages/{name}?display=`), so an editor that addresses a file passes the file's display type or none for the shared page.
