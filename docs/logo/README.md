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
inverting the colour.

| File | Use |
|---|---|
| `paperboy.svg` | the mark, for anything 32 px and larger |
| `paperboy-small.svg` | 16 to 24 px (favicon, list icons): without dividers and with a thicker frame |
| `paperboy-square.svg` | square cut on a white tile: repository avatar, container image |
| `paperboy-lockup.svg` | mark and wordmark, for headers and the README |

`preview.html` shows all files on light and dark surfaces and at their sizes; open it in a browser.
It embeds the marks inline instead of linking them, so it renders from any location — a copy in a
temp directory, an editor preview — and does not depend on the folder next to it. It is generated:
change the SVGs and run `python3 build-preview.py`.

The wordmark is Go Mono Bold, the font family `pkg/render` draws text with, converted to outlines:
the file needs no font installed and cannot drift between renderers. The outlines were generated
from `golang.org/x/image/font/gofont/gomonobold` with `font/sfnt`; if the wordmark ever has to
change, regenerate the path instead of setting it as text.

The logo is part of the project and covered by `LICENSE`.

## What the UI serves

`internal/paperboy/ui/logo.svg` and `internal/paperboy/ui/favicon.svg` are the copies Paperboy
embeds: the lockup in the header of every operator page and the small cut as its favicon. They are
byte-for-byte copies of `paperboy-lockup.svg` and `paperboy-small.svg`; `TestOperatorPagesCarryTheLogo`
in `internal/paperboy` fails when one of them drifts.
