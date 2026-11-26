# 0171: A tap on the active option closes the action mode

- Status: Accepted, implemented
- Date: 2026-11-26
- Amends: [ADR 0165](0165-a-plugin-describes-its-action-options.md) point 7
- Answers: issue #476

## Context

ADR 0165 gave each option an `active` flag. The current option is drawn as an
inverted tile. A tap on that tile ran nothing and left the mode open. Only a tap
outside the tiles, a page key or 20 seconds without input closed it.

The active tile shows the current state. A tap on it says "nothing to change".
A tap outside the tiles closes the mode already. A key-operated device closes
the mode with a short `home` when no runnable option exists (ADR 0163).

## Decision

1. A tap on the active option closes the mode and runs nothing.
2. A tap on a disabled option still runs nothing and keeps the mode open. The
   operator can pick another option after a precondition blocks one.
3. The key path does not change. It skips active options, and a short `home`
   without a runnable option closes the mode.

## Alternatives

- **Keep the mode open.** A second gesture costs time, and the touch view
  differs from the key view.
- **Close on every non-runnable tile.** A disabled tile names a precondition,
  not a finished decision. A mistaken tap would end the mode.

## Consequences

- The active tile dismisses the touch view like a tap outside.
- A plugin below protocol 10 marks its current value as `disabled`, not
  `active`. A tap on that tile keeps the mode open until the plugin reports
  protocol 10.
