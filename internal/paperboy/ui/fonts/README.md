# Vendored UI font

The editor board and the virtual display show text pages as a dot-matrix LCD
module ([ADR 0115](../../../../docs/adr/0115-a-pixel-font-for-text-previews.md)).
The glyphs come from one vendored pixel font, so the preview keeps the exact
character grid and no browser fetches a font from a third party.

| File | What it is |
|---|---|
| `dotgothic16-latin.woff2` | [DotGothic16](https://fonts.google.com/specimen/DotGothic16) Regular, the latin subset, from <https://fonts.gstatic.com/s/dotgothic16/v22/v6-QGYjBJFKgyw5nSoDAGH7M6X8.woff2> (10 508 bytes, SHA-256 `d29abf817463fdba7e9e87fc954cfa2b60ea6534e8a79c46e858a0dc5aa6a6f6`) |
| `OFL.txt` | the SIL Open Font License 1.1 that DotGothic16 ships under, from <https://raw.githubusercontent.com/google/fonts/main/ofl/dotgothic16/OFL.txt> |

The font comes from the [Google Fonts collection](https://github.com/google/fonts/tree/main/ofl/dotgothic16),
designed by Fontworks Inc. It is a dot-matrix font with a real lowercase, and
the subset carries Latin-1, so it covers the German umlauts, `ß`, `°`, `µ` and
`€` that text pages show.

The binary embeds the file (`go:embed`) and serves it as
`/ui/fonts/dotgothic16-latin.woff2` behind the operator password. An upgrade
replaces the file, updates the embed line, this table and the SHA-256. The
licence stays next to the font.
