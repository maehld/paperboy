# 0166: A shared battery gauge

- Status: Accepted
- Date: 2026-11-25
- Related: ADR 0121 (the segmented gauge), ADR 0151 (the design language), ADR 0107 (the plugin versions), issue #451

## Context

ADR 0121 gave the built-in plugin a segmented battery gauge. Its drawing
lives in `server/internal/builtin`. The evcc plugin draws its own battery icon
for the flow diagram. The two use different frames, segments and marks, so one
charge looks unlike the other.

ADR 0121 says a later plugin repeats the drawing or gets a shared helper.
ADR 0151 already provides the shared helper package `sdk/design`.

## Decision

1. **One component in `sdk/design`.** `DrawBattery` takes a rectangle, a
   `Battery` value and a stroke. `Battery` carries the charge as a fraction, a
   missing reading, a charging mark and a level.
2. **The evcc outline wins.** The frame is rounded and thick. Its radius is
   22 % of the height. A stroke of zero follows the height.
3. **The fill never paints over the outer line.** On a tall gauge the outline
   lies on top of the fill. On a short gauge the fill lies over the inner edge,
   so it reaches the wall.
4. **The fill follows the wall.** The fill is clipped to the rounded inner
   contour, so the first segment does not square off the corner. On a gauge at
   least 32 pixels high, a gap separates the fill from the wall. On a smaller
   gauge the fill sits on the wall, because the gap would eat the fill.
5. **The cap is narrow and flat on the left.** Its width is at most a third of
   the height. It sits on the body and rounds only on the right.
6. **The marks stay.** Four or five segments show the charge. A bolt marks a
   charging battery. A slash marks a missing reading. `warn` and `critical`
   thicken the outline.
7. **The flow shows no bolt.** The evcc flow passes only the charge. Its
   arrows and the power value show charging and discharging.
8. **The change raises the plugin versions.** Every plugin that imports
   `sdk/design` raises its version ([ADR 0107](0107-plugin-builds-reach-installations.md)).

## Alternatives

- **Keep two drawings:** the same charge keeps two looks, and a later plugin
  must choose one.
- **Use `DrawMeter`:** a meter has no cap, no bolt and no level, so the gauge
  would lose what ADR 0121 added.
- **A continuous fill:** the gauge would no longer read in clear steps.
- **A bolt in the flow:** the flow already shows charging with the direction
  of the arrow and the power value.
- **The gap on every size:** on a short gauge the gap leaves too little fill.

## Consequences

- The built-in `battery` widget and the header gauge become rounded and thicker.
  The cap is narrower.
- The evcc flow icon uses the same gauge. It gains the rounded frame and loses
  its fixed four segments.
- `server/internal/builtin` drops its own gauge helpers. `sdk/design` owns the
  drawing and its tests.
- The screenshots of the paperboy and evcc pages change.
