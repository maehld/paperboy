# 0173: A charging mark over an empty battery

- Status: Accepted
- Date: 2026-11-26
- Related: ADR 0121 (the segmented gauge), ADR 0166 (the shared battery gauge), issue #482

## Context

ADR 0121 point 4 and ADR 0166 point 6 punch the charging bolt out of the
charge. The mark is white on the filled part and black on the empty part. At
the boundary of the two it reads as two marks. At header size it becomes a
smudge, and a charging battery is hard to tell from a full one.

The operator wants to see the end of charge. While the device charges, the
mark matters more than the level. At 100 % the charger goes to standby, and
the full gauge is the signal that the charge is done.

## Decision

1. **The body is empty while charging.** Below 100 % the gauge draws the
   outline and the cap, but no segments. A black bolt stands over the body.
2. **The wall opens around the bolt.** A 1 px white gap follows the bolt
   shape. The top and bottom wall stay open for a few pixels around the
   intersections, so the bolt passes through the wall and does not end on it.
3. **The bolt stands over the body.** It reaches past the top and bottom. The
   body moves inward by 12 % of the icon height on each side while charging,
   so the whole mark stays inside the icon box and the layout does not change.
4. **100 % ends the mark.** At a fraction of 0.995 and above the full segmented
   gauge returns, also while the charger still reports `charging`. The value
   rounds to 100 % from 99.5, so the mark goes when the page says 100 %.
5. **The level is gone while charging.** The percentage beside the widget is
   the only progress. The header shows the mark alone.
6. **The other states stay.** 0 % while charging shows the bolt, not an empty
   battery. A missing reading keeps the slash. `warn` and `critical` keep the
   thicker outline.
7. **The shared gauge changes.** `DrawBattery` in `sdk/design` owns the mark,
   so the built-in gauge and the evcc gauge stay equal. Every plugin that
   imports `sdk/design` raises its version ([ADR 0107](0107-plugin-builds-reach-installations.md)).

## Alternatives

- **The bolt beside the gauge:** the level stays, but the icon grows and the
  mark reads as a second symbol.
- **A cable instead of the battery:** the end of charge is clear, but the
  cable is a smudge at header size and the warning outline disappears.
- **A horizontal bolt:** it fills the wide body, but at header size it reads
  as a "Z".
- **The bolt over the fill with a white gap:** the level stays, but the gap
  eats too much of the fill at header size.
- **The word "charging":** it is clear where text fits, but the header has no
  text.

## Consequences

- A charging battery shows no level. The value beside the widget carries it.
- The gauge body is a little shorter while charging and grows back at 100 %.
- The mark reads at 18 pixels and above. Below that it is coarse, but the full
  gauge at 100 % stays distinct.
- Point 4 of ADR 0121 is superseded. Point 6 of ADR 0166 is amended.
- Every plugin that imports `sdk/design` raises its version.
