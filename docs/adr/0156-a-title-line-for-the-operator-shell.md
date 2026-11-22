# 0156: A title line for the operator shell

- Status: Accepted
- Date: 2026-11-22
- Related: ADR 0152 (design system and rail), ADR 0153 (overview and grouped rail), issue #397, PR #408
- Amends: the shell of [ADR 0152](0152-a-design-system-and-a-rail-for-the-operator-ui.md)

## Context

ADR 0152 put the breadcrumb in the top bar and the build and the theme toggle next to the search button. Every editor carried its own action bar in the content, and every list carried a card head with its count, a filter and its new-object button. The same object therefore appeared twice, and actions moved between pages.

Issue #397 reworked the shell with three mockups. The chosen concept keeps one line that names the object and puts the actions at its right edge. The search becomes a shortcut hint, the theme becomes a link at the foot of the rail, and a list puts only its count next to its title.

## Decision

1. **One title line.** The top bar carries the path and the object on one line: the sections stay muted links, the current object is the `h1`, and the file follows as a muted monospace hint. No second breadcrumb row remains.
2. **Status and actions at the right edge.** The status slot comes first, then the actions. The slot keeps a save state, a toast or an error. The green dot marks a saved state.
3. **The rail foot carries the utilities.** A `Search ⌘K` link opens the command palette, the build label sits beside it, and the theme is a link. Below 64 rem the foot becomes a second rail row.
4. **A list shows its count.** The count replaces the file hint in the title line. The new-object button is the only action at the right edge. The local filter fields go away, because the command palette searches across objects.
5. **An editor has no action bar in the content.** The page owns the action markup, but the shell owns its position. The page supplies one `title-actions` block.
6. **A phone keeps one primary action.** An editor keeps *Save*, a list keeps its *New* button. The other actions move into a `⋯` menu. Moving the nodes keeps their listeners and their hidden state; the menu closes on a click, an outside click or Escape.
7. **The overview names itself.** It shows `Overview` in the title line, so no page is left without a title.
8. **The brand and the title share one baseline.** The heading row aligns on the baseline of the word *Paperboy*.

## Alternatives

- **A separate action bar below the top bar** (concept A). It gives every page one fixed slot, but costs height on pages without an action.
- **Actions and title in the top bar without a rail foot** (concept B before the review). It saves height, but the search field and the theme crowd the title on a narrow screen.
- **Keep the local editor bars** (concept C). It changes the least, but the title still scrolls away and the position differs between pages.
- **Keep the list filters.** The lists are short, and the command palette already searches pages, plans, devices and widgets. The filter duplicated that path.
- **Hide secondary actions on a phone without a menu.** It would make Rename, Delete and Reload unreachable. The menu keeps them available.

## Consequences

- The shell owns one title slot and one action slot. A page cannot move its title or its actions.
- The status is a small text slot, not a floating toast. A page that does not save keeps it empty.
- The top bar is shorter than before. The rail foot grows by about two lines on the desktop and one row on a phone.
- Search, build and theme exist once, so a page cannot forget them.
- The action block stays server-rendered. `titlebar.js` only moves nodes; it adds no actions and no data.
- The old `.bar` and `.crumbs` styles go away. The design document `04-operator-ui.md` owns the new state.
- A page without JavaScript on a phone keeps all actions wrapped under the title, not in the menu.
- No protocol, plugin or device behavior changes.
