# 0132: Alert methods per device

- Status: Accepted, implemented
- Date: 2026-11-18
- Builds on: ADR 0032 (device files), 0042 (device API), 0131 (a plan rule alerts the device)
- Answers: issue #320

## Context

ADR 0131 makes a rule ask the device to signal. The server sends `beep` and `blink`, and each hardware runs the method it has. The choice is fixed per hardware: the Sticky beeps, the E1001 and E1003 beep and blink, the LCD2004 blinks.

An operator cannot mute one device or ask for a single method. A bedroom display should stay silent while the same plan alerts the kitchen. A display with a buzzer should be able to blink only. The plan must stay device-independent, so the choice belongs to the device file.

## Decision

1. A device file names `alert_methods` as a list of `beep` and `blink`.
   - An omitted key keeps the hardware default from ADR 0131: both flags.
   - An empty list mutes the device: the server sends neither flag.
   - `[beep, blink]` is equal to the default.
2. The server maps the list to the two flags of the sync answer. It adds no protocol field: the firmware still knows only `beep` and `blink`.
3. The firmware ignores a method its hardware lacks. Paperboy does not check the list against the hardware, so a Sticky with `[blink]` stays silent. A warning would need a capability report from the device; a later ADR may add one.
4. An unknown method and a duplicate are rejected when the file loads and when the editor saves it.
5. The device editor offers hardware default, beep and blink, beep only, blink only and silent.
6. The firmware does not change.

## Consequences

- One plan alerts different devices in their chosen way. A silent device keeps its page but makes no sound and no light.
- A method the device does not have has no effect. The operator sees no warning yet.
- An omitted key keeps the behavior of ADR 0131, so existing files stay as they are.
- A written `[beep, blink]` and an omitted key mean the same. The editor writes the list only after the operator changes it.
- No firmware version changes, because only the server narrows the flags.

## Alternatives

- **Two booleans `beep` and `blink`.** An omitted key and `false` need a third state; a list with a pointer spells the same in one field.
- **One value `beep | blink | both | none`.** It reads well but needs a new value for every method. A list extends without a new parser branch.
- **The methods on the plan rule.** The same plan serves devices of different kinds, so a rule-level choice is wrong for at least one of them.
- **A setting in the device web UI.** It would be invisible to Paperboy and outside the instance directory.
- **A capability report in registration.** It would let Paperboy warn about a method the device lacks. It adds a protocol field and a hardware table; defer it until a real need appears.
