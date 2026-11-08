# 0076: One stylesheet and one navigation for the operator UI

- Status: Accepted
- Date: 2026-11-08
- Builds on: ADR 0040 (dashboard), 0055 (widgets page), 0068 (page editor), 0069 (plan editor), 0070 (device files), 0071 (display types), 0074 (editor board)
- Answers: issue #122

## Context

Nine operator pages each carried their own copy of the header, the buttons and the action bar inside their HTML. The copies drifted apart:

- The plan preview named the device in the heading until ADR 0058's successor (issue 120) pulled it back into the shared shape; editors still labelled their back link differently (`← Pages`, `← Pages and plans`, `← Pages, plans and devices`, `← Pages, plans, devices and display types`), and three of them showed the back link twice, once as a paragraph above the bar and once inside it.
- The widgets page kept `Reload` and its status in its sub-tab strip, while every editor has them in the action bar.
- The display type editor hid the fields of the other display kind with the `hidden` attribute. A rule such as `label.field { display: grid }` beats the browser's `[hidden] { display: none }`, so `ppi` and `gray_levels` stayed visible for a `kind: text` display and `charset` for an image display.

## Decision

### 1. One stylesheet

`internal/paperboy/ui/ui.css` is embedded in the binary and served as `GET /ui/ui.css`. It holds what every page repeats: the body defaults, the header with brand, navigation and build label, the action bar, the status text, the buttons, panels, hints, code, sub-tabs and the display-type switch, plus `[hidden] { display: none !important }`.

Every page links it first and keeps only its own rules afterwards. Where a page needs a different value (the editors' smaller body text, the full-height page editor's bar margin) it overrides one property instead of repeating the block.

### 2. One action bar

Every editing surface shows exactly one bar, in this order:

```
← <list>   <kind> <name>   <where>            <status> [Specialize] [Delete] [Reload] [Save]
```

- The back link names the list, not its contents: `← Pages`, `← Plans`, `← Devices`, `← Display types`, `← Widgets`, and each goes to that list's tab (`/ui/editor?tab=plans`), which the old links did not.
- `Delete` disappears where there is nothing to delete, `Save` where nothing is saved (the widgets page copies a cell instead), and the page editor keeps `Save as <type> variant`.
- The widgets page has no object and no action of its own, so it shows **no bar at all**: its status sits next to the display type switch, and the widget it shows is named in the preview head.
- The sub-tab strip stays the way back only where a page has tabs; editors have the bar.

### 3. The widgets page is an editor

The widgets page drops `Reload` and the status from its tab strip and gets the same bar (title, status, `Reload`). Its `Copy cell` button has its own id now, so the page editor's `Save` handler and the widgets page's copy handler are two buttons instead of one that changes its meaning.

## Consequences

- A page cannot drift from the others without editing `ui.css`; a test (`TestOperatorPagesLinkTheSharedStylesheet`, `TestEditorBarsShareThePattern`) fails when a page stops linking the stylesheet, grows a second bar, loses its back link or reorders `Delete`, `Reload` and `Save`.
- The design is unchanged: the shared file carries the values the pages already used. Only the duplicated back-link paragraphs and the widgets page's tab-strip buttons disappear.
- The display type editor now really shows the fields of its kind only — `hidden` works because the rule is `!important` and lives in one place.
- The pages keep their own content CSS; a page-specific look is still possible and does not need a new shared rule.
- Still open: the looks of the dashboard cards and the tables are page-specific and could move to shared classes later; nothing forces a build step or a CSS framework.
