# Paperboy logo

The mark is a page as Paperboy composes it: four by three cells of 16 units, one cell drawn by a
widget, the top-right corner turned down like a newspaper.

```text
64 × 48 units      the page
16 × 16 units      one cell
3 units            frame
2 units            dividers between cells
```

Everything is a filled shape in one colour on a transparent background — no strokes that depend on
a renderer, no gradients, no white fills. The mark therefore scales to any size and inverts by
inverting the colour; `paperboy-lockup-inverted.svg` is the lockup in white for dark surfaces.

| File | Use |
|---|---|
| `paperboy.svg` | the mark, for anything 32 px and larger |
| `paperboy-small.svg` | 16 to 24 px (favicon, list icons): without dividers and with a thicker frame |
| `paperboy-square.svg` | square cut on a white tile: repository avatar, container image |
| `paperboy-lockup.svg` | mark and wordmark, for headers and the README |
| `paperboy-lockup-inverted.svg` | the same lockup in white, for dark surfaces (README dark mode) |

`preview.html` shows the marks on light and dark surfaces and at their sizes; open it in a browser.
It embeds the marks inline instead of linking them, so it renders from any location — a copy in a
temp directory, an editor preview — and does not depend on the folder next to it. It is generated:
change the SVGs and run `python3 build-preview.py`.

The wordmark is Go Mono Bold, the font family `pkg/render` draws text with, converted to outlines:
the file needs no font installed and cannot drift between renderers. The outlines were generated
from `golang.org/x/image/font/gofont/gomonobold` with `font/sfnt`; if the wordmark ever has to
change, regenerate the path instead of setting it as text.

The logo is part of the project and covered by `LICENSE`.

## The mark in pages and on devices

The built-in plugin draws the mark from this geometry instead of a raster copy:
`paperboy/logo` (`internal/builtin/logo.go`) scales it to any cell and
`wordmark: true` adds the lockup's wordmark in Go Mono Bold. The firmware draws
the same shapes (`pb_logo_ink` in `firmware/esphome/paperboy.h`): the lockup
above the instructions of the Wi-Fi and Paperboy URL screens and alone for 5 s at
power-on, with the firmware version (ADR 0095, 0098). It prints the wordmark in
Go Mono Bold from a subset of this face (`firmware/esphome/fonts/`), because the
firmware cannot draw outlines; a change to the shapes or the wordmark has to
touch both, and the font subset follows the design.

## What the UI serves

`internal/paperboy/ui/logo.svg` and `internal/paperboy/ui/favicon.svg` are the copies Paperboy
embeds: the lockup in the header of every operator page and the small cut as its favicon. They are
byte-for-byte copies of `paperboy-lockup.svg` and `paperboy-small.svg`; `TestOperatorPagesCarryTheLogo`
in `internal/paperboy` fails when one of them drifts.
