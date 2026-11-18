# 0131: A plan rule alerts the device

- Status: Accepted, implemented
- Date: 2026-11-18
- Builds on: ADR 0025 (plans), 0041 (firmware), 0042 (device API), 0064 (device protocol v4), 0130 (what a plan refresh does)
- Answers: issue #318

## Context

A plan selects the page and the refresh, but it cannot alert. A condition rule can show a low glucose page, yet the device stays silent. An operator sees the warning only when they look at the panel.

An alert is not a condition. Conditions select a rule and never act. An alert is the effect of a matching rule. Two cases exist: one signal when a rule starts, and a repeated signal while it applies. A low glucose value must remind at every refresh until it recovers.

The hardware differs. The Sticky, E1001 and E1003 have a buzzer and an LED; the LCD2004 has a backlight but no buzzer. A plan must not name the sound or the light, or it would know the device.

## Decision

1. A rule names `alert` with `once` or `refresh`. `never` and the missing key stay quiet; `Encode` writes no key for them.
2. `once` alerts in the sync answer that starts the rule. A rule change is the same edge that resets the page cursor. A later sync of the same rule stays quiet, also with button input.
3. `refresh` alerts when the rule starts and on every sync with `reason: timer`. A button, touch, menu or result sync stays quiet. The rule's `refresh` decides the reminder interval.
4. The device API carries `alert: true` in a sync answer that wants an alert. The server takes it from the plan choice. A critical battery page and a plan without a matching rule carry none.
5. The firmware runs its alert script after the paint, also when the image hash did not change. `alert: refresh` must signal on unchanged content.
6. The device decides what the script does:
   - The Sticky beeps for 150 ms.
   - The E1001 and E1003 beep and blink the green LED twice.
   - The LCD2004 blinks its backlight twice.
   - A device without an alert path ignores the field.
7. The buzzer pins are GPIO48 on the Sticky and GPIO45 on the E1001 and E1003. The green LED sits on GPIO6 (E1001) and GPIO16 (E1003). The LCD2004 drives its backlight bit through the PCF8574.
8. The plan editor offers never, once and on refresh. The plan preview names the mode.

## Consequences

- A plan alerts without a plugin and without naming the hardware. The same rule sounds a Sticky, flashes an E1001 and blinks an LCD2004.
- `refresh` on a short refresh repeats the signal. That is intended for a low glucose reminder; `once` avoids it.
- A rule that already applies and later gains `alert: once` stays quiet until it starts again. `refresh` signals on the next timed refresh.
- An alert lives in the answer of one sync. A lost answer loses that alert; the next refresh signals again.
- An old server sends no `alert` and new firmware stays quiet. An old firmware ignores the field, so the protocol version stays.
- A new alert idea changes the firmware script of one hardware type. The plan key and the API field stay.

## Alternatives

- **`beep`.** It names one output although a device may blink instead. The LCD2004 has no buzzer.
- **A boolean `alert: true`.** It cannot tell a one-time alarm from a repeated reminder.
- **An alert condition.** Conditions select a rule and never act. A second meaning of the list would make an alert look like a value.
- **`paperboy/alert` as a plan action.** Actions run from the menu, not when a condition matches.
- **An alert only on a timer refresh.** A rule that starts with a button would stay silent; `refresh` also signals at the start.
- **A melody or pattern in the plan.** The plan would know the hardware. The device script decides.
