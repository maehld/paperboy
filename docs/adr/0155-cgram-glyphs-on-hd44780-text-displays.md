# 0155: CGRAM glyphs on HD44780 text displays

- Status: Accepted
- Date: 2026-11-22
- Related: ADR 0044 (text displays), ADR 0118 (text boxes), issue #399, issue #406
- Amends: the `hd44780` charset of [ADR 0044](0044-text-displays-on-the-device-api.md)

## Context

A text page marks a direction with ASCII today: `flow: arrow` draws `<` and `>`, and a trend value draws `^`, `>` and `v` ([issue #399](https://github.com/maehld/paperboy/issues/399)). On the HD44780 LCD2004 these look like comparison operators. The controller has eight CGRAM slots of 5 × 8 pixels, and ESPHome 2026.10.1 loads `user_characters` in `LCDDisplay::setup()`.

The display runs on mains power and never sleeps, so one load per boot is enough. Position 0 is unusable through `it.print()`, because byte `0x00` ends the string.

## Decision

1. **Five glyphs.** The firmware defines `←`, `→`, `↑`, `↓` and `✓` in CGRAM positions 1–5. Two slots stay free.
2. **The page stays Unicode.** `flow: arrow` writes `←` for a positive value and `→` for a negative one. A trend value writes `↑`, `→` or `↓`, doubled for a strong move. Literal text and enum labels may carry `✓`.
3. **The server keeps the glyphs for `hd44780`.** `hd44780Kept` gains the five characters, next to `ä ö ü ß ° µ`. The firmware maps their UTF-8 sequences to CGRAM slots 1–5 in `pb_hd44780()`.
4. **`ascii` falls back.** The transliteration table maps `←` to `<`, `→` to `>`, `↑` to `^`, `↓` to `v` and `✓` to `OK`. `utf8` stays unchanged.
5. **The migration path is `?`.** Old firmware maps a new glyph to `?` until it updates. Paperboy offers the update, and the issue accepts the gap. There is no capability negotiation and no server-side firmware gate.
6. **The firmware keeps the old input working.** ASCII `<`, `>`, `^` and `v` still pass through, so a display can update before or after the server.

## Alternatives

- **Unicode arrows only.** The LCD2004 would show `?`, because the A00 ROM has no arrows.
- **A capability field or firmware-version gate.** It avoids the `?` gap, but adds protocol and cache complexity. The gap is cosmetic and short.
- **More CGRAM glyphs.** Battery, charging and weather symbols would consume the remaining slots and need their own design. They stay out.
- **Custom characters in the server.** The device API carries UTF-8 JSON, not controller bytes. Raw `0x01` would break the text contract, and `clean()` replaces control characters.
- **ASCII `<`/`>` in CGRAM.** It would hijack those characters everywhere, including comparisons in a text page.

## Consequences

- The page files and the plugin protocol do not change. Only the rendering of the existing cell key changes.
- An HD44780 display needs firmware 2.9.0 for the glyphs. Older firmware shows `?` for them and keeps the ASCII look for old server output.
- `charset: ascii` stays readable and keeps one column per arrow. The checkmark becomes `OK`, two columns.
- The virtual display and the page editor use the same Unicode text. The vendored pixel font needs the five glyphs, otherwise the system-font fallback draws them.
- No plugin version changes, because no plugin code and no protocol field changes.
