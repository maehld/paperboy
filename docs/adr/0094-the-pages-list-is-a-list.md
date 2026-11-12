# 0094: The pages list is a list

- Status: Accepted, implemented
- Date: 2026-11-12
- Builds on: ADR 0068 (page editor), 0079 (renaming and orphaned variants), 0084 (the overview links, the editors act), 0093 (every page file is addressable)
- Supersedes: the chip links and the Delete in the list of ADR 0093
- Answers: issue #186

## Context

The pages tab of `/ui/editor` carried a display type switch above the table, and the variants column was a set of links that opened exactly one file (ADR 0093). The display type only decided which variant a row described, and most pages have one file, so the switch changed little. The links made the row and the chips two competing ways in, and the Delete that ADR 0093 kept for files no editor can open was the only action in a list of links. The overview read like a file picker, not like a list of pages.

## Decision

### 1. The pages tab has no display type

The switch above the table goes away, and with it the display type in the tab link. The new-page dialog keeps its display type choice, but it starts on the shared page.

### 2. A row resolves per page

A row shows the shared page when it exists, else the first variant alphabetically. Kind, grid, title and the row link describe and open exactly that file. An orphaned variant opens through a configured display type of its kind (ADR 0079); a broken file opens read-only for the first configured display type (ADR 0093).

### 3. The variants column only names the files

`shared · small`, as plain text, with the path in the tooltip and a parse error in red. No links, no bold resolved file.

### 4. The list is a list

No Delete in the list. Rename and Delete live in the editor of the object (ADR 0073, 0084). A file that no editor can open is removed on disk.

### 5. The editor names its file

Behind the page name the page editor shows the open file: `page countdown - shared` or `page countdown - small`. The label follows the toggle between the two files, `Save as <type> variant` and a save. The display type switch in the editor stays, because it decides kind, pixels and ppi.

## Consequences

- The overview answers which pages there are and what is in them, not which file belongs to which type; the editor says which file is open.
- A broken file that no editor can open is only removable on disk.
- The addressing of `GET /ui/editor/page` (`shared=1`, `file=<display type>`) and the toggle between the files of a type stay; so does the display type of the runtime and the page API, which still resolves `GET /api/pages/{name}?display=`.
