# 0131: A plan rule sounds the buzzer

- Status: Accepted, implemented
- Date: 2026-11-18
- Builds on: ADR 0025 (plans), 0041 (firmware), 0042 (device API), 0064 (device protocol v4), 0130 (what a plan refresh does)
- Answers: issue #318

## Context

The three ePaper devices have a buzzer: the Sticky on GPIO48, the E1001 and E1003 on GPIO45. A plan selects the page and the refresh, but it cannot alert. A condition rule can show a low glucose page, yet the device stays silent. An operator sees the warning only when they look at the panel.

A beep is not a condition. Conditions select a rule and never act. A beep is the effect of a matching rule. Two cases exist: one signal when a rule starts, and a repeated signal while it applies. A low glucose value must remind at every refresh until it recovers.

## Decision

1. A rule names `beep` with `once` or `always`. `never` and the missing key stay quiet; `Encode` writes no key for them.
2. `once` sounds one beep in the sync answer that starts the rule. A rule change is the same edge that resets the page cursor. A later sync of the same rule stays quiet, also with button input.
3. `always` sounds a beep in every sync answer while the rule applies, including a timer refresh and a button wake. The rule's `refresh` decides the reminder interval.
4. The device API carries `beep: true` in a sync answer that wants a sound. The server takes it from the plan choice. A critical battery page and a plan without a matching rule carry none.
5. The firmware beeps after the panel finished the paint, also when the image hash did not change. `beep: always` must sound on unchanged content. The beep is 150 ms of a 2.7 kHz square wave on the LEDC output.
6. The buzzer pins are GPIO48 on the Sticky and GPIO45 on the E1001 and E1003.
7. The plan editor offers never, once and always. The plan preview names the mode.

## Consequences

- A plan alerts without a plugin. The rule already decides the page; it now decides the sound.
- `always` on a short refresh repeats the sound. That is intended for a low glucose reminder; `once` avoids it.
- A rule that already applies and later gains `beep: once` stays quiet until it starts again. `always` sounds on the next sync.
- A beep lives in the answer of one sync. A lost answer loses that beep; the next sync of `always` sounds again.
- An old server sends no `beep` and new firmware stays quiet. An old firmware ignores the field, so the protocol version stays.
- The LCD2004 text display has no buzzer and is unchanged.

## Alternatives

- **A boolean `beep: true`.** It cannot tell a one-time alarm from a repeated reminder. A low glucose rule needs the repetition.
- **A beep condition.** Conditions select a rule and never act. A second meaning of the list would make a beep look like a value.
- **`paperboy/beep` as a plan action.** Actions run from the menu, not when a condition matches.
- **A beep only on a timer refresh.** `always` is simpler to explain, and a button wake already means the user is at the device.
- **A melody or pattern.** A single short beep is enough. A pattern needs more config and more firmware code.
