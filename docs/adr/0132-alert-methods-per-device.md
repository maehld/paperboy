# 0132: Alert methods per device

- Status: Accepted, implemented
- Date: 2026-11-18
- Builds on: ADR 0032 (device files), 0042 (device API), 0131 (a plan rule alerts the device)
- Answers: issue #320

## Context

ADR 0131 makes a rule ask the device to signal. The server sends `beep` and `blink`, and each hardware runs the method it has. The choice is fixed per hardware: the Sticky beeps, the E1001 and E1003 beep and blink, the LCD2004 blinks.

An operator cannot mute one device or ask for a single method. A bedroom display should stay silent while the same plan alerts the kitchen. A display with a buzzer should be able to blink only. The plan must stay device-independent, so the choice belongs to the device file.

An alert is also intrusive: a beep at night, a blinking display in a bedroom. A new installation and an upgrade must not start to signal without the operator's decision.

## Decision

1. A device file names `alert_methods` as a list of `beep` and `blink`.
   - An omitted key keeps the device silent: alerting is opt-in.
   - An empty list is the same as an omitted key.
   - `[beep]`, `[blink]` and `[beep, blink]` enable one or both methods.
2. The server maps the list to the two flags of the sync answer. It adds no protocol field: the firmware still knows only `beep` and `blink`.
3. The firmware ignores a method its hardware lacks. Paperboy does not check the list against the hardware, so a Sticky with `[blink]` stays silent. A warning would need a capability report from the device; a later ADR may add one.
4. An unknown method and a duplicate are rejected when the file loads and when the editor saves it.
5. The device editor offers both, beep only, blink only and none. None writes no key.
6. The firmware does not change.

## Consequences

- A rule with `alert` does nothing on a device without `alert_methods`. The operator enables a method per device.
- An existing device file without the key stays silent after the upgrade. No device starts to beep or blink on its own.
- One plan alerts different devices in their chosen way. A silent device keeps its page but makes no sound and no light.
- A method the device does not have has no effect. The operator sees no warning yet.
- A written `[beep, blink]` and a written `[beep]` on a Sticky sound the same. The file names what the server may request, not what the hardware does.
- No firmware version changes, because only the server selects the flags.

## Alternatives

- **An omitted key means both.** An upgrade would make every device beep and blink without the operator asking. Rejected.
- **Two booleans `beep` and `blink`.** An omitted key and `false` need a third state; a list with a pointer spells the same in one field.
- **One value `beep | blink | both | none`.** It reads well but needs a new value for every method. A list extends without a new parser branch.
- **The methods on the plan rule.** The same plan serves devices of different kinds, so a rule-level choice is wrong for at least one of them.
- **A setting in the device web UI.** It would be invisible to Paperboy and outside the instance directory.
- **A capability report in registration.** It would let Paperboy warn about a method the device lacks. It adds a protocol field and a hardware table; defer it until a real need appears.
