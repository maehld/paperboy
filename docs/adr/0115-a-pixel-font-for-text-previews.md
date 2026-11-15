# 0115: A pixel font for text previews

- Status: Accepted
- Date: 2026-11-15
- Related: ADR 0068 (page editor), ADR 0074 (the board), ADR 0076 (one stylesheet), ADR 0091 (virtual display), ADR 0111 (text pages from plugin values)

## Context

A text page is a character grid ([ADR 0111](0111-text-pages-from-plugin-values.md)). The editor board and the virtual display show it one character per slot, but the browser's monospace fonts leave large gaps inside a cell and look nothing like the LCD module the page is made for. Fonts differ between operating systems and browsers, so a preview cannot promise the same grid twice.

The operator UI has no build step and loads no third-party asset from the network ([ADR 0068](0068-page-editor-ui.md), [0076](0076-one-stylesheet-and-one-navigation.md), [0106](0106-self-updating-pages-with-htmx.md)). htmx is the only vendored script. A font must therefore live in the repository, arrive with the binary and stay behind the operator password.

## Decision

1. **Vendor one dot-matrix font.** The UI ships DotGothic16 Regular under the SIL Open Font License 1.1, as a latin woff2 subset in `internal/paperboy/ui/fonts/`. The licence, the source URL and the SHA-256 sit next to the file.
2. **Serve it from the binary.** `go:embed` puts the file into Paperboy, and `GET /ui/fonts/dotgothic16-latin.woff2` serves it behind the UI password. The shared stylesheet declares the `@font-face`, so the editor page and the virtual display load the same file. There is no font CDN and no build step.
3. **Prefer a font with a real lowercase.** An all-caps pixel font makes `Datum` and `DATUM` look the same, which hides a mistake in a label or a value. DotGothic16 draws lowercase at a lower x-height and keeps the dot-matrix grid. It also fills the glyph box: its cap height is 0.79 em, so the character cell looks used. Of the candidates, Doto lacks `µ`, Pixelify Sans is thin at the cell sizes of a 20 × 4 panel, and VT323 has a small x-height. DotGothic16 covers Latin-1 and keeps upper and lower case apart.
4. **Draw a character module.** The text board is a dark green panel with light green pixels. Each slot is one character cell, the cells sit in a grid with a one-pixel gap, and the font size follows the slot height. A text page never reflows: 20 × 4 stays 20 × 4, and 16 × 2 stays 16 × 2.
5. **Fall back for missing glyphs.** The font stack continues with the system monospace fonts. A character outside the subset still occupies its slot instead of breaking the grid.
6. **The device output does not change.** The font belongs to the operator UI only. A text display receives the same lines it received before ([ADR 0044](0044-text-displays-on-the-device-api.md)).

## Consequences

- The board and the virtual display show the same LCD look on every operating system.
- The binary grows by about 8 KiB, and an update of the font needs the file, the embed line, the README and the SHA-256.
- DotGothic16's latin subset covers Latin-1, so German umlauts, `ß`, `°`, `µ` and `€` arrive; other scripts fall back and may look less consistent.
- Lowercase is shorter than uppercase in the same slot. A page that uses `Datum` and `JETZT` therefore shows the difference the preview is meant to reveal.
- A second font or an icon set needs its own ADR, because this one opens the UI to a second vendored asset beside htmx.
- A screenshot of the preview is not a photo of the panel. The module look helps the operator judge the layout; it does not predict the exact pixels of the firmware.
