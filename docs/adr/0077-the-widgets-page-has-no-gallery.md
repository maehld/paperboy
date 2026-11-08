# 0077: The widgets page has no gallery

- Status: Accepted
- Date: 2026-11-08
- Builds on: ADR 0055 (widget gallery and arg values), 0068 (page editor), 0076 (one stylesheet and one navigation)
- Amends: ADR 0055 §2 (the gallery form and the gallery tab are gone; the arg values with `enum` stay)
- Answers: issue #122

## Context

The widgets page had two faces (ADR 0055): the page editor without a page, which edited one widget, and the *Gallery*, a catalogue of every widget of every plugin in the sizes the plugin declares, each with a GET form built from the arg schema.

The gallery needed a second template (`ui/plugins.html`, ~250 lines), a form model of its own (`galleryForm`, `galleryField`, `galleryOption`, `galleryPlugin`, ~350 lines in `widgets.go`), its own sizes and the arg formats, and its own snippet format. Most of it repeated what the page editor without a page already does: it shows every widget in the palette with its declared sizes, renders the chosen one and builds the cell snippet, and it shows the arguments of a cell.

The second face was also the reason the widgets page was the only page with a sub-tab strip that carried buttons, and the two faces laid out differently, so switching between them moved the whole header — the navigation stopped being one navigation (ADR 0076).

## Decision

The gallery is removed. `GET /ui/plugins` always renders the page editor without a page, with the display type switch, the palette, the preview, the inspector and the snippet. A link that still names `?tab=gallery` lands on that page instead of failing.

What stays: `GET /api/widget/{plugin}/{widget}` (the preview on that page and any other consumer), the declared `sizes` in `describe`, and the optional `enum` on an arg with its rejection in `Values.Check` (ADR 0055 §1).

What goes: `ui/plugins.html`, its template, `handleGallery`, the gallery form model, `argList`, `cellSnippet`, `previewArgs`, `isDefault`, `yamlValue`, `plainYAML`, `argValue`, `jsonOrString`, `enumText` and `widgetTabs` — around 500 lines.

## Consequences

- One template (`ui/editor-page.html`) drives the page editor and the widgets page; both show the same action bar and neither has a sub-tab strip, so the header stays put.
- The catalogue view is gone: to see every widget in its sizes, the palette lists them with their spans, and the page editor shows the chosen one. A widget's arguments are documented by its plugin (`docs/plugins/`, `describe`).
- No test builds forms for args any more. `TestWidgetsPage` checks the page itself, that the tab strip is gone, and that an old `?tab=gallery` link still opens it.
- The snippets the gallery printed with non-default args are still available: the inspector produces the same cell text.
- Still open: a page that lists the widgets of one plugin with all their declared sizes exists only as the palette; if that turns out to be missed, it belongs in the page editor's palette or the plugin documentation, not in a second face of the widgets page.
