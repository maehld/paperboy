# 0172: The firmware measures a press for five seconds

- Status: Accepted, implemented
- Date: 2026-11-26
- Amends: [ADR 0067](0067-buttons-and-the-menu.md) §4 (a held button reports at the hold timeout)
- Answers: issue #478

## Context

ADR 0067 leaves the long-press threshold to the server. The firmware only
measures the press and sends `hold_ms`. It waited 700 ms for the release and
queued the event at that timeout. The firmware timeout and the server threshold
had the same value.

This coupling made the threshold immovable. A higher threshold would never see
a held press, because no device reported more than 700 ms. A longer press also
lost its real duration.

## Decision

1. **The firmware waits for the release for up to five seconds.** `hold_ms` is
   the real duration up to that limit. A button still held after five seconds
   reports five seconds and starts its sync.

2. **The server threshold stays at 700 ms.** It can move up to five seconds
   without a firmware release. It still decides what a long press does.

3. **A normal tap is unchanged.** Its event is queued on release, as before.

## Alternatives

- **Keep the 700 ms timeout.** Rejected. Every threshold change needs a firmware
  release, and the real duration stays lost.
- **Send the release in a second event.** Rejected. It complicates the event
  queue and the sync for a value that the server does not need yet.
- **Use a smaller window, for example two seconds.** Rejected. Five seconds
  leaves room for a later threshold and costs only the hold time of the user.

## Consequences

- A long press takes effect on release, not at the threshold. A user who holds
  the button until the display reacts waits up to five seconds.
- The device stays awake for the hold, at most five seconds per press.
- The protocol, the server and the virtual display do not change.
- The defensive 60 s clamp stays in the firmware; the five-second timeout
  reaches it first.
