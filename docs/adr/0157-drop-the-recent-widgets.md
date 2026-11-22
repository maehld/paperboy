# 0157: Drop the recent widgets from the palette

- Status: Accepted
- Date: 2026-11-22
- Related: [ADR 0152](0152-a-design-system-and-a-rail-for-the-operator-ui.md), issue #412, issue #415
- Amends: the pinned and recent widgets of [ADR 0152](0152-a-design-system-and-a-rail-for-the-operator-ui.md)

## Context

ADR 0152 put pinned and recent widgets above the plugin groups. Every pick moved the recent widget to the top and rendered the palette again. The group and the groups below it jumped while the operator worked.

The palette gained two other ways to the widget since then. The selected cell opens the group of its widget, and the filter reaches every widget by name. A recent widget therefore repeated a path that the palette already offers.

## Decision

The palette keeps the pinned widgets. It drops the recent ones and writes no recent list.

A browser may still hold `pb-recent-widgets` from an earlier version. Paperboy ignores the key, and no migration removes it.

## Consequences

- The palette keeps its order while the operator places widgets.
- A widget that the operator uses often needs one pin. Pinned widgets stay a deliberate shortcut.
- The browser loses one key of state. Nothing else reads it.
