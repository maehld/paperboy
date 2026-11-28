# 0179: Pictograms from Weather Icons

- Status: Accepted, implemented
- Date: 2026-11-28
- Answers: issue #501

## Context

The weather plugin draws its pictograms from five OpenMoji PNGs: sun, cloud,
rain, snow and thunder. The widgets show them at 28 to 46 px. At that size the
hairline outlines fade, so the shape is hard to read on an e-paper panel.

Five pictures also cannot name every condition. Fog, drizzle and sleet share a
picture with another condition. `symbol()` ignored `is_day`, so a clear night
showed a sun.

[Weather Icons](https://erikflowers.github.io/weather-icons/) is a font with a
glyph for each condition and separate day and night glyphs. Its font is under
SIL OFL 1.1 and its code under MIT. A font gives solid shapes at small sizes
and needs one embedded file instead of many PNGs.

## Decision

1. The plugin embeds `icons/weathericons.ttf` and ships its `OFL.txt`. The five
   OpenMoji PNGs and their README go away.
2. The plugin parses the font once with `golang.org/x/image/font/opentype`. It
   draws each needed rune once into an `image.Gray` at about 128 px and turns
   the result into a `render.Mask` with `render.NewMask`. The mask is cached
   per glyph name and the widgets use `icon(name)`.
3. The glyph is drawn on a canvas with room around its ink. `render.NewMask`
   trims the canvas, so the glyph size inside the face does not matter.
4. `symbol(code, isDay)` maps every WMO code to a glyph name:
   - 0, 1: `day-sunny`, at night `night-clear`
   - 2: `day-cloudy`, at night `night-alt-partly-cloudy`
   - 3: `cloudy`
   - 45, 48: `fog`
   - 51, 53, 55: `sprinkle`
   - 56, 57, 66, 67: `sleet`
   - 61, 63, 65, 80, 81, 82: `rain`
   - 71, 73, 75, 77, 85, 86: `snow`
   - 95, 96, 99: `thunderstorm`
   - anything else: `cloudy`

   Day and night apply only to the codes 0, 1 and 2. The `days` widget always
   uses the day glyph.
5. `sdk/render` and `sdk/design` do not change. A change there raises the
   version of every plugin ([ADR 0107](0107-plugin-builds-reach-installations.md)).
   The weather plugin raises its own version to 1.3.0.

## Consequences

- The pictograms keep a solid shape at the small sizes of the widgets, and a
  clear night no longer shows a sun.
- Every WMO condition has a picture of its own.
- The plugin carries one 100 KiB font instead of five PNGs and imports
  `golang.org/x/image/font/opentype`, which `sdk/render` already brings into the
  module graph.
- A new glyph needs a name and its code point from `weather-icons.css` in
  `iconRunes`. The test fails when the font lacks a glyph the widgets can name.
- The screenshots on the plugin page still show the old pictograms until
  `scripts/screenshots.sh` runs again.
