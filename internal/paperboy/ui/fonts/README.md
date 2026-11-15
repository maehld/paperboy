# Vendored UI font

The editor board and the virtual display show text pages as a dot-matrix LCD
module ([ADR 0115](../../../../docs/adr/0115-a-pixel-font-for-text-previews.md)).
The glyphs come from one vendored pixel font, so the preview keeps the exact
character grid and no browser fetches a font from a third party.

| File | What it is |
|---|---|
| `silkscreen-latin.woff2` | [Silkscreen](https://fonts.google.com/specimen/Silkscreen) Regular, the latin subset, from <https://fonts.gstatic.com/s/silkscreen/v6/m8JXjfVPf62XiF7kO-i9YLNlaw.woff2> (8 404 bytes, SHA-256 `e6c72ea4702249202bcdd79d3343057e4e25ef1f04e3fcffd8602ab53b40b4cc`) |
| `OFL.txt` | the SIL Open Font License 1.1 that Silkscreen ships under, from <https://raw.githubusercontent.com/google/fonts/main/ofl/silkscreen/OFL.txt> |

The font comes from the [Google Fonts collection](https://github.com/google/fonts/tree/main/ofl/silkscreen),
designed by Jason Kottke. The subset carries Latin-1, so it covers the German
umlauts, `ß`, `°`, `µ` and `€` that text pages show.

The binary embeds the file (`go:embed`) and serves it as
`/ui/fonts/silkscreen-latin.woff2` behind the operator password. An upgrade
replaces the file, updates the embed line, this table and the SHA-256. The
licence stays next to the font.
