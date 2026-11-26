# 0170: A long page key jumps to the end of the list

- Status: Accepted, implemented
- Date: 2026-11-26
- Amends: [ADR 0161](0161-home-syncs-and-the-menu-goes.md) §3 (a long next or previous jumps to the first page)
- Answers: issue #469

## Context

A long `previous` and a long `next` both jumped to the first page of the plan
(ADR 0161 §3). The keys are directional, yet the long press was not: a long
`next` from the first page did nothing.

Short presses already walk the extended list, the visible pages plus the page of
the active trigger (ADR 0158). The ends of that list are the two natural jump
targets.

## Decision

1. **A long `previous` jumps to the first page of the extended list.** A long
   `next` jumps to its last page.

2. **The extended list stays the one short presses page.** A hidden page is not
   in it, so no long press reaches a page that the plan hides.

3. **Several presses apply in order.** A wake that carries a short `next` and a
   long `next` moves one page and then to the last page, like any other wake
   (ADR 0042).

4. **The key-operated action mode keeps its meaning.** While the mode is open,
   `next` and `previous` move the selection, also when held (ADR 0163 §4).

5. **The protocol does not change.** The device sends `next`, `previous` and
   `home` with `hold_ms`; the server maps a held press to its page role.

## Alternatives

- **Keep both long presses on the first page.** Rejected. A directional key
  should not jump two ways to the same page.
- **Long `next` pages one page like a short press.** Rejected. Then the key
  loses its jump, and no press reaches the last page quickly.
- **Long `next` goes to the last page of the plan, including hidden pages.**
  Rejected. A hidden page is not in the rotation on purpose.
- **Let a held key wrap from the end to the start.** Rejected. A long press is
  a jump, not a page step.

## Consequences

- A device reaches both ends of its list with one press.
- The device protocol, the firmware and the plugins do not change.
- The virtual display needs no change: it already reports the hold time.
