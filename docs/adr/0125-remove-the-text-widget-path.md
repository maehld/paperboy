# 0125: Remove the text widget path

- Status: Accepted, implemented
- Date: 2026-11-16
- Related: ADR 0044 (text displays), ADR 0058 (the protocol version check), ADR 0092 (scrolling text lines), ADR 0111 (text pages from values), ADR 0124 (the Playground shows image display types), issue #279
- Supersedes: the "the text action stays in the protocol" rule of ADR 0111 and the widget part of ADR 0092
- Amends: ADR 0058 (the protocol can remove a request when the version rises; it no longer only grows)

## Context

ADR 0111 replaced the text widgets of a text page with values. Every shipped plugin moved to values and declares image widgets only; the built-in plugin kept a `Text` method and the `text` widget kind for `GET /api/widget`. ADR 0124 removed the text display types from the Playground, so the only remaining authoring path was gone too.

What remained was a compatibility layer with one consumer: the single-widget endpoint. It kept `ActionText`, the `Texter` interface, `Result.lines`, `WidgetRequest.scroll`, the `text` kind and a second drawing path in the built-in plugin. No shipped plugin used it, and the layer made every text-specific branch in the host, the page model and the tests look alive.

## Decision

1. **The protocol has no text action.** Version 8 removes `ActionText`, the `Texter` interface, `Result.lines` and `WidgetRequest.scroll`. `describe.widgets[].kinds` is `image`; the `text` kind is gone.
2. **The host and the page model drop the text widget batch.** `page.Batch.Text`, `page.Result.Lines`, the `ActionText` branch in `Host.Fetch` and `page.TextLines` are removed.
3. **The built-in plugin drops its text form.** The `Text` method and the `statusLines` text rows are gone; `header`, `battery`, `device-status`, `text` and `clock` declare `image` only. The `text` widget itself stays as an image widget for literal text on image pages.
4. **`GET /api/widget` rejects a text display type.** The endpoint renders one image widget; a text display answers `400` with a hint that text pages are built from values. The page preview `GET /api/preview/{display}/{page}` keeps answering text pages.
5. **Version 8 is the cut, the floor stays 5.** A plugin below 8 that still has text widgets is accepted, because Paperboy never asks it for text. A plugin that only speaks text fails when it is asked for a page, and that is its own stale code.
6. **Text pages are unchanged.** Values, boxes, `at`/`span`, `label`, `unit`, `scale`, `align`, `overflow`, scrolling boxes, the device API's `text` object, the preview, the virtual display and the editor stay as they are.

## Alternatives

- **Keep the compatibility layer.** It costs a second drawing path in the built-in plugin, two result shapes in the protocol and tests for a path no UI offers. The layer would stay forever without a user.
- **Remove the requests but keep the types.** A plugin would compile against `Texter` and never be called. That hides the removal instead of doing it.
- **Raise the minimum to 8.** Every plugin ships with Paperboy, so it would work, but it would refuse a working value-only plugin for no benefit. The floor stays at 5.
- **Keep a text variant of `GET /api/widget` that renders values.** The endpoint is defined per widget, not per page. The Playground already offers image display types only, and a page preview shows a text page.

## Consequences

- Every plugin binary changes and raises its version (ADR 0075, 0107).
- The protocol shrinks for the first time. ADR 0058's "only grows additively" rule now has one exception, recorded here.
- The built-in plugin has one drawing path and one set of tests less.
- A page that still names `paperboy/text` on a text display cannot be rendered; text pages use values.
- The old `scroll` request field and the 200-column limit of a text widget disappear with the action.
