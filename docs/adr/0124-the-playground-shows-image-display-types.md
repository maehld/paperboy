# 0124: The Playground shows image display types only

- Status: Accepted, implemented
- Date: 2026-11-16
- Related: ADR 0055 (allowed arg values and playground), ADR 0074 (editor board), ADR 0077 (the widgets page has no gallery), ADR 0111 (text pages from plugin values), ADR 0118 (text boxes and overflow), ADR 0120 (arguments for values), issue #272
- Supersedes: the sentence "the LCD block stays only in the playground" of ADR 0074

## Context

The Playground (`GET /ui/plugins`) is the page editor without a page (ADR 0055, 0077). Its display type switch listed every configured display type, including text displays. On a text display it fell back to the legacy `text` action of the built-in plugin: the palette listed the widgets whose `kinds` include `text`, the preview showed their lines in the LCD block, and the snippet was an image-page cell.

That authoring path is gone. A text page is a character grid of boxes (ADR 0111, 0118, 0120). Every box names a declared value with its arguments or literal text, and Paperboy formats and maps the content. No cell the Playground builds is valid in a text page, and the value palette that the text page editor offers never appeared there. The switch showed a display type and widgets the operator could not use.

ADR 0074 said the LCD block stays only in the playground, because the editor board drew text as character slots. ADR 0116 gave the board and the virtual display the LCD look too, so the sentence no longer describes the system.

## Decision

1. **The Playground offers image display types only.** `GET /ui/plugins` lists only configured display types with a pixel grid in its switch. A text display type does not appear.
2. **A text type in a link falls back.** A link with `display=<text type>` opens the first configured image display type. A hint below the switch says that text displays are edited under *Configuration → Pages* and links that page.
3. **An instance with text displays only explains itself.** If every configured display type is a text display, the page says so and links *Configuration → Pages* with status `200`. It does not fall back to text widgets.
4. **The protocol keeps the text action.** `GET /api/widget` still answers text widgets for text display types, and the built-in plugin still declares its text widgets. Only the UI stops offering them. Older plugins and other consumers keep working.
5. **The LCD block leaves the playground.** The preview panel keeps only its image. The board of the page editor and the virtual display keep the LCD module of ADR 0116.

## Consequences

- The Playground always starts from the reference grid of an image page, and its snippet is always an image-page cell.
- An old bookmark with a text display type still works: it lands on an image type with the notice.
- Text widgets of the built-in plugin have no authoring UI. They stay reachable by URL and through `GET /api/widget`, and a page that still uses them keeps rendering.
- The sentence of ADR 0074 about the LCD block is superseded; the ADR stays as written.
- The palette of an image page no longer offers the *Text* entry, because literal text belongs to a text page's value palette.
