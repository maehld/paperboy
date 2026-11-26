# 0170: A sign language for glucose states

- Status: Accepted, implemented
- Date: 2026-11-26
- Builds on: [ADR 0099](0099-glucose-readings-from-the-dexcom-share-service.md) (the Dexcom plugin), [ADR 0038](0038-icons.md) (a pictogram is a mask, not an emoji glyph), [ADR 0127](0127-plan-conditions-are-values.md) (the `state` value)
- Answers: issue #467

## Context

The `glucose` widget shows a number, a trend arrow, the change and a band. An
adult reads it in a second. A child who cannot read cannot use it.

The obvious answer is a face. A face judges, though: a smile says "good", and a
sad face for a high reading says the wrong thing. A smile on a stale reading is
dangerous. A face also looks childish to an older child.

The panels are monochrome, with one bit or four gray levels. The fonts contain
no emoji glyphs ([ADR 0038](0038-icons.md)). A sign must be a vector shape or a
mask.

A plan can switch pages on `dexcom/state`
([ADR 0127](0127-plan-conditions-are-values.md)). The page still needs a symbol
a child can read.

## Decision

The dexcom plugin adds a `sign` widget for image displays. It draws one large
sign for the state of the newest reading:

| State | Sign |
|---|---|
| in band | check |
| high | up arrow |
| low | down arrow and a candy |
| urgent low | stop sign and a candy |
| stale or no reading | question mark in the muted ink |

1. The signs are shapes, not faces and not text. A check, arrows, a stop sign
   and a question mark come from traffic and kindergarten. They stay legible on
   a one-bit panel.
2. A candy appears only with a low. Fast carbs are the one action a child can
   take alone. A high shows no action, because the correction belongs to an
   adult. The candy never replaces the adult.
3. A stale or missing reading shows a question mark. The widget never keeps the
   last sign, so a child never sees a check for data that is gone.
4. The mapping is fixed and has no argument. Every page shows the same sign for
   the same state, and the documentation explains it once.
5. The widget lives in the plugin, not in `sdk/design`. No other plugin has a
   comparable state. A second user moves the drawing into the design language.

The widget reads the thresholds from the same `level` function as the `glucose`
widget and the `state` value. It reads no new data and changes no protocol
version.

## Consequences

- A child can read the state without reading. A parent sees the same state in
  the detailed widgets on the same page.
- The family teaches the mapping once. The candy may raise the wish for a sweet.
- The signs are no alarm. A low still needs an adult and the alert of the plan
  ([ADR 0131](0131-a-plan-rule-alerts-the-device.md)).
- The example page `glukose-kind` shows the sign next to the `glucose` and
  `values` widgets.

## Alternatives

- **Smileys.** Rejected: a face judges, a sad face for a high reading is the
  wrong message, and a smile on stale data is unsafe.
- **A traffic light.** Rejected: the color carries the signal, and the panels
  are gray.
- **A target or a gauge.** Rejected: reading a scale is a skill, and a small
  cell turns the target into mush.
- **Action icons only.** Rejected: a high has no safe action for a child, and an
  hourglass says "wait" for a value that may need insulin.
- **A check, an adult and a question mark only.** This is the smallest set. It
  is safer, but it loses the direction. The chosen set keeps the direction and
  stays simple.
