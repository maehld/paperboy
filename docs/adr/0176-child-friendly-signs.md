# 0176: Child-friendly smileys in the dexcom plugin

- Status: Accepted, implemented
- Date: 2026-11-27
- Builds on: [ADR 0151](0151-a-shared-design-language-for-plugin-widgets.md) (the design language), [ADR 0127](0127-plan-conditions-are-values.md) (a stale value answers no key), [ADR 0155](0155-cgram-glyphs-on-hd44780-text-displays.md) (the CGRAM glyphs)
- Answers: issue #489

## Context

The dexcom plugin draws a glucose reading, a trend arrow and a target range. A
child sees the page but cannot read the number. The e-paper has two, four or
sixteen gray levels and no colour. A red and green traffic light is therefore
no option, and colour vision is common enough to avoid colour alone.

The Dexcom app stays the alarm. The page refreshes in minutes. A symbol must
not claim that the data is current. Paperboy already draws a stale reading in
gray, and the `state` value answers no key
([ADR 0127](0127-plan-conditions-are-values.md)).

Earlier drafts used a target, a dot above or below a line, an octagon with an
exclamation mark, a crescent, a neutral face and a fill that followed the
value. None settled. A face is what a child reads fastest, and the operator
wants to pick its weight: a light outline for a calm page, a solid face for a
bold one. The urgent low should read as a face, not as a sign beside it.

## Decision

1. **A happy face means in the range.** The reading is in the target range
   (including the tight range). The face smiles.
2. **A sad face means above or below.** High and low share the sad face with a
   frown. The value, the band and the word (`Hoch`/`High`, `Niedrig`/`Low`) show
   the direction.
3. **The urgent low has an O-mouth.** The face is surprised: the eyes stay, the
   mouth is a small ring. There is no exclamation mark and no octagon.
4. **Stale data keeps the happy face in gray.** A missing or stale reading draws
   the happy face and the value in gray. There is no sleeping face and no
   crescent.
5. **The style is a choice.** The `glucose` widget gets
   `sign: none | outline | solid`, default `none`. `outline` draws the face as a
   line, `solid` fills it. The style does not change with the value; the same
   face keeps its weight in every state.
6. **The smiley is opt-in.** Existing pages do not change. The sign takes a
   column on the left; the sign and the reading are centred as a pair. A smiley
   below 24 px drops, and the value always stays.
7. **No neutral face, no target.** A face without a clear state is left out. The
   target and the octagon are gone.
8. **The clinical ranges stay in the clinical widgets.** The tight range and the
   very high range have no own face. `ranges`, `agp` and `values.tight` carry
   that detail.
9. **A stale reading loses the alarm box.** The value and the smiley are gray.
   The bar marker gets a white halo, so it stays visible in the gray band.
10. **The text display uses the existing glyphs.** The labels of the state cell
    carry `✓`, `↑`, `↓` and `!`. The HD44780 draws the check and the arrows from
    CGRAM ([ADR 0155](0155-cgram-glyphs-on-hd44780-text-displays.md)); `ascii`
    falls back to `OK`, `^` and `v`.
11. **The plugin draws the smileys itself.** The shapes are vector geometry in
    the plugin, not PNG masks. The protocol does not change. The plugin version
    rises ([ADR 0075](0075-plugin-versions.md)).

## Alternatives

- **A fill that follows the value.** It couples the face to the inverted box:
  outline in the range, solid out of it. The operator wants the style as a
  choice instead, because the page, not the reading, decides its weight.
- **The exclamation mark.** It sat beside the face like an added sign. The
  O-mouth is part of the face and reads at every size.
- **The target.** It is calm and precise, but a child reads a face faster.
- **The octagon.** It is the stop sign, but it sits oddly next to a face.
- **A neutral face.** It names no state and looks indifferent.
- **Gaze up or down.** It is elegant at 96 px, but the eyes move about 1 px at
  24 px.
- **A scared face for urgent.** It frightens the child more than it helps.
- **A red and green traffic light.** The panels have no colour, and colour alone
  fails for colour vision.
- **A buddy or mascot.** It costs room and turns the sign into a companion.
- **A protocol value for the sign.** The plugin draws its own widget. The host
  needs no new symbol language.

## Consequences

- The operator picks the weight: `outline` is light and quiet, `solid` is bold
  and reads at 24 px. The choice lives in the page.
- A child reads the state at a glance: happy, sad, surprised, or gray.
- High and low share the sad face. The word, the value and the band must carry
  the direction, and the sad face can feel like blame. The sign stays opt-in,
  and the value and the word stay factual.
- The smiley costs room. The value gets smaller beside it. A small cell drops
  the smiley.
- The clinical ranges stay in `ranges`, `agp` and `values.tight`; the smiley is
  deliberately coarse.
- The page is no alarm. The age and the gray face name old data. The Dexcom app
  keeps the alarm.
- The plugin version rises, and the docs, the screenshots and the example page
  follow.
