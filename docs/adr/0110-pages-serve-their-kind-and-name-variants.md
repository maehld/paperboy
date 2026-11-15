# 0110: Pages serve their kind and name their variants

- Status: Proposed
- Date: 2026-11-15
- Related: ADR 0044 (text displays), ADR 0049 (pages from plugin widgets), ADR 0050 (physical sizes), ADR 0068 (page editor), ADR 0073 (edit creates and deletes), ADR 0079 (renames), ADR 0093 (every page file is addressable), ADR 0094 (the pages list), ADR 0102 (rotation)

## Context

Paperboy resolves a page name to one of two files: `pages/<name>.<display type>.yaml` first, then `pages/<name>.yaml` (ADR 0049). The second file is the shared page and serves every display type without a variant. The kind (`image` or `text`) and the grid sit in the file, so one shared page has one kind for every target. `Fits` rejects a display type of the other kind and a text page whose grid is not the character grid of the display (ADR 0044).

The relation between a file and its display types is implicit. A plan names a page and knows nothing about display types (`internal/plan`), so the page decides which device gets which layout. A shared file silently covers every type, and a variant wins without a word about the type it takes. Four ADRs build on that rule:

- the editor addresses one file of the set with `shared=1` and `file=<type>` (ADR 0093),
- it toggles between the files and writes a variant on purpose (ADR 0068, 0093),
- a rename moves the shared page and every variant as one set (ADR 0079),
- the list shows `shared · small` because the resolution is invisible (ADR 0094).

The grid is physical since ADR 0050, so one image layout usually serves every panel. A second file exists where the layout really differs, mostly another orientation (ADR 0102) or another kind (text).

Issue #238 shows the cost. The page editor writes the image grid of a shared page into a text variant. The open file and the selected display type can disagree. A guard in one path would hide that case, not the cause. No file says which display types it speaks for. No page says which exceptions it has.

## Decision

1. **The page file is the layout for its kind.** `pages/<name>.yaml` carries `title`, `kind`, `grid` (or `rows`, ADR 0111), `style`, `notices` and `cells`. It serves every configured display type of its kind. A text page serves only the types with its character grid.
2. **`variants` names the exceptions.** The key is a map from display type to a file label: `sticky: hochkant` means that `pages/<name>.hochkant.yaml` serves the sticky. Several types may name one file. A type appears once, and the parser rejects a duplicate key.
3. **A variant holds the layout, not the page.** It carries `kind`, grid or rows, style, notices and cells. It inherits the title and its display types from the page. `title` and `variants` on a variant are rejected with a hint, and so is a variant of a variant.
4. **One form per display type.** The page file serves a type unless a variant names it. There is no precedence and no fallback order: the map decides, and it has one entry per type.
5. **A broken entry costs one display type, not the page.** An entry for a type that no longer exists is unused. The file stays, the editor shows an info and Paperboy writes no log line. An entry that names a missing file or a file of another kind leaves its display type without a page. The editor shows it and Paperboy logs it. The page keeps serving its other types.
6. **A display type without a page does not fit.** A rule skips it and logs the reason once per display type and page, as today. The reason is no file, another kind or another character grid.
7. **The editor shows the coverage.** For the open page it lists the variant files. For every configured display type it shows whether the page serves it and through which file. A variant starts from the display type: kind and character grid come from the type, never from the open file. That is the defect of issue #238.
8. **The preview renders the open file.** Its choice lists the display types of the open file's kind. For text it lists only the types with its character grid. When the chosen type is served by another file of the page, the editor warns and offers to open that file. It never renders another file in place of the open one.
9. **Plans stay display-type agnostic.** A rule names one page per screen and keeps its conditions. The device resolves the page file or its variant.
10. **Migration needs no code.** The old shared page becomes the page file, and every old variant gets one entry in `variants`. The operator removes `title` from the variants. A page that exists only as variants needs a page file.

```yaml
# pages/uebersicht.yaml
title: Übersicht
kind: image
grid: [6, 8]
variants:
  sticky: hochkant
  lcd2004: lcd
cells:
  - at: [0, 0]
    span: [6, 2]
    widget: paperboy/header

# pages/uebersicht.hochkant.yaml: the same screen, portrait panel
kind: image
grid: [6, 8]
cells:
  - at: [0, 0]
    span: [6, 1]
    widget: paperboy/header

# pages/uebersicht.lcd.yaml: a text page (ADR 0111) for the LCD
```

## Alternatives

- **A list of display types in every file** (the second draft). Taking one type out forces the other file to name every remaining type, and the editor must rewrite that list whenever the configuration changes. Rejected.
- **Keep the shared file next to variant files, variant first.** That is the state this ADR replaces. A shared file covers types silently, and a variant wins without saying which type it takes.
- **A target list in the variant file.** Paperboy would read two files for one answer, and the page could not show its coverage. It brings back the enumeration the page avoids.
- **Nest variants.** No use case, and every extra level needs its own resolution.
- **Let an unused variant break the page.** A leftover file would stop a working page.
- **Guard the editor only.** It repairs issue #238 for one path. The implicit precedence stays, and the next path can drift again.

## Consequences

- The common page stays one file. A variant is one entry plus one file, and the page owns both.
- The pages list shows one row per page with its variants and the display types they serve. The `shared` label and the file toggle go away.
- The loader reads the page file and then the file the map names, so a variant costs one file more than today. No directory scan is needed: the map lists the files.
- A rename moves the page and the files the map names. A file that the map does not name stays and shows as an info.
- `Fits` keeps its kind and grid checks, now against the resolved file.
- The log keeps its meaning for a page that cannot serve a display type. It stays rare, because a page serves its kind by default.
- Risk: a page serves a new display type of its kind automatically. The coverage list in the editor shows it, and the plan decides per device.
- `docs/content.md`, `docs/runtime.md`, `docs/design/02-content.md`, `docs/design/04-operator-ui.md` and `docs/glossary.md` describe the page file, the variant and the coverage.
