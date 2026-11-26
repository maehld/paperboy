# 0173: Clothing advice from the apparent temperature

- Status: Accepted, implemented
- Date: 2026-11-26
- Replaces: the `clothing` argument of `current` removed in #393
- Answers: issue #429

## Context

The weather page tells the household what the day brings. It also wants to
answer the next question: what do we wear? The old `current` widget had a
`clothing: true` argument that showed an OpenMoji T-shirt or a coat. That
argument belonged to the v1 icon templates (ADR 0038), reduced the advice to
two states and used the current apparent temperature and the wind force. #393
removed it, and the page lost the advice.

The display hangs in the hallway, so the advice must read at a glance. It must
also cover the layers that a day with a wide temperature range needs: a warm
afternoon and a cold morning. A single pictogram cannot show a combination.

## Decision

1. **The weather plugin gets its own `clothing` widget.** `current` stays a
   weather widget and carries no clothing argument.
2. **The advice uses the daily apparent temperature.** It already includes
   wind and humidity. The top and the trousers follow the daily maximum, the
   outer layer and the accessories follow the daily minimum. The widget
   therefore advises the whole day, not the current moment.
3. **The bands are fixed.** A felt maximum of 25 °C or more is a T-shirt.
   18–24 °C is a long-sleeve shirt, 10–17 °C a pullover and below 10 °C a
   pullover with a thermal layer. Shorts need a felt maximum of 24 °C. A felt
   minimum of 16 °C or more needs no jacket. 9–15 °C needs a light jacket,
   1–8 °C a jacket and below 1 °C a winter coat.
4. **Accessories follow the felt minimum.** A scarf starts below 8 °C, a hat
   below 5 °C and gloves below 0 °C.
5. **The day's weather adds extras.** Rain at 50 % probability or 1 mm
   replaces a light outer layer with a rain jacket. Over a winter coat the
   widget names the rain jacket as an extra layer. Snow adds waterproof shoes.
   Gusts at 6 Bft add windproof clothing; gusts at 8 Bft add no umbrella. A
   clear day at 28 °C or more adds sun protection.
6. **The outfit is a row of pictograms.** The widget shows up to six OpenMoji
   pictograms in the order you dress: top, trousers, jacket, scarf, hat and
   gloves. It draws only what the day needs. A cell at least three times as
   wide as high puts the row beside the text; every other cell puts it below.
   The plugin draws the row with `sdk/render`, so the shared design language
   gains no component for one widget.
7. **The headline names the outermost garment**, or the top when no jacket is
   needed. The caption gives the felt range, and the detail lines carry the
   extras that have no pictogram.

## Consequences

- The plugin reads the daily apparent minimum and maximum, the maximum gusts
  and the snow sum from Open-Meteo.
- The advice is a recommendation for the day and cannot follow a warm noon.
  A personalised rule set would need arguments or a settings page later.
- The pictograms `tshirt`, `pullover`, `jacket`, `trousers`, `shorts`, `scarf`,
  `hat` and `gloves` are back in the plugin, and the two old ones return.
- A cell that has room for the text but not for the row keeps the text. A cell
  too small for the text still answers `ErrTooSmall`.
