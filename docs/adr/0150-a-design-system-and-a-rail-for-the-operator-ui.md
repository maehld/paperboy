# 0150: A design system and a rail for the operator UI

- Status: Accepted
- Date: 2026-11-21
- Builds on: ADR 0040 (dashboard), 0068 (page editor without a build step), 0076 (one stylesheet and one navigation), 0106 (htmx)
- Also builds on: ADR 0116 (pixel font), 0136 (hardening)
- Amends: the one-file rule of [ADR 0076](0076-one-stylesheet-and-one-navigation.md) and its flat navigation
- Answers: issue #382

## Context

The look and the path through the UI grew page by page:

- `ui.css` has 79 lines. It carries the header, the action bar and the buttons. Every page adds its own rules in a `<style>` block, 452 lines in total. ADR 0076 called the card and table looks "still open".
- The header is copied into ten templates. Each page parses its own template, so there is no shared layout.
- Five entries stand side by side: Dashboard, Playground, Configuration, Downloads, Stats. "Configuration" hides four object types behind a second tab row. "Playground" is a tool without an object, but it looks like a section. `/ui/plan` marks Dashboard as active and `/ui/virtual` marks nothing.
- Objects do not link to each other. A device card opens no page and no plan. A page names no plan that uses it.
- The widget palette lists 43 widgets as text, grouped by plugin. Sizes appear as a note. A plugin adds widgets, and the list stops working.
- ADR 0068 keeps the service free of a build step. ADR 0106 allows one vendored file, htmx. ADR 0136 sets `frame-ancestors 'none'`, and every asset comes from the binary.

## Decision

### 1. The identity is "Ink & Paper"

The UI looks like a well-printed instrument: paper, ink, hairlines, machine data in monospace, colour only where it means something.

| Token | Light | Dark ("night paper") |
|---|---|---|
| `--pb-bg` | `#F6F4EF` | `#1A1917` |
| `--pb-surface` | `#FFFFFF` | `#232220` |
| `--pb-surface-sunken` | `#EFEBE3` | `#121110` |
| `--pb-ink` | `#16181C` | `#EDEAE3` |
| `--pb-ink-muted` | `#5F6268` | `#A7A29A` |
| `--pb-hairline` | `#DCD8CF` | `#38352F` |
| `--pb-accent` | `#1B4D8F` | `#7FA9E0` |
| `--pb-ok` | `#1F7A3F` | `#6CC58A` |
| `--pb-warn` | `#96600A` | `#E0A94E` |
| `--pb-error` | `#A8291E` | `#F08A80` |

- The LCD green stays inside the LCD frame (ADR 0116). It never becomes an interface accent.
- Machine data uses the monospace stack with tabular numbers: ids, MACs, versions, hashes, sizes.
- Type scale 13, 15, 18, 24 and 32 px. Body text is 15 px on a 1.5 line height.
- Spacing uses a 4 px step. Radii are 4, 8 and 12 px. One shadow, for dialogs only.
- One density. Tables, lists and cards share the same row height.
- Breakpoints: 40rem, 64rem and 90rem. Below 40rem a table becomes a card list.
- Motion takes 120 ms, and `prefers-reduced-motion` turns it off.
- Dark mode follows `prefers-color-scheme`. A toggle in the header stores the choice in the browser. The server keeps no theme state.
- `tokens.css` uses `light-dark()` and `color-scheme`, so each colour exists once and the toggle only sets `data-theme` on `<html>`. That needs a browser from 2024.

### 2. One source of truth, three files

`ui.css` splits into three files. `tokens.css` holds the values above, for light and dark. `ui.css` holds the components: header, rail, bar, button, field, card, table, dialog, toast, badge, empty state and tabs. `editor.css` holds the board, the palette and the inspector. Every page links them in that order, and a page keeps layout rules only.

The principle of ADR 0076 stays: a page cannot drift from the others. The one-file rule goes.

### 3. The shell renders once

One partial (`ui/layout.html`) holds the document head, the header, the rail and the breadcrumb. Every page parses it and fills a block. The navigation therefore exists once.

- Left rail on desktop: Devices, Pages, Plans, Plugins, System. A drawer below 64rem.
- System holds Downloads, Stats and the Styleguide.
- The Playground becomes a tool under Plugins. The palette and every widget card link to it, and the route stays.
- A breadcrumb replaces `← <list>`. It names the whole path and every part is a link.
- A command palette on `⌘K`/`Ctrl+K` searches pages, plans, devices, display types and widgets. It reads the existing list APIs in the browser. The server gets no search route.

Routes name the object: `/ui/pages`, `/ui/pages/{name}`, `/ui/plans/{name}`, `/ui/devices/{id}`, `/ui/display-types/{name}`, `/ui/system/downloads` and `/ui/system/stats`. The old routes go away without a redirect. The device routes do not change.

### 4. Objects link their relations

A device page names its display type, its plan and the page it shows. A page names the plans that use it and the devices that show it. A plan names each page with its coverage and the devices that use it. A display type names the pages that serve it.

The relations are computed from the configuration snapshot, so a page does not read the state lock for them.

### 5. The platform does the work

Native `<dialog>` for every prompt and confirmation, the `popover` attribute for menus, `<details>` for disclosure, and view transitions where a page changes. No new library. Alpine.js is considered only if the editors still carry painful DOM code after this decision.

### 6. The palette scales

The palette shows a thumbnail per widget from `/api/widget`, ordered by fit for the selected cell. A filter covers title, plugin, widget and argument names. `/` focuses it, the arrow keys select, Enter places. Recent and pinned widgets live in the browser.

## Consequences

- Every page changes. The work ships in stages with before and after pictures from `scripts/ui-shots.mjs`.
- The page tests that pin the shared stylesheet and the bar stay. The navigation test follows the partial.
- Two stylesheets more are served from the binary, so the UI stays offline. No build step, no package manager in the release.
- A theme toggle is client state. It survives a reload through `localStorage` and does not follow a user to another browser.
- `Configuration` disappears as a term. The glossary, the documents and the links follow in the same commits.
- The variant flow is not part of this decision. It gets its own issue (#384).
- The rail costs horizontal space on a phone. The drawer hides it, and the breadcrumb keeps the position.
