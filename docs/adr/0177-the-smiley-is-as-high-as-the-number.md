# 0177: The smiley is as high as the number

- Status: Accepted, implemented
- Date: 2026-11-27
- Builds on: [ADR 0176](0176-child-friendly-signs.md) (the smiley family), [ADR 0151](0151-a-shared-design-language-for-plugin-widgets.md) (the type scale), [ADR 0050](0050-physical-scaling-by-display-ppi.md) (points and ppi)
- Answers: issue #496

## Context

ADR 0176 introduced the smiley family. The implementation kept a fixed 72 px
cap from an earlier draft. On the e1003 the number grows with the type scale,
but the face stayed at 72 px, so it looked small next to the reading. The
merged code also dropped the exclamation mark when the O-mouth arrived; the
docs and the collage still showed it.

## Decision

1. **The face is as high as the capitals of the number.** The capitals fill
   72 % of the font size, the face fills 94 % of its square. The square is
   therefore 0.72 / 0.94 ≈ 0.77 of the value size.
2. **The face and the reading stay one pair.** The sign holds a column on the
   left; the value box and its ring count into the gap. A face below 24 px
   drops, and the value always stays.
3. **The urgent low is the surprised O-mouth alone.** The value box, the ring
   and the word carry the alarm. No mark sits beside the face.
4. **The style stays a choice.** `outline` or `solid` comes from the page
   ([ADR 0176](0176-child-friendly-signs.md)). The size follows the number, not
   the cell; the value's Hero cap stays.

## Alternatives

- **Keep the 72 px cap.** It is simple, but a large panel and a large reading
  get a small face.
- **Size the face like the cell.** It would fill a huge cell, but the face would
  lose its relation to the number and the page's type scale.
- **Keep the exclamation mark.** It was a leftover from before the O-mouth; the
  face already names the urgent low, and the value box and the word carry the
  alarm.
- **Change the Hero itself.** Whether the number grows beyond the type scale is
  a separate decision about the design language; this ADR does not take it.

## Consequences

- The face follows the number on every panel. The e1003 gets a face at the
  number's height instead of a fixed 72 px.
- The number still follows the Hero of the type scale. A very large cell keeps
  its empty room; the face does not drive the number.
- The docs, the collage and the screenshots lose the leftover mark and show the
  face at the number's height.
- The plugin version rises, because the rendering changes.
