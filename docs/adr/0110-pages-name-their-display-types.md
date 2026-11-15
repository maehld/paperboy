# 0110: Pages name their display types

- Status: Proposed
- Date: 2026-11-15
- Related: ADR 0044 (text displays), ADR 0049 (pages from plugin widgets), ADR 0050 (physical sizes), ADR 0068 (page editor), ADR 0073 (edit creates and deletes), ADR 0079 (renames), ADR 0093 (every page file is addressable), ADR 0094 (the pages list), ADR 0102 (rotation)

## Context

Paperboy resolves a page name to one of two files: `pages/<name>.<display type>.yaml` first, then `pages/<name>.yaml` (ADR 0049). The second file is the shared page and serves every display type without a variant. The kind (`image` or `text`) and the grid sit in the file, so one shared page has one kind for every target. `Fits` rejects a display type of the other kind and a text page whose grid is not the character grid of the display (ADR 0044).

The grid is physical since ADR 0050, so one image layout usually serves every panel. Variants exist for panels where the layout really differs, mostly another orientation (ADR 0102).

A page name therefore means a set of files plus an implicit rule, not a file. Four ADRs build on that rule:

- the editor addresses one file of the set with `shared=1` and `file=<type>` (ADR 0093),
- it toggles between the files and writes a variant on purpose (ADR 0068, 0093),
- a rename moves the shared page and every variant as one set (ADR 0079),
- the list shows `shared · small` because the resolution is invisible (ADR 0094).

Issue #238 shows the cost. The page editor writes the image grid of a shared page into a text variant. The open file and the selected display type can disagree. A guard in one path would hide that case, not the cause: the relation between a file and its display types is implicit.

## Decision

1. **One page is one file.** `pages/<name>.yaml` holds the page. The variant suffix disappears, and a name maps to exactly one file.
2. **`kind` is required.** `image` or `text`, with no default. The two kinds have different cells and grids, so a default hides a decision that the parser can check.
3. **`display_types` names the targets.** The key is optional: without it, a page targets every configured display type of its kind. A named type must have the page's kind. Text types named by one page must share one character grid.
4. **Validation happens where the configuration is known.** The parser checks kind, grid, cells and `scroll` as today. The loader and the editor check `display_types` against `displays/` and answer a hint for an unknown type or a wrong kind.
5. **`Fits` asks for the targets.** A display type that the page does not name does not fit. The text grid check stays as a runtime guard, because a configuration change can move it.
6. **The plan keeps its semantics.** A page that does not target the device's type is skipped and logged once per display type and page. Rotation and the page number count the fitting pages, as today.
7. **The editor opens a file by name.** The display type selects the preview and must be one of the page's targets of its kind. There is no `shared=1`, no `file=`, no toggle and no `Save as <type> variant`. A rename and a delete act on one file.
8. **No migration code.** The loader reads only `pages/<name>.yaml`. A scan of `pages/` logs one warning for every file name that still carries a suffix. `docs/content.md` documents the manual merge: identical variants become one page with all their types, differing ones become separate pages with separate names.

```yaml
# pages/uebersicht.yaml
title: Übersicht
kind: image
display_types: [sticky, e1001, e1003]
grid: [6, 8]
cells:
  - at: [0, 0]
    span: [6, 2]
    widget: paperboy/header
```

## Alternatives

- **Guard the editor only.** It repairs issue #238 for one path. The resolution rule and the set semantics stay, and the next path can drift again.
- **Drop the shared file and keep variants.** A page name stays a set of files with a resolution rule. The editor keeps its toggle. Two display types with one layout then need two identical files.
- **Make `display_types` required.** More explicit, but every page repeats the long list, and today's shared page already means every type of its kind.
- **Target devices instead of display types.** Device identity would enter page content, while the plan already selects content per device.
- **Derive `kind` from `display_types`.** The parser would need the configuration and could not check cells and `scroll` alone.
- **A directory per kind.** The file name must stay the page name that plans and notices use.

## Consequences

- One path per page: `page.Paths`, the fallback loop, `VariantExists`, the file toggle, the variants column and the set rename go away.
- A layout that differs per panel is a separate page with a separate name, and the plan lists both. The page count stays correct because it counts fitting pages only.
- A new display type of a kind joins every page of that kind that does not name its types. The pages list shows the resolved targets, so the default is visible.
- The cut is hard: an installation merges its variants by hand, and the log names every ignored file.
- `docs/content.md`, `docs/runtime.md`, `docs/design/02-content.md`, `docs/design/04-operator-ui.md` and `docs/glossary.md` lose the terms "variant" and "shared page".
- Tests cover the target check, the editor addressing, rename and the list.
- Risk: many panels can grow the page list, because one screen becomes one page per layout. That is the price for explicit files.
