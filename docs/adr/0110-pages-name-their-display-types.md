# 0110: Pages name their display types

- Status: Proposed
- Date: 2026-11-15
- Related: ADR 0044 (text displays), ADR 0049 (pages from plugin widgets), ADR 0050 (physical sizes), ADR 0068 (page editor), ADR 0073 (edit creates and deletes), ADR 0079 (renames), ADR 0093 (every page file is addressable), ADR 0094 (the pages list), ADR 0102 (rotation)

## Context

Paperboy resolves a page name to one of two files: `pages/<name>.<display type>.yaml` first, then `pages/<name>.yaml` (ADR 0049). The second file is the shared page and serves every display type without a variant. The kind (`image` or `text`) and the grid sit in the file, so one shared page has one kind for every target. `Fits` rejects a display type of the other kind and a text page whose grid is not the character grid of the display (ADR 0044).

The resolution is implicit. A plan names a page and knows nothing about display types (`internal/plan`), so the page decides which device gets which layout. The order of the two files does that today, and a shared file silently covers every type. Four ADRs build on that rule:

- the editor addresses one file of the set with `shared=1` and `file=<type>` (ADR 0093),
- it toggles between the files and writes a variant on purpose (ADR 0068, 0093),
- a rename moves the shared page and every variant as one set (ADR 0079),
- the list shows `shared · small` because the resolution is invisible (ADR 0094).

The grid is physical since ADR 0050, so one image layout usually serves every panel. A second file exists where the layout really differs, mostly another orientation (ADR 0102) or another kind (text).

Issue #238 shows the cost. The page editor writes the image grid of a shared page into a text variant. The open file and the selected display type can disagree. A guard in one path would hide that case, not the cause. The targets of a file are implicit, and a shared file claims types that another file also claims.

## Decision

1. **A page is a name with one or more files.** `pages/<name>.yaml` and `pages/<name>.<label>.yaml`. The label is a free hint of a-z, 0-9 and `-`; the targets in the file are the truth. The base file is one member like the others.
2. **`kind` is required.** `image` or `text`, with no default. The two kinds have different cells and grids, so a default hides a decision that the parser can check.
3. **`display_types` names the targets.** The key is optional: without it, a file targets every configured display type of its kind. A named type must have the file's kind. Text types of one file must share one character grid.
4. **Exactly one file per display type.** Within one page name the target sets are disjoint. An overlap is a configuration error that names the display type and both files. There is no order and no fallback: Paperboy picks a file because the file names the type.
5. **Validation happens where the configuration is known.** The parser checks kind, grid, cells and `scroll`. The loader and the editor check the targets against `displays/` and answer a hint for an unknown type, a wrong kind or an overlap.
6. **A file that does not name the type is no skip.** Paperboy selects no file and writes no log line. One plan serves several kinds, so a missing layout is normal. A rule without any page for the device's type keeps its notice, and the notice names the rule, the type and the pages. An ambiguous page is a configuration error: one log line per page and reload, and a notice if a plan selects the page.
7. **Plans stay display-type agnostic.** A rule names one page per screen and keeps its conditions. The device resolves the file that names its type, so a rule does not repeat a screen per type.
8. **The editor opens one file of a page.** `display_types` is a multiple choice. A separate *Preview this display type* choice picks one of the page's targets, the first by default. The editor creates a layout for a further type and lists the files of the page. It refuses a save that overlaps the targets of another file. There is no `shared=1`, no `Save as variant` and no file toggle.
9. **Migration needs no code.** Old files keep their names and load as page files. A text variant next to an image page is disjoint already. An image variant overlaps its base file, and the log names the page, the type and both files, with a hint to name the targets.

```yaml
# pages/uebersicht.yaml: the two landscape panels, one layout
title: Übersicht
kind: image
display_types: [e1001, e1003]
grid: [6, 8]
cells:
  - at: [0, 0]
    span: [6, 2]
    widget: paperboy/header

# pages/uebersicht.sticky.yaml: the same screen, portrait panel
title: Übersicht
kind: image
display_types: [sticky]
grid: [6, 8]
cells:
  - at: [0, 0]
    span: [6, 1]
    widget: paperboy/header
```

## Alternatives

- **One file per page, the plan lists every layout.** The first draft of this ADR. It couples plans to display types: the plan would name `uebersicht` and `uebersicht-portrait`, and a condition set would sit next to a list of layouts. A layout that also matches a type shows the screen twice in rotation, and nothing detects it. Rejected.
- **Keep variant and shared files.** The order decides, and a shared file covers types silently. A second file for the same type wins without a word. That is the state this ADR replaces.
- **Resolve by the label in the file name.** A file name would have to name a display type. One file could not serve two types. Rejected.
- **Make `display_types` required.** More explicit, but every page repeats the long list, and today's shared page means every type of its kind already.
- **Let the last file win, or order the files.** It hides a mistake. An error with the two file names is cheaper for the operator.
- **Derive `kind` from `display_types`.** The parser would need the configuration and could not check cells and `scroll` alone.
- **Target devices instead of display types.** Device identity would enter page content, while the plan already selects content per device.
- **Guard the editor only.** It repairs issue #238 for one path. The implicit targets and the silent precedence stay, and the next path can drift again.

## Consequences

- The plan stays display-type agnostic: one entry per screen, one condition set per rule, for every kind of device.
- A page is still a set of files, but the set is explicit and checked. There is no order, no fallback and no file that speaks for another. `page.Paths`, the shared fallback, `VariantExists`, the toggle and the set rename go away.
- The loader indexes `pages/` instead of probing two paths, because the files of a page are not derivable from its name.
- The worst case of the old model stays impossible. No file covers the display types of another file, so the editor cannot write a layout into a kind it does not target.
- The skip log and its once-per-page bookkeeping go away. An overlap is loud instead. It gives one log line per page and reload and an error in the pages list and in the editor. A plan that selects the page serves a notice.
- A layout that differs per panel is a file in the same page, and the operator sees one page with several files.
- A new display type of a kind joins every page of that kind that does not name its types.
- The migration is gentle: old files load, and the log names every overlap. The operator adds `display_types`, or merges two files into one.
- `docs/content.md`, `docs/runtime.md`, `docs/design/02-content.md`, `docs/design/04-operator-ui.md` and `docs/glossary.md` lose the terms "variant" and "shared page".
- Tests cover the disjoint targets, the editor addressing, rename and the list.
- Risk: many panels can make the pages list busy, because a screen can have one file per layout. The list groups them under one name.
